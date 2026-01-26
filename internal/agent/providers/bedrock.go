package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/toolconv"
	"github.com/haasonsaas/nexus/pkg/models"
)

// BedrockProvider implements the agent.LLMProvider interface for AWS Bedrock.
// It provides access to foundation models hosted on AWS including Anthropic Claude,
// Amazon Titan, Meta Llama, and more.
//
// Bedrock uses the AWS SDK and supports streaming via ConverseStream API.
// Authentication is handled via AWS credentials (environment, IAM role, or explicit).
//
// Thread Safety:
// BedrockProvider is safe for concurrent use across multiple goroutines.
type BedrockProvider struct {
	client       *bedrockruntime.Client
	defaultModel string
	maxRetries   int
	retryDelay   time.Duration
	region       string
}

// BedrockConfig holds configuration for the Bedrock provider.
type BedrockConfig struct {
	// Region is the AWS region (default: us-east-1)
	Region string

	// AccessKeyID for explicit credentials (optional, uses default chain if empty)
	AccessKeyID string

	// SecretAccessKey for explicit credentials (optional)
	SecretAccessKey string

	// SessionToken for temporary credentials (optional)
	SessionToken string

	// DefaultModel is the model to use when not specified (default: anthropic.claude-3-sonnet-20240229-v1:0)
	DefaultModel string

	// MaxRetries for transient failures (default: 3)
	MaxRetries int

	// RetryDelay base delay between retries (default: 1s)
	RetryDelay time.Duration
}

// NewBedrockProvider creates a new AWS Bedrock provider instance.
//
// Parameters:
//   - cfg: BedrockConfig with optional AWS credentials and settings
//
// Returns:
//   - *BedrockProvider: Configured provider instance
//   - error: Returns error if AWS client creation fails
//
// Example with default credentials:
//
//	provider, err := NewBedrockProvider(BedrockConfig{
//	    Region: "us-east-1",
//	})
//
// Example with explicit credentials:
//
//	provider, err := NewBedrockProvider(BedrockConfig{
//	    Region:          "us-west-2",
//	    AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
//	    SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
//	})
func NewBedrockProvider(cfg BedrockConfig) (*BedrockProvider, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}

	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "anthropic.claude-3-sonnet-20240229-v1:0"
	}

	// Configure AWS SDK
	var awsCfg aws.Config
	var err error

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		// Use explicit credentials
		awsCfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithRegion(cfg.Region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID,
				cfg.SecretAccessKey,
				cfg.SessionToken,
			)),
		)
	} else {
		// Use default credential chain (env, IAM role, etc.)
		awsCfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithRegion(cfg.Region),
		)
	}

	if err != nil {
		return nil, fmt.Errorf("bedrock: failed to load AWS config: %w", err)
	}

	client := bedrockruntime.NewFromConfig(awsCfg)

	return &BedrockProvider{
		client:       client,
		defaultModel: cfg.DefaultModel,
		maxRetries:   cfg.MaxRetries,
		retryDelay:   cfg.RetryDelay,
		region:       cfg.Region,
	}, nil
}

// Name returns the provider identifier.
func (p *BedrockProvider) Name() string {
	return "bedrock"
}

// Models returns the list of available models on Bedrock.
// Note: Actual availability depends on your AWS account's model access.
func (p *BedrockProvider) Models() []agent.Model {
	return []agent.Model{
		// Anthropic Claude models
		{ID: "anthropic.claude-3-opus-20240229-v1:0", Name: "Claude 3 Opus (Bedrock)", ContextSize: 200000, SupportsVision: true},
		{ID: "anthropic.claude-3-sonnet-20240229-v1:0", Name: "Claude 3 Sonnet (Bedrock)", ContextSize: 200000, SupportsVision: true},
		{ID: "anthropic.claude-3-haiku-20240307-v1:0", Name: "Claude 3 Haiku (Bedrock)", ContextSize: 200000, SupportsVision: true},
		{ID: "anthropic.claude-v2:1", Name: "Claude 2.1 (Bedrock)", ContextSize: 200000, SupportsVision: false},
		// Amazon Titan models
		{ID: "amazon.titan-text-express-v1", Name: "Titan Text Express", ContextSize: 8192, SupportsVision: false},
		{ID: "amazon.titan-text-lite-v1", Name: "Titan Text Lite", ContextSize: 4096, SupportsVision: false},
		// Meta Llama models
		{ID: "meta.llama3-70b-instruct-v1:0", Name: "Llama 3 70B (Bedrock)", ContextSize: 8192, SupportsVision: false},
		{ID: "meta.llama3-8b-instruct-v1:0", Name: "Llama 3 8B (Bedrock)", ContextSize: 8192, SupportsVision: false},
		// Mistral models
		{ID: "mistral.mixtral-8x7b-instruct-v0:1", Name: "Mixtral 8x7B (Bedrock)", ContextSize: 32768, SupportsVision: false},
		{ID: "mistral.mistral-7b-instruct-v0:2", Name: "Mistral 7B (Bedrock)", ContextSize: 32768, SupportsVision: false},
		// Cohere models
		{ID: "cohere.command-r-plus-v1:0", Name: "Command R+ (Bedrock)", ContextSize: 128000, SupportsVision: false},
		{ID: "cohere.command-r-v1:0", Name: "Command R (Bedrock)", ContextSize: 128000, SupportsVision: false},
	}
}

// SupportsTools indicates whether this provider supports tool/function calling.
// Bedrock supports tool use via the Converse API for compatible models.
func (p *BedrockProvider) SupportsTools() bool {
	return true
}

// Complete sends a completion request to Bedrock and returns a streaming response.
func (p *BedrockProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if p.client == nil {
		return nil, NewProviderError("bedrock", req.Model, errors.New("Bedrock client not initialized"))
	}

	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	// Convert messages to Bedrock format
	messages, err := p.convertMessages(req.Messages)
	if err != nil {
		return nil, fmt.Errorf("bedrock: failed to convert messages: %w", err)
	}

	// Build Converse request
	converseReq := &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(model),
		Messages: messages,
	}

	// Add system prompt
	if req.System != "" {
		converseReq.System = []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{Value: req.System},
		}
	}

	// Add inference config
	if req.MaxTokens > 0 {
		maxTokens := min(req.MaxTokens, math.MaxInt32)
		converseReq.InferenceConfig = &types.InferenceConfiguration{
			// #nosec G115 -- bounded by min above
			MaxTokens: aws.Int32(int32(maxTokens)),
		}
	}

	// Add tool configuration
	if len(req.Tools) > 0 {
		converseReq.ToolConfig = toolconv.ToBedrockTools(req.Tools)
	}

	// Create stream with retries
	var stream *bedrockruntime.ConverseStreamOutput
	var lastErr error

	for attempt := 0; attempt < p.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(p.retryDelay * time.Duration(attempt)):
			}
		}

		stream, lastErr = p.client.ConverseStream(ctx, converseReq)
		if lastErr == nil {
			break
		}

		if !p.isRetryableError(lastErr) {
			return nil, p.wrapError(lastErr, model)
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("bedrock: max retries exceeded: %w", p.wrapError(lastErr, model))
	}

	chunks := make(chan *agent.CompletionChunk)
	go p.processStream(ctx, stream, chunks, model)

	return chunks, nil
}

// processStream processes the Bedrock streaming response.
func (p *BedrockProvider) processStream(ctx context.Context, stream *bedrockruntime.ConverseStreamOutput, chunks chan<- *agent.CompletionChunk, model string) {
	defer close(chunks)

	eventStream := stream.GetStream()
	defer eventStream.Close()

	var currentToolCall *models.ToolCall
	var toolInputBuilder strings.Builder

	eventChan := eventStream.Events()
	for {
		select {
		case <-ctx.Done():
			chunks <- &agent.CompletionChunk{Error: ctx.Err(), Done: true}
			return
		case event, ok := <-eventChan:
			if !ok {
				// Channel closed - emit any pending tool call
				if currentToolCall != nil && currentToolCall.ID != "" {
					currentToolCall.Input = json.RawMessage(toolInputBuilder.String())
					chunks <- &agent.CompletionChunk{ToolCall: currentToolCall}
				}
				// Check for stream error
				if err := eventStream.Err(); err != nil {
					chunks <- &agent.CompletionChunk{Error: p.wrapError(err, model), Done: true}
				} else {
					chunks <- &agent.CompletionChunk{Done: true}
				}
				return
			}

			// Handle different event types
			switch ev := event.(type) {
			case *types.ConverseStreamOutputMemberContentBlockStart:
				// Start of a new content block
				if toolUse, ok := ev.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
					currentToolCall = &models.ToolCall{
						ID:   aws.ToString(toolUse.Value.ToolUseId),
						Name: aws.ToString(toolUse.Value.Name),
					}
					toolInputBuilder.Reset()
				}

			case *types.ConverseStreamOutputMemberContentBlockDelta:
				// Delta content
				switch delta := ev.Value.Delta.(type) {
				case *types.ContentBlockDeltaMemberText:
					// Text delta
					if delta.Value != "" {
						chunks <- &agent.CompletionChunk{Text: delta.Value}
					}
				case *types.ContentBlockDeltaMemberToolUse:
					// Tool input delta - Input is *string
					if delta.Value.Input != nil {
						toolInputBuilder.WriteString(*delta.Value.Input)
					}
				}

			case *types.ConverseStreamOutputMemberContentBlockStop:
				// Content block complete
				if currentToolCall != nil && currentToolCall.ID != "" {
					currentToolCall.Input = json.RawMessage(toolInputBuilder.String())
					chunks <- &agent.CompletionChunk{ToolCall: currentToolCall}
					currentToolCall = nil
					toolInputBuilder.Reset()
				}

			case *types.ConverseStreamOutputMemberMessageStop:
				// Message complete
				chunks <- &agent.CompletionChunk{Done: true}
				return

			case *types.ConverseStreamOutputMemberMetadata:
				// Usage metadata (optional)
				// Could emit token counts here if needed
			}
		}
	}
}

// convertMessages converts internal messages to Bedrock Converse format.
func (p *BedrockProvider) convertMessages(messages []agent.CompletionMessage) ([]types.Message, error) {
	result := make([]types.Message, 0, len(messages))

	for _, msg := range messages {
		if msg.Role == "system" {
			continue // System messages handled separately
		}

		var content []types.ContentBlock

		// Add text content
		if msg.Content != "" {
			content = append(content, &types.ContentBlockMemberText{Value: msg.Content})
		}

		// Note: Image attachments would need to be fetched from URL and converted to bytes
		// This is not implemented as it requires HTTP fetching which should be done by the caller
		// For now, we skip image attachments - callers should pre-process images into base64

		// Add tool results
		for _, tr := range msg.ToolResults {
			toolContent := []types.ToolResultContentBlock{
				&types.ToolResultContentBlockMemberText{Value: tr.Content},
			}
			content = append(content, &types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: aws.String(tr.ToolCallID),
					Content:   toolContent,
				},
			})
		}

		// Add tool calls (from assistant)
		for _, tc := range msg.ToolCalls {
			var inputDoc any
			if err := json.Unmarshal(tc.Input, &inputDoc); err != nil {
				inputDoc = map[string]any{}
			}
			content = append(content, &types.ContentBlockMemberToolUse{
				Value: types.ToolUseBlock{
					ToolUseId: aws.String(tc.ID),
					Name:      aws.String(tc.Name),
					Input:     document.NewLazyDocument(inputDoc),
				},
			})
		}

		// Map role
		role := types.ConversationRoleUser
		if msg.Role == "assistant" {
			role = types.ConversationRoleAssistant
		}

		if len(content) > 0 {
			result = append(result, types.Message{
				Role:    role,
				Content: content,
			})
		}
	}

	return result, nil
}

// isRetryableError determines if an error should trigger a retry.
func (p *BedrockProvider) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if providerErr, ok := GetProviderError(err); ok {
		return providerErr.Reason.IsRetryable()
	}

	errMsg := err.Error()

	// AWS throttling errors
	if strings.Contains(errMsg, "ThrottlingException") ||
		strings.Contains(errMsg, "TooManyRequestsException") ||
		strings.Contains(errMsg, "ServiceUnavailableException") {
		return true
	}

	// Generic retryable patterns
	retryable := []string{"rate limit", "429", "500", "502", "503", "504", "timeout", "deadline exceeded"}
	for _, s := range retryable {
		if strings.Contains(strings.ToLower(errMsg), s) {
			return true
		}
	}
	return false
}

func (p *BedrockProvider) wrapError(err error, model string) error {
	if err == nil {
		return nil
	}
	if IsProviderError(err) {
		return err
	}
	return NewProviderError("bedrock", model, err)
}
