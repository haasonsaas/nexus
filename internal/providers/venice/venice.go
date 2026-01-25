// Package venice provides a Venice AI API provider for the nexus agent system.
//
// Venice AI is a privacy-focused LLM provider offering both fully private models
// (no logging) and anonymized access to models from other providers via their proxy.
//
// The provider uses an OpenAI-compatible API, making integration straightforward.
// Key differences from direct OpenAI:
//   - Base URL: https://api.venice.ai/api/v1
//   - Privacy modes: "private" (no logging) or "anonymized" (via Venice proxy)
//   - Access to privacy-focused open source models (Llama, DeepSeek, Qwen)
//   - Anonymized access to Claude and GPT models
//
// Thread Safety:
// VeniceProvider is safe for concurrent use across multiple goroutines.
package venice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
	openai "github.com/sashabaranov/go-openai"
)

const (
	// BaseURL is the Venice AI API endpoint
	BaseURL = "https://api.venice.ai/api/v1"

	// DefaultModel is the default model to use when not specified
	DefaultModel = "llama-3.3-70b"
)

// ModelCatalogEntry describes a Venice model's capabilities.
type ModelCatalogEntry struct {
	ID            string   // Model identifier
	Name          string   // Human-readable name
	Reasoning     bool     // Whether the model supports reasoning/thinking
	Input         []string // Supported input types: "text", "image"
	ContextWindow int      // Maximum context window in tokens
	MaxTokens     int      // Maximum output tokens
	Privacy       string   // "private" (no logging) or "anonymized" (via Venice proxy)
}

// VeniceCatalog contains all available Venice models.
// This is used as a fallback when the API discovery fails.
var VeniceCatalog = []ModelCatalogEntry{
	// Private models (fully private, no logging)
	{ID: "llama-3.3-70b", Name: "Llama 3.3 70B", Reasoning: false, Input: []string{"text"}, ContextWindow: 131072, MaxTokens: 8192, Privacy: "private"},
	{ID: "llama-3.2-3b", Name: "Llama 3.2 3B", Reasoning: false, Input: []string{"text"}, ContextWindow: 131072, MaxTokens: 8192, Privacy: "private"},
	{ID: "qwen3-235b-a22b-thinking-2507", Name: "Qwen3 235B Thinking", Reasoning: true, Input: []string{"text"}, ContextWindow: 131072, MaxTokens: 8192, Privacy: "private"},
	{ID: "deepseek-v3.2", Name: "DeepSeek V3.2", Reasoning: true, Input: []string{"text"}, ContextWindow: 163840, MaxTokens: 8192, Privacy: "private"},
	// Anonymized models (via Venice proxy)
	{ID: "claude-opus-45", Name: "Claude Opus 4.5 (via Venice)", Reasoning: true, Input: []string{"text", "image"}, ContextWindow: 202752, MaxTokens: 8192, Privacy: "anonymized"},
	{ID: "openai-gpt-52", Name: "GPT-5.2 (via Venice)", Reasoning: true, Input: []string{"text"}, ContextWindow: 262144, MaxTokens: 8192, Privacy: "anonymized"},
}

// VeniceConfig holds configuration for the Venice provider.
type VeniceConfig struct {
	// APIKey is the Venice API key (required)
	APIKey string

	// DefaultModel is the model to use when not specified in request (optional)
	// Default: llama-3.3-70b
	DefaultModel string

	// BaseURL allows overriding the API endpoint (optional)
	// Default: https://api.venice.ai/api/v1
	BaseURL string

	// MaxRetries is the maximum retry attempts for transient failures (default: 3)
	MaxRetries int

	// RetryDelay is the base delay between retries (default: 1s)
	RetryDelay time.Duration
}

// Client wraps the Venice API with OpenAI-compatible client.
type Client struct {
	apiKey       string
	baseURL      string
	http         *http.Client
	openaiClient *openai.Client
	defaultModel string
	maxRetries   int
	retryDelay   time.Duration
}

// NewClient creates a new Venice API client.
//
// Parameters:
//   - apiKey: Venice API key (required)
//
// Returns:
//   - *Client: Configured client instance
func NewClient(apiKey string) *Client {
	return NewClientWithConfig(VeniceConfig{APIKey: apiKey})
}

// NewClientWithConfig creates a new Venice API client with custom configuration.
func NewClientWithConfig(cfg VeniceConfig) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = BaseURL
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = DefaultModel
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}

	c := &Client{
		apiKey:       cfg.APIKey,
		baseURL:      cfg.BaseURL,
		http:         &http.Client{Timeout: 120 * time.Second},
		defaultModel: cfg.DefaultModel,
		maxRetries:   cfg.MaxRetries,
		retryDelay:   cfg.RetryDelay,
	}

	// Configure OpenAI client with Venice base URL
	if cfg.APIKey != "" {
		clientConfig := openai.DefaultConfig(cfg.APIKey)
		clientConfig.BaseURL = cfg.BaseURL
		c.openaiClient = openai.NewClientWithConfig(clientConfig)
	}

	return c
}

// CompletionRequest represents a completion request to Venice.
type CompletionRequest struct {
	Model     string                    `json:"model"`
	System    string                    `json:"system,omitempty"`
	Messages  []agent.CompletionMessage `json:"messages"`
	Tools     []agent.Tool              `json:"tools,omitempty"`
	MaxTokens int                       `json:"max_tokens,omitempty"`
}

// CompletionResponse represents a completion response from Venice.
type CompletionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content string `json:"content"`
}

// Complete sends a completion request to Venice and returns a streaming response.
func (c *Client) Complete(ctx context.Context, req *CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if c.openaiClient == nil {
		return nil, errors.New("venice: API key not configured")
	}

	model := req.Model
	if model == "" {
		model = c.defaultModel
	}

	// Convert messages to OpenAI format
	messages, err := c.convertMessages(req.Messages, req.System)
	if err != nil {
		return nil, fmt.Errorf("venice: failed to convert messages: %w", err)
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
		chatReq.Tools = c.convertTools(req.Tools)
	}

	// Create stream with retries
	var stream *openai.ChatCompletionStream
	var lastErr error

	for attempt := 0; attempt < c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.retryDelay * time.Duration(attempt)):
			}
		}

		stream, lastErr = c.openaiClient.CreateChatCompletionStream(ctx, chatReq)
		if lastErr == nil {
			break
		}

		if !c.isRetryableError(lastErr) {
			return nil, fmt.Errorf("venice: %w", lastErr)
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("venice: max retries exceeded: %w", lastErr)
	}

	chunks := make(chan *agent.CompletionChunk)
	go c.processStream(ctx, stream, chunks)

	return chunks, nil
}

// processStream processes the streaming response.
func (c *Client) processStream(ctx context.Context, stream *openai.ChatCompletionStream, chunks chan<- *agent.CompletionChunk) {
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
			chunks <- &agent.CompletionChunk{Error: err, Done: true}
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
func (c *Client) convertMessages(messages []agent.CompletionMessage, system string) ([]openai.ChatCompletionMessage, error) {
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
func (c *Client) convertTools(tools []agent.Tool) []openai.Tool {
	result := make([]openai.Tool, len(tools))

	for i, tool := range tools {
		var schemaMap map[string]any
		if err := json.Unmarshal(tool.Schema(), &schemaMap); err != nil {
			schemaMap = map[string]any{"type": "object", "properties": map[string]any{}}
		}

		result[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  schemaMap,
			},
		}
	}

	return result
}

// isRetryableError determines if an error should trigger a retry.
func (c *Client) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	retryable := []string{"rate limit", "429", "500", "502", "503", "504", "timeout", "deadline exceeded"}
	for _, s := range retryable {
		if strings.Contains(errMsg, s) {
			return true
		}
	}
	return false
}

// DiscoverModels fetches models from Venice API with fallback to static catalog.
func DiscoverModels(ctx context.Context, apiKey string) ([]ModelCatalogEntry, error) {
	if apiKey == "" {
		return VeniceCatalog, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", BaseURL+"/models", nil)
	if err != nil {
		return VeniceCatalog, nil
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return VeniceCatalog, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return VeniceCatalog, nil
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return VeniceCatalog, nil
	}

	// If we got models from API, merge with our catalog for metadata
	if len(result.Data) > 0 {
		catalogMap := make(map[string]ModelCatalogEntry)
		for _, entry := range VeniceCatalog {
			catalogMap[entry.ID] = entry
		}

		models := make([]ModelCatalogEntry, 0, len(result.Data))
		for _, m := range result.Data {
			if entry, ok := catalogMap[m.ID]; ok {
				models = append(models, entry)
			} else {
				// Unknown model from API, add with defaults
				models = append(models, ModelCatalogEntry{
					ID:            m.ID,
					Name:          m.ID,
					Reasoning:     false,
					Input:         []string{"text"},
					ContextWindow: 32000,
					MaxTokens:     4096,
					Privacy:       "private",
				})
			}
		}
		return models, nil
	}

	return VeniceCatalog, nil
}

// VeniceProvider implements the agent.LLMProvider interface for Venice AI.
type VeniceProvider struct {
	client       *Client
	defaultModel string
}

// NewVeniceProvider creates a new Venice provider instance.
//
// Parameters:
//   - cfg: VeniceConfig with API key and optional settings
//
// Returns:
//   - *VeniceProvider: Configured provider instance
//   - error: Returns error if API key is empty
func NewVeniceProvider(cfg VeniceConfig) (*VeniceProvider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("venice: API key is required")
	}

	client := NewClientWithConfig(cfg)

	defaultModel := cfg.DefaultModel
	if defaultModel == "" {
		defaultModel = DefaultModel
	}

	return &VeniceProvider{
		client:       client,
		defaultModel: defaultModel,
	}, nil
}

// Name returns the provider identifier.
func (p *VeniceProvider) Name() string {
	return "venice"
}

// Models returns the list of available Venice models with their capabilities.
func (p *VeniceProvider) Models() []agent.Model {
	models := make([]agent.Model, len(VeniceCatalog))
	for i, entry := range VeniceCatalog {
		supportsVision := false
		for _, input := range entry.Input {
			if input == "image" {
				supportsVision = true
				break
			}
		}
		models[i] = agent.Model{
			ID:             entry.ID,
			Name:           entry.Name,
			ContextSize:    entry.ContextWindow,
			SupportsVision: supportsVision,
		}
	}
	return models
}

// SupportsTools indicates whether this provider supports tool/function calling.
// Venice supports tools via its OpenAI-compatible API.
func (p *VeniceProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to Venice and returns a streaming response.
func (p *VeniceProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	veniceReq := &CompletionRequest{
		Model:     model,
		System:    req.System,
		Messages:  req.Messages,
		Tools:     req.Tools,
		MaxTokens: req.MaxTokens,
	}

	return p.client.Complete(ctx, veniceReq)
}

// GetModelInfo returns detailed information about a specific model.
func GetModelInfo(modelID string) *ModelCatalogEntry {
	for _, entry := range VeniceCatalog {
		if entry.ID == modelID {
			return &entry
		}
	}
	return nil
}

// IsPrivateModel returns true if the model is fully private (no logging).
func IsPrivateModel(modelID string) bool {
	info := GetModelInfo(modelID)
	if info == nil {
		return false
	}
	return info.Privacy == "private"
}

// SupportsReasoning returns true if the model supports extended thinking/reasoning.
func SupportsReasoning(modelID string) bool {
	info := GetModelInfo(modelID)
	if info == nil {
		return false
	}
	return info.Reasoning
}
