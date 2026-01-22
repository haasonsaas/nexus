package managers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/internal/tools/sandbox/firecracker"
)

// ToolingManager manages tools, MCP servers, and related infrastructure.
// It handles tool registration, policy enforcement, and tool lifecycle.
type ToolingManager struct {
	mu     sync.RWMutex
	config *config.Config
	logger *slog.Logger

	// Tool infrastructure
	browserPool        *browser.Pool
	firecrackerBackend *firecracker.Backend
	mcpManager         *mcp.Manager
	policyResolver     *policy.Resolver
	hooksRegistry      *hooks.Registry

	// Lifecycle
	started bool
}

// ToolingManagerConfig holds configuration for ToolingManager.
type ToolingManagerConfig struct {
	Config         *config.Config
	Logger         *slog.Logger
	MCPManager     *mcp.Manager
	PolicyResolver *policy.Resolver
	HooksRegistry  *hooks.Registry
}

// NewToolingManager creates a new ToolingManager.
func NewToolingManager(cfg ToolingManagerConfig) *ToolingManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Create defaults if not provided
	policyResolver := cfg.PolicyResolver
	if policyResolver == nil {
		policyResolver = policy.NewResolver()
	}

	hooksRegistry := cfg.HooksRegistry
	if hooksRegistry == nil {
		hooksRegistry = hooks.NewRegistry(logger)
	}

	return &ToolingManager{
		config:         cfg.Config,
		logger:         logger.With("component", "tooling-manager"),
		mcpManager:     cfg.MCPManager,
		policyResolver: policyResolver,
		hooksRegistry:  hooksRegistry,
	}
}

// Start initializes and starts tool infrastructure.
func (m *ToolingManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	// Start MCP manager if configured
	if m.mcpManager != nil && m.config.MCP.Enabled {
		if err := m.mcpManager.Start(ctx); err != nil {
			return fmt.Errorf("start MCP manager: %w", err)
		}
		m.logger.Info("MCP manager started")
	}

	// Initialize browser pool if enabled
	if m.config.Tools.Browser.Enabled {
		pool, err := browser.NewPool(browser.PoolConfig{
			Headless: m.config.Tools.Browser.Headless,
		})
		if err != nil {
			return fmt.Errorf("create browser pool: %w", err)
		}
		m.browserPool = pool
		m.logger.Info("browser pool initialized")
	}

	// Initialize firecracker backend if configured
	if m.config.Tools.Sandbox.Enabled && m.config.Tools.Sandbox.Backend == "firecracker" {
		if err := m.initFirecrackerBackend(ctx); err != nil {
			m.logger.Warn("firecracker backend unavailable", "error", err)
			// Continue without firecracker - will fall back to docker
		}
	}

	m.started = true
	m.logger.Info("tooling manager started")
	return nil
}

// Stop gracefully shuts down tool infrastructure.
func (m *ToolingManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	var errs []error

	// Close browser pool
	if m.browserPool != nil {
		if err := m.browserPool.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close browser pool: %w", err))
		}
	}

	// Close firecracker backend
	if m.firecrackerBackend != nil {
		if err := m.firecrackerBackend.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close firecracker backend: %w", err))
		}
	}

	// Stop MCP manager
	if m.mcpManager != nil {
		if err := m.mcpManager.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("stop MCP manager: %w", err))
		}
	}

	m.started = false
	m.logger.Info("tooling manager stopped")

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// RegisterTools registers all enabled tools with the given runtime.
func (m *ToolingManager) RegisterTools(ctx context.Context, runtime *agent.Runtime) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Register browser tool
	if m.browserPool != nil {
		runtime.RegisterTool(browser.NewBrowserTool(m.browserPool))
		m.logger.Debug("registered browser tool")
	}

	// Register MCP tools
	if m.mcpManager != nil && m.config.MCP.Enabled {
		mcp.RegisterToolsWithRegistrar(runtime, m.mcpManager, m.policyResolver)
		m.logger.Debug("registered MCP tools")
	}

	return nil
}

// BrowserPool returns the browser pool.
func (m *ToolingManager) BrowserPool() *browser.Pool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.browserPool
}

// FirecrackerBackend returns the firecracker backend.
func (m *ToolingManager) FirecrackerBackend() *firecracker.Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.firecrackerBackend
}

// MCPManager returns the MCP manager.
func (m *ToolingManager) MCPManager() *mcp.Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mcpManager
}

// PolicyResolver returns the tool policy resolver.
func (m *ToolingManager) PolicyResolver() *policy.Resolver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.policyResolver
}

// HooksRegistry returns the hooks registry.
func (m *ToolingManager) HooksRegistry() *hooks.Registry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hooksRegistry
}

// initFirecrackerBackend initializes the firecracker backend.
func (m *ToolingManager) initFirecrackerBackend(ctx context.Context) error {
	fcConfig := firecracker.DefaultBackendConfig()
	fcConfig.NetworkEnabled = m.config.Tools.Sandbox.NetworkEnabled

	if m.config.Tools.Sandbox.PoolSize > 0 {
		fcConfig.PoolConfig.InitialSize = m.config.Tools.Sandbox.PoolSize
	}
	if m.config.Tools.Sandbox.MaxPoolSize > 0 {
		fcConfig.PoolConfig.MaxSize = m.config.Tools.Sandbox.MaxPoolSize
	}
	if m.config.Tools.Sandbox.Limits.MaxCPU > 0 {
		vcpus := int64((m.config.Tools.Sandbox.Limits.MaxCPU + 999) / 1000)
		if vcpus < 1 {
			vcpus = 1
		}
		fcConfig.DefaultVCPUs = vcpus
		fcConfig.PoolConfig.DefaultVCPUs = vcpus
	}

	backend, err := firecracker.NewBackend(fcConfig)
	if err != nil {
		return fmt.Errorf("create backend: %w", err)
	}

	if err := backend.Start(ctx); err != nil {
		_ = backend.Close()
		return fmt.Errorf("start backend: %w", err)
	}

	m.firecrackerBackend = backend
	m.logger.Info("firecracker backend initialized")
	return nil
}

// TriggerHook triggers a hook event.
func (m *ToolingManager) TriggerHook(ctx context.Context, event *hooks.Event) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.hooksRegistry == nil {
		return nil
	}
	return m.hooksRegistry.Trigger(ctx, event)
}

// TriggerHookAsync triggers a hook event asynchronously.
func (m *ToolingManager) TriggerHookAsync(ctx context.Context, event *hooks.Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.hooksRegistry != nil {
		m.hooksRegistry.TriggerAsync(ctx, event)
	}
}
