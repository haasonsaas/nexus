package agent

import (
	"context"
	"encoding/json"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// LLMProvider is the interface for LLM backends.
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

// CompletionRequest is the input to an LLM completion.
type CompletionRequest struct {
	Model     string              `json:"model"`
	System    string              `json:"system,omitempty"`
	Messages  []CompletionMessage `json:"messages"`
	Tools     []Tool              `json:"tools,omitempty"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
}

// CompletionMessage is a message in the conversation.
type CompletionMessage struct {
	Role        string              `json:"role"`
	Content     string              `json:"content,omitempty"`
	ToolCalls   []models.ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []models.ToolResult `json:"tool_results,omitempty"`
}

// CompletionChunk is a streaming response chunk.
type CompletionChunk struct {
	Text     string           `json:"text,omitempty"`
	ToolCall *models.ToolCall `json:"tool_call,omitempty"`
	Done     bool             `json:"done,omitempty"`
	Error    error            `json:"-"`
}

// Model represents an available LLM model.
type Model struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContextSize  int    `json:"context_size"`
	SupportsVision bool `json:"supports_vision"`
}

// Tool is the interface for agent tools.
type Tool interface {
	// Name returns the tool name for LLM function calling.
	Name() string

	// Description returns the tool description.
	Description() string

	// Schema returns the JSON schema for parameters.
	Schema() json.RawMessage

	// Execute runs the tool with given parameters.
	Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}

// ToolResult is the output of a tool execution.
type ToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Runtime orchestrates agent conversations.
type Runtime struct {
	provider LLMProvider
	tools    *ToolRegistry
	sessions sessions.Store
}

// NewRuntime creates a new agent runtime.
func NewRuntime(provider LLMProvider, sessions sessions.Store) *Runtime {
	return &Runtime{
		provider: provider,
		tools:    NewToolRegistry(),
		sessions: sessions,
	}
}

// RegisterTool adds a tool to the runtime.
func (r *Runtime) RegisterTool(tool Tool) {
	r.tools.Register(tool)
}

// Process handles an incoming message and streams the response.
func (r *Runtime) Process(ctx context.Context, session *models.Session, msg *models.Message) (<-chan *ResponseChunk, error) {
	chunks := make(chan *ResponseChunk)

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

// ResponseChunk is a streaming response chunk from the runtime.
type ResponseChunk struct {
	Text       string             `json:"text,omitempty"`
	ToolResult *models.ToolResult `json:"tool_result,omitempty"`
	Error      error              `json:"-"`
}

// ToolRegistry manages available tools.
type ToolRegistry struct {
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
	r.tools[tool.Name()] = tool
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// Execute runs a tool by name.
func (r *ToolRegistry) Execute(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	tool, ok := r.tools[name]
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
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}
