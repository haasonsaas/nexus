// Package tasks implements scheduled task execution for Nexus agents.
//
// Tasks enable cron-based triggers for agent workflows, supporting:
//   - Cron expressions for flexible scheduling
//   - Distributed locking for multi-instance deployments
//   - Agent runtime integration for task execution
//   - Execution history and status tracking
package tasks

import (
	"encoding/json"
	"time"
)

// TaskStatus represents the current state of a scheduled task.
type TaskStatus string

const (
	// TaskStatusActive indicates the task is active and will be scheduled.
	TaskStatusActive TaskStatus = "active"

	// TaskStatusPaused indicates the task is paused and will not run.
	TaskStatusPaused TaskStatus = "paused"

	// TaskStatusDisabled indicates the task is disabled.
	TaskStatusDisabled TaskStatus = "disabled"
)

// ExecutionStatus represents the state of a task execution.
type ExecutionStatus string

const (
	// ExecutionStatusPending indicates the execution is waiting to start.
	ExecutionStatusPending ExecutionStatus = "pending"

	// ExecutionStatusRunning indicates the execution is in progress.
	ExecutionStatusRunning ExecutionStatus = "running"

	// ExecutionStatusSucceeded indicates the execution completed successfully.
	ExecutionStatusSucceeded ExecutionStatus = "succeeded"

	// ExecutionStatusFailed indicates the execution failed.
	ExecutionStatusFailed ExecutionStatus = "failed"

	// ExecutionStatusTimedOut indicates the execution exceeded its timeout.
	ExecutionStatusTimedOut ExecutionStatus = "timed_out"

	// ExecutionStatusCancelled indicates the execution was cancelled.
	ExecutionStatusCancelled ExecutionStatus = "cancelled"
)

// ScheduledTask defines a task that runs on a schedule.
type ScheduledTask struct {
	// ID is the unique identifier for the task.
	ID string `json:"id"`

	// Name is a human-readable name for the task.
	Name string `json:"name"`

	// Description provides details about what the task does.
	Description string `json:"description,omitempty"`

	// AgentID identifies which agent executes this task.
	AgentID string `json:"agent_id"`

	// Schedule is the cron expression or interval for execution.
	// Supports standard cron (5-field) and extended cron (6-field with seconds).
	Schedule string `json:"schedule"`

	// Timezone for schedule interpretation (e.g., "America/New_York").
	// Defaults to UTC if empty.
	Timezone string `json:"timezone,omitempty"`

	// Prompt is the message sent to the agent when the task executes.
	Prompt string `json:"prompt"`

	// Config holds additional task configuration.
	Config TaskConfig `json:"config"`

	// Status is the current status of the task.
	Status TaskStatus `json:"status"`

	// NextRunAt is the next scheduled execution time.
	NextRunAt time.Time `json:"next_run_at"`

	// LastRunAt is the last execution time.
	LastRunAt *time.Time `json:"last_run_at,omitempty"`

	// LastExecutionID references the most recent execution.
	LastExecutionID string `json:"last_execution_id,omitempty"`

	// CreatedAt is when the task was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the task was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// Metadata holds arbitrary task metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TaskConfig holds configuration options for a scheduled task.
type TaskConfig struct {
	// Timeout is the maximum duration for task execution.
	// Defaults to 5 minutes if not set.
	Timeout time.Duration `json:"timeout,omitempty"`

	// MaxRetries is the number of retry attempts on failure.
	MaxRetries int `json:"max_retries,omitempty"`

	// RetryDelay is the delay between retry attempts.
	RetryDelay time.Duration `json:"retry_delay,omitempty"`

	// Overlap controls whether overlapping executions are allowed.
	// If false (default), a new execution won't start if one is already running.
	AllowOverlap bool `json:"allow_overlap,omitempty"`

	// Channel specifies the channel context for execution (optional).
	Channel string `json:"channel,omitempty"`

	// ChannelID specifies the channel ID for execution (optional).
	ChannelID string `json:"channel_id,omitempty"`

	// SessionID specifies a fixed session for execution (optional).
	// If empty, a new session is created per execution.
	SessionID string `json:"session_id,omitempty"`

	// SystemPrompt overrides the default system prompt for this task.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Model overrides the default model for this task.
	Model string `json:"model,omitempty"`
}

// TaskExecution represents a single execution of a scheduled task.
type TaskExecution struct {
	// ID is the unique identifier for this execution.
	ID string `json:"id"`

	// TaskID references the parent task.
	TaskID string `json:"task_id"`

	// Status is the current execution status.
	Status ExecutionStatus `json:"status"`

	// ScheduledAt is when this execution was scheduled to run.
	ScheduledAt time.Time `json:"scheduled_at"`

	// StartedAt is when execution actually started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// FinishedAt is when execution completed.
	FinishedAt *time.Time `json:"finished_at,omitempty"`

	// SessionID is the agent session used for this execution.
	SessionID string `json:"session_id,omitempty"`

	// Prompt is the prompt that was sent to the agent.
	Prompt string `json:"prompt"`

	// Response is the agent's response.
	Response string `json:"response,omitempty"`

	// Error contains error details if the execution failed.
	Error string `json:"error,omitempty"`

	// AttemptNumber is the retry attempt (1-based).
	AttemptNumber int `json:"attempt_number"`

	// WorkerID identifies which scheduler instance ran this execution.
	WorkerID string `json:"worker_id,omitempty"`

	// LockedAt is when the execution was locked by a worker.
	LockedAt *time.Time `json:"locked_at,omitempty"`

	// LockedUntil is when the lock expires.
	LockedUntil *time.Time `json:"locked_until,omitempty"`

	// Duration is the execution duration.
	Duration time.Duration `json:"duration,omitempty"`

	// Metadata holds arbitrary execution metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// IsTerminal returns true if the execution is in a terminal state.
func (e *TaskExecution) IsTerminal() bool {
	switch e.Status {
	case ExecutionStatusSucceeded, ExecutionStatusFailed, ExecutionStatusTimedOut, ExecutionStatusCancelled:
		return true
	default:
		return false
	}
}

// MarshalConfig marshals TaskConfig to JSON.
func (c TaskConfig) MarshalConfig() ([]byte, error) {
	return json.Marshal(c)
}

// UnmarshalConfig unmarshals JSON to TaskConfig.
func UnmarshalConfig(data []byte) (TaskConfig, error) {
	var c TaskConfig
	if len(data) == 0 {
		return c, nil
	}
	err := json.Unmarshal(data, &c)
	return c, err
}

// DefaultTaskConfig returns a TaskConfig with sensible defaults.
func DefaultTaskConfig() TaskConfig {
	return TaskConfig{
		Timeout:      5 * time.Minute,
		MaxRetries:   0,
		RetryDelay:   30 * time.Second,
		AllowOverlap: false,
	}
}
