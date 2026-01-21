package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
	openai "github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements the LLMProvider interface for OpenAI's API.
type OpenAIProvider struct {
	client     *openai.Client
	apiKey     string
	maxRetries int
	retryDelay time.Duration
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	if apiKey == "" {
		return &OpenAIProvider{
			apiKey:     "",
			maxRetries: 3,
			retryDelay: time.Second,
		}
	}

	return &OpenAIProvider{
		client:     openai.NewClient(apiKey),
		apiKey:     apiKey,
		maxRetries: 3,
		retryDelay: time.Second,
	}
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Models returns available OpenAI models.
func (p *OpenAIProvider) Models() []agent.Model {
	return []agent.Model{
		{
			ID:             "gpt-4o",
			Name:           "GPT-4o",
			ContextSize:    128000,
			SupportsVision: true,
		},
		{
			ID:             "gpt-4-turbo",
			Name:           "GPT-4 Turbo",
			ContextSize:    128000,
			SupportsVision: true,
		},
		{
			ID:             "gpt-3.5-turbo",
			Name:           "GPT-3.5 Turbo",
			ContextSize:    16385,
			SupportsVision: false,
		},
		{
			ID:             "gpt-4",
			Name:           "GPT-4",
			ContextSize:    8192,
			SupportsVision: false,
		},
	}
}

// SupportsTools returns whether OpenAI supports tool use.
func (p *OpenAIProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request and returns a streaming response.
func (p *OpenAIProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if p.client == nil {
		return nil, errors.New("OpenAI API key not configured")
	}

	// Convert messages to OpenAI format
	messages, err := p.convertToOpenAIMessages(req.Messages, req.System)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Build the request
	chatReq := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   true,
	}

	if req.MaxTokens > 0 {
		chatReq.MaxTokens = req.MaxTokens
	}

	// Add tools if provided
	if len(req.Tools) > 0 {
		chatReq.Tools = p.convertToOpenAITools(req.Tools)
	}

	// Create streaming request with retries
	var stream *openai.ChatCompletionStream
	var lastErr error

	for attempt := 0; attempt < p.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(p.retryDelay * time.Duration(attempt)):
			}
		}

		stream, lastErr = p.client.CreateChatCompletionStream(ctx, chatReq)
		if lastErr == nil {
			break
		}

		// Check if error is retryable
		if !p.isRetryableError(lastErr) {
			return nil, fmt.Errorf("non-retryable error: %w", lastErr)
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
	}

	// Process stream in goroutine
	chunks := make(chan *agent.CompletionChunk)
	go p.processStream(ctx, stream, chunks)

	return chunks, nil
}

// processStream processes the OpenAI stream and converts to internal format.
func (p *OpenAIProvider) processStream(ctx context.Context, stream *openai.ChatCompletionStream, chunks chan<- *agent.CompletionChunk) {
	defer close(chunks)
	defer stream.Close()

	// Track tool calls being built across chunks
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
				// Send any completed tool calls
				for _, tc := range toolCalls {
					if tc.ID != "" && tc.Name != "" {
						chunks <- &agent.CompletionChunk{
							ToolCall: tc,
						}
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

		// Handle text content
		if delta.Content != "" {
			chunks <- &agent.CompletionChunk{
				Text: delta.Content,
			}
		}

		// Handle tool calls
		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				index := 0
				if tc.Index != nil {
					index = *tc.Index
				}

				// Initialize tool call if not exists
				if toolCalls[index] == nil {
					toolCalls[index] = &models.ToolCall{}
				}

				// Update tool call
				if tc.ID != "" {
					toolCalls[index].ID = tc.ID
				}
				if tc.Function.Name != "" {
					toolCalls[index].Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					// Append arguments (they come in chunks)
					var currentArgs string
					if toolCalls[index].Input != nil {
						currentArgs = string(toolCalls[index].Input)
					}
					currentArgs += tc.Function.Arguments
					toolCalls[index].Input = json.RawMessage(currentArgs)
				}
			}
		}

		// Check if we have finish reason indicating tool calls are complete
		if response.Choices[0].FinishReason == "tool_calls" {
			for _, tc := range toolCalls {
				if tc.ID != "" && tc.Name != "" {
					chunks <- &agent.CompletionChunk{
						ToolCall: tc,
					}
				}
			}
			toolCalls = make(map[int]*models.ToolCall) // Reset for potential next iteration
		}
	}
}

// convertToOpenAIMessages converts internal messages to OpenAI format.
func (p *OpenAIProvider) convertToOpenAIMessages(messages []agent.CompletionMessage, system string) ([]openai.ChatCompletionMessage, error) {
	result := make([]openai.ChatCompletionMessage, 0, len(messages)+1)

	// Add system message if provided
	if system != "" {
		result = append(result, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: system,
		})
	}

	// Convert each message
	for _, msg := range messages {
		oaiMsg := openai.ChatCompletionMessage{
			Role: msg.Role,
		}

		// Handle different message types
		switch msg.Role {
		case "user", "system":
			// Check if message has image attachments (vision support)
			if len(msg.Attachments) > 0 {
				hasImages := false
				for _, att := range msg.Attachments {
					if att.Type == "image" {
						hasImages = true
						break
					}
				}

				if hasImages {
					// Use multi-content format for vision
					contentParts := make([]openai.ChatMessagePart, 0)

					// Add text content first if present
					if msg.Content != "" {
						contentParts = append(contentParts, openai.ChatMessagePart{
							Type: openai.ChatMessagePartTypeText,
							Text: msg.Content,
						})
					}

					// Add image attachments
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
			} else {
				oaiMsg.Content = msg.Content
			}

		case "assistant":
			oaiMsg.Content = msg.Content
			// Handle tool calls from assistant
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
			// Handle tool results
			if len(msg.ToolResults) > 0 {
				// OpenAI expects one message per tool result
				for _, tr := range msg.ToolResults {
					result = append(result, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						Content:    tr.Content,
						ToolCallID: tr.ToolCallID,
					})
				}
				continue // Skip the append below
			}
		}

		result = append(result, oaiMsg)
	}

	return result, nil
}

// convertToOpenAITools converts internal tools to OpenAI format.
func (p *OpenAIProvider) convertToOpenAITools(tools []agent.Tool) []openai.Tool {
	result := make([]openai.Tool, len(tools))

	for i, tool := range tools {
		// Parse the schema
		var schemaMap map[string]any
		if err := json.Unmarshal(tool.Schema(), &schemaMap); err != nil {
			// Use empty schema if parsing fails
			schemaMap = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
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

// isRetryableError checks if an error should be retried.
func (p *OpenAIProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific OpenAI API errors that are retryable
	errMsg := err.Error()

	// Rate limit errors
	if contains(errMsg, "rate limit") || contains(errMsg, "429") {
		return true
	}

	// Server errors
	if contains(errMsg, "500") || contains(errMsg, "502") || contains(errMsg, "503") || contains(errMsg, "504") {
		return true
	}

	// Timeout errors
	if contains(errMsg, "timeout") || contains(errMsg, "deadline exceeded") {
		return true
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
		(findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
