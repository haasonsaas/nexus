// Package gateway provides the main Nexus gateway server.
//
// managed_server.go provides a managed server configuration that uses
// the component managers for cleaner lifecycle management.
package gateway

import (
	"context"
	"log/slog"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/infra"
)

// ManagedServer wraps a Server with component managers for cleaner lifecycle.
type ManagedServer struct {
	*Server

	components       *infra.ComponentManager
	toolManager      *ToolManager
	schedulerManager *SchedulerManager
	mediaManager     *MediaManager
}

// ManagedServerConfig configures a ManagedServer.
type ManagedServerConfig struct {
	Config *config.Config
	Logger *slog.Logger
	// ConfigPath is the path to the loaded config file.
	ConfigPath string
}

// NewManagedServer creates a new managed server with component managers.
func NewManagedServer(cfg ManagedServerConfig) (*ManagedServer, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Create the base server
	server, err := NewServer(cfg.Config, logger)
	if err != nil {
		return nil, err
	}
	server.configPath = cfg.ConfigPath

	// Create component manager
	components := infra.NewComponentManager(logger)

	// Create tool manager
	toolManager := NewToolManager(ToolManagerConfig{
		Config:         cfg.Config,
		MCPManager:     server.mcpManager,
		PolicyResolver: server.toolPolicyResolver,
		JobStore:       server.jobStore,
		Logger:         logger.With("component", "tool-manager"),
	})

	// Create scheduler manager
	schedulerManager := NewSchedulerManager(SchedulerManagerConfig{
		Config:    cfg.Config,
		TaskStore: server.taskStore,
		Channels:  server.channels,
		Logger:    logger.With("component", "scheduler-manager"),
	})

	// Create media manager
	mediaManager := NewMediaManager(MediaManagerConfig{
		Config: cfg.Config,
		Logger: logger.With("component", "media-manager"),
	})

	// Register components in order of dependency
	components.Register(mediaManager)
	components.Register(toolManager)
	components.Register(schedulerManager)

	return &ManagedServer{
		Server:           server,
		components:       components,
		toolManager:      toolManager,
		schedulerManager: schedulerManager,
		mediaManager:     mediaManager,
	}, nil
}

// Start starts the managed server and all component managers.
func (m *ManagedServer) Start(ctx context.Context) error {
	// Start component managers first
	if err := m.components.Start(ctx); err != nil {
		return err
	}

	// If tool manager has a browser pool or firecracker backend, update server references
	if pool := m.toolManager.GetBrowserPool(); pool != nil {
		m.Server.browserPool = pool
	}
	if fcBackend := m.toolManager.GetFirecrackerBackend(); fcBackend != nil {
		m.Server.firecrackerBackend = fcBackend
	}

	// If media manager has processor/aggregator, update server references
	if processor := m.mediaManager.GetProcessor(); processor != nil {
		m.Server.mediaProcessor = processor
	}
	if aggregator := m.mediaManager.GetAggregator(); aggregator != nil {
		m.Server.mediaAggregator = aggregator
	}

	// If scheduler manager has cron/task scheduler, update server references
	if cron := m.schedulerManager.GetCronScheduler(); cron != nil {
		m.Server.cronScheduler = cron
	}
	if task := m.schedulerManager.GetTaskScheduler(); task != nil {
		m.Server.taskScheduler = task
	}

	// Start the base server
	return m.Server.Start(ctx)
}

// Stop stops the managed server and all component managers.
func (m *ManagedServer) Stop(ctx context.Context) error {
	// Stop the base server first
	if err := m.Server.Stop(ctx); err != nil {
		m.Server.logger.Error("error stopping server", "error", err)
	}

	// Stop component managers in reverse order
	return m.components.Stop(ctx)
}

// Health returns aggregated health status from all components.
func (m *ManagedServer) Health(ctx context.Context) map[string]infra.ComponentHealth {
	return m.components.Health(ctx)
}

// ToolManager returns the tool manager.
func (m *ManagedServer) ToolManager() *ToolManager {
	return m.toolManager
}

// SchedulerManager returns the scheduler manager.
func (m *ManagedServer) SchedulerManager() *SchedulerManager {
	return m.schedulerManager
}

// MediaManager returns the media manager.
func (m *ManagedServer) MediaManager() *MediaManager {
	return m.mediaManager
}

// RegisterToolsWithRuntime registers all managed tools with the runtime.
// This should be called after the runtime is initialized.
func (m *ManagedServer) RegisterToolsWithRuntime(ctx context.Context) error {
	runtime, err := m.Server.ensureRuntime(ctx)
	if err != nil {
		return err
	}

	return m.toolManager.RegisterTools(ctx, runtime)
}

// StartTaskScheduler starts the task scheduler with the runtime.
// This should be called after the runtime is initialized.
func (m *ManagedServer) StartTaskScheduler(ctx context.Context) error {
	runtime, err := m.Server.ensureRuntime(ctx)
	if err != nil {
		return err
	}

	return m.schedulerManager.StartTaskScheduler(ctx, runtime, m.Server.sessions)
}
