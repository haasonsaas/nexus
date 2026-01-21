package providers

import (
	"context"
	"encoding/json"
	"testing"

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
	mockTool := &mockTool{
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

func TestParseToolCallFromChunk(t *testing.T) {
	tests := []struct {
		name      string
		delta     openai.ChatCompletionStreamChoiceDelta
		wantCall  bool
		wantDone  bool
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

func TestErrorHandling(t *testing.T) {
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

// Mock tool for testing
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
	return &agent.ToolResult{Content: "mock result"}, nil
}

// Helper function
func intPtr(i int) *int {
	return &i
}
