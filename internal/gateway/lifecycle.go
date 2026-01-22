// Package gateway provides the main Nexus gateway server.
//
// lifecycle.go contains server lifecycle management including startup, shutdown,
// and background task management (task scheduler, job pruning, hooks).
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/internal/tasks"
)

// Start begins serving requests and starts all background services.
// This method blocks until the gRPC server stops or encounters an error.
func (s *Server) Start(ctx context.Context) error {
	s.startTime = time.Now()
	if s.mcpManager != nil {
		if err := s.mcpManager.Start(ctx); err != nil {
			return fmt.Errorf("failed to start MCP manager: %w", err)
		}
	}
	// Start channel adapters
	if err := s.channels.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start channels: %w", err)
	}
	if s.cronScheduler != nil {
		if err := s.cronScheduler.Start(ctx); err != nil {
			return fmt.Errorf("failed to start cron scheduler: %w", err)
		}
	}

	// Start task scheduler if enabled
	if err := s.startTaskScheduler(ctx); err != nil {
		return fmt.Errorf("failed to start task scheduler: %w", err)
	}

	// Start message processing
	s.startProcessing(ctx)

	// Start job pruning background task
	s.startJobPruning(ctx)

	// Trigger gateway:startup hook
	startupEvent := hooks.NewEvent(hooks.EventGatewayStartup, "").
		WithContext("workspace", s.config.Workspace.Path).
		WithContext("host", s.config.Server.Host).
		WithContext("grpc_port", s.config.Server.GRPCPort)
	s.hooksRegistry.TriggerAsync(ctx, startupEvent)

	// Start gRPC server
	return s.startGRPCServer()
}

// startGRPCServer starts the gRPC server on the configured address.
func (s *Server) startGRPCServer() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.logger.Info("starting gRPC server", "addr", addr)
	return s.grpc.Serve(lis)
}

// Stop gracefully shuts down the server and all background services.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("stopping server")

	// Trigger gateway:shutdown hook
	shutdownEvent := hooks.NewEvent(hooks.EventGatewayShutdown, "").
		WithContext("uptime", time.Since(s.startTime).String())
	if err := s.hooksRegistry.Trigger(ctx, shutdownEvent); err != nil {
		s.logger.Warn("shutdown hook error", "error", err)
	}

	if s.cancel != nil {
		s.cancel()
	}

	// Cancel background discovery goroutines
	if s.startupCancel != nil {
		s.startupCancel()
	}

	// Stop accepting new connections
	s.grpc.GracefulStop()

	// Stop channel adapters
	if err := s.channels.StopAll(ctx); err != nil {
		s.logger.Error("error stopping channels", "error", err)
	}

	if err := s.waitForProcessing(ctx); err != nil {
		return err
	}

	if s.browserPool != nil {
		if err := s.browserPool.Close(); err != nil {
			s.logger.Error("error closing browser pool", "error", err)
		}
	}

	if closer, ok := s.sessions.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			s.logger.Error("error closing session store", "error", err)
		}
	}

	if s.vectorMemory != nil {
		if err := s.vectorMemory.Close(); err != nil {
			s.logger.Error("error closing vector memory", "error", err)
		}
	}
	if s.skillsManager != nil {
		if err := s.skillsManager.Close(); err != nil {
			s.logger.Error("error closing skills manager", "error", err)
		}
	}
	if s.cronScheduler != nil {
		if err := s.cronScheduler.Stop(ctx); err != nil {
			s.logger.Error("error stopping cron scheduler", "error", err)
		}
	}
	if s.taskScheduler != nil {
		if err := s.taskScheduler.Stop(ctx); err != nil {
			s.logger.Error("error stopping task scheduler", "error", err)
		}
	}
	if closer, ok := s.taskStore.(tasks.Closer); ok {
		if err := closer.Close(); err != nil {
			s.logger.Error("error closing task store", "error", err)
		}
	}
	if s.mcpManager != nil {
		if err := s.mcpManager.Stop(); err != nil {
			s.logger.Error("error stopping MCP manager", "error", err)
		}
	}
	if s.firecrackerBackend != nil {
		if err := s.firecrackerBackend.Close(); err != nil {
			s.logger.Error("error closing firecracker backend", "error", err)
		}
	}
	if err := s.stores.Close(); err != nil {
		s.logger.Error("error closing storage stores", "error", err)
	}
	if closer, ok := s.jobStore.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			s.logger.Error("error closing job store", "error", err)
		}
	}

	return nil
}

// startTaskScheduler initializes and starts the task scheduler if enabled.
func (s *Server) startTaskScheduler(ctx context.Context) error {
	if s.taskStore == nil || !s.config.Tasks.Enabled {
		return nil
	}

	// Ensure runtime is available (needed for task execution)
	runtime, err := s.ensureRuntime(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize runtime for task scheduler: %w", err)
	}

	// Create the executor that uses the agent runtime
	executor := tasks.NewAgentExecutor(runtime, s.sessions, tasks.AgentExecutorConfig{
		Logger: s.logger.With("component", "task-executor"),
	})

	// Build scheduler config from settings
	schedulerCfg := tasks.DefaultSchedulerConfig()
	if s.config.Tasks.WorkerID != "" {
		schedulerCfg.WorkerID = s.config.Tasks.WorkerID
	}
	if s.config.Tasks.PollInterval > 0 {
		schedulerCfg.PollInterval = s.config.Tasks.PollInterval
	}
	if s.config.Tasks.AcquireInterval > 0 {
		schedulerCfg.AcquireInterval = s.config.Tasks.AcquireInterval
	}
	if s.config.Tasks.LockDuration > 0 {
		schedulerCfg.LockDuration = s.config.Tasks.LockDuration
	}
	if s.config.Tasks.MaxConcurrency > 0 {
		schedulerCfg.MaxConcurrency = s.config.Tasks.MaxConcurrency
	}
	if s.config.Tasks.CleanupInterval > 0 {
		schedulerCfg.CleanupInterval = s.config.Tasks.CleanupInterval
	}
	if s.config.Tasks.StaleTimeout > 0 {
		schedulerCfg.StaleTimeout = s.config.Tasks.StaleTimeout
	}
	schedulerCfg.Logger = s.logger.With("component", "task-scheduler")

	// Create and start the scheduler
	s.taskScheduler = tasks.NewScheduler(s.taskStore, executor, schedulerCfg)

	if err := s.taskScheduler.Start(ctx); err != nil {
		return fmt.Errorf("task scheduler start: %w", err)
	}

	s.logger.Info("task scheduler started",
		"worker_id", s.taskScheduler.WorkerID(),
		"max_concurrency", schedulerCfg.MaxConcurrency,
	)

	return nil
}

// startJobPruning starts a background goroutine that prunes old jobs.
func (s *Server) startJobPruning(ctx context.Context) {
	if s.jobStore == nil {
		return
	}
	retention := s.config.Tools.Jobs.Retention
	interval := s.config.Tools.Jobs.PruneInterval
	if retention <= 0 || interval <= 0 {
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruned, err := s.jobStore.Prune(ctx, retention)
				if err != nil {
					s.logger.Error("job pruning failed", "error", err)
				} else if pruned > 0 {
					s.logger.Info("pruned old jobs", "count", pruned)
				}
			}
		}
	}()
}

// createHookHandler creates a handler function for a discovered hook.
// The hook's content (from HOOK.md) is logged when triggered.
func createHookHandler(h *hooks.HookEntry, logger *slog.Logger) hooks.Handler {
	return func(ctx context.Context, event *hooks.Event) error {
		logger.Debug("hook triggered",
			"hook", h.Config.Name,
			"event_type", event.Type,
			"event_action", event.Action,
		)
		// For now, hooks are informational - they log when triggered.
		// Future: Execute hook scripts, send notifications, etc.
		return nil
	}
}
