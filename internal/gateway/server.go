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
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/commands"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/internal/hooks/bundled"
	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/media"
	"github.com/haasonsaas/nexus/internal/media/transcribe"
	"github.com/haasonsaas/nexus/internal/memory"
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
	config    *config.Config
	grpc      *grpc.Server
	channels  *channels.Registry
	logger    *slog.Logger
	wg        sync.WaitGroup
	cancel    context.CancelFunc
	startTime time.Time

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
		ctx := context.Background()
		if err := skillsMgr.Discover(ctx); err != nil {
			logger.Error("skill discovery failed", "error", err)
			return
		}
		if err := skillsMgr.StartWatching(ctx); err != nil {
			logger.Error("skill watcher failed", "error", err)
		}
	}()

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
		discoverCtx := context.Background()
		sources := hooks.BuildDefaultSources(
			cfg.Workspace.Path,
			hooks.DefaultLocalPath(),
			nil, // extra dirs
		)
		// Add embedded bundled hooks source
		sources = append([]hooks.DiscoverySource{
			hooks.NewEmbeddedSource(bundled.BundledFS(), hooks.SourceBundled, hooks.PriorityBundled),
		}, sources...)
		discoveredHooks, err := hooks.DiscoverAll(discoverCtx, sources)
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

	server := &Server{
		config:             cfg,
		grpc:               grpcServer,
		channels:           channels.NewRegistry(),
		logger:             logger,
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
		commandRegistry:    commandRegistry,
		commandParser:      commandParser,
		activeRuns:         make(map[string]activeRun),
	}
	grpcSvc := newGRPCService(server)
	proto.RegisterNexusGatewayServer(grpcServer, grpcSvc)
	proto.RegisterSessionServiceServer(grpcServer, grpcSvc)
	proto.RegisterAgentServiceServer(grpcServer, grpcSvc)
	proto.RegisterChannelServiceServer(grpcServer, grpcSvc)
	proto.RegisterHealthServiceServer(grpcServer, grpcSvc)
	registerBuiltinChannelPlugins(server.channelPlugins)

	if err := server.registerChannelsFromConfig(); err != nil {
		return nil, err
	}

	return server, nil
}

// Channels returns the channel registry for accessing registered channel adapters.
func (s *Server) Channels() *channels.Registry {
	return s.channels
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
	return s.runtimePlugins.LoadChannels(s.config, s.channels)
}
