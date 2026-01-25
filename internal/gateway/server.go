// Package gateway provides the main Nexus gateway server.
//
// server.go contains the core Server struct definition and constructor.
// Related functionality is organized in separate files:
//   - lifecycle.go: server startup, shutdown, and background tasks
//   - processing.go: message processing and broadcast handling
//   - runtime.go: runtime initialization, provider setup, tool registration
//   - helpers.go: utility functions for message handling
//   - middleware.go: gRPC interceptors
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/artifacts"
	"github.com/haasonsaas/nexus/internal/audit"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/canvas"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/commands"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/internal/hooks/bundled"
	"github.com/haasonsaas/nexus/internal/identity"
	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/media"
	"github.com/haasonsaas/nexus/internal/media/transcribe"
	"github.com/haasonsaas/nexus/internal/memory"
	modelcatalog "github.com/haasonsaas/nexus/internal/models"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/internal/plugins"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/haasonsaas/nexus/internal/storage"
	"github.com/haasonsaas/nexus/internal/tasks"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/internal/tools/sandbox/firecracker"
	"github.com/haasonsaas/nexus/pkg/models"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// Server is the main Nexus gateway server that handles gRPC requests, manages channels,
// and coordinates between the agent runtime, session store, and various subsystems.
type Server struct {
	config      *config.Config
	configPath  string
	grpc        *grpc.Server
	channels    *channels.Registry
	logger      *slog.Logger
	auditLogger *audit.Logger
	wg          sync.WaitGroup
	cancel      context.CancelFunc
	startTime   time.Time

	// startupCancel cancels background discovery goroutines launched during initialization
	startupCancel context.CancelFunc

	handleMessageHook func(context.Context, *models.Message)

	runtimeMu   sync.Mutex
	runtime     *agent.Runtime
	sessions    sessions.Store
	branchStore sessions.BranchStore
	stores      storage.StoreSet

	browserPool     *browser.Pool
	memoryLogger    *sessions.MemoryLogger
	skillsManager   *skills.Manager
	vectorMemory    *memory.Manager
	mediaProcessor  media.Processor
	mediaAggregator *media.Aggregator

	channelPlugins     *channelPluginRegistry
	runtimePlugins     *plugins.RuntimeRegistry
	authService        *auth.Service
	cronScheduler      *cron.Scheduler
	taskScheduler      *tasks.Scheduler
	taskStore          tasks.Store
	mcpManager         *mcp.Manager
	firecrackerBackend *firecracker.Backend
	toolManager        *ToolManager

	toolPolicyResolver *policy.Resolver
	llmProvider        agent.LLMProvider
	defaultModel       string
	jobStore           jobs.Store
	approvalChecker    *agent.ApprovalChecker
	commandRegistry    *commands.Registry
	commandParser      *commands.Parser
	activeRuns         map[string]activeRun
	activeRunsMu       sync.Mutex

	broadcastManager *BroadcastManager
	hooksRegistry    *hooks.Registry

	edgeManager *edge.Manager
	edgeService *edge.Service
	edgeTOFU    *edge.TOFUAuthenticator

	modelCatalog     *modelcatalog.Catalog
	bedrockDiscovery *modelcatalog.BedrockDiscovery

	// Artifact repository for tool-produced files
	artifactRepo artifacts.Repository

	// Event timeline for observability and debugging
	eventStore    *observability.MemoryEventStore
	eventRecorder *observability.EventRecorder

	// Trace directory plugin for run tracing
	tracePlugin *agent.TraceDirectoryPlugin

	// Identity linking for cross-channel user mapping
	identityStore identity.Store

	// messageSem limits concurrent message processing to prevent unbounded goroutine growth
	messageSem chan struct{}

	// normalizer normalizes incoming messages to canonical format
	normalizer *MessageNormalizer

	// streamingRegistry manages streaming behavior per channel
	streamingRegistry *StreamingRegistry

	// canvasHost serves the dedicated canvas host
	canvasHost *canvas.Host
	// canvasManager handles realtime canvas state updates
	canvasManager *canvas.Manager

	// httpServer serves the HTTP dashboard, API, and control plane WebSocket
	httpServer   *http.Server
	httpListener net.Listener

	configApplyMu sync.Mutex

	// singletonLock prevents multiple gateway instances from running
	singletonLock *GatewayLockHandle

	// integration wires up cross-cutting observability and health systems
	integration *Integration
}

// NewServer creates a new gateway server with the given configuration and logger.
// If cfg is nil, an empty config is used. If logger is nil, slog.Default() is used.
func NewServer(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Create startup context for background discovery goroutines
	startupCtx, startupCancel := context.WithCancel(context.Background())
	startupCancelUsed := false
	defer func() {
		if !startupCancelUsed {
			startupCancel()
		}
	}()

	// Create gRPC server with interceptors
	apiKeys := make([]auth.APIKeyConfig, 0, len(cfg.Auth.APIKeys))
	for _, entry := range cfg.Auth.APIKeys {
		apiKeys = append(apiKeys, auth.APIKeyConfig{
			Key:    entry.Key,
			UserID: entry.UserID,
			Email:  entry.Email,
			Name:   entry.Name,
		})
	}
	authService := auth.NewService(auth.Config{
		JWTSecret:   cfg.Auth.JWTSecret,
		TokenExpiry: cfg.Auth.TokenExpiry,
		APIKeys:     apiKeys,
	})
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingInterceptor(logger),
			auth.UnaryInterceptor(authService, logger),
		),
		grpc.ChainStreamInterceptor(
			streamLoggingInterceptor(logger),
			auth.StreamInterceptor(authService, logger),
		),
	)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("nexus", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable reflection for development
	reflection.Register(grpcServer)

	// Initialize skills manager
	skillsMgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create skills manager: %w", err)
	}
	// Discover skills (non-blocking, errors logged)
	go func() {
		if err := skillsMgr.Discover(startupCtx); err != nil {
			logger.Error("skill discovery failed", "error", err)
			return
		}
		if err := skillsMgr.StartWatching(startupCtx); err != nil {
			logger.Error("skill watcher failed", "error", err)
		}
	}()

	// Initialize dedicated canvas host when enabled
	var canvasHost *canvas.Host
	if cfg.CanvasHost.Enabled != nil && *cfg.CanvasHost.Enabled {
		host, err := canvas.NewHost(cfg.CanvasHost, cfg.Canvas, logger)
		if err != nil {
			logger.Warn("canvas host init failed", "error", err)
		} else {
			canvasHost = host
		}
	}

	// Initialize canvas manager and store
	var canvasStore canvas.Store
	if cfg.Database.URL != "" {
		storeCfg := storage.DefaultCockroachConfig()
		if cfg.Database.MaxConnections > 0 {
			storeCfg.MaxOpenConns = cfg.Database.MaxConnections
		}
		if cfg.Database.ConnMaxLifetime > 0 {
			storeCfg.ConnMaxLifetime = cfg.Database.ConnMaxLifetime
		}
		dbStore, err := canvas.NewCockroachStoreFromDSN(cfg.Database.URL, storeCfg)
		if err != nil {
			logger.Warn("canvas store falling back to memory", "error", err)
			canvasStore = canvas.NewMemoryStore()
		} else {
			canvasStore = dbStore
			logger.Info("using database-backed canvas store")
		}
	} else {
		canvasStore = canvas.NewMemoryStore()
	}
	canvasMetrics := canvas.NewMetrics()
	canvasManager := canvas.NewManager(canvasStore, logger)
	canvasManager.SetMetrics(canvasMetrics)
	if canvasHost != nil {
		canvasHost.SetManager(canvasManager)
		canvasHost.SetMetrics(canvasMetrics)
		canvasHost.SetAuthService(authService)
	}

	var auditLogger *audit.Logger
	loggerInstance, err := audit.NewLogger(cfg.Canvas.Audit)
	if err != nil {
		logger.Warn("audit logger init failed", "error", err)
	} else {
		auditLogger = loggerInstance
	}
	canvasManager.SetAuditLogger(auditLogger)

	// Initialize vector memory manager (optional, returns nil if not enabled)
	if cfg.VectorMemory.Enabled && cfg.VectorMemory.Pgvector.UseCockroachDB && cfg.VectorMemory.Pgvector.DSN == "" {
		cfg.VectorMemory.Pgvector.DSN = cfg.Database.URL
	}
	vectorMem, err := memory.NewManager(&cfg.VectorMemory)
	if err != nil {
		logger.Warn("vector memory not initialized", "error", err)
	}
	var mediaProcessor media.Processor
	var mediaAggregator *media.Aggregator
	if cfg.Transcription.Enabled {
		transcriber, err := transcribe.New(transcribe.Config{
			Provider: cfg.Transcription.Provider,
			APIKey:   cfg.Transcription.APIKey,
			BaseURL:  cfg.Transcription.BaseURL,
			Model:    cfg.Transcription.Model,
			Language: cfg.Transcription.Language,
			Logger:   logger,
		})
		if err != nil {
			logger.Warn("transcription not initialized", "error", err)
		} else {
			processor := media.NewDefaultProcessor(logger)
			processor.SetTranscriber(transcriber)
			mediaProcessor = processor
			mediaAggregator = media.NewAggregator(processor, logger)
		}
	}
	mcpManager := mcp.NewManager(&cfg.MCP, logger)
	toolPolicyResolver := policy.NewResolver()
	commandRegistry := commands.NewRegistry(logger)
	commands.RegisterBuiltins(commandRegistry)
	commandParser := commands.NewParser(commandRegistry)

	modelCatalog := modelcatalog.NewCatalog()
	var bedrockDiscovery *modelcatalog.BedrockDiscovery
	if cfg.LLM.Bedrock.Enabled {
		bedrockCfg := buildBedrockDiscoveryConfig(cfg.LLM.Bedrock, logger)
		bedrockDiscovery = modelcatalog.NewBedrockDiscovery(bedrockCfg, logger)
		if err := bedrockDiscovery.RegisterWithCatalog(startupCtx, modelCatalog); err != nil {
			logger.Warn("bedrock discovery failed", "error", err)
		}
	}

	// Create job store (prefer DB when available)
	var jobStore jobs.Store
	if cfg.Database.URL != "" {
		dbJobStore, err := jobs.NewCockroachStoreFromDSN(cfg.Database.URL, nil)
		if err != nil {
			logger.Warn("job store falling back to memory", "error", err)
			jobStore = jobs.NewMemoryStore()
		} else {
			jobStore = dbJobStore
			logger.Info("using database-backed job store")
		}
	} else {
		jobStore = jobs.NewMemoryStore()
	}

	stores, err := initStorageStores(cfg)
	if err != nil {
		return nil, err
	}
	if stores.Users != nil {
		authService.SetUserStore(stores.Users)
	}
	registerOAuthProviders(authService, cfg.Auth.OAuth)

	var cronScheduler *cron.Scheduler
	if cfg.Cron.Enabled {
		cronScheduler, err = cron.NewScheduler(cfg.Cron, cron.WithLogger(logger))
		if err != nil {
			return nil, fmt.Errorf("cron scheduler: %w", err)
		}
	}

	// Initialize task store if tasks are enabled
	var taskStore tasks.Store
	if cfg.Tasks.Enabled && cfg.Database.URL != "" {
		taskStoreCfg := tasks.DefaultCockroachConfig()
		if cfg.Database.MaxConnections > 0 {
			taskStoreCfg.MaxOpenConns = cfg.Database.MaxConnections
		}
		if cfg.Database.ConnMaxLifetime > 0 {
			taskStoreCfg.ConnMaxLifetime = cfg.Database.ConnMaxLifetime
		}
		dbTaskStore, err := tasks.NewCockroachStoreFromDSN(cfg.Database.URL, taskStoreCfg)
		if err != nil {
			logger.Warn("task store initialization failed, scheduled tasks disabled", "error", err)
		} else {
			taskStore = dbTaskStore
			logger.Info("scheduled tasks store initialized")
		}
	}

	// Initialize hooks registry
	hooksRegistry := hooks.NewRegistry(logger)
	hooks.SetGlobalRegistry(hooksRegistry)

	// Discover and register hooks (non-blocking)
	go func() {
		sources := hooks.BuildDefaultSources(
			cfg.Workspace.Path,
			hooks.DefaultLocalPath(),
			nil, // extra dirs
		)
		// Add embedded bundled hooks source
		sources = append([]hooks.DiscoverySource{
			hooks.NewEmbeddedSource(bundled.BundledFS(), hooks.SourceBundled, hooks.PriorityBundled),
		}, sources...)
		discoveredHooks, err := hooks.DiscoverAll(startupCtx, sources)
		if err != nil {
			logger.Error("hook discovery failed", "error", err)
			return
		}

		gatingCtx := hooks.NewGatingContext(nil)
		eligible := hooks.FilterEligible(discoveredHooks, gatingCtx)
		logger.Info("discovered hooks", "total", len(discoveredHooks), "eligible", len(eligible))

		// Register discovered hooks with the registry
		for _, h := range eligible {
			for _, eventKey := range h.Config.Events {
				hooksRegistry.Register(eventKey, createHookHandler(h, logger),
					hooks.WithName(h.Config.Name),
					hooks.WithSource(string(h.Source)),
					hooks.WithPriority(h.Config.Priority),
				)
			}
		}
	}()

	artifactSetup, err := buildArtifactSetup(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("artifact setup: %w", err)
	}
	artifactCleanupNeeded := true
	defer func() {
		if !artifactCleanupNeeded {
			return
		}
		if artifactSetup != nil && artifactSetup.cleanup != nil {
			artifactSetup.cleanup.Stop()
		}
		if artifactSetup != nil {
			if closer, ok := artifactSetup.repo.(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					logger.Warn("failed to close artifact repository", "error", err)
				}
			}
		}
	}()

	// Initialize edge manager if enabled
	var edgeManager *edge.Manager
	var edgeService *edge.Service
	var edgeTOFU *edge.TOFUAuthenticator
	var artifactRepo artifacts.Repository
	if cfg.Edge.Enabled {
		edgeAuth, tofuAuth, err := buildEdgeAuthenticator(cfg)
		if err != nil {
			return nil, fmt.Errorf("edge authenticator: %w", err)
		}
		edgeTOFU = tofuAuth

		managerConfig := edge.ManagerConfig{
			HeartbeatInterval:  cfg.Edge.HeartbeatInterval,
			HeartbeatTimeout:   cfg.Edge.HeartbeatTimeout,
			DefaultToolTimeout: cfg.Edge.DefaultToolTimeout,
			MaxConcurrentTools: cfg.Edge.MaxConcurrentTools,
			EventBufferSize:    cfg.Edge.EventBufferSize,
		}
		edgeManager = edge.NewManager(managerConfig, edgeAuth, logger)
		if artifactSetup != nil {
			if artifactSetup.repo != nil {
				edgeManager.SetArtifactRepository(artifactSetup.repo)
				artifactRepo = artifactSetup.repo
			}
			if artifactSetup.redactor != nil {
				edgeManager.SetArtifactRedactionPolicy(artifactSetup.redactor)
			}
		}
		edgeService = edge.NewService(edgeManager)
		logger.Info("edge service initialized", "auth_mode", cfg.Edge.AuthMode)
	}
	if artifactSetup != nil && artifactSetup.cleanup != nil {
		go artifactSetup.cleanup.Start(startupCtx)
	}

	// Initialize event store for observability timeline
	eventStore := observability.NewMemoryEventStore(10000) // Store up to 10k events
	eventRecorder := observability.NewEventRecorder(eventStore, nil)

	// Initialize identity store for cross-channel linking
	identityStore := identity.NewMemoryStore()
	// Import identity links from config if present
	if len(cfg.Session.Scoping.IdentityLinks) > 0 {
		if err := identityStore.ImportFromConfig(context.Background(), cfg.Session.Scoping.IdentityLinks); err != nil {
			logger.Warn("failed to import identity links from config", "error", err)
		}
	}

	// Initialize integration subsystems for observability and health
	integration := NewIntegration(&IntegrationConfig{
		DiagnosticsEnabled: true,
		HealthProbeTimeout: 10 * time.Second,
		UsageCacheTTL:      5 * time.Minute,
		AutoMigrate:        true,
		StateDir:           cfg.Workspace.Path,
	})

	// Configure provider usage fetchers from LLM provider configs
	var anthropicKey, openaiKey, geminiKey string
	if p, ok := cfg.LLM.Providers["anthropic"]; ok {
		anthropicKey = p.APIKey
	}
	if p, ok := cfg.LLM.Providers["openai"]; ok {
		openaiKey = p.APIKey
	}
	if p, ok := cfg.LLM.Providers["google"]; ok {
		geminiKey = p.APIKey
	} else if p, ok := cfg.LLM.Providers["gemini"]; ok {
		geminiKey = p.APIKey
	}
	integration.ConfigureProviderUsage(anthropicKey, openaiKey, geminiKey)

	startupCancelUsed = true
	server := &Server{
		config:             cfg,
		grpc:               grpcServer,
		channels:           channels.NewRegistry(),
		logger:             logger,
		auditLogger:        auditLogger,
		startupCancel:      startupCancel,
		channelPlugins:     newChannelPluginRegistry(),
		runtimePlugins:     plugins.DefaultRuntimeRegistry(),
		skillsManager:      skillsMgr,
		vectorMemory:       vectorMem,
		mediaProcessor:     mediaProcessor,
		mediaAggregator:    mediaAggregator,
		stores:             stores,
		authService:        authService,
		cronScheduler:      cronScheduler,
		taskStore:          taskStore,
		mcpManager:         mcpManager,
		toolPolicyResolver: toolPolicyResolver,
		jobStore:           jobStore,
		hooksRegistry:      hooksRegistry,
		edgeManager:        edgeManager,
		edgeService:        edgeService,
		edgeTOFU:           edgeTOFU,
		modelCatalog:       modelCatalog,
		bedrockDiscovery:   bedrockDiscovery,
		canvasHost:         canvasHost,
		canvasManager:      canvasManager,
		artifactRepo:       artifactRepo,
		eventStore:         eventStore,
		eventRecorder:      eventRecorder,
		identityStore:      identityStore,
		commandRegistry:    commandRegistry,
		commandParser:      commandParser,
		activeRuns:         make(map[string]activeRun),
		messageSem:         make(chan struct{}, 100), // Limit concurrent message handlers
		normalizer:         NewMessageNormalizer(),
		streamingRegistry:  NewStreamingRegistry(),
		integration:        integration,
	}
	if artifactSetup != nil {
		server.artifactRepo = artifactSetup.repo
	}
	if server.canvasHost != nil {
		server.canvasHost.SetActionHandler(server.handleCanvasAction)
	}
	grpcSvc := newGRPCService(server)
	proto.RegisterNexusGatewayServer(grpcServer, grpcSvc)
	proto.RegisterSessionServiceServer(grpcServer, grpcSvc)
	proto.RegisterAgentServiceServer(grpcServer, grpcSvc)
	proto.RegisterChannelServiceServer(grpcServer, grpcSvc)
	proto.RegisterHealthServiceServer(grpcServer, grpcSvc)
	proto.RegisterArtifactServiceServer(grpcServer, grpcSvc)
	proto.RegisterEventServiceServer(grpcServer, newEventService(server))
	proto.RegisterTaskServiceServer(grpcServer, newTaskService(server))
	proto.RegisterMessageServiceServer(grpcServer, newMessageService(server))
	proto.RegisterIdentityServiceServer(grpcServer, newIdentityService(identityStore))
	proto.RegisterProvisioningServiceServer(grpcServer, newProvisioningService(server))
	if edgeService != nil {
		proto.RegisterEdgeServiceServer(grpcServer, edgeService)
	}
	registerBuiltinChannelPlugins(server.channelPlugins)

	if err := server.registerChannelsFromConfig(); err != nil {
		return nil, err
	}

	artifactCleanupNeeded = false
	return server, nil
}

// Channels returns the channel registry for accessing registered channel adapters.
func (s *Server) Channels() *channels.Registry {
	return s.channels
}

// TaskStore returns the task store for scheduled task operations.
func (s *Server) TaskStore() tasks.Store {
	return s.taskStore
}

// Normalizer returns the message normalizer.
func (s *Server) Normalizer() *MessageNormalizer {
	return s.normalizer
}

// StreamingRegistry returns the streaming behavior registry.
func (s *Server) StreamingRegistry() *StreamingRegistry {
	return s.streamingRegistry
}

// registerChannelsFromConfig registers channel adapters based on configuration.
func (s *Server) registerChannelsFromConfig() error {
	if s.channelPlugins == nil {
		s.channelPlugins = newChannelPluginRegistry()
		registerBuiltinChannelPlugins(s.channelPlugins)
	}
	if err := s.channelPlugins.LoadEnabled(s.config, s.channels, s.logger); err != nil {
		return err
	}
	if s.runtimePlugins == nil {
		s.runtimePlugins = plugins.DefaultRuntimeRegistry()
	}
	if err := s.runtimePlugins.LoadChannels(s.config, s.channels); err != nil {
		return err
	}
	s.configureSlackCanvas()
	return nil
}

// buildEdgeAuthenticator creates an edge authenticator based on configuration.
func buildEdgeAuthenticator(cfg *config.Config) (edge.Authenticator, *edge.TOFUAuthenticator, error) {
	switch cfg.Edge.AuthMode {
	case "dev":
		return edge.NewDevAuthenticator(), nil, nil
	case "tofu":
		auth := edge.NewTOFUAuthenticator(nil)
		return auth, auth, nil
	case "token", "":
		return edge.NewTokenAuthenticator(cfg.Edge.Tokens), nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown edge auth mode: %s", cfg.Edge.AuthMode)
	}
}

// EdgeManager returns the edge manager for managing edge connections.
func (s *Server) EdgeManager() *edge.Manager {
	return s.edgeManager
}
