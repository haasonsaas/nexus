package canvas

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/haasonsaas/nexus/internal/storage"
)

// CockroachStore implements Store using CockroachDB/Postgres.
type CockroachStore struct {
	db *sql.DB
}

// NewCockroachStoreFromDSN creates a canvas store using a DSN.
func NewCockroachStoreFromDSN(dsn string, config *storage.CockroachConfig) (*CockroachStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("dsn is required")
	}
	if config == nil {
		config = storage.DefaultCockroachConfig()
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &CockroachStore{db: db}, nil
}

// Close closes the underlying database connection.
func (s *CockroachStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *CockroachStore) CreateSession(ctx context.Context, session *Session) error {
	if session == nil {
		return ErrNotFound
	}
	if session.ID == "" {
		session.ID = uuid.NewString()
	}
	if session.Key == "" {
		return ErrNotFound
	}
	now := time.Now()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = session.CreatedAt
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO canvas_sessions (id, key, workspace_id, channel_id, thread_ts, owner_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`,
		session.ID,
		session.Key,
		session.WorkspaceID,
		session.ChannelID,
		nullString(session.ThreadTS),
		nullString(session.OwnerID),
		session.CreatedAt,
		session.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("create canvas session: %w", err)
	}
	return nil
}

func (s *CockroachStore) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, key, workspace_id, channel_id, thread_ts, owner_id, created_at, updated_at
		FROM canvas_sessions WHERE id = $1
	`, id)

	var session Session
	var threadTS sql.NullString
	var ownerID sql.NullString
	if err := row.Scan(
		&session.ID,
		&session.Key,
		&session.WorkspaceID,
		&session.ChannelID,
		&threadTS,
		&ownerID,
		&session.CreatedAt,
		&session.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get canvas session: %w", err)
	}
	session.ThreadTS = threadTS.String
	session.OwnerID = ownerID.String
	return &session, nil
}

func (s *CockroachStore) GetSessionByKey(ctx context.Context, key string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, key, workspace_id, channel_id, thread_ts, owner_id, created_at, updated_at
		FROM canvas_sessions WHERE key = $1
	`, key)

	var session Session
	var threadTS sql.NullString
	var ownerID sql.NullString
	if err := row.Scan(
		&session.ID,
		&session.Key,
		&session.WorkspaceID,
		&session.ChannelID,
		&threadTS,
		&ownerID,
		&session.CreatedAt,
		&session.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get canvas session by key: %w", err)
	}
	session.ThreadTS = threadTS.String
	session.OwnerID = ownerID.String
	return &session, nil
}

func (s *CockroachStore) UpdateSession(ctx context.Context, session *Session) error {
	if session == nil || session.ID == "" {
		return ErrNotFound
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = time.Now()
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE canvas_sessions
		SET key = $1, workspace_id = $2, channel_id = $3, thread_ts = $4, owner_id = $5, updated_at = $6
		WHERE id = $7
	`,
		session.Key,
		session.WorkspaceID,
		session.ChannelID,
		nullString(session.ThreadTS),
		nullString(session.OwnerID),
		session.UpdatedAt,
		session.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("update canvas session: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *CockroachStore) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM canvas_sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete canvas session: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *CockroachStore) UpsertState(ctx context.Context, state *State) error {
	if state == nil || state.SessionID == "" {
		return ErrNotFound
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO canvas_state (session_id, state_json, updated_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (session_id) DO UPDATE
		SET state_json = excluded.state_json, updated_at = excluded.updated_at
	`,
		state.SessionID,
		json.RawMessage(state.StateJSON),
		state.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert canvas state: %w", err)
	}
	return nil
}

func (s *CockroachStore) GetState(ctx context.Context, sessionID string) (*State, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT session_id, state_json, updated_at
		FROM canvas_state WHERE session_id = $1
	`, sessionID)

	var state State
	var raw []byte
	if err := row.Scan(&state.SessionID, &raw, &state.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get canvas state: %w", err)
	}
	if len(raw) > 0 {
		state.StateJSON = append([]byte(nil), raw...)
	}
	return &state, nil
}

func (s *CockroachStore) DeleteState(ctx context.Context, sessionID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM canvas_state WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete canvas state: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *CockroachStore) AppendEvent(ctx context.Context, event *Event) error {
	if event == nil || event.SessionID == "" {
		return ErrNotFound
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO canvas_events (id, session_id, type, payload_json, created_at)
		VALUES ($1,$2,$3,$4,$5)
	`,
		event.ID,
		event.SessionID,
		event.Type,
		json.RawMessage(event.Payload),
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("append canvas event: %w", err)
	}
	return nil
}

func (s *CockroachStore) ListEvents(ctx context.Context, sessionID string, opts EventListOptions) ([]*Event, error) {
	query := `SELECT id, session_id, type, payload_json, created_at FROM canvas_events WHERE session_id = $1`
	args := []any{sessionID}
	if !opts.Since.IsZero() {
		query += fmt.Sprintf(" AND created_at >= $%d", len(args)+1)
		args = append(args, opts.Since)
	}
	query += " ORDER BY created_at ASC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list canvas events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var event Event
		var raw []byte
		if err := rows.Scan(&event.ID, &event.SessionID, &event.Type, &raw, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan canvas event: %w", err)
		}
		if len(raw) > 0 {
			event.Payload = append([]byte(nil), raw...)
		}
		events = append(events, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list canvas events: %w", err)
	}
	return events, nil
}

func (s *CockroachStore) DeleteEvents(ctx context.Context, sessionID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM canvas_events WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete canvas events: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

func nullString(value string) sql.NullString {
	if strings.TrimSpace(value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	pqErr, ok := err.(*pq.Error)
	if ok && pqErr.Code == "23505" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate")
}
