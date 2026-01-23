package tasks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDefaultSchedulerConfig(t *testing.T) {
	cfg := DefaultSchedulerConfig()

	if cfg.WorkerID == "" {
		t.Error("WorkerID should be set to a UUID")
	}
	if cfg.PollInterval != 10*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 10*time.Second)
	}
	if cfg.AcquireInterval != 1*time.Second {
		t.Errorf("AcquireInterval = %v, want %v", cfg.AcquireInterval, 1*time.Second)
	}
	if cfg.LockDuration != 10*time.Minute {
		t.Errorf("LockDuration = %v, want %v", cfg.LockDuration, 10*time.Minute)
	}
	if cfg.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency = %d, want 5", cfg.MaxConcurrency)
	}
	if cfg.CleanupInterval != 1*time.Minute {
		t.Errorf("CleanupInterval = %v, want %v", cfg.CleanupInterval, 1*time.Minute)
	}
	if cfg.StaleTimeout != 30*time.Minute {
		t.Errorf("StaleTimeout = %v, want %v", cfg.StaleTimeout, 30*time.Minute)
	}
}

// mockStore implements Store interface for testing
type mockStore struct {
	mu             sync.Mutex
	tasks          map[string]*ScheduledTask
	executions     map[string]*TaskExecution
	getDueTasksErr error
	acquireErr     error
	acquired       *TaskExecution
}

func newMockStore() *mockStore {
	return &mockStore{
		tasks:      make(map[string]*ScheduledTask),
		executions: make(map[string]*TaskExecution),
	}
}

func (m *mockStore) CreateTask(ctx context.Context, task *ScheduledTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
	return nil
}

func (m *mockStore) GetTask(ctx context.Context, id string) (*ScheduledTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id], nil
}

func (m *mockStore) UpdateTask(ctx context.Context, task *ScheduledTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
	return nil
}

func (m *mockStore) DeleteTask(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
}

func (m *mockStore) ListTasks(ctx context.Context, opts ListTasksOptions) ([]*ScheduledTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*ScheduledTask
	for _, t := range m.tasks {
		result = append(result, t)
	}
	return result, nil
}

func (m *mockStore) CreateExecution(ctx context.Context, exec *TaskExecution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executions[exec.ID] = exec
	return nil
}

func (m *mockStore) GetExecution(ctx context.Context, id string) (*TaskExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executions[id], nil
}

func (m *mockStore) UpdateExecution(ctx context.Context, exec *TaskExecution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executions[exec.ID] = exec
	return nil
}

func (m *mockStore) ListExecutions(ctx context.Context, taskID string, opts ListExecutionsOptions) ([]*TaskExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*TaskExecution
	for _, e := range m.executions {
		if e.TaskID == taskID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockStore) GetDueTasks(ctx context.Context, now time.Time, limit int) ([]*ScheduledTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getDueTasksErr != nil {
		return nil, m.getDueTasksErr
	}
	var result []*ScheduledTask
	for _, t := range m.tasks {
		if t.Status == TaskStatusActive && !t.NextRunAt.After(now) {
			result = append(result, t)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockStore) AcquireExecution(ctx context.Context, workerID string, lockDuration time.Duration) (*TaskExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acquireErr != nil {
		return nil, m.acquireErr
	}
	return m.acquired, nil
}

func (m *mockStore) ReleaseExecution(ctx context.Context, executionID string) error {
	return nil
}

func (m *mockStore) CompleteExecution(ctx context.Context, executionID string, status ExecutionStatus, response string, err string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if exec, ok := m.executions[executionID]; ok {
		exec.Status = status
		exec.Response = response
		exec.Error = err
	}
	return nil
}

func (m *mockStore) GetRunningExecutions(ctx context.Context, taskID string) ([]*TaskExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*TaskExecution
	for _, e := range m.executions {
		if e.TaskID == taskID && e.Status == ExecutionStatusRunning {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockStore) CleanupStaleExecutions(ctx context.Context, timeout time.Duration) (int, error) {
	return 0, nil
}

// mockExecutor implements Executor interface for testing
type mockExecutor struct {
	response string
	err      error
	called   bool
}

func (m *mockExecutor) Execute(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
	m.called = true
	return m.response, m.err
}

func TestNewScheduler(t *testing.T) {
	store := newMockStore()
	executor := &mockExecutor{}

	t.Run("creates scheduler with default config", func(t *testing.T) {
		s := NewScheduler(store, executor, SchedulerConfig{})
		if s == nil {
			t.Fatal("expected non-nil scheduler")
		}
		if s.config.WorkerID == "" {
			t.Error("WorkerID should be set")
		}
		if s.config.MaxConcurrency != 5 {
			t.Errorf("MaxConcurrency = %d, want 5", s.config.MaxConcurrency)
		}
	})

	t.Run("uses provided config values", func(t *testing.T) {
		cfg := SchedulerConfig{
			WorkerID:       "custom-worker",
			MaxConcurrency: 10,
			PollInterval:   30 * time.Second,
		}
		s := NewScheduler(store, executor, cfg)
		if s.config.WorkerID != "custom-worker" {
			t.Errorf("WorkerID = %q, want %q", s.config.WorkerID, "custom-worker")
		}
		if s.config.MaxConcurrency != 10 {
			t.Errorf("MaxConcurrency = %d, want 10", s.config.MaxConcurrency)
		}
	})

	t.Run("applies defaults for zero values", func(t *testing.T) {
		cfg := SchedulerConfig{
			WorkerID:        "test-worker",
			PollInterval:    0,
			AcquireInterval: 0,
			LockDuration:    0,
			MaxConcurrency:  0,
			CleanupInterval: 0,
			StaleTimeout:    0,
		}
		s := NewScheduler(store, executor, cfg)

		if s.config.PollInterval != 10*time.Second {
			t.Errorf("PollInterval = %v, want %v", s.config.PollInterval, 10*time.Second)
		}
		if s.config.AcquireInterval != 1*time.Second {
			t.Errorf("AcquireInterval = %v, want %v", s.config.AcquireInterval, 1*time.Second)
		}
		if s.config.LockDuration != 10*time.Minute {
			t.Errorf("LockDuration = %v, want %v", s.config.LockDuration, 10*time.Minute)
		}
		if s.config.MaxConcurrency != 5 {
			t.Errorf("MaxConcurrency = %d, want 5", s.config.MaxConcurrency)
		}
	})
}

func TestScheduler_StartStop(t *testing.T) {
	store := newMockStore()
	executor := &mockExecutor{}
	cfg := SchedulerConfig{
		WorkerID:        "test-worker",
		PollInterval:    100 * time.Millisecond,
		AcquireInterval: 50 * time.Millisecond,
		CleanupInterval: 100 * time.Millisecond,
	}
	s := NewScheduler(store, executor, cfg)

	ctx := context.Background()

	t.Run("starts successfully", func(t *testing.T) {
		err := s.Start(ctx)
		if err != nil {
			t.Fatalf("Start error: %v", err)
		}
		if !s.IsRunning() {
			t.Error("expected scheduler to be running")
		}
	})

	t.Run("start is idempotent", func(t *testing.T) {
		err := s.Start(ctx)
		if err != nil {
			t.Fatalf("second Start error: %v", err)
		}
	})

	t.Run("stops successfully", func(t *testing.T) {
		stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		err := s.Stop(stopCtx)
		if err != nil {
			t.Fatalf("Stop error: %v", err)
		}
		if s.IsRunning() {
			t.Error("expected scheduler to not be running")
		}
	})

	t.Run("stop is idempotent", func(t *testing.T) {
		stopCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()

		err := s.Stop(stopCtx)
		if err != nil {
			t.Fatalf("second Stop error: %v", err)
		}
	})
}

func TestScheduler_WorkerID(t *testing.T) {
	store := newMockStore()
	executor := &mockExecutor{}
	cfg := SchedulerConfig{WorkerID: "my-worker-123"}
	s := NewScheduler(store, executor, cfg)

	if s.WorkerID() != "my-worker-123" {
		t.Errorf("WorkerID() = %q, want %q", s.WorkerID(), "my-worker-123")
	}
}

func TestScheduler_IsRunning(t *testing.T) {
	store := newMockStore()
	executor := &mockExecutor{}
	s := NewScheduler(store, executor, SchedulerConfig{
		WorkerID:        "test",
		PollInterval:    100 * time.Millisecond,
		AcquireInterval: 50 * time.Millisecond,
		CleanupInterval: 100 * time.Millisecond,
	})

	if s.IsRunning() {
		t.Error("expected scheduler to not be running initially")
	}

	ctx := context.Background()
	_ = s.Start(ctx)

	if !s.IsRunning() {
		t.Error("expected scheduler to be running after Start")
	}

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = s.Stop(stopCtx)

	if s.IsRunning() {
		t.Error("expected scheduler to not be running after Stop")
	}
}

func TestScheduler_CalculateNextRun(t *testing.T) {
	store := newMockStore()
	executor := &mockExecutor{}
	s := NewScheduler(store, executor, SchedulerConfig{})

	now := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	t.Run("calculates next run for cron expression", func(t *testing.T) {
		// Every hour at minute 0
		next, err := s.calculateNextRun("0 * * * *", "", now)
		if err != nil {
			t.Fatalf("calculateNextRun error: %v", err)
		}
		expected := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Errorf("next = %v, want %v", next, expected)
		}
	})

	t.Run("returns zero for @at schedule", func(t *testing.T) {
		next, err := s.calculateNextRun("@at 2024-01-15T12:00:00Z", "", now)
		if err != nil {
			t.Fatalf("calculateNextRun error: %v", err)
		}
		if !next.IsZero() {
			t.Error("expected zero time for @at schedule")
		}
	})

	t.Run("returns zero for @once schedule", func(t *testing.T) {
		next, err := s.calculateNextRun("@once", "", now)
		if err != nil {
			t.Fatalf("calculateNextRun error: %v", err)
		}
		if !next.IsZero() {
			t.Error("expected zero time for @once schedule")
		}
	})

	t.Run("handles timezone", func(t *testing.T) {
		// Every day at 9 AM in New York
		next, err := s.calculateNextRun("0 9 * * *", "America/New_York", now)
		if err != nil {
			t.Fatalf("calculateNextRun error: %v", err)
		}
		// Should be in the future
		if !next.After(now) {
			t.Errorf("next run should be after now")
		}
	})

	t.Run("uses UTC for invalid timezone", func(t *testing.T) {
		next, err := s.calculateNextRun("0 * * * *", "Invalid/Timezone", now)
		if err != nil {
			t.Fatalf("calculateNextRun error: %v", err)
		}
		if next.IsZero() {
			t.Error("expected valid next run time")
		}
	})

	t.Run("returns error for invalid cron expression", func(t *testing.T) {
		_, err := s.calculateNextRun("invalid cron", "", now)
		if err == nil {
			t.Error("expected error for invalid cron expression")
		}
	})

	t.Run("supports extended cron with seconds", func(t *testing.T) {
		// Every 30 seconds
		next, err := s.calculateNextRun("*/30 * * * * *", "", now)
		if err != nil {
			t.Fatalf("calculateNextRun error: %v", err)
		}
		// Should be within 30 seconds
		diff := next.Sub(now)
		if diff > 30*time.Second {
			t.Errorf("next run diff = %v, want <= 30s", diff)
		}
	})
}

func TestScheduler_StopWithTimeout(t *testing.T) {
	store := newMockStore()
	executor := &mockExecutor{
		response: "done",
	}
	s := NewScheduler(store, executor, SchedulerConfig{
		WorkerID:        "test",
		PollInterval:    100 * time.Millisecond,
		AcquireInterval: 50 * time.Millisecond,
		CleanupInterval: 100 * time.Millisecond,
	})

	ctx := context.Background()
	_ = s.Start(ctx)

	// Stop with very short timeout
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
	defer cancel()

	// This might timeout or succeed depending on timing
	_ = s.Stop(stopCtx)
}

func TestListTasksOptions_Struct(t *testing.T) {
	status := TaskStatusActive
	opts := ListTasksOptions{
		Status:          &status,
		AgentID:         "agent-123",
		Limit:           10,
		Offset:          20,
		IncludeDisabled: true,
	}

	if *opts.Status != TaskStatusActive {
		t.Errorf("Status = %v, want %v", *opts.Status, TaskStatusActive)
	}
	if opts.Limit != 10 {
		t.Errorf("Limit = %d, want 10", opts.Limit)
	}
}

func TestListExecutionsOptions_Struct(t *testing.T) {
	status := ExecutionStatusSucceeded
	since := time.Now().Add(-24 * time.Hour)
	until := time.Now()

	opts := ListExecutionsOptions{
		Status: &status,
		Limit:  50,
		Offset: 100,
		Since:  &since,
		Until:  &until,
	}

	if *opts.Status != ExecutionStatusSucceeded {
		t.Errorf("Status = %v, want %v", *opts.Status, ExecutionStatusSucceeded)
	}
	if opts.Limit != 50 {
		t.Errorf("Limit = %d, want 50", opts.Limit)
	}
}

func TestScheduler_PollDueTasks(t *testing.T) {
	store := newMockStore()
	executor := &mockExecutor{}
	s := NewScheduler(store, executor, SchedulerConfig{WorkerID: "test"})

	ctx := context.Background()
	now := time.Now()

	// Add a due task
	task := &ScheduledTask{
		ID:        "task-1",
		Name:      "Test Task",
		AgentID:   "agent-1",
		Schedule:  "*/5 * * * *",
		Prompt:    "Run test",
		Status:    TaskStatusActive,
		NextRunAt: now.Add(-1 * time.Minute), // Due
		Config:    DefaultTaskConfig(),
	}
	store.CreateTask(ctx, task)

	// Poll should create an execution
	s.pollDueTasks(ctx)

	// Check that an execution was created
	store.mu.Lock()
	execCount := len(store.executions)
	store.mu.Unlock()

	if execCount != 1 {
		t.Errorf("execution count = %d, want 1", execCount)
	}
}

func TestScheduler_HandleAcquireError(t *testing.T) {
	store := newMockStore()
	store.acquireErr = errors.New("database error")
	executor := &mockExecutor{}
	s := NewScheduler(store, executor, SchedulerConfig{WorkerID: "test"})

	// tryAcquireExecution should handle error gracefully
	ctx := context.Background()
	s.tryAcquireExecution(ctx)
	// Should not panic
}

func TestScheduler_HandleGetDueTasksError(t *testing.T) {
	store := newMockStore()
	store.getDueTasksErr = errors.New("database error")
	executor := &mockExecutor{}
	s := NewScheduler(store, executor, SchedulerConfig{WorkerID: "test"})

	// pollDueTasks should handle error gracefully
	ctx := context.Background()
	s.pollDueTasks(ctx)
	// Should not panic
}
