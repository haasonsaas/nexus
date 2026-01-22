package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
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

// NewCockroachStoreFromDSN creates a new CockroachDB task store.
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

// CreateTask creates a new scheduled task.
func (s *CockroachStore) CreateTask(ctx context.Context, task *ScheduledTask) error {
	if task == nil {
		return fmt.Errorf("task is required")
	}

	configJSON, err := task.Config.MarshalConfig()
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	metadataJSON, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO scheduled_tasks (
			id, name, description, agent_id, schedule, timezone,
			prompt, config, status, next_run_at, last_run_at,
			last_execution_id, created_at, updated_at, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`,
		task.ID,
		task.Name,
		nullableString(task.Description),
		task.AgentID,
		task.Schedule,
		nullableString(task.Timezone),
		task.Prompt,
		configJSON,
		string(task.Status),
		task.NextRunAt,
		nullableTime(task.LastRunAt),
		nullableString(task.LastExecutionID),
		task.CreatedAt,
		task.UpdatedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	return nil
}

// GetTask retrieves a task by ID.
func (s *CockroachStore) GetTask(ctx context.Context, id string) (*ScheduledTask, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, agent_id, schedule, timezone,
			   prompt, config, status, next_run_at, last_run_at,
			   last_execution_id, created_at, updated_at, metadata
		FROM scheduled_tasks WHERE id = $1
	`, id)

	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return task, nil
}

// UpdateTask updates an existing task.
func (s *CockroachStore) UpdateTask(ctx context.Context, task *ScheduledTask) error {
	if task == nil {
		return fmt.Errorf("task is required")
	}

	configJSON, err := task.Config.MarshalConfig()
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	metadataJSON, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	task.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx, `
		UPDATE scheduled_tasks SET
			name = $2,
			description = $3,
			agent_id = $4,
			schedule = $5,
			timezone = $6,
			prompt = $7,
			config = $8,
			status = $9,
			next_run_at = $10,
			last_run_at = $11,
			last_execution_id = $12,
			updated_at = $13,
			metadata = $14
		WHERE id = $1
	`,
		task.ID,
		task.Name,
		nullableString(task.Description),
		task.AgentID,
		task.Schedule,
		nullableString(task.Timezone),
		task.Prompt,
		configJSON,
		string(task.Status),
		task.NextRunAt,
		nullableTime(task.LastRunAt),
		nullableString(task.LastExecutionID),
		task.UpdatedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	return nil
}

// DeleteTask deletes a task by ID.
func (s *CockroachStore) DeleteTask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

// ListTasks returns tasks with optional filtering.
func (s *CockroachStore) ListTasks(ctx context.Context, opts ListTasksOptions) ([]*ScheduledTask, error) {
	query := `
		SELECT id, name, description, agent_id, schedule, timezone,
			   prompt, config, status, next_run_at, last_run_at,
			   last_execution_id, created_at, updated_at, metadata
		FROM scheduled_tasks WHERE 1=1
	`
	args := []any{}
	argPos := 1

	if opts.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, string(*opts.Status))
		argPos++
	}

	if opts.AgentID != "" {
		query += fmt.Sprintf(" AND agent_id = $%d", argPos)
		args = append(args, opts.AgentID)
		argPos++
	}

	if !opts.IncludeDisabled {
		query += fmt.Sprintf(" AND status != $%d", argPos)
		args = append(args, string(TaskStatusDisabled))
		argPos++
	}

	query += " ORDER BY next_run_at ASC"

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
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*ScheduledTask
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	return tasks, nil
}

// CreateExecution creates a new task execution record.
func (s *CockroachStore) CreateExecution(ctx context.Context, exec *TaskExecution) error {
	if exec == nil {
		return fmt.Errorf("execution is required")
	}

	metadataJSON, err := json.Marshal(exec.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO task_executions (
			id, task_id, status, scheduled_at, started_at, finished_at,
			session_id, prompt, response, error, attempt_number,
			worker_id, locked_at, locked_until, duration, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`,
		exec.ID,
		exec.TaskID,
		string(exec.Status),
		exec.ScheduledAt,
		nullableTime(exec.StartedAt),
		nullableTime(exec.FinishedAt),
		nullableString(exec.SessionID),
		exec.Prompt,
		nullableString(exec.Response),
		nullableString(exec.Error),
		exec.AttemptNumber,
		nullableString(exec.WorkerID),
		nullableTime(exec.LockedAt),
		nullableTime(exec.LockedUntil),
		exec.Duration,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("create execution: %w", err)
	}

	return nil
}

// GetExecution retrieves an execution by ID.
func (s *CockroachStore) GetExecution(ctx context.Context, id string) (*TaskExecution, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, status, scheduled_at, started_at, finished_at,
			   session_id, prompt, response, error, attempt_number,
			   worker_id, locked_at, locked_until, duration, metadata
		FROM task_executions WHERE id = $1
	`, id)

	exec, err := scanExecution(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get execution: %w", err)
	}
	return exec, nil
}

// UpdateExecution updates an execution record.
func (s *CockroachStore) UpdateExecution(ctx context.Context, exec *TaskExecution) error {
	if exec == nil {
		return fmt.Errorf("execution is required")
	}

	metadataJSON, err := json.Marshal(exec.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE task_executions SET
			status = $2,
			started_at = $3,
			finished_at = $4,
			session_id = $5,
			response = $6,
			error = $7,
			attempt_number = $8,
			worker_id = $9,
			locked_at = $10,
			locked_until = $11,
			duration = $12,
			metadata = $13
		WHERE id = $1
	`,
		exec.ID,
		string(exec.Status),
		nullableTime(exec.StartedAt),
		nullableTime(exec.FinishedAt),
		nullableString(exec.SessionID),
		nullableString(exec.Response),
		nullableString(exec.Error),
		exec.AttemptNumber,
		nullableString(exec.WorkerID),
		nullableTime(exec.LockedAt),
		nullableTime(exec.LockedUntil),
		exec.Duration,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("update execution: %w", err)
	}

	return nil
}

// ListExecutions returns executions for a task.
func (s *CockroachStore) ListExecutions(ctx context.Context, taskID string, opts ListExecutionsOptions) ([]*TaskExecution, error) {
	query := `
		SELECT id, task_id, status, scheduled_at, started_at, finished_at,
			   session_id, prompt, response, error, attempt_number,
			   worker_id, locked_at, locked_until, duration, metadata
		FROM task_executions WHERE task_id = $1
	`
	args := []any{taskID}
	argPos := 2

	if opts.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, string(*opts.Status))
		argPos++
	}

	if opts.Since != nil {
		query += fmt.Sprintf(" AND scheduled_at >= $%d", argPos)
		args = append(args, *opts.Since)
		argPos++
	}

	if opts.Until != nil {
		query += fmt.Sprintf(" AND scheduled_at <= $%d", argPos)
		args = append(args, *opts.Until)
		argPos++
	}

	query += " ORDER BY scheduled_at DESC"

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
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()

	var executions []*TaskExecution
	for rows.Next() {
		exec, err := scanExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		executions = append(executions, exec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}

	return executions, nil
}

// GetDueTasks returns tasks due for execution.
func (s *CockroachStore) GetDueTasks(ctx context.Context, now time.Time, limit int) ([]*ScheduledTask, error) {
	query := `
		SELECT id, name, description, agent_id, schedule, timezone,
			   prompt, config, status, next_run_at, last_run_at,
			   last_execution_id, created_at, updated_at, metadata
		FROM scheduled_tasks
		WHERE status = $1 AND next_run_at <= $2
		ORDER BY next_run_at ASC
	`
	args := []any{string(TaskStatusActive), now}

	if limit > 0 {
		query += " LIMIT $3"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get due tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*ScheduledTask
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get due tasks: %w", err)
	}

	return tasks, nil
}

// AcquireExecution attempts to acquire a lock on a pending execution.
// Uses SELECT FOR UPDATE SKIP LOCKED for distributed locking.
func (s *CockroachStore) AcquireExecution(ctx context.Context, workerID string, lockDuration time.Duration) (*TaskExecution, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			_ = err
		}
	}()

	now := time.Now()
	lockUntil := now.Add(lockDuration)

	// SELECT FOR UPDATE SKIP LOCKED ensures only one worker acquires each execution
	row := tx.QueryRowContext(ctx, `
		SELECT id, task_id, status, scheduled_at, started_at, finished_at,
			   session_id, prompt, response, error, attempt_number,
			   worker_id, locked_at, locked_until, duration, metadata
		FROM task_executions
		WHERE status = $1
		  AND (locked_until IS NULL OR locked_until < $2)
		ORDER BY scheduled_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`, string(ExecutionStatusPending), now)

	exec, err := scanExecution(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan execution: %w", err)
	}

	// Update the execution with lock info
	_, err = tx.ExecContext(ctx, `
		UPDATE task_executions SET
			status = $1,
			worker_id = $2,
			locked_at = $3,
			locked_until = $4,
			started_at = $5
		WHERE id = $6
	`,
		string(ExecutionStatusRunning),
		workerID,
		now,
		lockUntil,
		now,
		exec.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update execution lock: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Update the returned execution with the new values
	exec.Status = ExecutionStatusRunning
	exec.WorkerID = workerID
	exec.LockedAt = &now
	exec.LockedUntil = &lockUntil
	exec.StartedAt = &now

	return exec, nil
}

// ReleaseExecution releases the lock on an execution.
func (s *CockroachStore) ReleaseExecution(ctx context.Context, executionID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_executions SET
			locked_at = NULL,
			locked_until = NULL,
			worker_id = NULL
		WHERE id = $1
	`, executionID)
	if err != nil {
		return fmt.Errorf("release execution: %w", err)
	}
	return nil
}

// CompleteExecution marks an execution as complete with the given status.
func (s *CockroachStore) CompleteExecution(ctx context.Context, executionID string, status ExecutionStatus, response string, errMsg string) error {
	now := time.Now()

	// Get the execution to calculate duration
	exec, err := s.GetExecution(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get execution: %w", err)
	}
	if exec == nil {
		return fmt.Errorf("execution not found: %s", executionID)
	}

	var duration time.Duration
	if exec.StartedAt != nil {
		duration = now.Sub(*exec.StartedAt)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE task_executions SET
			status = $1,
			finished_at = $2,
			response = $3,
			error = $4,
			duration = $5,
			locked_at = NULL,
			locked_until = NULL
		WHERE id = $6
	`,
		string(status),
		now,
		nullableString(response),
		nullableString(errMsg),
		duration,
		executionID,
	)
	if err != nil {
		return fmt.Errorf("complete execution: %w", err)
	}

	return nil
}

// GetRunningExecutions returns executions currently running for a task.
func (s *CockroachStore) GetRunningExecutions(ctx context.Context, taskID string) ([]*TaskExecution, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, status, scheduled_at, started_at, finished_at,
			   session_id, prompt, response, error, attempt_number,
			   worker_id, locked_at, locked_until, duration, metadata
		FROM task_executions
		WHERE task_id = $1 AND status = $2
	`, taskID, string(ExecutionStatusRunning))
	if err != nil {
		return nil, fmt.Errorf("get running executions: %w", err)
	}
	defer rows.Close()

	var executions []*TaskExecution
	for rows.Next() {
		exec, err := scanExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		executions = append(executions, exec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get running executions: %w", err)
	}

	return executions, nil
}

// CleanupStaleExecutions finds executions that have been running longer
// than the specified timeout and marks them as timed out.
func (s *CockroachStore) CleanupStaleExecutions(ctx context.Context, timeout time.Duration) (int, error) {
	cutoff := time.Now().Add(-timeout)

	result, err := s.db.ExecContext(ctx, `
		UPDATE task_executions SET
			status = $1,
			finished_at = NOW(),
			error = $2
		WHERE status = $3 AND started_at < $4
	`,
		string(ExecutionStatusTimedOut),
		"execution timed out",
		string(ExecutionStatusRunning),
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup stale executions: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}

	return int(count), nil
}

// Scanner interface for both *sql.Row and *sql.Rows
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (*ScheduledTask, error) {
	var task ScheduledTask
	var (
		description     sql.NullString
		timezone        sql.NullString
		configJSON      []byte
		status          string
		lastRunAt       sql.NullTime
		lastExecutionID sql.NullString
		metadataJSON    []byte
	)

	err := s.Scan(
		&task.ID,
		&task.Name,
		&description,
		&task.AgentID,
		&task.Schedule,
		&timezone,
		&task.Prompt,
		&configJSON,
		&status,
		&task.NextRunAt,
		&lastRunAt,
		&lastExecutionID,
		&task.CreatedAt,
		&task.UpdatedAt,
		&metadataJSON,
	)
	if err != nil {
		return nil, err
	}

	task.Status = TaskStatus(status)

	if description.Valid {
		task.Description = description.String
	}
	if timezone.Valid {
		task.Timezone = timezone.String
	}
	if lastRunAt.Valid {
		task.LastRunAt = &lastRunAt.Time
	}
	if lastExecutionID.Valid {
		task.LastExecutionID = lastExecutionID.String
	}

	if len(configJSON) > 0 {
		var err error
		task.Config, err = UnmarshalConfig(configJSON)
		if err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &task.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return &task, nil
}

func scanExecution(s scanner) (*TaskExecution, error) {
	var exec TaskExecution
	var (
		status       string
		startedAt    sql.NullTime
		finishedAt   sql.NullTime
		sessionID    sql.NullString
		response     sql.NullString
		errorMsg     sql.NullString
		workerID     sql.NullString
		lockedAt     sql.NullTime
		lockedUntil  sql.NullTime
		duration     int64
		metadataJSON []byte
	)

	err := s.Scan(
		&exec.ID,
		&exec.TaskID,
		&status,
		&exec.ScheduledAt,
		&startedAt,
		&finishedAt,
		&sessionID,
		&exec.Prompt,
		&response,
		&errorMsg,
		&exec.AttemptNumber,
		&workerID,
		&lockedAt,
		&lockedUntil,
		&duration,
		&metadataJSON,
	)
	if err != nil {
		return nil, err
	}

	exec.Status = ExecutionStatus(status)
	exec.Duration = time.Duration(duration)

	if startedAt.Valid {
		exec.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		exec.FinishedAt = &finishedAt.Time
	}
	if sessionID.Valid {
		exec.SessionID = sessionID.String
	}
	if response.Valid {
		exec.Response = response.String
	}
	if errorMsg.Valid {
		exec.Error = errorMsg.String
	}
	if workerID.Valid {
		exec.WorkerID = workerID.String
	}
	if lockedAt.Valid {
		exec.LockedAt = &lockedAt.Time
	}
	if lockedUntil.Valid {
		exec.LockedUntil = &lockedUntil.Time
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &exec.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return &exec, nil
}

func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullableTime(t *time.Time) sql.NullTime {
	if t == nil || t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
