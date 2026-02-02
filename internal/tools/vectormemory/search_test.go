package vectormemory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/pkg/models"
)

type fakeSearcher struct {
	lastSearch     *models.SearchRequest
	lastHierarchy  *memory.HierarchyRequest
	response       *models.SearchResponse
	hierarchyError error
	searchError    error
}

func (f *fakeSearcher) Search(_ context.Context, req *models.SearchRequest) (*models.SearchResponse, error) {
	f.lastSearch = req
	return f.response, f.searchError
}

func (f *fakeSearcher) SearchHierarchical(_ context.Context, req *memory.HierarchyRequest) (*models.SearchResponse, error) {
	f.lastHierarchy = req
	return f.response, f.hierarchyError
}

func TestSearchTool_UsesHierarchyWhenEnabled(t *testing.T) {
	mgr := &fakeSearcher{
		response: &models.SearchResponse{
			Results: []*models.SearchResult{
				{Entry: &models.MemoryEntry{ID: "m1", Content: "hello"}},
			},
		},
	}
	cfg := &memory.Config{}
	cfg.Search.Hierarchy.Enabled = true

	tool := NewSearchTool(mgr, cfg)
	session := &models.Session{ID: "s1", ChannelID: "c1", AgentID: "a1"}
	ctx := agent.WithSession(context.Background(), session)

	result, err := tool.Execute(ctx, json.RawMessage(`{"query":"hello"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if mgr.lastHierarchy == nil {
		t.Fatal("expected hierarchical search")
	}
	if mgr.lastHierarchy.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", mgr.lastHierarchy.SessionID, "s1")
	}
	if mgr.lastHierarchy.ChannelID != "c1" {
		t.Errorf("ChannelID = %q, want %q", mgr.lastHierarchy.ChannelID, "c1")
	}
	if mgr.lastHierarchy.AgentID != "a1" {
		t.Errorf("AgentID = %q, want %q", mgr.lastHierarchy.AgentID, "a1")
	}
}

func TestSearchTool_ExplicitScopeUsesSearch(t *testing.T) {
	mgr := &fakeSearcher{
		response: &models.SearchResponse{
			Results: []*models.SearchResult{
				{Entry: &models.MemoryEntry{ID: "m1", Content: "hello"}},
			},
		},
	}
	cfg := &memory.Config{}
	cfg.Search.Hierarchy.Enabled = true

	tool := NewSearchTool(mgr, cfg)
	session := &models.Session{ID: "s1"}
	ctx := agent.WithSession(context.Background(), session)

	result, err := tool.Execute(ctx, json.RawMessage(`{"query":"hello","scope":"session"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if mgr.lastSearch == nil {
		t.Fatal("expected scoped search")
	}
	if mgr.lastSearch.Scope != models.ScopeSession {
		t.Errorf("Scope = %q, want %q", mgr.lastSearch.Scope, models.ScopeSession)
	}
	if mgr.lastSearch.ScopeID != "s1" {
		t.Errorf("ScopeID = %q, want %q", mgr.lastSearch.ScopeID, "s1")
	}
}
