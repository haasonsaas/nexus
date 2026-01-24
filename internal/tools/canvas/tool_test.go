package canvas

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	canvascore "github.com/haasonsaas/nexus/internal/canvas"
	"github.com/haasonsaas/nexus/internal/config"
)

func TestNewTool(t *testing.T) {
	tool := NewTool(nil, nil)
	if tool == nil {
		t.Error("expected non-nil tool")
	}
}

func TestTool_Name(t *testing.T) {
	tool := NewTool(nil, nil)
	if tool.Name() != "canvas" {
		t.Errorf("expected 'canvas', got %q", tool.Name())
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

func TestTool_Execute_InvalidParams(t *testing.T) {
	tool := NewTool(nil, nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestTool_Execute_EmptyAction(t *testing.T) {
	tool := NewTool(nil, nil)
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
	tool := NewTool(nil, nil)
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

func TestTool_Execute_URL_NilHost(t *testing.T) {
	tool := NewTool(nil, nil)
	params, _ := json.Marshal(map[string]interface{}{"action": "url"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil host")
	}
	if !strings.Contains(result.Content, "unavailable") {
		t.Errorf("expected 'unavailable' in error: %s", result.Content)
	}
}

func TestTool_Execute_Push_NilManager(t *testing.T) {
	tool := NewTool(nil, nil)
	params, _ := json.Marshal(map[string]interface{}{
		"action":     "push",
		"session_id": "test",
		"payload":    map[string]interface{}{"data": "test"},
	})
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

func TestTool_Execute_Push_MissingSessionID(t *testing.T) {
	store := canvascore.NewMemoryStore()
	manager := canvascore.NewManager(store, nil)
	tool := NewTool(nil, manager)
	params, _ := json.Marshal(map[string]interface{}{
		"action":  "push",
		"payload": map[string]interface{}{"data": "test"},
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
	if !strings.Contains(result.Content, "session_id") {
		t.Errorf("expected 'session_id' in error: %s", result.Content)
	}
}

func TestTool_Execute_Push_MissingPayload(t *testing.T) {
	store := canvascore.NewMemoryStore()
	manager := canvascore.NewManager(store, nil)
	tool := NewTool(nil, manager)
	params, _ := json.Marshal(map[string]interface{}{
		"action":     "push",
		"session_id": "test",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing payload")
	}
	if !strings.Contains(result.Content, "payload") {
		t.Errorf("expected 'payload' in error: %s", result.Content)
	}
}

func TestTool_Execute_Reset_MissingState(t *testing.T) {
	store := canvascore.NewMemoryStore()
	manager := canvascore.NewManager(store, nil)
	tool := NewTool(nil, manager)
	params, _ := json.Marshal(map[string]interface{}{
		"action":     "reset",
		"session_id": "test",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing state")
	}
	if !strings.Contains(result.Content, "state") {
		t.Errorf("expected 'state' in error: %s", result.Content)
	}
}

func TestTool_Execute_Snapshot_MissingSessionID(t *testing.T) {
	store := canvascore.NewMemoryStore()
	manager := canvascore.NewManager(store, nil)
	tool := NewTool(nil, manager)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "snapshot",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestCanvasToolURL(t *testing.T) {
	root := t.TempDir()
	cfg := config.CanvasHostConfig{
		Host: "127.0.0.1",
		Port: 18793,
		Root: root,
	}
	host, err := canvascore.NewHost(cfg, config.CanvasConfig{}, nil)
	if err != nil {
		t.Fatalf("host: %v", err)
	}

	tool := NewTool(host, nil)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "url",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Content, ":18793") {
		t.Fatalf("expected url, got %s", result.Content)
	}
}

func TestCanvasToolURL_WithPath(t *testing.T) {
	root := t.TempDir()
	cfg := config.CanvasHostConfig{
		Host: "127.0.0.1",
		Port: 18793,
		Root: root,
	}
	host, err := canvascore.NewHost(cfg, config.CanvasConfig{}, nil)
	if err != nil {
		t.Fatalf("host: %v", err)
	}

	tool := NewTool(host, nil)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "url",
		"path":   "/subpath/file.html",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "subpath") {
		t.Fatalf("expected path in url, got %s", result.Content)
	}
}

func TestCanvasToolPresent(t *testing.T) {
	root := t.TempDir()
	cfg := config.CanvasHostConfig{
		Host: "127.0.0.1",
		Port: 18793,
		Root: root,
	}
	host, err := canvascore.NewHost(cfg, config.CanvasConfig{}, nil)
	if err != nil {
		t.Fatalf("host: %v", err)
	}

	tool := NewTool(host, nil)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "present",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "url") {
		t.Fatalf("expected url in result: %s", result.Content)
	}
}

func TestCanvasToolPushSnapshot(t *testing.T) {
	ctx := context.Background()
	store := canvascore.NewMemoryStore()
	manager := canvascore.NewManager(store, nil)

	session := &canvascore.Session{
		Key:         "slack:workspace:channel",
		WorkspaceID: "workspace",
		ChannelID:   "channel",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	tool := NewTool(nil, manager)
	pushParams, _ := json.Marshal(map[string]interface{}{
		"action":     "push",
		"session_id": session.ID,
		"payload": map[string]interface{}{
			"status": "ok",
		},
	})
	result, err := tool.Execute(ctx, pushParams)
	if err != nil {
		t.Fatalf("push execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("push error: %s", result.Content)
	}

	snapshotParams, _ := json.Marshal(map[string]interface{}{
		"action":     "snapshot",
		"session_id": session.ID,
	})
	snapshot, err := tool.Execute(ctx, snapshotParams)
	if err != nil {
		t.Fatalf("snapshot execute: %v", err)
	}
	if snapshot.IsError {
		t.Fatalf("snapshot error: %s", snapshot.Content)
	}
	var parsed struct {
		Events []struct{} `json:"events"`
	}
	if err := json.Unmarshal([]byte(snapshot.Content), &parsed); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if len(parsed.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(parsed.Events))
	}
}
