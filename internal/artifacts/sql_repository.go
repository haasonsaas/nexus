package artifacts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/observability"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// SQLRepository stores artifact metadata in SQL and artifact data in a Store backend.
type SQLRepository struct {
	db     *sql.DB
	store  Store
	logger *slog.Logger
}

// NewSQLRepository creates a SQL-backed repository and ensures the schema exists.
func NewSQLRepository(db *sql.DB, store Store, logger *slog.Logger) (*SQLRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	if store == nil {
		return nil, fmt.Errorf("artifact store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	repo := &SQLRepository{
		db:     db,
		store:  store,
		logger: logger,
	}
	if err := repo.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return repo, nil
}

// Close closes the underlying store and database connection.
func (r *SQLRepository) Close() error {
	if r.store != nil {
		if err := r.store.Close(); err != nil {
			return err
		}
	}
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// StoreArtifact persists an artifact from tool execution.
func (r *SQLRepository) StoreArtifact(ctx context.Context, artifact *pb.Artifact, data io.Reader) error {
	if artifact == nil {
		return fmt.Errorf("artifact is required")
	}
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
	if sessionID := observability.GetSessionID(ctx); sessionID != "" {
		meta.SessionID = sessionID
	}
	if edgeID := observability.GetEdgeID(ctx); edgeID != "" {
		meta.EdgeID = edgeID
	}

	ttl := time.Duration(artifact.TtlSeconds) * time.Second
	if ttl == 0 {
		ttl = GetDefaultTTL(artifact.Type)
	}
	meta.ExpiresAt = now.Add(ttl)

	if strings.HasPrefix(artifact.Reference, "redacted://") {
		meta.Reference = artifact.Reference
		meta.Size = 0
		if err := r.upsertMetadata(ctx, meta); err != nil {
			return err
		}
		r.logger.Info("artifact redacted", "id", artifact.Id, "type", artifact.Type)
		return nil
	}

	opts := PutOptions{
		MimeType: artifact.MimeType,
		TTL:      ttl,
		Metadata: map[string]string{
			"type": artifact.Type,
		},
	}
	if meta.SessionID != "" {
		opts.Metadata["session_id"] = meta.SessionID
	}
	if meta.EdgeID != "" {
		opts.Metadata["edge_id"] = meta.EdgeID
	}

	ref, err := r.store.Put(ctx, artifact.Id, data, opts)
	if err != nil {
		return fmt.Errorf("store artifact: %w", err)
	}
	artifact.Reference = ref
	meta.Reference = ref

	if err := r.upsertMetadata(ctx, meta); err != nil {
		if delErr := r.store.Delete(ctx, artifact.Id); delErr != nil {
			r.logger.Warn("failed to cleanup stored artifact after metadata upsert error", "id", artifact.Id, "error", delErr)
		}
		return err
	}

	r.logger.Info("artifact stored",
		"id", artifact.Id,
		"type", artifact.Type,
		"size", artifact.Size,
		"reference", artifact.Reference)

	return nil
}

// GetArtifact retrieves artifact metadata and data.
func (r *SQLRepository) GetArtifact(ctx context.Context, artifactID string) (*pb.Artifact, io.ReadCloser, error) {
	meta, err := r.fetchMetadata(ctx, artifactID)
	if err != nil {
		return nil, nil, err
	}

	if !meta.ExpiresAt.IsZero() && time.Now().After(meta.ExpiresAt) {
		if err := r.DeleteArtifact(ctx, artifactID); err != nil {
			r.logger.Warn("failed to delete expired artifact", "id", artifactID, "error", err)
		}
		return nil, nil, fmt.Errorf("artifact expired: %s", artifactID)
	}

	artifact := &pb.Artifact{
		Id:         meta.ID,
		Type:       meta.Type,
		MimeType:   meta.MimeType,
		Filename:   meta.Filename,
		Size:       meta.Size,
		Reference:  meta.Reference,
		TtlSeconds: meta.TTLSeconds,
	}

	if strings.HasPrefix(meta.Reference, "redacted://") {
		return artifact, io.NopCloser(strings.NewReader("")), nil
	}

	reader, err := r.store.Get(ctx, artifactID)
	if err != nil {
		return nil, nil, fmt.Errorf("get artifact data: %w", err)
	}
	return artifact, reader, nil
}

// ListArtifacts finds artifacts matching criteria.
func (r *SQLRepository) ListArtifacts(ctx context.Context, filter Filter) ([]*pb.Artifact, error) {
	where := []string{"(expires_at IS NULL OR expires_at > now())"}
	args := []any{}
	argIdx := 1

	if filter.SessionID != "" {
		where = append(where, fmt.Sprintf("session_id = $%d", argIdx))
		args = append(args, filter.SessionID)
		argIdx++
	}
	if filter.EdgeID != "" {
		where = append(where, fmt.Sprintf("edge_id = $%d", argIdx))
		args = append(args, filter.EdgeID)
		argIdx++
	}
	if filter.Type != "" {
		where = append(where, fmt.Sprintf("type = $%d", argIdx))
		args = append(args, filter.Type)
		argIdx++
	}
	if !filter.CreatedAfter.IsZero() {
		where = append(where, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, filter.CreatedAfter)
		argIdx++
	}
	if !filter.CreatedBefore.IsZero() {
		where = append(where, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, filter.CreatedBefore)
		argIdx++
	}

	query := `SELECT id, session_id, edge_id, type, mime_type, filename, size, reference, ttl_seconds, created_at, expires_at
		FROM artifacts`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	var results []*pb.Artifact
	for rows.Next() {
		meta, err := scanMetadata(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, &pb.Artifact{
			Id:         meta.ID,
			Type:       meta.Type,
			MimeType:   meta.MimeType,
			Filename:   meta.Filename,
			Size:       meta.Size,
			Reference:  meta.Reference,
			TtlSeconds: meta.TTLSeconds,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	return results, nil
}

// DeleteArtifact removes an artifact and its data.
func (r *SQLRepository) DeleteArtifact(ctx context.Context, artifactID string) error {
	meta, err := r.fetchMetadata(ctx, artifactID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if meta.Reference != "" && !strings.HasPrefix(meta.Reference, "redacted://") {
		if err := r.store.Delete(ctx, artifactID); err != nil {
			return fmt.Errorf("delete artifact data: %w", err)
		}
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = $1`, artifactID); err != nil {
		return fmt.Errorf("delete artifact metadata: %w", err)
	}
	r.logger.Info("artifact deleted", "id", artifactID)
	return nil
}

// PruneExpired removes expired artifacts.
func (r *SQLRepository) PruneExpired(ctx context.Context) (int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM artifacts WHERE expires_at IS NOT NULL AND expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("list expired artifacts: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan expired artifact: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("list expired artifacts: %w", err)
	}

	count := 0
	for _, id := range ids {
		if err := r.DeleteArtifact(ctx, id); err == nil {
			count++
		}
	}
	r.logger.Info("pruned expired artifacts", "count", count)
	return count, nil
}

func (r *SQLRepository) ensureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS artifacts (
			id STRING PRIMARY KEY,
			session_id STRING,
			edge_id STRING,
			type STRING,
			mime_type STRING,
			filename STRING,
			size INT8,
			reference STRING,
			ttl_seconds INT4,
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_session_id ON artifacts (session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_edge_id ON artifacts (edge_id)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_type ON artifacts (type)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_created_at ON artifacts (created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_expires_at ON artifacts (expires_at)`,
	}
	for _, stmt := range statements {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure artifacts schema: %w", err)
		}
	}
	return nil
}

func (r *SQLRepository) upsertMetadata(ctx context.Context, meta *Metadata) error {
	if meta == nil {
		return fmt.Errorf("metadata is required")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO artifacts (
			id, session_id, edge_id, type, mime_type, filename, size, reference,
			ttl_seconds, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
			session_id = EXCLUDED.session_id,
			edge_id = EXCLUDED.edge_id,
			type = EXCLUDED.type,
			mime_type = EXCLUDED.mime_type,
			filename = EXCLUDED.filename,
			size = EXCLUDED.size,
			reference = EXCLUDED.reference,
			ttl_seconds = EXCLUDED.ttl_seconds,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
	`,
		meta.ID,
		nullString(meta.SessionID),
		nullString(meta.EdgeID),
		meta.Type,
		meta.MimeType,
		meta.Filename,
		meta.Size,
		meta.Reference,
		meta.TTLSeconds,
		meta.CreatedAt,
		nullTime(meta.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("upsert artifact metadata: %w", err)
	}
	return nil
}

func (r *SQLRepository) fetchMetadata(ctx context.Context, artifactID string) (*Metadata, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, session_id, edge_id, type, mime_type, filename, size, reference, ttl_seconds, created_at, expires_at
		FROM artifacts WHERE id = $1
	`, artifactID)
	meta, err := scanMetadata(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("artifact not found: %s: %w", artifactID, err)
		}
		return nil, err
	}
	return meta, nil
}

type metadataScanner interface {
	Scan(dest ...any) error
}

func scanMetadata(row metadataScanner) (*Metadata, error) {
	var meta Metadata
	var sessionID sql.NullString
	var edgeID sql.NullString
	var mimeType sql.NullString
	var filename sql.NullString
	var reference sql.NullString
	var ttlSeconds sql.NullInt32
	var expiresAt sql.NullTime
	if err := row.Scan(
		&meta.ID,
		&sessionID,
		&edgeID,
		&meta.Type,
		&mimeType,
		&filename,
		&meta.Size,
		&reference,
		&ttlSeconds,
		&meta.CreatedAt,
		&expiresAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan artifact metadata: %w", err)
	}
	if sessionID.Valid {
		meta.SessionID = sessionID.String
	}
	if edgeID.Valid {
		meta.EdgeID = edgeID.String
	}
	if mimeType.Valid {
		meta.MimeType = mimeType.String
	}
	if filename.Valid {
		meta.Filename = filename.String
	}
	if reference.Valid {
		meta.Reference = reference.String
	}
	if ttlSeconds.Valid {
		meta.TTLSeconds = ttlSeconds.Int32
	}
	if expiresAt.Valid {
		meta.ExpiresAt = expiresAt.Time
	}
	return &meta, nil
}

func nullString(value string) sql.NullString {
	if strings.TrimSpace(value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullTime(value time.Time) sql.NullTime {
	if value.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value, Valid: true}
}
