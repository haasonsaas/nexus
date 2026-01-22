// Package store provides document storage interfaces and implementations
// for the RAG (Retrieval-Augmented Generation) system.
package store

import (
	"context"

	"github.com/haasonsaas/nexus/pkg/models"
)

// DocumentStore defines the interface for document and chunk storage.
// Implementations handle persistence, indexing, and retrieval of documents.
type DocumentStore interface {
	// AddDocument stores a document and its chunks.
	// If the document already exists, it is updated.
	AddDocument(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error

	// GetDocument retrieves a document by ID.
	GetDocument(ctx context.Context, id string) (*models.Document, error)

	// ListDocuments lists documents with optional filtering.
	ListDocuments(ctx context.Context, opts *ListOptions) ([]*models.Document, error)

	// DeleteDocument removes a document and all its chunks.
	DeleteDocument(ctx context.Context, id string) error

	// GetChunk retrieves a single chunk by ID.
	GetChunk(ctx context.Context, id string) (*models.DocumentChunk, error)

	// GetChunksByDocument retrieves all chunks for a document.
	GetChunksByDocument(ctx context.Context, documentID string) ([]*models.DocumentChunk, error)

	// Search performs semantic search over chunks.
	Search(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error)

	// UpdateChunkEmbeddings updates embeddings for chunks.
	// Used when re-embedding with a new model.
	UpdateChunkEmbeddings(ctx context.Context, embeddings map[string][]float32) error

	// Stats returns statistics about the store.
	Stats(ctx context.Context) (*StoreStats, error)

	// Close releases resources.
	Close() error
}

// ListOptions configures document listing.
type ListOptions struct {
	// Limit is the maximum number of documents to return.
	// Default: 100
	Limit int

	// Offset is the number of documents to skip.
	Offset int

	// Source filters by document source.
	Source string

	// Tags filters by document tags (any match).
	Tags []string

	// AgentID filters by agent scope.
	AgentID string

	// SessionID filters by session scope.
	SessionID string

	// ChannelID filters by channel scope.
	ChannelID string

	// OrderBy specifies the sort field.
	// Options: "created_at", "updated_at", "name"
	// Default: "created_at"
	OrderBy string

	// OrderDesc reverses the sort order.
	OrderDesc bool
}

// StoreStats contains statistics about the document store.
type StoreStats struct {
	// TotalDocuments is the number of stored documents.
	TotalDocuments int64 `json:"total_documents"`

	// TotalChunks is the number of stored chunks.
	TotalChunks int64 `json:"total_chunks"`

	// TotalTokens is the approximate total token count.
	TotalTokens int64 `json:"total_tokens,omitempty"`

	// StorageBytes is the storage size in bytes (if available).
	StorageBytes int64 `json:"storage_bytes,omitempty"`

	// EmbeddingDimension is the configured embedding dimension.
	EmbeddingDimension int `json:"embedding_dimension"`
}

// SearchOptions provides additional search configuration.
type SearchOptions struct {
	// Scope limits search to a specific scope.
	Scope models.DocumentScope

	// ScopeID is the ID for the scope.
	ScopeID string

	// Limit is the maximum results to return.
	Limit int

	// Threshold is the minimum similarity score.
	Threshold float32

	// Tags filters by chunk tags.
	Tags []string

	// DocumentIDs limits search to specific documents.
	DocumentIDs []string

	// IncludeMetadata includes full metadata in results.
	IncludeMetadata bool
}

// DefaultSearchOptions returns default search options.
func DefaultSearchOptions() *SearchOptions {
	return &SearchOptions{
		Scope:     models.DocumentScopeGlobal,
		Limit:     10,
		Threshold: 0.7,
	}
}
