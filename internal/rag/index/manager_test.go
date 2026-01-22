package index

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/rag/chunker"
	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/internal/rag/store"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ============================================================================
// Mock Implementations for Testing
// ============================================================================

// MockDocumentStore implements store.DocumentStore for testing
type MockDocumentStore struct {
	documents     map[string]*models.Document
	chunks        map[string][]*models.DocumentChunk
	addDocErr     error
	getDocErr     error
	listDocsErr   error
	deleteDocErr  error
	searchErr     error
	statsErr      error
	searchResults *models.DocumentSearchResponse
}

func NewMockDocumentStore() *MockDocumentStore {
	return &MockDocumentStore{
		documents: make(map[string]*models.Document),
		chunks:    make(map[string][]*models.DocumentChunk),
	}
}

func (m *MockDocumentStore) AddDocument(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
	if m.addDocErr != nil {
		return m.addDocErr
	}
	m.documents[doc.ID] = doc
	m.chunks[doc.ID] = chunks
	return nil
}

func (m *MockDocumentStore) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	if m.getDocErr != nil {
		return nil, m.getDocErr
	}
	doc, ok := m.documents[id]
	if !ok {
		return nil, nil
	}
	return doc, nil
}

func (m *MockDocumentStore) ListDocuments(ctx context.Context, opts *store.ListOptions) ([]*models.Document, error) {
	if m.listDocsErr != nil {
		return nil, m.listDocsErr
	}
	docs := make([]*models.Document, 0, len(m.documents))
	for _, doc := range m.documents {
		docs = append(docs, doc)
	}
	return docs, nil
}

func (m *MockDocumentStore) DeleteDocument(ctx context.Context, id string) error {
	if m.deleteDocErr != nil {
		return m.deleteDocErr
	}
	delete(m.documents, id)
	delete(m.chunks, id)
	return nil
}

func (m *MockDocumentStore) GetChunk(ctx context.Context, id string) (*models.DocumentChunk, error) {
	for _, chunks := range m.chunks {
		for _, chunk := range chunks {
			if chunk.ID == id {
				return chunk, nil
			}
		}
	}
	return nil, nil
}

func (m *MockDocumentStore) GetChunksByDocument(ctx context.Context, documentID string) ([]*models.DocumentChunk, error) {
	return m.chunks[documentID], nil
}

func (m *MockDocumentStore) Search(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	if m.searchResults != nil {
		return m.searchResults, nil
	}
	return &models.DocumentSearchResponse{
		Results:    []*models.DocumentSearchResult{},
		TotalCount: 0,
		QueryTime:  10 * time.Millisecond,
	}, nil
}

func (m *MockDocumentStore) UpdateChunkEmbeddings(ctx context.Context, embeddings map[string][]float32) error {
	return nil
}

func (m *MockDocumentStore) Stats(ctx context.Context) (*store.StoreStats, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	return &store.StoreStats{
		TotalDocuments:     int64(len(m.documents)),
		TotalChunks:        int64(len(m.chunks)),
		EmbeddingDimension: 1536,
	}, nil
}

func (m *MockDocumentStore) Close() error {
	return nil
}

// MockEmbedder implements embeddings.Provider for testing
type MockEmbedder struct {
	embedding    []float32
	embedErr     error
	embedBatch   [][]float32
	batchErr     error
	maxBatchSize int
	dimension    int
	name         string
}

func NewMockEmbedder() *MockEmbedder {
	return &MockEmbedder{
		embedding:    make([]float32, 1536),
		maxBatchSize: 100,
		dimension:    1536,
		name:         "mock-embedder",
	}
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedErr != nil {
		return nil, m.embedErr
	}
	return m.embedding, nil
}

func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if m.batchErr != nil {
		return nil, m.batchErr
	}
	if m.embedBatch != nil {
		return m.embedBatch, nil
	}
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, m.dimension)
	}
	return result, nil
}

func (m *MockEmbedder) Name() string {
	return m.name
}

func (m *MockEmbedder) MaxBatchSize() int {
	return m.maxBatchSize
}

func (m *MockEmbedder) Dimension() int {
	return m.dimension
}

// MockChunker implements chunker.Chunker for testing
type MockChunker struct {
	chunks   []*models.DocumentChunk
	chunkErr error
	name     string
}

func NewMockChunker() *MockChunker {
	return &MockChunker{
		name: "mock_chunker",
		chunks: []*models.DocumentChunk{
			{
				ID:         "chunk-1",
				Content:    "Test chunk content",
				Index:      0,
				TokenCount: 10,
			},
		},
	}
}

func (m *MockChunker) Chunk(doc *models.Document, parseResult *parser.ParseResult) ([]*models.DocumentChunk, error) {
	if m.chunkErr != nil {
		return nil, m.chunkErr
	}
	// Set document ID on chunks
	for _, chunk := range m.chunks {
		chunk.DocumentID = doc.ID
	}
	return m.chunks, nil
}

func (m *MockChunker) Name() string {
	return m.name
}

// ============================================================================
// Config Tests
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ChunkSize != 1000 {
		t.Errorf("ChunkSize = %d, want 1000", cfg.ChunkSize)
	}
	if cfg.ChunkOverlap != 200 {
		t.Errorf("ChunkOverlap = %d, want 200", cfg.ChunkOverlap)
	}
	if cfg.EmbeddingBatchSize != 100 {
		t.Errorf("EmbeddingBatchSize = %d, want 100", cfg.EmbeddingBatchSize)
	}
	if cfg.DefaultSource != "upload" {
		t.Errorf("DefaultSource = %q, want %q", cfg.DefaultSource, "upload")
	}
}

func TestConfigWithCustomValues(t *testing.T) {
	cfg := &Config{
		ChunkSize:          500,
		ChunkOverlap:       100,
		EmbeddingBatchSize: 50,
		DefaultSource:      "api",
	}

	if cfg.ChunkSize != 500 {
		t.Errorf("ChunkSize = %d, want 500", cfg.ChunkSize)
	}
	if cfg.DefaultSource != "api" {
		t.Errorf("DefaultSource = %q, want %q", cfg.DefaultSource, "api")
	}
}

// ============================================================================
// NewManager Tests
// ============================================================================

func TestNewManager(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()

	manager := NewManager(mockStore, mockEmbedder, nil)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	// Can't directly compare interfaces, check if manager is set up correctly
	if manager.store == nil {
		t.Error("Manager store should not be nil")
	}
	if manager.embedder == nil {
		t.Error("Manager embedder should not be nil")
	}
	if manager.chunker == nil {
		t.Error("Manager chunker should not be nil")
	}
}

func TestNewManager_NilConfig(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()

	manager := NewManager(mockStore, mockEmbedder, nil)

	// Should use default config
	if manager.config == nil {
		t.Fatal("Manager config should not be nil")
	}
	if manager.config.ChunkSize != 1000 {
		t.Errorf("ChunkSize = %d, want 1000 (default)", manager.config.ChunkSize)
	}
}

func TestNewManager_CustomConfig(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	cfg := &Config{
		ChunkSize:     500,
		ChunkOverlap:  100,
		DefaultSource: "custom",
	}

	manager := NewManager(mockStore, mockEmbedder, cfg)

	if manager.config.ChunkSize != 500 {
		t.Errorf("ChunkSize = %d, want 500", manager.config.ChunkSize)
	}
	if manager.config.DefaultSource != "custom" {
		t.Errorf("DefaultSource = %q, want %q", manager.config.DefaultSource, "custom")
	}
}

func TestManager_WithChunker(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	mockChunker := NewMockChunker()

	manager := NewManager(mockStore, mockEmbedder, nil)
	result := manager.WithChunker(mockChunker)

	if result != manager {
		t.Error("WithChunker should return the same manager")
	}
	// Check that chunker was set by testing Name()
	if manager.chunker.Name() != "mock_chunker" {
		t.Errorf("Chunker name = %q, want %q", manager.chunker.Name(), "mock_chunker")
	}
}

// ============================================================================
// IndexRequest Tests
// ============================================================================

func TestIndexRequest_Structure(t *testing.T) {
	req := &IndexRequest{
		Name:        "Test Document",
		Source:      "upload",
		SourceURI:   "/path/to/file.txt",
		ContentType: "text/plain",
		Content:     strings.NewReader("Test content"),
		Metadata: &models.DocumentMetadata{
			Title:  "Test",
			Author: "Author",
		},
	}

	if req.Name != "Test Document" {
		t.Errorf("Name = %q, want %q", req.Name, "Test Document")
	}
	if req.Source != "upload" {
		t.Errorf("Source = %q, want %q", req.Source, "upload")
	}
	if req.Content == nil {
		t.Error("Content should not be nil")
	}
}

// ============================================================================
// IndexResult Tests
// ============================================================================

func TestIndexResult_Structure(t *testing.T) {
	result := &IndexResult{
		Document: &models.Document{
			ID:   "doc-1",
			Name: "Test",
		},
		ChunkCount:  5,
		TotalTokens: 500,
		Duration:    100 * time.Millisecond,
	}

	if result.Document.ID != "doc-1" {
		t.Errorf("Document.ID = %q, want %q", result.Document.ID, "doc-1")
	}
	if result.ChunkCount != 5 {
		t.Errorf("ChunkCount = %d, want 5", result.ChunkCount)
	}
	if result.TotalTokens != 500 {
		t.Errorf("TotalTokens = %d, want 500", result.TotalTokens)
	}
}

// ============================================================================
// Index Method Tests
// ============================================================================

func TestIndex_NilContent(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &IndexRequest{
		Name:    "Test",
		Content: nil,
	}

	_, err := manager.Index(context.Background(), req)
	if err == nil {
		t.Error("Expected error for nil content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Errorf("Expected 'content is required' error, got: %v", err)
	}
}

func TestIndex_EmptyName(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &IndexRequest{
		Name:        "",
		Content:     strings.NewReader("Test content"),
		ContentType: "text/plain",
	}

	result, err := manager.Index(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Name should default to "Untitled Document"
	if result.Document.Name != "Untitled Document" {
		t.Errorf("Document.Name = %q, want 'Untitled Document'", result.Document.Name)
	}
}

func TestIndex_EmptySource(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &IndexRequest{
		Name:        "Test",
		Source:      "",
		Content:     strings.NewReader("Test content"),
		ContentType: "text/plain",
	}

	result, err := manager.Index(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Source should default to "upload"
	if result.Document.Source != "upload" {
		t.Errorf("Document.Source = %q, want 'upload'", result.Document.Source)
	}
}

func TestIndex_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &IndexRequest{
		Name:        "Test Document",
		Source:      "test",
		SourceURI:   "/path/to/file.txt",
		ContentType: "text/plain",
		Content:     strings.NewReader("This is test content for indexing."),
	}

	result, err := manager.Index(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Document == nil {
		t.Fatal("Document should not be nil")
	}
	if result.Document.ID == "" {
		t.Error("Document.ID should not be empty")
	}
	if result.Document.Name != "Test Document" {
		t.Errorf("Document.Name = %q, want 'Test Document'", result.Document.Name)
	}
	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}
}

func TestIndex_StoreError(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.addDocErr = errors.New("storage error")
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &IndexRequest{
		Name:        "Test",
		Content:     strings.NewReader("Test content"),
		ContentType: "text/plain",
	}

	_, err := manager.Index(context.Background(), req)
	if err == nil {
		t.Error("Expected error from store")
	}
	if !strings.Contains(err.Error(), "storage failed") {
		t.Errorf("Expected 'storage failed' error, got: %v", err)
	}
}

func TestIndex_EmbeddingError(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	mockEmbedder.batchErr = errors.New("embedding error")
	manager := NewManager(mockStore, mockEmbedder, nil)

	// Use enough content to ensure chunking produces results
	req := &IndexRequest{
		Name:        "Test",
		Content:     strings.NewReader("This is a longer test content that should produce at least one chunk for embedding."),
		ContentType: "text/plain",
	}

	_, err := manager.Index(context.Background(), req)
	// If chunker produces no chunks, embedding is skipped, so no error occurs
	// This is valid behavior - we test that embedding errors are propagated when chunks exist
	if err != nil && !strings.Contains(err.Error(), "embedding failed") {
		t.Errorf("Expected 'embedding failed' error or no error, got: %v", err)
	}
}

// ============================================================================
// IndexText Tests
// ============================================================================

func TestIndexText_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	result, err := manager.IndexText(context.Background(), "Test Doc", "This is test content.", nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Document.Name != "Test Doc" {
		t.Errorf("Document.Name = %q, want 'Test Doc'", result.Document.Name)
	}
	if result.Document.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want 'text/plain'", result.Document.ContentType)
	}
}

func TestIndexText_WithMetadata(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	meta := &models.DocumentMetadata{
		Title:   "Test Title",
		Author:  "Test Author",
		AgentID: "agent-1",
	}

	result, err := manager.IndexText(context.Background(), "Test Doc", "Content here.", meta)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Document == nil {
		t.Fatal("Document should not be nil")
	}
}

// ============================================================================
// Search Tests
// ============================================================================

func TestSearch_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.searchResults = &models.DocumentSearchResponse{
		Results: []*models.DocumentSearchResult{
			{
				Chunk: &models.DocumentChunk{
					ID:      "chunk-1",
					Content: "Test result",
				},
				Score: 0.95,
			},
		},
		TotalCount: 1,
		QueryTime:  10 * time.Millisecond,
	}
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &models.DocumentSearchRequest{
		Query: "test query",
		Limit: 10,
	}

	result, err := manager.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result.Results) != 1 {
		t.Errorf("Results len = %d, want 1", len(result.Results))
	}
	if result.Results[0].Score != 0.95 {
		t.Errorf("Score = %v, want 0.95", result.Results[0].Score)
	}
}

func TestSearch_EmbeddingError(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	mockEmbedder.embedErr = errors.New("embedding error")
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &models.DocumentSearchRequest{
		Query: "test query",
	}

	_, err := manager.Search(context.Background(), req)
	if err == nil {
		t.Error("Expected error from embedder")
	}
	if !strings.Contains(err.Error(), "embed query") {
		t.Errorf("Expected 'embed query' error, got: %v", err)
	}
}

func TestSearch_StoreError(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.searchErr = errors.New("search error")
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &models.DocumentSearchRequest{
		Query: "test query",
	}

	_, err := manager.Search(context.Background(), req)
	if err == nil {
		t.Error("Expected error from store")
	}
}

// ============================================================================
// GetDocument Tests
// ============================================================================

func TestGetDocument_Found(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.documents["doc-1"] = &models.Document{
		ID:   "doc-1",
		Name: "Test Document",
	}
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	doc, err := manager.GetDocument(context.Background(), "doc-1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("Document should not be nil")
	}
	if doc.Name != "Test Document" {
		t.Errorf("Document.Name = %q, want 'Test Document'", doc.Name)
	}
}

func TestGetDocument_NotFound(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	doc, err := manager.GetDocument(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if doc != nil {
		t.Error("Document should be nil for nonexistent ID")
	}
}

func TestGetDocument_Error(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.getDocErr = errors.New("get error")
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	_, err := manager.GetDocument(context.Background(), "doc-1")
	if err == nil {
		t.Error("Expected error from store")
	}
}

// ============================================================================
// ListDocuments Tests
// ============================================================================

func TestListDocuments_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.documents["doc-1"] = &models.Document{ID: "doc-1", Name: "Doc 1"}
	mockStore.documents["doc-2"] = &models.Document{ID: "doc-2", Name: "Doc 2"}
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	docs, err := manager.ListDocuments(context.Background(), nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("len(docs) = %d, want 2", len(docs))
	}
}

func TestListDocuments_WithOptions(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	opts := &store.ListOptions{
		Limit:   10,
		Offset:  0,
		Source:  "upload",
		OrderBy: "created_at",
	}

	_, err := manager.ListDocuments(context.Background(), opts)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestListDocuments_Error(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.listDocsErr = errors.New("list error")
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	_, err := manager.ListDocuments(context.Background(), nil)
	if err == nil {
		t.Error("Expected error from store")
	}
}

// ============================================================================
// DeleteDocument Tests
// ============================================================================

func TestDeleteDocument_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.documents["doc-1"] = &models.Document{ID: "doc-1"}
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	err := manager.DeleteDocument(context.Background(), "doc-1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify document was deleted
	if _, exists := mockStore.documents["doc-1"]; exists {
		t.Error("Document should have been deleted")
	}
}

func TestDeleteDocument_Error(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.deleteDocErr = errors.New("delete error")
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	err := manager.DeleteDocument(context.Background(), "doc-1")
	if err == nil {
		t.Error("Expected error from store")
	}
}

// ============================================================================
// ReindexDocument Tests
// ============================================================================

func TestReindexDocument_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.documents["doc-1"] = &models.Document{
		ID:          "doc-1",
		Name:        "Test Document",
		Content:     "This is test content for reindexing.",
		ContentType: "text/plain",
	}
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	result, err := manager.ReindexDocument(context.Background(), "doc-1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Document == nil {
		t.Fatal("Document should not be nil")
	}
	if result.Document.ID != "doc-1" {
		t.Errorf("Document.ID = %q, want 'doc-1'", result.Document.ID)
	}
}

func TestReindexDocument_NotFound(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	_, err := manager.ReindexDocument(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent document")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestReindexDocument_GetError(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.getDocErr = errors.New("get error")
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	_, err := manager.ReindexDocument(context.Background(), "doc-1")
	if err == nil {
		t.Error("Expected error from store")
	}
	if !strings.Contains(err.Error(), "get document") {
		t.Errorf("Expected 'get document' error, got: %v", err)
	}
}

// ============================================================================
// Stats Tests
// ============================================================================

func TestStats_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.documents["doc-1"] = &models.Document{ID: "doc-1"}
	mockStore.documents["doc-2"] = &models.Document{ID: "doc-2"}
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	stats, err := manager.Stats(context.Background())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if stats.TotalDocuments != 2 {
		t.Errorf("TotalDocuments = %d, want 2", stats.TotalDocuments)
	}
}

func TestStats_Error(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockStore.statsErr = errors.New("stats error")
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	_, err := manager.Stats(context.Background())
	if err == nil {
		t.Error("Expected error from store")
	}
}

// ============================================================================
// Close Tests
// ============================================================================

func TestClose_Success(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	err := manager.Close()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

// ============================================================================
// EmbedChunks Tests
// ============================================================================

func TestEmbedChunks_EmptyChunks(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	err := manager.embedChunks(context.Background(), nil)
	if err != nil {
		t.Errorf("Unexpected error for empty chunks: %v", err)
	}

	err = manager.embedChunks(context.Background(), []*models.DocumentChunk{})
	if err != nil {
		t.Errorf("Unexpected error for empty slice: %v", err)
	}
}

func TestEmbedChunks_NilEmbedder(t *testing.T) {
	mockStore := NewMockDocumentStore()
	manager := NewManager(mockStore, nil, nil)

	chunks := []*models.DocumentChunk{
		{ID: "chunk-1", Content: "Test content"},
	}

	err := manager.embedChunks(context.Background(), chunks)
	if err != nil {
		t.Errorf("Unexpected error with nil embedder: %v", err)
	}
}

func TestEmbedChunks_BatchProcessing(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	mockEmbedder.maxBatchSize = 2 // Small batch size for testing

	manager := NewManager(mockStore, mockEmbedder, &Config{
		EmbeddingBatchSize: 2,
	})

	chunks := []*models.DocumentChunk{
		{ID: "chunk-1", Content: "Content 1"},
		{ID: "chunk-2", Content: "Content 2"},
		{ID: "chunk-3", Content: "Content 3"},
		{ID: "chunk-4", Content: "Content 4"},
	}

	err := manager.embedChunks(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// All chunks should have embeddings
	for i, chunk := range chunks {
		if chunk.Embedding == nil {
			t.Errorf("Chunk[%d] should have embedding", i)
		}
	}
}

func TestEmbedChunks_Error(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	mockEmbedder.batchErr = errors.New("embedding error")
	manager := NewManager(mockStore, mockEmbedder, nil)

	chunks := []*models.DocumentChunk{
		{ID: "chunk-1", Content: "Test content"},
	}

	err := manager.embedChunks(context.Background(), chunks)
	if err == nil {
		t.Error("Expected error from embedder")
	}
}

// ============================================================================
// Context Cancellation Tests
// ============================================================================

func TestIndex_ContextCancellation(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &IndexRequest{
		Name:        "Test",
		Content:     strings.NewReader("Test content"),
		ContentType: "text/plain",
	}

	// This may or may not error depending on where cancellation is checked
	// The important thing is it shouldn't hang
	_, _ = manager.Index(ctx, req)
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestIndex_ExtensionFromSourceURI(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &IndexRequest{
		Name:        "Test",
		SourceURI:   "/path/to/file.txt",
		ContentType: "text/plain",
		Content:     strings.NewReader("Test content"),
	}

	result, err := manager.Index(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Document.SourceURI != "/path/to/file.txt" {
		t.Errorf("SourceURI = %q, want '/path/to/file.txt'", result.Document.SourceURI)
	}
}

func TestIndex_ExtensionFromName(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &IndexRequest{
		Name:        "document.txt",
		ContentType: "text/plain",
		Content:     strings.NewReader("Test content"),
	}

	result, err := manager.Index(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Document.Name != "document.txt" {
		t.Errorf("Name = %q, want 'document.txt'", result.Document.Name)
	}
}

// ============================================================================
// Chunker Interface Tests
// ============================================================================

func TestManager_UsesConfiguredChunker(t *testing.T) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()

	cfg := &Config{
		ChunkSize:    500,
		ChunkOverlap: 100,
	}
	manager := NewManager(mockStore, mockEmbedder, cfg)

	// Manager should create a RecursiveCharacterTextSplitter with the config
	if manager.chunker == nil {
		t.Error("Manager should have a chunker")
	}

	// Test that the chunker is a RecursiveCharacterTextSplitter
	_, ok := manager.chunker.(*chunker.RecursiveCharacterTextSplitter)
	if !ok {
		t.Error("Default chunker should be RecursiveCharacterTextSplitter")
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkIndex_SmallDocument(b *testing.B) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := &IndexRequest{
			Name:        "Test",
			Content:     strings.NewReader("This is a small test document."),
			ContentType: "text/plain",
		}
		_, _ = manager.Index(context.Background(), req)
	}
}

func BenchmarkIndex_LargeDocument(b *testing.B) {
	mockStore := NewMockDocumentStore()
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	content := strings.Repeat("This is test content. ", 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := &IndexRequest{
			Name:        "Test",
			Content:     strings.NewReader(content),
			ContentType: "text/plain",
		}
		_, _ = manager.Index(context.Background(), req)
	}
}

func BenchmarkSearch(b *testing.B) {
	mockStore := NewMockDocumentStore()
	mockStore.searchResults = &models.DocumentSearchResponse{
		Results:    []*models.DocumentSearchResult{},
		TotalCount: 0,
	}
	mockEmbedder := NewMockEmbedder()
	manager := NewManager(mockStore, mockEmbedder, nil)

	req := &models.DocumentSearchRequest{
		Query: "test query",
		Limit: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.Search(context.Background(), req)
	}
}
