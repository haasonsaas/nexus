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

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/discord"
	"github.com/haasonsaas/nexus/internal/channels/slack"
	"github.com/haasonsaas/nexus/internal/channels/telegram"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	"github.com/haasonsaas/nexus/internal/tools/sandbox"
	"github.com/haasonsaas/nexus/internal/tools/websearch"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Server is the main Nexus gateway server.
type Server struct {
	config   *config.Config
	grpc     *grpc.Server
	channels *channels.Registry
	logger   *slog.Logger
	wg       sync.WaitGroup
	cancel   context.CancelFunc

	handleMessageHook func(context.Context, *models.Message)

	runtimeMu sync.Mutex
	runtime   *agent.Runtime
	sessions  sessions.Store

	browserPool  *browser.Pool
	memoryLogger *sessions.MemoryLogger
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
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingInterceptor(logger),
			authUnaryInterceptor(cfg.Auth.JWTSecret, logger),
		),
		grpc.ChainStreamInterceptor(
			streamLoggingInterceptor(logger),
			authStreamInterceptor(cfg.Auth.JWTSecret, logger),
		),
	)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("nexus", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable reflection for development
	reflection.Register(grpcServer)

	server := &Server{
		config:   cfg,
		grpc:     grpcServer,
		channels: channels.NewRegistry(),
		logger:   logger,
	}

	if err := server.registerChannelsFromConfig(); err != nil {
		return nil, err
	}

	return server, nil
}

// Start begins serving requests.
func (s *Server) Start(ctx context.Context) error {
	// Start channel adapters
	if err := s.channels.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start channels: %w", err)
	}

	// Start message processing
	s.startProcessing(ctx)

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

	return nil
}

func (s *Server) startProcessing(ctx context.Context) {
	processCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go s.processMessages(processCtx)
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

	if err := s.sessions.AppendMessage(ctx, session.ID, msg); err != nil {
		s.logger.Error("failed to persist inbound message", "error", err)
	}
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(msg); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
		}
	}

	promptCtx := ctx
	if systemPrompt := s.systemPromptForMessage(session, msg); systemPrompt != "" {
		promptCtx = agent.WithSystemPrompt(promptCtx, systemPrompt)
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

	adapter, ok := s.channels.Get(msg.Channel)
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

	if err := s.sessions.AppendMessage(ctx, session.ID, outbound); err != nil {
		s.logger.Error("failed to persist outbound message", "error", err)
	}
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(outbound); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
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
	if s.memoryLogger == nil && s.config.Session.Memory.Enabled {
		s.memoryLogger = sessions.NewMemoryLogger(s.config.Session.Memory.Directory)
	}

	provider, defaultModel, err := s.newProvider()
	if err != nil {
		return nil, err
	}

	runtime := agent.NewRuntime(provider, s.sessions)
	if defaultModel != "" {
		runtime.SetDefaultModel(defaultModel)
	}
	if system := buildSystemPrompt(s.config, SystemPromptOptions{}); system != "" {
		runtime.SetSystemPrompt(system)
	}
	if err := s.registerTools(runtime); err != nil {
		return nil, err
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

func (s *Server) registerTools(runtime *agent.Runtime) error {
	if s.config.Tools.Sandbox.Enabled {
		opts := []sandbox.Option{}
		if s.config.Tools.Sandbox.PoolSize > 0 {
			opts = append(opts, sandbox.WithPoolSize(s.config.Tools.Sandbox.PoolSize))
		}
		if s.config.Tools.Sandbox.Timeout > 0 {
			opts = append(opts, sandbox.WithDefaultTimeout(s.config.Tools.Sandbox.Timeout))
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

	return nil
}

func (s *Server) registerChannelsFromConfig() error {
	if s.config.Channels.Telegram.Enabled {
		if s.config.Channels.Telegram.BotToken == "" {
			return errors.New("telegram bot token is required")
		}
		mode := telegram.ModeLongPolling
		webhookURL := strings.TrimSpace(s.config.Channels.Telegram.Webhook)
		if webhookURL != "" {
			mode = telegram.ModeWebhook
		}
		adapter, err := telegram.NewAdapter(telegram.Config{
			Token:      s.config.Channels.Telegram.BotToken,
			Mode:       mode,
			WebhookURL: webhookURL,
			Logger:     s.logger,
		})
		if err != nil {
			return err
		}
		s.channels.Register(adapter)
	}

	if s.config.Channels.Discord.Enabled {
		if s.config.Channels.Discord.BotToken == "" {
			return errors.New("discord bot token is required")
		}
		adapter, err := discord.NewAdapter(discord.Config{
			Token:  s.config.Channels.Discord.BotToken,
			Logger: s.logger,
		})
		if err != nil {
			return err
		}
		s.channels.Register(adapter)
	}

	if s.config.Channels.Slack.Enabled {
		if s.config.Channels.Slack.BotToken == "" || s.config.Channels.Slack.AppToken == "" {
			return errors.New("slack bot token and app token are required")
		}
		adapter, err := slack.NewAdapter(slack.Config{
			BotToken: s.config.Channels.Slack.BotToken,
			AppToken: s.config.Channels.Slack.AppToken,
			Logger:   s.logger,
		})
		if err != nil {
			return err
		}
		s.channels.Register(adapter)
	}

	return nil
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
			channelID, _ = msg.Metadata["slack_channel"].(string)
		}
		if channelID == "" {
			return "", errors.New("slack channel id missing")
		}
		if !scopeUsesThread(s.config.Session.SlackScope) {
			return channelID, nil
		}
		threadTS := ""
		if msg.Metadata != nil {
			threadTS, _ = msg.Metadata["slack_thread_ts"].(string)
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

func authUnaryInterceptor(secret string, logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if secret == "" {
			return handler(ctx, req)
		}

		token, err := extractBearerToken(ctx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "missing credentials")
		}
		if err := validateJWT(secret, token); err != nil {
			logger.Warn("jwt validation failed", "error", err)
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(ctx, req)
	}
}

func authStreamInterceptor(secret string, logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if secret == "" {
			return handler(srv, ss)
		}

		token, err := extractBearerToken(ss.Context())
		if err != nil {
			return status.Error(codes.Unauthenticated, "missing credentials")
		}
		if err := validateJWT(secret, token); err != nil {
			logger.Warn("jwt validation failed", "error", err)
			return status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(srv, ss)
	}
}

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.New("metadata missing")
	}
	for _, value := range md.Get("authorization") {
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "bearer ") {
			return strings.TrimSpace(value[len("bearer "):]), nil
		}
	}
	return "", errors.New("authorization token missing")
}

func validateJWT(secret, token string) error {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return err
	}
	if !parsed.Valid {
		return errors.New("token invalid")
	}
	return nil
}
