package system

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/commands"
)

type mockHealthProvider struct {
	summary *commands.HealthSummary
	err     error
}

func (m *mockHealthProvider) Check(ctx context.Context, opts *commands.HealthCheckOptions) (*commands.HealthSummary, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.summary, nil
}

func TestHealthTool_Name(t *testing.T) {
	tool := NewHealthTool(nil)
	if got := tool.Name(); got != "system_health" {
		t.Errorf("Name() = %q, want %q", got, "system_health")
	}
}

func TestHealthTool_Description(t *testing.T) {
	tool := NewHealthTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() should not be empty")
	}
}

func TestHealthTool_Schema(t *testing.T) {
	tool := NewHealthTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("Schema() should not be empty")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("Schema() should be valid JSON: %v", err)
	}
}

func TestHealthTool_Execute_NilProvider(t *testing.T) {
	tool := NewHealthTool(nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("Execute() should return error when provider is nil")
	}
}

func TestHealthTool_Execute_Success(t *testing.T) {
	provider := &mockHealthProvider{
		summary: &commands.HealthSummary{
			OK:         true,
			Ts:         1234567890,
			DurationMs: 100,
		},
	}
	tool := NewHealthTool(provider)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"probe_channels": false}`))
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
