package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestAPIAnalyticsOverview_JSON(t *testing.T) {
	ctx := context.Background()
	store := sessions.NewMemoryStore()

	session := &models.Session{
		AgentID:   "main",
		Channel:   models.ChannelAPI,
		ChannelID: "chan-1",
		Key:       "main:api:chan-1",
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	now := time.Now().Add(-30 * time.Minute)
	if err := store.AppendMessage(ctx, session.ID, &models.Message{
		ID:        "m1",
		SessionID: session.ID,
		Channel:   models.ChannelAPI,
		ChannelID: "chan-1",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "hi",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}

	if err := store.AppendMessage(ctx, session.ID, &models.Message{
		ID:        "m2",
		SessionID: session.ID,
		Channel:   models.ChannelAPI,
		ChannelID: "chan-1",
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   "working...",
		ToolCalls: []models.ToolCall{
			{ID: "tc1", Name: "websearch", Input: json.RawMessage(`{"q":"nexus"}`)},
			{ID: "tc2", Name: "memory_search", Input: json.RawMessage(`{"q":"tool events"}`)},
		},
		CreatedAt: now.Add(1 * time.Second),
	}); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}

	if err := store.AppendMessage(ctx, session.ID, &models.Message{
		ID:        "m3",
		SessionID: session.ID,
		Channel:   models.ChannelAPI,
		ChannelID: "chan-1",
		Direction: models.DirectionInbound,
		Role:      models.RoleTool,
		ToolResults: []models.ToolResult{
			{ToolCallID: "tc1", Content: "ok", IsError: false},
			{ToolCallID: "tc2", Content: "fail", IsError: true},
		},
		CreatedAt: now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("AppendMessage tool: %v", err)
	}

	handler, err := NewHandler(&Config{
		SessionStore:    store,
		DefaultAgentID:  "main",
		ServerStartTime: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/api/v1/analytics/overview?period=24h&agent=main", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var overview AnalyticsOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &overview); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if overview.AgentID != "main" {
		t.Fatalf("AgentID=%q, want %q", overview.AgentID, "main")
	}
	if overview.TotalConversations != 1 {
		t.Fatalf("TotalConversations=%d, want %d", overview.TotalConversations, 1)
	}
	if overview.TotalMessages != 3 {
		t.Fatalf("TotalMessages=%d, want %d", overview.TotalMessages, 3)
	}
	if overview.ToolCalls != 2 {
		t.Fatalf("ToolCalls=%d, want %d", overview.ToolCalls, 2)
	}
	if overview.ToolResults != 2 {
		t.Fatalf("ToolResults=%d, want %d", overview.ToolResults, 2)
	}
	if overview.ToolErrors != 1 {
		t.Fatalf("ToolErrors=%d, want %d", overview.ToolErrors, 1)
	}
	if overview.ToolErrorRatePct <= 0 {
		t.Fatalf("ToolErrorRatePct=%v, want > 0", overview.ToolErrorRatePct)
	}
	if len(overview.TopTools) != 2 {
		t.Fatalf("TopTools=%v, want 2 entries", overview.TopTools)
	}
}
