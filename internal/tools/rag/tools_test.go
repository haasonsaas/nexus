package rag

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/memory/embeddings"
	"github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/internal/rag/store"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubEmbedder struct {
	dim int
}

func (e stubEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return make([]float32, e.dim), nil
}

func (e stubEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, e.dim)
	}
	return out, nil
}

func (e stubEmbedder) Name() string { return "stub" }

func (e stubEmbedder) Dimension() int { return e.dim }

func (e stubEmbedder) MaxBatchSize() int { return 8 }

var _ embeddings.Provider = stubEmbedder{}

type errorEmbedder struct {
	err error
}

func (e errorEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, e.err
}

func (e errorEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, e.err
}

func (e errorEmbedder) Name() string { return "error" }

func (e errorEmbedder) Dimension() int { return 0 }

func (e errorEmbedder) MaxBatchSize() int { return 1 }

var _ embeddings.Provider = errorEmbedder{}

type recordingStore struct {
	lastSearch *models.DocumentSearchRequest
	lastDoc    *models.Document
	addErr     error
	searchErr  error
}

func (s *recordingStore) AddDocument(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
	s.lastDoc = doc
	return s.addErr
}

func (s *recordingStore) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	return nil, nil
}

func (s *recordingStore) ListDocuments(ctx context.Context, opts *store.ListOptions) ([]*models.Document, error) {
	return nil, nil
}

func (s *recordingStore) DeleteDocument(ctx context.Context, id string) error {
	return nil
}

func (s *recordingStore) GetChunk(ctx context.Context, id string) (*models.DocumentChunk, error) {
	return nil, nil
}

func (s *recordingStore) GetChunksByDocument(ctx context.Context, documentID string) ([]*models.DocumentChunk, error) {
	return nil, nil
}

func (s *recordingStore) Search(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
	copyReq := *req
	s.lastSearch = &copyReq
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return &models.DocumentSearchResponse{}, nil
}

func (s *recordingStore) UpdateChunkEmbeddings(ctx context.Context, embeddings map[string][]float32) error {
	return nil
}

func (s *recordingStore) Stats(ctx context.Context) (*store.StoreStats, error) {
	return &store.StoreStats{}, nil
}

func (s *recordingStore) Close() error {
	return nil
}

var _ store.DocumentStore = (*recordingStore)(nil)

func TestSearchToolScopeIDFromSession(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewSearchTool(manager, nil)

	session := &models.Session{
		ID:        "session-1",
		AgentID:   "agent-1",
		Channel:   models.ChannelAPI,
		ChannelID: "channel-1",
	}
	ctx := agent.WithSession(context.Background(), session)

	params := json.RawMessage(`{"query":"hello","scope":"agent"}`)
	_, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if store.lastSearch == nil {
		t.Fatal("expected search request")
	}
	if store.lastSearch.Scope != models.DocumentScopeAgent {
		t.Fatalf("expected scope agent, got %v", store.lastSearch.Scope)
	}
	if store.lastSearch.ScopeID != session.AgentID {
		t.Fatalf("expected scopeID %q, got %q", session.AgentID, store.lastSearch.ScopeID)
	}
}

func TestSearchToolNoResults(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewSearchTool(manager, nil)

	params := json.RawMessage(`{"query":"nothing"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected no error result")
	}
	if result.Content == "" || result.Content == "[]" {
		t.Fatalf("expected friendly no-results message")
	}
}

func TestSearchToolEmbedderError(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, errorEmbedder{err: context.DeadlineExceeded}, nil)
	tool := NewSearchTool(manager, nil)

	params := json.RawMessage(`{"query":"timeout"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got: %s", result.Content)
	}
}

func TestSearchToolClampsLimit(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewSearchTool(manager, &SearchToolConfig{DefaultLimit: 2, MaxLimit: 3})

	params := json.RawMessage(`{"query":"limit","limit":99}`)
	_, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if store.lastSearch == nil {
		t.Fatal("expected search request")
	}
	if store.lastSearch.Limit != 3 {
		t.Fatalf("expected limit 3, got %d", store.lastSearch.Limit)
	}
}

func TestSearchToolStoreError(t *testing.T) {
	store := &recordingStore{searchErr: context.DeadlineExceeded}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewSearchTool(manager, nil)

	params := json.RawMessage(`{"query":"timeout"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got: %s", result.Content)
	}
}

func TestUploadToolAddsScopeMetadata(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewUploadTool(manager, nil)

	session := &models.Session{
		ID:        "session-1",
		AgentID:   "agent-1",
		Channel:   models.ChannelAPI,
		ChannelID: "channel-1",
	}
	ctx := agent.WithSession(context.Background(), session)

	params := json.RawMessage(`{"name":"Doc","content":"hello world","content_type":"text/plain"}`)
	_, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if store.lastDoc == nil {
		t.Fatal("expected document to be stored")
	}
	if store.lastDoc.Metadata.AgentID != session.AgentID {
		t.Fatalf("expected AgentID %q, got %q", session.AgentID, store.lastDoc.Metadata.AgentID)
	}
	if store.lastDoc.Metadata.SessionID != session.ID {
		t.Fatalf("expected SessionID %q, got %q", session.ID, store.lastDoc.Metadata.SessionID)
	}
	if store.lastDoc.Metadata.ChannelID != session.ChannelID {
		t.Fatalf("expected ChannelID %q, got %q", session.ChannelID, store.lastDoc.Metadata.ChannelID)
	}
}

func TestUploadToolRejectsLargeContent(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewUploadTool(manager, &UploadToolConfig{MaxContentLength: 5})

	params := json.RawMessage(`{"name":"Doc","content":"too long","content_type":"text/plain"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
	if store.lastDoc != nil {
		t.Fatalf("expected no document stored")
	}
}

func TestUploadToolRejectsContentType(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewUploadTool(manager, &UploadToolConfig{AllowedContentTypes: []string{"text/plain"}})

	params := json.RawMessage(`{"name":"Doc","content":"hello","content_type":"text/markdown"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
	if store.lastDoc != nil {
		t.Fatalf("expected no document stored")
	}
}

func TestUploadToolStoreError(t *testing.T) {
	store := &recordingStore{addErr: context.DeadlineExceeded}
	manager := index.NewManager(store, stubEmbedder{dim: 3}, nil)
	tool := NewUploadTool(manager, nil)

	params := json.RawMessage(`{"name":"Doc","content":"hello","content_type":"text/plain"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
}

func TestUploadToolEmbedderError(t *testing.T) {
	store := &recordingStore{}
	manager := index.NewManager(store, errorEmbedder{err: context.Canceled}, nil)
	tool := NewUploadTool(manager, nil)

	payload, err := json.Marshal(map[string]any{
		"name":         "Doc",
		"content":      strings.Repeat("a", 120),
		"content_type": "text/plain",
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	params := json.RawMessage(payload)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got: %s", result.Content)
	}
	if store.lastDoc != nil {
		t.Fatalf("expected no document stored on embedder error")
	}
}
