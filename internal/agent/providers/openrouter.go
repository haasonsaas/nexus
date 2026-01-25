package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/toolconv"
	"github.com/haasonsaas/nexus/pkg/models"
	openai "github.com/sashabaranov/go-openai"
)

// OpenRouterProvider implements the agent.LLMProvider interface for OpenRouter's API.
// OpenRouter provides a unified interface to multiple LLM providers including
// OpenAI, Anthropic, Google, and many open-source models.
//
// OpenRouter uses an OpenAI-compatible API, making integration straightforward.
// Key differences from direct OpenAI:
//   - Base URL: https://openrouter.ai/api/v1
//   - Additional headers for app identification (X-Title, HTTP-Referer)
//   - Access to models from multiple providers through unified interface
//   - Model IDs use format: provider/model-name (e.g., "anthropic/claude-3-opus")
//
// Thread Safety:
// OpenRouterProvider is safe for concurrent use across multiple goroutines.
type OpenRouterProvider struct {
	client       *openai.Client
	apiKey       string
	defaultModel string
	base         BaseProvider
}

// OpenRouterConfig holds configuration for the OpenRouter provider.
type OpenRouterConfig struct {
	// APIKey is the OpenRouter API key (required)
	APIKey string

	// DefaultModel is the model to use when not specified in request (optional)
	// Examples: "openai/gpt-4o", "anthropic/claude-3-opus", "google/gemini-pro"
	DefaultModel string

	// AppName is your app's name shown in OpenRouter dashboard (optional)
	AppName string

	// SiteURL is your site URL for tracking (optional)
	SiteURL string

	// MaxRetries is the maximum retry attempts for transient failures (default: 3)
	MaxRetries int

	// RetryDelay is the base delay between retries (default: 1s)
	RetryDelay time.Duration
}

// NewOpenRouterProvider creates a new OpenRouter provider instance.
//
// Parameters:
//   - cfg: OpenRouterConfig with API key and optional settings
//
// Returns:
//   - *OpenRouterProvider: Configured provider instance
//   - error: Returns error if API key is empty
//
// Example:
//
//	provider, err := NewOpenRouterProvider(OpenRouterConfig{
//	    APIKey:       os.Getenv("OPENROUTER_API_KEY"),
//	    DefaultModel: "openai/gpt-4o",
//	    AppName:      "My App",
//	})
func NewOpenRouterProvider(cfg OpenRouterConfig) (*OpenRouterProvider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("openrouter: API key is required")
	}

	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}

	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "openai/gpt-4o"
	}

	// Configure OpenAI client with OpenRouter base URL
	clientConfig := openai.DefaultConfig(cfg.APIKey)
	clientConfig.BaseURL = "https://openrouter.ai/api/v1"

	return &OpenRouterProvider{
		client:       openai.NewClientWithConfig(clientConfig),
		apiKey:       cfg.APIKey,
		defaultModel: cfg.DefaultModel,
		base:         NewBaseProvider("openrouter", cfg.MaxRetries, cfg.RetryDelay),
	}, nil
}

// Name returns the provider identifier.
func (p *OpenRouterProvider) Name() string {
	return "openrouter"
}

// Models returns a curated list of popular models available via OpenRouter.
// OpenRouter supports 200+ models; this returns commonly used ones.
func (p *OpenRouterProvider) Models() []agent.Model {
	return []agent.Model{
		// OpenAI models
		{ID: "openai/gpt-4o", Name: "GPT-4o", ContextSize: 128000, SupportsVision: true},
		{ID: "openai/gpt-4-turbo", Name: "GPT-4 Turbo", ContextSize: 128000, SupportsVision: true},
		{ID: "openai/gpt-3.5-turbo", Name: "GPT-3.5 Turbo", ContextSize: 16385, SupportsVision: false},
		// Anthropic models
		{ID: "anthropic/claude-3-opus", Name: "Claude 3 Opus", ContextSize: 200000, SupportsVision: true},
		{ID: "anthropic/claude-3-sonnet", Name: "Claude 3 Sonnet", ContextSize: 200000, SupportsVision: true},
		{ID: "anthropic/claude-3-haiku", Name: "Claude 3 Haiku", ContextSize: 200000, SupportsVision: true},
		// Google models
		{ID: "google/gemini-pro", Name: "Gemini Pro", ContextSize: 32000, SupportsVision: false},
		{ID: "google/gemini-pro-vision", Name: "Gemini Pro Vision", ContextSize: 32000, SupportsVision: true},
		// Open source models
		{ID: "meta-llama/llama-3-70b-instruct", Name: "Llama 3 70B", ContextSize: 8192, SupportsVision: false},
		{ID: "mistralai/mixtral-8x7b-instruct", Name: "Mixtral 8x7B", ContextSize: 32768, SupportsVision: false},
		{ID: "nousresearch/nous-hermes-2-mixtral-8x7b-dpo", Name: "Nous Hermes 2 Mixtral", ContextSize: 32768, SupportsVision: false},
	}
}

// SupportsTools indicates whether this provider supports tool/function calling.
// OpenRouter passes through tool support from underlying providers.
func (p *OpenRouterProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to OpenRouter and returns a streaming response.
func (p *OpenRouterProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if p.client == nil {
		return nil, NewProviderError("openrouter", req.Model, errors.New("OpenRouter client not initialized"))
	}

	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	// Convert messages to OpenAI format
	messages, err := p.convertMessages(req.Messages, req.System)
	if err != nil {
		return nil, fmt.Errorf("openrouter: failed to convert messages: %w", err)
	}

	// Build request
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

	// Create stream with retries
	var stream *openai.ChatCompletionStream
	lastErr := p.base.Retry(ctx, p.isRetryableError, func() error {
		var err error
		stream, err = p.client.CreateChatCompletionStream(ctx, chatReq)
		return err
	})

	if lastErr != nil {
		wrapped := p.wrapError(lastErr, model)
		if p.isRetryableError(lastErr) {
			return nil, fmt.Errorf("openrouter: max retries exceeded: %w", wrapped)
		}
		return nil, wrapped
	}

	chunks := make(chan *agent.CompletionChunk)
	go p.processStream(ctx, stream, chunks, model)

	return chunks, nil
}

// processStream processes the streaming response.
func (p *OpenRouterProvider) processStream(ctx context.Context, stream *openai.ChatCompletionStream, chunks chan<- *agent.CompletionChunk, model string) {
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
			chunks <- &agent.CompletionChunk{Error: p.wrapError(err, model), Done: true}
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
func (p *OpenRouterProvider) convertMessages(messages []agent.CompletionMessage, system string) ([]openai.ChatCompletionMessage, error) {
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
			// Handle vision attachments
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
func (p *OpenRouterProvider) convertTools(tools []agent.Tool) []openai.Tool {
	return toolconv.ToOpenAITools(tools)
}

// isRetryableError determines if an error should trigger a retry.
func (p *OpenRouterProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if providerErr, ok := GetProviderError(err); ok {
		return providerErr.Reason.IsRetryable()
	}

	errMsg := err.Error()
	retryable := []string{"rate limit", "429", "500", "502", "503", "504", "timeout", "deadline exceeded"}
	for _, s := range retryable {
		if contains(errMsg, s) {
			return true
		}
	}
	return false
}

func (p *OpenRouterProvider) wrapError(err error, model string) error {
	if err == nil {
		return nil
	}
	if IsProviderError(err) {
		return err
	}
	return NewProviderError("openrouter", model, err)
}
