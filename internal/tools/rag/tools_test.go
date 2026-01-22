package rag

import (
	"context"
	"encoding/json"
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

type recordingStore struct {
	lastSearch *models.DocumentSearchRequest
	lastDoc    *models.Document
}

func (s *recordingStore) AddDocument(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
	s.lastDoc = doc
	return nil
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
