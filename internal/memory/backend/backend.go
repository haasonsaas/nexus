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

// SearchOptions defines options for backend search operations.
type SearchOptions struct {
	Scope     models.MemoryScope
	ScopeID   string
	Limit     int
	Threshold float32
	Filters   map[string]any
}

// Config contains common backend configuration.
type Config struct {
	Dimension int // Embedding dimension (e.g., 1536 for text-embedding-3-small)
}
