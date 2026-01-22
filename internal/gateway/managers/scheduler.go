package managers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tasks"
)

// SchedulerManager manages scheduled tasks, cron jobs, and background job processing.
type SchedulerManager struct {
	mu     sync.RWMutex
	config *config.Config
	logger *slog.Logger

	// Schedulers
	cronScheduler *cron.Scheduler
	taskScheduler *tasks.Scheduler

	// Stores
	taskStore tasks.Store
	jobStore  jobs.Store

	// Dependencies (injected)
	runtime  *agent.Runtime
	sessions sessions.Store

	// Background tasks
	wg     sync.WaitGroup
	cancel context.CancelFunc

	// Lifecycle
	started bool
}

// SchedulerManagerConfig holds configuration for SchedulerManager.
type SchedulerManagerConfig struct {
	Config    *config.Config
	Logger    *slog.Logger
	TaskStore tasks.Store
	JobStore  jobs.Store
}

// NewSchedulerManager creates a new SchedulerManager.
func NewSchedulerManager(cfg SchedulerManagerConfig) (*SchedulerManager, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	m := &SchedulerManager{
		config:    cfg.Config,
		logger:    logger.With("component", "scheduler-manager"),
		taskStore: cfg.TaskStore,
		jobStore:  cfg.JobStore,
	}

	// Initialize cron scheduler if enabled
	if cfg.Config.Cron.Enabled {
		cronSched, err := cron.NewScheduler(cfg.Config.Cron, cron.WithLogger(logger))
		if err != nil {
			return nil, fmt.Errorf("create cron scheduler: %w", err)
		}
		m.cronScheduler = cronSched
	}

	return m, nil
}

// SetRuntime sets the agent runtime for task execution.
// Must be called before Start if tasks are enabled.
func (m *SchedulerManager) SetRuntime(runtime *agent.Runtime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtime = runtime
}

// SetSessions sets the session store for task execution.
// Must be called before Start if tasks are enabled.
func (m *SchedulerManager) SetSessions(sessions sessions.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = sessions
}

// Start initializes and starts all schedulers and background tasks.
func (m *SchedulerManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	// Create cancellable context for background tasks
	bgCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	// Start cron scheduler
	if m.cronScheduler != nil {
		if err := m.cronScheduler.Start(ctx); err != nil {
			cancel()
			return fmt.Errorf("start cron scheduler: %w", err)
		}
		m.logger.Info("cron scheduler started")
	}

	// Start task scheduler if enabled and configured
	if err := m.startTaskScheduler(ctx); err != nil {
		cancel()
		if m.cronScheduler != nil {
			_ = m.cronScheduler.Stop(ctx) //nolint:errcheck // best-effort cleanup
		}
		return fmt.Errorf("start task scheduler: %w", err)
	}

	// Start job pruning background task
	m.startJobPruning(bgCtx)

	m.started = true
	m.logger.Info("scheduler manager started")
	return nil
}

// Stop gracefully shuts down all schedulers and background tasks.
func (m *SchedulerManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	// Cancel background tasks
	if m.cancel != nil {
		m.cancel()
	}

	// Wait for background tasks to complete
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		m.logger.Warn("timeout waiting for background tasks to complete")
	}

	var errs []error

	// Stop task scheduler
	if m.taskScheduler != nil {
		if err := m.taskScheduler.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop task scheduler: %w", err))
		}
	}

	// Close task store
	if closer, ok := m.taskStore.(tasks.Closer); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close task store: %w", err))
		}
	}

	// Stop cron scheduler
	if m.cronScheduler != nil {
		if err := m.cronScheduler.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop cron scheduler: %w", err))
		}
	}

	// Close job store
	if closer, ok := m.jobStore.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close job store: %w", err))
		}
	}

	m.started = false
	m.logger.Info("scheduler manager stopped")

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// CronScheduler returns the cron scheduler.
func (m *SchedulerManager) CronScheduler() *cron.Scheduler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cronScheduler
}

// TaskScheduler returns the task scheduler.
func (m *SchedulerManager) TaskScheduler() *tasks.Scheduler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskScheduler
}

// TaskStore returns the task store.
func (m *SchedulerManager) TaskStore() tasks.Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskStore
}

// JobStore returns the job store.
func (m *SchedulerManager) JobStore() jobs.Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobStore
}

// startTaskScheduler initializes and starts the task scheduler if enabled.
func (m *SchedulerManager) startTaskScheduler(ctx context.Context) error {
	if m.taskStore == nil || !m.config.Tasks.Enabled {
		return nil
	}

	if m.runtime == nil {
		return fmt.Errorf("runtime not set (call SetRuntime before Start)")
	}

	// Create the executor that uses the agent runtime
	executor := tasks.NewAgentExecutor(m.runtime, m.sessions, tasks.AgentExecutorConfig{
		Logger: m.logger.With("component", "task-executor"),
	})

	// Build scheduler config from settings
	schedulerCfg := tasks.DefaultSchedulerConfig()
	if m.config.Tasks.WorkerID != "" {
		schedulerCfg.WorkerID = m.config.Tasks.WorkerID
	}
	if m.config.Tasks.PollInterval > 0 {
		schedulerCfg.PollInterval = m.config.Tasks.PollInterval
	}
	if m.config.Tasks.AcquireInterval > 0 {
		schedulerCfg.AcquireInterval = m.config.Tasks.AcquireInterval
	}
	if m.config.Tasks.LockDuration > 0 {
		schedulerCfg.LockDuration = m.config.Tasks.LockDuration
	}
	if m.config.Tasks.MaxConcurrency > 0 {
		schedulerCfg.MaxConcurrency = m.config.Tasks.MaxConcurrency
	}
	if m.config.Tasks.CleanupInterval > 0 {
		schedulerCfg.CleanupInterval = m.config.Tasks.CleanupInterval
	}
	if m.config.Tasks.StaleTimeout > 0 {
		schedulerCfg.StaleTimeout = m.config.Tasks.StaleTimeout
	}
	schedulerCfg.Logger = m.logger.With("component", "task-scheduler")

	// Create and start the scheduler
	m.taskScheduler = tasks.NewScheduler(m.taskStore, executor, schedulerCfg)

	if err := m.taskScheduler.Start(ctx); err != nil {
		return fmt.Errorf("task scheduler start: %w", err)
	}

	m.logger.Info("task scheduler started",
		"worker_id", m.taskScheduler.WorkerID(),
		"max_concurrency", schedulerCfg.MaxConcurrency,
	)

	return nil
}

// startJobPruning starts a background goroutine that prunes old jobs.
func (m *SchedulerManager) startJobPruning(ctx context.Context) {
	if m.jobStore == nil {
		return
	}
	retention := m.config.Tools.Jobs.Retention
	interval := m.config.Tools.Jobs.PruneInterval
	if retention <= 0 || interval <= 0 {
		return
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruned, err := m.jobStore.Prune(ctx, retention)
				if err != nil {
					m.logger.Error("job pruning failed", "error", err)
				} else if pruned > 0 {
					m.logger.Info("pruned old jobs", "count", pruned)
				}
			}
		}
	}()
}
