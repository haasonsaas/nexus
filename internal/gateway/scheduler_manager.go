// Package gateway provides the main Nexus gateway server.
//
// scheduler_manager.go provides centralized scheduling management for cron and tasks.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/infra"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tasks"
)

// SchedulerManager manages cron scheduling and task scheduling for the gateway.
type SchedulerManager struct {
	*infra.BaseComponent

	mu sync.RWMutex

	config        *config.Config
	cronScheduler *cron.Scheduler
	taskScheduler *tasks.Scheduler
	taskStore     tasks.Store
}

// SchedulerManagerConfig configures the SchedulerManager.
type SchedulerManagerConfig struct {
	Config    *config.Config
	TaskStore tasks.Store
	Logger    *slog.Logger
}

// NewSchedulerManager creates a new scheduler manager.
func NewSchedulerManager(cfg SchedulerManagerConfig) *SchedulerManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &SchedulerManager{
		BaseComponent: infra.NewBaseComponent("scheduler-manager", logger),
		config:        cfg.Config,
		taskStore:     cfg.TaskStore,
	}
}

// Start initializes and starts all schedulers.
func (m *SchedulerManager) Start(ctx context.Context) error {
	if !m.TransitionTo(infra.ComponentStateNew, infra.ComponentStateStarting) {
		if m.IsRunning() {
			return nil
		}
		return fmt.Errorf("scheduler manager cannot start from state %s", m.State())
	}

	// Start cron scheduler if enabled
	if m.config.Cron.Enabled {
		if err := m.startCronScheduler(ctx); err != nil {
			m.MarkFailed()
			return fmt.Errorf("cron scheduler: %w", err)
		}
	}

	m.MarkStarted()
	m.Logger().Info("scheduler manager started",
		"cron_enabled", m.config.Cron.Enabled,
		"tasks_enabled", m.config.Tasks.Enabled,
	)
	return nil
}

// StartTaskScheduler initializes and starts the task scheduler.
// This is separate from Start because it requires the runtime to be available.
func (m *SchedulerManager) StartTaskScheduler(ctx context.Context, runtime *agent.Runtime, sessionStore sessions.Store) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.taskStore == nil || !m.config.Tasks.Enabled {
		return nil
	}

	// Create the executor that uses the agent runtime
	executor := tasks.NewAgentExecutor(runtime, sessionStore, tasks.AgentExecutorConfig{
		Logger: m.Logger().With("component", "task-executor"),
	})

	// Build scheduler config from settings
	schedulerCfg := tasks.DefaultSchedulerConfig()
	tasksCfg := m.config.Tasks

	if tasksCfg.WorkerID != "" {
		schedulerCfg.WorkerID = tasksCfg.WorkerID
	}
	if tasksCfg.PollInterval > 0 {
		schedulerCfg.PollInterval = tasksCfg.PollInterval
	}
	if tasksCfg.AcquireInterval > 0 {
		schedulerCfg.AcquireInterval = tasksCfg.AcquireInterval
	}
	if tasksCfg.LockDuration > 0 {
		schedulerCfg.LockDuration = tasksCfg.LockDuration
	}
	if tasksCfg.MaxConcurrency > 0 {
		schedulerCfg.MaxConcurrency = tasksCfg.MaxConcurrency
	}
	if tasksCfg.CleanupInterval > 0 {
		schedulerCfg.CleanupInterval = tasksCfg.CleanupInterval
	}
	if tasksCfg.StaleTimeout > 0 {
		schedulerCfg.StaleTimeout = tasksCfg.StaleTimeout
	}
	schedulerCfg.Logger = m.Logger().With("component", "task-scheduler")

	// Create and start the scheduler
	m.taskScheduler = tasks.NewScheduler(m.taskStore, executor, schedulerCfg)

	if err := m.taskScheduler.Start(ctx); err != nil {
		return fmt.Errorf("task scheduler start: %w", err)
	}

	m.Logger().Info("task scheduler started",
		"worker_id", m.taskScheduler.WorkerID(),
		"max_concurrency", schedulerCfg.MaxConcurrency,
	)

	return nil
}

// Stop shuts down all schedulers.
func (m *SchedulerManager) Stop(ctx context.Context) error {
	if !m.TransitionTo(infra.ComponentStateRunning, infra.ComponentStateStopping) {
		if m.State() == infra.ComponentStateStopped {
			return nil
		}
		if m.State() != infra.ComponentStateFailed {
			return nil
		}
	}

	m.mu.Lock()
	cronSched := m.cronScheduler
	taskSched := m.taskScheduler
	taskSt := m.taskStore
	m.cronScheduler = nil
	m.taskScheduler = nil
	m.mu.Unlock()

	var errs []error

	if cronSched != nil {
		if err := cronSched.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("cron scheduler: %w", err))
		}
	}

	if taskSched != nil {
		if err := taskSched.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("task scheduler: %w", err))
		}
	}

	if closer, ok := taskSt.(tasks.Closer); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("task store: %w", err))
		}
	}

	m.MarkStopped()

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping scheduler manager: %v", errs)
	}

	m.Logger().Info("scheduler manager stopped")
	return nil
}

// Health returns the health status of the scheduler manager.
func (m *SchedulerManager) Health(_ context.Context) infra.ComponentHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	details := make(map[string]string)

	if m.cronScheduler != nil {
		details["cron"] = "active"
	}
	if m.taskScheduler != nil {
		details["task_scheduler"] = "active"
		details["task_worker_id"] = m.taskScheduler.WorkerID()
	}

	switch m.State() {
	case infra.ComponentStateRunning:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthHealthy,
			Message: "running",
			Details: details,
		}
	case infra.ComponentStateStopped:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthUnhealthy,
			Message: "stopped",
		}
	case infra.ComponentStateFailed:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthUnhealthy,
			Message: "failed",
		}
	default:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthUnknown,
			Message: m.State().String(),
		}
	}
}

// startCronScheduler initializes and starts the cron scheduler.
func (m *SchedulerManager) startCronScheduler(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	scheduler, err := cron.NewScheduler(m.config.Cron, cron.WithLogger(m.Logger()))
	if err != nil {
		return err
	}

	if err := scheduler.Start(ctx); err != nil {
		return err
	}

	m.cronScheduler = scheduler
	m.Logger().Info("cron scheduler started", "jobs", len(m.config.Cron.Jobs))
	return nil
}

// GetCronScheduler returns the cron scheduler if active.
func (m *SchedulerManager) GetCronScheduler() *cron.Scheduler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cronScheduler
}

// GetTaskScheduler returns the task scheduler if active.
func (m *SchedulerManager) GetTaskScheduler() *tasks.Scheduler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskScheduler
}

// GetTaskStore returns the task store.
func (m *SchedulerManager) GetTaskStore() tasks.Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskStore
}

// Ensure SchedulerManager implements FullLifecycleComponent.
var _ infra.FullLifecycleComponent = (*SchedulerManager)(nil)
