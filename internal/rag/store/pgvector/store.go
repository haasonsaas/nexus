// Package pgvector provides a document store implementation using PostgreSQL with pgvector extension.
package pgvector

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/rag/store"
	"github.com/haasonsaas/nexus/pkg/models"
	_ "github.com/lib/pq" // PostgreSQL driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store implements store.DocumentStore using pgvector.
type Store struct {
	db        *sql.DB
	dimension int
	ownsDB    bool // whether this store owns the db connection
}

// Config contains configuration for the pgvector store.
type Config struct {
	// DSN is the PostgreSQL connection string.
	// If empty, DB must be provided.
	DSN string

	// DB is an existing database connection to reuse.
	// If provided, DSN is ignored and the store will not close the connection.
	DB *sql.DB

	// Dimension is the embedding dimension (e.g., 1536 for text-embedding-3-small).
	Dimension int

	// RunMigrations controls whether to run migrations on startup.
	// Default is true.
	RunMigrations bool
}

// New creates a new pgvector document store.
func New(cfg Config) (*Store, error) {
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

	s := &Store{
		db:        db,
		dimension: cfg.Dimension,
		ownsDB:    ownsDB,
	}

	// Run migrations by default
	if cfg.RunMigrations {
		if err := s.runMigrations(context.Background()); err != nil {
			if ownsDB {
				db.Close()
			}
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	return s, nil
}

// runMigrations applies pending database migrations.
func (s *Store) runMigrations(ctx context.Context) error {
	// Ensure schema_migrations table exists
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS rag_schema_migrations (
			id TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create rag_schema_migrations: %w", err)
	}

	// Load migrations
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	// Get applied migrations
	applied, err := s.appliedMigrations(ctx)
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

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.ID, err)
		}

		if _, err := tx.ExecContext(ctx, m.UpSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.ID, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO rag_schema_migrations (id) VALUES ($1)`, m.ID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.ID, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.ID, err)
		}
	}

	return nil
}

func (s *Store) appliedMigrations(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM rag_schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query rag_schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan rag_schema_migrations: %w", err)
		}
		applied[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rag_schema_migrations: %w", err)
	}
	return applied, nil
}

// AddDocument stores a document and its chunks.
func (s *Store) AddDocument(ctx context.Context, doc *models.Document, chunks []*models.DocumentChunk) error {
	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	doc.UpdatedAt = time.Now()
	doc.ChunkCount = len(chunks)

	for i, chunk := range chunks {
		if err := s.validateEmbedding(chunk.Embedding, true); err != nil {
			return fmt.Errorf("validate embedding for chunk %d: %w", i, err)
		}
	}

	metadata, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("marshal document metadata: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Upsert document
	_, err = tx.ExecContext(ctx, `
		INSERT INTO rag_documents (id, name, source, source_uri, content_type, content, metadata, chunk_count, total_tokens, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			source = EXCLUDED.source,
			source_uri = EXCLUDED.source_uri,
			content_type = EXCLUDED.content_type,
			content = EXCLUDED.content,
			metadata = EXCLUDED.metadata,
			chunk_count = EXCLUDED.chunk_count,
			total_tokens = EXCLUDED.total_tokens,
			updated_at = EXCLUDED.updated_at
	`, doc.ID, doc.Name, doc.Source, doc.SourceURI, doc.ContentType, doc.Content,
		string(metadata), doc.ChunkCount, doc.TotalTokens, doc.CreatedAt, doc.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert document: %w", err)
	}

	// Delete existing chunks (for updates)
	_, err = tx.ExecContext(ctx, `DELETE FROM rag_document_chunks WHERE document_id = $1`, doc.ID)
	if err != nil {
		return fmt.Errorf("delete existing chunks: %w", err)
	}

	// Insert chunks
	if len(chunks) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO rag_document_chunks (id, document_id, chunk_index, content, start_offset, end_offset, metadata, token_count, embedding, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`)
		if err != nil {
			return fmt.Errorf("prepare chunk insert: %w", err)
		}
		defer stmt.Close()

		for _, chunk := range chunks {
			if chunk.ID == "" {
				chunk.ID = uuid.New().String()
			}
			if chunk.CreatedAt.IsZero() {
				chunk.CreatedAt = time.Now()
			}

			chunkMeta, err := json.Marshal(chunk.Metadata)
			if err != nil {
				return fmt.Errorf("marshal chunk metadata: %w", err)
			}

			embeddingStr := encodeEmbedding(chunk.Embedding)

			_, err = stmt.ExecContext(ctx,
				chunk.ID, doc.ID, chunk.Index, chunk.Content,
				chunk.StartOffset, chunk.EndOffset, string(chunkMeta),
				chunk.TokenCount, embeddingStr, chunk.CreatedAt)
			if err != nil {
				return fmt.Errorf("insert chunk: %w", err)
			}
		}
	}

	return tx.Commit()
}

// GetDocument retrieves a document by ID.
func (s *Store) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	var doc models.Document
	var metadataJSON string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, source, source_uri, content_type, content, metadata, chunk_count, total_tokens, created_at, updated_at
		FROM rag_documents
		WHERE id = $1
	`, id).Scan(
		&doc.ID, &doc.Name, &doc.Source, &doc.SourceURI, &doc.ContentType,
		&doc.Content, &metadataJSON, &doc.ChunkCount, &doc.TotalTokens,
		&doc.CreatedAt, &doc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query document: %w", err)
	}

	if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
		return nil, fmt.Errorf("unmarshal document metadata: %w", err)
	}

	return &doc, nil
}

// ListDocuments lists documents with optional filtering.
func (s *Store) ListDocuments(ctx context.Context, opts *store.ListOptions) ([]*models.Document, error) {
	if opts == nil {
		opts = &store.ListOptions{}
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.OrderBy == "" {
		opts.OrderBy = "created_at"
	}

	query := `SELECT id, name, source, source_uri, content_type, content, metadata, chunk_count, total_tokens, created_at, updated_at FROM rag_documents WHERE 1=1`
	args := []any{}
	argNum := 1

	if opts.Source != "" {
		query += fmt.Sprintf(" AND source = $%d", argNum)
		args = append(args, opts.Source)
		argNum++
	}
	if opts.AgentID != "" {
		query += fmt.Sprintf(" AND metadata->>'agent_id' = $%d", argNum)
		args = append(args, opts.AgentID)
		argNum++
	}
	if opts.SessionID != "" {
		query += fmt.Sprintf(" AND metadata->>'session_id' = $%d", argNum)
		args = append(args, opts.SessionID)
		argNum++
	}
	if opts.ChannelID != "" {
		query += fmt.Sprintf(" AND metadata->>'channel_id' = $%d", argNum)
		args = append(args, opts.ChannelID)
		argNum++
	}
	if len(opts.Tags) > 0 {
		query += fmt.Sprintf(" AND metadata->'tags' ?| $%d", argNum)
		args = append(args, opts.Tags)
		argNum++
	}

	// Order by
	orderDir := "ASC"
	if opts.OrderDesc {
		orderDir = "DESC"
	}
	switch opts.OrderBy {
	case "name":
		query += fmt.Sprintf(" ORDER BY name %s", orderDir)
	case "updated_at":
		query += fmt.Sprintf(" ORDER BY updated_at %s", orderDir)
	default:
		query += fmt.Sprintf(" ORDER BY created_at %s", orderDir)
	}

	// Pagination
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argNum, argNum+1)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	var docs []*models.Document
	for rows.Next() {
		var doc models.Document
		var metadataJSON string

		err := rows.Scan(
			&doc.ID, &doc.Name, &doc.Source, &doc.SourceURI, &doc.ContentType,
			&doc.Content, &metadataJSON, &doc.ChunkCount, &doc.TotalTokens,
			&doc.CreatedAt, &doc.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}

		if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal document metadata: %w", err)
		}

		docs = append(docs, &doc)
	}

	return docs, rows.Err()
}

// DeleteDocument removes a document and all its chunks.
func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rag_documents WHERE id = $1`, id)
	return err
}

// GetChunk retrieves a single chunk by ID.
func (s *Store) GetChunk(ctx context.Context, id string) (*models.DocumentChunk, error) {
	var chunk models.DocumentChunk
	var metadataJSON string
	var embeddingStr sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, document_id, chunk_index, content, start_offset, end_offset, metadata, token_count, embedding, created_at
		FROM rag_document_chunks
		WHERE id = $1
	`, id).Scan(
		&chunk.ID, &chunk.DocumentID, &chunk.Index, &chunk.Content,
		&chunk.StartOffset, &chunk.EndOffset, &metadataJSON,
		&chunk.TokenCount, &embeddingStr, &chunk.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query chunk: %w", err)
	}

	if err := json.Unmarshal([]byte(metadataJSON), &chunk.Metadata); err != nil {
		return nil, fmt.Errorf("unmarshal chunk metadata: %w", err)
	}

	if embeddingStr.Valid {
		chunk.Embedding = decodeEmbedding(embeddingStr.String)
	}

	return &chunk, nil
}

// GetChunksByDocument retrieves all chunks for a document.
func (s *Store) GetChunksByDocument(ctx context.Context, documentID string) ([]*models.DocumentChunk, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, document_id, chunk_index, content, start_offset, end_offset, metadata, token_count, embedding, created_at
		FROM rag_document_chunks
		WHERE document_id = $1
		ORDER BY chunk_index ASC
	`, documentID)
	if err != nil {
		return nil, fmt.Errorf("query chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*models.DocumentChunk
	for rows.Next() {
		var chunk models.DocumentChunk
		var metadataJSON string
		var embeddingStr sql.NullString

		err := rows.Scan(
			&chunk.ID, &chunk.DocumentID, &chunk.Index, &chunk.Content,
			&chunk.StartOffset, &chunk.EndOffset, &metadataJSON,
			&chunk.TokenCount, &embeddingStr, &chunk.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}

		if err := json.Unmarshal([]byte(metadataJSON), &chunk.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal chunk metadata: %w", err)
		}

		if embeddingStr.Valid {
			chunk.Embedding = decodeEmbedding(embeddingStr.String)
		}

		chunks = append(chunks, &chunk)
	}

	return chunks, rows.Err()
}

// Search performs semantic search over chunks.
func (s *Store) Search(ctx context.Context, req *models.DocumentSearchRequest, embedding []float32) (*models.DocumentSearchResponse, error) {
	start := time.Now()

	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.7
	}
	if err := s.validateEmbedding(embedding, false); err != nil {
		return nil, err
	}

	queryVec := encodeEmbedding(embedding)

	// Build query with scope filter
	query := `
		SELECT
			c.id, c.document_id, c.chunk_index, c.content, c.start_offset, c.end_offset,
			c.metadata, c.token_count, c.embedding, c.created_at,
			1 - (c.embedding <=> $1::vector) as similarity
		FROM rag_document_chunks c
		WHERE c.embedding IS NOT NULL
	`
	args := []any{queryVec.String}
	argNum := 2

	// Scope filters
	switch req.Scope {
	case models.DocumentScopeAgent:
		query += fmt.Sprintf(" AND c.metadata->>'agent_id' = $%d", argNum)
		args = append(args, req.ScopeID)
		argNum++
	case models.DocumentScopeSession:
		query += fmt.Sprintf(" AND c.metadata->>'session_id' = $%d", argNum)
		args = append(args, req.ScopeID)
		argNum++
	case models.DocumentScopeChannel:
		query += fmt.Sprintf(" AND c.metadata->>'channel_id' = $%d", argNum)
		args = append(args, req.ScopeID)
		argNum++
	}

	// Tag filter
	if len(req.Tags) > 0 {
		query += fmt.Sprintf(" AND c.metadata->'tags' ?| $%d", argNum)
		args = append(args, req.Tags)
		argNum++
	}

	// Document ID filter
	if len(req.DocumentIDs) > 0 {
		placeholders := make([]string, len(req.DocumentIDs))
		for i, id := range req.DocumentIDs {
			placeholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, id)
			argNum++
		}
		query += fmt.Sprintf(" AND c.document_id IN (%s)", strings.Join(placeholders, ","))
	}

	// Threshold filter
	query += fmt.Sprintf(" AND (1 - (c.embedding <=> $1::vector)) >= $%d", argNum)
	args = append(args, req.Threshold)
	argNum++

	// Order and limit
	query += " ORDER BY c.embedding <=> $1::vector ASC"
	query += fmt.Sprintf(" LIMIT $%d", argNum)
	args = append(args, req.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []*models.DocumentSearchResult
	for rows.Next() {
		var chunk models.DocumentChunk
		var metadataJSON string
		var embeddingStr sql.NullString
		var similarity float64

		err := rows.Scan(
			&chunk.ID, &chunk.DocumentID, &chunk.Index, &chunk.Content,
			&chunk.StartOffset, &chunk.EndOffset, &metadataJSON,
			&chunk.TokenCount, &embeddingStr, &chunk.CreatedAt, &similarity)
		if err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}

		if err := json.Unmarshal([]byte(metadataJSON), &chunk.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal chunk metadata: %w", err)
		}

		// Only include embedding if requested
		if req.IncludeMetadata && embeddingStr.Valid {
			chunk.Embedding = decodeEmbedding(embeddingStr.String)
		}

		results = append(results, &models.DocumentSearchResult{
			Chunk: &chunk,
			Score: float32(similarity),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return &models.DocumentSearchResponse{
		Results:    results,
		TotalCount: len(results),
		QueryTime:  time.Since(start),
	}, nil
}

// UpdateChunkEmbeddings updates embeddings for chunks.
func (s *Store) UpdateChunkEmbeddings(ctx context.Context, embeddings map[string][]float32) error {
	if len(embeddings) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE rag_document_chunks SET embedding = $1 WHERE id = $2`)
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer stmt.Close()

	for id, embedding := range embeddings {
		if err := s.validateEmbedding(embedding, true); err != nil {
			return fmt.Errorf("validate embedding for chunk %s: %w", id, err)
		}
		embeddingStr := encodeEmbedding(embedding)
		_, err := stmt.ExecContext(ctx, embeddingStr, id)
		if err != nil {
			return fmt.Errorf("update chunk %s: %w", id, err)
		}
	}

	return tx.Commit()
}

// Stats returns statistics about the store.
func (s *Store) Stats(ctx context.Context) (*store.StoreStats, error) {
	stats := &store.StoreStats{
		EmbeddingDimension: s.dimension,
	}

	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rag_documents`).Scan(&stats.TotalDocuments)
	if err != nil {
		return nil, fmt.Errorf("count documents: %w", err)
	}

	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(token_count), 0) FROM rag_document_chunks`).Scan(&stats.TotalChunks, &stats.TotalTokens)
	if err != nil {
		return nil, fmt.Errorf("count chunks: %w", err)
	}

	return stats, nil
}

// Close releases resources.
func (s *Store) Close() error {
	if s.ownsDB && s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Helper functions

func (s *Store) validateEmbedding(embedding []float32, allowEmpty bool) error {
	if len(embedding) == 0 {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("embedding is empty")
	}
	if s.dimension > 0 && len(embedding) != s.dimension {
		return fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(embedding), s.dimension)
	}
	for _, v := range embedding {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return fmt.Errorf("embedding contains invalid values")
		}
	}
	return nil
}

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

func decodeEmbedding(s string) []float32 {
	if s == "" {
		return nil
	}

	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")

	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	embedding := make([]float32, len(parts))
	for i, p := range parts {
		var f float64
		fmt.Sscanf(strings.TrimSpace(p), "%f", &f)
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
