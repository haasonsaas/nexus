package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/ratelimit"
)

func TestActionHandler_Success(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.CanvasHostConfig{
		Port:      18793,
		Root:      tmpDir,
		Namespace: "/__nexus__",
	}
	host, err := NewHost(cfg, config.CanvasConfig{}, nil)
	if err != nil {
		t.Fatalf("NewHost error: %v", err)
	}

	var got Action
	host.SetActionHandler(func(_ context.Context, action Action) error {
		got = action
		return nil
	})

	body := `{"session_id":"session-123","name":"clicked","source_component_id":"btn-1","context":{"ok":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader(body))
	rec := httptest.NewRecorder()
	host.actionsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	if got.SessionID != "session-123" {
		t.Fatalf("expected session_id to be set")
	}
	if got.Name != "clicked" {
		t.Fatalf("expected name to be set")
	}
}

func TestActionHandler_RateLimit(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.CanvasHostConfig{
		Port:      18793,
		Root:      tmpDir,
		Namespace: "/__nexus__",
	}
	canvasCfg := config.CanvasConfig{
		Actions: config.CanvasActionConfig{
			RateLimit: ratelimit.Config{Enabled: true, RequestsPerSecond: 1, BurstSize: 1},
		},
	}
	host, err := NewHost(cfg, canvasCfg, nil)
	if err != nil {
		t.Fatalf("NewHost error: %v", err)
	}
	host.SetActionHandler(func(_ context.Context, action Action) error { return nil })

	payload := map[string]any{"session_id": "session-1", "name": "clicked"}
	data, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader(string(data)))
	rec := httptest.NewRecorder()
	host.actionsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader(string(data)))
	rec2 := httptest.NewRecorder()
	host.actionsHandler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec2.Code)
	}
}

func TestActionHandler_BodyTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.CanvasHostConfig{
		Port:      18793,
		Root:      tmpDir,
		Namespace: "/__nexus__",
	}
	host, err := NewHost(cfg, config.CanvasConfig{}, nil)
	if err != nil {
		t.Fatalf("NewHost error: %v", err)
	}

	host.SetActionHandler(func(_ context.Context, action Action) error { return nil })

	oversize := strings.Repeat("a", (1<<20)+1)
	body := fmt.Sprintf(`{"session_id":"session-123","name":"clicked","context":{"blob":"%s"}}`, oversize)
	req := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader(body))
	rec := httptest.NewRecorder()
	host.actionsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}
