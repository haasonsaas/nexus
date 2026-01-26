// Package providers implements LLM provider integrations for the Nexus agent framework.
//
// This file implements the Google/Gemini provider using the new Google Gen AI Go SDK.
package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/toolconv"
	"github.com/haasonsaas/nexus/pkg/models"
	"google.golang.org/genai"
)

// GoogleProvider implements the agent.LLMProvider interface for Google's Gemini API.
// It provides a production-ready client with streaming support, automatic retries,
// tool calling, vision support, and comprehensive error handling.
//
// The provider handles several critical responsibilities:
//   - Converting between internal message formats and Gemini's API format
//   - Managing streaming responses using Go 1.23 iterators
//   - Implementing retry logic with exponential backoff for transient failures
//   - Handling tool (function) calls and results in multi-turn conversations
//   - Processing different content parts (text, images, function calls/responses)
//
// Thread Safety:
// GoogleProvider is safe for concurrent use across multiple goroutines.
// Each Complete() call creates an independent stream and goroutine.
//
// Example:
//
//	provider, err := NewGoogleProvider(GoogleConfig{
//	    APIKey:       os.Getenv("GOOGLE_API_KEY"),
//	    MaxRetries:   3,
//	    DefaultModel: "gemini-2.0-flash",
//	})
//	if err != nil {
//	    return err
//	}
//
//	req := &agent.CompletionRequest{
//	    Model:    "gemini-2.0-flash",
//	    Messages: []agent.CompletionMessage{{Role: "user", Content: "Explain quantum computing"}},
//	    Tools:    myTools, // Optional tool definitions
//	}
//
//	chunks, err := provider.Complete(ctx, req)
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Printf("Stream error: %v", chunk.Error)
//	        break
//	    }
//	    if chunk.Text != "" {
//	        fmt.Print(chunk.Text)
//	    }
//	    if chunk.ToolCall != nil {
//	        // Execute tool and continue conversation
//	    }
//	}
type GoogleProvider struct {
	// client is the underlying Google Gen AI SDK client used for API calls.
	client *genai.Client

	// apiKey stores the Google API key for authentication.
	apiKey string

	// maxRetries defines the maximum number of retry attempts for failed requests.
	// Applies to retryable errors like rate limits (429), server errors (5xx),
	// timeouts, and connection issues. Default: 3
	maxRetries int

	// retryDelay is the base delay between retry attempts.
	// Actual delay uses exponential backoff: retryDelay * 2^attempt.
	// Default: 1 second
	retryDelay time.Duration

	// defaultModel is used when CompletionRequest.Model is empty.
	// Default: "gemini-2.0-flash"
	defaultModel string

	base BaseProvider
}

// GoogleConfig holds configuration parameters for creating a GoogleProvider.
//
// All fields except APIKey are optional and will be set to sensible defaults
// if not provided. The configuration is validated during NewGoogleProvider().
//
// Example:
//
//	config := GoogleConfig{
//	    APIKey:       os.Getenv("GOOGLE_API_KEY"), // Required
//	    MaxRetries:   5,                           // Optional: default 3
//	    RetryDelay:   2 * time.Second,             // Optional: default 1s
//	    DefaultModel: "gemini-1.5-pro",            // Optional: default gemini-2.0-flash
//	}
type GoogleConfig struct {
	// APIKey is the Google AI API authentication key (required).
	// Obtain from: https://aistudio.google.com/apikey
	APIKey string

	// MaxRetries sets the maximum retry attempts for transient failures (optional).
	// Set to 0 to disable retries. Default: 3
	// Higher values increase reliability but may increase latency.
	MaxRetries int

	// RetryDelay sets the base delay between retry attempts (optional).
	// Actual delay uses exponential backoff. Default: 1 second
	// Example: with RetryDelay=1s, delays are: 1s, 2s, 4s, 8s, etc.
	RetryDelay time.Duration

	// DefaultModel sets the model to use when request doesn't specify one (optional).
	// Default: "gemini-2.0-flash"
	// Available models: see Models() method for current list.
	DefaultModel string
}

// NewGoogleProvider creates a new Google provider instance with the given configuration.
//
// This constructor validates the configuration, applies defaults for optional fields,
// and initializes the underlying Google Gen AI SDK client. The returned provider is
// ready to use for completion requests.
//
// Configuration Defaults:
//   - MaxRetries: 3 (if <= 0)
//   - RetryDelay: 1 second (if <= 0)
//   - DefaultModel: "gemini-2.0-flash" (if empty)
//
// Parameters:
//   - config: GoogleConfig containing API key and optional settings
//
// Returns:
//   - *GoogleProvider: Configured provider instance ready for use
//   - error: Returns error if APIKey is empty or client initialization fails
//
// Errors:
//   - "google: API key is required": When config.APIKey is empty string
//   - "google: failed to create client": When SDK client creation fails
//
// Example:
//
//	provider, err := NewGoogleProvider(GoogleConfig{
//	    APIKey:     os.Getenv("GOOGLE_API_KEY"),
//	    MaxRetries: 5,  // Override default
//	})
//	if err != nil {
//	    log.Fatalf("Failed to create provider: %v", err)
//	}
func NewGoogleProvider(config GoogleConfig) (*GoogleProvider, error) {
	if config.APIKey == "" {
		return nil, errors.New("google: API key is required")
	}

	// Apply defaults for optional configuration
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	if config.RetryDelay <= 0 {
		config.RetryDelay = time.Second
	}

	if config.DefaultModel == "" {
		config.DefaultModel = "gemini-2.0-flash"
	}

	// Initialize the Google Gen AI SDK client with API key
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  config.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("google: failed to create client: %w", err)
	}

	return &GoogleProvider{
		client:       client,
		apiKey:       config.APIKey,
		maxRetries:   config.MaxRetries,
		retryDelay:   config.RetryDelay,
		defaultModel: config.DefaultModel,
		base:         NewBaseProvider("google", config.MaxRetries, config.RetryDelay),
	}, nil
}

// Name returns the provider identifier used for routing and logging.
//
// This identifier should be stable and lowercase. It's used by the agent runtime
// to select the appropriate provider and in metrics/logging.
//
// Returns:
//   - string: Always returns "google"
func (p *GoogleProvider) Name() string {
	return "google"
}

// Models returns the list of available Gemini models with their capabilities.
//
// This method returns metadata about each supported Gemini model including:
//   - Model ID (used in API requests)
//   - Human-readable name
//   - Context window size in tokens
//   - Vision support capability
//
// Returns:
//   - []agent.Model: Slice of model definitions with capabilities
//
// Example:
//
//	models := provider.Models()
//	for _, model := range models {
//	    fmt.Printf("%s: %d tokens, vision=%v\n",
//	        model.Name, model.ContextSize, model.SupportsVision)
//	}
//
// Current Models (as of 2025-01):
//   - Gemini 2.0 Flash: Latest fast model (1M context, vision)
//   - Gemini 2.0 Flash Lite: Lightweight variant (1M context, vision)
//   - Gemini 1.5 Pro: High capability model (2M context, vision)
//   - Gemini 1.5 Flash: Fast balanced model (1M context, vision)
//   - Gemini 1.5 Flash-8B: Efficient model (1M context, vision)
func (p *GoogleProvider) Models() []agent.Model {
	return []agent.Model{
		{
			ID:             "gemini-2.0-flash",
			Name:           "Gemini 2.0 Flash",
			ContextSize:    1000000,
			SupportsVision: true,
		},
		{
			ID:             "gemini-2.0-flash-lite",
			Name:           "Gemini 2.0 Flash Lite",
			ContextSize:    1000000,
			SupportsVision: true,
		},
		{
			ID:             "gemini-1.5-pro",
			Name:           "Gemini 1.5 Pro",
			ContextSize:    2000000,
			SupportsVision: true,
		},
		{
			ID:             "gemini-1.5-flash",
			Name:           "Gemini 1.5 Flash",
			ContextSize:    1000000,
			SupportsVision: true,
		},
		{
			ID:             "gemini-1.5-flash-8b",
			Name:           "Gemini 1.5 Flash-8B",
			ContextSize:    1000000,
			SupportsVision: true,
		},
	}
}

// SupportsTools indicates whether this provider supports tool/function calling.
//
// Google Gemini models support function calling, allowing the LLM to request execution
// of defined functions during the conversation. This enables agentic workflows where
// the model can interact with external systems, APIs, and data sources.
//
// Tool calling workflow:
//  1. Define tools with name, description, and JSON schema
//  2. Include tools in CompletionRequest
//  3. Model may return ToolCall chunks requesting tool execution
//  4. Execute tools and send results back in subsequent messages
//  5. Model uses results to formulate final response
//
// Returns:
//   - bool: Always returns true for Google provider
//
// See Also:
//   - convertTools() for tool format conversion
//   - processStreamResponse() for handling tool call events
func (p *GoogleProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to Gemini and returns a streaming response channel.
//
// This method is the primary interface for interacting with Gemini models. It handles:
//   - Request validation and format conversion
//   - Streaming response processing using Go iterators
//   - Automatic retries with exponential backoff
//   - Tool call detection and streaming
//   - Context cancellation
//   - Error handling
//
// The method returns immediately with a channel that will receive completion chunks
// as they arrive. The channel is closed when the stream completes or encounters an error.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts. Canceling stops the stream.
//   - req: Completion request with messages, model, tools, etc.
//
// Returns:
//   - <-chan *agent.CompletionChunk: Read-only channel of response chunks
//   - error: Returns error only if request creation fails, not streaming errors
//
// Errors:
// Creation errors (returned immediately):
//   - Message conversion failures
//   - Invalid tool schemas
//
// Streaming errors (sent via chunk.Error):
//   - "google: max retries exceeded": After exhausting retry attempts
//   - "google: stream error": Server-side streaming failures
//   - context.Canceled: When context is cancelled
//   - context.DeadlineExceeded: When context times out
//
// Example - Basic Usage:
//
//	req := &agent.CompletionRequest{
//	    Model:     "gemini-2.0-flash",
//	    System:    "You are a helpful coding assistant.",
//	    Messages:  []agent.CompletionMessage{
//	        {Role: "user", Content: "Write a hello world in Go"},
//	    },
//	    MaxTokens: 1024,
//	}
//
//	chunks, err := provider.Complete(ctx, req)
//	if err != nil {
//	    return fmt.Errorf("failed to create completion: %w", err)
//	}
//
//	var response strings.Builder
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        return fmt.Errorf("stream error: %w", chunk.Error)
//	    }
//	    if chunk.Text != "" {
//	        response.WriteString(chunk.Text)
//	        fmt.Print(chunk.Text) // Print as it arrives
//	    }
//	    if chunk.Done {
//	        break
//	    }
//	}
func (p *GoogleProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	chunks := make(chan *agent.CompletionChunk)

	go func() {
		defer close(chunks)

		model := p.getModel(req.Model)
		contents, err := p.convertMessages(req.Messages)
		if err != nil {
			chunks <- &agent.CompletionChunk{Error: p.wrapError(err, model)}
			return
		}

		config := p.buildConfig(req)

		err = p.base.RetryWithBackoff(ctx, p.isRetryableError, func() error {
			streamIter := p.client.Models.GenerateContentStream(ctx, model, contents, config)
			if err := p.processStreamResponse(ctx, streamIter, chunks, model); err != nil {
				return p.wrapError(err, model)
			}
			return nil
		}, func(attempt int) time.Duration {
			return p.retryDelay * time.Duration(math.Pow(2, float64(attempt-1)))
		})

		if err != nil {
			if ctx.Err() != nil {
				chunks <- &agent.CompletionChunk{Error: ctx.Err()}
				return
			}
			if p.isRetryableError(err) {
				chunks <- &agent.CompletionChunk{Error: fmt.Errorf("google: max retries exceeded: %w", err)}
				return
			}
			chunks <- &agent.CompletionChunk{Error: err}
			return
		}

		chunks <- &agent.CompletionChunk{Done: true}
	}()

	return chunks, nil
}

// processStreamResponse processes the streaming response from Gemini.
//
// This method consumes the iterator and converts Gemini's response format into
// our internal CompletionChunk format. It handles multiple content types and manages
// the stateful accumulation of tool calls.
//
// Parameters:
//   - ctx: Context for cancellation
//   - streamIter: Gemini streaming iterator (Go 1.23 iter.Seq2)
//   - chunks: Channel to send converted chunks to
//   - model: Model name for error wrapping
//
// Returns:
//   - error: Returns error if stream processing fails
func (p *GoogleProvider) processStreamResponse(ctx context.Context, streamIter iter.Seq2[*genai.GenerateContentResponse, error], chunks chan<- *agent.CompletionChunk, model string) error {
	var streamErr error

	// Process each response from the iterator using for-range (Go 1.23+)
	for resp, err := range streamIter {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return err
		}

		if resp == nil {
			continue
		}

		// Process candidates
		for _, candidate := range resp.Candidates {
			if candidate == nil || candidate.Content == nil {
				continue
			}

			for _, part := range candidate.Content.Parts {
				if part == nil {
					continue
				}

				// Handle text content
				if part.Text != "" {
					chunks <- &agent.CompletionChunk{
						Text: part.Text,
					}
				}

				// Handle function calls
				if part.FunctionCall != nil {
					// Convert function call args to JSON
					argsJSON, jsonErr := json.Marshal(part.FunctionCall.Args)
					if jsonErr != nil {
						argsJSON = []byte("{}")
					}

					toolCall := &models.ToolCall{
						ID:    generateToolCallID(part.FunctionCall.Name),
						Name:  part.FunctionCall.Name,
						Input: argsJSON,
					}

					chunks <- &agent.CompletionChunk{
						ToolCall: toolCall,
					}
				}
			}
		}
	}

	return streamErr
}

// convertMessages converts internal message format to Gemini API format.
//
// This method handles the translation between our unified message format and
// Gemini's specific requirements:
//   - User messages become "user" role with Parts
//   - Assistant messages become "model" role with Parts
//   - Tool results become function response Parts
//   - Tool calls become function call Parts
//   - Attachments are converted to inline data Parts
//
// Parameters:
//   - messages: Internal message format from CompletionRequest
//
// Returns:
//   - []*genai.Content: Gemini-formatted content array
//   - error: Returns error if conversion fails
func (p *GoogleProvider) convertMessages(messages []agent.CompletionMessage) ([]*genai.Content, error) {
	var result []*genai.Content

	for _, msg := range messages {
		// Skip system messages - they're handled via SystemInstruction in config
		if msg.Role == "system" {
			continue
		}

		content := &genai.Content{}

		// Map roles
		switch msg.Role {
		case "user":
			content.Role = genai.RoleUser
		case "assistant":
			content.Role = genai.RoleModel
		case "tool":
			content.Role = genai.RoleUser // Tool results come from user side
		default:
			content.Role = genai.RoleUser
		}

		// Add text content if present
		if msg.Content != "" {
			content.Parts = append(content.Parts, &genai.Part{
				Text: msg.Content,
			})
		}

		// Add attachments (images, etc.)
		for _, att := range msg.Attachments {
			if att.Type == "image" {
				part, err := p.convertAttachment(att)
				if err != nil {
					continue // Skip problematic attachments
				}
				content.Parts = append(content.Parts, part)
			}
		}

		// Add tool calls (for assistant messages)
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal(tc.Input, &args); err != nil {
				args = make(map[string]any)
			}

			content.Parts = append(content.Parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					Name: tc.Name,
					Args: args,
				},
			})
		}

		// Add tool results (for tool messages)
		for _, tr := range msg.ToolResults {
			// Parse result content as JSON if possible
			var response map[string]any
			if err := json.Unmarshal([]byte(tr.Content), &response); err != nil {
				// If not JSON, wrap in a result field
				response = map[string]any{
					"result": tr.Content,
					"error":  tr.IsError,
				}
			}

			content.Parts = append(content.Parts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     getToolNameFromID(tr.ToolCallID, messages),
					Response: response,
				},
			})
		}

		if len(content.Parts) > 0 {
			result = append(result, content)
		}
	}

	return result, nil
}

// convertAttachment converts an attachment to a Gemini Part.
//
// This method handles image attachments by:
//   - Detecting base64-encoded data URLs
//   - Creating inline data blobs with proper MIME types
//   - Falling back to file URIs for non-data URLs
//
// Parameters:
//   - att: Attachment to convert
//
// Returns:
//   - *genai.Part: Gemini-formatted part
//   - error: Returns error if conversion fails
func (p *GoogleProvider) convertAttachment(att models.Attachment) (*genai.Part, error) {
	// Check if it's a base64 data URL
	if strings.HasPrefix(att.URL, "data:") {
		// Parse data URL: data:[<mediatype>][;base64],<data>
		parts := strings.SplitN(att.URL, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid data URL format")
		}

		// Extract MIME type
		mimeType := "image/jpeg" // default
		if strings.Contains(parts[0], ";") {
			mimeTypeParts := strings.Split(strings.TrimPrefix(parts[0], "data:"), ";")
			if len(mimeTypeParts) > 0 && mimeTypeParts[0] != "" {
				mimeType = mimeTypeParts[0]
			}
		} else {
			mimeType = strings.TrimPrefix(parts[0], "data:")
		}

		// Decode base64 data
		data, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 data: %w", err)
		}

		return &genai.Part{
			InlineData: &genai.Blob{
				Data:     data,
				MIMEType: mimeType,
			},
		}, nil
	}

	// For regular URLs, use FileData
	mimeType := att.MimeType
	if mimeType == "" {
		mimeType = guessMimeType(att.URL)
	}

	return &genai.Part{
		FileData: &genai.FileData{
			FileURI:  att.URL,
			MIMEType: mimeType,
		},
	}, nil
}

// convertTools converts internal tool definitions to Gemini Tool format.
//
// This method translates tool definitions from our internal format to Gemini's
// function declaration schema. Each tool includes:
//   - Name: Function identifier for the LLM
//   - Description: Natural language description of what the tool does
//   - Parameters: JSON Schema defining required/optional parameters
//
// Parameters:
//   - tools: Internal tool definitions implementing agent.Tool interface
//
// Returns:
//   - []*genai.Tool: Gemini-formatted tool definitions
func (p *GoogleProvider) convertTools(tools []agent.Tool) []*genai.Tool {
	return toolconv.ToGeminiTools(tools)
}

// buildConfig builds the GenerateContentConfig from a CompletionRequest.
//
// This method configures:
//   - System instruction (from req.System)
//   - Tools/functions
//   - Max output tokens
//   - Other generation parameters
//
// Parameters:
//   - req: Completion request containing configuration
//
// Returns:
//   - *genai.GenerateContentConfig: Configured generation settings
func (p *GoogleProvider) buildConfig(req *agent.CompletionRequest) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{}

	// Set system instruction if provided
	if req.System != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{Text: req.System},
			},
		}
	}

	// Set max tokens if provided
	if req.MaxTokens > 0 {
		maxTokens := min(req.MaxTokens, math.MaxInt32)
		// #nosec G115 -- bounded by min above
		config.MaxOutputTokens = int32(maxTokens)
	}

	// Convert and set tools
	if len(req.Tools) > 0 {
		config.Tools = p.convertTools(req.Tools)
	}

	return config
}

// getModel returns the model ID to use for the request.
//
// If the request specifies a model, that model is used. Otherwise, returns
// the provider's default model configured during initialization.
//
// Parameters:
//   - model: Model ID from CompletionRequest (may be empty)
//
// Returns:
//   - string: Model ID to use (never empty)
func (p *GoogleProvider) getModel(model string) string {
	if model == "" {
		return p.defaultModel
	}
	return model
}

// isRetryableError determines if an error should trigger a retry attempt.
//
// This method classifies errors into retryable and non-retryable categories.
// Retryable errors are typically transient (rate limits, server issues, network
// problems) while non-retryable errors are permanent (invalid API key, malformed
// request, etc.).
//
// Retryable Error Categories:
//   - Rate limits: 429 status, "rate_limit", "too many requests"
//   - Server errors: 500, 502, 503, 504 status codes
//   - Timeouts: "timeout", "deadline exceeded"
//   - Network: "connection reset", "connection refused"
//
// Parameters:
//   - err: Error to classify
//
// Returns:
//   - bool: true if error should be retried, false otherwise
func (p *GoogleProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if providerErr, ok := GetProviderError(err); ok {
		return providerErr.Reason.IsRetryable()
	}

	errMsg := strings.ToLower(err.Error())

	// Rate limit errors
	if strings.Contains(errMsg, "rate limit") ||
		strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "too many requests") ||
		strings.Contains(errMsg, "resource exhausted") ||
		strings.Contains(errMsg, "quota") {
		return true
	}

	// Server errors (5xx)
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

// wrapError wraps an error in a ProviderError with Google-specific context.
//
// This method extracts relevant information from Google API errors and creates
// a standardized ProviderError for consistent error handling across providers.
//
// Parameters:
//   - err: Original error to wrap
//   - model: Model name for context
//
// Returns:
//   - error: Wrapped ProviderError
func (p *GoogleProvider) wrapError(err error, model string) error {
	if err == nil {
		return nil
	}
	if IsProviderError(err) {
		return err
	}

	providerErr := NewProviderError("google", model, err)

	// Try to extract status code from error message
	errMsg := strings.ToLower(err.Error())

	if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "unauthenticated") {
		providerErr = providerErr.WithStatus(http.StatusUnauthorized)
	} else if strings.Contains(errMsg, "403") || strings.Contains(errMsg, "permission denied") {
		providerErr = providerErr.WithStatus(http.StatusForbidden)
	} else if strings.Contains(errMsg, "404") || strings.Contains(errMsg, "not found") {
		providerErr = providerErr.WithStatus(http.StatusNotFound)
	} else if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "resource exhausted") {
		providerErr = providerErr.WithStatus(http.StatusTooManyRequests)
	} else if strings.Contains(errMsg, "500") {
		providerErr = providerErr.WithStatus(http.StatusInternalServerError)
	} else if strings.Contains(errMsg, "503") {
		providerErr = providerErr.WithStatus(http.StatusServiceUnavailable)
	}

	return providerErr
}

// CountTokens estimates the token count for a completion request.
//
// This provides a rough approximation using character-based estimation rather
// than actual tokenization. The estimate uses ~4 characters per token, which
// is typical for English text.
//
// Parameters:
//   - req: Completion request to estimate tokens for
//
// Returns:
//   - int: Estimated token count
func (p *GoogleProvider) CountTokens(req *agent.CompletionRequest) int {
	total := 0

	// Count system prompt tokens
	total += len(req.System) / 4

	// Count message content and metadata
	for _, msg := range req.Messages {
		total += len(msg.Content) / 4
		total += len(msg.Role) / 4

		for _, tc := range msg.ToolCalls {
			total += len(tc.Name) / 4
			total += len(tc.Input) / 4
		}

		for _, tr := range msg.ToolResults {
			total += len(tr.Content) / 4
		}
	}

	// Count tool definitions
	for _, tool := range req.Tools {
		total += len(tool.Name()) / 4
		total += len(tool.Description()) / 4
		total += len(tool.Schema()) / 4
	}

	return total
}

// Helper functions

// generateToolCallID generates a unique ID for a tool call.
// Gemini doesn't provide tool call IDs, so we generate them.
func generateToolCallID(name string) string {
	return fmt.Sprintf("call_%s_%d", name, time.Now().UnixNano())
}

// getToolNameFromID retrieves the tool name from a tool call ID by looking
// at previous messages that contain tool calls.
func getToolNameFromID(toolCallID string, messages []agent.CompletionMessage) string {
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.ID == toolCallID {
				return tc.Name
			}
		}
	}
	// Fall back to extracting from the ID format "call_<name>_<timestamp>"
	parts := strings.Split(toolCallID, "_")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// guessMimeType guesses the MIME type from a URL based on file extension.
func guessMimeType(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(lower, ".pdf"):
		return "application/pdf"
	default:
		return "image/jpeg" // Default to JPEG for images
	}
}
