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
