package sessions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	sessionstore "github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

type echoProvider struct{}

func (echoProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	ch := make(chan *agent.CompletionChunk, 1)
	ch <- &agent.CompletionChunk{Text: "pong"}
	close(ch)
	return ch, nil
}

func (echoProvider) Name() string { return "echo" }

func (echoProvider) Models() []agent.Model { return nil }

func (echoProvider) SupportsTools() bool { return false }

// ListTool tests

func TestNewListTool(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	tool := NewListTool(store, "")
	if tool.defaultAgent != "main" {
		t.Errorf("expected default agent 'main', got %q", tool.defaultAgent)
	}
}

func TestListTool_Name(t *testing.T) {
	tool := NewListTool(nil, "")
	if tool.Name() != "sessions_list" {
		t.Errorf("expected 'sessions_list', got %q", tool.Name())
	}
}

func TestListTool_Description(t *testing.T) {
	tool := NewListTool(nil, "")
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestListTool_Schema(t *testing.T) {
	tool := NewListTool(nil, "")
	schema := tool.Schema()
	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema should be valid JSON: %v", err)
	}
}

func TestListTool_Execute_NilStore(t *testing.T) {
	tool := NewListTool(nil, "main")
	params, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil store")
	}
}

func TestListTool_Execute_InvalidParams(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	tool := NewListTool(store, "main")
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

// HistoryTool tests

func TestHistoryTool_Name(t *testing.T) {
	tool := NewHistoryTool(nil)
	if tool.Name() != "sessions_history" {
		t.Errorf("expected 'sessions_history', got %q", tool.Name())
	}
}

func TestHistoryTool_Execute_NilStore(t *testing.T) {
	tool := NewHistoryTool(nil)
	params, _ := json.Marshal(map[string]interface{}{"session_id": "test"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil store")
	}
}

func TestHistoryTool_Execute_MissingSessionID(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	tool := NewHistoryTool(store)
	params, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

// StatusTool tests

func TestStatusTool_Name(t *testing.T) {
	tool := NewStatusTool(nil)
	if tool.Name() != "session_status" {
		t.Errorf("expected 'session_status', got %q", tool.Name())
	}
}

func TestStatusTool_Execute_NilStore(t *testing.T) {
	tool := NewStatusTool(nil)
	params, _ := json.Marshal(map[string]interface{}{"session_id": "test"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil store")
	}
}

func TestStatusTool_Execute_MissingSessionID(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	tool := NewStatusTool(store)
	params, _ := json.Marshal(map[string]interface{}{})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

// SendTool tests

func TestSendTool_Name(t *testing.T) {
	tool := NewSendTool(nil, nil)
	if tool.Name() != "sessions_send" {
		t.Errorf("expected 'sessions_send', got %q", tool.Name())
	}
}

func TestSendTool_Execute_NilStore(t *testing.T) {
	tool := NewSendTool(nil, nil)
	params, _ := json.Marshal(map[string]interface{}{
		"session_id": "test",
		"message":    "hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil store")
	}
}

func TestSendTool_Execute_MissingSessionID(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	tool := NewSendTool(store, nil)
	params, _ := json.Marshal(map[string]interface{}{
		"message": "hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestSendTool_Execute_MissingMessage(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	tool := NewSendTool(store, nil)
	params, _ := json.Marshal(map[string]interface{}{
		"session_id": "test",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing message")
	}
}

// Integration tests

func TestSessionsListHistoryStatus(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	session := &models.Session{
		AgentID:   "main",
		Channel:   models.ChannelTelegram,
		ChannelID: "123",
		Key:       "main:telegram:123",
	}
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	listTool := NewListTool(store, "main")
	listParams, _ := json.Marshal(map[string]interface{}{})
	listResult, err := listTool.Execute(context.Background(), listParams)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listResult.Content, session.ID) {
		t.Fatalf("expected session in list: %s", listResult.Content)
	}

	msg := &models.Message{
		ID:        "m1",
		SessionID: session.ID,
		Role:      models.RoleUser,
		Content:   "hi",
		CreatedAt: time.Now(),
	}
	if err := store.AppendMessage(context.Background(), session.ID, msg); err != nil {
		t.Fatalf("append message: %v", err)
	}

	historyTool := NewHistoryTool(store)
	historyParams, _ := json.Marshal(map[string]interface{}{
		"session_id": session.ID,
		"limit":      10,
	})
	historyResult, err := historyTool.Execute(context.Background(), historyParams)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(historyResult.Content, "hi") {
		t.Fatalf("expected history content: %s", historyResult.Content)
	}

	statusTool := NewStatusTool(store)
	statusParams, _ := json.Marshal(map[string]interface{}{
		"session_id": session.ID,
	})
	statusResult, err := statusTool.Execute(context.Background(), statusParams)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(statusResult.Content, session.ID) {
		t.Fatalf("expected status content: %s", statusResult.Content)
	}
}

func TestSessionsSendWaitsForResponse(t *testing.T) {
	store := sessionstore.NewMemoryStore()
	session := &models.Session{
		AgentID:   "main",
		Channel:   models.ChannelTelegram,
		ChannelID: "123",
		Key:       "main:telegram:123",
	}
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	runtime := agent.NewRuntime(echoProvider{}, store)
	tool := NewSendTool(store, runtime)
	params, _ := json.Marshal(map[string]interface{}{
		"session_id": session.ID,
		"message":    "ping",
		"wait":       true,
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(result.Content, "pong") {
		t.Fatalf("expected response, got %s", result.Content)
	}
}
