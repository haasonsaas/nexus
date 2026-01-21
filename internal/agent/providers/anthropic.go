package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// AnthropicProvider implements the agent.LLMProvider interface for Anthropic's Claude API.
type AnthropicProvider struct {
	client      *anthropic.Client
	apiKey      string
	maxRetries  int
	retryDelay  time.Duration
	defaultModel string
}

// AnthropicConfig holds configuration for the Anthropic provider.
type AnthropicConfig struct {
	APIKey       string
	MaxRetries   int
	RetryDelay   time.Duration
	DefaultModel string
}

// NewAnthropicProvider creates a new Anthropic provider instance.
func NewAnthropicProvider(config AnthropicConfig) (*AnthropicProvider, error) {
	if config.APIKey == "" {
		return nil, errors.New("anthropic: API key is required")
	}

	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	if config.RetryDelay <= 0 {
		config.RetryDelay = time.Second
	}

	if config.DefaultModel == "" {
		config.DefaultModel = "claude-sonnet-4-20250514"
	}

	client := anthropic.NewClient(
		option.WithAPIKey(config.APIKey),
	)

	return &AnthropicProvider{
		client:       client,
		apiKey:       config.APIKey,
		maxRetries:   config.MaxRetries,
		retryDelay:   config.RetryDelay,
		defaultModel: config.DefaultModel,
	}, nil
}

// Name returns the provider name.
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// Models returns the list of available Claude models.
func (p *AnthropicProvider) Models() []agent.Model {
	return []agent.Model{
		{
			ID:             "claude-sonnet-4-20250514",
			Name:           "Claude Sonnet 4",
			ContextSize:    200000,
			SupportsVision: true,
		},
		{
			ID:             "claude-opus-4-20250514",
			Name:           "Claude Opus 4",
			ContextSize:    200000,
			SupportsVision: true,
		},
		{
			ID:             "claude-3-5-sonnet-20241022",
			Name:           "Claude 3.5 Sonnet",
			ContextSize:    200000,
			SupportsVision: true,
		},
		{
			ID:             "claude-3-opus-20240229",
			Name:           "Claude 3 Opus",
			ContextSize:    200000,
			SupportsVision: true,
		},
		{
			ID:             "claude-3-sonnet-20240229",
			Name:           "Claude 3 Sonnet",
			ContextSize:    200000,
			SupportsVision: true,
		},
		{
			ID:             "claude-3-haiku-20240307",
			Name:           "Claude 3 Haiku",
			ContextSize:    200000,
			SupportsVision: true,
		},
	}
}

// SupportsTools returns true as Anthropic supports tool use (function calling).
func (p *AnthropicProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request and returns a streaming response.
func (p *AnthropicProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	chunks := make(chan *agent.CompletionChunk)

	go func() {
		defer close(chunks)

		// Convert request to Anthropic format with retries
		var stream *anthropic.MessageStreamEvent
		var err error

		for attempt := 0; attempt <= p.maxRetries; attempt++ {
			stream, err = p.createStream(ctx, req)
			if err == nil {
				break
			}

			// Check if error is retryable
			if !p.isRetryableError(err) {
				chunks <- &agent.CompletionChunk{Error: err}
				return
			}

			// Exponential backoff
			if attempt < p.maxRetries {
				backoff := p.retryDelay * time.Duration(math.Pow(2, float64(attempt)))
				select {
				case <-ctx.Done():
					chunks <- &agent.CompletionChunk{Error: ctx.Err()}
					return
				case <-time.After(backoff):
					continue
				}
			}
		}

		if err != nil {
			chunks <- &agent.CompletionChunk{Error: fmt.Errorf("anthropic: max retries exceeded: %w", err)}
			return
		}

		// Process streaming events
		p.processStream(stream, chunks)
	}()

	return chunks, nil
}

// createStream creates an Anthropic streaming request.
func (p *AnthropicProvider) createStream(ctx context.Context, req *agent.CompletionRequest) (*anthropic.MessageStreamEvent, error) {
	// Convert messages
	messages, err := p.convertMessages(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to convert messages: %w", err)
	}

	// Build parameters
	params := anthropic.MessageNewParams{
		Model:     anthropic.F(p.getModel(req.Model)),
		Messages:  anthropic.F(messages),
		MaxTokens: anthropic.F(int64(p.getMaxTokens(req.MaxTokens))),
	}

	// Add system prompt if provided
	if req.System != "" {
		params.System = anthropic.F([]anthropic.TextBlockParam{
			anthropic.NewTextBlock(req.System),
		})
	}

	// Add tools if provided
	if len(req.Tools) > 0 {
		tools, err := p.convertTools(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("anthropic: failed to convert tools: %w", err)
		}
		params.Tools = anthropic.F(tools)
	}

	// Create streaming request
	stream := p.client.Messages.NewStreaming(ctx, params)

	return &stream, nil
}

// processStream processes the streaming events and sends chunks.
func (p *AnthropicProvider) processStream(stream *anthropic.MessageStreamEvent, chunks chan<- *agent.CompletionChunk) {
	var currentToolCall *models.ToolCall
	var currentToolInput strings.Builder

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case anthropic.MessageStreamEventTypeContentBlockStart:
			if data, ok := event.ContentBlockStart(); ok {
				if toolUse, ok := data.ContentBlock.AsUnion().(anthropic.ToolUseBlock); ok {
					currentToolCall = &models.ToolCall{
						ID:   toolUse.ID,
						Name: toolUse.Name,
					}
					currentToolInput.Reset()
				}
			}

		case anthropic.MessageStreamEventTypeContentBlockDelta:
			if data, ok := event.ContentBlockDelta(); ok {
				delta := data.Delta

				// Handle text delta
				if textDelta, ok := delta.AsUnion().(anthropic.TextDelta); ok {
					chunks <- &agent.CompletionChunk{
						Text: textDelta.Text,
					}
				}

				// Handle tool input delta
				if inputDelta, ok := delta.AsUnion().(anthropic.InputJSONDelta); ok {
					currentToolInput.WriteString(inputDelta.PartialJSON)
				}
			}

		case anthropic.MessageStreamEventTypeContentBlockStop:
			// Finalize tool call if we were building one
			if currentToolCall != nil {
				currentToolCall.Input = json.RawMessage(currentToolInput.String())
				chunks <- &agent.CompletionChunk{
					ToolCall: currentToolCall,
				}
				currentToolCall = nil
			}

		case anthropic.MessageStreamEventTypeMessageStop:
			chunks <- &agent.CompletionChunk{
				Done: true,
			}

		case anthropic.MessageStreamEventTypeError:
			if data, ok := event.Error(); ok {
				chunks <- &agent.CompletionChunk{
					Error: fmt.Errorf("anthropic stream error: %s", data.Error.Message),
				}
			}
		}
	}

	// Check for stream errors
	if err := stream.Err(); err != nil {
		chunks <- &agent.CompletionChunk{
			Error: fmt.Errorf("anthropic: stream error: %w", err),
		}
	}
}

// convertMessages converts internal messages to Anthropic format.
func (p *AnthropicProvider) convertMessages(messages []agent.CompletionMessage) ([]anthropic.MessageParam, error) {
	var result []anthropic.MessageParam

	for _, msg := range messages {
		// Skip system messages (handled separately)
		if msg.Role == "system" {
			continue
		}

		// Build content blocks
		var content []anthropic.ContentBlockParamUnion

		// Add text content
		if msg.Content != "" {
			content = append(content, anthropic.NewTextBlock(msg.Content))
		}

		// Add tool results
		for _, toolResult := range msg.ToolResults {
			content = append(content, anthropic.NewToolResultBlock(
				toolResult.ToolCallID,
				toolResult.Content,
				toolResult.IsError,
			))
		}

		// Add tool calls (for assistant messages)
		for _, toolCall := range msg.ToolCalls {
			var input map[string]interface{}
			if err := json.Unmarshal(toolCall.Input, &input); err != nil {
				return nil, fmt.Errorf("invalid tool call input: %w", err)
			}

			content = append(content, anthropic.NewToolUseBlock(
				toolCall.ID,
				toolCall.Name,
				input,
			))
		}

		// Map role
		role := anthropic.MessageParamRole(msg.Role)
		if msg.Role == "tool" {
			role = anthropic.MessageParamRoleUser
		}

		result = append(result, anthropic.MessageParam{
			Role:    anthropic.F(role),
			Content: anthropic.F(content),
		})
	}

	return result, nil
}

// convertTools converts internal tools to Anthropic format.
func (p *AnthropicProvider) convertTools(tools []agent.Tool) ([]anthropic.ToolParam, error) {
	var result []anthropic.ToolParam

	for _, tool := range tools {
		// Parse schema
		var schema map[string]interface{}
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			return nil, fmt.Errorf("invalid tool schema for %s: %w", tool.Name(), err)
		}

		result = append(result, anthropic.ToolParam{
			Name:        anthropic.F(tool.Name()),
			Description: anthropic.F(tool.Description()),
			InputSchema: anthropic.F(schema),
		})
	}

	return result, nil
}

// getModel returns the model to use, defaulting if not specified.
func (p *AnthropicProvider) getModel(model string) string {
	if model == "" {
		return p.defaultModel
	}
	return model
}

// getMaxTokens returns the max tokens to use, defaulting if not specified.
func (p *AnthropicProvider) getMaxTokens(maxTokens int) int {
	if maxTokens <= 0 {
		return 4096
	}
	return maxTokens
}

// isRetryableError checks if an error is retryable.
func (p *AnthropicProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// Rate limit errors
	if strings.Contains(errMsg, "rate_limit") ||
		strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "too many requests") {
		return true
	}

	// Server errors
	if strings.Contains(errMsg, "500") ||
		strings.Contains(errMsg, "502") ||
		strings.Contains(errMsg, "503") ||
		strings.Contains(errMsg, "504") ||
		strings.Contains(errMsg, "internal server error") ||
		strings.Contains(errMsg, "bad gateway") ||
		strings.Contains(errMsg, "service unavailable") ||
		strings.Contains(errMsg, "gateway timeout") {
		return true
	}

	// Timeout errors
	if strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "deadline exceeded") {
		return true
	}

	// Connection errors
	if strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "no such host") {
		return true
	}

	return false
}

// CountTokens estimates token count for a request (rough approximation).
func (p *AnthropicProvider) CountTokens(req *agent.CompletionRequest) int {
	// Simple character-based estimation: ~4 chars per token
	total := 0

	// Count system prompt
	total += len(req.System) / 4

	// Count messages
	for _, msg := range req.Messages {
		total += len(msg.Content) / 4
		total += len(msg.Role) / 4

		// Count tool calls
		for _, tc := range msg.ToolCalls {
			total += len(tc.Name) / 4
			total += len(tc.Input) / 4
		}

		// Count tool results
		for _, tr := range msg.ToolResults {
			total += len(tr.Content) / 4
		}
	}

	// Count tools (schemas)
	for _, tool := range req.Tools {
		total += len(tool.Name()) / 4
		total += len(tool.Description()) / 4
		total += len(tool.Schema()) / 4
	}

	return total
}

// ParseSSEStream is a helper to parse Server-Sent Events manually if needed.
func ParseSSEStream(reader io.Reader, handler func(eventType, data string) error) error {
	scanner := bufio.NewScanner(reader)
	var eventType string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line signals end of event
			if eventType != "" || len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				if err := handler(eventType, data); err != nil {
					return err
				}
				eventType = ""
				dataLines = nil
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataLines = append(dataLines, data)
		}
	}

	return scanner.Err()
}
