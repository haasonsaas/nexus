package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/channels"
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
	jobtools "github.com/haasonsaas/nexus/internal/tools/jobs"
	"github.com/haasonsaas/nexus/internal/tools/memorysearch"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/internal/tools/sandbox"
	"github.com/haasonsaas/nexus/internal/tools/sandbox/firecracker"
	"github.com/haasonsaas/nexus/internal/tools/websearch"
	"github.com/haasonsaas/nexus/pkg/models"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// Server is the main Nexus gateway server.
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

	broadcastManager *BroadcastManager
	hooksRegistry    *hooks.Registry
}

// NewServer creates a new gateway server.
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

// Channels returns the channel registry.
func (s *Server) Channels() *channels.Registry {
	return s.channels
}

// Start begins serving requests.
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
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.logger.Info("starting gRPC server", "addr", addr)
	return s.grpc.Serve(lis)
}

// Stop gracefully shuts down the server.
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

func (s *Server) startProcessing(ctx context.Context) {
	processCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go s.processMessages(processCtx)
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

// processMessages handles incoming messages from all channels.
func (s *Server) processMessages(ctx context.Context) {
	defer s.wg.Done()
	messages := s.channels.AggregateMessages(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			s.wg.Add(1)
			go func(message *models.Message) {
				defer s.wg.Done()
				s.handleMessage(ctx, message)
			}(msg)
		}
	}
}

// handleMessage processes a single incoming message.
func (s *Server) handleMessage(ctx context.Context, msg *models.Message) {
	s.logger.Debug("received message",
		"channel", msg.Channel,
		"content_length", len(msg.Content),
	)

	if s.handleMessageHook != nil {
		s.handleMessageHook(ctx, msg)
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}

	runtime, err := s.ensureRuntime(ctx)
	if err != nil {
		s.logger.Error("runtime initialization failed", "error", err)
		return
	}

	// Check for broadcast routing
	peerID := s.extractPeerID(msg)
	if peerID != "" && s.broadcastManager != nil && s.broadcastManager.IsBroadcastPeer(peerID) {
		s.handleBroadcastMessage(ctx, peerID, msg, runtime)
		return
	}

	channelID, err := s.resolveConversationID(msg)
	if err != nil {
		s.logger.Error("failed to resolve conversation id", "error", err)
		return
	}

	agentID := defaultAgentID
	if s.config != nil && s.config.Session.DefaultAgentID != "" {
		agentID = s.config.Session.DefaultAgentID
	}
	key := sessions.SessionKey(agentID, msg.Channel, channelID)
	session, err := s.sessions.GetOrCreate(ctx, key, agentID, msg.Channel, channelID)
	if err != nil {
		s.logger.Error("failed to get or create session", "error", err)
		return
	}

	msg.SessionID = session.ID
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	s.enrichMessageWithMedia(ctx, msg)

	// Note: inbound message persistence is handled by runtime.Process()
	// to avoid double-persisting the same message.
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(msg); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
		}
	}

	promptCtx := ctx
	if systemPrompt := s.systemPromptForMessage(ctx, session, msg); systemPrompt != "" {
		promptCtx = agent.WithSystemPrompt(promptCtx, systemPrompt)
	}
	if s.toolPolicyResolver != nil {
		if toolPolicy := s.toolPolicyForAgent(ctx, agentID); toolPolicy != nil {
			promptCtx = agent.WithToolPolicy(promptCtx, s.toolPolicyResolver, toolPolicy)
		}
	}

	chunks, err := runtime.Process(promptCtx, session, msg)
	if err != nil {
		s.logger.Error("runtime processing failed", "error", err)
		return
	}

	var response strings.Builder
	var toolResults []models.ToolResult
	for chunk := range chunks {
		if chunk.Error != nil {
			s.logger.Error("runtime stream error", "error", chunk.Error)
			return
		}
		if chunk.Text != "" {
			response.WriteString(chunk.Text)
		}
		if chunk.ToolResult != nil {
			toolResults = append(toolResults, *chunk.ToolResult)
		}
	}

	if response.Len() == 0 && len(toolResults) == 0 {
		return
	}

	adapter, ok := s.channels.GetOutbound(msg.Channel)
	if !ok {
		s.logger.Error("no adapter registered for channel", "channel", msg.Channel)
		return
	}

	outbound := &models.Message{
		SessionID:   session.ID,
		Channel:     msg.Channel,
		Direction:   models.DirectionOutbound,
		Role:        models.RoleAssistant,
		Content:     response.String(),
		ToolResults: toolResults,
		Metadata:    s.buildReplyMetadata(msg),
		CreatedAt:   time.Now(),
	}

	if err := adapter.Send(ctx, outbound); err != nil {
		s.logger.Error("failed to send outbound message", "error", err)
		return
	}

	// Note: assistant message persistence is handled by runtime.Process()
	// The runtime persists the full message with tool calls during the agentic loop.
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(outbound); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
		}
	}

	if session.Metadata != nil {
		if pending, ok := session.Metadata["memory_flush_pending"].(bool); ok && pending {
			session.Metadata["memory_flush_pending"] = false
			session.Metadata["memory_flush_confirmed_at"] = time.Now().Format(time.RFC3339)
			if err := s.sessions.Update(ctx, session); err != nil {
				s.logger.Error("failed to update memory flush confirmation", "error", err)
			}
		}
	}
}

// extractPeerID extracts a peer identifier from the message metadata.
// The peer_id is used to determine if the message should be broadcast to multiple agents.
func (s *Server) extractPeerID(msg *models.Message) string {
	if msg.Metadata == nil {
		return ""
	}
	// Check for explicit peer_id in metadata
	if peerID, ok := msg.Metadata["peer_id"].(string); ok && peerID != "" {
		return peerID
	}
	// Fall back to channel-specific identifiers that can serve as peer IDs
	switch msg.Channel {
	case models.ChannelTelegram:
		if chatID, ok := msg.Metadata["chat_id"]; ok {
			switch v := chatID.(type) {
			case int64:
				return strconv.FormatInt(v, 10)
			case int:
				return strconv.Itoa(v)
			case string:
				return v
			}
		}
	case models.ChannelSlack:
		// Use user_id as peer identifier for Slack
		if userID, ok := msg.Metadata["slack_user"].(string); ok && userID != "" {
			return userID
		}
	case models.ChannelDiscord:
		// Use user_id as peer identifier for Discord
		if userID, ok := msg.Metadata["discord_user_id"].(string); ok && userID != "" {
			return userID
		}
	case models.ChannelWhatsApp:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	case models.ChannelSignal:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	case models.ChannelIMessage:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	case models.ChannelMatrix:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	}
	return ""
}

// handleBroadcastMessage processes a message that should be broadcast to multiple agents.
func (s *Server) handleBroadcastMessage(ctx context.Context, peerID string, msg *models.Message, runtime *agent.Runtime) {
	s.logger.Debug("processing broadcast message",
		"peer_id", peerID,
		"channel", msg.Channel,
	)

	// Ensure broadcast manager has current runtime and sessions
	s.broadcastManager.runtime = runtime
	s.broadcastManager.sessions = s.sessions

	results, err := s.broadcastManager.ProcessBroadcast(
		ctx,
		peerID,
		msg,
		s.resolveConversationID,
		s.systemPromptForMessage,
	)
	if err != nil {
		s.logger.Error("broadcast processing failed", "error", err)
		return
	}

	adapter, ok := s.channels.GetOutbound(msg.Channel)
	if !ok {
		s.logger.Error("no adapter registered for channel", "channel", msg.Channel)
		return
	}

	// Send responses from all agents
	for _, result := range results {
		if result.Error != nil {
			s.logger.Error("agent broadcast error",
				"agent_id", result.AgentID,
				"error", result.Error,
			)
			continue
		}

		if result.Response == "" {
			continue
		}

		outbound := &models.Message{
			SessionID: result.SessionID,
			Channel:   msg.Channel,
			Direction: models.DirectionOutbound,
			Role:      models.RoleAssistant,
			Content:   result.Response,
			Metadata:  s.buildReplyMetadata(msg),
			CreatedAt: time.Now(),
		}

		// Add agent identifier to metadata so the recipient knows which agent responded
		if outbound.Metadata == nil {
			outbound.Metadata = make(map[string]any)
		}
		outbound.Metadata["broadcast_agent_id"] = result.AgentID

		if err := adapter.Send(ctx, outbound); err != nil {
			s.logger.Error("failed to send broadcast response",
				"agent_id", result.AgentID,
				"error", err,
			)
		}

		if s.memoryLogger != nil {
			if err := s.memoryLogger.Append(outbound); err != nil {
				s.logger.Error("failed to write memory log", "error", err)
			}
		}
	}
}

func (s *Server) waitForProcessing(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loggingInterceptor logs unary RPC calls.
func loggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		logger.Debug("rpc call", "method", info.FullMethod)
		resp, err := handler(ctx, req)
		if err != nil {
			logger.Error("rpc error", "method", info.FullMethod, "error", err)
		}
		return resp, err
	}
}

// streamLoggingInterceptor logs streaming RPC calls.
func streamLoggingInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		logger.Debug("stream started", "method", info.FullMethod)
		err := handler(srv, ss)
		if err != nil {
			logger.Error("stream error", "method", info.FullMethod, "error", err)
		}
		logger.Debug("stream ended", "method", info.FullMethod)
		return err
	}
}

const defaultAgentID = "main"

func (s *Server) ensureRuntime(ctx context.Context) (*agent.Runtime, error) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	if s.runtime != nil {
		return s.runtime, nil
	}

	if s.sessions == nil {
		store, err := s.newSessionStore()
		if err != nil {
			return nil, err
		}
		s.sessions = store
	}
	if s.branchStore == nil {
		if cr, ok := s.sessions.(*sessions.CockroachStore); ok {
			s.branchStore = sessions.NewCockroachBranchStore(cr.DB())
		} else {
			s.branchStore = sessions.NewMemoryBranchStore()
		}
	}
	if s.memoryLogger == nil && s.config.Session.Memory.Enabled {
		s.memoryLogger = sessions.NewMemoryLogger(s.config.Session.Memory.Directory)
	}

	provider, defaultModel, err := s.newProvider()
	if err != nil {
		return nil, err
	}
	if s.llmProvider == nil {
		s.llmProvider = provider
		s.defaultModel = defaultModel
	}
	if s.mcpManager != nil {
		if err := s.mcpManager.Start(ctx); err != nil {
			return nil, fmt.Errorf("mcp manager: %w", err)
		}
	}

	runtime := agent.NewRuntime(provider, s.sessions)
	if s.branchStore != nil {
		runtime.SetBranchStore(s.branchStore)
	}
	if defaultModel != "" {
		runtime.SetDefaultModel(defaultModel)
	}
	if system := buildSystemPrompt(s.config, SystemPromptOptions{}); system != "" {
		runtime.SetSystemPrompt(system)
	}
	runtime.SetOptions(agent.RuntimeOptions{
		MaxIterations:     s.config.Tools.Execution.MaxIterations,
		ToolParallelism:   s.config.Tools.Execution.Parallelism,
		ToolTimeout:       s.config.Tools.Execution.Timeout,
		ToolMaxAttempts:   s.config.Tools.Execution.MaxAttempts,
		ToolRetryBackoff:  s.config.Tools.Execution.RetryBackoff,
		DisableToolEvents: s.config.Tools.Execution.DisableEvents,
		MaxToolCalls:      s.config.Tools.Execution.MaxToolCalls,
		RequireApproval:   s.config.Tools.Execution.RequireApproval,
		AsyncTools:        s.config.Tools.Execution.Async,
		JobStore:          s.jobStore,
		Logger:            s.logger,
	})
	if err := s.registerTools(ctx, runtime); err != nil {
		return nil, err
	}
	if s.runtimePlugins != nil {
		if err := s.runtimePlugins.LoadTools(s.config, runtime); err != nil {
			return nil, err
		}
	}
	s.registerMCPSamplingHandler()

	// Initialize broadcast manager if configured
	if s.broadcastManager == nil && s.config.Gateway.Broadcast.Groups != nil && len(s.config.Gateway.Broadcast.Groups) > 0 {
		s.broadcastManager = NewBroadcastManager(
			BroadcastConfig{
				Strategy: BroadcastStrategy(s.config.Gateway.Broadcast.Strategy),
				Groups:   s.config.Gateway.Broadcast.Groups,
			},
			s.sessions,
			runtime,
			s.logger,
		)
	}

	s.runtime = runtime
	return runtime, nil
}

func (s *Server) newSessionStore() (sessions.Store, error) {
	if s.config.Database.URL == "" {
		return nil, errors.New("database url is required for session persistence")
	}

	poolCfg := sessions.DefaultCockroachConfig()
	if s.config.Database.MaxConnections > 0 {
		poolCfg.MaxOpenConns = s.config.Database.MaxConnections
	}
	if s.config.Database.ConnMaxLifetime > 0 {
		poolCfg.ConnMaxLifetime = s.config.Database.ConnMaxLifetime
	}

	return sessions.NewCockroachStoreFromDSN(s.config.Database.URL, poolCfg)
}

func (s *Server) newProvider() (agent.LLMProvider, string, error) {
	providerID := strings.TrimSpace(s.config.LLM.DefaultProvider)
	if providerID == "" {
		providerID = "anthropic"
	}
	providerID = strings.ToLower(providerID)

	providerCfg, ok := s.config.LLM.Providers[providerID]
	if !ok {
		return nil, "", fmt.Errorf("provider config missing for %q", providerID)
	}

	switch providerID {
	case "anthropic":
		if providerCfg.APIKey == "" {
			return nil, "", errors.New("anthropic api key is required")
		}
		provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
			APIKey:       providerCfg.APIKey,
			DefaultModel: providerCfg.DefaultModel,
			BaseURL:      providerCfg.BaseURL,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, providerCfg.DefaultModel, nil
	case "openai":
		if providerCfg.APIKey == "" {
			return nil, "", errors.New("openai api key is required")
		}
		provider := providers.NewOpenAIProviderWithConfig(providers.OpenAIConfig{
			APIKey:  providerCfg.APIKey,
			BaseURL: providerCfg.BaseURL,
		})
		return provider, providerCfg.DefaultModel, nil
	default:
		return nil, "", fmt.Errorf("unsupported provider %q", providerID)
	}
}

func (s *Server) registerTools(ctx context.Context, runtime *agent.Runtime) error {
	if s.config.Tools.Sandbox.Enabled {
		opts := []sandbox.Option{}
		backend := strings.ToLower(strings.TrimSpace(s.config.Tools.Sandbox.Backend))
		switch backend {
		case "", "docker":
			// default
		case "firecracker":
			fcConfig := firecracker.DefaultBackendConfig()
			fcConfig.NetworkEnabled = s.config.Tools.Sandbox.NetworkEnabled
			if s.config.Tools.Sandbox.PoolSize > 0 {
				fcConfig.PoolConfig.InitialSize = s.config.Tools.Sandbox.PoolSize
			}
			if s.config.Tools.Sandbox.MaxPoolSize > 0 {
				fcConfig.PoolConfig.MaxSize = s.config.Tools.Sandbox.MaxPoolSize
			}
			if s.config.Tools.Sandbox.Limits.MaxCPU > 0 {
				vcpus := int64((s.config.Tools.Sandbox.Limits.MaxCPU + 999) / 1000)
				if vcpus < 1 {
					vcpus = 1
				}
				fcConfig.DefaultVCPUs = vcpus
				fcConfig.PoolConfig.DefaultVCPUs = vcpus
			}
			if memMB, err := parseMemoryMB(s.config.Tools.Sandbox.Limits.MaxMemory); err == nil && memMB > 0 {
				fcConfig.DefaultMemMB = int64(memMB)
				fcConfig.PoolConfig.DefaultMemMB = int64(memMB)
			}
			fcBackend, err := firecracker.NewBackend(fcConfig)
			if err != nil {
				s.logger.Warn("firecracker backend unavailable, falling back to docker", "error", err)
			} else if err := fcBackend.Start(ctx); err != nil {
				s.logger.Warn("firecracker backend start failed, falling back to docker", "error", err)
				_ = fcBackend.Close()
			} else {
				sandbox.InitFirecrackerBackend(fcBackend)
				s.firecrackerBackend = fcBackend
				opts = append(opts, sandbox.WithBackend(sandbox.BackendFirecracker))
			}
		default:
			return fmt.Errorf("unsupported sandbox backend %q", backend)
		}

		if s.config.Tools.Sandbox.PoolSize > 0 {
			opts = append(opts, sandbox.WithPoolSize(s.config.Tools.Sandbox.PoolSize))
		}
		if s.config.Tools.Sandbox.MaxPoolSize > 0 {
			opts = append(opts, sandbox.WithMaxPoolSize(s.config.Tools.Sandbox.MaxPoolSize))
		}
		if s.config.Tools.Sandbox.Timeout > 0 {
			opts = append(opts, sandbox.WithDefaultTimeout(s.config.Tools.Sandbox.Timeout))
		}
		if s.config.Tools.Sandbox.Limits.MaxCPU > 0 {
			opts = append(opts, sandbox.WithDefaultCPU(s.config.Tools.Sandbox.Limits.MaxCPU))
		}
		if memMB, err := parseMemoryMB(s.config.Tools.Sandbox.Limits.MaxMemory); err == nil && memMB > 0 {
			opts = append(opts, sandbox.WithDefaultMemory(memMB))
		}
		if s.config.Tools.Sandbox.NetworkEnabled {
			opts = append(opts, sandbox.WithNetworkEnabled(true))
		}
		if err := sandbox.Register(runtime, opts...); err != nil {
			return fmt.Errorf("sandbox tool: %w", err)
		}
	}

	if s.config.Tools.Browser.Enabled {
		pool, err := browser.NewPool(browser.PoolConfig{
			Headless: s.config.Tools.Browser.Headless,
		})
		if err != nil {
			return fmt.Errorf("browser pool: %w", err)
		}
		s.browserPool = pool
		runtime.RegisterTool(browser.NewBrowserTool(pool))
	}

	if s.config.Tools.WebSearch.Enabled {
		searchConfig := &websearch.Config{
			SearXNGURL: s.config.Tools.WebSearch.URL,
		}
		switch strings.ToLower(strings.TrimSpace(s.config.Tools.WebSearch.Provider)) {
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

	if s.config.Tools.MemorySearch.Enabled {
		searchConfig := &memorysearch.Config{
			Directory:     s.config.Tools.MemorySearch.Directory,
			MemoryFile:    s.config.Tools.MemorySearch.MemoryFile,
			WorkspacePath: s.config.Workspace.Path,
			MaxResults:    s.config.Tools.MemorySearch.MaxResults,
			MaxSnippetLen: s.config.Tools.MemorySearch.MaxSnippetLen,
			Mode:          s.config.Tools.MemorySearch.Mode,
			Embeddings: memorysearch.EmbeddingsConfig{
				Provider: s.config.Tools.MemorySearch.Embeddings.Provider,
				APIKey:   s.config.Tools.MemorySearch.Embeddings.APIKey,
				BaseURL:  s.config.Tools.MemorySearch.Embeddings.BaseURL,
				Model:    s.config.Tools.MemorySearch.Embeddings.Model,
				CacheDir: s.config.Tools.MemorySearch.Embeddings.CacheDir,
				CacheTTL: s.config.Tools.MemorySearch.Embeddings.CacheTTL,
				Timeout:  s.config.Tools.MemorySearch.Embeddings.Timeout,
			},
		}
		runtime.RegisterTool(memorysearch.NewMemorySearchTool(searchConfig))
	}

	if s.jobStore != nil {
		runtime.RegisterTool(jobtools.NewStatusTool(s.jobStore))
	}

	if s.config.MCP.Enabled && s.mcpManager != nil {
		mcp.RegisterToolsWithRegistrar(runtime, s.mcpManager, s.toolPolicyResolver)
	}

	return nil
}

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

func initStorageStores(cfg *config.Config) (storage.StoreSet, error) {
	if cfg == nil || strings.TrimSpace(cfg.Database.URL) == "" {
		return storage.NewMemoryStores(), nil
	}
	dbCfg := storage.DefaultCockroachConfig()
	if cfg.Database.MaxConnections > 0 {
		dbCfg.MaxOpenConns = cfg.Database.MaxConnections
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		dbCfg.ConnMaxLifetime = cfg.Database.ConnMaxLifetime
	}
	stores, err := storage.NewCockroachStoresFromDSN(cfg.Database.URL, dbCfg)
	if err != nil {
		return storage.StoreSet{}, fmt.Errorf("storage database: %w", err)
	}
	return stores, nil
}

func registerOAuthProviders(service *auth.Service, cfg config.OAuthConfig) {
	if service == nil {
		return
	}
	if strings.TrimSpace(cfg.Google.ClientID) != "" && strings.TrimSpace(cfg.Google.ClientSecret) != "" {
		service.RegisterProvider("google", auth.NewGoogleProvider(auth.OAuthProviderConfig{
			ClientID:     cfg.Google.ClientID,
			ClientSecret: cfg.Google.ClientSecret,
			RedirectURL:  cfg.Google.RedirectURL,
		}))
	}
	if strings.TrimSpace(cfg.GitHub.ClientID) != "" && strings.TrimSpace(cfg.GitHub.ClientSecret) != "" {
		service.RegisterProvider("github", auth.NewGitHubProvider(auth.OAuthProviderConfig{
			ClientID:     cfg.GitHub.ClientID,
			ClientSecret: cfg.GitHub.ClientSecret,
			RedirectURL:  cfg.GitHub.RedirectURL,
		}))
	}
}

func (s *Server) resolveConversationID(msg *models.Message) (string, error) {
	switch msg.Channel {
	case models.ChannelTelegram:
		if msg.Metadata != nil {
			if chatID, ok := msg.Metadata["chat_id"]; ok {
				switch v := chatID.(type) {
				case int64:
					return strconv.FormatInt(v, 10), nil
				case int:
					return strconv.Itoa(v), nil
				case string:
					return v, nil
				}
			}
		}
		if msg.SessionID != "" {
			var id int64
			if _, err := fmt.Sscanf(msg.SessionID, "telegram:%d", &id); err == nil {
				return strconv.FormatInt(id, 10), nil
			}
		}
		return "", errors.New("telegram chat id missing")
	case models.ChannelSlack:
		channelID := ""
		if msg.Metadata != nil {
			if val, ok := msg.Metadata["slack_channel"].(string); ok {
				channelID = val
			}
		}
		if channelID == "" {
			return "", errors.New("slack channel id missing")
		}
		if !scopeUsesThread(s.config.Session.SlackScope) {
			return channelID, nil
		}
		threadTS := ""
		if msg.Metadata != nil {
			if val, ok := msg.Metadata["slack_thread_ts"].(string); ok {
				threadTS = val
			}
		}
		if threadTS == "" {
			if msg.Metadata != nil {
				if ts, ok := msg.Metadata["slack_ts"].(string); ok && ts != "" {
					threadTS = ts
				}
			}
		}
		if threadTS == "" {
			return channelID, nil
		}
		return fmt.Sprintf("%s:%s", channelID, threadTS), nil
	case models.ChannelDiscord:
		if msg.Metadata != nil {
			if channelID, ok := msg.Metadata["discord_channel_id"].(string); ok && channelID != "" {
				if scopeUsesThread(s.config.Session.DiscordScope) {
					if threadID, ok := msg.Metadata["discord_thread_id"].(string); ok && threadID != "" {
						return threadID, nil
					}
				}
				return channelID, nil
			}
		}
		return "", errors.New("discord channel id missing")
	default:
		return "", fmt.Errorf("unsupported channel %q", msg.Channel)
	}
}

func (s *Server) enrichMessageWithMedia(ctx context.Context, msg *models.Message) {
	if msg == nil || s.mediaAggregator == nil || s.config == nil || !s.config.Transcription.Enabled {
		return
	}
	if len(msg.Attachments) == 0 {
		return
	}

	var downloader channels.AttachmentDownloader
	if adapter, ok := s.channels.Get(msg.Channel); ok {
		if d, ok := adapter.(channels.AttachmentDownloader); ok {
			downloader = d
		}
	}

	mediaAttachments := make([]*media.Attachment, 0, len(msg.Attachments))
	for i := range msg.Attachments {
		att := msg.Attachments[i]
		mediaType := media.DetectMediaType(att.MimeType, att.Filename)
		switch strings.ToLower(att.Type) {
		case "voice":
			mediaType = media.MediaTypeAudio
			if att.MimeType == "" {
				att.MimeType = "audio/ogg"
			}
		case "audio":
			mediaType = media.MediaTypeAudio
		}
		if mediaType != media.MediaTypeAudio {
			continue
		}

		mediaAtt := &media.Attachment{
			ID:       att.ID,
			Type:     mediaType,
			MimeType: att.MimeType,
			Filename: att.Filename,
			Size:     att.Size,
			URL:      att.URL,
		}

		if downloader != nil {
			data, mimeType, filename, err := downloader.DownloadAttachment(ctx, msg, &att)
			if err != nil {
				s.logger.Warn("attachment download failed", "error", err, "channel", msg.Channel)
			} else {
				mediaAtt.Data = data
				if mimeType != "" {
					mediaAtt.MimeType = mimeType
				}
				if filename != "" {
					mediaAtt.Filename = filename
				}
			}
		}

		if len(mediaAtt.Data) == 0 && !isHTTPURL(mediaAtt.URL) {
			continue
		}

		mediaAttachments = append(mediaAttachments, mediaAtt)
	}

	if len(mediaAttachments) == 0 {
		return
	}

	opts := media.DefaultOptions()
	opts.EnableVision = false
	opts.EnableTranscription = true
	opts.TranscriptionLanguage = s.config.Transcription.Language

	mediaCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		mediaCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	content := s.mediaAggregator.Aggregate(mediaCtx, mediaAttachments, opts)
	if content == nil || content.Text == "" {
		return
	}

	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}
	msg.Metadata["media_text"] = content.Text
	if len(content.Errors) > 0 {
		msg.Metadata["media_errors"] = content.Errors
	}

	if msg.Content == "" {
		msg.Content = content.Text
	} else {
		msg.Content = msg.Content + "\n\n" + content.Text
	}
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func parseMemoryMB(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	upper := strings.ToUpper(trimmed)
	multiplier := 1.0
	switch {
	case strings.HasSuffix(upper, "GB"):
		multiplier = 1024.0
		upper = strings.TrimSuffix(upper, "GB")
	case strings.HasSuffix(upper, "G"):
		multiplier = 1024.0
		upper = strings.TrimSuffix(upper, "G")
	case strings.HasSuffix(upper, "MB"):
		upper = strings.TrimSuffix(upper, "MB")
	case strings.HasSuffix(upper, "M"):
		upper = strings.TrimSuffix(upper, "M")
	case strings.HasSuffix(upper, "KB"):
		multiplier = 1.0 / 1024.0
		upper = strings.TrimSuffix(upper, "KB")
	case strings.HasSuffix(upper, "K"):
		multiplier = 1.0 / 1024.0
		upper = strings.TrimSuffix(upper, "K")
	}
	valueFloat, err := strconv.ParseFloat(strings.TrimSpace(upper), 64)
	if err != nil {
		return 0, err
	}
	return int(valueFloat * multiplier), nil
}

func (s *Server) buildReplyMetadata(msg *models.Message) map[string]any {
	metadata := make(map[string]any)

	if msg.Metadata == nil {
		return metadata
	}

	switch msg.Channel {
	case models.ChannelTelegram:
		if chatID, ok := msg.Metadata["chat_id"]; ok {
			metadata["chat_id"] = chatID
		}
		if msg.ChannelID != "" {
			if id, err := strconv.Atoi(msg.ChannelID); err == nil {
				metadata["reply_to_message_id"] = id
			}
		}
	case models.ChannelSlack:
		if channelID, ok := msg.Metadata["slack_channel"].(string); ok {
			metadata["slack_channel"] = channelID
		}
		threadTS := ""
		if ts, ok := msg.Metadata["slack_thread_ts"].(string); ok && ts != "" {
			threadTS = ts
		} else if ts, ok := msg.Metadata["slack_ts"].(string); ok && ts != "" {
			threadTS = ts
		}
		if threadTS != "" {
			metadata["slack_thread_ts"] = threadTS
		}
	case models.ChannelDiscord:
		if threadID, ok := msg.Metadata["discord_thread_id"].(string); ok && threadID != "" {
			metadata["discord_channel_id"] = threadID
		} else if channelID, ok := msg.Metadata["discord_channel_id"].(string); ok {
			metadata["discord_channel_id"] = channelID
		}
	}

	return metadata
}

func scopeUsesThread(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "channel":
		return false
	default:
		return true
	}
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
