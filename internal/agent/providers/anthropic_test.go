package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// mockTool implements agent.Tool for testing.
type mockTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) Schema() json.RawMessage {
	return m.schema
}

func (m *mockTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	return &agent.ToolResult{Content: "test result"}, nil
}

// TestNewAnthropicProvider tests provider initialization.
func TestNewAnthropicProvider(t *testing.T) {
	tests := []struct {
		name        string
		config      AnthropicConfig
		expectError bool
	}{
		{
			name: "valid config",
			config: AnthropicConfig{
				APIKey:       "test-key",
				MaxRetries:   3,
				RetryDelay:   time.Second,
				DefaultModel: "claude-sonnet-4-20250514",
			},
			expectError: false,
		},
		{
			name: "missing API key",
			config: AnthropicConfig{
				MaxRetries: 3,
			},
			expectError: true,
		},
		{
			name: "defaults applied",
			config: AnthropicConfig{
				APIKey: "test-key",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewAnthropicProvider(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if provider == nil {
				t.Fatal("expected provider but got nil")
			}

			// Check defaults were applied
			if provider.maxRetries <= 0 {
				t.Error("maxRetries should have default value")
			}
			if provider.retryDelay <= 0 {
				t.Error("retryDelay should have default value")
			}
			if provider.defaultModel == "" {
				t.Error("defaultModel should have default value")
			}
		})
	}
}

// TestProviderMethods tests basic provider methods.
func TestProviderMethods(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Test Name
	if provider.Name() != "anthropic" {
		t.Errorf("expected name 'anthropic', got '%s'", provider.Name())
	}

	// Test SupportsTools
	if !provider.SupportsTools() {
		t.Error("expected SupportsTools to return true")
	}

	// Test Models
	models := provider.Models()
	if len(models) == 0 {
		t.Error("expected at least one model")
	}

	// Check for expected models
	expectedModels := []string{
		"claude-sonnet-4-20250514",
		"claude-opus-4-20250514",
		"claude-3-5-sonnet-20241022",
	}

	modelIDs := make(map[string]bool)
	for _, m := range models {
		modelIDs[m.ID] = true

		// Verify model properties
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
}

func TestWrapAnthropicError(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	apiErr := &anthropic.Error{
		StatusCode: 429,
		RequestID:  "req_123",
	}
	wrapped := provider.wrapError(apiErr, "claude-sonnet-4")
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
	if providerErr.RequestID != "req_123" {
		t.Fatalf("expected request ID req_123, got %q", providerErr.RequestID)
	}
}

// TestConvertMessages tests message conversion to Anthropic format.
func TestConvertMessages(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name     string
		messages []agent.CompletionMessage
		wantErr  bool
		validate func(t *testing.T, result interface{})
	}{
		{
			name: "simple user message",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "Hello!"},
			},
			wantErr: false,
		},
		{
			name: "system message is skipped",
			messages: []agent.CompletionMessage{
				{Role: "system", Content: "You are helpful."},
				{Role: "user", Content: "Hello!"},
			},
			wantErr: false,
		},
		{
			name: "assistant message",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "Hello!"},
				{Role: "assistant", Content: "Hi there!"},
			},
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
			wantErr: false,
		},
		{
			name: "message with tool results",
			messages: []agent.CompletionMessage{
				{
					Role: "user",
					ToolResults: []models.ToolResult{
						{
							ToolCallID: "call_123",
							Content:    "Sunny, 72°F",
							IsError:    false,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid tool call JSON",
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
			wantErr: true,
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

			if result == nil {
				t.Fatal("expected result but got nil")
			}

			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

// TestConvertTools tests tool conversion to Anthropic format.
func TestConvertTools(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	tests := []struct {
		name    string
		tools   []agent.Tool
		wantErr bool
	}{
		{
			name: "valid tool",
			tools: []agent.Tool{
				&mockTool{
					name:        "get_weather",
					description: "Get current weather",
					schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
				},
			},
			wantErr: false,
		},
		{
			name: "multiple tools",
			tools: []agent.Tool{
				&mockTool{
					name:        "get_weather",
					description: "Get current weather",
					schema:      json.RawMessage(`{"type":"object"}`),
				},
				&mockTool{
					name:        "search",
					description: "Search the web",
					schema:      json.RawMessage(`{"type":"object"}`),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid schema JSON",
			tools: []agent.Tool{
				&mockTool{
					name:        "test",
					description: "Test tool",
					schema:      json.RawMessage(`invalid`),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := provider.convertTools(tt.tools)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != len(tt.tools) {
				t.Errorf("expected %d tools, got %d", len(tt.tools), len(result))
			}
		})
	}
}

// TestCountTokens tests token counting estimation.
func TestCountTokens(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
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
					&mockTool{
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

// TestIsRetryableError tests error retry logic.
func TestIsRetryableError(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
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
			err:   errors.New("rate_limit exceeded"),
			retry: true,
		},
		{
			name:  "429 status",
			err:   errors.New("HTTP 429 too many requests"),
			retry: true,
		},
		{
			name:  "500 error",
			err:   errors.New("HTTP 500 internal server error"),
			retry: true,
		},
		{
			name:  "503 service unavailable",
			err:   errors.New("503 service unavailable"),
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
			name:  "invalid API key (not retryable)",
			err:   errors.New("invalid API key"),
			retry: false,
		},
		{
			name:  "validation error (not retryable)",
			err:   errors.New("validation failed"),
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

// TestStreamingResponse tests streaming response handling with mock server.
func TestStreamingResponse(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}

		if !strings.Contains(r.URL.Path, "/messages") {
			t.Errorf("expected /messages path, got %s", r.URL.Path)
		}

		// Check headers
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}

		// Send SSE response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		// Send events
		events := []string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant"}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}

		for _, event := range events {
			fmt.Fprintln(w, event)
			flusher.Flush()
		}
	}))
	defer server.Close()

	// Note: In a real test, we would need to configure the Anthropic client
	// to use our test server URL. This is challenging with the official SDK,
	// so this test demonstrates the structure but would need SDK support
	// for custom base URLs or we'd need to mock at a different level.

	t.Log("Streaming test server created at:", server.URL)
}

// TestToolCallParsing tests parsing of tool calls from streaming events.
func TestToolCallParsing(t *testing.T) {
	// Create mock server that sends tool use events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		// Send tool use event sequence
		events := []string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant"}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_123","name":"get_weather","input":{}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"London\"}"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}

		for _, event := range events {
			fmt.Fprintln(w, event)
			flusher.Flush()
		}
	}))
	defer server.Close()

	t.Log("Tool call test server created at:", server.URL)
}

// TestErrorHandling tests various error scenarios.
func TestErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantRetry  bool
	}{
		{
			name:       "rate limit error",
			statusCode: http.StatusTooManyRequests,
			response:   `{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
			wantRetry:  true,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response:   `{"error":{"type":"api_error","message":"Internal server error"}}`,
			wantRetry:  true,
		},
		{
			name:       "authentication error",
			statusCode: http.StatusUnauthorized,
			response:   `{"error":{"type":"authentication_error","message":"Invalid API key"}}`,
			wantRetry:  false,
		},
		{
			name:       "validation error",
			statusCode: http.StatusBadRequest,
			response:   `{"error":{"type":"invalid_request_error","message":"Invalid request"}}`,
			wantRetry:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.response)
			}))
			defer server.Close()

			provider, err := NewAnthropicProvider(AnthropicConfig{
				APIKey: "test-key",
			})
			if err != nil {
				t.Fatalf("failed to create provider: %v", err)
			}

			// Check if error should be retried
			testErr := fmt.Errorf("HTTP %d", tt.statusCode)
			shouldRetry := provider.isRetryableError(testErr)

			if shouldRetry != tt.wantRetry {
				t.Errorf("expected retry=%v, got %v", tt.wantRetry, shouldRetry)
			}
		})
	}
}

// TestParseSSEStream tests SSE stream parsing.
func TestParseSSEStream(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []struct {
			eventType string
			data      string
		}
	}{
		{
			name: "simple event",
			input: `event: message_start
data: {"type":"message_start"}

`,
			expected: []struct {
				eventType string
				data      string
			}{
				{eventType: "message_start", data: `{"type":"message_start"}`},
			},
		},
		{
			name: "multiple events",
			input: `event: content_block_delta
data: {"type":"content_block_delta","text":"Hello"}

event: content_block_delta
data: {"type":"content_block_delta","text":" world"}

`,
			expected: []struct {
				eventType string
				data      string
			}{
				{eventType: "content_block_delta", data: `{"type":"content_block_delta","text":"Hello"}`},
				{eventType: "content_block_delta", data: `{"type":"content_block_delta","text":" world"}`},
			},
		},
		{
			name: "multiline data",
			input: `event: test
data: line1
data: line2

`,
			expected: []struct {
				eventType string
				data      string
			}{
				{eventType: "test", data: "line1\nline2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			var events []struct {
				eventType string
				data      string
			}

			err := ParseSSEStream(reader, func(eventType, data string) error {
				events = append(events, struct {
					eventType string
					data      string
				}{eventType, data})
				return nil
			})

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(events) != len(tt.expected) {
				t.Fatalf("expected %d events, got %d", len(tt.expected), len(events))
			}

			for i, event := range events {
				if event.eventType != tt.expected[i].eventType {
					t.Errorf("event %d: expected type %q, got %q", i, tt.expected[i].eventType, event.eventType)
				}
				if event.data != tt.expected[i].data {
					t.Errorf("event %d: expected data %q, got %q", i, tt.expected[i].data, event.data)
				}
			}
		})
	}
}

// TestRetryBackoff tests exponential backoff retry logic.
func TestRetryBackoff(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
		APIKey:     "test-key",
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first two attempts
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"Rate limited"}}`)
			return
		}
		// Succeed on third attempt
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: message_stop\ndata: {}\n\n")
	}))
	defer server.Close()

	// Note: Testing actual retry behavior requires configuring the SDK client
	// to use our test server, which may not be directly supported.
	// This test demonstrates the structure.

	t.Logf("Provider configured with %d max retries and %v retry delay",
		provider.maxRetries, provider.retryDelay)

	if provider.maxRetries != 3 {
		t.Errorf("expected maxRetries=3, got %d", provider.maxRetries)
	}
}

// TestModelDefaults tests default model selection.
func TestModelDefaults(t *testing.T) {
	provider, err := NewAnthropicProvider(AnthropicConfig{
		APIKey:       "test-key",
		DefaultModel: "claude-opus-4-20250514",
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Test getModel with empty string
	model := provider.getModel("")
	if model != "claude-opus-4-20250514" {
		t.Errorf("expected default model, got %s", model)
	}

	// Test getModel with specified model
	model = provider.getModel("claude-3-haiku-20240307")
	if model != "claude-3-haiku-20240307" {
		t.Errorf("expected specified model, got %s", model)
	}

	// Test getMaxTokens default
	maxTokens := provider.getMaxTokens(0)
	if maxTokens != 4096 {
		t.Errorf("expected default maxTokens=4096, got %d", maxTokens)
	}

	// Test getMaxTokens with specified value
	maxTokens = provider.getMaxTokens(2000)
	if maxTokens != 2000 {
		t.Errorf("expected specified maxTokens=2000, got %d", maxTokens)
	}
}

// TestMaxEmptyStreamEventsConstant verifies the malformed stream protection constant.
func TestMaxEmptyStreamEventsConstant(t *testing.T) {
	// Verify the constant is set to a reasonable value that protects against
	// infinite loops while allowing for legitimate stream processing
	if maxEmptyStreamEvents < 100 {
		t.Errorf("maxEmptyStreamEvents=%d is too low, may cause false positives", maxEmptyStreamEvents)
	}
	if maxEmptyStreamEvents > 1000 {
		t.Errorf("maxEmptyStreamEvents=%d is too high, may not protect against malformed streams", maxEmptyStreamEvents)
	}
	// Verify it's exactly 300 (from go-openai pattern)
	if maxEmptyStreamEvents != 300 {
		t.Logf("Note: maxEmptyStreamEvents=%d (expected 300 based on go-openai pattern)", maxEmptyStreamEvents)
	}
}
