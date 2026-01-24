package nodes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/edge"
)

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
}
