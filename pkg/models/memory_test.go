package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryEntry_Struct(t *testing.T) {
	now := time.Now()
	entry := MemoryEntry{
		ID:        "mem-123",
		SessionID: "session-456",
		ChannelID: "channel-789",
		AgentID:   "agent-abc",
		Content:   "Memory content here",
		Metadata: MemoryMetadata{
			Source: "message",
			Role:   "user",
			Tags:   []string{"important"},
			Extra:  map[string]any{"key": "value"},
		},
		Embedding: []float32{0.1, 0.2, 0.3, 0.4},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if entry.ID != "mem-123" {
		t.Errorf("ID = %q, want %q", entry.ID, "mem-123")
	}
	if entry.SessionID != "session-456" {
		t.Errorf("SessionID = %q, want %q", entry.SessionID, "session-456")
	}
	if len(entry.Embedding) != 4 {
		t.Errorf("Embedding length = %d, want 4", len(entry.Embedding))
	}
}

func TestMemoryEntry_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := MemoryEntry{
		ID:        "mem-123",
		SessionID: "session-456",
		Content:   "Test content",
		Metadata: MemoryMetadata{
			Source: "document",
			Role:   "assistant",
			Tags:   []string{"tag1", "tag2"},
		},
		Embedding: []float32{0.1, 0.2}, // Won't be serialized
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded MemoryEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content = %q, want %q", decoded.Content, original.Content)
	}
	// Embedding should not be serialized (json:"-")
	if decoded.Embedding != nil {
		t.Error("Embedding should be nil after JSON round-trip")
	}
}

func TestMemoryMetadata_Struct(t *testing.T) {
	meta := MemoryMetadata{
		Source: "note",
		Role:   "system",
		Tags:   []string{"tag1", "tag2", "tag3"},
		Extra:  map[string]any{"priority": "high", "count": 5},
	}

	if meta.Source != "note" {
		t.Errorf("Source = %q, want %q", meta.Source, "note")
	}
	if meta.Role != "system" {
		t.Errorf("Role = %q, want %q", meta.Role, "system")
	}
	if len(meta.Tags) != 3 {
		t.Errorf("Tags length = %d, want 3", len(meta.Tags))
	}
	if meta.Extra["priority"] != "high" {
		t.Errorf("Extra[priority] = %v, want %q", meta.Extra["priority"], "high")
	}
}

func TestMemoryScope_Constants(t *testing.T) {
	tests := []struct {
		constant MemoryScope
		expected string
	}{
		{ScopeSession, "session"},
		{ScopeChannel, "channel"},
		{ScopeAgent, "agent"},
		{ScopeGlobal, "global"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestSearchRequest_Struct(t *testing.T) {
	req := SearchRequest{
		Query:     "test search query",
		Scope:     ScopeAgent,
		ScopeID:   "agent-123",
		Limit:     20,
		Threshold: 0.8,
		Filters:   map[string]any{"tag": "important"},
	}

	if req.Query != "test search query" {
		t.Errorf("Query = %q, want %q", req.Query, "test search query")
	}
	if req.Scope != ScopeAgent {
		t.Errorf("Scope = %v, want %v", req.Scope, ScopeAgent)
	}
	if req.Limit != 20 {
		t.Errorf("Limit = %d, want 20", req.Limit)
	}
	if req.Threshold != 0.8 {
		t.Errorf("Threshold = %v, want 0.8", req.Threshold)
	}
}

func TestSearchResult_Struct(t *testing.T) {
	entry := &MemoryEntry{ID: "mem-123", Content: "test"}
	result := SearchResult{
		Entry:      entry,
		Score:      0.92,
		Highlights: []string{"matched snippet 1", "matched snippet 2"},
	}

	if result.Entry == nil {
		t.Fatal("Entry is nil")
	}
	if result.Entry.ID != "mem-123" {
		t.Errorf("Entry.ID = %q, want %q", result.Entry.ID, "mem-123")
	}
	if result.Score != 0.92 {
		t.Errorf("Score = %v, want 0.92", result.Score)
	}
	if len(result.Highlights) != 2 {
		t.Errorf("Highlights length = %d, want 2", len(result.Highlights))
	}
}

func TestSearchResponse_Struct(t *testing.T) {
	response := SearchResponse{
		Results: []*SearchResult{
			{Score: 0.95},
			{Score: 0.90},
			{Score: 0.85},
		},
		TotalCount: 150,
		QueryTime:  100 * time.Millisecond,
	}

	if len(response.Results) != 3 {
		t.Errorf("Results length = %d, want 3", len(response.Results))
	}
	if response.TotalCount != 150 {
		t.Errorf("TotalCount = %d, want 150", response.TotalCount)
	}
	if response.QueryTime != 100*time.Millisecond {
		t.Errorf("QueryTime = %v, want 100ms", response.QueryTime)
	}
}
