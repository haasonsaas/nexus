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

// OpenAIProvider implements the agent.LLMProvider interface for OpenAI's GPT models.
// It provides streaming completions, tool/function calling, vision support, and
// automatic retry logic for production use.
//
// The provider handles several key responsibilities:
//   - Converting between internal message formats and OpenAI's API format
//   - Managing streaming responses with real-time token delivery
//   - Implementing retry logic with backoff for transient failures
//   - Handling multi-turn tool (function) calling conversations
//   - Supporting vision-capable models with image attachments
//
// Key Differences from Anthropic Provider:
//   - System messages are included in the messages array (not separate)
//   - Tool calls stream incrementally and must be accumulated
//   - Vision support uses multi-content message format
//   - Tool results require separate messages (one per tool call)
//
// Thread Safety:
// OpenAIProvider is safe for concurrent use across multiple goroutines.
// Each Complete() call creates an independent stream and goroutine.
//
// Example:
//
//	provider := NewOpenAIProvider(os.Getenv("OPENAI_API_KEY"))
//
//	req := &agent.CompletionRequest{
//	    Model:    "gpt-4o",
//	    System:   "You are a helpful assistant.",
//	    Messages: []agent.CompletionMessage{{Role: "user", Content: "Hello!"}},
//	}
//
//	chunks, err := provider.Complete(ctx, req)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Printf("Error: %v", chunk.Error)
//	        break
//	    }
//	    fmt.Print(chunk.Text)
//	}
type OpenAIProvider struct {
	// client is the underlying OpenAI SDK client used for API calls.
	client *openai.Client

	// apiKey stores the OpenAI API key for authentication.
	// Format: sk-...
	apiKey string

	// maxRetries defines the maximum number of retry attempts for failed requests.
	// Applies to retryable errors like rate limits (429), server errors (5xx).
	// Default: 3
	maxRetries int

	// retryDelay is the base delay between retry attempts.
	// Actual delay is: retryDelay * attempt (linear backoff).
	// Default: 1 second
	retryDelay time.Duration
}

// NewOpenAIProvider creates a new OpenAI provider instance.
//
// If an empty API key is provided, the provider will be created but Complete()
// will return an error when called. This allows for delayed configuration.
//
// Configuration Defaults:
//   - MaxRetries: 3 (hardcoded)
//   - RetryDelay: 1 second (hardcoded)
//
// Parameters:
//   - apiKey: OpenAI API key (can be empty for delayed configuration)
//
// Returns:
//   - *OpenAIProvider: Provider instance (never nil)
//
// Example:
//
//	// Standard usage
//	provider := NewOpenAIProvider(os.Getenv("OPENAI_API_KEY"))
//
//	// Delayed configuration
//	provider := NewOpenAIProvider("")
//	// Later: will error on Complete() calls until configured
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	// Allow empty API key for delayed configuration
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

// Name returns the provider identifier used for routing and logging.
//
// This identifier should be stable and lowercase. It's used by the agent runtime
// to select the appropriate provider and in metrics/logging.
//
// Returns:
//   - string: Always returns "openai"
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Models returns the list of available GPT models with their capabilities.
//
// This method returns metadata about supported OpenAI models including:
//   - Model ID (used in API requests)
//   - Human-readable name
//   - Context window size in tokens
//   - Vision support capability
//
// Note: OpenAI frequently updates models and deprecates old ones. This list
// may not reflect the absolute latest models. Check OpenAI's documentation
// for the most current model availability.
//
// Returns:
//   - []agent.Model: Slice of model definitions with capabilities
//
// Example:
//
//	models := provider.Models()
//	for _, model := range models {
//	    if model.SupportsVision {
//	        fmt.Printf("Vision model: %s (%d tokens)\n", model.Name, model.ContextSize)
//	    }
//	}
//
// Current Models:
//   - GPT-4o: Latest multimodal model (128K context, vision)
//   - GPT-4 Turbo: Fast GPT-4 variant (128K context, vision)
//   - GPT-3.5 Turbo: Cost-effective for simple tasks (16K context, no vision)
//   - GPT-4: Original GPT-4 (8K context, no vision)
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

// SupportsTools indicates whether this provider supports tool/function calling.
//
// OpenAI's GPT models support function calling, allowing the model to request
// execution of defined functions during conversations. This enables agentic
// workflows where the model can interact with external systems.
//
// Function Calling Workflow:
//  1. Define functions with name, description, and parameters schema
//  2. Include functions in CompletionRequest.Tools
//  3. Model may return ToolCall chunks (streamed incrementally)
//  4. Execute functions and send results in subsequent messages
//  5. Model uses results to formulate final response
//
// OpenAI-Specific Behavior:
//   - Tool calls stream incrementally (ID, name, arguments come in chunks)
//   - Must accumulate chunks to build complete tool call
//   - FinishReason "tool_calls" signals all tool calls are complete
//
// Returns:
//   - bool: Always returns true for OpenAI provider
//
// See Also:
//   - convertToOpenAITools() for tool format conversion
//   - processStream() for handling incremental tool call chunks
func (p *OpenAIProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to GPT and returns a streaming response channel.
//
// This is the primary interface for interacting with OpenAI models. It handles:
//   - Request validation and API key checks
//   - Message format conversion (including system prompt injection)
//   - Vision support with multi-content messages
//   - Streaming response processing
//   - Automatic retries with linear backoff
//   - Tool call accumulation across chunks
//   - Context cancellation
//
// The method returns immediately with a channel that will receive completion chunks
// as they arrive from OpenAI's streaming API.
//
// OpenAI Streaming Specifics:
//   - Tool calls arrive incrementally (ID, name, args streamed separately)
//   - Must accumulate tool arguments across multiple delta chunks
//   - Tool calls complete when FinishReason is "tool_calls"
//   - Multiple tool calls can be in progress simultaneously (tracked by index)
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - req: Completion request with messages, model, tools, etc.
//
// Returns:
//   - <-chan *agent.CompletionChunk: Read-only channel of response chunks
//   - error: Returns error only for immediate failures (not streaming errors)
//
// Errors:
// Immediate errors (returned):
//   - "OpenAI API key not configured": Client is nil (empty API key)
//   - "failed to convert messages": Message format conversion failed
//   - "non-retryable error": Authentication or validation error
//   - "max retries exceeded": All retry attempts exhausted
//
// Streaming errors (sent via chunk.Error):
//   - io.EOF: Stream completed normally (triggers Done chunk)
//   - context.Canceled: Context was cancelled
//   - Network errors: Connection issues during streaming
//
// Example - Basic Text Generation:
//
//	req := &agent.CompletionRequest{
//	    Model:     "gpt-4o",
//	    System:    "You are a helpful assistant.",
//	    Messages:  []agent.CompletionMessage{{Role: "user", Content: "Hello!"}},
//	    MaxTokens: 500,
//	}
//
//	chunks, err := provider.Complete(ctx, req)
//	if err != nil {
//	    log.Fatalf("Failed to start completion: %v", err)
//	}
//
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Printf("Stream error: %v", chunk.Error)
//	        break
//	    }
//	    fmt.Print(chunk.Text)
//	}
//
// Example - With Vision:
//
//	req := &agent.CompletionRequest{
//	    Model: "gpt-4o",
//	    Messages: []agent.CompletionMessage{{
//	        Role:    "user",
//	        Content: "What's in this image?",
//	        Attachments: []models.Attachment{{
//	            Type: "image",
//	            URL:  "https://example.com/photo.jpg",
//	        }},
//	    }},
//	}
//
//	chunks, err := provider.Complete(ctx, req)
//	// Process response...
//
// Example - With Function Calling:
//
//	chunks, err := provider.Complete(ctx, &agent.CompletionRequest{
//	    Model:    "gpt-4o",
//	    Messages: []agent.CompletionMessage{{Role: "user", Content: "What's 15 * 7?"}},
//	    Tools:    []agent.Tool{calculatorTool},
//	})
//
//	for chunk := range chunks {
//	    if chunk.ToolCall != nil {
//	        // Tool call is complete, execute it
//	        result := executeFunction(chunk.ToolCall.Name, chunk.ToolCall.Input)
//	        // Continue conversation with result...
//	    }
//	}
func (p *OpenAIProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	// Validate API key is configured
	if p.client == nil {
		return nil, errors.New("OpenAI API key not configured")
	}

	// Convert internal messages to OpenAI format (includes system prompt)
	messages, err := p.convertToOpenAIMessages(req.Messages, req.System)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Build OpenAI API request
	chatReq := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   true, // Always use streaming for real-time responses
	}

	if req.MaxTokens > 0 {
		chatReq.MaxTokens = req.MaxTokens
	}

	// Add tool/function definitions if provided
	if len(req.Tools) > 0 {
		chatReq.Tools = p.convertToOpenAITools(req.Tools)
	}

	// Create streaming request with retry logic
	var stream *openai.ChatCompletionStream
	var lastErr error

	// Linear backoff retry loop (delay increases linearly: 0s, 1s, 2s, 3s)
	for attempt := 0; attempt < p.maxRetries; attempt++ {
		if attempt > 0 {
			// Wait with linear backoff before retry
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

		// Check if error is retryable (rate limits, server errors)
		if !p.isRetryableError(lastErr) {
			return nil, fmt.Errorf("non-retryable error: %w", lastErr)
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
	}

	// Spawn goroutine to process stream and send chunks
	chunks := make(chan *agent.CompletionChunk)
	go p.processStream(ctx, stream, chunks)

	return chunks, nil
}

// processStream processes OpenAI's streaming response and converts to internal format.
//
// This method consumes the OpenAI stream, handles incremental updates, and manages
// the complex state required for tool call accumulation.
//
// Key Responsibilities:
//   - Reading streaming response chunks with stream.Recv()
//   - Extracting text deltas and emitting them immediately
//   - Accumulating tool call fragments across multiple chunks
//   - Detecting tool call completion via FinishReason
//   - Handling EOF and error conditions
//   - Context cancellation support
//
// Tool Call Accumulation:
// OpenAI streams tool calls incrementally across multiple chunks:
//  1. First chunk contains ID and function name
//  2. Subsequent chunks contain argument fragments (streamed JSON)
//  3. FinishReason "tool_calls" signals all tool calls are complete
//  4. Multiple tool calls can be in progress (tracked by index in map)
//
// Parameters:
//   - ctx: Context for cancellation
//   - stream: OpenAI streaming response
//   - chunks: Channel to send converted chunks to (closed by this method)
//
// Chunk Emissions:
//   - Text chunks: Emitted immediately for each delta.Content
//   - Tool call chunks: Emitted when FinishReason is "tool_calls"
//   - Done chunk: Emitted on io.EOF
//   - Error chunk: Emitted on streaming errors
func (p *OpenAIProvider) processStream(ctx context.Context, stream *openai.ChatCompletionStream, chunks chan<- *agent.CompletionChunk) {
	defer close(chunks)
	defer stream.Close()

	// Track tool calls being accumulated across multiple chunks
	// Map key is the tool call index (OpenAI can return multiple tool calls)
	toolCalls := make(map[int]*models.ToolCall)

	for {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			chunks <- &agent.CompletionChunk{Error: ctx.Err(), Done: true}
			return
		default:
		}

		// Receive next chunk from stream
		response, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				// Stream completed normally - emit any pending tool calls
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
			// Stream error
			chunks <- &agent.CompletionChunk{Error: err, Done: true}
			return
		}

		// Skip empty responses
		if len(response.Choices) == 0 {
			continue
		}

		delta := response.Choices[0].Delta

		// Handle text content - emit immediately for real-time streaming
		if delta.Content != "" {
			chunks <- &agent.CompletionChunk{
				Text: delta.Content,
			}
		}

		// Handle tool call deltas - accumulate across chunks
		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				// Get tool call index (OpenAI uses index to track multiple parallel calls)
				index := 0
				if tc.Index != nil {
					index = *tc.Index
				}

				// Initialize tool call if this is the first chunk for this index
				if toolCalls[index] == nil {
					toolCalls[index] = &models.ToolCall{}
				}

				// Update tool call fields (each field comes in first chunk it appears)
				if tc.ID != "" {
					toolCalls[index].ID = tc.ID
				}
				if tc.Function.Name != "" {
					toolCalls[index].Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					// Append argument fragments (streamed as JSON chunks)
					var currentArgs string
					if toolCalls[index].Input != nil {
						currentArgs = string(toolCalls[index].Input)
					}
					currentArgs += tc.Function.Arguments
					toolCalls[index].Input = json.RawMessage(currentArgs)
				}
			}
		}

		// Check finish reason - "tool_calls" means all tool calls are complete
		if response.Choices[0].FinishReason == "tool_calls" {
			for _, tc := range toolCalls {
				if tc.ID != "" && tc.Name != "" {
					chunks <- &agent.CompletionChunk{
						ToolCall: tc,
					}
				}
			}
			// Reset for potential next iteration (though typically stream ends here)
			toolCalls = make(map[int]*models.ToolCall)
		}
	}
}

// convertToOpenAIMessages converts internal message format to OpenAI API format.
//
// This method handles the translation between our unified message format and
// OpenAI's specific requirements:
//   - System prompts are injected as first message with role "system"
//   - User/assistant messages are converted with their content
//   - Tool calls are included in assistant messages
//   - Tool results are converted to separate "tool" role messages
//   - Vision attachments use multi-content format
//
// OpenAI Format Specifics:
//   - System message is part of messages array (unlike Anthropic)
//   - Tool results each become a separate message with role "tool"
//   - Vision uses MultiContent field instead of Content string
//
// Parameters:
//   - messages: Internal message format from CompletionRequest
//   - system: System prompt to inject as first message (optional)
//
// Returns:
//   - []openai.ChatCompletionMessage: OpenAI-formatted messages
//   - error: Currently never returns error (reserved for future use)
//
// Example Conversion:
//
//	Internal:
//	  System: "You are helpful"
//	  Messages: [{Role: "user", Content: "Hi"}]
//
//	Converts to:
//	  [{Role: "system", Content: "You are helpful"},
//	   {Role: "user", Content: "Hi"}]
func (p *OpenAIProvider) convertToOpenAIMessages(messages []agent.CompletionMessage, system string) ([]openai.ChatCompletionMessage, error) {
	result := make([]openai.ChatCompletionMessage, 0, len(messages)+1)

	// Inject system message as first message (OpenAI-specific)
	if system != "" {
		result = append(result, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: system,
		})
	}

	// Convert each internal message to OpenAI format
	for _, msg := range messages {
		oaiMsg := openai.ChatCompletionMessage{
			Role: msg.Role,
		}

		// Handle message based on role and content type
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
					// Use multi-content format for vision (GPT-4o, GPT-4 Turbo)
					contentParts := make([]openai.ChatMessagePart, 0)

					// Add text content first if present
					if msg.Content != "" {
						contentParts = append(contentParts, openai.ChatMessagePart{
							Type: openai.ChatMessagePartTypeText,
							Text: msg.Content,
						})
					}

					// Add image attachments as URLs
					for _, att := range msg.Attachments {
						if att.Type == "image" {
							contentParts = append(contentParts, openai.ChatMessagePart{
								Type: openai.ChatMessagePartTypeImageURL,
								ImageURL: &openai.ChatMessageImageURL{
									URL:    att.URL,
									Detail: openai.ImageURLDetailAuto, // Auto-select detail level
								},
							})
						}
					}

					oaiMsg.MultiContent = contentParts
				} else {
					// No images, use simple string content
					oaiMsg.Content = msg.Content
				}
			} else {
				// No attachments, use simple string content
				oaiMsg.Content = msg.Content
			}

		case "assistant":
			oaiMsg.Content = msg.Content

			// Handle tool calls from assistant (function calling requests)
			if len(msg.ToolCalls) > 0 {
				oaiMsg.ToolCalls = make([]openai.ToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					oaiMsg.ToolCalls[i] = openai.ToolCall{
						ID:   tc.ID,
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      tc.Name,
							Arguments: string(tc.Input), // JSON arguments as string
						},
					}
				}
			}

		case "tool":
			// Handle tool results - OpenAI requires separate message per result
			if len(msg.ToolResults) > 0 {
				for _, tr := range msg.ToolResults {
					result = append(result, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						Content:    tr.Content,
						ToolCallID: tr.ToolCallID, // Links result to tool call
					})
				}
				continue // Skip the append below
			}
		}

		result = append(result, oaiMsg)
	}

	return result, nil
}

// convertToOpenAITools converts internal tool definitions to OpenAI function format.
//
// This method translates tool definitions from our internal format to OpenAI's
// function calling schema. Each tool becomes a function definition with:
//   - Name: Function identifier
//   - Description: Natural language description
//   - Parameters: JSON Schema for function parameters
//
// Parameters:
//   - tools: Internal tool definitions implementing agent.Tool interface
//
// Returns:
//   - []openai.Tool: OpenAI-formatted function definitions
//
// Error Handling:
// If a tool's schema is invalid JSON, it's replaced with an empty object schema.
// This prevents one bad tool from breaking all function calling.
//
// Example:
//
//	Internal tool:
//	  Name: "get_weather"
//	  Description: "Get current weather"
//	  Schema: {"type":"object","properties":{"city":{"type":"string"}}}
//
//	Converts to OpenAI function with same fields in FunctionDefinition.
func (p *OpenAIProvider) convertToOpenAITools(tools []agent.Tool) []openai.Tool {
	result := make([]openai.Tool, len(tools))

	for i, tool := range tools {
		// Parse JSON schema into map
		var schemaMap map[string]any
		if err := json.Unmarshal(tool.Schema(), &schemaMap); err != nil {
			// Graceful degradation: use empty schema if parsing fails
			// This allows other tools to work even if one has a bad schema
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
				Parameters:  schemaMap, // JSON Schema for parameters
			},
		}
	}

	return result
}

// isRetryableError determines if an error should trigger a retry attempt.
//
// This method classifies errors into retryable and non-retryable categories
// based on error messages. Retryable errors are typically transient.
//
// Retryable Error Categories:
//   - Rate limits: "rate limit", "429"
//   - Server errors: "500", "502", "503", "504"
//   - Timeouts: "timeout", "deadline exceeded"
//
// Non-Retryable Errors:
//   - Authentication: API key issues
//   - Validation: Malformed requests
//   - Not found: Invalid endpoints or models
//
// Parameters:
//   - err: Error to classify
//
// Returns:
//   - bool: true if error should be retried, false otherwise
func (p *OpenAIProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check error message for retryable indicators
	errMsg := err.Error()

	// Rate limit errors - too many requests
	if contains(errMsg, "rate limit") || contains(errMsg, "429") {
		return true
	}

	// Server errors (5xx) - temporary OpenAI infrastructure issues
	if contains(errMsg, "500") || contains(errMsg, "502") || contains(errMsg, "503") || contains(errMsg, "504") {
		return true
	}

	// Timeout errors - request took too long
	if contains(errMsg, "timeout") || contains(errMsg, "deadline exceeded") {
		return true
	}

	return false
}

// contains checks if a string contains a substring.
//
// This is a simple helper function for error message inspection.
// It performs exact string matching (case-sensitive).
//
// Parameters:
//   - s: String to search in
//   - substr: Substring to search for
//
// Returns:
//   - bool: true if substr is found in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
		(findSubstring(s, substr) >= 0))
}

// findSubstring finds the first occurrence of a substring.
//
// Internal helper for contains(). Uses simple linear search.
//
// Parameters:
//   - s: String to search in
//   - substr: Substring to find
//
// Returns:
//   - int: Index of first occurrence, or -1 if not found
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
