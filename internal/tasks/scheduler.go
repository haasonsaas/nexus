package tasks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// cronParser supports both standard (5-field) and extended (6-field with seconds) cron expressions.
var cronParser = cron.NewParser(
	cron.SecondOptional |
		cron.Minute |
		cron.Hour |
		cron.Dom |
		cron.Month |
		cron.Dow |
		cron.Descriptor,
)

// SchedulerConfig configures the task scheduler.
type SchedulerConfig struct {
	// WorkerID uniquely identifies this scheduler instance.
	// Used for distributed locking. Defaults to a UUID.
	WorkerID string

	// PollInterval is how often the scheduler checks for due tasks.
	// Defaults to 10 seconds.
	PollInterval time.Duration

	// AcquireInterval is how often the scheduler tries to acquire pending executions.
	// Defaults to 1 second.
	AcquireInterval time.Duration

	// LockDuration is how long an execution lock is held.
	// Should be longer than the maximum expected execution time.
	// Defaults to 10 minutes.
	LockDuration time.Duration

	// MaxConcurrency is the maximum number of concurrent task executions.
	// Defaults to 5.
	MaxConcurrency int

	// CleanupInterval is how often stale executions are cleaned up.
	// Defaults to 1 minute.
	CleanupInterval time.Duration

	// StaleTimeout is how long an execution can run before being marked stale.
	// Defaults to 30 minutes.
	StaleTimeout time.Duration

	// Logger for scheduler events.
	Logger *slog.Logger
}

// DefaultSchedulerConfig returns a SchedulerConfig with sensible defaults.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		WorkerID:        uuid.NewString(),
		PollInterval:    10 * time.Second,
		AcquireInterval: 1 * time.Second,
		LockDuration:    10 * time.Minute,
		MaxConcurrency:  5,
		CleanupInterval: 1 * time.Minute,
		StaleTimeout:    30 * time.Minute,
	}
}

// Scheduler manages task scheduling and execution coordination.
type Scheduler struct {
	store    Store
	executor Executor
	config   SchedulerConfig
	logger   *slog.Logger

	// Concurrency control
	sem    chan struct{}
	wg     sync.WaitGroup
	cancel context.CancelFunc

	// State
	mu      sync.RWMutex
	running bool
}

// Executor defines the interface for task execution.
type Executor interface {
	// Execute runs a task and returns the response and any error.
	Execute(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (response string, err error)
}

// NewScheduler creates a new task scheduler.
func NewScheduler(store Store, executor Executor, config SchedulerConfig) *Scheduler {
	if config.WorkerID == "" {
		config.WorkerID = uuid.NewString()
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 10 * time.Second
	}
	if config.AcquireInterval <= 0 {
		config.AcquireInterval = 1 * time.Second
	}
	if config.LockDuration <= 0 {
		config.LockDuration = 10 * time.Minute
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 5
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}
	if config.StaleTimeout <= 0 {
		config.StaleTimeout = 30 * time.Minute
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default().With("component", "task-scheduler")
	}

	return &Scheduler{
		store:    store,
		executor: executor,
		config:   config,
		logger:   logger,
		sem:      make(chan struct{}, config.MaxConcurrency),
	}
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.logger.Info("starting task scheduler",
		"worker_id", s.config.WorkerID,
		"poll_interval", s.config.PollInterval,
		"max_concurrency", s.config.MaxConcurrency,
	)

	// Start poll loop for due tasks
	s.wg.Add(1)
	go s.pollLoop(ctx)

	// Start acquire loop for pending executions
	s.wg.Add(1)
	go s.acquireLoop(ctx)

	// Start cleanup loop for stale executions
	s.wg.Add(1)
	go s.cleanupLoop(ctx)

	return nil
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.logger.Info("stopping task scheduler", "worker_id", s.config.WorkerID)

	if s.cancel != nil {
		s.cancel()
	}

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("task scheduler stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pollLoop checks for due tasks and creates pending executions.
func (s *Scheduler) pollLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	// Run immediately on start
	s.pollDueTasks(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollDueTasks(ctx)
		}
	}
}

// pollDueTasks finds tasks due for execution and creates pending executions.
func (s *Scheduler) pollDueTasks(ctx context.Context) {
	now := time.Now()

	tasks, err := s.store.GetDueTasks(ctx, now, 100)
	if err != nil {
		s.logger.Error("failed to get due tasks", "error", err)
		return
	}

	for _, task := range tasks {
		if err := s.scheduleTask(ctx, task, now); err != nil {
			s.logger.Error("failed to schedule task",
				"task_id", task.ID,
				"task_name", task.Name,
				"error", err,
			)
		}
	}
}

// scheduleTask creates a pending execution for a due task.
func (s *Scheduler) scheduleTask(ctx context.Context, task *ScheduledTask, now time.Time) error {
	// Check for overlap if not allowed
	if !task.Config.AllowOverlap {
		running, err := s.store.GetRunningExecutions(ctx, task.ID)
		if err != nil {
			return fmt.Errorf("check running executions: %w", err)
		}
		if len(running) > 0 {
			s.logger.Debug("skipping task due to running execution",
				"task_id", task.ID,
				"running_executions", len(running),
			)
			// Update next run time even if we skip
			return s.updateNextRun(ctx, task, now)
		}
	}

	// Create pending execution
	exec := &TaskExecution{
		ID:            uuid.NewString(),
		TaskID:        task.ID,
		Status:        ExecutionStatusPending,
		ScheduledAt:   task.NextRunAt,
		Prompt:        task.Prompt,
		AttemptNumber: 1,
	}

	if err := s.store.CreateExecution(ctx, exec); err != nil {
		return fmt.Errorf("create execution: %w", err)
	}

	s.logger.Info("scheduled task execution",
		"task_id", task.ID,
		"task_name", task.Name,
		"execution_id", exec.ID,
	)

	// Update task's next run time
	return s.updateNextRun(ctx, task, now)
}

// updateNextRun calculates and updates the task's next run time.
func (s *Scheduler) updateNextRun(ctx context.Context, task *ScheduledTask, lastRun time.Time) error {
	nextRun, err := s.calculateNextRun(task.Schedule, task.Timezone, lastRun)
	if err != nil {
		// Disable the task if schedule is invalid
		s.logger.Error("invalid schedule, disabling task",
			"task_id", task.ID,
			"schedule", task.Schedule,
			"error", err,
		)
		task.Status = TaskStatusDisabled
		task.UpdatedAt = time.Now()
		return s.store.UpdateTask(ctx, task)
	}

	// Zero time means no more runs (one-shot schedule completed)
	if nextRun.IsZero() {
		s.logger.Info("one-shot task completed, disabling",
			"task_id", task.ID,
			"task_name", task.Name,
		)
		task.Status = TaskStatusDisabled
		task.LastRunAt = &lastRun
		task.UpdatedAt = time.Now()
		return s.store.UpdateTask(ctx, task)
	}

	task.NextRunAt = nextRun
	task.LastRunAt = &lastRun
	task.UpdatedAt = time.Now()

	return s.store.UpdateTask(ctx, task)
}

// calculateNextRun computes the next execution time for a schedule.
// Returns zero time for one-shot schedules (e.g., "@at <timestamp>").
func (s *Scheduler) calculateNextRun(schedule string, timezone string, after time.Time) (time.Time, error) {
	// Handle one-shot schedules - these should only run once
	if strings.HasPrefix(schedule, "@at ") || strings.HasPrefix(schedule, "@once") {
		// Return zero time to indicate no more runs
		return time.Time{}, nil
	}

	sched, err := cronParser.Parse(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse schedule: %w", err)
	}

	loc := time.UTC
	if timezone != "" {
		var err error
		loc, err = time.LoadLocation(timezone)
		if err != nil {
			s.logger.Warn("invalid timezone, using UTC",
				"timezone", timezone,
				"error", err,
			)
			loc = time.UTC
		}
	}

	return sched.Next(after.In(loc)), nil
}

// acquireLoop continuously tries to acquire and execute pending executions.
func (s *Scheduler) acquireLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.AcquireInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tryAcquireExecution(ctx)
		}
	}
}

// tryAcquireExecution attempts to acquire and execute a pending execution.
func (s *Scheduler) tryAcquireExecution(ctx context.Context) {
	// Check if we have capacity
	select {
	case s.sem <- struct{}{}:
		// Acquired semaphore
	default:
		// At max concurrency, skip this cycle
		return
	}

	// Try to acquire an execution
	exec, err := s.store.AcquireExecution(ctx, s.config.WorkerID, s.config.LockDuration)
	if err != nil {
		<-s.sem // Release semaphore
		s.logger.Error("failed to acquire execution", "error", err)
		return
	}

	if exec == nil {
		<-s.sem // Release semaphore, no work available
		return
	}

	// Execute in goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.sem }() // Release semaphore when done

		s.executeTask(ctx, exec)
	}()
}

// executeTask runs a task execution.
func (s *Scheduler) executeTask(ctx context.Context, exec *TaskExecution) {
	s.logger.Info("executing task",
		"execution_id", exec.ID,
		"task_id", exec.TaskID,
		"attempt", exec.AttemptNumber,
	)

	// Get the task
	task, err := s.store.GetTask(ctx, exec.TaskID)
	if err != nil || task == nil {
		s.completeExecution(ctx, exec, ExecutionStatusFailed, "", "task not found")
		return
	}

	// Set up execution timeout
	timeout := task.Config.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the task
	response, execErr := s.executor.Execute(execCtx, task, exec)

	// Determine status
	var status ExecutionStatus
	var errMsg string

	switch {
	case execCtx.Err() == context.DeadlineExceeded:
		status = ExecutionStatusTimedOut
		errMsg = "execution timed out"
	case execErr != nil:
		status = ExecutionStatusFailed
		errMsg = execErr.Error()
	default:
		status = ExecutionStatusSucceeded
	}

	s.completeExecution(ctx, exec, status, response, errMsg)

	// Update task with last execution
	task.LastExecutionID = exec.ID
	now := time.Now()
	task.LastRunAt = &now
	task.UpdatedAt = now
	if err := s.store.UpdateTask(ctx, task); err != nil {
		s.logger.Error("failed to update task after execution",
			"task_id", task.ID,
			"error", err,
		)
	}

	// Handle retries if failed
	if status == ExecutionStatusFailed && task.Config.MaxRetries > 0 && exec.AttemptNumber <= task.Config.MaxRetries {
		s.scheduleRetry(ctx, task, exec)
	}
}

// completeExecution marks an execution as complete.
func (s *Scheduler) completeExecution(ctx context.Context, exec *TaskExecution, status ExecutionStatus, response, errMsg string) {
	if err := s.store.CompleteExecution(ctx, exec.ID, status, response, errMsg); err != nil {
		s.logger.Error("failed to complete execution",
			"execution_id", exec.ID,
			"error", err,
		)
		return
	}

	s.logger.Info("completed task execution",
		"execution_id", exec.ID,
		"task_id", exec.TaskID,
		"status", status,
	)
}

// scheduleRetry creates a new execution for retry.
func (s *Scheduler) scheduleRetry(ctx context.Context, task *ScheduledTask, failedExec *TaskExecution) {
	delay := task.Config.RetryDelay
	if delay <= 0 {
		delay = 30 * time.Second
	}

	retryExec := &TaskExecution{
		ID:            uuid.NewString(),
		TaskID:        task.ID,
		Status:        ExecutionStatusPending,
		ScheduledAt:   time.Now().Add(delay),
		Prompt:        failedExec.Prompt,
		AttemptNumber: failedExec.AttemptNumber + 1,
	}

	if err := s.store.CreateExecution(ctx, retryExec); err != nil {
		s.logger.Error("failed to schedule retry",
			"task_id", task.ID,
			"attempt", retryExec.AttemptNumber,
			"error", err,
		)
		return
	}

	s.logger.Info("scheduled retry",
		"task_id", task.ID,
		"execution_id", retryExec.ID,
		"attempt", retryExec.AttemptNumber,
		"delay", delay,
	)
}

// cleanupLoop periodically cleans up stale executions.
func (s *Scheduler) cleanupLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupStaleExecutions(ctx)
		}
	}
}

// cleanupStaleExecutions marks long-running executions as timed out.
func (s *Scheduler) cleanupStaleExecutions(ctx context.Context) {
	count, err := s.store.CleanupStaleExecutions(ctx, s.config.StaleTimeout)
	if err != nil {
		s.logger.Error("failed to cleanup stale executions", "error", err)
		return
	}

	if count > 0 {
		s.logger.Warn("cleaned up stale executions", "count", count)
	}
}

// IsRunning returns whether the scheduler is currently running.
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// WorkerID returns this scheduler's worker ID.
func (s *Scheduler) WorkerID() string {
	return s.config.WorkerID
}
