package nodes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/edge"
)

func TestNewTool(t *testing.T) {
	tool := NewTool(nil, nil)
	if tool == nil {
		t.Error("expected non-nil tool")
	}
}

func TestTool_Name(t *testing.T) {
	tool := NewTool(nil, nil)
	if tool.Name() != "nodes" {
		t.Errorf("expected 'nodes', got %q", tool.Name())
	}
}

func TestTool_Description(t *testing.T) {
	tool := NewTool(nil, nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestTool_Schema(t *testing.T) {
	tool := NewTool(nil, nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("expected non-empty schema")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema should be valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("expected type 'object', got %v", parsed["type"])
	}
}

func TestTool_Execute_NilManager(t *testing.T) {
	tool := NewTool(nil, nil)
	params, _ := json.Marshal(map[string]interface{}{"action": "status"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil manager")
	}
	if !strings.Contains(result.Content, "unavailable") {
		t.Errorf("expected 'unavailable' in error: %s", result.Content)
	}
}

func TestTool_Execute_InvalidParams(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestTool_Execute_EmptyAction(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)
	params, _ := json.Marshal(map[string]interface{}{"action": ""})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty action")
	}
	if !strings.Contains(result.Content, "required") {
		t.Errorf("expected 'required' in error: %s", result.Content)
	}
}

func TestTool_Execute_UnsupportedAction(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)
	params, _ := json.Marshal(map[string]interface{}{"action": "invalid"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unsupported action")
	}
	if !strings.Contains(result.Content, "unsupported") {
		t.Errorf("expected 'unsupported' in error: %s", result.Content)
	}
}

func TestNodesToolStatusEmpty(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action": "status",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "\"edges\"") {
		t.Fatalf("expected edges field, got %s", result.Content)
	}
}

func TestNodesToolList(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "edges") {
		t.Fatalf("expected edges field, got %s", result.Content)
	}
}

func TestNodesToolDescribeMissingEdgeID(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action": "describe",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing edge_id")
	}
	if !strings.Contains(result.Content, "edge_id") {
		t.Errorf("expected 'edge_id' in error: %s", result.Content)
	}
}

func TestNodesToolDescribeMissing(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action":  "describe",
		"edge_id": "missing",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' in error: %s", result.Content)
	}
}

func TestNodesToolPendingUnsupported(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action": "pending",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "unsupported") || !strings.Contains(result.Content, "tofu") {
		t.Errorf("expected tofu-related error: %s", result.Content)
	}
}

func TestNodesToolApproveUnsupported(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action":  "approve",
		"edge_id": "test-edge",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error without tofu auth")
	}
}

func TestNodesToolRejectUnsupported(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action":  "reject",
		"edge_id": "test-edge",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error without tofu auth")
	}
}

func TestNodesToolInvokeMissingEdgeID(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action": "invoke",
		"tool":   "test_tool",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing edge_id")
	}
}

func TestNodesToolInvokeMissingTool(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action":  "invoke",
		"edge_id": "test-edge",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing tool")
	}
	if !strings.Contains(result.Content, "tool") {
		t.Errorf("expected 'tool' in error: %s", result.Content)
	}
}

func TestNodesToolInvokeEdgeNotFound(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action":  "invoke",
		"edge_id": "nonexistent",
		"tool":    "test_tool",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent edge")
	}
}

func TestNodesToolActionCaseInsensitive(t *testing.T) {
	manager := edge.NewManager(edge.DefaultManagerConfig(), edge.NewDevAuthenticator(), nil)
	tool := NewTool(manager, nil)

	for _, action := range []string{"STATUS", "Status", "LIST", "List"} {
		params, _ := json.Marshal(map[string]interface{}{"action": action})
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("execute with action %q: %v", action, err)
		}
		if result.IsError {
			t.Errorf("action %q should not error: %s", action, result.Content)
		}
	}
}
