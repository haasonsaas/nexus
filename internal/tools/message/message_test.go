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

func TestNewTool(t *testing.T) {
	registry := channels.NewRegistry()
	store := sessionstore.NewMemoryStore()

	tool := NewTool("", registry, store, "")
	if tool.Name() != "message" {
		t.Errorf("expected default name 'message', got %q", tool.Name())
	}
	if tool.defaultAgent != "main" {
		t.Errorf("expected default agent 'main', got %q", tool.defaultAgent)
	}
}

func TestNewTool_CustomName(t *testing.T) {
	tool := NewTool("send_message", nil, nil, "custom_agent")
	if tool.Name() != "send_message" {
		t.Errorf("expected 'send_message', got %q", tool.Name())
	}
	if tool.defaultAgent != "custom_agent" {
		t.Errorf("expected 'custom_agent', got %q", tool.defaultAgent)
	}
}

func TestTool_Description(t *testing.T) {
	tool := NewTool("message", nil, nil, "")
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestTool_Schema(t *testing.T) {
	tool := NewTool("message", nil, nil, "")
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

func TestTool_Execute_NilRegistry(t *testing.T) {
	tool := NewTool("message", nil, nil, "")
	params, _ := json.Marshal(map[string]interface{}{
		"channel": "telegram",
		"to":      "123",
		"content": "hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil registry")
	}
	if !strings.Contains(result.Content, "unavailable") {
		t.Errorf("expected 'unavailable' in error: %s", result.Content)
	}
}

func TestTool_Execute_InvalidParams(t *testing.T) {
	registry := channels.NewRegistry()
	tool := NewTool("message", registry, nil, "")
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestTool_Execute_MissingChannel(t *testing.T) {
	registry := channels.NewRegistry()
	tool := NewTool("message", registry, nil, "")
	params, _ := json.Marshal(map[string]interface{}{
		"to":      "123",
		"content": "hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing channel")
	}
	if !strings.Contains(result.Content, "channel") && !strings.Contains(result.Content, "required") {
		t.Errorf("expected error about channel: %s", result.Content)
	}
}

func TestTool_Execute_MissingTo(t *testing.T) {
	registry := channels.NewRegistry()
	tool := NewTool("message", registry, nil, "")
	params, _ := json.Marshal(map[string]interface{}{
		"channel": "telegram",
		"content": "hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing 'to'")
	}
	if !strings.Contains(result.Content, "to") && !strings.Contains(result.Content, "required") {
		t.Errorf("expected error about 'to': %s", result.Content)
	}
}

func TestTool_Execute_MissingContent(t *testing.T) {
	registry := channels.NewRegistry()
	tool := NewTool("message", registry, nil, "")
	params, _ := json.Marshal(map[string]interface{}{
		"channel": "telegram",
		"to":      "123",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing content")
	}
	if !strings.Contains(result.Content, "content") && !strings.Contains(result.Content, "required") {
		t.Errorf("expected error about content: %s", result.Content)
	}
}

func TestTool_Execute_UnsupportedAction(t *testing.T) {
	registry := channels.NewRegistry()
	tool := NewTool("message", registry, nil, "")
	params, _ := json.Marshal(map[string]interface{}{
		"action":  "invalid",
		"channel": "telegram",
		"to":      "123",
		"content": "hello",
	})
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

func TestTool_Execute_ChannelNotAvailable(t *testing.T) {
	registry := channels.NewRegistry()
	tool := NewTool("message", registry, nil, "")
	params, _ := json.Marshal(map[string]interface{}{
		"channel": "telegram",
		"to":      "123",
		"content": "hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unavailable channel")
	}
	if !strings.Contains(result.Content, "not available") {
		t.Errorf("expected 'not available' in error: %s", result.Content)
	}
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

func TestMessageToolSend_WithExplicitAction(t *testing.T) {
	registry := channels.NewRegistry()
	adapter := &stubAdapter{}
	registry.Register(adapter)

	tool := NewTool("message", registry, nil, "")
	params, _ := json.Marshal(map[string]interface{}{
		"action":  "send",
		"channel": "telegram",
		"to":      "456",
		"content": "explicit action",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if len(adapter.sent) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(adapter.sent))
	}
}

func TestMessageToolSend_NoSessionStore(t *testing.T) {
	registry := channels.NewRegistry()
	adapter := &stubAdapter{}
	registry.Register(adapter)

	// No session store - should still work
	tool := NewTool("message", registry, nil, "")
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
		t.Fatalf("expected success without session store: %s", result.Content)
	}
}
