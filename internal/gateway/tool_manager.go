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
	"github.com/haasonsaas/nexus/internal/attention"
	"github.com/haasonsaas/nexus/internal/canvas"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/internal/infra"
	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/mcp"
	modelcatalog "github.com/haasonsaas/nexus/internal/models"
	ragindex "github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/haasonsaas/nexus/internal/tasks"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	canvastools "github.com/haasonsaas/nexus/internal/tools/canvas"
	"github.com/haasonsaas/nexus/internal/tools/computeruse"
	crontools "github.com/haasonsaas/nexus/internal/tools/cron"
	exectools "github.com/haasonsaas/nexus/internal/tools/exec"
	"github.com/haasonsaas/nexus/internal/tools/facts"
	"github.com/haasonsaas/nexus/internal/tools/files"
	gatewaytools "github.com/haasonsaas/nexus/internal/tools/gateway"
	"github.com/haasonsaas/nexus/internal/tools/homeassistant"
	jobtools "github.com/haasonsaas/nexus/internal/tools/jobs"
	"github.com/haasonsaas/nexus/internal/tools/memorysearch"
	"github.com/haasonsaas/nexus/internal/tools/message"
	modelstools "github.com/haasonsaas/nexus/internal/tools/models"
	nodestools "github.com/haasonsaas/nexus/internal/tools/nodes"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	ragtools "github.com/haasonsaas/nexus/internal/tools/rag"
	"github.com/haasonsaas/nexus/internal/tools/reminders"
	"github.com/haasonsaas/nexus/internal/tools/sandbox"
	"github.com/haasonsaas/nexus/internal/tools/sandbox/firecracker"
	"github.com/haasonsaas/nexus/internal/tools/servicenow"
	sessiontools "github.com/haasonsaas/nexus/internal/tools/sessions"
	"github.com/haasonsaas/nexus/internal/tools/websearch"
	"github.com/haasonsaas/nexus/pkg/models"
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
	sessionStore   sessions.Store
	skillsManager  *skills.Manager
	attentionFeed  *attention.Feed
	channels       *channels.Registry
	cronScheduler  *cron.Scheduler
	canvasHost     *canvas.Host
	canvasManager  *canvas.Manager
	gateway        *Server
	modelCatalog   *modelcatalog.Catalog
	bedrockDisc    *modelcatalog.BedrockDiscovery
	edgeManager    *edge.Manager
	edgeTOFU       *edge.TOFUAuthenticator
	taskStore      tasks.Store
	ragManager     *ragindex.Manager

	// Managed resources
	browserPool        *browser.Pool
	firecrackerBackend *firecracker.Backend

	// Registered tools tracking
	registeredTools []string
	mcpTools        []string
	toolSummaries   []models.ToolSummary
}

// ToolManagerConfig configures the ToolManager.
type ToolManagerConfig struct {
	Config         *config.Config
	MCPManager     *mcp.Manager
	PolicyResolver *policy.Resolver
	JobStore       jobs.Store
	Sessions       sessions.Store
	SkillsManager  *skills.Manager
	AttentionFeed  *attention.Feed
	Channels       *channels.Registry
	CronScheduler  *cron.Scheduler
	CanvasHost     *canvas.Host
	CanvasManager  *canvas.Manager
	Gateway        *Server
	ModelCatalog   *modelcatalog.Catalog
	BedrockDisc    *modelcatalog.BedrockDiscovery
	EdgeManager    *edge.Manager
	EdgeTOFU       *edge.TOFUAuthenticator
	TaskStore      tasks.Store
	RAGManager     *ragindex.Manager
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
		sessionStore:    cfg.Sessions,
		skillsManager:   cfg.SkillsManager,
		attentionFeed:   cfg.AttentionFeed,
		channels:        cfg.Channels,
		cronScheduler:   cfg.CronScheduler,
		canvasHost:      cfg.CanvasHost,
		canvasManager:   cfg.CanvasManager,
		gateway:         cfg.Gateway,
		modelCatalog:    cfg.ModelCatalog,
		bedrockDisc:     cfg.BedrockDisc,
		edgeManager:     cfg.EdgeManager,
		edgeTOFU:        cfg.EdgeTOFU,
		taskStore:       cfg.TaskStore,
		ragManager:      cfg.RAGManager,
		registeredTools: make([]string, 0),
		mcpTools:        make([]string, 0),
		toolSummaries:   make([]models.ToolSummary, 0),
	}
}

// SetSessionStore updates the session store reference for tool registration.
func (m *ToolManager) SetSessionStore(store sessions.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionStore = store
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
	if m.config != nil && m.config.RAG.Enabled {
		if m.ragManager == nil {
			details["rag"] = "unavailable"
		} else {
			details["rag"] = "active"
		}
	}

	if m.browserPool != nil {
		details["browser_pool"] = "active"
	}
	if m.firecrackerBackend != nil {
		details["firecracker"] = "active"
	}

	switch m.State() {
	case infra.ComponentStateRunning:
		if m.config != nil && m.config.RAG.Enabled && m.ragManager == nil {
			return infra.ComponentHealth{
				State:   infra.ServiceHealthUnhealthy,
				Message: "rag unavailable",
				Details: details,
			}
		}
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
	if runtime == nil {
		return nil
	}

	m.mu.Lock()
	if m.config == nil {
		m.mu.Unlock()
		return nil
	}
	cfg := m.config
	prevTools := append([]string(nil), m.registeredTools...)
	prevMCP := append([]string(nil), m.mcpTools...)
	browserPool := m.browserPool
	resolver := m.policyResolver
	m.registeredTools = nil
	m.toolSummaries = nil
	m.mcpTools = nil
	m.browserPool = nil
	m.mu.Unlock()

	for _, name := range prevTools {
		runtime.UnregisterTool(name)
	}
	for _, name := range prevMCP {
		runtime.UnregisterTool(name)
	}
	if resolver != nil {
		resolver.ResetMCP()
	}
	if browserPool != nil {
		if err := browserPool.Close(); err != nil {
			m.Logger().Warn("failed to close browser pool", "error", err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return nil
	}
	cfg = m.config

	fileCfg := files.Config{Workspace: cfg.Workspace.Path}
	m.registerCoreTool(runtime, files.NewReadTool(fileCfg))
	m.registerCoreTool(runtime, files.NewWriteTool(fileCfg))
	m.registerCoreTool(runtime, files.NewEditTool(fileCfg))
	m.registerCoreTool(runtime, files.NewApplyPatchTool(fileCfg))

	execManager := exectools.NewManager(cfg.Workspace.Path)
	m.registerCoreTool(runtime, exectools.NewExecTool("exec", execManager))
	m.registerCoreTool(runtime, exectools.NewExecTool("bash", execManager))
	m.registerCoreTool(runtime, exectools.NewProcessTool(execManager))

	if m.sessionStore != nil {
		m.registerCoreTool(runtime, sessiontools.NewListTool(m.sessionStore, cfg.Session.DefaultAgentID))
		m.registerCoreTool(runtime, sessiontools.NewHistoryTool(m.sessionStore))
		m.registerCoreTool(runtime, sessiontools.NewStatusTool(m.sessionStore))
		m.registerCoreTool(runtime, sessiontools.NewSendTool(m.sessionStore, runtime))
	}

	if m.channels != nil {
		m.registerCoreTool(runtime, message.NewTool("message", m.channels, m.sessionStore, cfg.Session.DefaultAgentID))
		m.registerCoreTool(runtime, message.NewTool("send_message", m.channels, m.sessionStore, cfg.Session.DefaultAgentID))
	}

	// Register sandbox tool
	if cfg.Tools.Sandbox.Enabled {
		if err := m.registerSandboxTool(ctx, runtime); err != nil {
			return fmt.Errorf("sandbox tool: %w", err)
		}
	}

	// Register browser tool
	if cfg.Tools.Browser.Enabled {
		if err := m.registerBrowserTool(runtime); err != nil {
			return fmt.Errorf("browser tool: %w", err)
		}
	}

	// Register web search tool
	if cfg.Tools.WebSearch.Enabled {
		m.registerWebSearchTool(runtime)
	}
	if cfg.Tools.WebFetch.Enabled {
		m.registerCoreTool(runtime, websearch.NewWebFetchTool(&websearch.FetchConfig{MaxChars: cfg.Tools.WebFetch.MaxChars}))
	}

	// Register memory search tool
	if cfg.Tools.MemorySearch.Enabled {
		m.registerMemorySearchTool(runtime)
		m.registerCoreTool(runtime, memorysearch.NewMemoryGetTool(&memorysearch.Config{
			Directory:     cfg.Tools.MemorySearch.Directory,
			MemoryFile:    cfg.Tools.MemorySearch.MemoryFile,
			WorkspacePath: cfg.Workspace.Path,
			MaxResults:    cfg.Tools.MemorySearch.MaxResults,
			MaxSnippetLen: cfg.Tools.MemorySearch.MaxSnippetLen,
			Mode:          cfg.Tools.MemorySearch.Mode,
			Embeddings: memorysearch.EmbeddingsConfig{
				Provider: cfg.Tools.MemorySearch.Embeddings.Provider,
				APIKey:   cfg.Tools.MemorySearch.Embeddings.APIKey,
				BaseURL:  cfg.Tools.MemorySearch.Embeddings.BaseURL,
				Model:    cfg.Tools.MemorySearch.Embeddings.Model,
				CacheDir: cfg.Tools.MemorySearch.Embeddings.CacheDir,
				CacheTTL: cfg.Tools.MemorySearch.Embeddings.CacheTTL,
				Timeout:  cfg.Tools.MemorySearch.Embeddings.Timeout,
			},
		}))
	}

	// Register RAG tools if enabled
	if cfg.RAG.Enabled && m.ragManager != nil {
		searchCfg := ragtools.DefaultSearchToolConfig()
		if cfg.RAG.Search.DefaultLimit > 0 {
			searchCfg.DefaultLimit = cfg.RAG.Search.DefaultLimit
		}
		if cfg.RAG.Search.MaxResults > 0 {
			searchCfg.MaxLimit = cfg.RAG.Search.MaxResults
		}
		if cfg.RAG.Search.DefaultThreshold > 0 {
			searchCfg.DefaultThreshold = cfg.RAG.Search.DefaultThreshold
		}
		m.registerCoreTool(runtime, ragtools.NewSearchTool(m.ragManager, &searchCfg))
		m.registerCoreTool(runtime, ragtools.NewUploadTool(m.ragManager, nil))
	}

	// Register structured fact extraction tool
	if cfg.Tools.FactExtract.Enabled {
		m.registerCoreTool(runtime, facts.NewExtractTool(cfg.Tools.FactExtract.MaxFacts))
	}

	// Register skill-provided tools
	if m.skillsManager != nil {
		for _, skill := range m.skillsManager.ListEligible() {
			for _, tool := range skills.BuildSkillTools(skill, execManager) {
				m.registerCoreTool(runtime, tool)
			}
		}
	}

	// Register job status tool
	if m.jobStore != nil {
		m.registerCoreTool(runtime, jobtools.NewStatusTool(m.jobStore))
	}

	// Register reminder tools if task store is available
	if m.taskStore != nil && cfg.Tasks.Enabled {
		m.registerCoreTool(runtime, reminders.NewSetTool(m.taskStore))
		m.registerCoreTool(runtime, reminders.NewCancelTool(m.taskStore))
		m.registerCoreTool(runtime, reminders.NewListTool(m.taskStore))
	}

	// Register attention feed tools
	if m.attentionFeed != nil {
		m.registerCoreTool(runtime, attention.NewListAttentionTool(m.attentionFeed))
		m.registerCoreTool(runtime, attention.NewGetAttentionTool(m.attentionFeed))
		m.registerCoreTool(runtime, attention.NewHandleAttentionTool(m.attentionFeed))
		m.registerCoreTool(runtime, attention.NewSnoozeAttentionTool(m.attentionFeed))
		m.registerCoreTool(runtime, attention.NewStatsAttentionTool(m.attentionFeed))
	}

	// Register Home Assistant tools if enabled
	if cfg.Channels.HomeAssistant.Enabled {
		haClient, err := homeassistant.NewClient(homeassistant.Config{
			BaseURL: cfg.Channels.HomeAssistant.BaseURL,
			Token:   cfg.Channels.HomeAssistant.Token,
			Timeout: cfg.Channels.HomeAssistant.Timeout,
		})
		if err != nil {
			return fmt.Errorf("home assistant client: %w", err)
		}
		m.registerCoreTool(runtime, homeassistant.NewCallServiceTool(haClient))
		m.registerCoreTool(runtime, homeassistant.NewGetStateTool(haClient))
		m.registerCoreTool(runtime, homeassistant.NewListEntitiesTool(haClient))
	}

	// Register ServiceNow tools if enabled
	if cfg.Tools.ServiceNow.Enabled {
		snowClient := servicenow.NewClient(servicenow.Config{
			InstanceURL: cfg.Tools.ServiceNow.InstanceURL,
			Username:    cfg.Tools.ServiceNow.Username,
			Password:    cfg.Tools.ServiceNow.Password,
		})
		m.registerCoreTool(runtime, servicenow.NewListTicketsTool(snowClient))
		m.registerCoreTool(runtime, servicenow.NewGetTicketTool(snowClient))
		m.registerCoreTool(runtime, servicenow.NewAddCommentTool(snowClient))
		m.registerCoreTool(runtime, servicenow.NewResolveTicketTool(snowClient))
		m.registerCoreTool(runtime, servicenow.NewUpdateTicketTool(snowClient))
	}

	// Register MCP tools
	if cfg.MCP.Enabled && m.mcpManager != nil {
		mcpTools := mcp.RegisterToolsWithRegistrar(runtime, m.mcpManager, m.policyResolver)
		m.mcpTools = mcpTools
		m.Logger().Info("registered MCP tools", "count", len(mcpTools))
	}

	if m.cronScheduler != nil {
		m.registerCoreTool(runtime, crontools.NewTool(m.cronScheduler))
	}
	if m.canvasHost != nil || m.canvasManager != nil {
		m.registerCoreTool(runtime, canvastools.NewTool(m.canvasHost, m.canvasManager))
	}
	if m.gateway != nil {
		m.registerCoreTool(runtime, gatewaytools.NewTool(m.gateway))
	}
	if m.modelCatalog != nil {
		m.registerCoreTool(runtime, modelstools.NewTool(m.modelCatalog, m.bedrockDisc))
	}
	if cfg.Edge.Enabled && m.edgeManager != nil {
		m.registerCoreTool(runtime, nodestools.NewTool(m.edgeManager, m.edgeTOFU))
	}
	if cfg.Edge.Enabled && m.edgeManager != nil {
		m.registerEdgeTools(runtime)
	}

	m.Logger().Info("tools registered",
		"native", len(m.registeredTools),
		"mcp", len(m.mcpTools),
	)

	return nil
}

func (m *ToolManager) registerEdgeTools(runtime *agent.Runtime) {
	provider := edge.NewToolProvider(m.edgeManager)
	for _, tool := range provider.GetTools() {
		m.registerCoreTool(runtime, tool)
	}
	m.Logger().Info("registered edge tools", "count", len(provider.GetTools()))

	if m.config != nil && m.config.Tools.ComputerUse.Enabled {
		m.registerCoreTool(runtime, computeruse.NewTool(m.edgeManager, computeruse.Config{
			EdgeID:          m.config.Tools.ComputerUse.EdgeID,
			DisplayWidthPx:  m.config.Tools.ComputerUse.DisplayWidthPx,
			DisplayHeightPx: m.config.Tools.ComputerUse.DisplayHeightPx,
			DisplayNumber:   m.config.Tools.ComputerUse.DisplayNumber,
		}))
		m.Logger().Info("registered computer use tool", "edge_id", m.config.Tools.ComputerUse.EdgeID)
	}
}

// ReloadMCPTools refreshes MCP tool registrations and policy aliases.
func (m *ToolManager) ReloadMCPTools(runtime *agent.Runtime, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}

	m.mu.Lock()
	m.config = cfg
	previous := append([]string(nil), m.mcpTools...)
	m.mcpTools = nil
	mcpManager := m.mcpManager
	resolver := m.policyResolver
	m.mu.Unlock()

	if runtime != nil {
		for _, name := range previous {
			runtime.UnregisterTool(name)
		}
	}

	if resolver != nil {
		resolver.ResetMCP()
	}

	if runtime == nil || !cfg.MCP.Enabled || mcpManager == nil {
		return nil
	}

	tools := mcp.RegisterToolsWithRegistrar(runtime, mcpManager, resolver)

	m.mu.Lock()
	m.mcpTools = tools
	m.mu.Unlock()

	m.Logger().Info("reloaded MCP tools", "count", len(tools))
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

	executor, err := sandbox.NewExecutor(opts...)
	if err != nil {
		return err
	}
	m.registerCoreTool(runtime, executor)
	return nil
}

// setupFirecrackerBackend initializes the firecracker backend.
func (m *ToolManager) setupFirecrackerBackend(ctx context.Context, cfg *config.SandboxConfig) error {
	if m.firecrackerBackend != nil {
		return nil
	}
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
	m.registerCoreTool(runtime, browser.NewBrowserTool(pool))
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

	m.registerCoreTool(runtime, websearch.NewWebSearchTool(searchConfig))
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

	m.registerCoreTool(runtime, memorysearch.NewMemorySearchTool(searchConfig))
}

func (m *ToolManager) registerCoreTool(runtime *agent.Runtime, tool agent.Tool) {
	if runtime == nil || tool == nil {
		return
	}
	runtime.RegisterTool(tool)
	name := tool.Name()
	m.registeredTools = append(m.registeredTools, name)
	m.toolSummaries = append(m.toolSummaries, models.ToolSummary{
		Name:        name,
		Description: tool.Description(),
		Schema:      tool.Schema(),
		Source:      "core",
		Canonical:   "core." + name,
	})
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

// ToolSummaries returns detailed tool metadata for display.
func (m *ToolManager) ToolSummaries() []models.ToolSummary {
	m.mu.RLock()
	core := make([]models.ToolSummary, len(m.toolSummaries))
	copy(core, m.toolSummaries)
	mcpMgr := m.mcpManager
	cfg := m.config
	m.mu.RUnlock()

	if mcpMgr != nil && cfg != nil && cfg.MCP.Enabled {
		core = append(core, mcp.ToolSummaries(mcpMgr)...)
	}
	return core
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
