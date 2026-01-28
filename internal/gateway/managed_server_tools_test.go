package gateway

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/config"
	ragindex "github.com/haasonsaas/nexus/internal/rag/index"
	ragstore "github.com/haasonsaas/nexus/internal/rag/store"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubDocStore struct{}

type stubEmbedder struct{}

func (s *stubDocStore) AddDocument(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
	return nil
}

func (s *stubDocStore) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	return nil, nil
}

func (s *stubDocStore) ListDocuments(ctx context.Context, opts *ragstore.ListOptions) ([]*models.Document, error) {
	return nil, nil
}

func (s *stubDocStore) DeleteDocument(ctx context.Context, id string) error {
	return nil
}

func (s *stubDocStore) GetChunk(ctx context.Context, id string) (*models.DocumentChunk, error) {
	return nil, nil
}

func (s *stubDocStore) GetChunksByDocument(ctx context.Context, documentID string) ([]*models.DocumentChunk, error) {
	return nil, nil
}

func (s *stubDocStore) Search(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
	return &models.DocumentSearchResponse{}, nil
}

func (s *stubDocStore) UpdateChunkEmbeddings(ctx context.Context, embeddings map[string][]float32) error {
	return nil
}

func (s *stubDocStore) Stats(ctx context.Context) (*ragstore.StoreStats, error) {
	return &ragstore.StoreStats{}, nil
}

func (s *stubDocStore) Close() error {
	return nil
}

func (s *stubEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0}, nil
}

func (s *stubEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{0}
	}
	return vectors, nil
}

func (s *stubEmbedder) Name() string {
	return "stub"
}

func (s *stubEmbedder) Dimension() int {
	return 1
}

func (s *stubEmbedder) MaxBatchSize() int {
	return 16
}

func TestManagedServerRegistersRAGAttentionAndSkillTools(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	skillDir := filepath.Join(workspace, "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	skillContent := "" +
		"---\n" +
		"name: demo-skill\n" +
		"description: Demo skill\n" +
		"metadata:\n" +
		"  always: true\n" +
		"  tools:\n" +
		"    - name: demo_tool\n" +
		"      description: Demo tool\n" +
		"      command: echo\n" +
		"---\n"

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{Path: workspace},
		Attention: config.AttentionConfig{Enabled: true},
		RAG:       config.RAGConfig{Enabled: false},
	}

	server, err := NewManagedServer(ManagedServerConfig{Config: cfg, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("NewManagedServer() error = %v", err)
	}

	if server.skillsManager == nil {
		t.Fatalf("expected skills manager to be initialized")
	}
	if err := server.skillsManager.Discover(ctx); err != nil {
		t.Fatalf("skillsManager.Discover() error = %v", err)
	}

	ragManager := ragindex.NewManager(&stubDocStore{}, &stubEmbedder{}, nil)
	server.ragIndex = ragManager
	if server.toolManager != nil {
		server.toolManager.ragManager = ragManager
	}
	cfg.RAG.Enabled = true

	store := sessions.NewMemoryStore()
	runtime := agent.NewRuntime(providers.NewOllamaProvider(providers.OllamaConfig{DefaultModel: "llama3"}), store)
	server.runtime = runtime
	server.sessions = store

	if err := server.RegisterToolsWithRuntime(ctx); err != nil {
		t.Fatalf("RegisterToolsWithRuntime() error = %v", err)
	}

	registered := map[string]struct{}{}
	for _, name := range server.toolManager.RegisteredTools() {
		registered[name] = struct{}{}
	}
	for _, name := range []string{"document_search", "document_upload", "attention_list", "demo_tool"} {
		if _, ok := registered[name]; !ok {
			t.Fatalf("expected tool %q to be registered", name)
		}
	}
}
