package canvas

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	canvascore "github.com/haasonsaas/nexus/internal/canvas"
	"github.com/haasonsaas/nexus/internal/config"
)

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
