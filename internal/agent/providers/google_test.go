package providers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/toolconv"
	"github.com/haasonsaas/nexus/pkg/models"
)

// TestNewGoogleProvider tests provider initialization with various configurations.
func TestNewGoogleProvider(t *testing.T) {
	tests := []struct {
		name        string
		config      GoogleConfig
		expectError bool
		errContains string
	}{
		{
			name: "valid config with all fields",
			config: GoogleConfig{
				APIKey:       "test-api-key",
				MaxRetries:   5,
				RetryDelay:   2 * time.Second,
				DefaultModel: "gemini-1.5-pro",
			},
			expectError: false,
		},
		{
			name: "valid config with API key only (defaults applied)",
			config: GoogleConfig{
				APIKey: "test-api-key",
			},
			expectError: false,
		},
		{
			name: "missing API key",
			config: GoogleConfig{
				MaxRetries: 3,
			},
			expectError: true,
			errContains: "API key is required",
		},
		{
			name: "empty API key",
			config: GoogleConfig{
				APIKey: "",
			},
			expectError: true,
			errContains: "API key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewGoogleProvider(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if provider == nil {
				t.Fatal("expected provider but got nil")
			}

			// Verify defaults are applied
			if provider.maxRetries <= 0 {
				t.Error("maxRetries should have default value > 0")
			}
			if provider.retryDelay <= 0 {
				t.Error("retryDelay should have default value > 0")
			}
			if provider.defaultModel == "" {
				t.Error("defaultModel should have default value")
			}
		})
	}
}

// TestGoogleProviderDefaults tests that defaults are correctly applied.
func TestGoogleProviderDefaults(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Check default values
	if provider.maxRetries != 3 {
		t.Errorf("expected default maxRetries=3, got %d", provider.maxRetries)
	}
	if provider.retryDelay != time.Second {
		t.Errorf("expected default retryDelay=1s, got %v", provider.retryDelay)
	}
	if provider.defaultModel != "gemini-2.0-flash" {
		t.Errorf("expected default model gemini-2.0-flash, got %s", provider.defaultModel)
	}
}

// TestGoogleProviderName tests the provider name method.
func TestGoogleProviderName(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider.Name() != "google" {
		t.Errorf("expected name 'google', got '%s'", provider.Name())
	}
}

// TestGoogleProviderSupportsTools tests that the provider advertises tool support.
func TestGoogleProviderSupportsTools(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if !provider.SupportsTools() {
		t.Error("expected SupportsTools to return true")
	}
}

// TestGoogleProviderModels tests the available models list.
func TestGoogleProviderModels(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	models := provider.Models()
	if len(models) == 0 {
		t.Error("expected at least one model")
	}

	// Check for expected models
	expectedModels := []string{
		"gemini-2.0-flash",
		"gemini-2.0-flash-lite",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
		"gemini-1.5-flash-8b",
	}

	modelIDs := make(map[string]bool)
	for _, m := range models {
		modelIDs[m.ID] = true

		// Verify model properties are populated
		if m.Name == "" {
			t.Errorf("model %s has empty name", m.ID)
		}
		if m.ContextSize <= 0 {
			t.Errorf("model %s has invalid context size", m.ID)
		}
	}

	for _, expected := range expectedModels {
		if !modelIDs[expected] {
			t.Errorf("expected model %s not found", expected)
		}
	}

	// All Gemini models should support vision
	for _, m := range models {
		if !m.SupportsVision {
			t.Errorf("model %s should support vision", m.ID)
		}
	}
}

// TestGoogleProviderGetModel tests model selection with default fallback.
func TestGoogleProviderGetModel(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey:       "test-key",
		DefaultModel: "gemini-1.5-pro",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string returns default",
			input:    "",
			expected: "gemini-1.5-pro",
		},
		{
			name:     "specified model is returned",
			input:    "gemini-2.0-flash",
			expected: "gemini-2.0-flash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.getModel(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestGoogleProviderConvertMessages tests message format conversion.
func TestGoogleProviderConvertMessages(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name     string
		messages []agent.CompletionMessage
		wantLen  int
		wantErr  bool
	}{
		{
			name: "simple user message",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "Hello!"},
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "system message is skipped",
			messages: []agent.CompletionMessage{
				{Role: "system", Content: "You are helpful."},
				{Role: "user", Content: "Hello!"},
			},
			wantLen: 1, // system message skipped
			wantErr: false,
		},
		{
			name: "multi-turn conversation",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "Hello!"},
				{Role: "assistant", Content: "Hi there!"},
				{Role: "user", Content: "How are you?"},
			},
			wantLen: 3,
			wantErr: false,
		},
		{
			name: "message with tool calls",
			messages: []agent.CompletionMessage{
				{
					Role:    "assistant",
					Content: "Let me check that.",
					ToolCalls: []models.ToolCall{
						{
							ID:    "call_123",
							Name:  "get_weather",
							Input: json.RawMessage(`{"city":"London"}`),
						},
					},
				},
			},
			wantLen: 1,
			wantErr: false,
		},
		{
			name: "message with tool results",
			messages: []agent.CompletionMessage{
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []models.ToolCall{
						{ID: "call_123", Name: "get_weather", Input: json.RawMessage(`{"city":"NYC"}`)},
					},
				},
				{
					Role: "tool",
					ToolResults: []models.ToolResult{
						{
							ToolCallID: "call_123",
							Content:    `{"temperature": 72, "conditions": "sunny"}`,
							IsError:    false,
						},
					},
				},
			},
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "message with invalid tool call JSON (should use empty map)",
			messages: []agent.CompletionMessage{
				{
					Role: "assistant",
					ToolCalls: []models.ToolCall{
						{
							ID:    "call_123",
							Name:  "test",
							Input: json.RawMessage(`invalid json`),
						},
					},
				},
			},
			wantLen: 1, // Should still work with empty args
			wantErr: false,
		},
		{
			name: "empty messages",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: ""}, // Empty content, no parts
			},
			wantLen: 0, // Empty message with no parts is skipped
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := provider.convertMessages(tt.messages)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != tt.wantLen {
				t.Errorf("expected %d messages, got %d", tt.wantLen, len(result))
			}
		})
	}
}

// TestGoogleProviderConvertAttachment tests attachment conversion.
func TestGoogleProviderConvertAttachment(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name       string
		attachment models.Attachment
		wantErr    bool
		errMsg     string
	}{
		{
			name: "base64 data URL with MIME type",
			attachment: models.Attachment{
				Type: "image",
				URL:  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
			},
			wantErr: false,
		},
		{
			name: "base64 data URL without semicolon",
			attachment: models.Attachment{
				Type: "image",
				URL:  "data:image/jpeg,/9j/4AAQSkZJRg==",
			},
			wantErr: false,
		},
		{
			name: "regular URL with extension",
			attachment: models.Attachment{
				Type: "image",
				URL:  "https://example.com/image.jpg",
			},
			wantErr: false,
		},
		{
			name: "regular URL with explicit MIME type",
			attachment: models.Attachment{
				Type:     "image",
				URL:      "https://example.com/image",
				MimeType: "image/webp",
			},
			wantErr: false,
		},
		{
			name: "invalid data URL format",
			attachment: models.Attachment{
				Type: "image",
				URL:  "data:invalid-no-comma",
			},
			wantErr: true,
			errMsg:  "invalid data URL format",
		},
		{
			name: "invalid base64 data",
			attachment: models.Attachment{
				Type: "image",
				URL:  "data:image/png;base64,not-valid-base64!!!",
			},
			wantErr: true,
			errMsg:  "decode base64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := provider.convertAttachment(tt.attachment)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("expected result but got nil")
			}
		})
	}
}

// googleMockTool implements agent.Tool for testing.
type googleMockTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (m *googleMockTool) Name() string {
	return m.name
}

func (m *googleMockTool) Description() string {
	return m.description
}

func (m *googleMockTool) Schema() json.RawMessage {
	return m.schema
}

func (m *googleMockTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	return &agent.ToolResult{Content: "test result"}, nil
}

// TestGoogleProviderConvertTools tests tool definition conversion.
func TestGoogleProviderConvertTools(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name    string
		tools   []agent.Tool
		wantLen int
		wantNil bool
	}{
		{
			name:    "empty tools list",
			tools:   []agent.Tool{},
			wantNil: true,
		},
		{
			name:    "nil tools list",
			tools:   nil,
			wantNil: true,
		},
		{
			name: "single valid tool",
			tools: []agent.Tool{
				&googleMockTool{
					name:        "get_weather",
					description: "Get current weather",
					schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
				},
			},
			wantLen: 1,
		},
		{
			name: "multiple tools",
			tools: []agent.Tool{
				&googleMockTool{
					name:        "get_weather",
					description: "Get current weather",
					schema:      json.RawMessage(`{"type":"object"}`),
				},
				&googleMockTool{
					name:        "search",
					description: "Search the web",
					schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
				},
			},
			wantLen: 1, // Wrapped in single Tool with multiple FunctionDeclarations
		},
		{
			name: "tool with invalid schema JSON (skipped)",
			tools: []agent.Tool{
				&googleMockTool{
					name:        "invalid",
					description: "Invalid tool",
					schema:      json.RawMessage(`not valid json`),
				},
			},
			wantNil: true, // Invalid schemas are skipped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.convertTools(tt.tools)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected result but got nil")
			}

			if len(result) != tt.wantLen {
				t.Errorf("expected %d tools, got %d", tt.wantLen, len(result))
			}
		})
	}
}

// TestGoogleProviderConvertSchemaToGemini tests JSON Schema conversion.
func TestGoogleProviderConvertSchemaToGemini(t *testing.T) {
	tests := []struct {
		name        string
		schemaMap   map[string]any
		expectNil   bool
		expectType  string
		expectProps int
		expectReq   int
		expectEnum  int
	}{
		{
			name:      "nil schema",
			schemaMap: nil,
			expectNil: true,
		},
		{
			name: "simple object type",
			schemaMap: map[string]any{
				"type":        "object",
				"description": "A test object",
			},
			expectType: "OBJECT",
		},
		{
			name: "object with properties",
			schemaMap: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"age":  map[string]any{"type": "integer"},
				},
			},
			expectType:  "OBJECT",
			expectProps: 2,
		},
		{
			name: "object with required fields",
			schemaMap: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []any{"name"},
			},
			expectType:  "OBJECT",
			expectProps: 1,
			expectReq:   1,
		},
		{
			name: "string with enum",
			schemaMap: map[string]any{
				"type": "string",
				"enum": []any{"red", "green", "blue"},
			},
			expectType: "STRING",
			expectEnum: 3,
		},
		{
			name: "array with items",
			schemaMap: map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			expectType: "ARRAY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toolconv.ToGeminiSchema(tt.schemaMap)

			if tt.expectNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected result but got nil")
			}

			if tt.expectType != "" && string(result.Type) != tt.expectType {
				t.Errorf("expected type %s, got %s", tt.expectType, result.Type)
			}

			if tt.expectProps > 0 && len(result.Properties) != tt.expectProps {
				t.Errorf("expected %d properties, got %d", tt.expectProps, len(result.Properties))
			}

			if tt.expectReq > 0 && len(result.Required) != tt.expectReq {
				t.Errorf("expected %d required, got %d", tt.expectReq, len(result.Required))
			}

			if tt.expectEnum > 0 && len(result.Enum) != tt.expectEnum {
				t.Errorf("expected %d enum values, got %d", tt.expectEnum, len(result.Enum))
			}
		})
	}
}

// TestGoogleProviderBuildConfig tests completion request configuration.
func TestGoogleProviderBuildConfig(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name            string
		req             *agent.CompletionRequest
		expectSystem    bool
		expectMaxTokens bool
		expectTools     bool
	}{
		{
			name: "basic request without options",
			req: &agent.CompletionRequest{
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			expectSystem:    false,
			expectMaxTokens: false,
			expectTools:     false,
		},
		{
			name: "request with system prompt",
			req: &agent.CompletionRequest{
				System: "You are a helpful assistant.",
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			expectSystem: true,
		},
		{
			name: "request with max tokens",
			req: &agent.CompletionRequest{
				MaxTokens: 1024,
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			expectMaxTokens: true,
		},
		{
			name: "request with tools",
			req: &agent.CompletionRequest{
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "Hello"},
				},
				Tools: []agent.Tool{
					&googleMockTool{
						name:        "test",
						description: "Test tool",
						schema:      json.RawMessage(`{"type":"object"}`),
					},
				},
			},
			expectTools: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := provider.buildConfig(tt.req)

			if config == nil {
				t.Fatal("expected config but got nil")
			}

			hasSystem := config.SystemInstruction != nil
			if hasSystem != tt.expectSystem {
				t.Errorf("expected hasSystem=%v, got %v", tt.expectSystem, hasSystem)
			}

			hasMaxTokens := config.MaxOutputTokens > 0
			if hasMaxTokens != tt.expectMaxTokens {
				t.Errorf("expected hasMaxTokens=%v, got %v", tt.expectMaxTokens, hasMaxTokens)
			}

			hasTools := len(config.Tools) > 0
			if hasTools != tt.expectTools {
				t.Errorf("expected hasTools=%v, got %v", tt.expectTools, hasTools)
			}
		})
	}
}

// TestGoogleProviderIsRetryableError tests error classification for retry.
func TestGoogleProviderIsRetryableError(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name  string
		err   error
		retry bool
	}{
		{
			name:  "nil error",
			err:   nil,
			retry: false,
		},
		{
			name:  "rate limit error",
			err:   errors.New("rate limit exceeded"),
			retry: true,
		},
		{
			name:  "429 status",
			err:   errors.New("HTTP 429 too many requests"),
			retry: true,
		},
		{
			name:  "resource exhausted (quota)",
			err:   errors.New("resource exhausted"),
			retry: true,
		},
		{
			name:  "quota error",
			err:   errors.New("quota exceeded"),
			retry: true,
		},
		{
			name:  "500 error",
			err:   errors.New("HTTP 500 internal server error"),
			retry: true,
		},
		{
			name:  "502 bad gateway",
			err:   errors.New("502 bad gateway"),
			retry: true,
		},
		{
			name:  "503 service unavailable",
			err:   errors.New("503 service unavailable"),
			retry: true,
		},
		{
			name:  "504 gateway timeout",
			err:   errors.New("504 gateway timeout"),
			retry: true,
		},
		{
			name:  "timeout error",
			err:   errors.New("request timeout"),
			retry: true,
		},
		{
			name:  "deadline exceeded",
			err:   errors.New("context deadline exceeded"),
			retry: true,
		},
		{
			name:  "connection reset",
			err:   errors.New("connection reset by peer"),
			retry: true,
		},
		{
			name:  "connection refused",
			err:   errors.New("connection refused"),
			retry: true,
		},
		{
			name:  "no such host",
			err:   errors.New("no such host"),
			retry: true,
		},
		{
			name:  "invalid API key (not retryable)",
			err:   errors.New("invalid API key"),
			retry: false,
		},
		{
			name:  "validation error (not retryable)",
			err:   errors.New("validation failed"),
			retry: false,
		},
		{
			name:  "unknown error (not retryable)",
			err:   errors.New("something went wrong"),
			retry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.isRetryableError(tt.err)
			if result != tt.retry {
				t.Errorf("expected retry=%v, got %v for error: %v", tt.retry, result, tt.err)
			}
		})
	}
}

// TestGoogleProviderWrapError tests error wrapping with status extraction.
func TestGoogleProviderWrapError(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name       string
		err        error
		model      string
		wantStatus int
	}{
		{
			name:       "nil error",
			err:        nil,
			model:      "gemini-2.0-flash",
			wantStatus: 0,
		},
		{
			name:       "401 unauthorized",
			err:        errors.New("401 unauthenticated"),
			model:      "gemini-2.0-flash",
			wantStatus: 401,
		},
		{
			name:       "403 permission denied",
			err:        errors.New("permission denied"),
			model:      "gemini-2.0-flash",
			wantStatus: 403,
		},
		{
			name:       "404 not found",
			err:        errors.New("model not found"),
			model:      "gemini-2.0-flash",
			wantStatus: 404,
		},
		{
			name:       "429 resource exhausted",
			err:        errors.New("resource exhausted"),
			model:      "gemini-2.0-flash",
			wantStatus: 429,
		},
		{
			name:       "500 internal server error",
			err:        errors.New("500 internal server error"),
			model:      "gemini-2.0-flash",
			wantStatus: 500,
		},
		{
			name:       "503 service unavailable",
			err:        errors.New("503 service unavailable"),
			model:      "gemini-2.0-flash",
			wantStatus: 503,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := provider.wrapError(tt.err, tt.model)

			if tt.err == nil {
				if wrapped != nil {
					t.Errorf("expected nil for nil error, got %v", wrapped)
				}
				return
			}

			if wrapped == nil {
				t.Fatal("expected wrapped error but got nil")
			}

			// Check if the error is a ProviderError with expected status
			providerErr, ok := GetProviderError(wrapped)
			if !ok {
				t.Errorf("expected ProviderError, got %T", wrapped)
				return
			}

			if tt.wantStatus > 0 && providerErr.Status != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, providerErr.Status)
			}

			if providerErr.Provider != "google" {
				t.Errorf("expected provider 'google', got '%s'", providerErr.Provider)
			}

			if providerErr.Model != tt.model {
				t.Errorf("expected model '%s', got '%s'", tt.model, providerErr.Model)
			}
		})
	}
}

// TestGoogleProviderCountTokens tests token count estimation.
func TestGoogleProviderCountTokens(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name string
		req  *agent.CompletionRequest
		want int // approximate expected tokens
	}{
		{
			name: "simple message",
			req: &agent.CompletionRequest{
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "Hello, how are you?"},
				},
			},
			want: 5, // ~20 chars / 4 = 5 tokens
		},
		{
			name: "with system prompt",
			req: &agent.CompletionRequest{
				System: "You are a helpful assistant.",
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "Hello!"},
				},
			},
			want: 8, // system + message
		},
		{
			name: "with tools",
			req: &agent.CompletionRequest{
				Messages: []agent.CompletionMessage{
					{Role: "user", Content: "What's the weather?"},
				},
				Tools: []agent.Tool{
					&googleMockTool{
						name:        "get_weather",
						description: "Get current weather in a city",
						schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
					},
				},
			},
			want: 30, // rough estimate including tool schema
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := provider.CountTokens(tt.req)

			// Allow some variance in estimation
			if count == 0 {
				t.Error("expected non-zero token count")
			}

			// Just verify it's in a reasonable range
			if count < 0 || count > 100000 {
				t.Errorf("unreasonable token count: %d", count)
			}
		})
	}
}

// TestGuessMimeType tests MIME type detection from URL.
func TestGuessMimeType(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://example.com/image.jpg", "image/jpeg"},
		{"https://example.com/image.jpeg", "image/jpeg"},
		{"https://example.com/image.png", "image/png"},
		{"https://example.com/image.gif", "image/gif"},
		{"https://example.com/image.webp", "image/webp"},
		{"https://example.com/image.svg", "image/svg+xml"},
		{"https://example.com/doc.pdf", "application/pdf"},
		{"https://example.com/image", "image/jpeg"},    // default
		{"https://example.com/IMAGE.PNG", "image/png"}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := guessMimeType(tt.url)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestGenerateToolCallID tests tool call ID generation.
func TestGenerateToolCallID(t *testing.T) {
	id1 := generateToolCallID("get_weather")

	// IDs should contain the function name
	if !strings.Contains(id1, "get_weather") {
		t.Errorf("expected ID to contain function name, got %s", id1)
	}

	// IDs should have the expected prefix
	if !strings.HasPrefix(id1, "call_") {
		t.Errorf("expected ID to have call_ prefix, got %s", id1)
	}

	// Test with different function names - should produce different IDs
	id2 := generateToolCallID("search")
	if !strings.Contains(id2, "search") {
		t.Errorf("expected ID to contain function name 'search', got %s", id2)
	}

	// The IDs should be different because they have different function names
	if id1 == id2 {
		t.Error("IDs with different function names should be different")
	}
}

// TestGetToolNameFromID tests extracting tool name from ID.
func TestGetToolNameFromID(t *testing.T) {
	messages := []agent.CompletionMessage{
		{
			Role: "assistant",
			ToolCalls: []models.ToolCall{
				{ID: "call_get_weather_123", Name: "get_weather"},
				{ID: "call_search_456", Name: "search"},
			},
		},
	}

	tests := []struct {
		name       string
		toolCallID string
		expected   string
	}{
		{
			name:       "find from messages",
			toolCallID: "call_get_weather_123",
			expected:   "get_weather",
		},
		{
			name:       "find from messages (search)",
			toolCallID: "call_search_456",
			expected:   "search",
		},
		{
			name:       "extract from ID format",
			toolCallID: "call_unknown_789",
			expected:   "unknown",
		},
		{
			name:       "empty with minimal ID",
			toolCallID: "x",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getToolNameFromID(tt.toolCallID, messages)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestGoogleProviderIsRetryableProviderError tests retry logic with ProviderError.
func TestGoogleProviderIsRetryableProviderError(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Test with ProviderError that has retryable reason
	rateLimitErr := NewProviderError("google", "gemini-2.0-flash", errors.New("rate limit")).
		WithStatus(429)

	if !provider.isRetryableError(rateLimitErr) {
		t.Error("expected rate limit ProviderError to be retryable")
	}

	// Test with ProviderError that has non-retryable reason
	authErr := NewProviderError("google", "gemini-2.0-flash", errors.New("unauthorized")).
		WithStatus(401)

	if provider.isRetryableError(authErr) {
		t.Error("expected auth ProviderError to not be retryable")
	}
}

// TestGoogleProviderAlreadyWrappedError tests that already-wrapped errors are returned as-is.
func TestGoogleProviderAlreadyWrappedError(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create a ProviderError
	originalErr := NewProviderError("google", "gemini-2.0-flash", errors.New("test")).
		WithStatus(429).
		WithCode("rate_limit")

	// Wrap it again
	wrapped := provider.wrapError(originalErr, "different-model")

	// Should return the same error (not double-wrapped)
	if wrapped != originalErr {
		t.Error("expected already-wrapped error to be returned as-is")
	}
}

// TestGoogleProviderConvertMessagesWithAttachments tests message conversion with image attachments.
func TestGoogleProviderConvertMessagesWithAttachments(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []agent.CompletionMessage{
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
	}

	result, err := provider.convertMessages(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	// Should have 2 parts: text + image
	if len(result[0].Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(result[0].Parts))
	}
}

// TestGoogleProviderConvertMessagesWithNonImageAttachment tests non-image attachment handling.
func TestGoogleProviderConvertMessagesWithNonImageAttachment(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

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

	result, err := provider.convertMessages(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	// Should only have 1 part (text) since non-image attachments are skipped
	if len(result[0].Parts) != 1 {
		t.Errorf("expected 1 part (text only), got %d", len(result[0].Parts))
	}
}

// TestGoogleProviderConvertMessagesWithToolResultNonJSON tests tool result with non-JSON content.
func TestGoogleProviderConvertMessagesWithToolResultNonJSON(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []agent.CompletionMessage{
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []models.ToolCall{
				{ID: "call_test", Name: "get_weather", Input: json.RawMessage(`{}`)},
			},
		},
		{
			Role: "tool",
			ToolResults: []models.ToolResult{
				{
					ToolCallID: "call_test",
					Content:    "Plain text result, not JSON",
					IsError:    false,
				},
			},
		},
	}

	result, err := provider.convertMessages(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

// TestGoogleProviderConvertMessagesDefaultRole tests default role assignment.
func TestGoogleProviderConvertMessagesDefaultRole(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []agent.CompletionMessage{
		{
			Role:    "unknown_role", // Unknown role should default to user
			Content: "Hello",
		},
	}

	result, err := provider.convertMessages(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
}

// TestGoogleProviderCountTokensWithAllFields tests token counting with all field types.
func TestGoogleProviderCountTokensWithAllFields(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	req := &agent.CompletionRequest{
		System: "You are a helpful assistant specialized in weather forecasting.",
		Messages: []agent.CompletionMessage{
			{
				Role:    "user",
				Content: "What's the weather in London?",
			},
			{
				Role:    "assistant",
				Content: "Let me check that for you.",
				ToolCalls: []models.ToolCall{
					{
						ID:    "call_weather",
						Name:  "get_weather",
						Input: json.RawMessage(`{"city":"London","units":"celsius"}`),
					},
				},
			},
			{
				Role: "tool",
				ToolResults: []models.ToolResult{
					{
						ToolCallID: "call_weather",
						Content:    "The weather in London is 15°C with cloudy skies.",
					},
				},
			},
		},
		Tools: []agent.Tool{
			&googleMockTool{
				name:        "get_weather",
				description: "Get current weather for a city",
				schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"},"units":{"type":"string"}}}`),
			},
		},
	}

	count := provider.CountTokens(req)
	if count <= 0 {
		t.Error("expected positive token count")
	}

	// Should be a reasonable estimate for this request
	if count < 20 || count > 500 {
		t.Errorf("token count %d seems unreasonable for this request", count)
	}
}

// TestGoogleProviderConvertToolsWithEnumAndRequired tests schema with enum and required.
func TestGoogleProviderConvertToolsWithEnumAndRequired(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tools := []agent.Tool{
		&googleMockTool{
			name:        "set_temperature",
			description: "Set temperature with unit",
			schema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"temperature": {
						"type": "number",
						"description": "Temperature value"
					},
					"unit": {
						"type": "string",
						"enum": ["celsius", "fahrenheit", "kelvin"],
						"description": "Temperature unit"
					}
				},
				"required": ["temperature", "unit"]
			}`),
		},
	}

	result := provider.convertTools(tools)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result) != 1 {
		t.Errorf("expected 1 tool wrapper, got %d", len(result))
	}
}

// TestGoogleProviderConvertSchemaWithArrayItems tests array schema conversion.
func TestGoogleProviderConvertSchemaWithArrayItems(t *testing.T) {
	schemaMap := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "integer"},
			},
		},
	}

	result := toolconv.ToGeminiSchema(schemaMap)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Items == nil {
		t.Error("expected Items to be set for array type")
	}
}

// TestGoogleProviderConvertAttachmentWithMixedURLFormats tests various URL formats.
func TestGoogleProviderConvertAttachmentWithMixedURLFormats(t *testing.T) {
	provider, err := NewGoogleProvider(GoogleConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name       string
		attachment models.Attachment
		expectData bool // true if should be InlineData, false if FileData
	}{
		{
			name: "data URL with full MIME",
			attachment: models.Attachment{
				Type: "image",
				URL:  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
			},
			expectData: true,
		},
		{
			name: "regular HTTPS URL with PNG extension",
			attachment: models.Attachment{
				Type: "image",
				URL:  "https://example.com/photo.png",
			},
			expectData: false,
		},
		{
			name: "regular URL with explicit MIME type",
			attachment: models.Attachment{
				Type:     "image",
				URL:      "https://cdn.example.com/img/12345",
				MimeType: "image/webp",
			},
			expectData: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := provider.convertAttachment(tt.attachment)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectData {
				if result.InlineData == nil {
					t.Error("expected InlineData to be set")
				}
			} else {
				if result.FileData == nil {
					t.Error("expected FileData to be set")
				}
			}
		})
	}
}
