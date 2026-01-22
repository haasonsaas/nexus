package tasks

import (
	"context"
	"time"
)

// Store defines the interface for task persistence.
type Store interface {
	// Task CRUD operations

	// CreateTask creates a new scheduled task.
	CreateTask(ctx context.Context, task *ScheduledTask) error

	// GetTask retrieves a task by ID.
	GetTask(ctx context.Context, id string) (*ScheduledTask, error)

	// UpdateTask updates an existing task.
	UpdateTask(ctx context.Context, task *ScheduledTask) error

	// DeleteTask deletes a task by ID.
	DeleteTask(ctx context.Context, id string) error

	// ListTasks returns tasks with optional filtering.
	ListTasks(ctx context.Context, opts ListTasksOptions) ([]*ScheduledTask, error)

	// Execution operations

	// CreateExecution creates a new task execution record.
	CreateExecution(ctx context.Context, exec *TaskExecution) error

	// GetExecution retrieves an execution by ID.
	GetExecution(ctx context.Context, id string) (*TaskExecution, error)

	// UpdateExecution updates an execution record.
	UpdateExecution(ctx context.Context, exec *TaskExecution) error

	// ListExecutions returns executions for a task.
	ListExecutions(ctx context.Context, taskID string, opts ListExecutionsOptions) ([]*TaskExecution, error)

	// Scheduling operations

	// GetDueTasks returns tasks due for execution.
	// This should only return tasks where NextRunAt <= now and Status is active.
	GetDueTasks(ctx context.Context, now time.Time, limit int) ([]*ScheduledTask, error)

	// AcquireExecution attempts to acquire a lock on a pending execution.
	// Uses SELECT FOR UPDATE SKIP LOCKED for distributed locking.
	// Returns the execution if acquired, nil if not available.
	AcquireExecution(ctx context.Context, workerID string, lockDuration time.Duration) (*TaskExecution, error)

	// ReleaseExecution releases the lock on an execution.
	ReleaseExecution(ctx context.Context, executionID string) error

	// CompleteExecution marks an execution as complete with the given status.
	CompleteExecution(ctx context.Context, executionID string, status ExecutionStatus, response string, err string) error

	// GetRunningExecutions returns executions currently running for a task.
	// Used to check for overlap when AllowOverlap is false.
	GetRunningExecutions(ctx context.Context, taskID string) ([]*TaskExecution, error)

	// CleanupStaleExecutions finds executions that have been running longer
	// than the specified timeout and marks them as timed out.
	CleanupStaleExecutions(ctx context.Context, timeout time.Duration) (int, error)
}

// ListTasksOptions configures task listing.
type ListTasksOptions struct {
	// Status filters by task status.
	Status *TaskStatus

	// AgentID filters by agent.
	AgentID string

	// Limit is the maximum number of tasks to return.
	Limit int

	// Offset for pagination.
	Offset int

	// IncludeDisabled includes disabled tasks (default false).
	IncludeDisabled bool
}

// ListExecutionsOptions configures execution listing.
type ListExecutionsOptions struct {
	// Status filters by execution status.
	Status *ExecutionStatus

	// Limit is the maximum number of executions to return.
	Limit int

	// Offset for pagination.
	Offset int

	// Since filters to executions after this time.
	Since *time.Time

	// Until filters to executions before this time.
	Until *time.Time
}

// Closer is implemented by stores that need cleanup.
type Closer interface {
	Close() error
}
