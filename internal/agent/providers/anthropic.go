// Package providers implements LLM provider integrations for the Nexus agent framework.
//
// This package provides production-ready implementations of the agent.LLMProvider interface
// for various LLM services including Anthropic's Claude and OpenAI's GPT models. Each provider
// handles the complexities of API integration, streaming responses, error handling, retries,
// and format conversion.
//
// Key Features:
//   - Streaming responses for real-time token delivery
//   - Automatic retry logic with exponential backoff
//   - Tool/function calling support for agentic workflows
//   - Vision support for image-capable models
//   - Comprehensive error handling and context cancellation
//   - Rate limit management
//
// Example Usage:
//
//	// Create an Anthropic provider
//	provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
//	    APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
//	    MaxRetries:   3,
//	    RetryDelay:   time.Second,
//	    DefaultModel: "claude-sonnet-4-20250514",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Send a completion request
//	req := &agent.CompletionRequest{
//	    Model:     "claude-sonnet-4-20250514",
//	    System:    "You are a helpful assistant.",
//	    Messages:  []agent.CompletionMessage{{Role: "user", Content: "Hello!"}},
//	    MaxTokens: 1024,
//	}
//
//	chunks, err := provider.Complete(ctx, req)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Process streaming response
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Printf("Error: %v", chunk.Error)
//	        break
//	    }
//	    fmt.Print(chunk.Text)
//	}
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
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// AnthropicProvider implements the agent.LLMProvider interface for Anthropic's Claude API.
// It provides a production-ready client with streaming support, automatic retries,
// tool calling, and comprehensive error handling.
//
// The provider handles several critical responsibilities:
//   - Converting between internal message formats and Anthropic's API format
//   - Managing streaming Server-Sent Events (SSE) responses
//   - Implementing retry logic with exponential backoff for transient failures
//   - Handling tool (function) calls and results in multi-turn conversations
//   - Processing different content blocks (text, tool use, tool results)
//
// Thread Safety:
// AnthropicProvider is safe for concurrent use across multiple goroutines.
// Each Complete() call creates an independent stream and goroutine.
//
// Example:
//
//	provider, err := NewAnthropicProvider(AnthropicConfig{
//	    APIKey:     "sk-ant-...",
//	    MaxRetries: 3,
//	})
//	if err != nil {
//	    return err
//	}
//
//	req := &agent.CompletionRequest{
//	    Model:    "claude-sonnet-4-20250514",
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
type AnthropicProvider struct {
	// client is the underlying Anthropic SDK client used for API calls.
	client anthropic.Client

	// apiKey stores the Anthropic API key for authentication.
	// Format: sk-ant-api03-...
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
	// Default: "claude-sonnet-4-20250514"
	defaultModel string
}

// AnthropicConfig holds configuration parameters for creating an AnthropicProvider.
//
// All fields except APIKey are optional and will be set to sensible defaults
// if not provided. The configuration is validated during NewAnthropicProvider().
//
// Example:
//
//	config := AnthropicConfig{
//	    APIKey:       os.Getenv("ANTHROPIC_API_KEY"), // Required
//	    MaxRetries:   5,                              // Optional: default 3
//	    RetryDelay:   2 * time.Second,                // Optional: default 1s
//	    DefaultModel: "claude-opus-4-20250514",       // Optional: default sonnet-4
//	}
type AnthropicConfig struct {
	// APIKey is the Anthropic API authentication key (required).
	// Obtain from: https://console.anthropic.com/
	// Format: sk-ant-api03-...
	APIKey string

	// BaseURL overrides the default Anthropic API base URL.
	// Example: "https://api.anthropic.com/"
	BaseURL string

	// MaxRetries sets the maximum retry attempts for transient failures (optional).
	// Set to 0 to disable retries. Default: 3
	// Higher values increase reliability but may increase latency.
	MaxRetries int

	// RetryDelay sets the base delay between retry attempts (optional).
	// Actual delay uses exponential backoff. Default: 1 second
	// Example: with RetryDelay=1s, delays are: 1s, 2s, 4s, 8s, etc.
	RetryDelay time.Duration

	// DefaultModel sets the model to use when request doesn't specify one (optional).
	// Default: "claude-sonnet-4-20250514"
	// Available models: see Models() method for current list.
	DefaultModel string
}

// NewAnthropicProvider creates a new Anthropic provider instance with the given configuration.
//
// This constructor validates the configuration, applies defaults for optional fields,
// and initializes the underlying Anthropic SDK client. The returned provider is
// ready to use for completion requests.
//
// Configuration Defaults:
//   - MaxRetries: 3 (if <= 0)
//   - RetryDelay: 1 second (if <= 0)
//   - DefaultModel: "claude-sonnet-4-20250514" (if empty)
//
// Parameters:
//   - config: AnthropicConfig containing API key and optional settings
//
// Returns:
//   - *AnthropicProvider: Configured provider instance ready for use
//   - error: Returns error if APIKey is empty
//
// Errors:
//   - "anthropic: API key is required": When config.APIKey is empty string
//
// Example:
//
//	provider, err := NewAnthropicProvider(AnthropicConfig{
//	    APIKey:     os.Getenv("ANTHROPIC_API_KEY"),
//	    MaxRetries: 5,  // Override default
//	})
//	if err != nil {
//	    log.Fatalf("Failed to create provider: %v", err)
//	}
func NewAnthropicProvider(config AnthropicConfig) (*AnthropicProvider, error) {
	if config.APIKey == "" {
		return nil, errors.New("anthropic: API key is required")
	}

	// Apply defaults for optional configuration
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	if config.RetryDelay <= 0 {
		config.RetryDelay = time.Second
	}

	if config.DefaultModel == "" {
		config.DefaultModel = "claude-sonnet-4-20250514"
	}

	// Initialize the Anthropic SDK client with API key
	options := []option.RequestOption{option.WithAPIKey(config.APIKey)}
	if strings.TrimSpace(config.BaseURL) != "" {
		options = append(options, option.WithBaseURL(config.BaseURL))
	}
	client := anthropic.NewClient(options...)

	return &AnthropicProvider{
		client:       client,
		apiKey:       config.APIKey,
		maxRetries:   config.MaxRetries,
		retryDelay:   config.RetryDelay,
		defaultModel: config.DefaultModel,
	}, nil
}

// Name returns the provider identifier used for routing and logging.
//
// This identifier should be stable and lowercase. It's used by the agent runtime
// to select the appropriate provider and in metrics/logging.
//
// Returns:
//   - string: Always returns "anthropic"
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// Models returns the list of available Claude models with their capabilities.
//
// This method returns metadata about each supported Claude model including:
//   - Model ID (used in API requests)
//   - Human-readable name
//   - Context window size in tokens
//   - Vision support capability
//
// The list includes both current and legacy models. Model IDs include version
// suffixes (e.g., "20250514") for API compatibility.
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
//   - Claude Sonnet 4: Latest balanced model (200K context, vision)
//   - Claude Opus 4: Most capable model (200K context, vision)
//   - Claude 3.5 Sonnet: Previous generation (200K context, vision)
//   - Claude 3 Opus/Sonnet/Haiku: Legacy models (200K context, vision)
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

// SupportsTools indicates whether this provider supports tool/function calling.
//
// Anthropic Claude models support tool use, allowing the LLM to request execution
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
//   - bool: Always returns true for Anthropic provider
//
// See Also:
//   - convertTools() for tool format conversion
//   - processStream() for handling tool call events
func (p *AnthropicProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to Claude and returns a streaming response channel.
//
// This method is the primary interface for interacting with Claude models. It handles:
//   - Request validation and format conversion
//   - Streaming SSE response processing
//   - Automatic retries with exponential backoff
//   - Tool call detection and streaming
//   - Context cancellation
//   - Error handling
//
// The method returns immediately with a channel that will receive completion chunks
// as they arrive. The channel is closed when the stream completes or encounters an error.
//
// Request Processing:
//  1. Converts internal message format to Anthropic API format
//  2. Initializes streaming request with retry logic
//  3. Spawns goroutine to process SSE events
//  4. Returns channel for consuming chunks
//
// Streaming Behavior:
// - Chunks arrive in real-time as tokens are generated
// - Text chunks contain partial response text
// - ToolCall chunks contain complete tool invocation details
// - Final chunk has Done=true
// - Error chunk has Error field set and Done=true
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
//   - "anthropic: max retries exceeded": After exhausting retry attempts
//   - "anthropic: stream error": Server-side streaming failures
//   - context.Canceled: When context is cancelled
//   - context.DeadlineExceeded: When context times out
//
// Example - Basic Usage:
//
//	req := &agent.CompletionRequest{
//	    Model:     "claude-sonnet-4-20250514",
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
//
// Example - With Tools:
//
//	chunks, err := provider.Complete(ctx, &agent.CompletionRequest{
//	    Model:    "claude-sonnet-4-20250514",
//	    Messages: []agent.CompletionMessage{{Role: "user", Content: "What's 2+2?"}},
//	    Tools:    []agent.Tool{calculatorTool},
//	})
//
//	for chunk := range chunks {
//	    if chunk.ToolCall != nil {
//	        // Execute the requested tool
//	        result := executeCalculator(chunk.ToolCall.Input)
//	        // Send result back in next request...
//	    }
//	}
//
// Example - With Timeout:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	chunks, err := provider.Complete(ctx, req)
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        if errors.Is(chunk.Error, context.DeadlineExceeded) {
//	            log.Println("Request timed out after 30 seconds")
//	        }
//	        break
//	    }
//	    // Process chunks...
//	}
func (p *AnthropicProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	chunks := make(chan *agent.CompletionChunk)

	go func() {
		defer close(chunks)

		useBeta := p.hasComputerUse(req.Tools)
		var betaTools []anthropic.BetaToolUnionParam
		var betaErr error
		if useBeta {
			betaTools, betaErr = p.convertToolsBeta(req.Tools)
			if betaErr != nil {
				chunks <- &agent.CompletionChunk{Error: fmt.Errorf("anthropic: failed to convert tools: %w", betaErr)}
				return
			}
		}

		// Convert request to Anthropic format with retries
		var stream *ssestream.Stream[anthropic.MessageStreamEventUnion]
		var betaStream *ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion]
		var err error

		// Retry loop with exponential backoff for transient failures
		for attempt := 0; attempt <= p.maxRetries; attempt++ {
			if useBeta {
				betaStream, err = p.createBetaStream(ctx, req, betaTools)
			} else {
				stream, err = p.createStream(ctx, req)
			}
			if err == nil {
				break
			}

			// Check if error is retryable (rate limits, server errors, etc.)
			wrappedErr := p.wrapError(err, p.getModel(req.Model))
			if !p.isRetryableError(wrappedErr) {
				chunks <- &agent.CompletionChunk{Error: wrappedErr}
				return
			}

			// Exponential backoff: delay = baseDelay * 2^attempt
			// Example with 1s base: 1s, 2s, 4s, 8s
			if attempt < p.maxRetries {
				backoff := p.retryDelay * time.Duration(math.Pow(2, float64(attempt)))
				select {
				case <-ctx.Done():
					// Context cancelled or timed out during retry
					chunks <- &agent.CompletionChunk{Error: ctx.Err()}
					return
				case <-time.After(backoff):
					// Wait for backoff period before next retry
					continue
				}
			}
		}

		if err != nil {
			chunks <- &agent.CompletionChunk{Error: fmt.Errorf("anthropic: max retries exceeded: %w", p.wrapError(err, p.getModel(req.Model)))}
			return
		}

		// Process streaming events and send chunks to channel
		if useBeta {
			p.processBetaStream(betaStream, chunks, p.getModel(req.Model))
		} else {
			p.processStream(stream, chunks, p.getModel(req.Model))
		}
	}()

	return chunks, nil
}

// createStream creates an Anthropic streaming request from a completion request.
//
// This internal method handles the conversion from our internal request format
// to Anthropic's API format, including:
//   - Message format conversion (user/assistant/tool roles)
//   - System prompt configuration
//   - Tool definitions
//   - Model and token limits
//
// The method builds an Anthropic MessageNewParams and initiates a streaming
// request using the official SDK.
//
// Parameters:
//   - ctx: Context for cancellation
//   - req: Internal completion request
//
// Returns:
//   - *ssestream.Stream: Anthropic SSE stream for processing events
//   - error: Returns error if message/tool conversion fails
//
// Errors:
//   - "anthropic: failed to convert messages": Message format is invalid
//   - "anthropic: failed to convert tools": Tool schema is invalid
func (p *AnthropicProvider) createStream(ctx context.Context, req *agent.CompletionRequest) (*ssestream.Stream[anthropic.MessageStreamEventUnion], error) {
	// Convert messages
	messages, err := p.convertMessages(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to convert messages: %w", err)
	}

	// Build Anthropic API parameters
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.getModel(req.Model)),
		Messages:  messages,
		MaxTokens: int64(p.getMaxTokens(req.MaxTokens)),
	}

	// Add system prompt if provided (separate from messages in Anthropic API)
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{
			{
				Type: "text",
				Text: req.System,
			},
		}
	}

	// Add tool definitions if provided
	if len(req.Tools) > 0 {
		tools, err := p.convertTools(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("anthropic: failed to convert tools: %w", err)
		}
		params.Tools = tools
	}

	// Enable extended thinking if requested
	if req.EnableThinking {
		budgetTokens := int64(req.ThinkingBudgetTokens)
		if budgetTokens < 1024 {
			budgetTokens = 10000 // Default budget if not specified or too low
		}
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budgetTokens)
	}

	// Create streaming request using Anthropic SDK
	stream := p.client.Messages.NewStreaming(ctx, params)

	return stream, nil
}

// createBetaStream creates a beta Anthropic streaming request for computer use tools.
func (p *AnthropicProvider) createBetaStream(ctx context.Context, req *agent.CompletionRequest, tools []anthropic.BetaToolUnionParam) (*ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion], error) {
	// Convert messages to beta format
	messages, err := p.convertMessagesBeta(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to convert messages: %w", err)
	}

	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(p.getModel(req.Model)),
		Messages:  messages,
		MaxTokens: int64(p.getMaxTokens(req.MaxTokens)),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaComputerUse2025_01_24},
	}

	if req.System != "" {
		params.System = []anthropic.BetaTextBlockParam{
			{
				Type: "text",
				Text: req.System,
			},
		}
	}

	if len(tools) > 0 {
		params.Tools = tools
	}

	if req.EnableThinking {
		budgetTokens := int64(req.ThinkingBudgetTokens)
		if budgetTokens < 1024 {
			budgetTokens = 10000
		}
		params.Thinking = anthropic.BetaThinkingConfigParamOfEnabled(budgetTokens)
	}

	stream := p.client.Beta.Messages.NewStreaming(ctx, params)
	return stream, nil
}

// maxEmptyStreamEvents is the maximum number of consecutive empty events before
// treating the stream as malformed. This protects against streams that flood with
// empty events, which could otherwise cause excessive CPU usage and memory pressure.
// Based on patterns from sashabaranov/go-openai stream_reader implementation.
const maxEmptyStreamEvents = 300

// processStream processes Server-Sent Events from Anthropic's streaming API.
//
// This method consumes the SSE stream and converts Anthropic's event format into
// our internal CompletionChunk format. It handles multiple event types and manages
// the stateful accumulation of tool calls across events.
//
// Event Processing:
//   - content_block_start: Initializes new content blocks (text or tool use)
//   - content_block_delta: Streams incremental text or tool input JSON
//   - content_block_stop: Finalizes complete content blocks
//   - message_stop: Signals end of response
//   - error: Propagates API errors
//
// Tool Call Handling:
// Tool calls arrive in multiple events:
//  1. content_block_start with ToolUseBlock (contains ID and name)
//  2. Multiple content_block_delta events with partial JSON (streamed arguments)
//  3. content_block_stop to finalize the tool call
//
// The method accumulates tool input across delta events before sending the
// complete tool call chunk.
//
// Stream Health Protection:
// The method tracks consecutive empty events to detect malformed streams.
// If maxEmptyStreamEvents consecutive empty events occur, the stream is
// terminated with an error to prevent resource exhaustion.
//
// Parameters:
//   - stream: Anthropic SSE stream to consume
//   - chunks: Channel to send converted chunks to (will not be closed by this method)
//
// Chunk Emissions:
//   - Text chunks: Emitted for each text delta (real-time streaming)
//   - Tool call chunks: Emitted when tool call is complete (after block_stop)
//   - Done chunk: Emitted on message_stop
//   - Error chunk: Emitted on stream errors
func (p *AnthropicProvider) processStream(stream *ssestream.Stream[anthropic.MessageStreamEventUnion], chunks chan<- *agent.CompletionChunk, model string) {
	var currentToolCall *models.ToolCall
	var currentToolInput strings.Builder
	emptyEventCount := 0     // Track consecutive empty events for malformed stream detection
	inThinkingBlock := false // Track if we're currently in a thinking block

	// Track token usage across the stream
	var inputTokens int
	var outputTokens int

	// Track current tool call being assembled across multiple events
	for stream.Next() {
		event := stream.Current()
		eventProcessed := false // Track if this event produced meaningful output

		switch event.Type {
		case "message_start":
			// Extract input tokens from message_start event
			messageStart := event.AsMessageStart()
			if messageStart.Message.Usage.InputTokens > 0 {
				inputTokens = int(messageStart.Message.Usage.InputTokens)
			}
			eventProcessed = true

		case "content_block_start":
			// New content block starting (could be text, tool use, or thinking)
			contentBlockStart := event.AsContentBlockStart()
			contentBlock := contentBlockStart.ContentBlock

			// Check block type
			switch contentBlock.Type {
			case "thinking":
				// Start of a thinking block
				inThinkingBlock = true
				chunks <- &agent.CompletionChunk{
					ThinkingStart: true,
				}
				eventProcessed = true

			case "tool_use":
				// Initialize new tool call with ID and name
				toolUse := contentBlock.AsToolUse()
				currentToolCall = &models.ToolCall{
					ID:   toolUse.ID,
					Name: toolUse.Name,
				}
				currentToolInput.Reset()
				eventProcessed = true
			}

		case "content_block_delta":
			// Incremental content updates
			contentBlockDelta := event.AsContentBlockDelta()
			delta := contentBlockDelta.Delta

			// Handle different delta types
			switch delta.Type {
			case "text_delta":
				// Text delta - emit immediately for real-time streaming
				if delta.Text != "" {
					chunks <- &agent.CompletionChunk{
						Text: delta.Text,
					}
					eventProcessed = true
				}

			case "thinking_delta":
				// Thinking delta - emit thinking content
				if delta.Thinking != "" {
					chunks <- &agent.CompletionChunk{
						Thinking: delta.Thinking,
					}
					eventProcessed = true
				}

			case "input_json_delta":
				// Tool input delta - accumulate JSON fragments
				if delta.PartialJSON != "" {
					currentToolInput.WriteString(delta.PartialJSON)
					eventProcessed = true
				}
			}

		case "content_block_stop":
			// Content block complete
			if inThinkingBlock {
				// End of thinking block
				chunks <- &agent.CompletionChunk{
					ThinkingEnd: true,
				}
				inThinkingBlock = false
				eventProcessed = true
			} else if currentToolCall != nil {
				// Finalize tool call
				currentToolCall.Input = json.RawMessage(currentToolInput.String())
				chunks <- &agent.CompletionChunk{
					ToolCall: currentToolCall,
				}
				currentToolCall = nil
				eventProcessed = true
			}

		case "message_delta":
			// Extract output tokens from message_delta event (final usage)
			messageDelta := event.AsMessageDelta()
			if messageDelta.Usage.OutputTokens > 0 {
				outputTokens = int(messageDelta.Usage.OutputTokens)
			}
			eventProcessed = true

		case "message_stop":
			// Stream complete successfully - include token counts
			chunks <- &agent.CompletionChunk{
				Done:         true,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			}
			return // Exit immediately on successful completion

		case "error":
			// Server-side error during streaming
			chunks <- &agent.CompletionChunk{
				Error: p.wrapError(errors.New("anthropic stream error"), model),
			}
			return // Exit immediately on error
		}

		// Malformed stream protection: track consecutive empty events
		if eventProcessed {
			emptyEventCount = 0
		} else {
			emptyEventCount++
			if emptyEventCount >= maxEmptyStreamEvents {
				chunks <- &agent.CompletionChunk{
					Error: p.wrapError(
						fmt.Errorf("stream appears malformed: received %d consecutive empty events", emptyEventCount),
						model,
					),
				}
				return
			}
		}
	}

	// Check for errors that occurred during stream iteration
	if err := stream.Err(); err != nil {
		chunks <- &agent.CompletionChunk{
			Error: p.wrapError(err, model),
		}
	}
}

// processBetaStream processes Server-Sent Events from Anthropic's beta streaming API.
func (p *AnthropicProvider) processBetaStream(stream *ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion], chunks chan<- *agent.CompletionChunk, model string) {
	var currentToolCall *models.ToolCall
	var currentToolInput strings.Builder
	emptyEventCount := 0
	inThinkingBlock := false

	var inputTokens int
	var outputTokens int

	for stream.Next() {
		event := stream.Current()
		eventProcessed := false

		switch event.Type {
		case "message_start":
			messageStart := event.AsMessageStart()
			if messageStart.Message.Usage.InputTokens > 0 {
				inputTokens = int(messageStart.Message.Usage.InputTokens)
			}
			eventProcessed = true

		case "content_block_start":
			contentBlockStart := event.AsContentBlockStart()
			contentBlock := contentBlockStart.ContentBlock
			switch contentBlock.Type {
			case "thinking":
				inThinkingBlock = true
				chunks <- &agent.CompletionChunk{ThinkingStart: true}
				eventProcessed = true
			case "tool_use":
				toolUse := contentBlock.AsToolUse()
				currentToolCall = &models.ToolCall{
					ID:   toolUse.ID,
					Name: toolUse.Name,
				}
				currentToolInput.Reset()
				eventProcessed = true
			}

		case "content_block_delta":
			contentBlockDelta := event.AsContentBlockDelta()
			delta := contentBlockDelta.Delta
			switch delta.Type {
			case "text_delta":
				if delta.Text != "" {
					chunks <- &agent.CompletionChunk{Text: delta.Text}
					eventProcessed = true
				}
			case "thinking_delta":
				if delta.Thinking != "" {
					chunks <- &agent.CompletionChunk{Thinking: delta.Thinking}
					eventProcessed = true
				}
			case "input_json_delta":
				if delta.PartialJSON != "" {
					currentToolInput.WriteString(delta.PartialJSON)
					eventProcessed = true
				}
			}

		case "content_block_stop":
			if inThinkingBlock {
				chunks <- &agent.CompletionChunk{ThinkingEnd: true}
				inThinkingBlock = false
				eventProcessed = true
			} else if currentToolCall != nil {
				currentToolCall.Input = json.RawMessage(currentToolInput.String())
				chunks <- &agent.CompletionChunk{ToolCall: currentToolCall}
				currentToolCall = nil
				eventProcessed = true
			}

		case "message_delta":
			messageDelta := event.AsMessageDelta()
			if messageDelta.Usage.OutputTokens > 0 {
				outputTokens = int(messageDelta.Usage.OutputTokens)
			}
			eventProcessed = true

		case "message_stop":
			chunks <- &agent.CompletionChunk{
				Done:         true,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			}
			return

		case "error":
			chunks <- &agent.CompletionChunk{
				Error: p.wrapError(errors.New("anthropic stream error"), model),
			}
			return
		}

		if eventProcessed {
			emptyEventCount = 0
		} else {
			emptyEventCount++
			if emptyEventCount >= maxEmptyStreamEvents {
				chunks <- &agent.CompletionChunk{
					Error: p.wrapError(
						fmt.Errorf("stream appears malformed: received %d consecutive empty events", emptyEventCount),
						model,
					),
				}
				return
			}
		}
	}

	if err := stream.Err(); err != nil {
		chunks <- &agent.CompletionChunk{
			Error: p.wrapError(err, model),
		}
	}
}

// convertMessages converts internal message format to Anthropic API format.
//
// This method handles the translation between our unified message format and
// Anthropic's specific requirements:
//   - System messages are filtered out (handled separately via params.System)
//   - User and assistant messages are converted with content blocks
//   - Tool calls and tool results are converted to Anthropic's format
//   - Multiple content types per message are supported
//
// Message Format Differences:
//   - Internal: Separate fields for Content, ToolCalls, ToolResults
//   - Anthropic: Everything is content blocks in ContentBlockParamUnion array
//
// Parameters:
//   - messages: Internal message format from CompletionRequest
//
// Returns:
//   - []anthropic.MessageParam: Anthropic-formatted messages
//   - error: Returns error if tool call input JSON is invalid
//
// Example Conversion:
//
//	Internal message with tool call:
//	  {Role: "assistant", ToolCalls: [{ID: "1", Name: "search", Input: {"q":"test"}}]}
//
//	Converts to Anthropic format:
//	  anthropic.NewAssistantMessage(
//	      anthropic.NewToolUseBlock("1", map[string]any{"q":"test"}, "search")
//	  )
func (p *AnthropicProvider) convertMessages(messages []agent.CompletionMessage) ([]anthropic.MessageParam, error) {
	var result []anthropic.MessageParam

	for _, msg := range messages {
		// Skip system messages - they're handled separately in params.System
		if msg.Role == "system" {
			continue
		}

		// Build content blocks array (Anthropic uses array of content blocks)
		var content []anthropic.ContentBlockParamUnion

		// Add text content if present
		if msg.Content != "" {
			content = append(content, anthropic.NewTextBlock(msg.Content))
		}

		// Add tool results (responses from previously executed tools)
		for _, toolResult := range msg.ToolResults {
			content = append(content, anthropic.NewToolResultBlock(
				toolResult.ToolCallID,
				toolResult.Content,
				toolResult.IsError,
			))
		}

		// Add tool calls (for assistant messages requesting tool execution)
		for _, toolCall := range msg.ToolCalls {
			// Parse JSON input to map for Anthropic's format
			var input map[string]interface{}
			if err := json.Unmarshal(toolCall.Input, &input); err != nil {
				return nil, fmt.Errorf("invalid tool call input: %w", err)
			}

			content = append(content, anthropic.NewToolUseBlock(
				toolCall.ID,
				input,
				toolCall.Name,
			))
		}

		// Create message with appropriate role
		var message anthropic.MessageParam
		if msg.Role == "assistant" {
			message = anthropic.NewAssistantMessage(content...)
		} else {
			// User or tool role both map to user messages in Anthropic
			message = anthropic.NewUserMessage(content...)
		}

		result = append(result, message)
	}

	return result, nil
}

func (p *AnthropicProvider) hasComputerUse(tools []agent.Tool) bool {
	for _, tool := range tools {
		if provider, ok := tool.(agent.ComputerUseConfigProvider); ok {
			if provider.ComputerUseConfig() != nil {
				return true
			}
		}
	}
	return false
}

// convertMessagesBeta converts internal messages to Anthropic beta message format.
func (p *AnthropicProvider) convertMessagesBeta(messages []agent.CompletionMessage) ([]anthropic.BetaMessageParam, error) {
	var result []anthropic.BetaMessageParam

	for _, msg := range messages {
		if msg.Role == "system" {
			continue
		}

		var content []anthropic.BetaContentBlockParamUnion

		if msg.Content != "" {
			content = append(content, anthropic.NewBetaTextBlock(msg.Content))
		}

		content = append(content, betaAttachmentBlocks(msg.Attachments)...)

		for _, toolResult := range msg.ToolResults {
			toolBlock := anthropic.BetaToolResultBlockParam{
				ToolUseID: toolResult.ToolCallID,
			}
			if toolResult.IsError {
				toolBlock.IsError = anthropic.Bool(true)
			}

			var toolContent []anthropic.BetaToolResultBlockParamContentUnion
			if toolResult.Content != "" {
				toolContent = append(toolContent, anthropic.BetaToolResultBlockParamContentUnion{
					OfText: &anthropic.BetaTextBlockParam{Text: toolResult.Content},
				})
			}
			for _, attachment := range toolResult.Attachments {
				if img := betaImageBlockFromAttachment(attachment); img != nil {
					toolContent = append(toolContent, anthropic.BetaToolResultBlockParamContentUnion{
						OfImage: img,
					})
				}
			}
			if len(toolContent) > 0 {
				toolBlock.Content = toolContent
			}

			content = append(content, anthropic.BetaContentBlockParamUnion{
				OfToolResult: &toolBlock,
			})
		}

		for _, toolCall := range msg.ToolCalls {
			var input map[string]interface{}
			if err := json.Unmarshal(toolCall.Input, &input); err != nil {
				return nil, fmt.Errorf("invalid tool call input: %w", err)
			}
			content = append(content, anthropic.NewBetaToolUseBlock(
				toolCall.ID,
				input,
				toolCall.Name,
			))
		}

		role := anthropic.BetaMessageParamRoleUser
		if msg.Role == "assistant" {
			role = anthropic.BetaMessageParamRoleAssistant
		}
		result = append(result, anthropic.BetaMessageParam{
			Role:    role,
			Content: content,
		})
	}

	return result, nil
}

func betaAttachmentBlocks(attachments []models.Attachment) []anthropic.BetaContentBlockParamUnion {
	if len(attachments) == 0 {
		return nil
	}
	var blocks []anthropic.BetaContentBlockParamUnion
	for _, attachment := range attachments {
		if img := betaImageBlockFromAttachment(attachment); img != nil {
			blocks = append(blocks, anthropic.BetaContentBlockParamUnion{OfImage: img})
		}
	}
	return blocks
}

func betaImageBlockFromAttachment(att models.Attachment) *anthropic.BetaImageBlockParam {
	if att.Type != "image" && !strings.HasPrefix(att.MimeType, "image/") {
		return nil
	}
	if mediaType, data, ok := parseDataURL(att.URL); ok {
		mt, ok := betaMediaType(mediaType)
		if !ok {
			return nil
		}
		return &anthropic.BetaImageBlockParam{
			Source: anthropic.BetaImageBlockParamSourceUnion{
				OfBase64: &anthropic.BetaBase64ImageSourceParam{
					Data:      data,
					MediaType: mt,
				},
			},
		}
	}
	if att.URL != "" {
		return &anthropic.BetaImageBlockParam{
			Source: anthropic.BetaImageBlockParamSourceUnion{
				OfURL: &anthropic.BetaURLImageSourceParam{URL: att.URL},
			},
		}
	}
	return nil
}

func betaMediaType(mediaType string) (anthropic.BetaBase64ImageSourceMediaType, bool) {
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/jpg":
		return anthropic.BetaBase64ImageSourceMediaTypeImageJPEG, true
	case "image/png":
		return anthropic.BetaBase64ImageSourceMediaTypeImagePNG, true
	case "image/gif":
		return anthropic.BetaBase64ImageSourceMediaTypeImageGIF, true
	case "image/webp":
		return anthropic.BetaBase64ImageSourceMediaTypeImageWebP, true
	default:
		return "", false
	}
}

func parseDataURL(raw string) (string, string, bool) {
	if !strings.HasPrefix(raw, "data:") {
		return "", "", false
	}
	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	meta := strings.TrimPrefix(parts[0], "data:")
	if !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	mediaType := strings.TrimSuffix(meta, ";base64")
	if mediaType == "" {
		return "", "", false
	}
	return mediaType, parts[1], true
}

// convertTools converts internal tool definitions to Anthropic API format.
//
// This method translates tool definitions from our internal format to Anthropic's
// tool schema. Each tool includes:
//   - Name: Function identifier for the LLM
//   - Description: Natural language description of what the tool does
//   - Input schema: JSON Schema defining required/optional parameters
//
// Parameters:
//   - tools: Internal tool definitions implementing agent.Tool interface
//
// Returns:
//   - []anthropic.ToolUnionParam: Anthropic-formatted tool definitions
//   - error: Returns error if tool schema JSON is invalid
//
// Errors:
//   - "invalid tool schema for {name}": When tool.Schema() returns invalid JSON
//
// Example:
//
//	Internal tool:
//	  Name: "calculator"
//	  Description: "Performs basic arithmetic"
//	  Schema: {"type":"object","properties":{"operation":{"type":"string"}}}
//
//	Converts to Anthropic tool definition with same name, description, and schema.
func (p *AnthropicProvider) convertTools(tools []agent.Tool) ([]anthropic.ToolUnionParam, error) {
	var result []anthropic.ToolUnionParam

	for _, tool := range tools {
		// Parse JSON schema into Anthropic's schema format
		var schema anthropic.ToolInputSchemaParam
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			return nil, fmt.Errorf("invalid tool schema for %s: %w", tool.Name(), err)
		}

		// Create tool parameter with schema and name
		toolParam := anthropic.ToolUnionParamOfTool(schema, tool.Name())

		// Set description if we can access the underlying ToolParam
		if toolParam.OfTool == nil {
			return nil, fmt.Errorf("invalid tool schema for %s: missing tool definition", tool.Name())
		}
		toolParam.OfTool.Description = anthropic.String(tool.Description())

		result = append(result, toolParam)
	}

	return result, nil
}

// convertToolsBeta converts internal tool definitions to Anthropic beta tool format.
func (p *AnthropicProvider) convertToolsBeta(tools []agent.Tool) ([]anthropic.BetaToolUnionParam, error) {
	var result []anthropic.BetaToolUnionParam
	computerUseAdded := false

	for _, tool := range tools {
		if provider, ok := tool.(agent.ComputerUseConfigProvider); ok && !computerUseAdded {
			if cfg := provider.ComputerUseConfig(); cfg != nil && cfg.DisplayWidthPx > 0 && cfg.DisplayHeightPx > 0 {
				param := anthropic.BetaToolUnionParamOfComputerUseTool20250124(int64(cfg.DisplayHeightPx), int64(cfg.DisplayWidthPx))
				if param.OfComputerUseTool20250124 != nil && cfg.DisplayNumber > 0 {
					param.OfComputerUseTool20250124.DisplayNumber = anthropic.Int(int64(cfg.DisplayNumber))
				}
				result = append(result, param)
				computerUseAdded = true
				continue
			}
		}

		var schema anthropic.BetaToolInputSchemaParam
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			return nil, fmt.Errorf("invalid tool schema for %s: %w", tool.Name(), err)
		}

		toolParam := anthropic.BetaToolUnionParamOfTool(schema, tool.Name())
		if toolParam.OfTool == nil {
			return nil, fmt.Errorf("invalid tool schema for %s: missing tool definition", tool.Name())
		}
		toolParam.OfTool.Description = anthropic.String(tool.Description())
		result = append(result, toolParam)
	}

	return result, nil
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
func (p *AnthropicProvider) getModel(model string) string {
	if model == "" {
		return p.defaultModel
	}
	return model
}

// getMaxTokens returns the maximum tokens to generate for the request.
//
// If the request specifies max tokens, that value is used. Otherwise, returns
// a sensible default of 4096 tokens. This prevents runaway generations while
// allowing substantial responses.
//
// Parameters:
//   - maxTokens: Max tokens from CompletionRequest (may be 0)
//
// Returns:
//   - int: Max tokens to use (default 4096)
func (p *AnthropicProvider) getMaxTokens(maxTokens int) int {
	if maxTokens <= 0 {
		return 4096
	}
	return maxTokens
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
//   - Network: "connection reset", "connection refused", "no such host"
//
// Non-Retryable Errors:
//   - Authentication: 401, 403 (invalid API key)
//   - Validation: 400 (bad request format)
//   - Not found: 404 (invalid endpoint)
//
// Parameters:
//   - err: Error to classify
//
// Returns:
//   - bool: true if error should be retried, false otherwise
//
// Example:
//
//	err := doAPICall()
//	if isRetryableError(err) {
//	    time.Sleep(backoff)
//	    err = doAPICall() // Retry
//	}
func (p *AnthropicProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if providerErr, ok := GetProviderError(err); ok {
		return providerErr.Reason.IsRetryable()
	}

	errMsg := err.Error()

	// Rate limit errors - API is throttling requests
	if strings.Contains(errMsg, "rate_limit") ||
		strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "too many requests") {
		return true
	}

	// Server errors (5xx) - temporary Anthropic infrastructure issues
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

	// Timeout errors - request took too long
	if strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "deadline exceeded") {
		return true
	}

	// Connection errors - network connectivity issues
	if strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "no such host") {
		return true
	}

	return false
}

type anthropicErrorPayload struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

func (p *AnthropicProvider) wrapError(err error, model string) error {
	if err == nil {
		return nil
	}
	if IsProviderError(err) {
		return err
	}

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		providerErr := &ProviderError{
			Provider: "anthropic",
			Model:    model,
			Cause:    err,
			Reason:   FailoverUnknown,
		}
		providerErr = providerErr.WithStatus(apiErr.StatusCode)

		message := ""
		code := ""
		requestID := apiErr.RequestID

		raw := apiErr.RawJSON()
		if raw != "" {
			var payload anthropicErrorPayload
			if json.Unmarshal([]byte(raw), &payload) == nil {
				if payload.Error.Message != "" {
					message = payload.Error.Message
				}
				if payload.Error.Type != "" {
					code = payload.Error.Type
				}
				if payload.RequestID != "" {
					requestID = payload.RequestID
				}
			}
		}

		if message != "" {
			providerErr = providerErr.WithMessage(message)
		} else if providerErr.Message == "" {
			providerErr.Message = "anthropic request failed"
		}
		if code != "" {
			providerErr = providerErr.WithCode(code)
		}
		if requestID != "" {
			providerErr = providerErr.WithRequestID(requestID)
		}
		return providerErr
	}

	return NewProviderError("anthropic", model, err)
}

// CountTokens estimates the token count for a completion request.
//
// This provides a rough approximation using character-based estimation rather
// than actual tokenization. The estimate uses ~4 characters per token, which
// is typical for English text with Claude's tokenizer.
//
// What's Counted:
//   - System prompt
//   - Message content (all messages)
//   - Message roles
//   - Tool call names and arguments
//   - Tool result content
//   - Tool definitions (names, descriptions, schemas)
//
// Accuracy:
// This is a rough estimate and may differ from actual token count by 10-20%.
// For precise counting, use Anthropic's official tokenizer API. This estimate
// is useful for:
//   - Checking if request fits within context window
//   - Estimating API costs before sending
//   - Debugging context overflow issues
//
// Parameters:
//   - req: Completion request to estimate tokens for
//
// Returns:
//   - int: Estimated token count
//
// Example:
//
//	tokens := provider.CountTokens(req)
//	if tokens > 190000 {
//	    return fmt.Errorf("request too large: %d tokens (max 200K)", tokens)
//	}
func (p *AnthropicProvider) CountTokens(req *agent.CompletionRequest) int {
	// Simple character-based estimation: ~4 chars per token
	total := 0

	// Count system prompt tokens
	total += len(req.System) / 4

	// Count message content and metadata
	for _, msg := range req.Messages {
		total += len(msg.Content) / 4
		total += len(msg.Role) / 4

		// Count tool calls (name + JSON arguments)
		for _, tc := range msg.ToolCalls {
			total += len(tc.Name) / 4
			total += len(tc.Input) / 4
		}

		// Count tool results
		for _, tr := range msg.ToolResults {
			total += len(tr.Content) / 4
		}
	}

	// Count tool definitions (name + description + JSON schema)
	for _, tool := range req.Tools {
		total += len(tool.Name()) / 4
		total += len(tool.Description()) / 4
		total += len(tool.Schema()) / 4
	}

	return total
}

// ParseSSEStream is a utility function for manually parsing Server-Sent Events.
//
// This function provides a low-level SSE parser for cases where you need to handle
// SSE streams directly without using the Anthropic SDK. It's useful for:
//   - Custom streaming implementations
//   - Debugging SSE issues
//   - Proxying or transforming SSE streams
//
// SSE Format:
// SSE uses text-based format with events separated by blank lines:
//
//	event: message_start
//	data: {"type":"message_start","message":{...}}
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","delta":{...}}
//
// The parser calls the handler function for each complete event with:
//   - eventType: Value from "event:" line (or empty for default events)
//   - data: Combined value from all "data:" lines (joined with \n)
//
// Parameters:
//   - reader: io.Reader containing SSE stream
//   - handler: Callback function to process each event
//
// Returns:
//   - error: Returns error if handler returns error or scanner fails
//
// Example:
//
//	err := ParseSSEStream(response.Body, func(eventType, data string) error {
//	    switch eventType {
//	    case "message_start":
//	        fmt.Println("Stream starting")
//	    case "content_block_delta":
//	        var delta ContentDelta
//	        json.Unmarshal([]byte(data), &delta)
//	        fmt.Print(delta.Text)
//	    case "message_stop":
//	        fmt.Println("\nStream complete")
//	    }
//	    return nil
//	})
//
// Note: Most users should use the Anthropic SDK's built-in streaming rather
// than this low-level parser. This is exported for advanced use cases only.
func ParseSSEStream(reader io.Reader, handler func(eventType, data string) error) error {
	scanner := bufio.NewScanner(reader)
	var eventType string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line signals end of event - process accumulated data
		if line == "" {
			if eventType != "" || len(dataLines) > 0 {
				// Join multi-line data with newlines
				data := strings.Join(dataLines, "\n")
				if err := handler(eventType, data); err != nil {
					return err
				}
				// Reset for next event
				eventType = ""
				dataLines = nil
			}
			continue
		}

		// Parse event type line
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			// Parse data line (may be multiple per event)
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataLines = append(dataLines, data)
		}
		// Ignore other line types (comments starting with :, id:, retry:)
	}

	return scanner.Err()
}
