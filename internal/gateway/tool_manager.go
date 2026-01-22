// Package gateway provides the main Nexus gateway server.
//
// tool_manager.go provides centralized tool registration and management.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/infra"
	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	jobtools "github.com/haasonsaas/nexus/internal/tools/jobs"
	"github.com/haasonsaas/nexus/internal/tools/memorysearch"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/internal/tools/sandbox"
	"github.com/haasonsaas/nexus/internal/tools/sandbox/firecracker"
	"github.com/haasonsaas/nexus/internal/tools/websearch"
)

// ToolManager manages tool registration and lifecycle for the gateway.
// It handles both native tools and MCP tools with proper namespacing.
type ToolManager struct {
	*infra.BaseComponent

	mu sync.RWMutex

	config         *config.Config
	mcpManager     *mcp.Manager
	policyResolver *policy.Resolver
	jobStore       jobs.Store

	// Managed resources
	browserPool        *browser.Pool
	firecrackerBackend *firecracker.Backend

	// Registered tools tracking
	registeredTools []string
	mcpTools        []string
}

// ToolManagerConfig configures the ToolManager.
type ToolManagerConfig struct {
	Config         *config.Config
	MCPManager     *mcp.Manager
	PolicyResolver *policy.Resolver
	JobStore       jobs.Store
	Logger         *slog.Logger
}

// NewToolManager creates a new tool manager.
func NewToolManager(cfg ToolManagerConfig) *ToolManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &ToolManager{
		BaseComponent:   infra.NewBaseComponent("tool-manager", logger),
		config:          cfg.Config,
		mcpManager:      cfg.MCPManager,
		policyResolver:  cfg.PolicyResolver,
		jobStore:        cfg.JobStore,
		registeredTools: make([]string, 0),
		mcpTools:        make([]string, 0),
	}
}

// Start initializes managed tool resources.
func (m *ToolManager) Start(ctx context.Context) error {
	if !m.TransitionTo(infra.ComponentStateNew, infra.ComponentStateStarting) {
		if m.IsRunning() {
			return nil
		}
		return fmt.Errorf("tool manager cannot start from state %s", m.State())
	}

	// Start MCP manager if configured
	if m.mcpManager != nil {
		if err := m.mcpManager.Start(ctx); err != nil {
			m.MarkFailed()
			return fmt.Errorf("failed to start MCP manager: %w", err)
		}
	}

	m.MarkStarted()
	m.Logger().Info("tool manager started")
	return nil
}

// Stop shuts down managed tool resources.
func (m *ToolManager) Stop(ctx context.Context) error {
	if !m.TransitionTo(infra.ComponentStateRunning, infra.ComponentStateStopping) {
		if m.State() == infra.ComponentStateStopped {
			return nil
		}
		if m.State() != infra.ComponentStateFailed {
			return nil
		}
	}

	m.mu.Lock()
	browserPool := m.browserPool
	fcBackend := m.firecrackerBackend
	m.browserPool = nil
	m.firecrackerBackend = nil
	m.mu.Unlock()

	var errs []error

	if browserPool != nil {
		if err := browserPool.Close(); err != nil {
			errs = append(errs, fmt.Errorf("browser pool: %w", err))
		}
	}

	if fcBackend != nil {
		if err := fcBackend.Close(); err != nil {
			errs = append(errs, fmt.Errorf("firecracker backend: %w", err))
		}
	}

	if m.mcpManager != nil {
		if err := m.mcpManager.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("MCP manager: %w", err))
		}
	}

	m.MarkStopped()

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping tool manager: %v", errs)
	}

	m.Logger().Info("tool manager stopped")
	return nil
}

// Health returns the health status of the tool manager.
func (m *ToolManager) Health(_ context.Context) infra.ComponentHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	details := make(map[string]string)
	details["registered_tools"] = fmt.Sprintf("%d", len(m.registeredTools))
	details["mcp_tools"] = fmt.Sprintf("%d", len(m.mcpTools))

	if m.browserPool != nil {
		details["browser_pool"] = "active"
	}
	if m.firecrackerBackend != nil {
		details["firecracker"] = "active"
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

// RegisterTools registers all configured tools with the runtime.
func (m *ToolManager) RegisterTools(ctx context.Context, runtime *agent.Runtime) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil || runtime == nil {
		return nil
	}

	cfg := m.config

	// Register sandbox tool
	if cfg.Tools.Sandbox.Enabled {
		if err := m.registerSandboxTool(ctx, runtime); err != nil {
			return fmt.Errorf("sandbox tool: %w", err)
		}
		m.registeredTools = append(m.registeredTools, "sandbox")
	}

	// Register browser tool
	if cfg.Tools.Browser.Enabled {
		if err := m.registerBrowserTool(runtime); err != nil {
			return fmt.Errorf("browser tool: %w", err)
		}
		m.registeredTools = append(m.registeredTools, "browser")
	}

	// Register web search tool
	if cfg.Tools.WebSearch.Enabled {
		m.registerWebSearchTool(runtime)
		m.registeredTools = append(m.registeredTools, "web_search")
	}

	// Register memory search tool
	if cfg.Tools.MemorySearch.Enabled {
		m.registerMemorySearchTool(runtime)
		m.registeredTools = append(m.registeredTools, "memory_search")
	}

	// Register job status tool
	if m.jobStore != nil {
		runtime.RegisterTool(jobtools.NewStatusTool(m.jobStore))
		m.registeredTools = append(m.registeredTools, "job_status")
	}

	// Register MCP tools
	if cfg.MCP.Enabled && m.mcpManager != nil {
		mcpTools := mcp.RegisterToolsWithRegistrar(runtime, m.mcpManager, m.policyResolver)
		m.mcpTools = mcpTools
		m.Logger().Info("registered MCP tools", "count", len(mcpTools))
	}

	m.Logger().Info("tools registered",
		"native", len(m.registeredTools),
		"mcp", len(m.mcpTools),
	)

	return nil
}

// registerSandboxTool sets up and registers the sandbox tool.
func (m *ToolManager) registerSandboxTool(ctx context.Context, runtime *agent.Runtime) error {
	cfg := m.config.Tools.Sandbox

	opts := []sandbox.Option{}
	backend := cfg.Backend

	switch backend {
	case "", "docker":
		// Default Docker backend
	case "firecracker":
		if err := m.setupFirecrackerBackend(ctx, &cfg); err != nil {
			m.Logger().Warn("firecracker backend unavailable, falling back to docker", "error", err)
		} else {
			opts = append(opts, sandbox.WithBackend(sandbox.BackendFirecracker))
		}
	default:
		return fmt.Errorf("unsupported sandbox backend %q", backend)
	}

	// Apply configuration options
	if cfg.PoolSize > 0 {
		opts = append(opts, sandbox.WithPoolSize(cfg.PoolSize))
	}
	if cfg.MaxPoolSize > 0 {
		opts = append(opts, sandbox.WithMaxPoolSize(cfg.MaxPoolSize))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, sandbox.WithDefaultTimeout(cfg.Timeout))
	}
	if cfg.Limits.MaxCPU > 0 {
		opts = append(opts, sandbox.WithDefaultCPU(cfg.Limits.MaxCPU))
	}
	if memMB, err := parseMemoryMB(cfg.Limits.MaxMemory); err == nil && memMB > 0 {
		opts = append(opts, sandbox.WithDefaultMemory(memMB))
	}
	if cfg.NetworkEnabled {
		opts = append(opts, sandbox.WithNetworkEnabled(true))
	}

	return sandbox.Register(runtime, opts...)
}

// setupFirecrackerBackend initializes the firecracker backend.
func (m *ToolManager) setupFirecrackerBackend(ctx context.Context, cfg *config.SandboxConfig) error {
	fcConfig := firecracker.DefaultBackendConfig()
	fcConfig.NetworkEnabled = cfg.NetworkEnabled

	if cfg.PoolSize > 0 {
		fcConfig.PoolConfig.InitialSize = cfg.PoolSize
	}
	if cfg.MaxPoolSize > 0 {
		fcConfig.PoolConfig.MaxSize = cfg.MaxPoolSize
	}
	if cfg.Limits.MaxCPU > 0 {
		vcpus := int64((cfg.Limits.MaxCPU + 999) / 1000)
		if vcpus < 1 {
			vcpus = 1
		}
		fcConfig.DefaultVCPUs = vcpus
		fcConfig.PoolConfig.DefaultVCPUs = vcpus
	}
	if memMB, err := parseMemoryMB(cfg.Limits.MaxMemory); err == nil && memMB > 0 {
		fcConfig.DefaultMemMB = int64(memMB)
		fcConfig.PoolConfig.DefaultMemMB = int64(memMB)
	}

	fcBackend, err := firecracker.NewBackend(fcConfig)
	if err != nil {
		return err
	}

	if err := fcBackend.Start(ctx); err != nil {
		_ = fcBackend.Close()
		return err
	}

	sandbox.InitFirecrackerBackend(fcBackend)
	m.firecrackerBackend = fcBackend
	return nil
}

// registerBrowserTool sets up and registers the browser tool.
func (m *ToolManager) registerBrowserTool(runtime *agent.Runtime) error {
	cfg := m.config.Tools.Browser

	pool, err := browser.NewPool(browser.PoolConfig{
		Headless: cfg.Headless,
	})
	if err != nil {
		return err
	}

	m.browserPool = pool
	runtime.RegisterTool(browser.NewBrowserTool(pool))
	return nil
}

// registerWebSearchTool registers the web search tool.
func (m *ToolManager) registerWebSearchTool(runtime *agent.Runtime) {
	cfg := m.config.Tools.WebSearch

	searchConfig := &websearch.Config{
		SearXNGURL: cfg.URL,
	}

	switch cfg.Provider {
	case string(websearch.BackendSearXNG):
		searchConfig.DefaultBackend = websearch.BackendSearXNG
	case string(websearch.BackendBraveSearch):
		searchConfig.DefaultBackend = websearch.BackendBraveSearch
	case string(websearch.BackendDuckDuckGo):
		searchConfig.DefaultBackend = websearch.BackendDuckDuckGo
	default:
		if searchConfig.SearXNGURL != "" {
			searchConfig.DefaultBackend = websearch.BackendSearXNG
		} else {
			searchConfig.DefaultBackend = websearch.BackendDuckDuckGo
		}
	}

	runtime.RegisterTool(websearch.NewWebSearchTool(searchConfig))
}

// registerMemorySearchTool registers the memory search tool.
func (m *ToolManager) registerMemorySearchTool(runtime *agent.Runtime) {
	cfg := m.config.Tools.MemorySearch

	searchConfig := &memorysearch.Config{
		Directory:     cfg.Directory,
		MemoryFile:    cfg.MemoryFile,
		WorkspacePath: m.config.Workspace.Path,
		MaxResults:    cfg.MaxResults,
		MaxSnippetLen: cfg.MaxSnippetLen,
		Mode:          cfg.Mode,
		Embeddings: memorysearch.EmbeddingsConfig{
			Provider: cfg.Embeddings.Provider,
			APIKey:   cfg.Embeddings.APIKey,
			BaseURL:  cfg.Embeddings.BaseURL,
			Model:    cfg.Embeddings.Model,
			CacheDir: cfg.Embeddings.CacheDir,
			CacheTTL: cfg.Embeddings.CacheTTL,
			Timeout:  cfg.Embeddings.Timeout,
		},
	}

	runtime.RegisterTool(memorysearch.NewMemorySearchTool(searchConfig))
}

// GetBrowserPool returns the browser pool if active.
func (m *ToolManager) GetBrowserPool() *browser.Pool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.browserPool
}

// GetFirecrackerBackend returns the firecracker backend if active.
func (m *ToolManager) GetFirecrackerBackend() *firecracker.Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.firecrackerBackend
}

// RegisteredTools returns the list of registered native tool names.
func (m *ToolManager) RegisteredTools() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools := make([]string, len(m.registeredTools))
	copy(tools, m.registeredTools)
	return tools
}

// MCPTools returns the list of registered MCP tool names.
func (m *ToolManager) MCPTools() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools := make([]string, len(m.mcpTools))
	copy(tools, m.mcpTools)
	return tools
}

// AllTools returns all registered tool names (native + MCP).
func (m *ToolManager) AllTools() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	all := make([]string, 0, len(m.registeredTools)+len(m.mcpTools))
	all = append(all, m.registeredTools...)
	all = append(all, m.mcpTools...)
	return all
}

// Ensure ToolManager implements FullLifecycleComponent.
var _ infra.FullLifecycleComponent = (*ToolManager)(nil)
