package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/haasonsaas/nexus/pkg/models"
)

// CockroachConfig holds configuration for CockroachDB connection.
type CockroachConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	ConnectTimeout  time.Duration
}

// DefaultCockroachConfig returns default configuration.
func DefaultCockroachConfig() *CockroachConfig {
	return &CockroachConfig{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
		ConnectTimeout:  10 * time.Second,
	}
}

// CockroachStore implements Store using CockroachDB.
type CockroachStore struct {
	db *sql.DB
}

// NewCockroachStoreFromDSN creates a new Cockroach-backed job store.
func NewCockroachStoreFromDSN(dsn string, config *CockroachConfig) (*CockroachStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("dsn is required")
	}
	if config == nil {
		config = DefaultCockroachConfig()
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

// Close releases database resources.
func (s *CockroachStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Create stores a job.
func (s *CockroachStore) Create(ctx context.Context, job *Job) error {
	if job == nil {
		return nil
	}
	resultJSON, err := marshalResult(job.Result)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tool_jobs (id, tool_name, tool_call_id, status, created_at, started_at, finished_at, result, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`,
		job.ID,
		job.ToolName,
		job.ToolCallID,
		string(job.Status),
		job.CreatedAt,
		nullTime(job.StartedAt),
		nullTime(job.FinishedAt),
		resultJSON,
		nullableString(job.Error),
	)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// Update updates a job record.
func (s *CockroachStore) Update(ctx context.Context, job *Job) error {
	if job == nil {
		return nil
	}
	resultJSON, err := marshalResult(job.Result)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE tool_jobs
		SET tool_name = $2,
			tool_call_id = $3,
			status = $4,
			created_at = $5,
			started_at = $6,
			finished_at = $7,
			result = $8,
			error_message = $9
		WHERE id = $1
	`,
		job.ID,
		job.ToolName,
		job.ToolCallID,
		string(job.Status),
		job.CreatedAt,
		nullTime(job.StartedAt),
		nullTime(job.FinishedAt),
		resultJSON,
		nullableString(job.Error),
	)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	return nil
}

// Get returns a job by id.
func (s *CockroachStore) Get(ctx context.Context, id string) (*Job, error) {
	if id == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tool_name, tool_call_id, status, created_at, started_at, finished_at, result, error_message
		FROM tool_jobs WHERE id = $1
	`, id)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

// List returns jobs in reverse chronological order.
func (s *CockroachStore) List(ctx context.Context, limit, offset int) ([]*Job, error) {
	query := `
		SELECT id, tool_name, tool_call_id, status, created_at, started_at, finished_at, result, error_message
		FROM tool_jobs
		ORDER BY created_at DESC`
	args := []any{}
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return jobs, nil
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(scanner jobScanner) (*Job, error) {
	var (
		job          Job
		status       string
		startedAt    sql.NullTime
		finishedAt   sql.NullTime
		resultBytes  []byte
		errorMessage sql.NullString
	)
	if err := scanner.Scan(
		&job.ID,
		&job.ToolName,
		&job.ToolCallID,
		&status,
		&job.CreatedAt,
		&startedAt,
		&finishedAt,
		&resultBytes,
		&errorMessage,
	); err != nil {
		return nil, err
	}
	job.Status = Status(status)
	if startedAt.Valid {
		job.StartedAt = startedAt.Time
	}
	if finishedAt.Valid {
		job.FinishedAt = finishedAt.Time
	}
	if len(resultBytes) > 0 {
		var result models.ToolResult
		if err := json.Unmarshal(resultBytes, &result); err != nil {
			return nil, fmt.Errorf("unmarshal job result: %w", err)
		}
		job.Result = &result
	}
	if errorMessage.Valid {
		job.Error = errorMessage.String
	}
	return &job, nil
}

func marshalResult(result *models.ToolResult) ([]byte, error) {
	if result == nil {
		return nil, nil
	}
	return json.Marshal(result)
}

func nullableString(value string) sql.NullString {
	if value == "" {
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
