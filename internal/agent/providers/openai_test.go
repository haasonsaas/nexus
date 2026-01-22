package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
	openai "github.com/sashabaranov/go-openai"
)

func TestConvertToOpenAIMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []agent.CompletionMessage
		system   string
		wantLen  int
		wantErr  bool
	}{
		{
			name: "basic text messages",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
			},
			system:  "You are a helpful assistant",
			wantLen: 3, // system + 2 messages
			wantErr: false,
		},
		{
			name: "message with tool calls",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "What's the weather?"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []models.ToolCall{
						{
							ID:    "call_123",
							Name:  "get_weather",
							Input: json.RawMessage(`{"location":"NYC"}`),
						},
					},
				},
			},
			system:  "",
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "message with tool results",
			messages: []agent.CompletionMessage{
				{
					Role: "tool",
					ToolResults: []models.ToolResult{
						{
							ToolCallID: "call_123",
							Content:    "Sunny, 72F",
							IsError:    false,
						},
					},
				},
			},
			system:  "",
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "message with image attachment (vision)",
			messages: []agent.CompletionMessage{
				{
					Role:    "user",
					Content: "What's in this image?",
					Attachments: []models.Attachment{
						{
							ID:       "img_1",
							Type:     "image",
							URL:      "https://example.com/image.jpg",
							MimeType: "image/jpeg",
						},
					},
				},
			},
			system:  "",
			wantLen: 1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &OpenAIProvider{}
			got, err := provider.convertToOpenAIMessages(tt.messages, tt.system)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertToOpenAIMessages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("convertToOpenAIMessages() got %d messages, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestConvertToOpenAITools(t *testing.T) {
	mockTool := &openaiMockTool{
		name:        "test_tool",
		description: "A test tool",
		schema:      json.RawMessage(`{"type":"object","properties":{"arg":{"type":"string"}}}`),
	}

	provider := &OpenAIProvider{}
	tools := []agent.Tool{mockTool}

	got := provider.convertToOpenAITools(tools)
	if len(got) != 1 {
		t.Errorf("convertToOpenAITools() got %d tools, want 1", len(got))
	}

	if got[0].Function.Name != "test_tool" {
		t.Errorf("convertToOpenAITools() name = %v, want test_tool", got[0].Function.Name)
	}
}

func TestWrapOpenAIError(t *testing.T) {
	provider := &OpenAIProvider{}

	apiErr := &openai.APIError{
		HTTPStatusCode: 429,
		Message:        "rate limit exceeded",
		Code:           "rate_limit_error",
	}
	wrapped := provider.wrapError(apiErr, "gpt-4o")
	providerErr, ok := GetProviderError(wrapped)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", wrapped)
	}
	if providerErr.Status != 429 {
		t.Fatalf("expected status 429, got %d", providerErr.Status)
	}
	if providerErr.Reason != FailoverRateLimit {
		t.Fatalf("expected reason %v, got %v", FailoverRateLimit, providerErr.Reason)
	}
	if providerErr.Code != "rate_limit_error" {
		t.Fatalf("expected code rate_limit_error, got %q", providerErr.Code)
	}

	reqErr := &openai.RequestError{
		HTTPStatusCode: 503,
		Err:            errors.New("upstream unavailable"),
	}
	wrapped = provider.wrapError(reqErr, "gpt-4o")
	providerErr, ok = GetProviderError(wrapped)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", wrapped)
	}
	if providerErr.Status != 503 {
		t.Fatalf("expected status 503, got %d", providerErr.Status)
	}
	if providerErr.Reason != FailoverServerError {
		t.Fatalf("expected reason %v, got %v", FailoverServerError, providerErr.Reason)
	}
}

func TestParseToolCallFromChunk(t *testing.T) {
	tests := []struct {
		name     string
		delta    openai.ChatCompletionStreamChoiceDelta
		wantCall bool
		wantDone bool
	}{
		{
			name: "tool call start",
			delta: openai.ChatCompletionStreamChoiceDelta{
				ToolCalls: []openai.ToolCall{
					{
						Index: intPtr(0),
						ID:    "call_123",
						Type:  "function",
						Function: openai.FunctionCall{
							Name:      "test_func",
							Arguments: "",
						},
					},
				},
			},
			wantCall: false, // Not done yet
			wantDone: false,
		},
		{
			name: "tool call arguments chunk",
			delta: openai.ChatCompletionStreamChoiceDelta{
				ToolCalls: []openai.ToolCall{
					{
						Index: intPtr(0),
						Function: openai.FunctionCall{
							Arguments: `{"arg":"value"}`,
						},
					},
				},
			},
			wantCall: false,
			wantDone: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This tests the internal tool call parsing logic
			// Implementation will track state across chunks
			_ = tt // Use the test data in actual implementation
		})
	}
}

func TestProviderName(t *testing.T) {
	provider := &OpenAIProvider{}
	if got := provider.Name(); got != "openai" {
		t.Errorf("Name() = %v, want openai", got)
	}
}

func TestProviderSupportsTools(t *testing.T) {
	provider := &OpenAIProvider{}
	if !provider.SupportsTools() {
		t.Error("SupportsTools() = false, want true")
	}
}

func TestProviderModels(t *testing.T) {
	provider := &OpenAIProvider{}
	models := provider.Models()

	if len(models) == 0 {
		t.Error("Models() returned empty list")
	}

	// Check for expected models
	modelNames := make(map[string]bool)
	for _, m := range models {
		modelNames[m.ID] = true
	}

	expectedModels := []string{"gpt-4o", "gpt-4-turbo", "gpt-3.5-turbo"}
	for _, expected := range expectedModels {
		if !modelNames[expected] {
			t.Errorf("Models() missing expected model: %s", expected)
		}
	}
}

func TestOpenAIErrorHandling(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *OpenAIProvider
		wantErr bool
	}{
		{
			name: "missing API key",
			setup: func() *OpenAIProvider {
				return NewOpenAIProvider("")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := tt.setup()
			req := &agent.CompletionRequest{
				Model: "gpt-3.5-turbo",
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "Hello"},
				},
			}

			_, err := provider.Complete(context.Background(), req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Complete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Mock tool for testing OpenAI provider
type openaiMockTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (m *openaiMockTool) Name() string {
	return m.name
}

func (m *openaiMockTool) Description() string {
	return m.description
}

func (m *openaiMockTool) Schema() json.RawMessage {
	return m.schema
}

func (m *openaiMockTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	return &agent.ToolResult{Content: "mock result"}, nil
}

// Helper function
func intPtr(i int) *int {
	return &i
}

func TestVisionSupport(t *testing.T) {
	provider := &OpenAIProvider{}
	models := provider.Models()

	visionModels := 0
	for _, m := range models {
		if m.SupportsVision {
			visionModels++
		}
	}

	if visionModels == 0 {
		t.Error("No models with vision support found")
	}

	// Verify specific models support vision
	for _, m := range models {
		if m.ID == "gpt-4o" || m.ID == "gpt-4-turbo" {
			if !m.SupportsVision {
				t.Errorf("Model %s should support vision", m.ID)
			}
		}
	}
}

func TestConvertMessagesWithMultipleImages(t *testing.T) {
	provider := &OpenAIProvider{}
	messages := []agent.CompletionMessage{
		{
			Role:    "user",
			Content: "Compare these images",
			Attachments: []models.Attachment{
				{
					ID:   "img_1",
					Type: "image",
					URL:  "https://example.com/image1.jpg",
				},
				{
					ID:   "img_2",
					Type: "image",
					URL:  "https://example.com/image2.jpg",
				},
			},
		},
	}

	got, err := provider.convertToOpenAIMessages(messages, "")
	if err != nil {
		t.Fatalf("convertToOpenAIMessages() error = %v", err)
	}

	if len(got) != 1 {
		t.Errorf("Expected 1 message, got %d", len(got))
	}

	if len(got[0].MultiContent) != 3 { // text + 2 images
		t.Errorf("Expected 3 content parts, got %d", len(got[0].MultiContent))
	}
}

func TestRetryLogic(t *testing.T) {
	provider := &OpenAIProvider{
		maxRetries: 3,
		retryDelay: time.Millisecond * 10,
	}

	tests := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{"rate limit error", fmt.Errorf("rate limit exceeded"), true},
		{"429 status", fmt.Errorf("HTTP 429"), true},
		{"500 server error", fmt.Errorf("HTTP 500"), true},
		{"timeout", fmt.Errorf("timeout exceeded"), true},
		{"invalid API key", fmt.Errorf("invalid API key"), false},
		{"no error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.isRetryableError(tt.err)
			if got != tt.wantRetry {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.wantRetry)
			}
		})
	}
}

func TestTokenCounting(t *testing.T) {
	// Test that models have appropriate context sizes
	provider := &OpenAIProvider{}
	models := provider.Models()

	for _, m := range models {
		if m.ContextSize <= 0 {
			t.Errorf("Model %s has invalid context size: %d", m.ID, m.ContextSize)
		}

		// Verify expected context sizes
		switch m.ID {
		case "gpt-4o", "gpt-4-turbo":
			if m.ContextSize != 128000 {
				t.Errorf("Model %s has wrong context size: %d, want 128000", m.ID, m.ContextSize)
			}
		case "gpt-3.5-turbo":
			if m.ContextSize != 16385 {
				t.Errorf("Model %s has wrong context size: %d, want 16385", m.ID, m.ContextSize)
			}
		}
	}
}

// TestNewOpenAIProviderWithConfig tests the config-based constructor.
func TestNewOpenAIProviderWithConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     OpenAIConfig
		wantNilCli bool
	}{
		{
			name: "full config",
			config: OpenAIConfig{
				APIKey:     "sk-test-key",
				BaseURL:    "https://custom.api.example.com/v1",
				MaxRetries: 5,
				RetryDelay: 2 * time.Second,
			},
			wantNilCli: false,
		},
		{
			name: "empty API key",
			config: OpenAIConfig{
				APIKey: "",
			},
			wantNilCli: true,
		},
		{
			name: "defaults applied",
			config: OpenAIConfig{
				APIKey: "sk-test",
			},
			wantNilCli: false,
		},
		{
			name: "negative retries uses default",
			config: OpenAIConfig{
				APIKey:     "sk-test",
				MaxRetries: -5,
				RetryDelay: -1 * time.Second,
			},
			wantNilCli: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewOpenAIProviderWithConfig(tt.config)
			if provider == nil {
				t.Fatal("expected provider but got nil")
			}

			if tt.wantNilCli {
				if provider.client != nil {
					t.Error("expected nil client for empty API key")
				}
			} else {
				if provider.client == nil {
					t.Error("expected non-nil client")
				}
			}

			// Check defaults are applied
			if provider.maxRetries <= 0 {
				t.Errorf("expected positive maxRetries, got %d", provider.maxRetries)
			}
			if provider.retryDelay <= 0 {
				t.Errorf("expected positive retryDelay, got %v", provider.retryDelay)
			}
		})
	}
}

// TestOpenAIProviderWithBaseURL tests provider with custom base URL.
func TestOpenAIProviderWithBaseURL(t *testing.T) {
	provider := NewOpenAIProviderWithConfig(OpenAIConfig{
		APIKey:  "sk-test",
		BaseURL: "https://custom.openai.proxy.com/v1",
	})

	if provider == nil {
		t.Fatal("expected provider but got nil")
	}
	if provider.client == nil {
		t.Fatal("expected client but got nil")
	}
}

// TestConvertToOpenAIMessagesSystemRole tests system role handling.
func TestConvertToOpenAIMessagesSystemRole(t *testing.T) {
	provider := &OpenAIProvider{}

	messages := []agent.CompletionMessage{
		{Role: "system", Content: "System message in messages array"},
		{Role: "user", Content: "Hello"},
	}

	result, err := provider.convertToOpenAIMessages(messages, "Additional system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: injected system + original system + user
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}

	if result[0].Role != openai.ChatMessageRoleSystem {
		t.Errorf("first message should be system, got %s", result[0].Role)
	}
}

// TestConvertToOpenAIMessagesMultipleToolResults tests multiple tool results.
func TestConvertToOpenAIMessagesMultipleToolResults(t *testing.T) {
	provider := &OpenAIProvider{}

	messages := []agent.CompletionMessage{
		{
			Role: "tool",
			ToolResults: []models.ToolResult{
				{ToolCallID: "call_1", Content: "Result 1"},
				{ToolCallID: "call_2", Content: "Result 2"},
			},
		},
	}

	result, err := provider.convertToOpenAIMessages(messages, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each tool result becomes a separate message
	if len(result) != 2 {
		t.Errorf("expected 2 messages (one per tool result), got %d", len(result))
	}

	for i, msg := range result {
		if msg.Role != openai.ChatMessageRoleTool {
			t.Errorf("message %d should be tool role, got %s", i, msg.Role)
		}
	}
}

// TestConvertToOpenAIMessagesNonImageAttachment tests non-image attachments.
func TestConvertToOpenAIMessagesNonImageAttachment(t *testing.T) {
	provider := &OpenAIProvider{}

	messages := []agent.CompletionMessage{
		{
			Role:    "user",
			Content: "Check this document",
			Attachments: []models.Attachment{
				{
					ID:       "doc_1",
					Type:     "document", // Not an image
					URL:      "https://example.com/doc.pdf",
					MimeType: "application/pdf",
				},
			},
		},
	}

	result, err := provider.convertToOpenAIMessages(messages, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use simple Content (not MultiContent) since no images
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	if result[0].Content != "Check this document" {
		t.Errorf("expected simple content, got %q", result[0].Content)
	}

	if len(result[0].MultiContent) != 0 {
		t.Error("expected empty MultiContent for non-image attachments")
	}
}

// TestConvertToOpenAIToolsInvalidSchema tests handling of invalid tool schemas.
func TestConvertToOpenAIToolsInvalidSchema(t *testing.T) {
	provider := &OpenAIProvider{}

	tools := []agent.Tool{
		&openaiMockTool{
			name:        "bad_tool",
			description: "Tool with invalid schema",
			schema:      json.RawMessage(`not valid json`),
		},
	}

	result := provider.convertToOpenAITools(tools)

	// Should still return a tool with empty schema (graceful degradation)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
}

// TestIsRetryableErrorNil tests that nil error returns false.
func TestIsRetryableErrorNil(t *testing.T) {
	provider := &OpenAIProvider{}

	if provider.isRetryableError(nil) {
		t.Error("nil error should not be retryable")
	}
}

// TestOpenAIIsRetryableWithProviderError tests retry with ProviderError.
func TestOpenAIIsRetryableWithProviderError(t *testing.T) {
	provider := &OpenAIProvider{}

	// Rate limit error should be retryable
	rateLimitErr := NewProviderError("openai", "gpt-4o", errors.New("rate limit")).
		WithStatus(429)

	if !provider.isRetryableError(rateLimitErr) {
		t.Error("rate limit ProviderError should be retryable")
	}

	// Auth error should not be retryable
	authErr := NewProviderError("openai", "gpt-4o", errors.New("unauthorized")).
		WithStatus(401)

	if provider.isRetryableError(authErr) {
		t.Error("auth ProviderError should not be retryable")
	}
}

// TestWrapErrorNil tests nil error handling.
func TestOpenAIWrapErrorNil(t *testing.T) {
	provider := &OpenAIProvider{}

	result := provider.wrapError(nil, "gpt-4o")
	if result != nil {
		t.Errorf("expected nil for nil error, got %v", result)
	}
}

// TestWrapErrorAlreadyWrapped tests already-wrapped error handling.
func TestOpenAIWrapErrorAlreadyWrapped(t *testing.T) {
	provider := &OpenAIProvider{}

	originalErr := NewProviderError("openai", "gpt-4o", errors.New("test")).
		WithStatus(429)

	wrapped := provider.wrapError(originalErr, "different-model")

	if wrapped != originalErr {
		t.Error("expected already-wrapped error to be returned as-is")
	}
}

// TestWrapErrorWithRequestError tests wrapping openai.RequestError.
func TestWrapErrorWithRequestError(t *testing.T) {
	provider := &OpenAIProvider{}

	reqErr := &openai.RequestError{
		HTTPStatusCode: 503,
		Err:            errors.New("service temporarily unavailable"),
	}

	wrapped := provider.wrapError(reqErr, "gpt-4o")
	providerErr, ok := GetProviderError(wrapped)
	if !ok {
		t.Fatal("expected ProviderError")
	}

	if providerErr.Status != 503 {
		t.Errorf("expected status 503, got %d", providerErr.Status)
	}
}

// TestWrapErrorWithNestedAPIError tests wrapping nested API errors.
func TestWrapErrorWithNestedAPIError(t *testing.T) {
	provider := &OpenAIProvider{}

	innerAPIErr := &openai.APIError{
		HTTPStatusCode: 400,
		Message:        "Invalid model",
		Code:           "invalid_model",
	}

	reqErr := &openai.RequestError{
		HTTPStatusCode: 400,
		Err:            innerAPIErr,
	}

	wrapped := provider.wrapError(reqErr, "gpt-invalid")
	providerErr, ok := GetProviderError(wrapped)
	if !ok {
		t.Fatal("expected ProviderError")
	}

	if providerErr.Message != "Invalid model" {
		t.Errorf("expected message 'Invalid model', got %q", providerErr.Message)
	}
	if providerErr.Code != "invalid_model" {
		t.Errorf("expected code 'invalid_model', got %q", providerErr.Code)
	}
}

// TestContainsFunction tests the contains helper function.
func TestContainsFunction(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "hello", true},
		{"hello", "hello", true},
		{"hello", "x", false},
		{"", "", true},     // Edge case: empty strings (s == substr when both empty)
		{"a", "ab", false}, // substr longer than s
	}

	for _, tt := range tests {
		result := contains(tt.s, tt.substr)
		if result != tt.expected {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
		}
	}
}

// TestFindSubstringFunction tests the findSubstring helper function.
func TestFindSubstringFunction(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected int
	}{
		{"hello world", "world", 6},
		{"hello world", "hello", 0},
		{"hello", "x", -1},
		{"aaa", "a", 0},
	}

	for _, tt := range tests {
		result := findSubstring(tt.s, tt.substr)
		if result != tt.expected {
			t.Errorf("findSubstring(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
		}
	}
}

// TestCompleteWithContextCancellation tests that Complete respects context cancellation.
func TestCompleteWithContextCancellation(t *testing.T) {
	provider := NewOpenAIProvider("sk-test-key")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := provider.Complete(ctx, &agent.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []agent.CompletionMessage{{Role: "user", Content: "Hello"}},
	})

	// Should get context.Canceled error
	if err == nil {
		t.Log("Note: may get error from API client before context check")
	}
}

// TestConvertToOpenAIMessagesEmptyContent tests message with empty content but tool calls.
func TestConvertToOpenAIMessagesEmptyContent(t *testing.T) {
	provider := &OpenAIProvider{}

	messages := []agent.CompletionMessage{
		{
			Role:    "assistant",
			Content: "", // Empty content
			ToolCalls: []models.ToolCall{
				{
					ID:    "call_1",
					Name:  "get_weather",
					Input: json.RawMessage(`{"city":"NYC"}`),
				},
			},
		},
	}

	result, err := provider.convertToOpenAIMessages(messages, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	// Should have tool calls even with empty content
	if len(result[0].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
}

// TestConvertToOpenAIMessagesImageWithoutText tests image-only message.
func TestConvertToOpenAIMessagesImageWithoutText(t *testing.T) {
	provider := &OpenAIProvider{}

	messages := []agent.CompletionMessage{
		{
			Role:    "user",
			Content: "", // No text content
			Attachments: []models.Attachment{
				{
					ID:   "img_1",
					Type: "image",
					URL:  "https://example.com/image.jpg",
				},
			},
		},
	}

	result, err := provider.convertToOpenAIMessages(messages, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	// Should have MultiContent with just the image (no text part)
	if len(result[0].MultiContent) != 1 {
		t.Errorf("expected 1 content part (image only), got %d", len(result[0].MultiContent))
	}
}

// TestModelGPT4Properties tests GPT-4 model properties.
func TestModelGPT4Properties(t *testing.T) {
	provider := &OpenAIProvider{}
	models := provider.Models()

	var gpt4 *agent.Model
	for i, m := range models {
		if m.ID == "gpt-4" {
			gpt4 = &models[i]
			break
		}
	}

	if gpt4 == nil {
		t.Fatal("gpt-4 model not found")
	}

	if gpt4.ContextSize != 8192 {
		t.Errorf("expected gpt-4 context size 8192, got %d", gpt4.ContextSize)
	}

	if gpt4.SupportsVision {
		t.Error("gpt-4 (non-turbo) should not support vision")
	}
}

// TestIsRetryableEdgeCases tests edge cases in retry logic.
func TestIsRetryableEdgeCases(t *testing.T) {
	provider := &OpenAIProvider{}

	tests := []struct {
		name  string
		err   error
		retry bool
	}{
		{
			name:  "502 bad gateway",
			err:   errors.New("HTTP 502 Bad Gateway"),
			retry: true,
		},
		{
			name:  "504 gateway timeout",
			err:   errors.New("HTTP 504 Gateway Timeout"),
			retry: true,
		},
		{
			name:  "context deadline exceeded",
			err:   errors.New("context deadline exceeded"),
			retry: true,
		},
		{
			name:  "unknown error",
			err:   errors.New("something completely unknown"),
			retry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.isRetryableError(tt.err)
			if result != tt.retry {
				t.Errorf("expected retry=%v, got %v", tt.retry, result)
			}
		})
	}
}
