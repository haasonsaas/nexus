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
