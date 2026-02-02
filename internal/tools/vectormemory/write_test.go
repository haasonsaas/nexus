package vectormemory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/pkg/models"
)

type fakeIndexer struct {
	entries []*models.MemoryEntry
	err     error
}

func (f *fakeIndexer) Index(_ context.Context, entries []*models.MemoryEntry) error {
	f.entries = entries
	return f.err
}

func TestWriteTool_ChannelScopeUsesSessionContext(t *testing.T) {
	indexer := &fakeIndexer{}
	cfg := &memory.Config{}
	tool := NewWriteTool(indexer, cfg)

	session := &models.Session{
		ID:        "sess-1",
		ChannelID: "chan-1",
		AgentID:   "agent-1",
	}
	ctx := agent.WithSession(context.Background(), session)

	result, err := tool.Execute(ctx, json.RawMessage(`{"content":"hello","scope":"channel","tags":["summary"]}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if len(indexer.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(indexer.entries))
	}
	entry := indexer.entries[0]
	if entry.ChannelID != "chan-1" {
		t.Errorf("ChannelID = %q, want %q", entry.ChannelID, "chan-1")
	}
	if entry.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", entry.SessionID)
	}
	if entry.AgentID != "" {
		t.Errorf("AgentID = %q, want empty", entry.AgentID)
	}
	if len(entry.Metadata.Tags) != 1 || entry.Metadata.Tags[0] != "summary" {
		t.Errorf("Tags = %v, want [summary]", entry.Metadata.Tags)
	}
	if entry.Metadata.Extra["source_agent_id"] != "agent-1" {
		t.Errorf("source_agent_id = %v, want %q", entry.Metadata.Extra["source_agent_id"], "agent-1")
	}
}
