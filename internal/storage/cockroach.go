package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/pkg/models"
)

// NewCockroachStoresFromDSN creates Cockroach-backed stores using a DSN.
func NewCockroachStoresFromDSN(dsn string, config *CockroachConfig) (StoreSet, error) {
	if strings.TrimSpace(dsn) == "" {
		return StoreSet{}, fmt.Errorf("dsn is required")
	}
	if config == nil {
		config = DefaultCockroachConfig()
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return StoreSet{}, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return StoreSet{}, fmt.Errorf("ping database: %w", err)
	}

	stores := StoreSet{
		Agents:   &cockroachAgentStore{db: db},
		Channels: &cockroachChannelConnectionStore{db: db},
		Users:    &cockroachUserStore{db: db},
		closer:   db.Close,
	}
	return stores, nil
}

type cockroachAgentStore struct {
	db *sql.DB
}

func (s *cockroachAgentStore) Create(ctx context.Context, agent *models.Agent) error {
	if agent == nil || agent.ID == "" {
		return fmt.Errorf("agent is required")
	}
	cfg, err := json.Marshal(agent.Config)
	if err != nil {
		return fmt.Errorf("marshal agent config: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO agents (id, user_id, name, system_prompt, model, provider, tools, config, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		agent.ID,
		agent.UserID,
		agent.Name,
		agent.SystemPrompt,
		agent.Model,
		agent.Provider,
		pq.Array(agent.Tools),
		cfg,
		agent.CreatedAt,
		agent.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			return ErrAlreadyExists
		}
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

func (s *cockroachAgentStore) Get(ctx context.Context, id string) (*models.Agent, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, system_prompt, model, provider, tools, config, created_at, updated_at
		 FROM agents WHERE id = $1`, id)

	var agent models.Agent
	var tools []string
	var configBytes []byte
	if err := row.Scan(
		&agent.ID,
		&agent.UserID,
		&agent.Name,
		&agent.SystemPrompt,
		&agent.Model,
		&agent.Provider,
		pq.Array(&tools),
		&configBytes,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get agent: %w", err)
	}
	agent.Tools = tools
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &agent.Config); err != nil {
			return nil, fmt.Errorf("unmarshal agent config: %w", err)
		}
	}
	return &agent, nil
}

func (s *cockroachAgentStore) List(ctx context.Context, userID string, limit, offset int) ([]*models.Agent, int, error) {
	args := []any{}
	hasUserFilter := userID != ""
	if hasUserFilter {
		args = append(args, userID)
	}

	countQuery := "SELECT count(*) FROM agents"
	if hasUserFilter {
		countQuery = "SELECT count(*) FROM agents WHERE user_id = $1"
	}
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count agents: %w", err)
	}

	argsList := append([]any{}, args...)
	limitClause := ""
	if limit > 0 {
		argsList = append(argsList, limit)
		limitClause = fmt.Sprintf(" LIMIT $%d", len(argsList))
	}
	if offset > 0 {
		argsList = append(argsList, offset)
		limitClause += fmt.Sprintf(" OFFSET $%d", len(argsList))
	}

	var queryBuilder strings.Builder
	queryBuilder.WriteString(`SELECT id, user_id, name, system_prompt, model, provider, tools, config, created_at, updated_at
		FROM agents`)
	if hasUserFilter {
		queryBuilder.WriteString(" WHERE user_id = $1")
	}
	queryBuilder.WriteString(" ORDER BY created_at DESC")
	queryBuilder.WriteString(limitClause)
	query := queryBuilder.String()

	rows, err := s.db.QueryContext(ctx, query, argsList...)
	if err != nil {
		return nil, 0, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	agents := []*models.Agent{}
	for rows.Next() {
		var agent models.Agent
		var tools []string
		var configBytes []byte
		if err := rows.Scan(
			&agent.ID,
			&agent.UserID,
			&agent.Name,
			&agent.SystemPrompt,
			&agent.Model,
			&agent.Provider,
			pq.Array(&tools),
			&configBytes,
			&agent.CreatedAt,
			&agent.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan agent: %w", err)
		}
		agent.Tools = tools
		if len(configBytes) > 0 {
			if err := json.Unmarshal(configBytes, &agent.Config); err != nil {
				return nil, 0, fmt.Errorf("unmarshal agent config: %w", err)
			}
		}
		agents = append(agents, &agent)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list agents: %w", err)
	}
	return agents, total, nil
}

func (s *cockroachAgentStore) Update(ctx context.Context, agent *models.Agent) error {
	if agent == nil || agent.ID == "" {
		return fmt.Errorf("agent is required")
	}
	cfg, err := json.Marshal(agent.Config)
	if err != nil {
		return fmt.Errorf("marshal agent config: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE agents
		 SET name = $1, system_prompt = $2, model = $3, provider = $4, tools = $5, config = $6, updated_at = $7
		 WHERE id = $8`,
		agent.Name,
		agent.SystemPrompt,
		agent.Model,
		agent.Provider,
		pq.Array(agent.Tools),
		cfg,
		agent.UpdatedAt,
		agent.ID,
	)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update agent rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *cockroachAgentStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete agent rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

type cockroachChannelConnectionStore struct {
	db *sql.DB
}

func (s *cockroachChannelConnectionStore) Create(ctx context.Context, conn *models.ChannelConnection) error {
	if conn == nil || conn.ID == "" {
		return fmt.Errorf("connection is required")
	}
	cfg, err := json.Marshal(conn.Config)
	if err != nil {
		return fmt.Errorf("marshal connection config: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO channel_connections (id, user_id, channel_type, channel_id, status, config, connected_at, last_activity_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		conn.ID,
		conn.UserID,
		string(conn.ChannelType),
		conn.ChannelID,
		string(conn.Status),
		cfg,
		conn.ConnectedAt,
		conn.LastActivityAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			return ErrAlreadyExists
		}
		return fmt.Errorf("create channel connection: %w", err)
	}
	return nil
}

func (s *cockroachChannelConnectionStore) Get(ctx context.Context, id string) (*models.ChannelConnection, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, channel_type, channel_id, status, config, connected_at, last_activity_at
		 FROM channel_connections WHERE id = $1`, id)
	var conn models.ChannelConnection
	var channelType string
	var status string
	var configBytes []byte
	if err := row.Scan(
		&conn.ID,
		&conn.UserID,
		&channelType,
		&conn.ChannelID,
		&status,
		&configBytes,
		&conn.ConnectedAt,
		&conn.LastActivityAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get channel connection: %w", err)
	}
	conn.ChannelType = models.ChannelType(channelType)
	conn.Status = models.ConnectionStatus(status)
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &conn.Config); err != nil {
			return nil, fmt.Errorf("unmarshal channel config: %w", err)
		}
	}
	return &conn, nil
}

func (s *cockroachChannelConnectionStore) List(ctx context.Context, userID string, limit, offset int) ([]*models.ChannelConnection, int, error) {
	args := []any{}
	hasUserFilter := userID != ""
	if hasUserFilter {
		args = append(args, userID)
	}

	countQuery := "SELECT count(*) FROM channel_connections"
	if hasUserFilter {
		countQuery = "SELECT count(*) FROM channel_connections WHERE user_id = $1"
	}
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count channel connections: %w", err)
	}

	argsList := append([]any{}, args...)
	limitClause := ""
	if limit > 0 {
		argsList = append(argsList, limit)
		limitClause = fmt.Sprintf(" LIMIT $%d", len(argsList))
	}
	if offset > 0 {
		argsList = append(argsList, offset)
		limitClause += fmt.Sprintf(" OFFSET $%d", len(argsList))
	}

	var queryBuilder strings.Builder
	queryBuilder.WriteString(`SELECT id, user_id, channel_type, channel_id, status, config, connected_at, last_activity_at
		FROM channel_connections`)
	if hasUserFilter {
		queryBuilder.WriteString(" WHERE user_id = $1")
	}
	queryBuilder.WriteString(" ORDER BY connected_at DESC")
	queryBuilder.WriteString(limitClause)
	query := queryBuilder.String()

	rows, err := s.db.QueryContext(ctx, query, argsList...)
	if err != nil {
		return nil, 0, fmt.Errorf("list channel connections: %w", err)
	}
	defer rows.Close()

	connections := []*models.ChannelConnection{}
	for rows.Next() {
		var conn models.ChannelConnection
		var channelType string
		var status string
		var configBytes []byte
		if err := rows.Scan(
			&conn.ID,
			&conn.UserID,
			&channelType,
			&conn.ChannelID,
			&status,
			&configBytes,
			&conn.ConnectedAt,
			&conn.LastActivityAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan channel connection: %w", err)
		}
		conn.ChannelType = models.ChannelType(channelType)
		conn.Status = models.ConnectionStatus(status)
		if len(configBytes) > 0 {
			if err := json.Unmarshal(configBytes, &conn.Config); err != nil {
				return nil, 0, fmt.Errorf("unmarshal channel config: %w", err)
			}
		}
		connections = append(connections, &conn)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list channel connections: %w", err)
	}
	return connections, total, nil
}

func (s *cockroachChannelConnectionStore) Update(ctx context.Context, conn *models.ChannelConnection) error {
	if conn == nil || conn.ID == "" {
		return fmt.Errorf("connection is required")
	}
	cfg, err := json.Marshal(conn.Config)
	if err != nil {
		return fmt.Errorf("marshal channel config: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE channel_connections
		 SET user_id = $1, channel_type = $2, channel_id = $3, status = $4, config = $5, connected_at = $6, last_activity_at = $7
		 WHERE id = $8`,
		conn.UserID,
		string(conn.ChannelType),
		conn.ChannelID,
		string(conn.Status),
		cfg,
		conn.ConnectedAt,
		conn.LastActivityAt,
		conn.ID,
	)
	if err != nil {
		return fmt.Errorf("update channel connection: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update channel connection rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *cockroachChannelConnectionStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM channel_connections WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete channel connection: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete channel connection rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

type cockroachUserStore struct {
	db *sql.DB
}

func (s *cockroachUserStore) FindOrCreate(ctx context.Context, info *auth.UserInfo) (*models.User, error) {
	if info == nil {
		return nil, fmt.Errorf("user info is required")
	}
	provider := normalizeProvider(info.Provider)
	providerID := strings.TrimSpace(info.ID)
	email := strings.TrimSpace(info.Email)

	// First attempt: try to find existing user
	if user, err := s.findExisting(ctx, provider, providerID, email); err != nil {
		return nil, err
	} else if user != nil {
		return s.updateFromInfo(ctx, user, info, provider, providerID)
	}

	// Try to insert new user
	user := &models.User{
		ID:         uuid.NewString(),
		Email:      email,
		Name:       info.Name,
		AvatarURL:  info.AvatarURL,
		Provider:   provider,
		ProviderID: providerID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := s.insert(ctx, user); err != nil {
		// Handle race condition: if insert fails due to duplicate,
		// another request created the user between our lookup and insert.
		// Retry the lookup to find the concurrently-created user.
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
			if existing, retryErr := s.findExisting(ctx, provider, providerID, email); retryErr != nil {
				return nil, retryErr
			} else if existing != nil {
				return s.updateFromInfo(ctx, existing, info, provider, providerID)
			}
			// User still not found after conflict - unexpected state
			return nil, fmt.Errorf("user conflict but not found on retry: %w", err)
		}
		return nil, err
	}
	return user, nil
}

// findExisting looks up a user by provider+providerID or email.
func (s *cockroachUserStore) findExisting(ctx context.Context, provider, providerID, email string) (*models.User, error) {
	if provider != "" && providerID != "" {
		if user, err := s.getByProvider(ctx, provider, providerID); err != nil {
			return nil, err
		} else if user != nil {
			return user, nil
		}
	}
	if email != "" {
		if user, err := s.getByEmail(ctx, email); err != nil {
			return nil, err
		} else if user != nil {
			return user, nil
		}
	}
	return nil, nil
}

func (s *cockroachUserStore) Get(ctx context.Context, id string) (*models.User, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, name, avatar_url, provider, provider_id, created_at, updated_at
		 FROM users WHERE id = $1`, id)
	var user models.User
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.AvatarURL,
		&user.Provider,
		&user.ProviderID,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &user, nil
}

func (s *cockroachUserStore) getByProvider(ctx context.Context, provider, providerID string) (*models.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, name, avatar_url, provider, provider_id, created_at, updated_at
		 FROM users WHERE provider = $1 AND provider_id = $2`, provider, providerID)
	var user models.User
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.AvatarURL,
		&user.Provider,
		&user.ProviderID,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by provider: %w", err)
	}
	return &user, nil
}

func (s *cockroachUserStore) getByEmail(ctx context.Context, email string) (*models.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, name, avatar_url, provider, provider_id, created_at, updated_at
		 FROM users WHERE lower(email) = lower($1)`, email)
	var user models.User
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.AvatarURL,
		&user.Provider,
		&user.ProviderID,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &user, nil
}

func (s *cockroachUserStore) insert(ctx context.Context, user *models.User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, name, avatar_url, provider, provider_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		user.ID,
		user.Email,
		user.Name,
		user.AvatarURL,
		user.Provider,
		user.ProviderID,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (s *cockroachUserStore) updateFromInfo(ctx context.Context, user *models.User, info *auth.UserInfo, provider, providerID string) (*models.User, error) {
	if info.Email != "" {
		user.Email = strings.TrimSpace(info.Email)
	}
	if info.Name != "" {
		user.Name = info.Name
	}
	if info.AvatarURL != "" {
		user.AvatarURL = info.AvatarURL
	}
	if provider != "" && providerID != "" {
		user.Provider = provider
		user.ProviderID = providerID
	}
	user.UpdatedAt = time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET email = $1, name = $2, avatar_url = $3, provider = $4, provider_id = $5, updated_at = $6
		 WHERE id = $7`,
		user.Email,
		user.Name,
		user.AvatarURL,
		user.Provider,
		user.ProviderID,
		user.UpdatedAt,
		user.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("update user rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrNotFound
	}
	return user, nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
