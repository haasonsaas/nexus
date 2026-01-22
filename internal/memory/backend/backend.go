// Package backend provides storage backend interfaces and implementations
// for the vector memory system.
package backend

import (
	"context"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Backend defines the interface for vector storage backends.
type Backend interface {
	// Index stores memory entries with their embeddings.
	Index(ctx context.Context, entries []*models.MemoryEntry) error

	// Search finds similar entries using the query embedding.
	Search(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*models.SearchResult, error)

	// Delete removes entries by ID.
	Delete(ctx context.Context, ids []string) error

	// Count returns the number of entries matching the scope.
	Count(ctx context.Context, scope models.MemoryScope, scopeID string) (int64, error)

	// Compact optimizes the storage (vacuuming, reindexing, etc.).
	Compact(ctx context.Context) error

	// Close releases resources.
	Close() error
}

// SearchMode specifies the search algorithm to use.
type SearchMode string

const (
	// SearchModeVector uses pure vector similarity search (default).
	SearchModeVector SearchMode = "vector"

	// SearchModeBM25 uses BM25 full-text search only.
	SearchModeBM25 SearchMode = "bm25"

	// SearchModeHybrid combines vector and BM25 search with weighted scoring.
	SearchModeHybrid SearchMode = "hybrid"
)

// SearchOptions defines options for backend search operations.
type SearchOptions struct {
	Scope     models.MemoryScope
	ScopeID   string
	Limit     int
	Threshold float32
	Filters   map[string]any

	// SearchMode specifies the search algorithm (default: vector).
	SearchMode SearchMode

	// HybridAlpha controls the weighting in hybrid mode.
	// 0.0 = pure BM25, 1.0 = pure vector.
	// Default: 0.7 (favor vector similarity).
	HybridAlpha float32

	// Query is the raw text query (required for BM25 and hybrid modes).
	Query string
}

// Config contains common backend configuration.
type Config struct {
	Dimension int // Embedding dimension (e.g., 1536 for text-embedding-3-small)
}
