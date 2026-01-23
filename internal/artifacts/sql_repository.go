package artifacts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/observability"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// SQLRepository implements Repository using SQL database for metadata storage.
type SQLRepository struct {
	db     *sql.DB
	store  Store
	logger *slog.Logger

	// Prepared statements
	stmtInsert       *sql.Stmt
	stmtGet          *sql.Stmt
	stmtList         *sql.Stmt
	stmtDelete       *sql.Stmt
	stmtPruneExpired *sql.Stmt
}

// NewSQLRepository creates a repository backed by SQL database and the given store.
func NewSQLRepository(db *sql.DB, store Store, logger *slog.Logger) (*SQLRepository, error) {
	if logger == nil {
		logger = slog.Default()
	}

	repo := &SQLRepository{
		db:     db,
		store:  store,
		logger: logger,
	}

	if err := repo.prepareStatements(); err != nil {
		return nil, fmt.Errorf("prepare statements: %w", err)
	}

	return repo, nil
}

func (r *SQLRepository) prepareStatements() error {
	var err error

	r.stmtInsert, err = r.db.Prepare(`
		INSERT INTO artifacts (id, session_id, edge_id, tool_call_id, type, mime_type, filename, size, reference, ttl_seconds, created_at, expires_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}

	r.stmtGet, err = r.db.Prepare(`
		SELECT id, session_id, edge_id, tool_call_id, type, mime_type, filename, size, reference, ttl_seconds, created_at, expires_at, metadata
		FROM artifacts WHERE id = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get: %w", err)
	}

	r.stmtDelete, err = r.db.Prepare(`DELETE FROM artifacts WHERE id = $1`)
	if err != nil {
		return fmt.Errorf("prepare delete: %w", err)
	}

	r.stmtPruneExpired, err = r.db.Prepare(`
		DELETE FROM artifacts WHERE expires_at IS NOT NULL AND expires_at < $1
		RETURNING id, reference
	`)
	if err != nil {
		return fmt.Errorf("prepare prune: %w", err)
	}

	return nil
}

// StoreArtifact persists an artifact from tool execution.
func (r *SQLRepository) StoreArtifact(ctx context.Context, artifact *pb.Artifact, data io.Reader) error {
	if artifact.Id == "" {
		artifact.Id = uuid.NewString()
	}

	now := time.Now()
	meta := &Metadata{
		ID:         artifact.Id,
		Type:       artifact.Type,
		MimeType:   artifact.MimeType,
		Filename:   artifact.Filename,
		Size:       artifact.Size,
		TTLSeconds: artifact.TtlSeconds,
		CreatedAt:  now,
	}

	// Get context values
	if sessionID := observability.GetSessionID(ctx); sessionID != "" {
		meta.SessionID = sessionID
	}
	if edgeID := observability.GetEdgeID(ctx); edgeID != "" {
		meta.EdgeID = edgeID
	}
	toolCallID := observability.GetToolCallID(ctx)

	// Calculate expiration
	ttl := time.Duration(artifact.TtlSeconds) * time.Second
	if ttl == 0 {
		ttl = GetDefaultTTL(artifact.Type)
	}
	meta.ExpiresAt = now.Add(ttl)

	// Handle redacted artifacts
	if strings.HasPrefix(artifact.Reference, "redacted://") {
		meta.Reference = artifact.Reference
		meta.Size = 0
		return r.insertMetadata(ctx, meta, toolCallID, nil)
	}

	// For small artifacts (<1MB), store inline
	const maxInlineSize = 1024 * 1024
	if artifact.Size < maxInlineSize && artifact.Size > 0 {
		buf := make([]byte, artifact.Size)
		n, err := io.ReadFull(data, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("read artifact data: %w", err)
		}
		artifact.Data = buf[:n]
		artifact.Reference = fmt.Sprintf("inline://%s", artifact.Id)
		meta.Reference = artifact.Reference

		// Store inline data in metadata JSON
		inlineData := map[string]interface{}{
			"inline_data": buf[:n],
		}
		return r.insertMetadata(ctx, meta, toolCallID, inlineData)
	}

	// Store in backend
	opts := PutOptions{
		MimeType: artifact.MimeType,
		TTL:      ttl,
		Metadata: map[string]string{
			"type": artifact.Type,
		},
	}
	ref, err := r.store.Put(ctx, artifact.Id, data, opts)
	if err != nil {
		return fmt.Errorf("store artifact: %w", err)
	}
	artifact.Reference = ref
	meta.Reference = ref

	if err := r.insertMetadata(ctx, meta, toolCallID, nil); err != nil {
		// Try to clean up stored data on metadata failure
		_ = r.store.Delete(ctx, artifact.Id)
		return err
	}

	r.logger.Info("artifact stored",
		"id", artifact.Id,
		"type", artifact.Type,
		"size", artifact.Size,
		"reference", artifact.Reference)

	return nil
}

func (r *SQLRepository) insertMetadata(ctx context.Context, meta *Metadata, toolCallID string, extraMeta map[string]interface{}) error {
	var metadataJSON []byte
	var err error
	if extraMeta != nil {
		metadataJSON, err = json.Marshal(extraMeta)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
	}

	var sessionID, edgeID, toolCallPtr *string
	if meta.SessionID != "" {
		sessionID = &meta.SessionID
	}
	if meta.EdgeID != "" {
		edgeID = &meta.EdgeID
	}
	if toolCallID != "" {
		toolCallPtr = &toolCallID
	}

	var expiresAt *time.Time
	if !meta.ExpiresAt.IsZero() {
		expiresAt = &meta.ExpiresAt
	}

	_, err = r.stmtInsert.ExecContext(ctx,
		meta.ID,
		sessionID,
		edgeID,
		toolCallPtr,
		meta.Type,
		meta.MimeType,
		meta.Filename,
		meta.Size,
		meta.Reference,
		meta.TTLSeconds,
		meta.CreatedAt,
		expiresAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("insert artifact metadata: %w", err)
	}

	return nil
}

// GetArtifact retrieves artifact metadata and data.
func (r *SQLRepository) GetArtifact(ctx context.Context, artifactID string) (*pb.Artifact, io.ReadCloser, error) {
	var (
		id, artType, mimeType, reference string
		sessionID, edgeID, toolCallID    sql.NullString
		filename                         sql.NullString
		size                             int64
		ttlSeconds                       int32
		createdAt                        time.Time
		expiresAt                        sql.NullTime
		metadataJSON                     sql.NullString
	)

	err := r.stmtGet.QueryRowContext(ctx, artifactID).Scan(
		&id, &sessionID, &edgeID, &toolCallID,
		&artType, &mimeType, &filename, &size, &reference,
		&ttlSeconds, &createdAt, &expiresAt, &metadataJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("artifact not found: %s", artifactID)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query artifact: %w", err)
	}

	// Check expiration
	if expiresAt.Valid && time.Now().After(expiresAt.Time) {
		_ = r.DeleteArtifact(ctx, artifactID)
		return nil, nil, fmt.Errorf("artifact expired: %s", artifactID)
	}

	artifact := &pb.Artifact{
		Id:         id,
		Type:       artType,
		MimeType:   mimeType,
		Size:       size,
		Reference:  reference,
		TtlSeconds: ttlSeconds,
	}
	if filename.Valid {
		artifact.Filename = filename.String
	}

	// Handle redacted artifacts
	if strings.HasPrefix(reference, "redacted://") {
		return artifact, io.NopCloser(bytes.NewReader(nil)), nil
	}

	// Handle inline data
	if strings.HasPrefix(reference, "inline://") && metadataJSON.Valid {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(metadataJSON.String), &meta); err == nil {
			if inlineData, ok := meta["inline_data"].([]interface{}); ok {
				data := make([]byte, len(inlineData))
				for i, v := range inlineData {
					if f, ok := v.(float64); ok {
						data[i] = byte(f)
					}
				}
				artifact.Data = data
				return artifact, io.NopCloser(bytes.NewReader(data)), nil
			}
		}
	}

	// Fetch from store
	data, err := r.store.Get(ctx, artifactID)
	if err != nil {
		return nil, nil, fmt.Errorf("get artifact data: %w", err)
	}

	return artifact, data, nil
}

// ListArtifacts finds artifacts matching criteria.
func (r *SQLRepository) ListArtifacts(ctx context.Context, filter Filter) ([]*pb.Artifact, error) {
	query := `
		SELECT id, session_id, edge_id, type, mime_type, filename, size, reference, ttl_seconds, created_at, expires_at
		FROM artifacts
		WHERE (expires_at IS NULL OR expires_at > $1)
	`
	args := []interface{}{time.Now()}
	argIdx := 2

	if filter.SessionID != "" {
		query += fmt.Sprintf(" AND session_id = $%d", argIdx)
		args = append(args, filter.SessionID)
		argIdx++
	}
	if filter.EdgeID != "" {
		query += fmt.Sprintf(" AND edge_id = $%d", argIdx)
		args = append(args, filter.EdgeID)
		argIdx++
	}
	if filter.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", argIdx)
		args = append(args, filter.Type)
		argIdx++
	}
	if !filter.CreatedAfter.IsZero() {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, filter.CreatedAfter)
		argIdx++
	}
	if !filter.CreatedBefore.IsZero() {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, filter.CreatedBefore)
		argIdx++
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query artifacts: %w", err)
	}
	defer rows.Close()

	var results []*pb.Artifact
	for rows.Next() {
		var (
			id, artType, mimeType, reference string
			sessionID, edgeID                sql.NullString
			filename                         sql.NullString
			size                             int64
			ttlSeconds                       int32
			createdAt                        time.Time
			expiresAt                        sql.NullTime
		)

		if err := rows.Scan(&id, &sessionID, &edgeID, &artType, &mimeType, &filename, &size, &reference, &ttlSeconds, &createdAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}

		artifact := &pb.Artifact{
			Id:         id,
			Type:       artType,
			MimeType:   mimeType,
			Size:       size,
			Reference:  reference,
			TtlSeconds: ttlSeconds,
		}
		if filename.Valid {
			artifact.Filename = filename.String
		}

		results = append(results, artifact)
	}

	return results, rows.Err()
}

// DeleteArtifact removes an artifact and its data.
func (r *SQLRepository) DeleteArtifact(ctx context.Context, artifactID string) error {
	// Get reference first to know where to delete from
	var reference string
	err := r.db.QueryRowContext(ctx, "SELECT reference FROM artifacts WHERE id = $1", artifactID).Scan(&reference)
	if err == sql.ErrNoRows {
		return nil // Already deleted
	}
	if err != nil {
		return fmt.Errorf("get artifact reference: %w", err)
	}

	// Delete metadata first
	_, err = r.stmtDelete.ExecContext(ctx, artifactID)
	if err != nil {
		return fmt.Errorf("delete artifact metadata: %w", err)
	}

	// Delete from store if not inline/redacted
	if !strings.HasPrefix(reference, "inline://") && !strings.HasPrefix(reference, "redacted://") {
		if err := r.store.Delete(ctx, artifactID); err != nil {
			r.logger.Warn("failed to delete artifact from store",
				"id", artifactID,
				"error", err)
		}
	}

	r.logger.Info("artifact deleted", "id", artifactID)
	return nil
}

// PruneExpired removes expired artifacts.
func (r *SQLRepository) PruneExpired(ctx context.Context) (int, error) {
	rows, err := r.stmtPruneExpired.QueryContext(ctx, time.Now())
	if err != nil {
		return 0, fmt.Errorf("prune expired artifacts: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, reference string
		if err := rows.Scan(&id, &reference); err != nil {
			continue
		}

		// Delete from store if not inline/redacted
		if !strings.HasPrefix(reference, "inline://") && !strings.HasPrefix(reference, "redacted://") {
			if err := r.store.Delete(ctx, id); err != nil {
				r.logger.Warn("failed to delete expired artifact from store",
					"id", id,
					"error", err)
			}
		}
		count++
	}

	r.logger.Info("pruned expired artifacts", "count", count)
	return count, rows.Err()
}

// Close releases resources.
func (r *SQLRepository) Close() error {
	var errs []error
	if r.stmtInsert != nil {
		if err := r.stmtInsert.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.stmtGet != nil {
		if err := r.stmtGet.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.stmtDelete != nil {
		if err := r.stmtDelete.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.stmtPruneExpired != nil {
		if err := r.stmtPruneExpired.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close statements: %v", errs)
	}
	return nil
}
