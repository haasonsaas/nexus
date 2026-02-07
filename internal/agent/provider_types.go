package agent

import (
	"context"
	"encoding/json"

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

	// EnableThinking enables extended thinking mode for supported models (e.g., Claude).
	// When enabled, the model uses additional compute for complex reasoning tasks.
	EnableThinking bool `json:"enable_thinking,omitempty"`

	// ThinkingBudgetTokens sets the token budget for extended thinking.
	// Only used when EnableThinking is true. If 0, a default budget is used.
	// Typical range: 1024-100000 tokens depending on task complexity.
	ThinkingBudgetTokens int `json:"thinking_budget_tokens,omitempty"`
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

	// Thinking contains reasoning/thinking text when extended thinking is enabled.
	// This is streamed separately from the main response text.
	Thinking string `json:"thinking,omitempty"`

	// ThinkingStart signals the beginning of a thinking block.
	ThinkingStart bool `json:"thinking_start,omitempty"`

	// ThinkingEnd signals the end of a thinking block.
	ThinkingEnd bool `json:"thinking_end,omitempty"`

	// InputTokens contains the number of input tokens consumed by this request.
	// Only populated in the final chunk (when Done is true).
	InputTokens int `json:"input_tokens,omitempty"`

	// OutputTokens contains the number of output tokens generated by this response.
	// Only populated in the final chunk (when Done is true).
	OutputTokens int `json:"output_tokens,omitempty"`
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

	// Artifacts contains any files/media produced by the tool.
	// These are converted to message attachments when sent to channels.
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// Artifact represents a file or media produced by a tool execution.
type Artifact struct {
	// ID is the unique identifier for the artifact.
	ID string `json:"id"`

	// Type describes the artifact type (screenshot, recording, file).
	Type string `json:"type"`

	// MimeType is the MIME type of the artifact data.
	MimeType string `json:"mime_type"`

	// Filename is the suggested filename for the artifact.
	Filename string `json:"filename,omitempty"`

	// Data contains the raw artifact bytes.
	Data []byte `json:"data,omitempty"`

	// URL is an optional URL where the artifact can be accessed.
	URL string `json:"url,omitempty"`
}

// ToolEventStore persists tool calls and results for audit, replay, and analytics.
// This is optional - if nil, tool events are not persisted separately from messages.
type ToolEventStore interface {
	// AddToolCall persists a tool call event.
	AddToolCall(ctx context.Context, sessionID, messageID string, call *models.ToolCall) error

	// AddToolResult persists a tool result event.
	AddToolResult(ctx context.Context, sessionID, messageID string, call *models.ToolCall, result *models.ToolResult) error
}

// ResponseChunk represents a streaming response chunk from the runtime.
// Each chunk may contain text, tool results, tool events, runtime events, or errors.
// Consumers should check each field and handle accordingly.
type ResponseChunk struct {
	Text          string               `json:"text,omitempty"`
	Thinking      string               `json:"thinking,omitempty"`
	ThinkingStart bool                 `json:"thinking_start,omitempty"`
	ThinkingEnd   bool                 `json:"thinking_end,omitempty"`
	ToolResult    *models.ToolResult   `json:"tool_result,omitempty"`
	ToolEvent     *models.ToolEvent    `json:"tool_event,omitempty"`
	Event         *models.RuntimeEvent `json:"event,omitempty"`
	Error         error                `json:"-"`
	// Artifacts contains any files/media produced by tool executions.
	// These should be converted to message attachments when sending to channels.
	Artifacts []Artifact `json:"artifacts,omitempty"`
}
