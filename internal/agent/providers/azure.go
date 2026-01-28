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

// AzureOpenAIProvider implements the agent.LLMProvider interface for Azure OpenAI Service.
// It provides access to GPT models deployed on Azure with enterprise features like
// private endpoints, managed identity, and regional deployment.
//
// Azure OpenAI uses a different URL structure and authentication than direct OpenAI:
//   - Base URL: https://{resource-name}.openai.azure.com
//   - API Version: Required query parameter (e.g., 2024-02-15-preview)
//   - Deployment: Model name maps to a deployment name in your Azure resource
//
// Thread Safety:
// AzureOpenAIProvider is safe for concurrent use across multiple goroutines.
type AzureOpenAIProvider struct {
	client       *openai.Client
	apiKey       string
	endpoint     string
	apiVersion   string
	defaultModel string
	maxRetries   int
	retryDelay   time.Duration
	base         BaseProvider
}

// AzureOpenAIConfig holds configuration for the Azure OpenAI provider.
type AzureOpenAIConfig struct {
	// Endpoint is the Azure OpenAI resource endpoint (required)
	// Format: https://{resource-name}.openai.azure.com
	Endpoint string

	// APIKey is the Azure OpenAI API key (required)
	APIKey string

	// APIVersion is the API version to use (default: 2024-02-15-preview)
	APIVersion string

	// DefaultModel is the deployment name to use when not specified (optional)
	DefaultModel string

	// MaxRetries is the maximum retry attempts for transient failures (default: 3)
	MaxRetries int

	// RetryDelay is the base delay between retries (default: 1s)
	RetryDelay time.Duration
}

// NewAzureOpenAIProvider creates a new Azure OpenAI provider instance.
//
// Parameters:
//   - cfg: AzureOpenAIConfig with endpoint, API key, and optional settings
//
// Returns:
//   - *AzureOpenAIProvider: Configured provider instance
//   - error: Returns error if required config is missing
//
// Example:
//
//	provider, err := NewAzureOpenAIProvider(AzureOpenAIConfig{
//	    Endpoint:     "https://my-resource.openai.azure.com",
//	    APIKey:       os.Getenv("AZURE_OPENAI_API_KEY"),
//	    DefaultModel: "gpt-4o-deployment",
//	})
func NewAzureOpenAIProvider(cfg AzureOpenAIConfig) (*AzureOpenAIProvider, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("azure: endpoint is required")
	}

	if cfg.APIKey == "" {
		return nil, errors.New("azure: API key is required")
	}

	if cfg.APIVersion == "" {
		cfg.APIVersion = "2024-02-15-preview"
	}

	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}

	// Configure client for Azure
	clientConfig := openai.DefaultAzureConfig(cfg.APIKey, cfg.Endpoint)
	clientConfig.APIVersion = cfg.APIVersion

	return &AzureOpenAIProvider{
		client:       openai.NewClientWithConfig(clientConfig),
		apiKey:       cfg.APIKey,
		endpoint:     cfg.Endpoint,
		apiVersion:   cfg.APIVersion,
		defaultModel: cfg.DefaultModel,
		maxRetries:   cfg.MaxRetries,
		retryDelay:   cfg.RetryDelay,
		base:         NewBaseProvider("azure", cfg.MaxRetries, cfg.RetryDelay),
	}, nil
}

// Name returns the provider identifier.
func (p *AzureOpenAIProvider) Name() string {
	return "azure"
}

// Models returns a placeholder list of models.
// Azure deployments are custom-named, so this returns common deployment patterns.
func (p *AzureOpenAIProvider) Models() []agent.Model {
	return []agent.Model{
		{ID: "gpt-4o", Name: "GPT-4o (Azure)", ContextSize: 128000, SupportsVision: true},
		{ID: "gpt-4-turbo", Name: "GPT-4 Turbo (Azure)", ContextSize: 128000, SupportsVision: true},
		{ID: "gpt-4", Name: "GPT-4 (Azure)", ContextSize: 8192, SupportsVision: false},
		{ID: "gpt-35-turbo", Name: "GPT-3.5 Turbo (Azure)", ContextSize: 16385, SupportsVision: false},
	}
}

// SupportsTools indicates whether this provider supports tool/function calling.
func (p *AzureOpenAIProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to Azure OpenAI and returns a streaming response.
func (p *AzureOpenAIProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if p.client == nil {
		return nil, NewProviderError("azure", req.Model, errors.New("Azure OpenAI client not initialized (set llm.providers.azure.api_key/base_url)"))
	}

	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	if model == "" {
		return nil, NewProviderError("azure", "", errors.New("model/deployment name is required"))
	}

	// Convert messages to OpenAI format
	messages, err := p.convertMessages(req.Messages, req.System)
	if err != nil {
		return nil, fmt.Errorf("azure: failed to convert messages: %w", err)
	}

	// Build request
	chatReq := openai.ChatCompletionRequest{
		Model:    model, // In Azure, this is the deployment name
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
	var lastErr error
	err = p.base.Retry(ctx, p.isRetryableError, func() error {
		stream, lastErr = p.client.CreateChatCompletionStream(ctx, chatReq)
		if lastErr != nil {
			lastErr = p.wrapError(lastErr, model)
			return lastErr
		}
		return nil
	})
	if err != nil {
		if p.isRetryableError(err) {
			return nil, fmt.Errorf("azure: max retries exceeded: %w", err)
		}
		return nil, err
	}

	chunks := make(chan *agent.CompletionChunk)
	go p.processStream(ctx, stream, chunks, model)

	return chunks, nil
}

// processStream processes the streaming response.
func (p *AzureOpenAIProvider) processStream(ctx context.Context, stream *openai.ChatCompletionStream, chunks chan<- *agent.CompletionChunk, model string) {
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
func (p *AzureOpenAIProvider) convertMessages(messages []agent.CompletionMessage, system string) ([]openai.ChatCompletionMessage, error) {
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
func (p *AzureOpenAIProvider) convertTools(tools []agent.Tool) []openai.Tool {
	return toolconv.ToOpenAITools(tools)
}

// isRetryableError determines if an error should trigger a retry.
func (p *AzureOpenAIProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if providerErr, ok := GetProviderError(err); ok {
		return providerErr.Reason.IsRetryable()
	}

	errMsg := err.Error()
	retryable := []string{"rate limit", "429", "500", "502", "503", "504", "timeout", "deadline exceeded", "throttl"}
	for _, s := range retryable {
		if contains(errMsg, s) {
			return true
		}
	}
	return false
}

func (p *AzureOpenAIProvider) wrapError(err error, model string) error {
	if err == nil {
		return nil
	}
	if IsProviderError(err) {
		return err
	}
	return NewProviderError("azure", model, err)
}
