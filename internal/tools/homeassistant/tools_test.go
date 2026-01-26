package homeassistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTools_GetState(t *testing.T) {
	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s want GET", r.Method)
		}
		if r.URL.Path != "/api/states/light.kitchen" {
			t.Fatalf("path=%s want /api/states/light.kitchen", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entity_id":"light.kitchen","state":"on","attributes":{"friendly_name":"Kitchen"}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(Config{BaseURL: srv.URL, Token: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tool := NewGetStateTool(client)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"entity_id":"light.kitchen"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("Authorization=%q want %q", gotAuth, "Bearer token")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out["entity_id"] != "light.kitchen" {
		t.Fatalf("entity_id=%v", out["entity_id"])
	}
}

func TestTools_CallService(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		gotBody string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"entity_id":"light.kitchen","state":"off"}]`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(Config{BaseURL: srv.URL, Token: "token"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tool := NewCallServiceTool(client)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{
  "domain":"light",
  "service":"turn_off",
  "service_data":{"entity_id":"light.kitchen"}
}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content)
	}
	if gotPath != "/api/services/light/turn_off" {
		t.Fatalf("path=%q want %q", gotPath, "/api/services/light/turn_off")
	}
	if !strings.Contains(gotBody, "\"entity_id\"") {
		t.Fatalf("request body missing entity_id: %s", gotBody)
	}
}

func TestTools_ListEntities(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s want GET", r.Method)
		}
		if r.URL.Path != "/api/states" {
			t.Fatalf("path=%s want /api/states", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
  {"entity_id":"light.kitchen","state":"on","attributes":{"friendly_name":"Kitchen"}},
  {"entity_id":"switch.router","state":"off","attributes":{"friendly_name":"Router"}}
]`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(Config{BaseURL: srv.URL, Token: "token"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tool := NewListEntitiesTool(client)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"domain":"light","limit":10}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content)
	}
	var out struct {
		Entities []struct {
			EntityID string `json:"entity_id"`
		} `json:"entities"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Total != 1 || len(out.Entities) != 1 {
		t.Fatalf("total=%d len=%d want 1", out.Total, len(out.Entities))
	}
	if out.Entities[0].EntityID != "light.kitchen" {
		t.Fatalf("entity_id=%q want %q", out.Entities[0].EntityID, "light.kitchen")
	}
}
