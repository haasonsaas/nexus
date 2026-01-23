package sqlitevec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/memory/backend"
	"github.com/haasonsaas/nexus/pkg/models"
	_ "modernc.org/sqlite" // Pure-Go SQLite driver
)

// newTestBackend creates a backend for testing, skipping if driver unavailable.
func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	b, err := New(Config{})
	if err != nil {
		if strings.Contains(err.Error(), "unknown driver") {
			t.Skip("SQLite driver not available (driver name mismatch)")
		}
		t.Fatalf("New error: %v", err)
	}
	return b
}

func TestNew(t *testing.T) {
	t.Run("default config uses memory database", func(t *testing.T) {
		b := newTestBackend(t)
		defer b.Close()

		if b.db == nil {
			t.Error("db should not be nil")
		}
		if b.dimension != 1536 {
			t.Errorf("dimension = %d, want 1536", b.dimension)
		}
	})

	t.Run("custom config", func(t *testing.T) {
		b, err := New(Config{
			Path:      ":memory:",
			Dimension: 768,
		})
		if err != nil {
			if strings.Contains(err.Error(), "unknown driver") {
				t.Skip("SQLite driver not available")
			}
			t.Fatalf("New error: %v", err)
		}
		defer b.Close()

		if b.dimension != 768 {
			t.Errorf("dimension = %d, want 768", b.dimension)
		}
	})
}

func TestBackend_Index(t *testing.T) {
	b := newTestBackend(t)
	defer b.Close()

	t.Run("index single entry", func(t *testing.T) {
		entry := &models.MemoryEntry{
			Content:   "Test content",
			SessionID: "session-1",
			Embedding: []float32{0.1, 0.2, 0.3},
			Metadata:  models.MemoryMetadata{Source: "test", Extra: map[string]any{"key": "value"}},
		}

		err := b.Index(context.Background(), []*models.MemoryEntry{entry})
		if err != nil {
			t.Fatalf("Index error: %v", err)
		}

		// Entry should have an ID assigned
		if entry.ID == "" {
			t.Error("entry.ID should be assigned")
		}
		if entry.CreatedAt.IsZero() {
			t.Error("entry.CreatedAt should be set")
		}
	})

	t.Run("index multiple entries", func(t *testing.T) {
		entries := []*models.MemoryEntry{
			{Content: "First", ChannelID: "channel-1"},
			{Content: "Second", ChannelID: "channel-1"},
			{Content: "Third", AgentID: "agent-1"},
		}

		err := b.Index(context.Background(), entries)
		if err != nil {
			t.Fatalf("Index error: %v", err)
		}

		for i, e := range entries {
			if e.ID == "" {
				t.Errorf("entries[%d].ID should be assigned", i)
			}
		}
	})

	t.Run("index with existing ID preserves it", func(t *testing.T) {
		entry := &models.MemoryEntry{
			ID:      "custom-id-123",
			Content: "Custom ID content",
		}

		err := b.Index(context.Background(), []*models.MemoryEntry{entry})
		if err != nil {
			t.Fatalf("Index error: %v", err)
		}

		if entry.ID != "custom-id-123" {
			t.Errorf("entry.ID = %q, want %q", entry.ID, "custom-id-123")
		}
	})
}

func TestBackend_Search(t *testing.T) {
	b := newTestBackend(t)
	defer b.Close()

	// Index some test data
	entries := []*models.MemoryEntry{
		{Content: "Apple is a fruit", SessionID: "session-1", Embedding: []float32{0.9, 0.1, 0.0}},
		{Content: "Banana is yellow", SessionID: "session-1", Embedding: []float32{0.8, 0.2, 0.0}},
		{Content: "Car is a vehicle", SessionID: "session-2", Embedding: []float32{0.1, 0.9, 0.0}},
	}
	if err := b.Index(context.Background(), entries); err != nil {
		t.Fatalf("Index error: %v", err)
	}

	t.Run("search without scope", func(t *testing.T) {
		results, err := b.Search(context.Background(), []float32{0.85, 0.15, 0.0}, nil)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}

		if len(results) == 0 {
			t.Error("expected results")
		}
	})

	t.Run("search with session scope", func(t *testing.T) {
		opts := &backend.SearchOptions{
			Scope:   models.ScopeSession,
			ScopeID: "session-1",
			Limit:   10,
		}
		results, err := b.Search(context.Background(), []float32{0.85, 0.15, 0.0}, opts)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}

		for _, r := range results {
			if r.Entry.SessionID != "session-1" {
				t.Errorf("result has SessionID = %q, want session-1", r.Entry.SessionID)
			}
		}
	})

	t.Run("search with limit", func(t *testing.T) {
		opts := &backend.SearchOptions{Limit: 1}
		results, err := b.Search(context.Background(), []float32{0.5, 0.5, 0.0}, opts)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}

		if len(results) > 1 {
			t.Errorf("expected at most 1 result, got %d", len(results))
		}
	})

	t.Run("search with threshold", func(t *testing.T) {
		opts := &backend.SearchOptions{
			Limit:     10,
			Threshold: 0.99, // Very high threshold
		}
		results, err := b.Search(context.Background(), []float32{0.1, 0.1, 0.0}, opts)
		if err != nil {
			t.Fatalf("Search error: %v", err)
		}

		// Should filter out low-scoring results
		for _, r := range results {
			if r.Score < 0.99 {
				t.Errorf("result score = %f, want >= 0.99", r.Score)
			}
		}
	})
}

func TestBackend_Delete(t *testing.T) {
	b := newTestBackend(t)
	defer b.Close()

	// Index test data
	entry := &models.MemoryEntry{ID: "delete-me", Content: "To be deleted"}
	if err := b.Index(context.Background(), []*models.MemoryEntry{entry}); err != nil {
		t.Fatalf("Index error: %v", err)
	}

	t.Run("delete existing entry", func(t *testing.T) {
		count, _ := b.Count(context.Background(), "", "")
		if count == 0 {
			t.Skip("no entries to delete")
		}

		err := b.Delete(context.Background(), []string{"delete-me"})
		if err != nil {
			t.Fatalf("Delete error: %v", err)
		}
	})

	t.Run("delete empty list", func(t *testing.T) {
		err := b.Delete(context.Background(), []string{})
		if err != nil {
			t.Errorf("Delete empty list error: %v", err)
		}
	})

	t.Run("delete non-existent entry", func(t *testing.T) {
		err := b.Delete(context.Background(), []string{"non-existent-id"})
		if err != nil {
			t.Errorf("Delete non-existent error: %v", err)
		}
	})
}

func TestBackend_Count(t *testing.T) {
	b := newTestBackend(t)
	defer b.Close()

	// Index test data with different scopes
	entries := []*models.MemoryEntry{
		{Content: "A", SessionID: "s1"},
		{Content: "B", SessionID: "s1"},
		{Content: "C", ChannelID: "c1"},
		{Content: "D", AgentID: "a1"},
	}
	if err := b.Index(context.Background(), entries); err != nil {
		t.Fatalf("Index error: %v", err)
	}

	t.Run("count all", func(t *testing.T) {
		count, err := b.Count(context.Background(), "", "")
		if err != nil {
			t.Fatalf("Count error: %v", err)
		}
		if count < 4 {
			t.Errorf("count = %d, want >= 4", count)
		}
	})

	t.Run("count by session", func(t *testing.T) {
		count, err := b.Count(context.Background(), models.ScopeSession, "s1")
		if err != nil {
			t.Fatalf("Count error: %v", err)
		}
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
	})

	t.Run("count by channel", func(t *testing.T) {
		count, err := b.Count(context.Background(), models.ScopeChannel, "c1")
		if err != nil {
			t.Fatalf("Count error: %v", err)
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})

	t.Run("count by agent", func(t *testing.T) {
		count, err := b.Count(context.Background(), models.ScopeAgent, "a1")
		if err != nil {
			t.Fatalf("Count error: %v", err)
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})
}

func TestBackend_Compact(t *testing.T) {
	b := newTestBackend(t)
	defer b.Close()

	err := b.Compact(context.Background())
	if err != nil {
		t.Errorf("Compact error: %v", err)
	}
}

func TestBackend_Close(t *testing.T) {
	b := newTestBackend(t)

	err := b.Close()
	if err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestNullString(t *testing.T) {
	t.Run("empty string returns invalid", func(t *testing.T) {
		ns := nullString("")
		if ns.Valid {
			t.Error("expected Valid to be false for empty string")
		}
	})

	t.Run("non-empty string returns valid", func(t *testing.T) {
		ns := nullString("test")
		if !ns.Valid {
			t.Error("expected Valid to be true for non-empty string")
		}
		if ns.String != "test" {
			t.Errorf("String = %q, want %q", ns.String, "test")
		}
	})
}

func TestEncodeDecodeEmbedding(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		original := []float32{0.1, 0.2, -0.5, 1.0, 0.0}
		encoded := encodeEmbedding(original)
		decoded := decodeEmbedding(encoded)

		if len(decoded) != len(original) {
			t.Fatalf("decoded length = %d, want %d", len(decoded), len(original))
		}
		for i := range original {
			if decoded[i] != original[i] {
				t.Errorf("decoded[%d] = %f, want %f", i, decoded[i], original[i])
			}
		}
	})

	t.Run("empty embedding", func(t *testing.T) {
		encoded := encodeEmbedding([]float32{})
		if encoded != nil {
			t.Errorf("expected nil for empty embedding, got %v", encoded)
		}

		decoded := decodeEmbedding(nil)
		if decoded != nil {
			t.Errorf("expected nil for nil input, got %v", decoded)
		}
	})

	t.Run("invalid length returns nil", func(t *testing.T) {
		// Not divisible by 4
		decoded := decodeEmbedding([]byte{1, 2, 3})
		if decoded != nil {
			t.Errorf("expected nil for invalid length, got %v", decoded)
		}
	})
}

func TestCosineSimilarity(t *testing.T) {
	t.Run("identical vectors", func(t *testing.T) {
		a := []float32{1.0, 0.0, 0.0}
		b := []float32{1.0, 0.0, 0.0}
		sim := cosineSimilarity(a, b)
		if sim < 0.99 || sim > 1.01 {
			t.Errorf("similarity = %f, want ~1.0", sim)
		}
	})

	t.Run("orthogonal vectors", func(t *testing.T) {
		a := []float32{1.0, 0.0, 0.0}
		b := []float32{0.0, 1.0, 0.0}
		sim := cosineSimilarity(a, b)
		if sim < -0.01 || sim > 0.01 {
			t.Errorf("similarity = %f, want ~0.0", sim)
		}
	})

	t.Run("opposite vectors", func(t *testing.T) {
		a := []float32{1.0, 0.0}
		b := []float32{-1.0, 0.0}
		sim := cosineSimilarity(a, b)
		if sim < -1.01 || sim > -0.99 {
			t.Errorf("similarity = %f, want ~-1.0", sim)
		}
	})

	t.Run("different lengths returns 0", func(t *testing.T) {
		a := []float32{1.0, 0.0}
		b := []float32{1.0, 0.0, 0.0}
		sim := cosineSimilarity(a, b)
		if sim != 0 {
			t.Errorf("similarity = %f, want 0", sim)
		}
	})

	t.Run("empty vectors returns 0", func(t *testing.T) {
		sim := cosineSimilarity([]float32{}, []float32{})
		if sim != 0 {
			t.Errorf("similarity = %f, want 0", sim)
		}
	})

	t.Run("zero vector returns 0", func(t *testing.T) {
		a := []float32{0.0, 0.0, 0.0}
		b := []float32{1.0, 0.0, 0.0}
		sim := cosineSimilarity(a, b)
		if sim != 0 {
			t.Errorf("similarity = %f, want 0 for zero vector", sim)
		}
	})
}

func TestSqrt32(t *testing.T) {
	tests := []struct {
		input    float32
		expected float32
		epsilon  float32
	}{
		{4.0, 2.0, 0.01},
		{9.0, 3.0, 0.01},
		{2.0, 1.414, 0.01},
		{0.0, 0.0, 0.01},
		{-1.0, 0.0, 0.01}, // Negative returns 0
	}

	for _, tt := range tests {
		result := sqrt32(tt.input)
		diff := result - tt.expected
		if diff < 0 {
			diff = -diff
		}
		if diff > tt.epsilon {
			t.Errorf("sqrt32(%f) = %f, want ~%f", tt.input, result, tt.expected)
		}
	}
}

func TestSortByScoreDesc(t *testing.T) {
	results := []*models.SearchResult{
		{Score: 0.5},
		{Score: 0.9},
		{Score: 0.3},
		{Score: 0.7},
	}

	sortByScoreDesc(results)

	expected := []float32{0.9, 0.7, 0.5, 0.3}
	for i, r := range results {
		if r.Score != expected[i] {
			t.Errorf("results[%d].Score = %f, want %f", i, r.Score, expected[i])
		}
	}
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		Path:      "/path/to/db.sqlite",
		Dimension: 512,
	}
	if cfg.Path != "/path/to/db.sqlite" {
		t.Errorf("Path = %q, want %q", cfg.Path, "/path/to/db.sqlite")
	}
	if cfg.Dimension != 512 {
		t.Errorf("Dimension = %d, want 512", cfg.Dimension)
	}
}

func TestBackend_ContextCancellation(t *testing.T) {
	b := newTestBackend(t)
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	time.Sleep(5 * time.Millisecond) // Let context expire

	// Operations on cancelled context might fail
	_ = b.Index(ctx, []*models.MemoryEntry{{Content: "test"}})
	// We don't check the error since behavior varies by implementation
}
