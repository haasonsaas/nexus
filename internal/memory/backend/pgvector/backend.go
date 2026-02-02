// Package pgvector provides a vector storage backend using PostgreSQL with pgvector extension.
package pgvector

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/memory/backend"
	"github.com/haasonsaas/nexus/pkg/models"
	pq "github.com/lib/pq" // PostgreSQL driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Backend implements the backend.Backend interface using pgvector.
type Backend struct {
	db        *sql.DB
	dimension int
	ownsDB    bool // whether this backend owns the db connection and should close it
}

// Config contains configuration for the pgvector backend.
type Config struct {
	// DSN is the PostgreSQL connection string.
	// If empty, DB must be provided.
	DSN string

	// DB is an existing database connection to reuse.
	// If provided, DSN is ignored and the backend will not close the connection.
	DB *sql.DB

	// Dimension is the embedding dimension (e.g., 1536 for text-embedding-3-small).
	Dimension int

	// RunMigrations controls whether to run migrations on startup.
	// Default is true.
	RunMigrations bool
}

// New creates a new pgvector backend.
func New(cfg Config) (*Backend, error) {
	if cfg.Dimension == 0 {
		cfg.Dimension = 1536 // Default to OpenAI text-embedding-3-small
	}

	var db *sql.DB
	var ownsDB bool
	var err error

	if cfg.DB != nil {
		// Reuse existing connection
		db = cfg.DB
		ownsDB = false
	} else if cfg.DSN != "" {
		// Create new connection
		db, err = sql.Open("postgres", cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		ownsDB = true

		// Verify connection
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to ping database: %w", err)
		}
	} else {
		return nil, fmt.Errorf("either DSN or DB must be provided")
	}

	b := &Backend{
		db:        db,
		dimension: cfg.Dimension,
		ownsDB:    ownsDB,
	}

	// Run migrations by default
	if cfg.RunMigrations {
		if err := b.runMigrations(context.Background()); err != nil {
			if ownsDB {
				db.Close()
			}
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	return b, nil
}

// runMigrations applies pending database migrations.
func (b *Backend) runMigrations(ctx context.Context) error {
	// Ensure schema_migrations table exists
	_, err := b.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memory_schema_migrations (
			id TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Load migrations
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	// Get applied migrations
	applied, err := b.appliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	// Apply pending migrations
	for _, m := range migrations {
		if applied[m.ID] {
			continue
		}

		if strings.TrimSpace(m.UpSQL) == "" {
			return fmt.Errorf("missing up migration for %s", m.ID)
		}

		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.ID, err)
		}

		if _, err := tx.ExecContext(ctx, m.UpSQL); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				_ = rbErr
			}
			return fmt.Errorf("apply migration %s: %w", m.ID, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_schema_migrations (id) VALUES ($1)`, m.ID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				_ = rbErr
			}
			return fmt.Errorf("record migration %s: %w", m.ID, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.ID, err)
		}
	}

	return nil
}

func (b *Backend) appliedMigrations(ctx context.Context) (map[string]bool, error) {
	rows, err := b.db.QueryContext(ctx, `SELECT id FROM memory_schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query memory_schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan memory_schema_migrations: %w", err)
		}
		applied[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory_schema_migrations: %w", err)
	}
	return applied, nil
}

// Index stores memory entries with their embeddings.
func (b *Backend) Index(ctx context.Context, entries []*models.MemoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			_ = err
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memories (id, session_id, channel_id, agent_id, content, metadata, embedding, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			session_id = EXCLUDED.session_id,
			channel_id = EXCLUDED.channel_id,
			agent_id = EXCLUDED.agent_id,
			content = EXCLUDED.content,
			metadata = EXCLUDED.metadata,
			embedding = EXCLUDED.embedding,
			updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		if entry.ID == "" {
			entry.ID = uuid.New().String()
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = time.Now()
		}
		entry.UpdatedAt = time.Now()

		metadata, err := json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		// Encode embedding as pgvector string format: [0.1,0.2,...]
		embeddingStr := encodeEmbedding(entry.Embedding)

		_, err = stmt.ExecContext(ctx,
			entry.ID,
			nullString(entry.SessionID),
			nullString(entry.ChannelID),
			nullString(entry.AgentID),
			entry.Content,
			string(metadata),
			embeddingStr,
			entry.CreatedAt,
			entry.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert entry: %w", err)
		}
	}

	return tx.Commit()
}

// Search finds similar entries using vector similarity, BM25, or hybrid search.
func (b *Backend) Search(ctx context.Context, queryEmbedding []float32, opts *backend.SearchOptions) ([]*models.SearchResult, error) {
	if opts == nil {
		opts = &backend.SearchOptions{Limit: 10}
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Route to appropriate search method based on mode
	switch opts.SearchMode {
	case backend.SearchModeBM25:
		return b.searchBM25(ctx, opts)
	case backend.SearchModeHybrid:
		return b.searchHybrid(ctx, queryEmbedding, opts)
	default:
		// Default to vector search
		return b.searchVector(ctx, queryEmbedding, opts)
	}
}

// searchVector performs pure vector similarity search.
func (b *Backend) searchVector(ctx context.Context, queryEmbedding []float32, opts *backend.SearchOptions) ([]*models.SearchResult, error) {
	// Encode query embedding as pgvector string
	queryVec := encodeEmbedding(queryEmbedding)

	// Build query with scope filter
	// Using 1 - cosine_distance to get similarity (0-1 range)
	query := `
		SELECT
			id, session_id, channel_id, agent_id, content, metadata,
			embedding, created_at, updated_at,
			1 - (embedding <=> $1::vector) as similarity
		FROM memories
		WHERE embedding IS NOT NULL
	`
	args := []any{queryVec}
	argNum := 2

	query, args, argNum = b.addScopeFilter(query, args, argNum, opts)

	// Add threshold filter
	if opts.Threshold > 0 {
		query += fmt.Sprintf(" AND (1 - (embedding <=> $1::vector)) >= $%d", argNum)
		args = append(args, opts.Threshold)
		argNum++
	}

	// Order by similarity (ascending distance = descending similarity)
	query += " ORDER BY embedding <=> $1::vector ASC"

	// Limit results
	query += fmt.Sprintf(" LIMIT $%d", argNum)
	args = append(args, opts.Limit)

	return b.executeSearch(ctx, query, args)
}

// searchBM25 performs full-text search using PostgreSQL's built-in FTS.
func (b *Backend) searchBM25(ctx context.Context, opts *backend.SearchOptions) ([]*models.SearchResult, error) {
	if opts.Query == "" {
		return nil, fmt.Errorf("query text is required for BM25 search")
	}

	// Convert query to tsquery format
	// plainto_tsquery handles natural language queries
	query := `
		SELECT
			id, session_id, channel_id, agent_id, content, metadata,
			embedding, created_at, updated_at,
			ts_rank_cd(content_tsv, plainto_tsquery('english', $1)) as similarity
		FROM memories
		WHERE content_tsv @@ plainto_tsquery('english', $1)
	`
	args := []any{opts.Query}
	argNum := 2

	query, args, argNum = b.addScopeFilter(query, args, argNum, opts)

	// Add threshold filter on BM25 rank
	if opts.Threshold > 0 {
		query += fmt.Sprintf(" AND ts_rank_cd(content_tsv, plainto_tsquery('english', $1)) >= $%d", argNum)
		args = append(args, opts.Threshold)
		argNum++
	}

	// Order by BM25 rank (descending)
	query += " ORDER BY similarity DESC"

	// Limit results
	query += fmt.Sprintf(" LIMIT $%d", argNum)
	args = append(args, opts.Limit)

	return b.executeSearch(ctx, query, args)
}

// searchHybrid combines vector and BM25 search with reciprocal rank fusion.
func (b *Backend) searchHybrid(ctx context.Context, queryEmbedding []float32, opts *backend.SearchOptions) ([]*models.SearchResult, error) {
	if opts.Query == "" {
		// Fall back to vector-only if no query text
		return b.searchVector(ctx, queryEmbedding, opts)
	}

	// Default hybrid alpha (weight for vector score)
	alpha := opts.HybridAlpha
	if alpha <= 0 {
		alpha = 0.7 // Default: 70% vector, 30% BM25
	}

	// Encode query embedding as pgvector string
	queryVec := encodeEmbedding(queryEmbedding)

	// Use Reciprocal Rank Fusion (RRF) to combine scores
	// RRF(d) = sum(1 / (k + rank_i(d))) where k is a constant (typically 60)
	// This query:
	// 1. Gets vector search results with ranks
	// 2. Gets BM25 search results with ranks
	// 3. Combines using weighted RRF
	query := `
		WITH vector_results AS (
			SELECT
				id, session_id, channel_id, agent_id, content, metadata,
				embedding, created_at, updated_at,
				1 - (embedding <=> $1::vector) as vec_score,
				ROW_NUMBER() OVER (ORDER BY embedding <=> $1::vector ASC) as vec_rank
			FROM memories
			WHERE embedding IS NOT NULL
		),
		bm25_results AS (
			SELECT
				id,
				ts_rank_cd(content_tsv, plainto_tsquery('english', $2)) as bm25_score,
				ROW_NUMBER() OVER (ORDER BY ts_rank_cd(content_tsv, plainto_tsquery('english', $2)) DESC) as bm25_rank
			FROM memories
			WHERE content_tsv @@ plainto_tsquery('english', $2)
		),
		combined AS (
			SELECT
				v.id, v.session_id, v.channel_id, v.agent_id, v.content, v.metadata,
				v.embedding, v.created_at, v.updated_at,
				-- Hybrid score: weighted combination of RRF scores
				($3 * (1.0 / (60 + v.vec_rank))) + ((1 - $3) * COALESCE(1.0 / (60 + b.bm25_rank), 0)) as similarity
			FROM vector_results v
			LEFT JOIN bm25_results b ON v.id = b.id
		)
		SELECT
			id, session_id, channel_id, agent_id, content, metadata,
			embedding, created_at, updated_at, similarity
		FROM combined
		WHERE 1=1
	`
	args := []any{queryVec, opts.Query, alpha}
	argNum := 4

	// Add scope filters
	switch opts.Scope {
	case models.ScopeSession:
		query += fmt.Sprintf(" AND session_id = $%d", argNum)
		args = append(args, opts.ScopeID)
		argNum++
	case models.ScopeChannel:
		query += fmt.Sprintf(" AND channel_id = $%d", argNum)
		args = append(args, opts.ScopeID)
		argNum++
	case models.ScopeAgent:
		query += fmt.Sprintf(" AND agent_id = $%d", argNum)
		args = append(args, opts.ScopeID)
		argNum++
	case models.ScopeGlobal:
		query += " AND (session_id IS NULL OR session_id = '') AND (channel_id IS NULL OR channel_id = '') AND (agent_id IS NULL OR agent_id = '')"
	}

	// Order by hybrid score
	query += " ORDER BY similarity DESC"

	// Limit results
	query += fmt.Sprintf(" LIMIT $%d", argNum)
	args = append(args, opts.Limit)

	return b.executeSearch(ctx, query, args)
}

// addScopeFilter adds scope filtering to a query.
func (b *Backend) addScopeFilter(query string, args []any, argNum int, opts *backend.SearchOptions) (string, []any, int) {
	switch opts.Scope {
	case models.ScopeSession:
		query += fmt.Sprintf(" AND session_id = $%d", argNum)
		args = append(args, opts.ScopeID)
		argNum++
	case models.ScopeChannel:
		query += fmt.Sprintf(" AND channel_id = $%d", argNum)
		args = append(args, opts.ScopeID)
		argNum++
	case models.ScopeAgent:
		query += fmt.Sprintf(" AND agent_id = $%d", argNum)
		args = append(args, opts.ScopeID)
		argNum++
	case models.ScopeGlobal:
		query += " AND (session_id IS NULL OR session_id = '') AND (channel_id IS NULL OR channel_id = '') AND (agent_id IS NULL OR agent_id = '')"
	}
	return query, args, argNum
}

// executeSearch executes a search query and returns results.
func (b *Backend) executeSearch(ctx context.Context, query string, args []any) ([]*models.SearchResult, error) {
	rows, err := b.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	var results []*models.SearchResult
	for rows.Next() {
		entry, similarity, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}

		results = append(results, &models.SearchResult{
			Entry: entry,
			Score: similarity,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

// Delete removes entries by ID.
func (b *Backend) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	_, err := b.db.ExecContext(ctx, "DELETE FROM memories WHERE id = ANY($1::uuid[])", pq.Array(ids))
	return err
}

// Count returns the number of entries matching the scope.
func (b *Backend) Count(ctx context.Context, scope models.MemoryScope, scopeID string) (int64, error) {
	query := "SELECT COUNT(*) FROM memories WHERE 1=1"
	args := []any{}
	argNum := 1

	switch scope {
	case models.ScopeSession:
		query += fmt.Sprintf(" AND session_id = $%d", argNum)
		args = append(args, scopeID)
	case models.ScopeChannel:
		query += fmt.Sprintf(" AND channel_id = $%d", argNum)
		args = append(args, scopeID)
	case models.ScopeAgent:
		query += fmt.Sprintf(" AND agent_id = $%d", argNum)
		args = append(args, scopeID)
	case models.ScopeGlobal:
		query += " AND (session_id IS NULL OR session_id = '') AND (channel_id IS NULL OR channel_id = '') AND (agent_id IS NULL OR agent_id = '')"
	}

	var count int64
	err := b.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// Compact optimizes the database by running VACUUM ANALYZE.
func (b *Backend) Compact(ctx context.Context) error {
	_, err := b.db.ExecContext(ctx, "VACUUM ANALYZE memories")
	return err
}

// Close releases resources.
func (b *Backend) Close() error {
	if b.ownsDB && b.db != nil {
		return b.db.Close()
	}
	return nil
}

// Helper functions

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func scanEntry(rows *sql.Rows) (*models.MemoryEntry, float32, error) {
	var entry models.MemoryEntry
	var sessionID, channelID, agentID sql.NullString
	var metadataJSON string
	var embeddingStr sql.NullString
	var similarity float64

	err := rows.Scan(
		&entry.ID,
		&sessionID,
		&channelID,
		&agentID,
		&entry.Content,
		&metadataJSON,
		&embeddingStr,
		&entry.CreatedAt,
		&entry.UpdatedAt,
		&similarity,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to scan row: %w", err)
	}

	entry.SessionID = sessionID.String
	entry.ChannelID = channelID.String
	entry.AgentID = agentID.String

	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &entry.Metadata); err != nil {
			return nil, 0, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	// Decode embedding from pgvector string format
	if embeddingStr.Valid {
		entry.Embedding = decodeEmbedding(embeddingStr.String)
	}

	return &entry, float32(similarity), nil
}

// encodeEmbedding converts []float32 to pgvector string format: [0.1,0.2,...]
func encodeEmbedding(embedding []float32) sql.NullString {
	if len(embedding) == 0 {
		return sql.NullString{}
	}

	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range embedding {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%g", f))
	}
	sb.WriteByte(']')

	return sql.NullString{String: sb.String(), Valid: true}
}

// decodeEmbedding converts pgvector string format back to []float32
func decodeEmbedding(s string) []float32 {
	if s == "" {
		return nil
	}

	// Remove brackets
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")

	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	embedding := make([]float32, len(parts))
	for i, p := range parts {
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%f", &f); err != nil {
			return nil
		}
		embedding[i] = float32(f)
	}

	return embedding
}

// Migration represents an embedded migration.
type Migration struct {
	ID      string
	UpSQL   string
	DownSQL string
}

func loadMigrations() ([]Migration, error) {
	paths, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}

	entries := map[string]*Migration{}
	for _, path := range paths {
		base := strings.TrimPrefix(path, "migrations/")
		suffix := ""
		switch {
		case strings.HasSuffix(base, ".up.sql"):
			suffix = ".up.sql"
		case strings.HasSuffix(base, ".down.sql"):
			suffix = ".down.sql"
		default:
			continue
		}
		id := strings.TrimSuffix(base, suffix)
		entry := entries[id]
		if entry == nil {
			entry = &Migration{ID: id}
			entries[id] = entry
		}
		data, err := migrationsFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", path, err)
		}
		if suffix == ".up.sql" {
			entry.UpSQL = string(data)
		} else {
			entry.DownSQL = string(data)
		}
	}

	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	migrations := make([]Migration, 0, len(ids))
	for _, id := range ids {
		migrations = append(migrations, *entries[id])
	}
	return migrations, nil
}
