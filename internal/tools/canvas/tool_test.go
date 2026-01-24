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
	host, err := canvascore.NewHost(cfg, nil)
	if err != nil {
		t.Fatalf("host: %v", err)
	}

	tool := NewTool(host)
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
