# Nexus Vector Memory System Design

## Overview

This document specifies the design for a vector-based memory search system in Nexus, supporting multiple storage backends (sqlite-vec, LanceDB, pgvector) and multiple embedding providers (OpenAI, Gemini, Ollama).

## Goals

1. **Semantic search**: Find relevant context by meaning, not just keywords
2. **Backend flexibility**: Support lightweight (sqlite-vec) to enterprise (pgvector) backends
3. **Cost efficiency**: Batch embeddings, caching, local model support
4. **Session scoping**: Memory scoped to session, channel, or global

---

## 1. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Memory Manager                              │
├─────────────────────────────────────────────────────────────────┤
│  Index()  │  Search()  │  Delete()  │  Compact()  │  Stats()    │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
      ┌───────────┐   ┌───────────┐   ┌───────────┐
      │ sqlite-vec│   │  LanceDB  │   │  pgvector │
      └───────────┘   └───────────┘   └───────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
      ┌───────────┐   ┌───────────┐   ┌───────────┐
      │  OpenAI   │   │  Gemini   │   │  Ollama   │
      └───────────┘   └───────────┘   └───────────┘
```

---

## 2. Data Model

### 2.1 Memory Entry

```go
// pkg/models/memory.go

type MemoryEntry struct {
    ID        string    `json:"id"`
    SessionID string    `json:"session_id,omitempty"`  // Scoping
    ChannelID string    `json:"channel_id,omitempty"`
    AgentID   string    `json:"agent_id,omitempty"`

    Content   string    `json:"content"`               // Original text
    Metadata  Metadata  `json:"metadata"`

    Embedding []float32 `json:"-"`                     // Not serialized
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type Metadata struct {
    Source    string         `json:"source"`     // "message", "document", "note"
    Role      string         `json:"role"`       // "user", "assistant"
    Tags      []string       `json:"tags"`
    Extra     map[string]any `json:"extra"`
}

type MemoryScope string

const (
    ScopeSession MemoryScope = "session"
    ScopeChannel MemoryScope = "channel"
    ScopeAgent   MemoryScope = "agent"
    ScopeGlobal  MemoryScope = "global"
)
```

### 2.2 Search Request/Response

```go
type SearchRequest struct {
    Query     string            `json:"query"`
    Scope     MemoryScope       `json:"scope"`
    ScopeID   string            `json:"scope_id"`   // Session/channel/agent ID
    Limit     int               `json:"limit"`
    Threshold float32           `json:"threshold"`  // Min similarity (0-1)
    Filters   map[string]any    `json:"filters"`    // Metadata filters
}

type SearchResult struct {
    Entry      *MemoryEntry `json:"entry"`
    Score      float32      `json:"score"`       // Similarity score
    Highlights []string     `json:"highlights"`  // Matched snippets
}

type SearchResponse struct {
    Results    []*SearchResult `json:"results"`
    TotalCount int             `json:"total_count"`
    QueryTime  time.Duration   `json:"query_time"`
}
```

---

## 3. Storage Backends

### 3.1 Backend Interface

```go
// internal/memory/backend/backend.go

type Backend interface {
    // Core operations
    Index(ctx context.Context, entries []*MemoryEntry) error
    Search(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error)
    Delete(ctx context.Context, ids []string) error

    // Maintenance
    Count(ctx context.Context, scope MemoryScope, scopeID string) (int64, error)
    Compact(ctx context.Context) error

    // Lifecycle
    Close() error
}

type SearchOptions struct {
    Scope     MemoryScope
    ScopeID   string
    Limit     int
    Threshold float32
    Filters   map[string]any
}
```

### 3.2 sqlite-vec Backend

Single-file SQLite with vector extension. Best for local/small deployments.

```go
// internal/memory/backend/sqlitevec/backend.go

type SQLiteVecBackend struct {
    db        *sql.DB
    dimension int
}

func New(path string, dimension int) (*SQLiteVecBackend, error) {
    db, err := sql.Open("sqlite3", path)
    if err != nil {
        return nil, err
    }

    // Load vec0 extension
    if _, err := db.Exec("SELECT load_extension('vec0')"); err != nil {
        return nil, fmt.Errorf("failed to load vec0 extension: %w", err)
    }

    // Create table with vector column
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS memories (
            id TEXT PRIMARY KEY,
            session_id TEXT,
            channel_id TEXT,
            agent_id TEXT,
            content TEXT NOT NULL,
            metadata TEXT,
            embedding F32_BLOB(%d),
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `, dimension)
    if err != nil {
        return nil, err
    }

    // Create virtual table for vector search
    _, err = db.Exec(`
        CREATE VIRTUAL TABLE IF NOT EXISTS memories_vec USING vec0(
            id TEXT PRIMARY KEY,
            embedding FLOAT[%d]
        )
    `, dimension)

    return &SQLiteVecBackend{db: db, dimension: dimension}, err
}

func (b *SQLiteVecBackend) Search(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error) {
    // Build scope filter
    scopeFilter := ""
    args := []any{embedding}

    switch opts.Scope {
    case ScopeSession:
        scopeFilter = "AND m.session_id = ?"
        args = append(args, opts.ScopeID)
    case ScopeChannel:
        scopeFilter = "AND m.channel_id = ?"
        args = append(args, opts.ScopeID)
    case ScopeAgent:
        scopeFilter = "AND m.agent_id = ?"
        args = append(args, opts.ScopeID)
    }

    query := fmt.Sprintf(`
        SELECT m.*, vec_distance_cosine(v.embedding, ?) as distance
        FROM memories m
        JOIN memories_vec v ON m.id = v.id
        WHERE 1=1 %s
        ORDER BY distance ASC
        LIMIT ?
    `, scopeFilter)

    args = append(args, opts.Limit)

    rows, err := b.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var results []*SearchResult
    for rows.Next() {
        var entry MemoryEntry
        var distance float32
        // ... scan fields

        score := 1 - distance // Convert distance to similarity
        if score >= opts.Threshold {
            results = append(results, &SearchResult{
                Entry: &entry,
                Score: score,
            })
        }
    }

    return results, nil
}
```

### 3.3 LanceDB Backend

Columnar format optimized for large-scale vector operations.

```go
// internal/memory/backend/lancedb/backend.go

import "github.com/lancedb/lancedb-go"

type LanceDBBackend struct {
    db    lancedb.Database
    table lancedb.Table
}

func New(path string, dimension int) (*LanceDBBackend, error) {
    db, err := lancedb.Connect(path)
    if err != nil {
        return nil, err
    }

    schema := arrow.NewSchema([]arrow.Field{
        {Name: "id", Type: arrow.BinaryTypes.String},
        {Name: "session_id", Type: arrow.BinaryTypes.String, Nullable: true},
        {Name: "channel_id", Type: arrow.BinaryTypes.String, Nullable: true},
        {Name: "agent_id", Type: arrow.BinaryTypes.String, Nullable: true},
        {Name: "content", Type: arrow.BinaryTypes.String},
        {Name: "metadata", Type: arrow.BinaryTypes.String},
        {Name: "embedding", Type: arrow.FixedSizeListOf(int32(dimension), arrow.PrimitiveTypes.Float32)},
        {Name: "created_at", Type: arrow.FixedWidthTypes.Timestamp_us},
    }, nil)

    table, err := db.CreateTable("memories", schema)
    if err != nil && !errors.Is(err, lancedb.ErrTableExists) {
        return nil, err
    }

    if table == nil {
        table, err = db.OpenTable("memories")
        if err != nil {
            return nil, err
        }
    }

    return &LanceDBBackend{db: db, table: table}, nil
}

func (b *LanceDBBackend) Search(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error) {
    query := b.table.Search(embedding).
        Limit(opts.Limit).
        DistanceType(lancedb.Cosine)

    // Add filters
    switch opts.Scope {
    case ScopeSession:
        query = query.Where(fmt.Sprintf("session_id = '%s'", opts.ScopeID))
    case ScopeChannel:
        query = query.Where(fmt.Sprintf("channel_id = '%s'", opts.ScopeID))
    case ScopeAgent:
        query = query.Where(fmt.Sprintf("agent_id = '%s'", opts.ScopeID))
    }

    results, err := query.Execute(ctx)
    if err != nil {
        return nil, err
    }

    // Convert to SearchResult...
    return results, nil
}
```

### 3.4 pgvector Backend

PostgreSQL with pgvector extension. Best for existing CockroachDB deployments.

```go
// internal/memory/backend/pgvector/backend.go

type PgvectorBackend struct {
    db        *sql.DB
    dimension int
}

func New(connStr string, dimension int) (*PgvectorBackend, error) {
    db, err := sql.Open("postgres", connStr)
    if err != nil {
        return nil, err
    }

    // Enable pgvector extension
    _, err = db.Exec("CREATE EXTENSION IF NOT EXISTS vector")
    if err != nil {
        return nil, err
    }

    // Create table
    _, err = db.Exec(fmt.Sprintf(`
        CREATE TABLE IF NOT EXISTS memories (
            id TEXT PRIMARY KEY,
            session_id TEXT,
            channel_id TEXT,
            agent_id TEXT,
            content TEXT NOT NULL,
            metadata JSONB,
            embedding vector(%d),
            created_at TIMESTAMPTZ DEFAULT NOW(),
            updated_at TIMESTAMPTZ DEFAULT NOW()
        )
    `, dimension))
    if err != nil {
        return nil, err
    }

    // Create vector index (IVFFlat for large datasets)
    _, err = db.Exec(`
        CREATE INDEX IF NOT EXISTS memories_embedding_idx
        ON memories USING ivfflat (embedding vector_cosine_ops)
        WITH (lists = 100)
    `)

    return &PgvectorBackend{db: db, dimension: dimension}, err
}

func (b *PgvectorBackend) Search(ctx context.Context, embedding []float32, opts *SearchOptions) ([]*SearchResult, error) {
    // Convert embedding to pgvector format
    vecStr := fmt.Sprintf("[%s]", joinFloats(embedding, ","))

    scopeFilter := ""
    args := []any{vecStr, opts.Limit}

    switch opts.Scope {
    case ScopeSession:
        scopeFilter = "AND session_id = $3"
        args = append(args, opts.ScopeID)
    // ... other scopes
    }

    query := fmt.Sprintf(`
        SELECT *, 1 - (embedding <=> $1::vector) as score
        FROM memories
        WHERE 1=1 %s
        ORDER BY embedding <=> $1::vector
        LIMIT $2
    `, scopeFilter)

    rows, err := b.db.QueryContext(ctx, query, args...)
    // ... process results
    return results, nil
}
```

---

## 4. Embedding Providers

### 4.1 Provider Interface

```go
// internal/memory/embeddings/embeddings.go

type EmbeddingProvider interface {
    // Single embedding
    Embed(ctx context.Context, text string) ([]float32, error)

    // Batch embedding (more efficient)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

    // Provider info
    Name() string
    Dimension() int
    MaxBatchSize() int
}
```

### 4.2 OpenAI Provider

```go
// internal/memory/embeddings/openai/openai.go

import "github.com/sashabaranov/go-openai"

type OpenAIProvider struct {
    client *openai.Client
    model  string
}

func New(apiKey string, model string) *OpenAIProvider {
    return &OpenAIProvider{
        client: openai.NewClient(apiKey),
        model:  model, // text-embedding-3-small, text-embedding-3-large
    }
}

func (p *OpenAIProvider) Dimension() int {
    switch p.model {
    case "text-embedding-3-small":
        return 1536
    case "text-embedding-3-large":
        return 3072
    default:
        return 1536
    }
}

func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
    resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
        Input: texts,
        Model: openai.EmbeddingModel(p.model),
    })
    if err != nil {
        return nil, err
    }

    embeddings := make([][]float32, len(resp.Data))
    for i, data := range resp.Data {
        embeddings[i] = data.Embedding
    }
    return embeddings, nil
}

func (p *OpenAIProvider) MaxBatchSize() int {
    return 2048 // OpenAI supports up to 2048 inputs per request
}
```

### 4.3 Gemini Provider

```go
// internal/memory/embeddings/gemini/gemini.go

import "cloud.google.com/go/vertexai/genai"

type GeminiProvider struct {
    client *genai.Client
    model  string
}

func New(projectID, location, model string) (*GeminiProvider, error) {
    ctx := context.Background()
    client, err := genai.NewClient(ctx, projectID, location)
    if err != nil {
        return nil, err
    }
    return &GeminiProvider{
        client: client,
        model:  model, // text-embedding-004
    }, nil
}

func (p *GeminiProvider) Dimension() int {
    return 768 // text-embedding-004
}

func (p *GeminiProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
    model := p.client.EmbeddingModel(p.model)

    var embeddings [][]float32
    for _, text := range texts {
        resp, err := model.EmbedContent(ctx, genai.Text(text))
        if err != nil {
            return nil, err
        }
        embeddings = append(embeddings, resp.Embedding.Values)
    }
    return embeddings, nil
}

func (p *GeminiProvider) MaxBatchSize() int {
    return 100 // Gemini batch limit
}
```

### 4.4 Ollama Provider (Local)

```go
// internal/memory/embeddings/ollama/ollama.go

type OllamaProvider struct {
    baseURL string
    model   string
}

func New(baseURL, model string) *OllamaProvider {
    if baseURL == "" {
        baseURL = "http://localhost:11434"
    }
    return &OllamaProvider{
        baseURL: baseURL,
        model:   model, // nomic-embed-text, mxbai-embed-large
    }
}

func (p *OllamaProvider) Dimension() int {
    switch p.model {
    case "nomic-embed-text":
        return 768
    case "mxbai-embed-large":
        return 1024
    default:
        return 768
    }
}

func (p *OllamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
    var embeddings [][]float32

    for _, text := range texts {
        req := map[string]any{
            "model":  p.model,
            "prompt": text,
        }

        body, _ := json.Marshal(req)
        resp, err := http.Post(p.baseURL+"/api/embeddings", "application/json", bytes.NewReader(body))
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()

        var result struct {
            Embedding []float32 `json:"embedding"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
            return nil, err
        }

        embeddings = append(embeddings, result.Embedding)
    }

    return embeddings, nil
}

func (p *OllamaProvider) MaxBatchSize() int {
    return 1 // Ollama processes one at a time
}
```

---

## 5. Memory Manager

### 5.1 Implementation

```go
// internal/memory/manager.go

type Manager struct {
    backend   Backend
    embedder  EmbeddingProvider
    config    *config.MemoryConfig
    batchSize int
    cache     *lru.Cache[string, []float32] // Query embedding cache
    mu        sync.RWMutex
}

func NewManager(cfg *config.MemoryConfig) (*Manager, error) {
    // Initialize backend
    var backend Backend
    switch cfg.Backend {
    case "sqlite-vec":
        backend, _ = sqlitevec.New(cfg.SQLiteVec.Path, cfg.Dimension)
    case "lancedb":
        backend, _ = lancedb.New(cfg.LanceDB.Path, cfg.Dimension)
    case "pgvector":
        backend, _ = pgvector.New(cfg.Pgvector.ConnString, cfg.Dimension)
    default:
        return nil, fmt.Errorf("unknown backend: %s", cfg.Backend)
    }

    // Initialize embedder
    var embedder EmbeddingProvider
    switch cfg.Embeddings.Provider {
    case "openai":
        embedder = openai.New(cfg.Embeddings.APIKey, cfg.Embeddings.Model)
    case "gemini":
        embedder, _ = gemini.New(cfg.Embeddings.ProjectID, cfg.Embeddings.Location, cfg.Embeddings.Model)
    case "ollama":
        embedder = ollama.New(cfg.Embeddings.OllamaURL, cfg.Embeddings.Model)
    }

    cache, _ := lru.New[string, []float32](1000)

    return &Manager{
        backend:   backend,
        embedder:  embedder,
        config:    cfg,
        batchSize: embedder.MaxBatchSize(),
        cache:     cache,
    }, nil
}

func (m *Manager) Index(ctx context.Context, entries []*MemoryEntry) error {
    // Batch embedding for efficiency
    for i := 0; i < len(entries); i += m.batchSize {
        end := min(i+m.batchSize, len(entries))
        batch := entries[i:end]

        texts := make([]string, len(batch))
        for j, entry := range batch {
            texts[j] = entry.Content
        }

        embeddings, err := m.embedder.EmbedBatch(ctx, texts)
        if err != nil {
            return fmt.Errorf("embedding failed: %w", err)
        }

        for j, entry := range batch {
            entry.Embedding = embeddings[j]
        }

        if err := m.backend.Index(ctx, batch); err != nil {
            return fmt.Errorf("indexing failed: %w", err)
        }
    }

    return nil
}

func (m *Manager) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
    start := time.Now()

    // Get query embedding (with caching)
    var queryEmbed []float32
    cacheKey := fmt.Sprintf("%s:%s", req.Scope, req.Query)

    if cached, ok := m.cache.Get(cacheKey); ok {
        queryEmbed = cached
    } else {
        embed, err := m.embedder.Embed(ctx, req.Query)
        if err != nil {
            return nil, fmt.Errorf("query embedding failed: %w", err)
        }
        queryEmbed = embed
        m.cache.Add(cacheKey, embed)
    }

    // Search backend
    results, err := m.backend.Search(ctx, queryEmbed, &SearchOptions{
        Scope:     req.Scope,
        ScopeID:   req.ScopeID,
        Limit:     req.Limit,
        Threshold: req.Threshold,
        Filters:   req.Filters,
    })
    if err != nil {
        return nil, err
    }

    return &SearchResponse{
        Results:    results,
        TotalCount: len(results),
        QueryTime:  time.Since(start),
    }, nil
}
```

---

## 6. Configuration

```yaml
# nexus.yaml
memory:
  enabled: true
  backend: sqlite-vec  # sqlite-vec | lancedb | pgvector
  dimension: 1536      # Must match embedding model

  # Backend-specific config
  sqlite_vec:
    path: ~/.nexus/memory.db

  lancedb:
    path: ~/.nexus/memory.lance

  pgvector:
    conn_string: ${DATABASE_URL}

  # Embedding provider
  embeddings:
    provider: openai   # openai | gemini | ollama
    api_key: ${OPENAI_API_KEY}
    model: text-embedding-3-small

    # Gemini-specific
    project_id: my-project
    location: us-central1

    # Ollama-specific
    ollama_url: http://localhost:11434

  # Indexing behavior
  indexing:
    auto_index_messages: true      # Index all messages
    min_content_length: 10         # Skip very short messages
    max_content_length: 0          # Optional cap for auto-indexed content (0 = no cap)
    batch_size: 100                # Batch size for bulk indexing
    allowed_roles: ["user", "assistant"]

  # Search defaults
  search:
    default_limit: 10
    default_threshold: 0.7
    default_scope: session
```

---

## 7. Memory Search Tool

```go
// internal/tools/memorysearch/tool.go

type MemorySearchTool struct {
    manager *memory.Manager
}

func (t *MemorySearchTool) Name() string {
    return "vector_memory_search"
}

func (t *MemorySearchTool) Description() string {
    return "Search vector memory for relevant context"
}

func (t *MemorySearchTool) Schema() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "Search query to find relevant memories"
            },
            "scope": {
                "type": "string",
                "enum": ["session", "channel", "agent", "global"],
                "description": "Scope to search within"
            },
            "limit": {
                "type": "integer",
                "description": "Maximum number of results",
                "default": 10
            }
        },
        "required": ["query"]
    }`)
}

func (t *MemorySearchTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
    var req struct {
        Query string `json:"query"`
        Scope string `json:"scope"`
        Limit int    `json:"limit"`
    }
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, err
    }

    // Get session context
    session := SessionFromContext(ctx)

    scope := memory.ScopeSession
    scopeID := session.ID
    if req.Scope != "" {
        scope = memory.MemoryScope(req.Scope)
        switch scope {
        case memory.ScopeChannel:
            scopeID = session.ChannelID
        case memory.ScopeAgent:
            scopeID = session.AgentID
        case memory.ScopeGlobal:
            scopeID = ""
        }
    }

    limit := 10
    if req.Limit > 0 {
        limit = req.Limit
    }

    resp, err := t.manager.Search(ctx, &memory.SearchRequest{
        Query:     req.Query,
        Scope:     scope,
        ScopeID:   scopeID,
        Limit:     limit,
        Threshold: 0.7,
    })
    if err != nil {
        return nil, err
    }

    // Format results
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("Found %d relevant memories:\n\n", len(resp.Results)))

    for i, result := range resp.Results {
        sb.WriteString(fmt.Sprintf("%d. [Score: %.2f] %s\n",
            i+1, result.Score, truncate(result.Entry.Content, 200)))
        sb.WriteString(fmt.Sprintf("   Source: %s, Created: %s\n\n",
            result.Entry.Metadata.Source,
            result.Entry.CreatedAt.Format(time.RFC3339)))
    }

    return &ToolResult{
        Content: sb.String(),
    }, nil
}
```

---

## 8. Memory Write Tool

```go
// internal/tools/vectormemory/write.go

func (t *WriteTool) Name() string {
    return "vector_memory_write"
}
```

---

## 9. CLI Commands

```bash
# Search memory
nexus memory search "how did we implement auth?" --scope channel --limit 5

# Index a file or directory
nexus memory index ./docs --scope global

# Show memory stats
nexus memory stats [--scope session|channel|global]

# Compact/optimize memory
nexus memory compact

# Export memory
nexus memory export --scope session --output memories.json

# Import memory
nexus memory import memories.json --scope global
```

---

## 9. Implementation Phases

### Phase 1: Core (Week 1)
- [ ] Backend interface definition
- [ ] sqlite-vec backend implementation
- [ ] OpenAI embedding provider
- [ ] Basic manager with Index/Search

### Phase 2: Providers (Week 2)
- [ ] Gemini embedding provider
- [ ] Ollama embedding provider
- [ ] Provider configuration
- [ ] Batch embedding optimization

### Phase 3: Backends (Week 3)
- [ ] LanceDB backend
- [ ] pgvector backend
- [ ] Backend auto-selection based on config

### Phase 4: Integration (Week 4)
- [ ] memory_search tool
- [ ] Auto-indexing of messages
- [ ] System prompt integration (recent context)
- [ ] CLI commands

---

## Appendix: Embedding Model Comparison

| Provider | Model | Dimension | Cost/1M tokens | Best For |
|----------|-------|-----------|----------------|----------|
| OpenAI | text-embedding-3-small | 1536 | $0.02 | General use, good balance |
| OpenAI | text-embedding-3-large | 3072 | $0.13 | Higher accuracy needs |
| Gemini | text-embedding-004 | 768 | $0.025 | Google ecosystem |
| Ollama | nomic-embed-text | 768 | Free (local) | Privacy, offline |
| Ollama | mxbai-embed-large | 1024 | Free (local) | Higher quality local |
