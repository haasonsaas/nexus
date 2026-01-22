// Package agent provides the core runtime and abstractions for LLM-powered agent workflows.
//
// This package implements the agent orchestration layer of Nexus, handling:
//   - LLM provider abstraction (Anthropic, OpenAI, etc.)
//   - Tool registration and execution
//   - Session-aware conversation management
//   - Streaming response handling
//
// # Architecture Overview
//
// The agent package follows a layered architecture:
//
//	┌─────────────────────────────────────────┐
//	│              Runtime                     │  Orchestration layer
//	├─────────────────────────────────────────┤
//	│  ToolRegistry    │    Sessions.Store    │  State management
//	├─────────────────────────────────────────┤
//	│            LLMProvider                  │  Provider abstraction
//	└─────────────────────────────────────────┘
//
// # Basic Usage
//
//	// Create a runtime with Anthropic provider
//	provider, _ := providers.NewAnthropicProvider(config)
//	store := sessions.NewMemoryStore()
//	runtime := agent.NewRuntime(provider, store)
//
//	// Register tools
//	runtime.RegisterTool(websearch.New(apiKey))
//	runtime.RegisterTool(sandbox.New(config))
//
//	// Process a message
//	session := &models.Session{ID: "user-123"}
//	msg := &models.Message{Role: "user", Content: "Search for Go tutorials"}
//
//	chunks, _ := runtime.Process(ctx, session, msg)
//	for chunk := range chunks {
//	    fmt.Print(chunk.Text)
//	}
//
// # Tool Execution
//
// Tools are executed when the LLM returns tool call requests:
//
//  1. LLM receives user message and available tools
//  2. LLM returns tool call with name and JSON arguments
//  3. Runtime looks up tool in registry
//  4. Tool executes and returns result
//  5. Result is sent back to LLM for final response
//
// # Streaming
//
// All responses are streamed via Go channels for real-time delivery:
//
//	chunks, _ := runtime.Process(ctx, session, msg)
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Printf("Error: %v", chunk.Error)
//	        break
//	    }
//	    if chunk.Text != "" {
//	        fmt.Print(chunk.Text)
//	    }
//	    if chunk.ToolResult != nil {
//	        log.Printf("Tool executed: %s", chunk.ToolResult.Content)
//	    }
//	}
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	agentctx "github.com/haasonsaas/nexus/internal/agent/context"
	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

type systemPromptKey struct{}

// WithSystemPrompt stores a request-scoped system prompt override in the context.
func WithSystemPrompt(ctx context.Context, prompt string) context.Context {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ctx
	}
	return context.WithValue(ctx, systemPromptKey{}, prompt)
}

func systemPromptFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(systemPromptKey{}).(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

type toolPolicyKey struct{}
type toolResolverKey struct{}

// WithToolPolicy stores a tool policy override in the context.
func WithToolPolicy(ctx context.Context, resolver *policy.Resolver, toolPolicy *policy.Policy) context.Context {
	if resolver == nil || toolPolicy == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, toolResolverKey{}, resolver)
	return context.WithValue(ctx, toolPolicyKey{}, toolPolicy)
}

func toolPolicyFromContext(ctx context.Context) (*policy.Resolver, *policy.Policy, bool) {
	resolver, ok := ctx.Value(toolResolverKey{}).(*policy.Resolver)
	if !ok || resolver == nil {
		return nil, nil, false
	}
	pol, ok := ctx.Value(toolPolicyKey{}).(*policy.Policy)
	if !ok || pol == nil {
		return nil, nil, false
	}
	return resolver, pol, true
}

// LLMProvider defines the interface for Large Language Model backends.
//
// Implementations of this interface handle the specifics of communicating with
// different LLM APIs (Anthropic, OpenAI, etc.) while presenting a unified
// streaming interface to the runtime.
//
// Thread Safety:
// Implementations must be safe for concurrent use. Multiple goroutines may
// call Complete() simultaneously for different requests.
//
// See Also:
//   - providers.AnthropicProvider for Anthropic Claude implementation
//   - providers.OpenAIProvider for OpenAI GPT implementation
type LLMProvider interface {
	// Complete sends a prompt and returns a streaming response.
	Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error)

	// Name returns the provider name.
	Name() string

	// Models returns available models.
	Models() []Model

	// SupportsTools returns whether the provider supports tool use.
	SupportsTools() bool
}

// CompletionRequest contains all parameters for an LLM completion request.
//
// This struct represents a complete request to an LLM provider, including
// the conversation history, system prompt, available tools, and generation
// parameters.
//
// Example:
//
//	req := &CompletionRequest{
//	    Model:     "claude-sonnet-4-20250514",
//	    System:    "You are a helpful coding assistant.",
//	    Messages:  []CompletionMessage{
//	        {Role: "user", Content: "Write a hello world in Go"},
//	    },
//	    MaxTokens: 1024,
//	}
type CompletionRequest struct {
	// Model specifies which LLM model to use (e.g., "claude-sonnet-4-20250514", "gpt-4o").
	// If empty, the provider's default model is used.
	Model string `json:"model"`

	// System is the system prompt that sets the assistant's behavior and personality.
	// This is handled separately from messages in most LLM APIs.
	System string `json:"system,omitempty"`

	// Messages contains the conversation history in chronological order.
	// Must include at least one message (typically the user's query).
	Messages []CompletionMessage `json:"messages"`

	// Tools defines available tools/functions the LLM can request to execute.
	// If empty, no tool calling is available.
	Tools []Tool `json:"tools,omitempty"`

	// MaxTokens limits the maximum length of the generated response.
	// If 0 or negative, the provider's default is used (typically 4096).
	MaxTokens int `json:"max_tokens,omitempty"`
}

// CompletionMessage represents a single message in a conversation.
//
// Messages can contain:
//   - Text content (user queries, assistant responses)
//   - Tool calls (assistant requesting tool execution)
//   - Tool results (responses from executed tools)
//   - Attachments (images, files for vision-capable models)
//
// Role values: "user", "assistant", "tool"
type CompletionMessage struct {
	// Role indicates who sent the message: "user", "assistant", or "tool"
	Role string `json:"role"`

	// Content is the text content of the message (may be empty for tool-only messages)
	Content string `json:"content,omitempty"`

	// ToolCalls contains any tool execution requests from the assistant
	ToolCalls []models.ToolCall `json:"tool_calls,omitempty"`

	// ToolResults contains responses from executed tools
	ToolResults []models.ToolResult `json:"tool_results,omitempty"`

	// Attachments contains images or files for vision-capable models
	Attachments []models.Attachment `json:"attachments,omitempty"`
}

// CompletionChunk represents a single chunk in a streaming LLM response.
//
// Chunks are delivered through channels as the LLM generates its response.
// Each chunk may contain:
//   - Partial text (most common - streaming text generation)
//   - A complete tool call (when LLM wants to execute a tool)
//   - Done signal (indicating stream completion)
//   - Error (if something went wrong)
//
// Processing Example:
//
//	for chunk := range chunks {
//	    switch {
//	    case chunk.Error != nil:
//	        return chunk.Error
//	    case chunk.ToolCall != nil:
//	        result := executeToolCall(chunk.ToolCall)
//	        // Continue conversation with result...
//	    case chunk.Text != "":
//	        fmt.Print(chunk.Text) // Stream to user
//	    case chunk.Done:
//	        break
//	    }
//	}
type CompletionChunk struct {
	// Text contains partial response text (streamed incrementally)
	Text string `json:"text,omitempty"`

	// ToolCall contains a complete tool execution request (when LLM needs tool output)
	ToolCall *models.ToolCall `json:"tool_call,omitempty"`

	// Done is true when the stream has completed successfully
	Done bool `json:"done,omitempty"`

	// Error contains any error that occurred (streaming is terminated)
	Error error `json:"-"`
}

// Model describes an available LLM model and its capabilities.
//
// This metadata is used for:
//   - Displaying available models to users
//   - Validating model selection
//   - Checking capability requirements (vision, context size)
type Model struct {
	// ID is the API identifier for the model (e.g., "claude-sonnet-4-20250514")
	ID string `json:"id"`

	// Name is the human-readable model name (e.g., "Claude Sonnet 4")
	Name string `json:"name"`

	// ContextSize is the maximum token context window
	ContextSize int `json:"context_size"`

	// SupportsVision indicates if the model can process images
	SupportsVision bool `json:"supports_vision"`
}

// Tool defines the interface for executable agent tools.
//
// Tools extend the agent's capabilities by allowing it to:
//   - Search the web
//   - Execute code in sandboxes
//   - Browse websites
//   - Query databases
//   - Call external APIs
//
// Implementing a Tool:
//
//	type Calculator struct{}
//
//	func (c *Calculator) Name() string { return "calculator" }
//
//	func (c *Calculator) Description() string {
//	    return "Performs mathematical calculations"
//	}
//
//	func (c *Calculator) Schema() json.RawMessage {
//	    return json.RawMessage(`{
//	        "type": "object",
//	        "properties": {
//	            "expression": {"type": "string", "description": "Math expression"}
//	        },
//	        "required": ["expression"]
//	    }`)
//	}
//
//	func (c *Calculator) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
//	    var input struct{ Expression string `json:"expression"` }
//	    json.Unmarshal(params, &input)
//	    result := evaluate(input.Expression)
//	    return &ToolResult{Content: result}, nil
//	}
type Tool interface {
	// Name returns the tool name for LLM function calling.
	// Must be a valid function name (alphanumeric, underscores).
	Name() string

	// Description returns a natural language description of what the tool does.
	// This helps the LLM decide when to use the tool.
	Description() string

	// Schema returns the JSON Schema defining the tool's parameters.
	// The LLM uses this to construct valid tool call arguments.
	Schema() json.RawMessage

	// Execute runs the tool with the given JSON parameters.
	// The params match the schema returned by Schema().
	// Returns the tool output or an error.
	Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}

// ToolResult contains the output from a tool execution.
//
// Results are sent back to the LLM which uses them to formulate
// its final response. Errors are also communicated via ToolResult
// with IsError=true, allowing the LLM to handle failures gracefully.
type ToolResult struct {
	// Content is the tool's output (text, JSON, etc.)
	Content string `json:"content"`

	// IsError indicates this result represents an error condition
	IsError bool `json:"is_error,omitempty"`
}

// ToolEventStore persists tool calls and results for audit, replay, and analytics.
// This is optional - if nil, tool events are not persisted separately from messages.
type ToolEventStore interface {
	// AddToolCall persists a tool call event.
	AddToolCall(ctx context.Context, sessionID, messageID string, call *models.ToolCall) error

	// AddToolResult persists a tool result event.
	AddToolResult(ctx context.Context, sessionID, messageID string, call *models.ToolCall, result *models.ToolResult) error
}

// Runtime orchestrates agent conversations with LLM providers and tools.
//
// The Runtime is the central coordination point for agent interactions:
//   - Manages conversation history via session storage
//   - Sends requests to configured LLM provider
//   - Executes tool calls requested by the LLM
//   - Streams responses back to callers
//
// Thread Safety:
// Runtime is safe for concurrent use. Multiple goroutines can call Process()
// simultaneously for different sessions.
//
// Example:
//
//	runtime := NewRuntime(anthropicProvider, sessionStore)
//	runtime.RegisterTool(websearch.New(apiKey))
//
//	chunks, _ := runtime.Process(ctx, session, userMessage)
//	for chunk := range chunks {
//	    // Handle streaming response
//	}
type Runtime struct {
	// provider is the LLM backend (Anthropic, OpenAI, etc.)
	provider LLMProvider

	// tools holds registered tools available for LLM function calling
	tools *ToolRegistry

	// sessions stores conversation history for continuity
	sessions sessions.Store

	// toolEvents optionally persists tool calls/results for audit and replay
	toolEvents ToolEventStore

	// opts configures runtime behavior (tool loop, approvals, async jobs).
	opts RuntimeOptions

	// defaultModel is used when requests omit a model
	defaultModel string

	// defaultSystem is used when requests omit a system prompt
	defaultSystem string

	// maxIterations limits the agentic loop iterations (default 5)
	maxIterations int

	// maxWallTime limits the total run duration (0 = no limit)
	maxWallTime time.Duration

	// toolExec configures tool execution behavior (timeouts, concurrency)
	toolExec ToolExecConfig

	// packOpts configures context packing behavior
	packOpts *agentctx.PackOptions

	// summarizeConfig configures conversation summarization
	summarizeConfig *agentctx.SummarizationConfig

	// plugins holds registered plugins for event hooks
	plugins *PluginRegistry
}

// NewRuntime creates a new agent runtime with the given provider and session store.
//
// The runtime is initialized with an empty tool registry. Use RegisterTool()
// to add tools after creation.
//
// Parameters:
//   - provider: LLM backend to use for completions
//   - sessions: Storage for conversation history
//
// Returns:
//   - *Runtime: Initialized runtime ready for use
//
// Example:
//
//	provider, _ := providers.NewAnthropicProvider(config)
//	store := sessions.NewCockroachStore(db)
//	runtime := NewRuntime(provider, store)
func NewRuntime(provider LLMProvider, sessions sessions.Store) *Runtime {
	return NewRuntimeWithOptions(provider, sessions, DefaultRuntimeOptions())
}

// NewRuntimeWithOptions creates a runtime with custom options.
func NewRuntimeWithOptions(provider LLMProvider, sessions sessions.Store, opts RuntimeOptions) *Runtime {
	opts = mergeRuntimeOptions(DefaultRuntimeOptions(), opts)
	runtime := &Runtime{
		provider: provider,
		tools:    NewToolRegistry(),
		sessions: sessions,
		opts:     opts,
		plugins:  NewPluginRegistry(),
	}
	if opts.MaxIterations > 0 {
		runtime.maxIterations = opts.MaxIterations
	}
	if opts.ToolParallelism > 0 || opts.ToolTimeout > 0 {
		runtime.toolExec = ToolExecConfig{
			Concurrency:    opts.ToolParallelism,
			PerToolTimeout: opts.ToolTimeout,
		}
	}
	return runtime
}

// SetOptions updates runtime behavior options.
func (r *Runtime) SetOptions(opts RuntimeOptions) {
	r.opts = mergeRuntimeOptions(r.opts, opts)
	if r.opts.MaxIterations > 0 {
		r.maxIterations = r.opts.MaxIterations
	}
	if r.opts.ToolParallelism > 0 || r.opts.ToolTimeout > 0 {
		r.toolExec = ToolExecConfig{
			Concurrency:    r.opts.ToolParallelism,
			PerToolTimeout: r.opts.ToolTimeout,
		}
	}
}

// SetDefaultModel configures the fallback model used when requests omit a model.
func (r *Runtime) SetDefaultModel(model string) {
	r.defaultModel = model
}

// SetSystemPrompt configures the fallback system prompt used when requests omit one.
func (r *Runtime) SetSystemPrompt(system string) {
	r.defaultSystem = system
}

// SetToolEventStore configures optional tool event persistence for audit and replay.
func (r *Runtime) SetToolEventStore(store ToolEventStore) {
	r.toolEvents = store
}

// SetMaxIterations configures the maximum agentic loop iterations (default 5).
func (r *Runtime) SetMaxIterations(max int) {
	r.maxIterations = max
}

// SetMaxWallTime configures the maximum total run duration.
// A value of 0 (default) means no limit.
func (r *Runtime) SetMaxWallTime(d time.Duration) {
	r.maxWallTime = d
}

// SetToolExecConfig configures tool execution behavior (timeouts, concurrency).
func (r *Runtime) SetToolExecConfig(config ToolExecConfig) {
	r.toolExec = config
}

// SetPackOptions configures context packing behavior.
func (r *Runtime) SetPackOptions(opts *agentctx.PackOptions) {
	r.packOpts = opts
}

// SetSummarizationConfig configures conversation summarization.
func (r *Runtime) SetSummarizationConfig(config *agentctx.SummarizationConfig) {
	r.summarizeConfig = config
}

// Use registers a plugin to receive agent events during processing.
// Plugins are called in registration order for each event.
//
// Example:
//
//	runtime.Use(&LoggerPlugin{})
//	runtime.Use(agent.PluginFunc(func(ctx context.Context, e models.AgentEvent) {
//	    log.Printf("Event: %s", e.Type)
//	}))
func (r *Runtime) Use(p Plugin) {
	r.plugins.Use(p)
}

// buildCompletionMessages converts stored message history to CompletionMessage slice.
// This handles the mapping of all role types including tool calls and results.
func (r *Runtime) buildCompletionMessages(history []*models.Message) ([]CompletionMessage, error) {
	out := make([]CompletionMessage, 0, len(history))

	for _, m := range history {
		if m == nil {
			continue
		}

		if m.Role == "" {
			return nil, fmt.Errorf("history message missing role (id=%s)", m.ID)
		}

		cm := CompletionMessage{
			Role: string(m.Role),
		}

		if m.Content != "" {
			cm.Content = m.Content
		}
		if len(m.Attachments) > 0 {
			cm.Attachments = m.Attachments
		}
		if len(m.ToolCalls) > 0 {
			cm.ToolCalls = m.ToolCalls
		}
		if len(m.ToolResults) > 0 {
			cm.ToolResults = m.ToolResults
		}

		out = append(out, cm)
	}

	return out, nil
}

// RegisterTool adds a tool to the runtime, making it available for LLM function calling.
//
// Tools are registered by name and can be invoked by the LLM during conversations.
// Registering a tool with the same name as an existing tool will overwrite it.
//
// Parameters:
//   - tool: Tool implementation to register
//
// Example:
//
//	runtime.RegisterTool(websearch.New(apiKey))
//	runtime.RegisterTool(sandbox.New(sandboxConfig))
//	runtime.RegisterTool(browser.New(browserConfig))
func (r *Runtime) RegisterTool(tool Tool) {
	r.tools.Register(tool)
}

// Process handles an incoming message and streams the response.
//
// This is the main entry point for agent interactions. It:
//  1. Retrieves conversation history from session storage
//  2. Builds a completion request with history + new message + tools
//  3. Sends the request to the LLM provider
//  4. Streams response chunks to the returned channel
//  5. Executes any tool calls requested by the LLM
//
// The returned channel will receive ResponseChunks until the stream completes.
// The channel is closed when processing finishes (success or error).
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - session: Session containing conversation metadata
//   - msg: The new user message to process
//
// Returns:
//   - <-chan *ResponseChunk: Channel of streaming response chunks
//   - error: Returns error only for immediate failures (not streaming errors)
//
// Example:
//
//	session := &models.Session{ID: "user-123", Channel: "telegram"}
//	msg := &models.Message{Role: "user", Content: "What's the weather?"}
//
//	chunks, err := runtime.Process(ctx, session, msg)
//	if err != nil {
//	    return err
//	}
//
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        return chunk.Error
//	    }
//	    fmt.Print(chunk.Text)
//	}
func (r *Runtime) Process(ctx context.Context, session *models.Session, msg *models.Message) (<-chan *ResponseChunk, error) {
	chunks := make(chan *ResponseChunk, processBufferSize)

	go func() {
		defer close(chunks)

		// Create ChunkAdapterSink to convert AgentEvents to ResponseChunks
		chunkSink := NewChunkAdapterSink(chunks)

		// Also dispatch to plugins
		pluginSink := NewPluginSink(r.plugins)
		sink := NewMultiSink(chunkSink, pluginSink)

		// Create emitter with the combined sink
		runID := session.ID + "-" + msg.ID
		emitter := NewEventEmitter(runID, sink)

		// Run the core agentic loop
		// Errors are emitted as run.error events which ChunkAdapterSink converts to ResponseChunk.Error
		_ = r.run(ctx, session, msg, emitter)
	}()

	return chunks, nil
}

// run is the core runner that executes the agentic loop, emitting AgentEvents.
// This is the single source of truth for agent execution - both Process() and
// ProcessStream() delegate to this method.
//
// The emitter dispatches events to whatever sink(s) are configured.
// Returns nil on success, error on fatal failures.
func (r *Runtime) run(ctx context.Context, session *models.Session, msg *models.Message, emitter *EventEmitter) error {
	// Apply wall time limit if configured
	var cancel context.CancelFunc
	wallTimeLimit := r.maxWallTime
	if wallTimeLimit > 0 {
		ctx, cancel = context.WithTimeout(ctx, wallTimeLimit)
		defer cancel()
	}

	// 1) Load history (pre-incoming message)
	history, err := r.sessions.GetHistory(ctx, session.ID, 50)
	if err != nil {
		emitter.RunError(ctx, err, false)
		return err
	}

	// 2) Persist inbound user message (source of truth)
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.SessionID == "" {
		msg.SessionID = session.ID
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	if msg.Direction == "" {
		msg.Direction = models.DirectionInbound
	}
	if err := r.sessions.AppendMessage(ctx, session.ID, msg); err != nil {
		wrappedErr := fmt.Errorf("failed to persist user message: %w", err)
		emitter.RunError(ctx, wrappedErr, false)
		return wrappedErr
	}

	// 3) Optional summarization
	var summaryMsg *models.Message
	if r.summarizeConfig != nil {
		summaryMsg = agentctx.FindLatestSummary(history)

		cfg := *r.summarizeConfig
		summaryProvider := &llmSummaryProvider{runtime: r}
		summarizer := agentctx.NewSummarizer(summaryProvider, cfg)

		if summarizer.ShouldSummarize(history, summaryMsg) {
			newSummary, sumErr := summarizer.Summarize(ctx, session.ID, history, summaryMsg)
			if sumErr != nil {
				emitter.RunError(ctx, sumErr, false)
				return sumErr
			}
			if newSummary != nil {
				if newSummary.ID == "" {
					newSummary.ID = uuid.NewString()
				}
				if newSummary.SessionID == "" {
					newSummary.SessionID = session.ID
				}
				if newSummary.CreatedAt.IsZero() {
					newSummary.CreatedAt = time.Now()
				}
				if err := r.sessions.AppendMessage(ctx, session.ID, newSummary); err != nil {
					wrappedErr := fmt.Errorf("failed to persist summary message: %w", err)
					emitter.RunError(ctx, wrappedErr, false)
					return wrappedErr
				}
				summaryMsg = newSummary
			}
		}
	} else {
		summaryMsg = agentctx.FindLatestSummary(history)
	}

	// 4) Context packing
	packOpts := agentctx.DefaultPackOptions()
	if r.packOpts != nil {
		packOpts = *r.packOpts
	}
	packer := agentctx.NewPacker(packOpts)

	packResult := packer.PackWithDiagnostics(history, msg, summaryMsg)
	packedModels := packResult.Messages

	// Emit context packed event with diagnostics
	emitter.ContextPacked(ctx, packResult.Diagnostics)

	// 5) System prompt composition
	var systemParts []string
	if system, ok := systemPromptFromContext(ctx); ok {
		systemParts = append(systemParts, system)
	} else if r.defaultSystem != "" {
		systemParts = append(systemParts, r.defaultSystem)
	}

	nonSystemPacked := make([]*models.Message, 0, len(packedModels))
	for _, m := range packedModels {
		if m == nil {
			continue
		}
		if m.Role == models.RoleSystem {
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}
			continue
		}
		nonSystemPacked = append(nonSystemPacked, m)
	}

	messages, err := r.buildCompletionMessages(nonSystemPacked)
	if err != nil {
		emitter.RunError(ctx, err, false)
		return err
	}

	// 6) Tools (filtered by policy)
	tools := r.tools.AsLLMTools()
	var resolver *policy.Resolver
	var toolPolicy *policy.Policy
	if res, pol, ok := toolPolicyFromContext(ctx); ok {
		resolver, toolPolicy = res, pol
		tools = filterToolsByPolicy(resolver, toolPolicy, tools)
	}

	// 7) Build base request
	req := &CompletionRequest{
		Messages:  messages,
		Tools:     tools,
		MaxTokens: 4096,
	}
	if r.defaultModel != "" {
		req.Model = r.defaultModel
	}
	if len(systemParts) > 0 {
		req.System = strings.Join(systemParts, "\n\n")
	}

	// Tool executor config
	toolExecCfg := r.toolExec
	if toolExecCfg.Concurrency <= 0 || toolExecCfg.PerToolTimeout <= 0 {
		toolExecCfg = DefaultToolExecConfig()
	}
	toolExec := NewToolExecutor(r.tools, toolExecCfg)

	// 8) Agentic loop
	maxIters := r.maxIterations
	if maxIters <= 0 {
		maxIters = 5
	}

	for iter := 0; iter < maxIters; iter++ {
		select {
		case <-ctx.Done():
			return r.handleContextDone(ctx, emitter, wallTimeLimit)
		default:
		}

		emitter.SetIter(iter)
		emitter.IterStarted(ctx)

		completion, err := r.provider.Complete(ctx, req)
		if err != nil {
			emitter.RunError(ctx, err, true)
			return err
		}

		assistantMsgID := uuid.NewString()
		var textBuilder strings.Builder
		var toolCalls []models.ToolCall

		for chunk := range completion {
			if chunk == nil {
				continue
			}
			if chunk.Error != nil {
				emitter.RunError(ctx, chunk.Error, true)
				return chunk.Error
			}
			if chunk.Done {
				break
			}
			if chunk.Text != "" {
				textBuilder.WriteString(chunk.Text)
				emitter.ModelDelta(ctx, chunk.Text)
			}
			if chunk.ToolCall != nil {
				tc := *chunk.ToolCall
				toolCalls = append(toolCalls, tc)

				// Persist tool call event immediately (best-effort)
				if r.toolEvents != nil {
					_ = r.toolEvents.AddToolCall(ctx, session.ID, assistantMsgID, &tc)
				}
			}
		}

		// Check if context was cancelled during stream processing
		if ctx.Err() != nil {
			return r.handleContextDone(ctx, emitter, wallTimeLimit)
		}

		// Persist assistant message
		assistantMsg := &models.Message{
			ID:        assistantMsgID,
			SessionID: session.ID,
			Role:      models.RoleAssistant,
			Direction: models.DirectionOutbound,
			Content:   textBuilder.String(),
			ToolCalls: toolCalls,
			CreatedAt: time.Now(),
		}
		if err := r.sessions.AppendMessage(ctx, session.ID, assistantMsg); err != nil {
			wrappedErr := fmt.Errorf("failed to persist assistant message: %w", err)
			emitter.RunError(ctx, wrappedErr, false)
			return wrappedErr
		}

		// Add assistant message to request
		req.Messages = append(req.Messages, CompletionMessage{
			Role:      "assistant",
			Content:   assistantMsg.Content,
			ToolCalls: assistantMsg.ToolCalls,
		})

		// No tools requested => done
		if len(toolCalls) == 0 {
			emitter.IterFinished(ctx)
			return nil
		}

		// Policy-filter tools BEFORE executor runs
		results := make([]models.ToolResult, len(toolCalls))
		denied := make([]bool, len(toolCalls))

		allowedCalls := make([]models.ToolCall, 0, len(toolCalls))
		allowedToOriginal := make([]int, 0, len(toolCalls))

		for i := range toolCalls {
			tc := toolCalls[i]

			if resolver != nil && toolPolicy != nil && !resolver.IsAllowed(toolPolicy, tc.Name) {
				denied[i] = true
				res := models.ToolResult{
					ToolCallID: tc.ID,
					Content:    "tool not allowed: " + tc.Name,
					IsError:    true,
				}
				results[i] = res

				// Emit tool finished with error for denied tools
				emitter.ToolFinished(ctx, tc.ID, tc.Name, false, []byte("tool not allowed by policy"), 0)

				// Persist (best-effort)
				if r.toolEvents != nil {
					_ = r.toolEvents.AddToolResult(ctx, session.ID, assistantMsg.ID, &tc, &res)
				}
				continue
			}

			allowedToOriginal = append(allowedToOriginal, i)
			allowedCalls = append(allowedCalls, tc)
		}

		// Execute allowed tools concurrently with events
		execResults := r.executeToolsWithEvents(ctx, toolExec, allowedCalls, emitter)

		// Merge executor results back into original ordering
		for _, er := range execResults {
			if er.Index < 0 || er.Index >= len(allowedToOriginal) {
				continue
			}
			origIdx := allowedToOriginal[er.Index]
			results[origIdx] = er.Result

			// Persist tool result (best-effort)
			if r.toolEvents != nil {
				tc := toolCalls[origIdx]
				res := results[origIdx]
				_ = r.toolEvents.AddToolResult(ctx, session.ID, assistantMsg.ID, &tc, &res)
			}
		}

		// Ensure all ToolCallIDs are set
		for i := range results {
			if results[i].ToolCallID == "" && i < len(toolCalls) {
				results[i].ToolCallID = toolCalls[i].ID
			}
		}

		// Persist tool message
		toolMsg := &models.Message{
			ID:          uuid.NewString(),
			SessionID:   session.ID,
			Role:        models.RoleTool,
			ToolResults: results,
			CreatedAt:   time.Now(),
		}
		if err := r.sessions.AppendMessage(ctx, session.ID, toolMsg); err != nil {
			wrappedErr := fmt.Errorf("failed to persist tool message: %w", err)
			emitter.RunError(ctx, wrappedErr, false)
			return wrappedErr
		}

		// Add tool message to request
		req.Messages = append(req.Messages, CompletionMessage{
			Role:        "tool",
			ToolResults: results,
		})

		emitter.IterFinished(ctx)
	}

	maxIterErr := fmt.Errorf("max iterations (%d) reached", maxIters)
	emitter.RunError(ctx, maxIterErr, false)
	return maxIterErr
}

// handleContextDone emits the appropriate event for context cancellation.
// It distinguishes between explicit cancellation and wall time timeout.
func (r *Runtime) handleContextDone(ctx context.Context, emitter *EventEmitter, wallTimeLimit time.Duration) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}

	// Use background context for emitting terminal events since the request context is cancelled.
	// Terminal events must be delivered regardless of context state.
	bgCtx := context.Background()

	// Check if this was a deadline exceeded (timeout) vs explicit cancellation
	if errors.Is(err, context.DeadlineExceeded) && wallTimeLimit > 0 {
		emitter.RunTimedOut(bgCtx, wallTimeLimit)
		return ErrContextCancelled // Return a consistent error type
	}

	// Explicit cancellation
	emitter.RunCancelled(bgCtx)
	return ErrContextCancelled
}

// executeToolsWithEvents executes tools concurrently and emits tool lifecycle events.
func (r *Runtime) executeToolsWithEvents(ctx context.Context, toolExec *ToolExecutor, calls []models.ToolCall, emitter *EventEmitter) []ToolExecResult {
	if len(calls) == 0 {
		return nil
	}

	// Emit tool.started for each tool
	for _, tc := range calls {
		emitter.ToolStarted(ctx, tc.ID, tc.Name, tc.Input)
	}

	// Execute concurrently
	startTimes := make(map[string]time.Time)
	for _, tc := range calls {
		startTimes[tc.ID] = time.Now()
	}

	results := toolExec.ExecuteConcurrently(ctx, calls, nil) // no legacy event callback

	// Emit tool.finished or tool.timed_out for each result
	for _, er := range results {
		if er.Index < 0 || er.Index >= len(calls) {
			continue
		}
		tc := calls[er.Index]
		elapsed := time.Since(startTimes[tc.ID])

		if er.TimedOut {
			// Emit distinct timeout event for observability
			emitter.ToolTimedOut(ctx, tc.ID, tc.Name, elapsed)
		} else {
			emitter.ToolFinished(ctx, tc.ID, tc.Name, !er.Result.IsError, []byte(er.Result.Content), elapsed)
		}
	}

	return results
}

// ProcessStream processes a user message and returns a channel of AgentEvents.
// This provides a unified event stream for UI rendering, logging, and plugins.
//
// The channel is closed when processing completes or an error occurs.
// Events include run lifecycle, model streaming, tool execution, and statistics.
//
// Example:
//
//	events, err := runtime.ProcessStream(ctx, session, msg)
//	if err != nil {
//	    return err
//	}
//	for event := range events {
//	    switch event.Type {
//	    case models.AgentEventModelDelta:
//	        fmt.Print(event.Stream.Delta)
//	    case models.AgentEventToolStarted:
//	        fmt.Printf("Tool: %s\n", event.Tool.Name)
//	    }
//	}
func (r *Runtime) ProcessStream(ctx context.Context, session *models.Session, msg *models.Message) (<-chan models.AgentEvent, error) {
	// Create backpressure sink with two-lane priority
	bpSink, eventCh := NewBackpressureSink(DefaultBackpressureConfig())

	go func() {
		defer bpSink.Close()

		// Create multi-sink that sends to both the backpressure sink and plugins
		pluginSink := NewPluginSink(r.plugins)
		sink := NewMultiSink(bpSink, pluginSink)

		// Create emitter with the multi-sink
		runID := session.ID + "-" + msg.ID
		emitter := NewEventEmitter(runID, sink)

		// Create stats collector as a plugin to track metrics
		statsCollector := NewStatsCollector(runID)
		statsSink := NewCallbackSink(statsCollector.OnEvent)

		// Wrap emitter to also collect stats
		combinedSink := NewMultiSink(sink, statsSink)
		emitter = NewEventEmitter(runID, combinedSink)

		// Emit run started
		emitter.RunStarted(ctx)

		// Run the core agentic loop
		_ = r.run(ctx, session, msg, emitter)

		// Get accumulated stats and add dropped events count
		stats := statsCollector.Stats()
		stats.DroppedEvents = int(bpSink.DroppedCount())

		// Emit run finished with stats (using background context for terminal event)
		emitter.RunFinished(context.Background(), stats)
	}()

	return eventCh, nil
}

// llmSummaryProvider implements agentctx.SummaryProvider using the runtime's LLM provider.
type llmSummaryProvider struct {
	runtime *Runtime
}

func (p *llmSummaryProvider) Summarize(ctx context.Context, messages []*models.Message, maxLength int) (string, error) {
	prompt := agentctx.BuildSummarizationPrompt(messages, maxLength)

	req := &CompletionRequest{
		Messages: []CompletionMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 1024,
	}

	if p.runtime.defaultModel != "" {
		req.Model = p.runtime.defaultModel
	}
	req.System = "You summarize conversations. Return only the summary text."

	ch, err := p.runtime.provider.Complete(ctx, req)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for chunk := range ch {
		if chunk == nil {
			continue
		}
		if chunk.ToolCall != nil {
			return "", fmt.Errorf("unexpected tool call during summarization: %s", chunk.ToolCall.Name)
		}
		if chunk.Error != nil {
			return "", chunk.Error
		}
		if chunk.Done {
			break
		}
		if chunk.Text != "" {
			b.WriteString(chunk.Text)
		}
	}

	return strings.TrimSpace(b.String()), nil
}

const processBufferSize = 10

// ResponseChunk is a streaming response chunk from the runtime.
type ResponseChunk struct {
	Text       string               `json:"text,omitempty"`
	ToolResult *models.ToolResult   `json:"tool_result,omitempty"`
	ToolEvent  *models.ToolEvent    `json:"tool_event,omitempty"`
	Event      *models.RuntimeEvent `json:"event,omitempty"`
	Error      error                `json:"-"`
}

// ToolRegistry manages available tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// Execute runs a tool by name.
func (r *ToolRegistry) Execute(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return &ToolResult{
			Content: "tool not found: " + name,
			IsError: true,
		}, nil
	}
	return tool.Execute(ctx, params)
}

// AsLLMTools converts registered tools to LLM tool format.
func (r *ToolRegistry) AsLLMTools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

func filterToolsByPolicy(resolver *policy.Resolver, toolPolicy *policy.Policy, tools []Tool) []Tool {
	if resolver == nil || toolPolicy == nil {
		return tools
	}
	filtered := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if resolver.IsAllowed(toolPolicy, tool.Name()) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func (r *Runtime) emitToolEvent(chunks chan<- *ResponseChunk, event *models.ToolEvent) {
	if r.opts.DisableToolEvents || event == nil {
		return
	}
	chunks <- &ResponseChunk{ToolEvent: event}
}

func (r *Runtime) requiresApproval(toolName string, resolver *policy.Resolver) bool {
	if len(r.opts.RequireApproval) == 0 {
		return false
	}
	name := normalizeToolName(toolName, resolver)
	for _, pattern := range r.opts.RequireApproval {
		if matchToolPattern(normalizeToolName(pattern, resolver), name) {
			return true
		}
	}
	return false
}

func (r *Runtime) isAsyncTool(toolName string, resolver *policy.Resolver) bool {
	if len(r.opts.AsyncTools) == 0 {
		return false
	}
	name := normalizeToolName(toolName, resolver)
	for _, pattern := range r.opts.AsyncTools {
		if matchToolPattern(normalizeToolName(pattern, resolver), name) {
			return true
		}
	}
	return false
}

func (r *Runtime) runToolJob(tc models.ToolCall, job *jobs.Job, toolExec *ToolExecutor) {
	if job == nil || r.opts.JobStore == nil {
		return
	}
	ctx := context.Background()
	job.Status = jobs.StatusRunning
	job.StartedAt = time.Now()
	_ = r.opts.JobStore.Update(ctx, job)

	var result models.ToolResult
	var execErr error
	if toolExec != nil {
		execResults := toolExec.ExecuteConcurrently(ctx, []models.ToolCall{tc}, nil)
		if len(execResults) > 0 {
			result = execResults[0].Result
		} else {
			execErr = fmt.Errorf("tool execution failed")
		}
	} else {
		res, err := r.tools.Execute(ctx, tc.Name, tc.Input)
		if err != nil {
			execErr = err
		} else if res != nil {
			result = models.ToolResult{
				ToolCallID: tc.ID,
				Content:    res.Content,
				IsError:    res.IsError,
			}
		}
	}

	if execErr != nil {
		job.Status = jobs.StatusFailed
		job.Error = execErr.Error()
	} else if result.IsError {
		job.Status = jobs.StatusFailed
		job.Error = result.Content
		job.Result = &result
	} else {
		job.Status = jobs.StatusSucceeded
		job.Result = &result
	}
	job.FinishedAt = time.Now()
	_ = r.opts.JobStore.Update(ctx, job)
}

func normalizeToolName(name string, resolver *policy.Resolver) string {
	if resolver == nil {
		return policy.NormalizeTool(name)
	}
	return resolver.CanonicalName(name)
}

func matchToolPattern(pattern, toolName string) bool {
	if pattern == "" || toolName == "" {
		return false
	}
	if pattern == "mcp:*" {
		return strings.HasPrefix(toolName, "mcp:")
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(toolName, prefix)
	}
	return pattern == toolName
}
