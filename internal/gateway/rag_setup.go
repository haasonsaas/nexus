package gateway

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/memory/embeddings"
	"github.com/haasonsaas/nexus/internal/memory/embeddings/ollama"
	"github.com/haasonsaas/nexus/internal/memory/embeddings/openai"
	ragcontext "github.com/haasonsaas/nexus/internal/rag/context"
	ragindex "github.com/haasonsaas/nexus/internal/rag/index"
	ragpgvector "github.com/haasonsaas/nexus/internal/rag/store/pgvector"
)

func initRAG(cfg *config.Config, logger *slog.Logger) (*ragindex.Manager, io.Closer, *ragcontext.Injector, error) {
	if cfg == nil || !cfg.RAG.Enabled {
		return nil, nil, nil, nil
	}

	storeCfg := cfg.RAG.Store
	backend := strings.ToLower(strings.TrimSpace(storeCfg.Backend))
	if backend == "" {
		backend = "pgvector"
	}
	if backend != "pgvector" {
		return nil, nil, nil, fmt.Errorf("unsupported RAG backend %q", backend)
	}

	var embProvider embeddings.Provider
	var err error
	switch strings.ToLower(strings.TrimSpace(cfg.RAG.Embeddings.Provider)) {
	case "openai", "":
		embProvider, err = openai.New(openai.Config{
			APIKey:  cfg.RAG.Embeddings.APIKey,
			BaseURL: cfg.RAG.Embeddings.BaseURL,
			Model:   cfg.RAG.Embeddings.Model,
		})
	case "ollama":
		embProvider, err = ollama.New(ollama.Config{
			BaseURL: cfg.RAG.Embeddings.BaseURL,
			Model:   cfg.RAG.Embeddings.Model,
		})
	default:
		return nil, nil, nil, fmt.Errorf("unknown RAG embedding provider %q", cfg.RAG.Embeddings.Provider)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init embedder: %w", err)
	}

	dimension := storeCfg.Dimension
	if dimension == 0 {
		dimension = embProvider.Dimension()
	}
	if embProvider.Dimension() != dimension {
		return nil, nil, nil, fmt.Errorf("embedding dimension mismatch: store=%d embedder=%d", dimension, embProvider.Dimension())
	}

	dsn := strings.TrimSpace(storeCfg.DSN)
	if dsn == "" && storeCfg.UseDatabaseURL {
		dsn = strings.TrimSpace(cfg.Database.URL)
	}
	if dsn == "" {
		return nil, nil, nil, fmt.Errorf("rag.store.dsn is required or set rag.store.use_database_url with database.url")
	}

	runMigrations := true
	if storeCfg.RunMigrations != nil {
		runMigrations = *storeCfg.RunMigrations
	}
	store, err := ragpgvector.New(ragpgvector.Config{
		DSN:           dsn,
		Dimension:     dimension,
		RunMigrations: runMigrations,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init rag store: %w", err)
	}

	indexCfg := &ragindex.Config{
		ChunkSize:          cfg.RAG.Chunking.ChunkSize,
		ChunkOverlap:       cfg.RAG.Chunking.ChunkOverlap,
		EmbeddingBatchSize: cfg.RAG.Embeddings.BatchSize,
		DefaultSource:      "gateway",
	}
	manager := ragindex.NewManager(store, embProvider, indexCfg)

	injectorCfg := ragcontext.DefaultInjectorConfig()
	injectorCfg.Enabled = cfg.RAG.ContextInjection.Enabled
	if cfg.RAG.ContextInjection.MaxChunks > 0 {
		injectorCfg.MaxChunks = cfg.RAG.ContextInjection.MaxChunks
	}
	if cfg.RAG.ContextInjection.MaxTokens > 0 {
		injectorCfg.MaxTokens = cfg.RAG.ContextInjection.MaxTokens
	}
	if cfg.RAG.ContextInjection.MinScore > 0 {
		injectorCfg.MinScore = cfg.RAG.ContextInjection.MinScore
	}
	if strings.TrimSpace(cfg.RAG.ContextInjection.Scope) != "" {
		injectorCfg.Scope = strings.TrimSpace(cfg.RAG.ContextInjection.Scope)
	}

	injector := ragcontext.NewInjector(manager, injectorCfg)

	if logger != nil {
		logger.Info("rag initialized", "backend", backend, "dimension", dimension)
	}

	return manager, store, injector, nil
}
