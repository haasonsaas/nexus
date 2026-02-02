// Package memory provides vector-based semantic memory search.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/memory/backend"
	"github.com/haasonsaas/nexus/internal/memory/backend/lancedb"
	"github.com/haasonsaas/nexus/internal/memory/backend/pgvector"
	"github.com/haasonsaas/nexus/internal/memory/backend/sqlitevec"
	"github.com/haasonsaas/nexus/internal/memory/embeddings"
	"github.com/haasonsaas/nexus/internal/memory/embeddings/ollama"
	"github.com/haasonsaas/nexus/internal/memory/embeddings/openai"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Manager coordinates memory storage and retrieval.
type Manager struct {
	backend  backend.Backend
	embedder embeddings.Provider
	config   *Config
	cache    *embeddingCache
}

// Config contains configuration for the memory manager.
type Config struct {
	Enabled   bool   `yaml:"enabled"`
	Backend   string `yaml:"backend"`   // sqlite-vec, lancedb, pgvector
	Dimension int    `yaml:"dimension"` // Must match embedding model

	// Backend-specific config
	SQLiteVec SQLiteVecConfig `yaml:"sqlite_vec"`
	Pgvector  PgvectorConfig  `yaml:"pgvector"`
	LanceDB   LanceDBConfig   `yaml:"lancedb"`

	// Embedding provider config
	Embeddings EmbeddingsConfig `yaml:"embeddings"`

	// Indexing behavior
	Indexing IndexingConfig `yaml:"indexing"`

	// Search defaults
	Search SearchConfig `yaml:"search"`

	// Auto-capture configuration
	AutoCapture AutoCaptureConfig `yaml:"auto_capture"`

	// Auto-recall configuration
	AutoRecall AutoRecallConfig `yaml:"auto_recall"`

	// Consolidation configuration
	Consolidation ConsolidationConfig `yaml:"consolidation"`
}

// SQLiteVecConfig contains sqlite-vec specific configuration.
type SQLiteVecConfig struct {
	Path string `yaml:"path"` // Path to database file
}

// PgvectorConfig contains pgvector specific configuration.
type PgvectorConfig struct {
	// DSN is the PostgreSQL connection string.
	// If empty and UseCockroachDB is true, gateway will fall back to database.url.
	DSN string `yaml:"dsn"`

	// UseCockroachDB indicates whether to reuse the configured database URL.
	// When true and DSN is empty, the gateway copies database.url into DSN.
	UseCockroachDB bool `yaml:"use_cockroachdb"`

	// DB is an existing database connection to reuse (set programmatically, not via config).
	DB *sql.DB `yaml:"-"`

	// RunMigrations controls whether to run migrations on startup.
	// Default is true.
	RunMigrations *bool `yaml:"run_migrations"`
}

// LanceDBConfig contains LanceDB specific configuration.
type LanceDBConfig struct {
	// Path is the directory path for LanceDB storage.
	Path string `yaml:"path"`

	// IndexType specifies the vector index type to use.
	// Options: auto, ivf_pq, ivf_flat, hnsw_pq, hnsw_sq
	IndexType string `yaml:"index_type"`

	// MetricType specifies the distance metric.
	// Options: cosine, l2, dot
	MetricType string `yaml:"metric_type"`

	// NProbes is the number of probes for IVF indexes.
	NProbes int `yaml:"n_probes"`

	// EF is the HNSW ef parameter for search.
	EF int `yaml:"ef"`

	// RefineFactor improves accuracy by re-ranking results.
	RefineFactor int `yaml:"refine_factor"`
}

// EmbeddingsConfig contains embedding provider configuration.
type EmbeddingsConfig struct {
	Provider string `yaml:"provider"` // openai, gemini, ollama
	APIKey   string `yaml:"api_key"`
	BaseURL  string `yaml:"base_url"`
	Model    string `yaml:"model"`

	// Ollama-specific
	OllamaURL string `yaml:"ollama_url"`

	// Gemini-specific
	ProjectID string `yaml:"project_id"`
	Location  string `yaml:"location"`
}

// IndexingConfig contains configuration for automatic indexing.
type IndexingConfig struct {
	AutoIndexMessages bool     `yaml:"auto_index_messages"`
	MinContentLength  int      `yaml:"min_content_length"`
	MaxContentLength  int      `yaml:"max_content_length"`
	BatchSize         int      `yaml:"batch_size"`
	AllowedRoles      []string `yaml:"allowed_roles"`
}

// SearchConfig contains default search parameters.
type SearchConfig struct {
	DefaultLimit     int             `yaml:"default_limit"`
	DefaultThreshold float32         `yaml:"default_threshold"`
	DefaultScope     string          `yaml:"default_scope"`
	Hierarchy        HierarchyConfig `yaml:"hierarchy"`
}

// HierarchyConfig configures hierarchical memory search across scopes.
type HierarchyConfig struct {
	Enabled    bool               `yaml:"enabled"`
	Scopes     []string           `yaml:"scopes"`
	Weights    map[string]float32 `yaml:"weights"`
	MaxResults int                `yaml:"max_results"`
}

// ConsolidationConfig controls background memory consolidation.
type ConsolidationConfig struct {
	Enabled          bool          `yaml:"enabled"`
	Interval         time.Duration `yaml:"interval"`
	MinMessages      int           `yaml:"min_messages"`
	MaxMessages      int           `yaml:"max_messages"`
	MaxSessions      int           `yaml:"max_sessions"`
	SummaryMaxChars  int           `yaml:"summary_max_chars"`
	SummaryMaxTokens int           `yaml:"summary_max_tokens"`
	Model            string        `yaml:"model"`
}

// NewManager creates a new memory manager with the given configuration.
func NewManager(cfg *Config) (*Manager, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// Set defaults
	if cfg.Dimension == 0 {
		cfg.Dimension = 1536
	}
	if cfg.Indexing.BatchSize == 0 {
		cfg.Indexing.BatchSize = 100
	}
	if cfg.Indexing.MinContentLength == 0 {
		cfg.Indexing.MinContentLength = 10
	}
	if cfg.Search.DefaultLimit == 0 {
		cfg.Search.DefaultLimit = 10
	}
	if cfg.Search.DefaultThreshold == 0 {
		cfg.Search.DefaultThreshold = 0.7
	}
	if cfg.Search.DefaultScope == "" {
		cfg.Search.DefaultScope = "session"
	}
	if cfg.Search.Hierarchy.Enabled {
		if len(cfg.Search.Hierarchy.Scopes) == 0 {
			cfg.Search.Hierarchy.Scopes = []string{"session", "agent", "channel", "global"}
		}
		if cfg.Search.Hierarchy.MaxResults == 0 {
			cfg.Search.Hierarchy.MaxResults = cfg.Search.DefaultLimit
		}
		if cfg.Search.Hierarchy.Weights == nil {
			cfg.Search.Hierarchy.Weights = map[string]float32{
				"session": 1.0,
				"agent":   0.8,
				"channel": 0.7,
				"global":  0.5,
			}
		}
	}
	if cfg.Consolidation.Enabled {
		if cfg.Consolidation.Interval == 0 {
			cfg.Consolidation.Interval = 6 * time.Hour
		}
		if cfg.Consolidation.MinMessages == 0 {
			cfg.Consolidation.MinMessages = 20
		}
		if cfg.Consolidation.MaxMessages == 0 {
			cfg.Consolidation.MaxMessages = 120
		}
		if cfg.Consolidation.MaxSessions == 0 {
			cfg.Consolidation.MaxSessions = 50
		}
		if cfg.Consolidation.SummaryMaxChars == 0 {
			cfg.Consolidation.SummaryMaxChars = 2000
		}
		if cfg.Consolidation.SummaryMaxTokens == 0 {
			cfg.Consolidation.SummaryMaxTokens = 512
		}
	}

	// Initialize backend
	var b backend.Backend
	var err error
	switch cfg.Backend {
	case "sqlite-vec", "sqlite", "":
		b, err = sqlitevec.New(sqlitevec.Config{
			Path:      cfg.SQLiteVec.Path,
			Dimension: cfg.Dimension,
		})
	case "pgvector", "postgres", "postgresql":
		runMigrations := true
		if cfg.Pgvector.RunMigrations != nil {
			runMigrations = *cfg.Pgvector.RunMigrations
		}
		b, err = pgvector.New(pgvector.Config{
			DSN:           cfg.Pgvector.DSN,
			DB:            cfg.Pgvector.DB,
			Dimension:     cfg.Dimension,
			RunMigrations: runMigrations,
		})
	case "lancedb", "lance":
		b, err = lancedb.New(lancedb.Config{
			Path:       cfg.LanceDB.Path,
			Dimension:  cfg.Dimension,
			IndexType:  lancedb.IndexType(cfg.LanceDB.IndexType),
			MetricType: cfg.LanceDB.MetricType,
		})
	default:
		return nil, fmt.Errorf("unknown backend: %s", cfg.Backend)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize backend: %w", err)
	}

	// Initialize embedder
	var emb embeddings.Provider
	switch cfg.Embeddings.Provider {
	case "openai", "":
		emb, err = openai.New(openai.Config{
			APIKey:  cfg.Embeddings.APIKey,
			BaseURL: cfg.Embeddings.BaseURL,
			Model:   cfg.Embeddings.Model,
		})
	case "ollama":
		emb, err = ollama.New(ollama.Config{
			BaseURL: cfg.Embeddings.OllamaURL,
			Model:   cfg.Embeddings.Model,
		})
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", cfg.Embeddings.Provider)
	}
	if err != nil {
		b.Close()
		return nil, fmt.Errorf("failed to initialize embedder: %w", err)
	}

	// Verify dimension matches
	if emb.Dimension() != cfg.Dimension {
		b.Close()
		return nil, fmt.Errorf("dimension mismatch: config=%d, embedder=%d", cfg.Dimension, emb.Dimension())
	}

	return &Manager{
		backend:  b,
		embedder: emb,
		config:   cfg,
		cache:    newEmbeddingCache(1000), // Cache up to 1000 query embeddings
	}, nil
}

// Index stores memory entries, generating embeddings as needed.
func (m *Manager) Index(ctx context.Context, entries []*models.MemoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Filter entries that need embeddings
	var needsEmbedding []*models.MemoryEntry
	for _, entry := range entries {
		if len(entry.Embedding) == 0 && len(entry.Content) >= m.config.Indexing.MinContentLength {
			needsEmbedding = append(needsEmbedding, entry)
		}
	}

	// Batch embed
	batchSize := m.embedder.MaxBatchSize()
	if m.config.Indexing.BatchSize > 0 && m.config.Indexing.BatchSize < batchSize {
		batchSize = m.config.Indexing.BatchSize
	}

	for i := 0; i < len(needsEmbedding); i += batchSize {
		end := i + batchSize
		if end > len(needsEmbedding) {
			end = len(needsEmbedding)
		}
		batch := needsEmbedding[i:end]

		texts := make([]string, len(batch))
		for j, entry := range batch {
			texts[j] = entry.Content
		}

		embeddings, err := m.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			return fmt.Errorf("failed to generate embeddings: %w", err)
		}

		for j, entry := range batch {
			entry.Embedding = embeddings[j]
		}
	}

	// Store in backend
	return m.backend.Index(ctx, entries)
}

// Search finds relevant memories using semantic similarity.
func (m *Manager) Search(ctx context.Context, req *models.SearchRequest) (*models.SearchResponse, error) {
	start := time.Now()

	// Apply defaults
	if req.Limit == 0 {
		req.Limit = m.config.Search.DefaultLimit
	}
	if req.Threshold == 0 {
		req.Threshold = m.config.Search.DefaultThreshold
	}
	if req.Scope == "" {
		req.Scope = models.MemoryScope(m.config.Search.DefaultScope)
	}

	// Get query embedding (with caching)
	cacheKey := fmt.Sprintf("%s:%s", req.Scope, req.Query)
	queryEmbed, ok := m.cache.get(cacheKey)
	if !ok {
		embed, err := m.embedder.Embed(ctx, req.Query)
		if err != nil {
			return nil, fmt.Errorf("failed to embed query: %w", err)
		}
		queryEmbed = embed
		m.cache.set(cacheKey, embed)
	}

	// Search backend
	results, err := m.backend.Search(ctx, queryEmbed, &backend.SearchOptions{
		Scope:     req.Scope,
		ScopeID:   req.ScopeID,
		Limit:     req.Limit,
		Threshold: req.Threshold,
		Filters:   req.Filters,
	})
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	return &models.SearchResponse{
		Results:    results,
		TotalCount: len(results),
		QueryTime:  time.Since(start),
	}, nil
}

// Delete removes memory entries by ID.
func (m *Manager) Delete(ctx context.Context, ids []string) error {
	return m.backend.Delete(ctx, ids)
}

// Count returns the number of memories in the given scope.
func (m *Manager) Count(ctx context.Context, scope models.MemoryScope, scopeID string) (int64, error) {
	return m.backend.Count(ctx, scope, scopeID)
}

// Compact optimizes the storage backend.
func (m *Manager) Compact(ctx context.Context) error {
	return m.backend.Compact(ctx)
}

// Stats returns statistics about the memory store.
func (m *Manager) Stats(ctx context.Context) (*Stats, error) {
	globalCount, err := m.backend.Count(ctx, models.ScopeAll, "")
	if err != nil {
		return nil, err
	}

	return &Stats{
		TotalEntries:      globalCount,
		Backend:           m.config.Backend,
		EmbeddingProvider: m.embedder.Name(),
		EmbeddingModel:    m.config.Embeddings.Model,
		Dimension:         m.config.Dimension,
	}, nil
}

// Close releases all resources.
func (m *Manager) Close() error {
	return m.backend.Close()
}

// Stats contains memory store statistics.
type Stats struct {
	TotalEntries      int64  `json:"total_entries"`
	Backend           string `json:"backend"`
	EmbeddingProvider string `json:"embedding_provider"`
	EmbeddingModel    string `json:"embedding_model"`
	Dimension         int    `json:"dimension"`
}

// lruNode represents a node in the doubly-linked list for LRU tracking.
type lruNode struct {
	key   string
	value []float32
	prev  *lruNode
	next  *lruNode
}

// embeddingCache is a true LRU cache for query embeddings using a doubly-linked list.
// Accessed items are promoted to the front, ensuring least-recently-used eviction.
type embeddingCache struct {
	mu       sync.RWMutex
	items    map[string]*lruNode
	head     *lruNode // Most recently used
	tail     *lruNode // Least recently used
	capacity int
}

func newEmbeddingCache(capacity int) *embeddingCache {
	return &embeddingCache{
		items:    make(map[string]*lruNode),
		capacity: capacity,
	}
}

// get retrieves a value and promotes it to the front (most recently used).
func (c *embeddingCache) get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.items[key]
	if !ok {
		return nil, false
	}

	// Promote to front (most recently used)
	c.moveToFront(node)
	return node.value, true
}

// set adds or updates a value, promoting it to the front.
func (c *embeddingCache) set(key string, value []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if node, exists := c.items[key]; exists {
		// Update existing node and move to front
		node.value = value
		c.moveToFront(node)
		return
	}

	// Create new node
	node := &lruNode{
		key:   key,
		value: value,
	}
	c.items[key] = node
	c.addToFront(node)

	// Evict least recently used if over capacity
	if len(c.items) > c.capacity {
		c.evictLRU()
	}
}

// moveToFront moves a node to the front of the list (most recently used).
func (c *embeddingCache) moveToFront(node *lruNode) {
	if node == c.head {
		return // Already at front
	}

	// Remove from current position
	c.removeNode(node)

	// Add to front
	c.addToFront(node)
}

// addToFront adds a node to the front of the list.
func (c *embeddingCache) addToFront(node *lruNode) {
	node.prev = nil
	node.next = c.head

	if c.head != nil {
		c.head.prev = node
	}
	c.head = node

	if c.tail == nil {
		c.tail = node
	}
}

// removeNode removes a node from the list without deleting from map.
func (c *embeddingCache) removeNode(node *lruNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
}

// evictLRU removes the least recently used item from the cache.
func (c *embeddingCache) evictLRU() {
	if c.tail == nil {
		return
	}

	// Remove from map
	delete(c.items, c.tail.key)

	// Remove from list
	if c.tail.prev != nil {
		c.tail.prev.next = nil
		c.tail = c.tail.prev
	} else {
		// Only one element
		c.head = nil
		c.tail = nil
	}
}
