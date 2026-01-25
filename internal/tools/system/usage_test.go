package system

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/usage"
)

type mockUsageProvider struct {
	usage    *usage.ProviderUsage
	allUsage []*usage.ProviderUsage
	err      error
}

func (m *mockUsageProvider) Get(ctx context.Context, provider string) (*usage.ProviderUsage, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.usage, nil
}

func (m *mockUsageProvider) GetAll(ctx context.Context) []*usage.ProviderUsage {
	return m.allUsage
}

func TestUsageTool_Name(t *testing.T) {
	tool := NewUsageTool(nil)
	if got := tool.Name(); got != "provider_usage" {
		t.Errorf("Name() = %q, want %q", got, "provider_usage")
	}
}

func TestUsageTool_Description(t *testing.T) {
	tool := NewUsageTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() should not be empty")
	}
}

func TestUsageTool_Schema(t *testing.T) {
	tool := NewUsageTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("Schema() should not be empty")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("Schema() should be valid JSON: %v", err)
	}
}

func TestUsageTool_Execute_NilProvider(t *testing.T) {
	tool := NewUsageTool(nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("Execute() should return error when provider is nil")
	}
}

func TestUsageTool_Execute_SingleProvider(t *testing.T) {
	provider := &mockUsageProvider{
		usage: &usage.ProviderUsage{
			Provider:    "anthropic",
			TotalTokens: 1000,
			FetchedAt:   1234567890,
		},
	}
	tool := NewUsageTool(provider)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"provider": "anthropic"}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() returned error: %s", result.Content)
	}
	if result.Content == "" {
		t.Error("Execute() should return content")
	}
}

func TestUsageTool_Execute_AllProviders(t *testing.T) {
	provider := &mockUsageProvider{
		allUsage: []*usage.ProviderUsage{
			{Provider: "anthropic", TotalTokens: 1000},
			{Provider: "openai", TotalTokens: 2000},
		},
	}
	tool := NewUsageTool(provider)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() returned error: %s", result.Content)
	}
}

func TestUsageTool_Execute_NoProviders(t *testing.T) {
	provider := &mockUsageProvider{
		allUsage: []*usage.ProviderUsage{},
	}
	tool := NewUsageTool(provider)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() returned error: %s", result.Content)
	}
	if result.Content != "No provider usage data available." {
		t.Errorf("Execute() = %q, want empty message", result.Content)
	}
}
