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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
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

	// defaultModel is used when requests omit a model
	defaultModel string

	// defaultSystem is used when requests omit a system prompt
	defaultSystem string

	// maxIterations limits the agentic loop iterations (default 5)
	maxIterations int
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
	return &Runtime{
		provider: provider,
		tools:    NewToolRegistry(),
		sessions: sessions,
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

		// 1. Get conversation history and build initial messages
		history, err := r.sessions.GetHistory(ctx, session.ID, 50)
		if err != nil {
			chunks <- &ResponseChunk{Error: err}
			return
		}

		messages, err := r.buildCompletionMessages(history)
		if err != nil {
			chunks <- &ResponseChunk{Error: err}
			return
		}

		// Add the new user message
		messages = append(messages, CompletionMessage{
			Role:        string(msg.Role),
			Content:     msg.Content,
			Attachments: msg.Attachments,
		})

		// 2. Get tools, filtered by policy if set
		tools := r.tools.AsLLMTools()
		var resolver *policy.Resolver
		var toolPolicy *policy.Policy
		if r, p, ok := toolPolicyFromContext(ctx); ok {
			resolver, toolPolicy = r, p
			tools = filterToolsByPolicy(resolver, toolPolicy, tools)
		}

		// 3. Build base completion request
		req := &CompletionRequest{
			Messages:  messages,
			Tools:     tools,
			MaxTokens: 4096,
		}
		if r.defaultModel != "" {
			req.Model = r.defaultModel
		}
		if system, ok := systemPromptFromContext(ctx); ok {
			req.System = system
		} else if r.defaultSystem != "" {
			req.System = r.defaultSystem
		}

		// 4. Agentic loop - iterate until no tool calls or max iterations
		maxIters := r.maxIterations
		if maxIters <= 0 {
			maxIters = 5
		}

		for iter := 0; iter < maxIters; iter++ {
			// Check for cancellation
			select {
			case <-ctx.Done():
				chunks <- &ResponseChunk{Error: ctx.Err()}
				return
			default:
			}

			// Call LLM
			completion, err := r.provider.Complete(ctx, req)
			if err != nil {
				chunks <- &ResponseChunk{Error: err}
				return
			}

			// Collect response: stream text, accumulate tool calls
			var textBuilder strings.Builder
			var toolCalls []models.ToolCall

			for chunk := range completion {
				if chunk.Error != nil {
					chunks <- &ResponseChunk{Error: chunk.Error}
					return
				}
				if chunk.Text != "" {
					textBuilder.WriteString(chunk.Text)
					chunks <- &ResponseChunk{Text: chunk.Text}
				}
				if chunk.ToolCall != nil {
					toolCalls = append(toolCalls, *chunk.ToolCall)
				}
			}

			// Build assistant message from this turn
			assistantMsg := &models.Message{
				ID:        uuid.NewString(),
				SessionID: session.ID,
				Role:      models.RoleAssistant,
				Content:   textBuilder.String(),
				ToolCalls: toolCalls,
				CreatedAt: time.Now(),
			}

			// Persist assistant message
			if err := r.sessions.AppendMessage(ctx, session.ID, assistantMsg); err != nil {
				chunks <- &ResponseChunk{Error: fmt.Errorf("failed to persist assistant message: %w", err)}
				return
			}

			// Add to conversation for next iteration
			messages = append(messages, CompletionMessage{
				Role:      "assistant",
				Content:   assistantMsg.Content,
				ToolCalls: assistantMsg.ToolCalls,
			})

			// No tool calls = done
			if len(toolCalls) == 0 {
				return
			}

			// Execute tools and collect results
			var toolResults []models.ToolResult
			for _, tc := range toolCalls {
				// Check policy at execution time
				if resolver != nil && toolPolicy != nil {
					if !resolver.IsAllowed(toolPolicy, tc.Name) {
						result := models.ToolResult{
							ToolCallID: tc.ID,
							Content:    "tool not allowed: " + tc.Name,
							IsError:    true,
						}
						toolResults = append(toolResults, result)
						chunks <- &ResponseChunk{ToolResult: &result}
						continue
					}
				}

				// Persist tool call event
				if r.toolEvents != nil {
					if err := r.toolEvents.AddToolCall(ctx, session.ID, assistantMsg.ID, &tc); err != nil {
						// Log but don't fail - tool event persistence is best-effort
					}
				}

				// Execute the tool
				execResult, err := r.tools.Execute(ctx, tc.Name, tc.Input)
				var result models.ToolResult
				if err != nil {
					result = models.ToolResult{
						ToolCallID: tc.ID,
						Content:    err.Error(),
						IsError:    true,
					}
				} else {
					result = models.ToolResult{
						ToolCallID: tc.ID,
						Content:    execResult.Content,
						IsError:    execResult.IsError,
					}
				}

				toolResults = append(toolResults, result)
				chunks <- &ResponseChunk{ToolResult: &result}

				// Persist tool result event
				if r.toolEvents != nil {
					if err := r.toolEvents.AddToolResult(ctx, session.ID, assistantMsg.ID, &tc, &result); err != nil {
						// Log but don't fail
					}
				}
			}

			// Build tool message with results
			toolMsg := &models.Message{
				ID:          uuid.NewString(),
				SessionID:   session.ID,
				Role:        models.RoleTool,
				ToolResults: toolResults,
				CreatedAt:   time.Now(),
			}

			// Persist tool message
			if err := r.sessions.AppendMessage(ctx, session.ID, toolMsg); err != nil {
				chunks <- &ResponseChunk{Error: fmt.Errorf("failed to persist tool message: %w", err)}
				return
			}

			// Add to conversation for next iteration
			messages = append(messages, CompletionMessage{
				Role:        "tool",
				ToolResults: toolResults,
			})

			// Update request for next iteration
			req.Messages = messages
		}

		// Hit max iterations
		chunks <- &ResponseChunk{Error: fmt.Errorf("max iterations (%d) reached", maxIters)}
	}()

	return chunks, nil
}

const processBufferSize = 10

// ResponseChunk is a streaming response chunk from the runtime.
type ResponseChunk struct {
	Text       string             `json:"text,omitempty"`
	ToolResult *models.ToolResult `json:"tool_result,omitempty"`
	Error      error              `json:"-"`
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
