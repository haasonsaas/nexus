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
	"sync"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

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

	// defaultModel is used when requests omit a model
	defaultModel string
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

		// 1. Get conversation history
		history, err := r.sessions.GetHistory(ctx, session.ID, 50)
		if err != nil {
			chunks <- &ResponseChunk{Error: err}
			return
		}

		// 2. Build completion request
		messages := make([]CompletionMessage, 0, len(history)+1)
		for _, m := range history {
			messages = append(messages, CompletionMessage{
				Role:    string(m.Role),
				Content: m.Content,
			})
		}
		messages = append(messages, CompletionMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})

		req := &CompletionRequest{
			Messages:  messages,
			Tools:     r.tools.AsLLMTools(),
			MaxTokens: 4096,
		}
		if req.Model == "" && r.defaultModel != "" {
			req.Model = r.defaultModel
		}

		// 3. Call LLM
		completion, err := r.provider.Complete(ctx, req)
		if err != nil {
			chunks <- &ResponseChunk{Error: err}
			return
		}

		// 4. Process stream, executing tools as needed
		for chunk := range completion {
			if chunk.Error != nil {
				chunks <- &ResponseChunk{Error: chunk.Error}
				return
			}

			if chunk.ToolCall != nil {
				// Execute tool
				result, err := r.tools.Execute(ctx, chunk.ToolCall.Name, chunk.ToolCall.Input)
				if err != nil {
					chunks <- &ResponseChunk{
						ToolResult: &models.ToolResult{
							ToolCallID: chunk.ToolCall.ID,
							Content:    err.Error(),
							IsError:    true,
						},
					}
				} else {
					chunks <- &ResponseChunk{
						ToolResult: &models.ToolResult{
							ToolCallID: chunk.ToolCall.ID,
							Content:    result.Content,
							IsError:    result.IsError,
						},
					}
				}
				// Continue conversation with tool result...
			} else if chunk.Text != "" {
				chunks <- &ResponseChunk{Text: chunk.Text}
			}
		}
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
