package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
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

const (
	bedrockImageMaxBytes = 20 * 1024 * 1024
	bedrockImageTimeout  = 30 * time.Second
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
	base         BaseProvider
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
		base:         NewBaseProvider("bedrock", cfg.MaxRetries, cfg.RetryDelay),
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
	messages, err := p.convertMessages(ctx, req.Messages)
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
	err = p.base.Retry(ctx, p.isRetryableError, func() error {
		stream, lastErr = p.client.ConverseStream(ctx, converseReq)
		if lastErr != nil {
			lastErr = p.wrapError(lastErr, model)
			return lastErr
		}
		return nil
	})
	if err != nil {
		if p.isRetryableError(err) {
			return nil, fmt.Errorf("bedrock: max retries exceeded: %w", err)
		}
		return nil, err
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
func (p *BedrockProvider) convertMessages(ctx context.Context, messages []agent.CompletionMessage) ([]types.Message, error) {
	result := make([]types.Message, 0, len(messages))
	if ctx == nil {
		ctx = context.Background()
	}

	for _, msg := range messages {
		if msg.Role == "system" {
			continue // System messages handled separately
		}

		var content []types.ContentBlock

		// Add text content
		if msg.Content != "" {
			content = append(content, &types.ContentBlockMemberText{Value: msg.Content})
		}

		// Add image attachments
		for _, attachment := range msg.Attachments {
			if attachment.Type != "image" {
				continue
			}
			imageBlock, err := p.convertImageAttachment(ctx, attachment)
			if err != nil {
				continue
			}
			content = append(content, imageBlock)
		}

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

func (p *BedrockProvider) convertImageAttachment(ctx context.Context, attachment models.Attachment) (*types.ContentBlockMemberImage, error) {
	data, mimeType, err := fetchImageAttachment(ctx, attachment)
	if err != nil {
		return nil, err
	}
	format, ok := bedrockImageFormat(mimeType, attachment.URL, attachment.Filename)
	if !ok {
		return nil, fmt.Errorf("unsupported image format")
	}
	return &types.ContentBlockMemberImage{
		Value: types.ImageBlock{
			Format: format,
			Source: &types.ImageSourceMemberBytes{Value: data},
		},
	}, nil
}

func fetchImageAttachment(ctx context.Context, attachment models.Attachment) ([]byte, string, error) {
	url := strings.TrimSpace(attachment.URL)
	if url == "" {
		return nil, "", fmt.Errorf("attachment url is required")
	}
	if strings.HasPrefix(url, "data:") {
		data, mimeType, err := decodeBedrockDataURL(url)
		if err != nil {
			return nil, "", err
		}
		if int64(len(data)) > bedrockImageMaxBytes {
			return nil, "", fmt.Errorf("attachment too large (%d bytes)", len(data))
		}
		if attachment.MimeType != "" {
			mimeType = attachment.MimeType
		}
		return data, normalizeMimeType(mimeType), nil
	}

	pathValue := strings.TrimPrefix(url, "file://")
	if pathValue != "" {
		if info, err := os.Stat(pathValue); err == nil && !info.IsDir() {
			if info.Size() > bedrockImageMaxBytes {
				return nil, "", fmt.Errorf("attachment too large (%d bytes)", info.Size())
			}
			payload, err := os.ReadFile(pathValue)
			if err != nil {
				return nil, "", fmt.Errorf("read attachment: %w", err)
			}
			mimeType := attachment.MimeType
			if mimeType == "" {
				mimeType = guessImageMimeType(pathValue, attachment.Filename)
			}
			return payload, normalizeMimeType(mimeType), nil
		}
	}

	requestCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, bedrockImageTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("fetch attachment returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > bedrockImageMaxBytes {
		return nil, "", fmt.Errorf("attachment too large (%d bytes)", resp.ContentLength)
	}
	limited := io.LimitReader(resp.Body, bedrockImageMaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("read attachment: %w", err)
	}
	if int64(len(data)) > bedrockImageMaxBytes {
		return nil, "", fmt.Errorf("attachment too large (%d bytes)", len(data))
	}
	mimeType := attachment.MimeType
	if mimeType == "" {
		mimeType = resp.Header.Get("Content-Type")
	}
	if mimeType == "" {
		mimeType = guessImageMimeType(url, attachment.Filename)
	}
	return data, normalizeMimeType(mimeType), nil
}

func decodeBedrockDataURL(raw string) ([]byte, string, error) {
	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid data url")
	}
	meta := strings.TrimPrefix(parts[0], "data:")
	mimeType := "image/jpeg"
	if meta != "" {
		metaParts := strings.Split(meta, ";")
		if len(metaParts) > 0 && metaParts[0] != "" {
			mimeType = metaParts[0]
		}
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, "", fmt.Errorf("decode data url: %w", err)
	}
	return data, mimeType, nil
}

func normalizeMimeType(mimeType string) string {
	if mimeType == "" {
		return ""
	}
	parts := strings.Split(mimeType, ";")
	return strings.TrimSpace(parts[0])
}

func bedrockImageFormat(mimeType, url, filename string) (types.ImageFormat, bool) {
	normalized := strings.ToLower(normalizeMimeType(mimeType))
	switch normalized {
	case "image/png":
		return types.ImageFormatPng, true
	case "image/jpeg", "image/jpg":
		return types.ImageFormatJpeg, true
	case "image/gif":
		return types.ImageFormatGif, true
	case "image/webp":
		return types.ImageFormatWebp, true
	}
	if ext := strings.ToLower(path.Ext(url)); ext != "" {
		return bedrockFormatFromExt(ext)
	}
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		return bedrockFormatFromExt(ext)
	}
	if ext := strings.ToLower(filepath.Ext(url)); ext != "" {
		return bedrockFormatFromExt(ext)
	}
	return "", false
}

func bedrockFormatFromExt(ext string) (types.ImageFormat, bool) {
	switch ext {
	case ".png":
		return types.ImageFormatPng, true
	case ".jpg", ".jpeg":
		return types.ImageFormatJpeg, true
	case ".gif":
		return types.ImageFormatGif, true
	case ".webp":
		return types.ImageFormatWebp, true
	default:
		return "", false
	}
}

func guessImageMimeType(url, filename string) string {
	if ext := strings.ToLower(path.Ext(url)); ext != "" {
		return mimeTypeFromExt(ext)
	}
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		return mimeTypeFromExt(ext)
	}
	if ext := strings.ToLower(filepath.Ext(url)); ext != "" {
		return mimeTypeFromExt(ext)
	}
	return ""
}

func mimeTypeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
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
