package lancedb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/haasonsaas/nexus/internal/memory/backend"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Path:      filepath.Join(t.TempDir(), "test_db"),
				Dimension: 128,
			},
			wantErr: false,
		},
		{
			name: "empty path",
			config: Config{
				Path:      "",
				Dimension: 128,
			},
			wantErr: true,
		},
		{
			name: "default dimension",
			config: Config{
				Path:      filepath.Join(t.TempDir(), "test_db_default"),
				Dimension: 0, // Should default to 1536
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if b != nil {
				defer b.Close()
			}
		})
	}
}

func TestBackend_Index(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_index_db")
	b, err := New(Config{
		Path:      dbPath,
		Dimension: 128,
	})
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()

	t.Run("single entry", func(t *testing.T) {
		entry := &models.MemoryEntry{
			ID:        "test-1",
			SessionID: "session-1",
			Content:   "Test content",
			Embedding: makeTestEmbedding(128),
		}

		err := b.Index(ctx, []*models.MemoryEntry{entry})
		if err != nil {
			t.Errorf("Index() error = %v", err)
		}
	})

	t.Run("multiple entries", func(t *testing.T) {
		entries := []*models.MemoryEntry{
			{
				ID:        "test-2",
				SessionID: "session-1",
				Content:   "Second entry",
				Embedding: makeTestEmbedding(128),
			},
			{
				ID:        "test-3",
				ChannelID: "channel-1",
				Content:   "Third entry",
				Embedding: makeTestEmbedding(128),
			},
		}

		err := b.Index(ctx, entries)
		if err != nil {
			t.Errorf("Index() error = %v", err)
		}
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		entry := &models.MemoryEntry{
			ID:        "test-dim",
			Content:   "Wrong dimension",
			Embedding: makeTestEmbedding(64), // Wrong dimension
		}

		err := b.Index(ctx, []*models.MemoryEntry{entry})
		if err == nil {
			t.Error("Index() should error on dimension mismatch")
		}
	})
}

func TestBackend_Search(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_search_db")
	b, err := New(Config{
		Path:       dbPath,
		Dimension:  128,
		MetricType: "cosine",
	})
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()

	entries := []*models.MemoryEntry{
		{
			ID:        "search-1",
			SessionID: "session-1",
			Content:   "First search entry",
			Embedding: makeTestEmbedding(128),
		},
		{
			ID:        "search-2",
			SessionID: "session-1",
			Content:   "Second search entry",
			Embedding: makeTestEmbedding(128),
		},
		{
			ID:        "search-3",
			SessionID: "session-2",
			Content:   "Third search entry",
			Embedding: makeTestEmbedding(128),
		},
	}

	if err := b.Index(ctx, entries); err != nil {
		t.Fatalf("Failed to index entries: %v", err)
	}

	t.Run("basic search", func(t *testing.T) {
		queryEmbed := makeTestEmbedding(128)
		results, err := b.Search(ctx, queryEmbed, &backend.SearchOptions{
			Limit: 10,
		})
		if err != nil {
			t.Errorf("Search() error = %v", err)
		}
		if len(results) == 0 {
			t.Error("Expected search results")
		}
	})

	t.Run("session scope", func(t *testing.T) {
		queryEmbed := makeTestEmbedding(128)
		results, err := b.Search(ctx, queryEmbed, &backend.SearchOptions{
			Scope:   models.ScopeSession,
			ScopeID: "session-1",
			Limit:   10,
		})
		if err != nil {
			t.Errorf("Search() error = %v", err)
		}
		for _, r := range results {
			if r.Entry.SessionID != "session-1" {
				t.Errorf("Expected session-1, got %s", r.Entry.SessionID)
			}
		}
	})

	t.Run("limit results", func(t *testing.T) {
		queryEmbed := makeTestEmbedding(128)
		results, err := b.Search(ctx, queryEmbed, &backend.SearchOptions{
			Limit: 2,
		})
		if err != nil {
			t.Errorf("Search() error = %v", err)
		}
		if len(results) > 2 {
			t.Errorf("Expected at most 2 results, got %d", len(results))
		}
	})
}

func TestBackend_Delete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_delete_db")
	b, err := New(Config{
		Path:      dbPath,
		Dimension: 128,
	})
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()

	entries := []*models.MemoryEntry{
		{
			ID:        "delete-1",
			Content:   "To be deleted",
			Embedding: makeTestEmbedding(128),
		},
		{
			ID:        "delete-2",
			Content:   "To keep",
			Embedding: makeTestEmbedding(128),
		},
	}

	if err := b.Index(ctx, entries); err != nil {
		t.Fatalf("Failed to index entries: %v", err)
	}

	t.Run("delete single", func(t *testing.T) {
		err := b.Delete(ctx, []string{"delete-1"})
		if err != nil {
			t.Errorf("Delete() error = %v", err)
		}

		count, _ := b.Count(ctx, models.ScopeGlobal, "")
		if count != 1 {
			t.Errorf("Expected 1 entry after delete, got %d", count)
		}
	})
}

func TestBackend_Count(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_count_db")
	b, err := New(Config{
		Path:      dbPath,
		Dimension: 128,
	})
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer b.Close()

	ctx := context.Background()

	entries := []*models.MemoryEntry{
		{
			ID:        "count-1",
			SessionID: "session-1",
			Content:   "First",
			Embedding: makeTestEmbedding(128),
		},
		{
			ID:        "count-2",
			SessionID: "session-1",
			Content:   "Second",
			Embedding: makeTestEmbedding(128),
		},
		{
			ID:        "count-3",
			SessionID: "session-2",
			Content:   "Third",
			Embedding: makeTestEmbedding(128),
		},
	}

	if err := b.Index(ctx, entries); err != nil {
		t.Fatalf("Failed to index entries: %v", err)
	}

	t.Run("global count", func(t *testing.T) {
		count, err := b.Count(ctx, models.ScopeGlobal, "")
		if err != nil {
			t.Errorf("Count() error = %v", err)
		}
		if count != 3 {
			t.Errorf("Expected 3 entries, got %d", count)
		}
	})

	t.Run("session count", func(t *testing.T) {
		count, err := b.Count(ctx, models.ScopeSession, "session-1")
		if err != nil {
			t.Errorf("Count() error = %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 entries for session-1, got %d", count)
		}
	})
}

func TestSimilarityFunctions(t *testing.T) {
	t.Run("cosine similarity", func(t *testing.T) {
		a := []float32{1.0, 0.0, 0.0}
		b := []float32{1.0, 0.0, 0.0}
		c := []float32{0.0, 1.0, 0.0}

		sim := cosineSimilarity(a, b)
		if sim < 0.99 || sim > 1.01 {
			t.Errorf("cosineSimilarity(same) = %v, want ~1", sim)
		}

		sim = cosineSimilarity(a, c)
		if sim < -0.01 || sim > 0.01 {
			t.Errorf("cosineSimilarity(orthogonal) = %v, want ~0", sim)
		}
	})

	t.Run("l2 distance", func(t *testing.T) {
		a := []float32{0.0, 0.0, 0.0}
		b := []float32{3.0, 4.0, 0.0}

		dist := l2Distance(a, b)
		if dist < 4.99 || dist > 5.01 {
			t.Errorf("l2Distance() = %v, want ~5", dist)
		}
	})

	t.Run("dot product", func(t *testing.T) {
		a := []float32{1.0, 2.0, 3.0}
		b := []float32{4.0, 5.0, 6.0}

		dot := dotProduct(a, b)
		expected := float32(1*4 + 2*5 + 3*6)
		if dot != expected {
			t.Errorf("dotProduct() = %v, want %v", dot, expected)
		}
	})
}

func makeTestEmbedding(dim int) []float32 {
	embedding := make([]float32, dim)
	for i := 0; i < dim; i++ {
		embedding[i] = float32(i) / float32(dim)
	}
	return embedding
}
