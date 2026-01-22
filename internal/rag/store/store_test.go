// Package store provides document storage interfaces and implementations
// for the RAG (Retrieval-Augmented Generation) system.
package store

import (
	"context"
	"errors"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

// MockDocumentStore provides a mock implementation of DocumentStore for testing.
type MockDocumentStore struct {
	AddDocumentFunc           func(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error
	GetDocumentFunc           func(ctx context.Context, id string) (*models.Document, error)
	ListDocumentsFunc         func(ctx context.Context, opts *ListOptions) ([]*models.Document, error)
	DeleteDocumentFunc        func(ctx context.Context, id string) error
	GetChunkFunc              func(ctx context.Context, id string) (*models.DocumentChunk, error)
	GetChunksByDocumentFunc   func(ctx context.Context, documentID string) ([]*models.DocumentChunk, error)
	SearchFunc                func(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error)
	UpdateChunkEmbeddingsFunc func(ctx context.Context, embeddings map[string][]float32) error
	StatsFunc                 func(ctx context.Context) (*StoreStats, error)
	CloseFunc                 func() error
}

func (m *MockDocumentStore) AddDocument(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
	if m.AddDocumentFunc != nil {
		return m.AddDocumentFunc(ctx, doc, chunks)
	}
	return nil
}

func (m *MockDocumentStore) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	if m.GetDocumentFunc != nil {
		return m.GetDocumentFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockDocumentStore) ListDocuments(ctx context.Context, opts *ListOptions) ([]*models.Document, error) {
	if m.ListDocumentsFunc != nil {
		return m.ListDocumentsFunc(ctx, opts)
	}
	return nil, nil
}

func (m *MockDocumentStore) DeleteDocument(ctx context.Context, id string) error {
	if m.DeleteDocumentFunc != nil {
		return m.DeleteDocumentFunc(ctx, id)
	}
	return nil
}

func (m *MockDocumentStore) GetChunk(ctx context.Context, id string) (*models.DocumentChunk, error) {
	if m.GetChunkFunc != nil {
		return m.GetChunkFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockDocumentStore) GetChunksByDocument(ctx context.Context, documentID string) ([]*models.DocumentChunk, error) {
	if m.GetChunksByDocumentFunc != nil {
		return m.GetChunksByDocumentFunc(ctx, documentID)
	}
	return nil, nil
}

func (m *MockDocumentStore) Search(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
	if m.SearchFunc != nil {
		return m.SearchFunc(ctx, req, embedding)
	}
	return &models.DocumentSearchResponse{}, nil
}

func (m *MockDocumentStore) UpdateChunkEmbeddings(ctx context.Context, embeddings map[string][]float32) error {
	if m.UpdateChunkEmbeddingsFunc != nil {
		return m.UpdateChunkEmbeddingsFunc(ctx, embeddings)
	}
	return nil
}

func (m *MockDocumentStore) Stats(ctx context.Context) (*StoreStats, error) {
	if m.StatsFunc != nil {
		return m.StatsFunc(ctx)
	}
	return &StoreStats{}, nil
}

func (m *MockDocumentStore) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Verify MockDocumentStore implements DocumentStore
var _ DocumentStore = (*MockDocumentStore)(nil)

func TestMockDocumentStore_ImplementsInterface(t *testing.T) {
	// This test verifies that MockDocumentStore properly implements DocumentStore
	var store DocumentStore = &MockDocumentStore{}
	if store == nil {
		t.Error("MockDocumentStore should implement DocumentStore")
	}
}

func TestMockDocumentStore_AddDocument(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(m *MockDocumentStore)
		doc     *models.Document
		chunks  []*models.DocumentChunk
		wantErr bool
	}{
		{
			name:    "default implementation returns nil",
			setup:   func(m *MockDocumentStore) {},
			doc:     &models.Document{ID: "doc-1"},
			chunks:  nil,
			wantErr: false,
		},
		{
			name: "custom implementation succeeds",
			setup: func(m *MockDocumentStore) {
				m.AddDocumentFunc = func(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
					return nil
				}
			},
			doc:     &models.Document{ID: "doc-1"},
			chunks:  []*models.DocumentChunk{{ID: "chunk-1"}},
			wantErr: false,
		},
		{
			name: "custom implementation returns error",
			setup: func(m *MockDocumentStore) {
				m.AddDocumentFunc = func(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
					return errors.New("add document error")
				}
			},
			doc:     &models.Document{ID: "doc-1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MockDocumentStore{}
			tt.setup(m)

			err := m.AddDocument(context.Background(), tt.doc, tt.chunks)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddDocument() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMockDocumentStore_GetDocument(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(m *MockDocumentStore)
		id      string
		wantDoc *models.Document
		wantErr bool
	}{
		{
			name:    "default returns nil",
			setup:   func(m *MockDocumentStore) {},
			id:      "doc-1",
			wantDoc: nil,
			wantErr: false,
		},
		{
			name: "returns document",
			setup: func(m *MockDocumentStore) {
				m.GetDocumentFunc = func(ctx context.Context, id string) (*models.Document, error) {
					return &models.Document{ID: id, Name: "Test Doc"}, nil
				}
			},
			id:      "doc-1",
			wantDoc: &models.Document{ID: "doc-1", Name: "Test Doc"},
			wantErr: false,
		},
		{
			name: "returns error",
			setup: func(m *MockDocumentStore) {
				m.GetDocumentFunc = func(ctx context.Context, id string) (*models.Document, error) {
					return nil, errors.New("not found")
				}
			},
			id:      "doc-1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MockDocumentStore{}
			tt.setup(m)

			doc, err := m.GetDocument(context.Background(), tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDocument() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantDoc != nil && (doc == nil || doc.ID != tt.wantDoc.ID) {
				t.Errorf("GetDocument() = %v, want %v", doc, tt.wantDoc)
			}
		})
	}
}

func TestMockDocumentStore_Search(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(m *MockDocumentStore)
		req       *models.DocumentSearchRequest
		embedding []float32
		wantCount int
		wantErr   bool
	}{
		{
			name:      "default returns empty response",
			setup:     func(m *MockDocumentStore) {},
			req:       &models.DocumentSearchRequest{Query: "test"},
			embedding: []float32{0.1, 0.2, 0.3},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "returns results",
			setup: func(m *MockDocumentStore) {
				m.SearchFunc = func(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
					return &models.DocumentSearchResponse{
						Results: []*models.DocumentSearchResult{
							{Chunk: &models.DocumentChunk{ID: "chunk-1"}, Score: 0.9},
							{Chunk: &models.DocumentChunk{ID: "chunk-2"}, Score: 0.8},
						},
						TotalCount: 2,
					}, nil
				}
			},
			req:       &models.DocumentSearchRequest{Query: "test"},
			embedding: []float32{0.1, 0.2, 0.3},
			wantCount: 2,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MockDocumentStore{}
			tt.setup(m)

			resp, err := m.Search(context.Background(), tt.req, tt.embedding)
			if (err != nil) != tt.wantErr {
				t.Errorf("Search() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(resp.Results) != tt.wantCount {
				t.Errorf("Search() results = %d, want %d", len(resp.Results), tt.wantCount)
			}
		})
	}
}

func TestListOptions_Fields(t *testing.T) {
	opts := &ListOptions{
		Limit:     50,
		Offset:    10,
		Source:    "upload",
		Tags:      []string{"tag1", "tag2"},
		AgentID:   "agent-1",
		SessionID: "session-1",
		ChannelID: "channel-1",
		OrderBy:   "created_at",
		OrderDesc: true,
	}

	if opts.Limit != 50 {
		t.Errorf("Limit = %d, want 50", opts.Limit)
	}
	if opts.Offset != 10 {
		t.Errorf("Offset = %d, want 10", opts.Offset)
	}
	if opts.Source != "upload" {
		t.Errorf("Source = %q, want 'upload'", opts.Source)
	}
	if len(opts.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(opts.Tags))
	}
	if opts.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want 'agent-1'", opts.AgentID)
	}
	if opts.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want 'session-1'", opts.SessionID)
	}
	if opts.ChannelID != "channel-1" {
		t.Errorf("ChannelID = %q, want 'channel-1'", opts.ChannelID)
	}
	if opts.OrderBy != "created_at" {
		t.Errorf("OrderBy = %q, want 'created_at'", opts.OrderBy)
	}
	if !opts.OrderDesc {
		t.Error("OrderDesc should be true")
	}
}

func TestStoreStats_Fields(t *testing.T) {
	stats := &StoreStats{
		TotalDocuments:     100,
		TotalChunks:        500,
		TotalTokens:        50000,
		StorageBytes:       1024 * 1024,
		EmbeddingDimension: 1536,
	}

	if stats.TotalDocuments != 100 {
		t.Errorf("TotalDocuments = %d, want 100", stats.TotalDocuments)
	}
	if stats.TotalChunks != 500 {
		t.Errorf("TotalChunks = %d, want 500", stats.TotalChunks)
	}
	if stats.TotalTokens != 50000 {
		t.Errorf("TotalTokens = %d, want 50000", stats.TotalTokens)
	}
	if stats.StorageBytes != 1024*1024 {
		t.Errorf("StorageBytes = %d, want %d", stats.StorageBytes, 1024*1024)
	}
	if stats.EmbeddingDimension != 1536 {
		t.Errorf("EmbeddingDimension = %d, want 1536", stats.EmbeddingDimension)
	}
}

func TestSearchOptions_Fields(t *testing.T) {
	opts := &SearchOptions{
		Scope:           models.DocumentScopeAgent,
		ScopeID:         "agent-1",
		Limit:           20,
		Threshold:       0.8,
		Tags:            []string{"important"},
		DocumentIDs:     []string{"doc-1", "doc-2"},
		IncludeMetadata: true,
	}

	if opts.Scope != models.DocumentScopeAgent {
		t.Errorf("Scope = %v, want DocumentScopeAgent", opts.Scope)
	}
	if opts.ScopeID != "agent-1" {
		t.Errorf("ScopeID = %q, want 'agent-1'", opts.ScopeID)
	}
	if opts.Limit != 20 {
		t.Errorf("Limit = %d, want 20", opts.Limit)
	}
	if opts.Threshold != 0.8 {
		t.Errorf("Threshold = %f, want 0.8", opts.Threshold)
	}
	if len(opts.Tags) != 1 {
		t.Errorf("Tags len = %d, want 1", len(opts.Tags))
	}
	if len(opts.DocumentIDs) != 2 {
		t.Errorf("DocumentIDs len = %d, want 2", len(opts.DocumentIDs))
	}
	if !opts.IncludeMetadata {
		t.Error("IncludeMetadata should be true")
	}
}

func TestDefaultSearchOptions(t *testing.T) {
	opts := DefaultSearchOptions()

	if opts == nil {
		t.Fatal("DefaultSearchOptions() returned nil")
	}
	if opts.Scope != models.DocumentScopeGlobal {
		t.Errorf("Scope = %v, want DocumentScopeGlobal", opts.Scope)
	}
	if opts.Limit != 10 {
		t.Errorf("Limit = %d, want 10", opts.Limit)
	}
	if opts.Threshold != 0.7 {
		t.Errorf("Threshold = %f, want 0.7", opts.Threshold)
	}
}

func TestMockDocumentStore_ListDocuments(t *testing.T) {
	m := &MockDocumentStore{
		ListDocumentsFunc: func(ctx context.Context, opts *ListOptions) ([]*models.Document, error) {
			docs := []*models.Document{
				{ID: "doc-1", Name: "Doc 1"},
				{ID: "doc-2", Name: "Doc 2"},
			}
			if opts != nil && opts.Limit > 0 && opts.Limit < len(docs) {
				docs = docs[:opts.Limit]
			}
			return docs, nil
		},
	}

	// Test without options
	docs, err := m.ListDocuments(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDocuments() error = %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("ListDocuments() returned %d docs, want 2", len(docs))
	}

	// Test with limit
	docs, err = m.ListDocuments(context.Background(), &ListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListDocuments() error = %v", err)
	}
	if len(docs) != 1 {
		t.Errorf("ListDocuments() with limit returned %d docs, want 1", len(docs))
	}
}

func TestMockDocumentStore_DeleteDocument(t *testing.T) {
	deleted := false
	m := &MockDocumentStore{
		DeleteDocumentFunc: func(ctx context.Context, id string) error {
			if id == "doc-1" {
				deleted = true
				return nil
			}
			return errors.New("not found")
		},
	}

	err := m.DeleteDocument(context.Background(), "doc-1")
	if err != nil {
		t.Errorf("DeleteDocument() error = %v", err)
	}
	if !deleted {
		t.Error("Document should be deleted")
	}

	err = m.DeleteDocument(context.Background(), "nonexistent")
	if err == nil {
		t.Error("DeleteDocument() should return error for nonexistent document")
	}
}

func TestMockDocumentStore_GetChunk(t *testing.T) {
	m := &MockDocumentStore{
		GetChunkFunc: func(ctx context.Context, id string) (*models.DocumentChunk, error) {
			if id == "chunk-1" {
				return &models.DocumentChunk{ID: id, Content: "Test content"}, nil
			}
			return nil, errors.New("chunk not found")
		},
	}

	chunk, err := m.GetChunk(context.Background(), "chunk-1")
	if err != nil {
		t.Fatalf("GetChunk() error = %v", err)
	}
	if chunk.ID != "chunk-1" {
		t.Errorf("GetChunk() ID = %q, want 'chunk-1'", chunk.ID)
	}

	_, err = m.GetChunk(context.Background(), "nonexistent")
	if err == nil {
		t.Error("GetChunk() should return error for nonexistent chunk")
	}
}

func TestMockDocumentStore_GetChunksByDocument(t *testing.T) {
	m := &MockDocumentStore{
		GetChunksByDocumentFunc: func(ctx context.Context, documentID string) ([]*models.DocumentChunk, error) {
			if documentID == "doc-1" {
				return []*models.DocumentChunk{
					{ID: "chunk-1", DocumentID: documentID},
					{ID: "chunk-2", DocumentID: documentID},
				}, nil
			}
			return nil, nil
		},
	}

	chunks, err := m.GetChunksByDocument(context.Background(), "doc-1")
	if err != nil {
		t.Fatalf("GetChunksByDocument() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Errorf("GetChunksByDocument() returned %d chunks, want 2", len(chunks))
	}

	chunks, err = m.GetChunksByDocument(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetChunksByDocument() error = %v", err)
	}
	if chunks != nil {
		t.Errorf("GetChunksByDocument() should return nil for nonexistent doc")
	}
}

func TestMockDocumentStore_UpdateChunkEmbeddings(t *testing.T) {
	updated := make(map[string]bool)
	m := &MockDocumentStore{
		UpdateChunkEmbeddingsFunc: func(ctx context.Context, embeddings map[string][]float32) error {
			for id := range embeddings {
				updated[id] = true
			}
			return nil
		},
	}

	embeddings := map[string][]float32{
		"chunk-1": {0.1, 0.2, 0.3},
		"chunk-2": {0.4, 0.5, 0.6},
	}

	err := m.UpdateChunkEmbeddings(context.Background(), embeddings)
	if err != nil {
		t.Fatalf("UpdateChunkEmbeddings() error = %v", err)
	}
	if !updated["chunk-1"] || !updated["chunk-2"] {
		t.Error("Both chunks should be updated")
	}
}

func TestMockDocumentStore_Stats(t *testing.T) {
	m := &MockDocumentStore{
		StatsFunc: func(ctx context.Context) (*StoreStats, error) {
			return &StoreStats{
				TotalDocuments:     10,
				TotalChunks:        50,
				EmbeddingDimension: 1536,
			}, nil
		},
	}

	stats, err := m.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.TotalDocuments != 10 {
		t.Errorf("TotalDocuments = %d, want 10", stats.TotalDocuments)
	}
	if stats.TotalChunks != 50 {
		t.Errorf("TotalChunks = %d, want 50", stats.TotalChunks)
	}
}

func TestMockDocumentStore_Close(t *testing.T) {
	closed := false
	m := &MockDocumentStore{
		CloseFunc: func() error {
			closed = true
			return nil
		},
	}

	err := m.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closed {
		t.Error("Close should be called")
	}
}

func TestMockDocumentStore_CloseWithError(t *testing.T) {
	m := &MockDocumentStore{
		CloseFunc: func() error {
			return errors.New("close error")
		},
	}

	err := m.Close()
	if err == nil {
		t.Error("Close() should return error")
	}
}

// Integration-style tests with mock

func TestDocumentStoreWorkflow(t *testing.T) {
	// Simulates a typical workflow: add document, search, get chunk, delete
	documents := make(map[string]*models.Document)
	chunks := make(map[string][]*models.DocumentChunk)

	store := &MockDocumentStore{
		AddDocumentFunc: func(ctx context.Context, doc *models.Document, docChunks []*models.DocumentChunk) error {
			documents[doc.ID] = doc
			chunks[doc.ID] = docChunks
			return nil
		},
		GetDocumentFunc: func(ctx context.Context, id string) (*models.Document, error) {
			if doc, ok := documents[id]; ok {
				return doc, nil
			}
			return nil, errors.New("not found")
		},
		GetChunksByDocumentFunc: func(ctx context.Context, documentID string) ([]*models.DocumentChunk, error) {
			return chunks[documentID], nil
		},
		SearchFunc: func(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
			var results []*models.DocumentSearchResult
			for _, docChunks := range chunks {
				for _, chunk := range docChunks {
					results = append(results, &models.DocumentSearchResult{
						Chunk: chunk,
						Score: 0.9,
					})
				}
			}
			return &models.DocumentSearchResponse{Results: results, TotalCount: len(results)}, nil
		},
		DeleteDocumentFunc: func(ctx context.Context, id string) error {
			delete(documents, id)
			delete(chunks, id)
			return nil
		},
	}

	ctx := context.Background()

	// Add a document
	doc := &models.Document{ID: "doc-1", Name: "Test Document"}
	docChunks := []*models.DocumentChunk{
		{ID: "chunk-1", DocumentID: "doc-1", Content: "Chunk 1 content"},
		{ID: "chunk-2", DocumentID: "doc-1", Content: "Chunk 2 content"},
	}

	err := store.AddDocument(ctx, doc, docChunks)
	if err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	// Verify document was added
	gotDoc, err := store.GetDocument(ctx, "doc-1")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if gotDoc.Name != "Test Document" {
		t.Errorf("GetDocument() name = %q, want 'Test Document'", gotDoc.Name)
	}

	// Verify chunks
	gotChunks, err := store.GetChunksByDocument(ctx, "doc-1")
	if err != nil {
		t.Fatalf("GetChunksByDocument() error = %v", err)
	}
	if len(gotChunks) != 2 {
		t.Errorf("GetChunksByDocument() returned %d chunks, want 2", len(gotChunks))
	}

	// Search
	searchResp, err := store.Search(ctx, &models.DocumentSearchRequest{Query: "test"}, []float32{0.1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if searchResp.TotalCount != 2 {
		t.Errorf("Search() TotalCount = %d, want 2", searchResp.TotalCount)
	}

	// Delete
	err = store.DeleteDocument(ctx, "doc-1")
	if err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}

	// Verify deletion
	_, err = store.GetDocument(ctx, "doc-1")
	if err == nil {
		t.Error("GetDocument() should return error after deletion")
	}
}

// Benchmark tests
func BenchmarkMockDocumentStore_AddDocument(b *testing.B) {
	store := &MockDocumentStore{
		AddDocumentFunc: func(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
			return nil
		},
	}

	doc := &models.Document{ID: "doc-1", Name: "Test"}
	chunks := []*models.DocumentChunk{{ID: "chunk-1", Content: "content"}}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.AddDocument(ctx, doc, chunks)
	}
}

func BenchmarkMockDocumentStore_Search(b *testing.B) {
	store := &MockDocumentStore{
		SearchFunc: func(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
			return &models.DocumentSearchResponse{
				Results: []*models.DocumentSearchResult{
					{Chunk: &models.DocumentChunk{ID: "chunk-1"}, Score: 0.9},
				},
				TotalCount: 1,
			}, nil
		},
	}

	req := &models.DocumentSearchRequest{Query: "test", Limit: 10}
	embedding := make([]float32, 1536)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Search(ctx, req, embedding)
	}
}
