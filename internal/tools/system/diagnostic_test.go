package system

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/infra"
)

type mockDiagnosticProvider struct {
	activityStats   channels.ActivityStats
	migrationStatus struct {
		current int
		latest  int
		pending int
		err     error
	}
}

func (m *mockDiagnosticProvider) GetActivityStats() channels.ActivityStats {
	return m.activityStats
}

func (m *mockDiagnosticProvider) GetMigrationStatus() (current, latest infra.MigrationVersion, pending int, err error) {
	return infra.MigrationVersion(m.migrationStatus.current),
		infra.MigrationVersion(m.migrationStatus.latest),
		m.migrationStatus.pending,
		m.migrationStatus.err
}

func TestDiagnosticTool_Name(t *testing.T) {
	tool := NewDiagnosticTool(nil)
	if got := tool.Name(); got != "system_diagnostic" {
		t.Errorf("Name() = %q, want %q", got, "system_diagnostic")
	}
}

func TestDiagnosticTool_Description(t *testing.T) {
	tool := NewDiagnosticTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() should not be empty")
	}
}

func TestDiagnosticTool_Schema(t *testing.T) {
	tool := NewDiagnosticTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("Schema() should not be empty")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("Schema() should be valid JSON: %v", err)
	}
}

func TestDiagnosticTool_Execute_NilProvider(t *testing.T) {
	tool := NewDiagnosticTool(nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("Execute() should return error when provider is nil")
	}
}

func TestDiagnosticTool_Execute_AllSections(t *testing.T) {
	provider := &mockDiagnosticProvider{
		activityStats: channels.ActivityStats{
			TotalChannels:  5,
			TotalInbound:   100,
			TotalOutbound:  50,
			RecentInbound:  10,
			RecentOutbound: 5,
			ByChannel:      map[string]int{"telegram": 3, "slack": 2},
		},
		migrationStatus: struct {
			current int
			latest  int
			pending int
			err     error
		}{current: 1, latest: 2, pending: 1, err: nil},
	}
	tool := NewDiagnosticTool(provider)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"section": "all"}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() returned error: %s", result.Content)
	}
	if result.Content == "" {
		t.Error("Execute() should return content")
	}

	// Verify JSON structure
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Errorf("Execute() result should be valid JSON: %v", err)
	}
	if _, ok := parsed["activity"]; !ok {
		t.Error("Execute() result should contain activity section")
	}
	if _, ok := parsed["migrations"]; !ok {
		t.Error("Execute() result should contain migrations section")
	}
}

func TestDiagnosticTool_Execute_ActivityOnly(t *testing.T) {
	provider := &mockDiagnosticProvider{
		activityStats: channels.ActivityStats{
			TotalChannels: 3,
		},
	}
	tool := NewDiagnosticTool(provider)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"section": "activity"}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() returned error: %s", result.Content)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Errorf("Execute() result should be valid JSON: %v", err)
	}
	if _, ok := parsed["activity"]; !ok {
		t.Error("Execute() result should contain activity section")
	}
	if _, ok := parsed["migrations"]; ok {
		t.Error("Execute() result should not contain migrations section")
	}
}

func TestDiagnosticTool_Execute_MigrationsOnly(t *testing.T) {
	provider := &mockDiagnosticProvider{
		migrationStatus: struct {
			current int
			latest  int
			pending int
			err     error
		}{current: 1, latest: 1, pending: 0, err: nil},
	}
	tool := NewDiagnosticTool(provider)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"section": "migrations"}`))
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() returned error: %s", result.Content)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Errorf("Execute() result should be valid JSON: %v", err)
	}
	if _, ok := parsed["migrations"]; !ok {
		t.Error("Execute() result should contain migrations section")
	}
	if _, ok := parsed["activity"]; ok {
		t.Error("Execute() result should not contain activity section")
	}
}
