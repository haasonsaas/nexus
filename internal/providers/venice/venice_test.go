package venice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-api-key")
	if client == nil {
		t.Fatal("expected client, got nil")
	}
	if client.apiKey != "test-api-key" {
		t.Errorf("expected apiKey 'test-api-key', got %q", client.apiKey)
	}
	if client.baseURL != BaseURL {
		t.Errorf("expected baseURL %q, got %q", BaseURL, client.baseURL)
	}
	if client.defaultModel != DefaultModel {
		t.Errorf("expected defaultModel %q, got %q", DefaultModel, client.defaultModel)
	}
}

func TestNewClientWithConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         VeniceConfig
		wantBaseURL string
		wantModel   string
		wantRetries int
		wantDelay   time.Duration
	}{
		{
			name: "default values",
			cfg: VeniceConfig{
				APIKey: "test-key",
			},
			wantBaseURL: BaseURL,
			wantModel:   DefaultModel,
			wantRetries: 3,
			wantDelay:   time.Second,
		},
		{
			name: "custom values",
			cfg: VeniceConfig{
				APIKey:       "test-key",
				BaseURL:      "https://custom.api.com/v1",
				DefaultModel: "custom-model",
				MaxRetries:   5,
				RetryDelay:   2 * time.Second,
			},
			wantBaseURL: "https://custom.api.com/v1",
			wantModel:   "custom-model",
			wantRetries: 5,
			wantDelay:   2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClientWithConfig(tt.cfg)
			if client.baseURL != tt.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", client.baseURL, tt.wantBaseURL)
			}
			if client.defaultModel != tt.wantModel {
				t.Errorf("defaultModel = %q, want %q", client.defaultModel, tt.wantModel)
			}
			if client.maxRetries != tt.wantRetries {
				t.Errorf("maxRetries = %d, want %d", client.maxRetries, tt.wantRetries)
			}
			if client.retryDelay != tt.wantDelay {
				t.Errorf("retryDelay = %v, want %v", client.retryDelay, tt.wantDelay)
			}
		})
	}
}

func TestNewVeniceProvider(t *testing.T) {
	tests := []struct {
		name    string
		cfg     VeniceConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: VeniceConfig{
				APIKey: "test-api-key",
			},
			wantErr: false,
		},
		{
			name:    "missing API key",
			cfg:     VeniceConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewVeniceProvider(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewVeniceProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && provider == nil {
				t.Error("expected provider, got nil")
			}
		})
	}
}

func TestVeniceProviderName(t *testing.T) {
	provider, err := NewVeniceProvider(VeniceConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if name := provider.Name(); name != "venice" {
		t.Errorf("Name() = %q, want %q", name, "venice")
	}
}

func TestVeniceProviderSupportsTools(t *testing.T) {
	provider, err := NewVeniceProvider(VeniceConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if !provider.SupportsTools() {
		t.Error("SupportsTools() = false, want true")
	}
}

func TestVeniceProviderModels(t *testing.T) {
	provider, err := NewVeniceProvider(VeniceConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	models := provider.Models()
	if len(models) == 0 {
		t.Error("Models() returned empty slice")
	}

	// Check that known models are present
	foundLlama := false
	foundClaude := false
	for _, m := range models {
		if m.ID == "llama-3.3-70b" {
			foundLlama = true
			if m.ContextSize != 131072 {
				t.Errorf("llama-3.3-70b context size = %d, want 131072", m.ContextSize)
			}
		}
		if m.ID == "claude-opus-45" {
			foundClaude = true
			if !m.SupportsVision {
				t.Error("claude-opus-45 should support vision")
			}
		}
	}

	if !foundLlama {
		t.Error("llama-3.3-70b not found in models")
	}
	if !foundClaude {
		t.Error("claude-opus-45 not found in models")
	}
}

func TestVeniceCatalog(t *testing.T) {
	if len(VeniceCatalog) == 0 {
		t.Error("VeniceCatalog is empty")
	}

	// Check each catalog entry has required fields
	for _, entry := range VeniceCatalog {
		if entry.ID == "" {
			t.Error("catalog entry has empty ID")
		}
		if entry.Name == "" {
			t.Errorf("catalog entry %q has empty Name", entry.ID)
		}
		if len(entry.Input) == 0 {
			t.Errorf("catalog entry %q has no input types", entry.ID)
		}
		if entry.ContextWindow <= 0 {
			t.Errorf("catalog entry %q has invalid ContextWindow: %d", entry.ID, entry.ContextWindow)
		}
		if entry.MaxTokens <= 0 {
			t.Errorf("catalog entry %q has invalid MaxTokens: %d", entry.ID, entry.MaxTokens)
		}
		if entry.Privacy != "private" && entry.Privacy != "anonymized" {
			t.Errorf("catalog entry %q has invalid Privacy: %q", entry.ID, entry.Privacy)
		}
	}
}

func TestGetModelInfo(t *testing.T) {
	tests := []struct {
		modelID     string
		wantNil     bool
		wantPrivacy string
	}{
		{
			modelID:     "llama-3.3-70b",
			wantNil:     false,
			wantPrivacy: "private",
		},
		{
			modelID:     "claude-opus-45",
			wantNil:     false,
			wantPrivacy: "anonymized",
		},
		{
			modelID: "nonexistent-model",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			info := GetModelInfo(tt.modelID)
			if (info == nil) != tt.wantNil {
				t.Errorf("GetModelInfo(%q) = %v, wantNil %v", tt.modelID, info, tt.wantNil)
				return
			}
			if !tt.wantNil && info.Privacy != tt.wantPrivacy {
				t.Errorf("GetModelInfo(%q).Privacy = %q, want %q", tt.modelID, info.Privacy, tt.wantPrivacy)
			}
		})
	}
}

func TestIsPrivateModel(t *testing.T) {
	tests := []struct {
		modelID     string
		wantPrivate bool
	}{
		{"llama-3.3-70b", true},
		{"deepseek-v3.2", true},
		{"claude-opus-45", false},
		{"openai-gpt-52", false},
		{"nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			if got := IsPrivateModel(tt.modelID); got != tt.wantPrivate {
				t.Errorf("IsPrivateModel(%q) = %v, want %v", tt.modelID, got, tt.wantPrivate)
			}
		})
	}
}

func TestSupportsReasoning(t *testing.T) {
	tests := []struct {
		modelID       string
		wantReasoning bool
	}{
		{"llama-3.3-70b", false},
		{"qwen3-235b-a22b-thinking-2507", true},
		{"deepseek-v3.2", true},
		{"claude-opus-45", true},
		{"nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			if got := SupportsReasoning(tt.modelID); got != tt.wantReasoning {
				t.Errorf("SupportsReasoning(%q) = %v, want %v", tt.modelID, got, tt.wantReasoning)
			}
		})
	}
}

func TestConvertMessages(t *testing.T) {
	client := NewClient("test-key")

	tests := []struct {
		name     string
		messages []agent.CompletionMessage
		system   string
		wantLen  int
	}{
		{
			name: "basic text messages",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
			},
			system:  "You are helpful",
			wantLen: 3, // system + 2 messages
		},
		{
			name: "no system message",
			messages: []agent.CompletionMessage{
				{Role: "user", Content: "Hello"},
			},
			system:  "",
			wantLen: 1,
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
		},
		{
			name: "message with image attachment",
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.convertMessages(tt.messages, tt.system)
			if err != nil {
				t.Errorf("convertMessages() error = %v", err)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("convertMessages() got %d messages, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestConvertTools(t *testing.T) {
	client := NewClient("test-key")

	mockTool := &mockTool{
		name:        "test_tool",
		description: "A test tool",
		schema:      json.RawMessage(`{"type":"object","properties":{"arg":{"type":"string"}}}`),
	}

	tools := []agent.Tool{mockTool}
	got := client.convertTools(tools)

	if len(got) != 1 {
		t.Errorf("convertTools() got %d tools, want 1", len(got))
		return
	}

	if got[0].Function.Name != "test_tool" {
		t.Errorf("tool name = %q, want %q", got[0].Function.Name, "test_tool")
	}
	if got[0].Function.Description != "A test tool" {
		t.Errorf("tool description = %q, want %q", got[0].Function.Description, "A test tool")
	}
}

func TestIsRetryableError(t *testing.T) {
	client := NewClient("test-key")

	tests := []struct {
		name      string
		errMsg    string
		wantRetry bool
	}{
		{"rate limit", "rate limit exceeded", true},
		{"429 error", "error: 429 too many requests", true},
		{"500 error", "internal server error 500", true},
		{"502 error", "bad gateway 502", true},
		{"503 error", "service unavailable 503", true},
		{"504 error", "gateway timeout 504", true},
		{"timeout", "request timeout", true},
		{"deadline", "context deadline exceeded", true},
		{"auth error", "invalid API key", false},
		{"not found", "model not found", false},
		{"nil error", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = &mockError{msg: tt.errMsg}
			}
			if got := client.isRetryableError(err); got != tt.wantRetry {
				t.Errorf("isRetryableError(%q) = %v, want %v", tt.errMsg, got, tt.wantRetry)
			}
		})
	}
}

func TestDiscoverModels(t *testing.T) {
	// Test with empty API key - should return static catalog
	ctx := context.Background()
	models, err := DiscoverModels(ctx, "")
	if err != nil {
		t.Errorf("DiscoverModels() error = %v", err)
		return
	}
	if len(models) != len(VeniceCatalog) {
		t.Errorf("DiscoverModels() returned %d models, want %d", len(models), len(VeniceCatalog))
	}
}

func TestCompleteWithoutAPIKey(t *testing.T) {
	client := NewClientWithConfig(VeniceConfig{})

	req := &CompletionRequest{
		Model: "llama-3.3-70b",
		Messages: []agent.CompletionMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	_, err := client.Complete(context.Background(), req)
	if err == nil {
		t.Error("Complete() should fail without API key")
	}
}

// mockTool implements agent.Tool for testing
type mockTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (m *mockTool) Name() string            { return m.name }
func (m *mockTool) Description() string     { return m.description }
func (m *mockTool) Schema() json.RawMessage { return m.schema }
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (*agent.ToolResult, error) {
	return &agent.ToolResult{Content: ""}, nil
}

// mockError implements error for testing
type mockError struct {
	msg string
}

func (e *mockError) Error() string { return e.msg }
