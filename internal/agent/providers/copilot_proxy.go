package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/toolconv"
	"github.com/haasonsaas/nexus/pkg/models"
	openai "github.com/sashabaranov/go-openai"
)

// CopilotProxyProvider implements the agent.LLMProvider interface for local Copilot Proxy.
// Copilot Proxy is a VS Code extension that exposes GitHub Copilot models via an
// OpenAI-compatible API endpoint.
//
// This provider enables access to models like GPT-5.2, Claude, and Gemini through
// Copilot subscriptions without requiring direct API keys.
//
// Usage:
//
//	provider, err := NewCopilotProxyProvider(CopilotProxyConfig{
//	    BaseURL: "http://localhost:3000/v1",
//	    Models:  []string{"gpt-5.2", "claude-sonnet-4.5"},
//	})
//
// Thread Safety:
// CopilotProxyProvider is safe for concurrent use.
type CopilotProxyProvider struct {
	client   *openai.Client
	baseURL  string
	models   []string
	modelMap map[string]agent.Model
}

// CopilotProxyConfig holds configuration for the Copilot Proxy provider.
type CopilotProxyConfig struct {
	// BaseURL is the Copilot Proxy endpoint (default: http://localhost:3000/v1)
	BaseURL string

	// Models is the list of model IDs available through the proxy
	Models []string

	// DefaultContextWindow is the default context window size (default: 128000)
	DefaultContextWindow int
}

// DefaultCopilotProxyModels are common model IDs available through Copilot.
var DefaultCopilotProxyModels = []string{
	"gpt-5.2",
	"gpt-5.2-codex",
	"gpt-5.1",
	"gpt-5.1-codex",
	"gpt-5-mini",
	"claude-opus-4.5",
	"claude-sonnet-4.5",
	"claude-haiku-4.5",
	"gemini-3-pro",
	"gemini-3-flash",
}

// NewCopilotProxyProvider creates a new Copilot Proxy provider instance.
func NewCopilotProxyProvider(cfg CopilotProxyConfig) (*CopilotProxyProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:3000/v1"
	}

	models := cfg.Models
	if len(models) == 0 {
		models = DefaultCopilotProxyModels
	}

	contextWindow := cfg.DefaultContextWindow
	if contextWindow <= 0 {
		contextWindow = 128000
	}

	// Build model map
	modelMap := make(map[string]agent.Model)
	for _, id := range models {
		modelMap[id] = agent.Model{
			ID:             id,
			Name:           id + " (Copilot Proxy)",
			ContextSize:    contextWindow,
			SupportsVision: true, // Assume vision support
		}
	}

	// Configure OpenAI client with custom base URL
	clientConfig := openai.DefaultConfig("n/a") // API key not required for local proxy
	clientConfig.BaseURL = baseURL

	return &CopilotProxyProvider{
		client:   openai.NewClientWithConfig(clientConfig),
		baseURL:  baseURL,
		models:   models,
		modelMap: modelMap,
	}, nil
}

// Name returns the provider identifier.
func (p *CopilotProxyProvider) Name() string {
	return "copilot-proxy"
}

// Models returns available models through the proxy.
func (p *CopilotProxyProvider) Models() []agent.Model {
	result := make([]agent.Model, 0, len(p.modelMap))
	for _, m := range p.modelMap {
		result = append(result, m)
	}
	return result
}

// SupportsTools indicates whether this provider supports tool/function calling.
func (p *CopilotProxyProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to the Copilot Proxy.
func (p *CopilotProxyProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if p.client == nil {
		return nil, NewProviderError("copilot-proxy", req.Model, errors.New("client not initialized"))
	}

	model := req.Model
	if model == "" && len(p.models) > 0 {
		model = p.models[0]
	}

	if model == "" {
		return nil, NewProviderError("copilot-proxy", "", errors.New("model is required"))
	}

	messages, err := p.convertMessages(req.Messages, req.System)
	if err != nil {
		return nil, err
	}

	chatReq := openai.ChatCompletionRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	}

	if req.MaxTokens > 0 {
		chatReq.MaxTokens = req.MaxTokens
	}

	if len(req.Tools) > 0 {
		chatReq.Tools = p.convertTools(req.Tools)
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, chatReq)
	if err != nil {
		return nil, NewProviderError("copilot-proxy", model, err)
	}

	chunks := make(chan *agent.CompletionChunk)
	go p.processStream(ctx, stream, chunks, model)

	return chunks, nil
}

// processStream processes the streaming response.
func (p *CopilotProxyProvider) processStream(ctx context.Context, stream *openai.ChatCompletionStream, chunks chan<- *agent.CompletionChunk, model string) {
	defer close(chunks)
	defer stream.Close()

	toolCalls := make(map[int]*models.ToolCall)

	for {
		select {
		case <-ctx.Done():
			chunks <- &agent.CompletionChunk{Error: ctx.Err(), Done: true}
			return
		default:
		}

		response, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				// Emit pending tool calls
				for _, tc := range toolCalls {
					if tc.ID != "" && tc.Name != "" {
						chunks <- &agent.CompletionChunk{ToolCall: tc}
					}
				}
				chunks <- &agent.CompletionChunk{Done: true}
				return
			}
			chunks <- &agent.CompletionChunk{Error: NewProviderError("copilot-proxy", model, err), Done: true}
			return
		}

		if len(response.Choices) == 0 {
			continue
		}

		delta := response.Choices[0].Delta

		if delta.Content != "" {
			chunks <- &agent.CompletionChunk{Text: delta.Content}
		}

		// Handle tool calls
		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				index := 0
				if tc.Index != nil {
					index = *tc.Index
				}

				if toolCalls[index] == nil {
					toolCalls[index] = &models.ToolCall{}
				}

				if tc.ID != "" {
					toolCalls[index].ID = tc.ID
				}
				if tc.Function.Name != "" {
					toolCalls[index].Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					var currentArgs string
					if toolCalls[index].Input != nil {
						currentArgs = string(toolCalls[index].Input)
					}
					currentArgs += tc.Function.Arguments
					toolCalls[index].Input = json.RawMessage(currentArgs)
				}
			}
		}

		if response.Choices[0].FinishReason == "tool_calls" {
			for _, tc := range toolCalls {
				if tc.ID != "" && tc.Name != "" {
					chunks <- &agent.CompletionChunk{ToolCall: tc}
				}
			}
			toolCalls = make(map[int]*models.ToolCall)
		}
	}
}

// convertMessages converts internal messages to OpenAI format.
func (p *CopilotProxyProvider) convertMessages(messages []agent.CompletionMessage, system string) ([]openai.ChatCompletionMessage, error) {
	result := make([]openai.ChatCompletionMessage, 0, len(messages)+1)

	if system != "" {
		result = append(result, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: system,
		})
	}

	for _, msg := range messages {
		oaiMsg := openai.ChatCompletionMessage{Role: msg.Role}

		switch msg.Role {
		case "user", "system":
			hasImages := false
			for _, att := range msg.Attachments {
				if att.Type == "image" {
					hasImages = true
					break
				}
			}

			if hasImages {
				contentParts := make([]openai.ChatMessagePart, 0)
				if msg.Content != "" {
					contentParts = append(contentParts, openai.ChatMessagePart{
						Type: openai.ChatMessagePartTypeText,
						Text: msg.Content,
					})
				}
				for _, att := range msg.Attachments {
					if att.Type == "image" {
						contentParts = append(contentParts, openai.ChatMessagePart{
							Type: openai.ChatMessagePartTypeImageURL,
							ImageURL: &openai.ChatMessageImageURL{
								URL:    att.URL,
								Detail: openai.ImageURLDetailAuto,
							},
						})
					}
				}
				oaiMsg.MultiContent = contentParts
			} else {
				oaiMsg.Content = msg.Content
			}

		case "assistant":
			oaiMsg.Content = msg.Content
			if len(msg.ToolCalls) > 0 {
				oaiMsg.ToolCalls = make([]openai.ToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					oaiMsg.ToolCalls[i] = openai.ToolCall{
						ID:   tc.ID,
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      tc.Name,
							Arguments: string(tc.Input),
						},
					}
				}
			}

		case "tool":
			if len(msg.ToolResults) > 0 {
				for _, tr := range msg.ToolResults {
					result = append(result, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						Content:    tr.Content,
						ToolCallID: tr.ToolCallID,
					})
				}
				continue
			}
		}

		result = append(result, oaiMsg)
	}

	return result, nil
}

// convertTools converts internal tool definitions to OpenAI format.
func (p *CopilotProxyProvider) convertTools(tools []agent.Tool) []openai.Tool {
	return toolconv.ToOpenAITools(tools)
}

// CheckHealth verifies connectivity to the Copilot Proxy.
func (p *CopilotProxyProvider) CheckHealth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := p.client.ListModels(ctx)
	if err != nil {
		return NewProviderError("copilot-proxy", "", err)
	}
	return nil
}
