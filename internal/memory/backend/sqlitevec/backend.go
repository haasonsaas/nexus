// Package sqlitevec provides a vector storage backend using SQLite with the vec0 extension.
package sqlitevec

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/memory/backend"
	"github.com/haasonsaas/nexus/pkg/models"
	_ "modernc.org/sqlite" // Pure-Go SQLite driver
)

// Backend implements the backend.Backend interface using sqlite-vec.
type Backend struct {
	db        *sql.DB
	dimension int
}

// Config contains configuration for the sqlite-vec backend.
type Config struct {
	Path      string // Path to SQLite database file
	Dimension int    // Embedding dimension
}

// New creates a new sqlite-vec backend.
func New(cfg Config) (*Backend, error) {
	if cfg.Path == "" {
		cfg.Path = ":memory:"
	}
	if cfg.Dimension == 0 {
		cfg.Dimension = 1536 // Default to OpenAI text-embedding-3-small
	}

	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	b := &Backend{
		db:        db,
		dimension: cfg.Dimension,
	}

	if err := b.init(); err != nil {
		db.Close()
		return nil, err
	}

	return b, nil
}

func (b *Backend) init() error {
	// Note: In production with CGO, you would load the vec0 extension:
	// _, err := b.db.Exec("SELECT load_extension('vec0')")

	// Create memories table
	_, err := b.db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			channel_id TEXT,
			agent_id TEXT,
			content TEXT NOT NULL,
			metadata TEXT,
			embedding BLOB,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create memories table: %w", err)
	}

	// Create indexes for scoping
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id)",
		"CREATE INDEX IF NOT EXISTS idx_memories_channel ON memories(channel_id)",
		"CREATE INDEX IF NOT EXISTS idx_memories_agent ON memories(agent_id)",
		"CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at)",
	}
	for _, idx := range indexes {
		if _, err := b.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// Index stores memory entries with their embeddings.
func (b *Backend) Index(ctx context.Context, entries []*models.MemoryEntry) error {
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
		INSERT OR REPLACE INTO memories (id, session_id, channel_id, agent_id, content, metadata, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
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

		embedding := encodeEmbedding(entry.Embedding)

		_, err = stmt.ExecContext(ctx,
			entry.ID,
			nullString(entry.SessionID),
			nullString(entry.ChannelID),
			nullString(entry.AgentID),
			entry.Content,
			string(metadata),
			embedding,
			entry.CreatedAt,
			entry.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert entry: %w", err)
		}
	}

	return tx.Commit()
}

// Search finds similar entries using cosine similarity.
func (b *Backend) Search(ctx context.Context, queryEmbedding []float32, opts *backend.SearchOptions) ([]*models.SearchResult, error) {
	if opts == nil {
		opts = &backend.SearchOptions{Limit: 10}
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Build query with scope filter
	query := `SELECT id, session_id, channel_id, agent_id, content, metadata, embedding, created_at, updated_at FROM memories WHERE 1=1`
	args := []any{}

	switch opts.Scope {
	case models.ScopeSession:
		query += " AND session_id = ?"
		args = append(args, opts.ScopeID)
	case models.ScopeChannel:
		query += " AND channel_id = ?"
		args = append(args, opts.ScopeID)
	case models.ScopeAgent:
		query += " AND agent_id = ?"
		args = append(args, opts.ScopeID)
	}

	// Note: In production with vec0 extension, you would use:
	// SELECT *, vec_distance_cosine(embedding, ?) as distance
	// ORDER BY distance ASC

	rows, err := b.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	var results []*models.SearchResult
	for rows.Next() {
		entry, embeddingBlob, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}

		// Decode embedding and calculate similarity
		embedding := decodeEmbedding(embeddingBlob)
		score := cosineSimilarity(queryEmbedding, embedding)

		if opts.Threshold > 0 && score < opts.Threshold {
			continue
		}

		results = append(results, &models.SearchResult{
			Entry: entry,
			Score: score,
		})
	}

	// Sort by score descending and limit
	sortByScoreDesc(results)
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// Delete removes entries by ID.
func (b *Backend) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, "DELETE FROM memories WHERE id = ?")
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("prepare delete statement: %w (rollback: %v)", err, rbErr)
		}
		return fmt.Errorf("prepare delete statement: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				return fmt.Errorf("delete memory %s: %w (rollback: %v)", id, err, rbErr)
			}
			return fmt.Errorf("delete memory %s: %w", id, err)
		}
	}

	return tx.Commit()
}

// Count returns the number of entries matching the scope.
func (b *Backend) Count(ctx context.Context, scope models.MemoryScope, scopeID string) (int64, error) {
	query := "SELECT COUNT(*) FROM memories WHERE 1=1"
	args := []any{}

	switch scope {
	case models.ScopeSession:
		query += " AND session_id = ?"
		args = append(args, scopeID)
	case models.ScopeChannel:
		query += " AND channel_id = ?"
		args = append(args, scopeID)
	case models.ScopeAgent:
		query += " AND agent_id = ?"
		args = append(args, scopeID)
	}

	var count int64
	err := b.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// Compact optimizes the database.
func (b *Backend) Compact(ctx context.Context) error {
	_, err := b.db.ExecContext(ctx, "VACUUM")
	return err
}

// Close releases resources.
func (b *Backend) Close() error {
	return b.db.Close()
}

// Helper functions

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func scanEntry(rows *sql.Rows) (*models.MemoryEntry, []byte, error) {
	var entry models.MemoryEntry
	var sessionID, channelID, agentID sql.NullString
	var metadataJSON string
	var embeddingBlob []byte

	err := rows.Scan(
		&entry.ID,
		&sessionID,
		&channelID,
		&agentID,
		&entry.Content,
		&metadataJSON,
		&embeddingBlob,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to scan row: %w", err)
	}

	entry.SessionID = sessionID.String
	entry.ChannelID = channelID.String
	entry.AgentID = agentID.String

	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &entry.Metadata); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &entry, embeddingBlob, nil
}

// encodeEmbedding converts []float32 to bytes for storage.
func encodeEmbedding(embedding []float32) []byte {
	if len(embedding) == 0 {
		return nil
	}
	// Simple encoding: 4 bytes per float32 using IEEE 754 bits
	data := make([]byte, len(embedding)*4)
	for i, f := range embedding {
		bits := math.Float32bits(f)
		data[i*4] = byte(bits)
		data[i*4+1] = byte(bits >> 8)
		data[i*4+2] = byte(bits >> 16)
		data[i*4+3] = byte(bits >> 24)
	}
	return data
}

// decodeEmbedding converts bytes back to []float32.
func decodeEmbedding(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		bits := uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}

// cosineSimilarity calculates the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt32(normA) * sqrt32(normB))
}

func sqrt32(x float32) float32 {
	// Newton-Raphson approximation
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// sortByScoreDesc sorts results by score in descending order.
func sortByScoreDesc(results []*models.SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}
