package message

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/channels"
	sessionstore "github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubAdapter struct {
	sent []*models.Message
}

func (a *stubAdapter) Type() models.ChannelType { return models.ChannelTelegram }

func (a *stubAdapter) Send(ctx context.Context, msg *models.Message) error {
	a.sent = append(a.sent, msg)
	return nil
}

func TestMessageToolSend(t *testing.T) {
	registry := channels.NewRegistry()
	adapter := &stubAdapter{}
	registry.Register(adapter)
	store := sessionstore.NewMemoryStore()

	tool := NewTool("message", registry, store, "main")
	params, _ := json.Marshal(map[string]interface{}{
		"channel": "telegram",
		"to":      "123",
		"content": "hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if len(adapter.sent) != 1 {
		t.Fatalf("expected send, got %d", len(adapter.sent))
	}
	if !strings.Contains(result.Content, "sent") {
		t.Fatalf("expected result status: %s", result.Content)
	}
}
