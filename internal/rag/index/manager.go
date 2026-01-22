// Package index provides the index manager for the RAG system.
// The manager coordinates parsing, chunking, embedding, and storage of documents.
package index

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/memory/embeddings"
	"github.com/haasonsaas/nexus/internal/rag/chunker"
	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/internal/rag/store"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Manager coordinates the RAG indexing pipeline.
// It handles parsing, chunking, embedding, and storing documents.
type Manager struct {
	store    store.DocumentStore
	embedder embeddings.Provider
	chunker  chunker.Chunker
	config   *Config
}

// Config contains configuration for the index manager.
type Config struct {
	// ChunkSize is the target chunk size in characters.
	// Default: 1000
	ChunkSize int `yaml:"chunk_size"`

	// ChunkOverlap is the overlap between chunks in characters.
	// Default: 200
	ChunkOverlap int `yaml:"chunk_overlap"`

	// EmbeddingBatchSize is the maximum texts per embedding batch.
	// Default: 100
	EmbeddingBatchSize int `yaml:"embedding_batch_size"`

	// DefaultSource is the default source for uploaded documents.
	// Default: "upload"
	DefaultSource string `yaml:"default_source"`
}

// DefaultConfig returns the default manager configuration.
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:          1000,
		ChunkOverlap:       200,
		EmbeddingBatchSize: 100,
		DefaultSource:      "upload",
	}
}

// NewManager creates a new index manager.
func NewManager(docStore store.DocumentStore, embedder embeddings.Provider, cfg *Config) *Manager {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	ensureDefaultParsers()

	// Create chunker with config
	chunkCfg := chunker.Config{
		ChunkSize:    cfg.ChunkSize,
		ChunkOverlap: cfg.ChunkOverlap,
	}

	return &Manager{
		store:    docStore,
		embedder: embedder,
		chunker:  chunker.NewRecursiveCharacterTextSplitter(chunkCfg),
		config:   cfg,
	}
}

// WithChunker sets a custom chunker.
func (m *Manager) WithChunker(c chunker.Chunker) *Manager {
	m.chunker = c
	return m
}

// IndexRequest contains parameters for indexing a document.
type IndexRequest struct {
	// DocumentID is an optional deterministic ID to use for idempotent uploads.
	// If empty, a new UUID is generated.
	DocumentID string

	// Name is the document name.
	Name string

	// Source indicates where the document came from.
	Source string

	// SourceURI is the original URI or path.
	SourceURI string

	// ContentType is the MIME type.
	ContentType string

	// Content is the document content reader.
	Content io.Reader

	// Metadata contains additional document metadata.
	Metadata *models.DocumentMetadata
}

// IndexResult contains the result of indexing a document.
type IndexResult struct {
	// Document is the indexed document.
	Document *models.Document

	// ChunkCount is the number of chunks created.
	ChunkCount int

	// TotalTokens is the approximate token count.
	TotalTokens int

	// Duration is the total indexing time.
	Duration time.Duration
}

// Index processes and stores a document.
// This performs the full pipeline: parse -> chunk -> embed -> store.
func (m *Manager) Index(ctx context.Context, req *IndexRequest) (*IndexResult, error) {
	start := time.Now()

	// Validate request
	if req.Content == nil {
		return nil, fmt.Errorf("content is required")
	}
	if req.Name == "" {
		req.Name = "Untitled Document"
	}
	if req.Source == "" {
		req.Source = m.config.DefaultSource
	}

	// Determine extension from name or source URI
	ext := ""
	if req.SourceURI != "" {
		ext = filepath.Ext(req.SourceURI)
	}
	if ext == "" && req.Name != "" {
		ext = filepath.Ext(req.Name)
	}

	// Parse document
	p, err := parser.DefaultRegistry.Get(req.ContentType, ext)
	if err != nil {
		return nil, fmt.Errorf("no parser available: %w", err)
	}

	parseResult, err := p.Parse(ctx, req.Content, req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	metadata := models.DocumentMetadata{}
	if parseResult.Metadata != nil {
		metadata = *parseResult.Metadata
	}

	// Create document
	docID := strings.TrimSpace(req.DocumentID)
	if docID == "" {
		docID = uuid.New().String()
	}
	doc := &models.Document{
		ID:          docID,
		Name:        req.Name,
		Source:      req.Source,
		SourceURI:   req.SourceURI,
		ContentType: req.ContentType,
		Content:     parseResult.Content,
		Metadata:    metadata,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Chunk document
	chunks, err := m.chunker.Chunk(doc, parseResult)
	if err != nil {
		return nil, fmt.Errorf("chunking failed: %w", err)
	}

	// Calculate total tokens
	totalTokens := 0
	for _, chunk := range chunks {
		totalTokens += chunk.TokenCount
	}
	doc.TotalTokens = totalTokens
	doc.ChunkCount = len(chunks)

	// Generate embeddings
	if err := m.embedChunks(ctx, chunks); err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	// Store document and chunks
	if err := m.store.AddDocument(ctx, doc, chunks); err != nil {
		return nil, fmt.Errorf("storage failed: %w", err)
	}

	return &IndexResult{
		Document:    doc,
		ChunkCount:  len(chunks),
		TotalTokens: totalTokens,
		Duration:    time.Since(start),
	}, nil
}

// IndexText indexes text content directly without parsing.
func (m *Manager) IndexText(ctx context.Context, name, content string, metadata *models.DocumentMetadata) (*IndexResult, error) {
	return m.Index(ctx, &IndexRequest{
		Name:        name,
		Source:      m.config.DefaultSource,
		ContentType: "text/plain",
		Content:     strings.NewReader(content),
		Metadata:    metadata,
	})
}

// embedChunks generates embeddings for chunks in batches.
func (m *Manager) embedChunks(ctx context.Context, chunks []*models.DocumentChunk) error {
	if len(chunks) == 0 || m.embedder == nil {
		return nil
	}

	batchSize := m.embedder.MaxBatchSize()
	if m.config.EmbeddingBatchSize > 0 && m.config.EmbeddingBatchSize < batchSize {
		batchSize = m.config.EmbeddingBatchSize
	}

	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]

		texts := make([]string, len(batch))
		for j, chunk := range batch {
			texts[j] = chunk.Content
		}

		embeddings, err := m.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed batch %d: %w", i/batchSize, err)
		}

		for j, chunk := range batch {
			chunk.Embedding = embeddings[j]
		}
	}

	return nil
}

// Search performs semantic search over indexed documents.
func (m *Manager) Search(ctx context.Context, req *models.DocumentSearchRequest) (*models.DocumentSearchResponse, error) {
	// Generate query embedding
	queryEmbedding, err := m.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Search store
	return m.store.Search(ctx, req, queryEmbedding)
}

// GetDocument retrieves a document by ID.
func (m *Manager) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	return m.store.GetDocument(ctx, id)
}

// ListDocuments lists documents with optional filtering.
func (m *Manager) ListDocuments(ctx context.Context, opts *store.ListOptions) ([]*models.Document, error) {
	return m.store.ListDocuments(ctx, opts)
}

// DeleteDocument removes a document and all its chunks.
func (m *Manager) DeleteDocument(ctx context.Context, id string) error {
	return m.store.DeleteDocument(ctx, id)
}

// ReindexDocument re-chunks and re-embeds an existing document.
func (m *Manager) ReindexDocument(ctx context.Context, id string) (*IndexResult, error) {
	start := time.Now()

	// Get existing document
	doc, err := m.store.GetDocument(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("document not found: %s", id)
	}

	// Parse content again
	p, err := parser.DefaultRegistry.Get(doc.ContentType, "")
	if err != nil {
		return nil, fmt.Errorf("no parser available: %w", err)
	}

	parseResult, err := p.Parse(ctx, strings.NewReader(doc.Content), &doc.Metadata)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	// Re-chunk
	chunks, err := m.chunker.Chunk(doc, parseResult)
	if err != nil {
		return nil, fmt.Errorf("chunking failed: %w", err)
	}

	// Calculate total tokens
	totalTokens := 0
	for _, chunk := range chunks {
		totalTokens += chunk.TokenCount
	}
	doc.TotalTokens = totalTokens
	doc.ChunkCount = len(chunks)
	doc.UpdatedAt = time.Now()

	// Generate embeddings
	if err := m.embedChunks(ctx, chunks); err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	// Store updated document and chunks
	if err := m.store.AddDocument(ctx, doc, chunks); err != nil {
		return nil, fmt.Errorf("storage failed: %w", err)
	}

	return &IndexResult{
		Document:    doc,
		ChunkCount:  len(chunks),
		TotalTokens: totalTokens,
		Duration:    time.Since(start),
	}, nil
}

// Stats returns statistics about the index.
func (m *Manager) Stats(ctx context.Context) (*store.StoreStats, error) {
	return m.store.Stats(ctx)
}

// Close releases resources.
func (m *Manager) Close() error {
	return m.store.Close()
}
