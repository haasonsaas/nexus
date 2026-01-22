package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
	_ "github.com/lib/pq"
)

// CockroachStore implements the Store interface using CockroachDB.
type CockroachStore struct {
	db *sql.DB

	// Prepared statements for performance
	stmtCreateSession *sql.Stmt
	stmtGetSession    *sql.Stmt
	stmtUpdateSession *sql.Stmt
	stmtDeleteSession *sql.Stmt
	stmtGetByKey      *sql.Stmt
	stmtAppendMessage *sql.Stmt
	stmtGetHistory    *sql.Stmt
}

// DB exposes the underlying database connection for related stores.
func (s *CockroachStore) DB() *sql.DB {
	return s.db
}

// CockroachConfig holds configuration for CockroachDB connection.
type CockroachConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	ConnectTimeout  time.Duration
}

// DefaultCockroachConfig returns default configuration.
func DefaultCockroachConfig() *CockroachConfig {
	return &CockroachConfig{
		Host:            "localhost",
		Port:            26257,
		User:            "root",
		Password:        "",
		Database:        "nexus",
		SSLMode:         "disable",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
		ConnectTimeout:  10 * time.Second,
	}
}

// NewCockroachStore creates a new CockroachDB store.
func NewCockroachStore(config *CockroachConfig) (*CockroachStore, error) {
	if config == nil {
		config = DefaultCockroachConfig()
	}

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=%d",
		config.Host, config.Port, config.User, config.Password,
		config.Database, config.SSLMode, int(config.ConnectTimeout.Seconds()),
	)

	return newCockroachStoreWithDSN(dsn, config)
}

// NewCockroachStoreFromDSN creates a new CockroachDB store using a raw DSN/URL.
func NewCockroachStoreFromDSN(dsn string, config *CockroachConfig) (*CockroachStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("dsn is required")
	}
	if config == nil {
		config = DefaultCockroachConfig()
	}

	return newCockroachStoreWithDSN(dsn, config)
}

func newCockroachStoreWithDSN(dsn string, config *CockroachConfig) (*CockroachStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &CockroachStore{db: db}

	// Prepare statements
	if err := store.prepareStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	return store, nil
}

// prepareStatements prepares all SQL statements for reuse.
func (s *CockroachStore) prepareStatements() error {
	var err error

	s.stmtCreateSession, err = s.db.Prepare(`
		INSERT INTO sessions (id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare create session: %w", err)
	}

	s.stmtGetSession, err = s.db.Prepare(`
		SELECT id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at
		FROM sessions WHERE id = $1
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare get session: %w", err)
	}

	s.stmtUpdateSession, err = s.db.Prepare(`
		UPDATE sessions SET title = $1, metadata = $2, updated_at = $3
		WHERE id = $4
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare update session: %w", err)
	}

	s.stmtDeleteSession, err = s.db.Prepare(`
		DELETE FROM sessions WHERE id = $1
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare delete session: %w", err)
	}

	s.stmtGetByKey, err = s.db.Prepare(`
		SELECT id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at
		FROM sessions WHERE key = $1
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare get by key: %w", err)
	}

	s.stmtAppendMessage, err = s.db.Prepare(`
		INSERT INTO messages (id, session_id, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare append message: %w", err)
	}

	s.stmtGetHistory, err = s.db.Prepare(`
		SELECT id, session_id, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at
		FROM messages WHERE session_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare get history: %w", err)
	}

	return nil
}

// Close closes the database connection and prepared statements.
func (s *CockroachStore) Close() error {
	var errs []error

	if s.stmtCreateSession != nil {
		if err := s.stmtCreateSession.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.stmtGetSession != nil {
		if err := s.stmtGetSession.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.stmtUpdateSession != nil {
		if err := s.stmtUpdateSession.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.stmtDeleteSession != nil {
		if err := s.stmtDeleteSession.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.stmtGetByKey != nil {
		if err := s.stmtGetByKey.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.stmtAppendMessage != nil {
		if err := s.stmtAppendMessage.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.stmtGetHistory != nil {
		if err := s.stmtGetHistory.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if err := s.db.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing store: %v", errs)
	}

	return nil
}

// Create creates a new session.
func (s *CockroachStore) Create(ctx context.Context, session *models.Session) error {
	if session.ID == "" {
		return fmt.Errorf("session ID is required")
	}

	metadata, err := json.Marshal(session.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = s.stmtCreateSession.ExecContext(ctx,
		session.ID,
		session.AgentID,
		session.Channel,
		session.ChannelID,
		session.Key,
		session.Title,
		metadata,
		session.CreatedAt,
		session.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

// Get retrieves a session by ID.
func (s *CockroachStore) Get(ctx context.Context, id string) (*models.Session, error) {
	session := &models.Session{}
	var metadataJSON []byte

	err := s.stmtGetSession.QueryRowContext(ctx, id).Scan(
		&session.ID,
		&session.AgentID,
		&session.Channel,
		&session.ChannelID,
		&session.Key,
		&session.Title,
		&metadataJSON,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return session, nil
}

// Update updates an existing session.
func (s *CockroachStore) Update(ctx context.Context, session *models.Session) error {
	metadata, err := json.Marshal(session.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	session.UpdatedAt = time.Now()

	result, err := s.stmtUpdateSession.ExecContext(ctx,
		session.Title,
		metadata,
		session.UpdatedAt,
		session.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session not found: %s", session.ID)
	}

	return nil
}

// Delete deletes a session by ID.
func (s *CockroachStore) Delete(ctx context.Context, id string) error {
	result, err := s.stmtDeleteSession.ExecContext(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	return nil
}

// GetByKey retrieves a session by its unique key.
func (s *CockroachStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	session := &models.Session{}
	var metadataJSON []byte

	err := s.stmtGetByKey.QueryRowContext(ctx, key).Scan(
		&session.ID,
		&session.AgentID,
		&session.Channel,
		&session.ChannelID,
		&session.Key,
		&session.Title,
		&metadataJSON,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found with key: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session by key: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return session, nil
}

// GetOrCreate retrieves an existing session by key or creates a new one atomically.
// Uses INSERT ... ON CONFLICT to avoid race conditions between concurrent requests.
func (s *CockroachStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	now := time.Now()
	id := generateID()

	// Use upsert to atomically insert or return existing
	// ON CONFLICT DO UPDATE with key = key is a no-op that still returns the row
	query := `
		INSERT INTO sessions (id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, '', '{}', $6, $7)
		ON CONFLICT (key) DO UPDATE SET key = sessions.key
		RETURNING id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at
	`

	session := &models.Session{}
	var metadataJSON []byte
	err := s.db.QueryRowContext(ctx, query, id, agentID, channel, channelID, key, now, now).Scan(
		&session.ID,
		&session.AgentID,
		&session.Channel,
		&session.ChannelID,
		&session.Key,
		&session.Title,
		&metadataJSON,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create session: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return session, nil
}

// List retrieves sessions with optional filtering.
func (s *CockroachStore) List(ctx context.Context, agentID string, opts ListOptions) ([]*models.Session, error) {
	query := `
		SELECT id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at
		FROM sessions
		WHERE agent_id = $1
	`
	args := []interface{}{agentID}
	argPos := 2

	if opts.Channel != "" {
		query += fmt.Sprintf(" AND channel = $%d", argPos)
		args = append(args, opts.Channel)
		argPos++
	}

	query += " ORDER BY updated_at DESC"

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, opts.Limit)
		argPos++
	}

	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.Session
	for rows.Next() {
		session := &models.Session{}
		var metadataJSON []byte

		err := rows.Scan(
			&session.ID,
			&session.AgentID,
			&session.Channel,
			&session.ChannelID,
			&session.Key,
			&session.Title,
			&metadataJSON,
			&session.CreatedAt,
			&session.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &session.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}

		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// AppendMessage adds a message to a session's history.
// Wraps both the message insert and session timestamp update in a transaction
// to ensure atomicity.
func (s *CockroachStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	if msg.ID == "" {
		return fmt.Errorf("message ID is required")
	}

	attachmentsJSON, err := json.Marshal(msg.Attachments)
	if err != nil {
		return fmt.Errorf("failed to marshal attachments: %w", err)
	}

	toolCallsJSON, err := json.Marshal(msg.ToolCalls)
	if err != nil {
		return fmt.Errorf("failed to marshal tool calls: %w", err)
	}

	toolResultsJSON, err := json.Marshal(msg.ToolResults)
	if err != nil {
		return fmt.Errorf("failed to marshal tool results: %w", err)
	}

	metadataJSON, err := json.Marshal(msg.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Use transaction to ensure both operations succeed or fail together
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() //nolint:errcheck // Rollback after commit returns ErrTxDone which is expected
	}()

	_, err = tx.StmtContext(ctx, s.stmtAppendMessage).ExecContext(ctx,
		msg.ID,
		sessionID,
		msg.Channel,
		msg.ChannelID,
		msg.Direction,
		msg.Role,
		msg.Content,
		attachmentsJSON,
		toolCallsJSON,
		toolResultsJSON,
		metadataJSON,
		msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to append message: %w", err)
	}

	// Update session's updated_at timestamp within the same transaction
	_, err = tx.ExecContext(ctx, "UPDATE sessions SET updated_at = $1 WHERE id = $2", time.Now(), sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session timestamp: %w", err)
	}

	return tx.Commit()
}

// GetHistory retrieves message history for a session.
func (s *CockroachStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100 // Default limit
	}

	rows, err := s.stmtGetHistory.QueryContext(ctx, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get history: %w", err)
	}
	defer rows.Close()

	var messages []*models.Message
	for rows.Next() {
		msg := &models.Message{}
		var attachmentsJSON, toolCallsJSON, toolResultsJSON, metadataJSON []byte

		err := rows.Scan(
			&msg.ID,
			&msg.SessionID,
			&msg.Channel,
			&msg.ChannelID,
			&msg.Direction,
			&msg.Role,
			&msg.Content,
			&attachmentsJSON,
			&toolCallsJSON,
			&toolResultsJSON,
			&metadataJSON,
			&msg.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		if len(attachmentsJSON) > 0 && string(attachmentsJSON) != "null" {
			if err := json.Unmarshal(attachmentsJSON, &msg.Attachments); err != nil {
				return nil, fmt.Errorf("failed to unmarshal attachments: %w", err)
			}
		}

		if len(toolCallsJSON) > 0 && string(toolCallsJSON) != "null" {
			if err := json.Unmarshal(toolCallsJSON, &msg.ToolCalls); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool calls: %w", err)
			}
		}

		if len(toolResultsJSON) > 0 && string(toolResultsJSON) != "null" {
			if err := json.Unmarshal(toolResultsJSON, &msg.ToolResults); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool results: %w", err)
			}
		}

		if len(metadataJSON) > 0 && string(metadataJSON) != "null" {
			if err := json.Unmarshal(metadataJSON, &msg.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}

		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %w", err)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// generateID generates a unique UUID.
func generateID() string {
	return uuid.NewString()
}
