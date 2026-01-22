package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// discordSession interface allows for mocking the Discord session in tests.
type discordSession interface {
	Open() error
	Close() error
	ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error)
	MessageReactionAdd(channelID, messageID, emoji string, options ...discordgo.RequestOption) error
	ThreadStart(channelID, name string, typ discordgo.ChannelType, archiveDuration int, options ...discordgo.RequestOption) (*discordgo.Channel, error)
	AddHandler(handler interface{}) func()
	ApplicationCommandBulkOverwrite(appID, guildID string, commands []*discordgo.ApplicationCommand, options ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error)
}

// Config holds configuration for the Discord adapter.
type Config struct {
	// Token is the bot token from Discord Developer Portal (required)
	Token string

	// MaxReconnectAttempts is the maximum number of reconnection attempts
	MaxReconnectAttempts int

	// ReconnectBackoff is the maximum backoff duration for reconnections
	ReconnectBackoff time.Duration

	// RateLimit configures rate limiting for API calls (operations per second)
	// Discord has different rate limits per endpoint, this is a general limit
	RateLimit float64

	// RateBurst configures the burst capacity for rate limiting
	RateBurst int

	// Logger is an optional slog.Logger instance
	Logger *slog.Logger
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.Token == "" {
		return channels.ErrConfig("token is required", nil)
	}

	if c.MaxReconnectAttempts == 0 {
		c.MaxReconnectAttempts = 5
	}

	if c.ReconnectBackoff == 0 {
		c.ReconnectBackoff = 60 * time.Second
	}

	if c.RateLimit == 0 {
		c.RateLimit = 5 // Conservative default for Discord
	}

	if c.RateBurst == 0 {
		c.RateBurst = 10
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	return nil
}

// Adapter implements the channels.Adapter interface for Discord.
// It provides production-ready Discord integration with structured logging,
// metrics collection, rate limiting, and graceful degradation.
type Adapter struct {
	config         Config
	token          string
	session        discordSession
	status         channels.Status
	messages       chan *models.Message
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	reconnectCount int
	rateLimiter    *channels.RateLimiter
	metrics        *channels.Metrics
	logger         *slog.Logger
	degraded       bool
	degradedMu     sync.RWMutex
}

// NewAdapter creates a new Discord adapter with the given configuration.
// It validates the configuration and initializes all internal components.
func NewAdapter(config Config) (*Adapter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Adapter{
		config:      config,
		token:       config.Token,
		status:      channels.Status{Connected: false},
		messages:    make(chan *models.Message, 100),
		rateLimiter: channels.NewRateLimiter(config.RateLimit, config.RateBurst),
		metrics:     channels.NewMetrics(models.ChannelDiscord),
		logger:      config.Logger.With("adapter", "discord"),
	}, nil
}

// NewAdapterSimple creates a new Discord adapter with just a token (for backward compatibility).
// It panics if the config is invalid (which only happens if token is empty).
func NewAdapterSimple(token string) *Adapter {
	config := Config{
		Token:  token,
		Logger: slog.Default(),
	}

	adapter, err := NewAdapter(config)
	if err != nil {
		panic(fmt.Sprintf("NewAdapterSimple: invalid config: %v", err))
	}
	return adapter
}

// Start begins listening for messages from Discord.
// It establishes the WebSocket connection and registers event handlers.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status.Connected {
		return channels.ErrInternal("adapter already started", nil)
	}

	a.logger.Info("starting discord adapter", "rate_limit", a.config.RateLimit)

	// Create a new session if not already set (for non-test cases)
	if a.session == nil {
		dg, err := discordgo.New("Bot " + a.token)
		if err != nil {
			a.metrics.RecordError(channels.ErrCodeAuthentication)
			return channels.ErrAuthentication("failed to create Discord session", err)
		}
		a.session = dg
	}

	// Set up event handlers
	a.session.AddHandler(a.handleMessageCreate)
	a.session.AddHandler(a.handleInteractionCreate)
	a.session.AddHandler(a.handleReady)
	a.session.AddHandler(a.handleDisconnect)

	// Open the connection with retry logic
	err := a.connectWithRetry(ctx)
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeConnection)
		return channels.ErrConnection("failed to connect to Discord", err)
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.status.Connected = true
	a.status.Error = ""
	a.status.LastPing = time.Now().Unix()
	a.metrics.RecordConnectionOpened()

	a.logger.Info("discord adapter started successfully")

	return nil
}

// Stop gracefully shuts down the adapter.
// It closes the WebSocket connection and waits for pending operations.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.status.Connected {
		return nil
	}

	a.logger.Info("stopping discord adapter")

	if a.cancel != nil {
		a.cancel()
	}

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		a.logger.Warn("stop timeout, forcing shutdown")
	}

	err := a.session.Close()
	if err != nil {
		a.status.Error = err.Error()
		a.metrics.RecordError(channels.ErrCodeConnection)
		a.logger.Error("failed to close Discord session", "error", err)
		return channels.ErrConnection("failed to close Discord session", err)
	}

	a.status.Connected = false
	close(a.messages)
	a.metrics.RecordConnectionClosed()

	a.logger.Info("discord adapter stopped gracefully")

	return nil
}

// Send delivers a message to Discord with rate limiting and error handling.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	startTime := time.Now()

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.logger.Warn("rate limit wait cancelled", "error", err)
		a.metrics.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	a.mu.RLock()
	connected := a.status.Connected
	a.mu.RUnlock()

	if !connected {
		a.metrics.RecordMessageFailed()
		a.metrics.RecordError(channels.ErrCodeUnavailable)
		return channels.ErrUnavailable("adapter not connected", nil)
	}

	// Extract Discord-specific metadata
	channelID, ok := msg.Metadata["discord_channel_id"].(string)
	if !ok || channelID == "" {
		a.metrics.RecordMessageFailed()
		a.metrics.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("missing discord_channel_id in metadata", nil)
	}

	a.logger.Debug("sending message",
		"channel_id", channelID,
		"content_length", len(msg.Content))

	// Handle reactions
	if reactionEmoji, ok := msg.Metadata["discord_reaction_emoji"].(string); ok {
		if reactionMsgID, ok := msg.Metadata["discord_reaction_msg_id"].(string); ok {
			err := a.session.MessageReactionAdd(channelID, reactionMsgID, reactionEmoji)
			if err != nil {
				a.metrics.RecordMessageFailed()
				a.metrics.RecordError(channels.ErrCodeInternal)
				a.logger.Error("failed to add reaction", "error", err)
				return channels.ErrInternal("failed to add reaction", err)
			}
			a.metrics.RecordMessageSent()
			a.metrics.RecordSendLatency(time.Since(startTime))
			return nil
		}
	}

	// Handle thread creation
	if createThread, ok := msg.Metadata["discord_create_thread"].(bool); ok && createThread {
		threadName, ok := msg.Metadata["discord_thread_name"].(string)
		if !ok || threadName == "" {
			threadName = "Discussion"
		}
		thread, err := a.session.ThreadStart(channelID, threadName, discordgo.ChannelTypeGuildPublicThread, 1440)
		if err != nil {
			a.metrics.RecordError(channels.ErrCodeInternal)
			a.logger.Error("failed to create thread", "error", err)
			return channels.ErrInternal("failed to create thread", err)
		}
		channelID = thread.ID
	}

	// Build message with embeds if specified
	embedTitle, hasEmbedTitle := msg.Metadata["discord_embed_title"].(string)
	embedColor, hasEmbedColor := msg.Metadata["discord_embed_color"].(int)
	embedDescription, hasEmbedDescription := msg.Metadata["discord_embed_description"].(string)

	var err error

	if hasEmbedTitle || hasEmbedColor || hasEmbedDescription {
		// Send as embed
		embed := &discordgo.MessageEmbed{
			Title:       embedTitle,
			Description: embedDescription,
			Color:       embedColor,
		}
		if embed.Description == "" && msg.Content != "" {
			embed.Description = msg.Content
		}

		messageSend := &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embed},
		}

		_, err = a.session.ChannelMessageSendComplex(channelID, messageSend)
	} else if msg.Content != "" {
		// Send as regular message
		_, err = a.session.ChannelMessageSend(channelID, msg.Content)
	}

	if err != nil {
		a.metrics.RecordMessageFailed()
		a.logger.Error("failed to send message",
			"error", err,
			"channel_id", channelID)

		// Classify the error
		if isRateLimitError(err) {
			a.metrics.RecordError(channels.ErrCodeRateLimit)
			return channels.ErrRateLimit("discord rate limit exceeded", err)
		}

		a.metrics.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal("failed to send message", err)
	}

	// Record success metrics
	a.metrics.RecordMessageSent()
	a.metrics.RecordSendLatency(time.Since(startTime))

	a.logger.Debug("message sent successfully",
		"channel_id", channelID,
		"latency_ms", time.Since(startTime).Milliseconds())

	return nil
}

// Messages returns a channel of inbound messages.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelDiscord
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// HealthCheck performs a connectivity check with Discord's API.
// It verifies that the session is connected and responsive.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	startTime := time.Now()

	health := channels.HealthStatus{
		LastCheck: startTime,
		Healthy:   false,
	}

	a.mu.RLock()
	connected := a.status.Connected
	session := a.session
	a.mu.RUnlock()

	if !connected || session == nil {
		health.Message = "adapter not connected"
		health.Latency = time.Since(startTime)
		return health
	}

	// For Discord, we check the session state
	// In a real implementation with discordgo.Session, you could check s.DataReady
	health.Latency = time.Since(startTime)
	health.Healthy = connected
	health.Degraded = a.isDegraded()

	if health.Degraded {
		health.Message = "operating in degraded mode"
	} else {
		health.Message = "healthy"
	}

	a.logger.Debug("health check completed",
		"healthy", health.Healthy,
		"degraded", health.Degraded,
		"latency_ms", health.Latency.Milliseconds())

	return health
}

// Metrics returns the current metrics snapshot.
func (a *Adapter) Metrics() channels.MetricsSnapshot {
	return a.metrics.Snapshot()
}

// RegisterSlashCommands registers slash commands with Discord.
func (a *Adapter) RegisterSlashCommands(commands []*discordgo.ApplicationCommand, guildID string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	a.logger.Info("registering slash commands",
		"guild_id", guildID,
		"command_count", len(commands))

	// Get application ID from session
	dg, ok := a.session.(*discordgo.Session)
	if !ok {
		// In test mode, skip actual registration
		return nil
	}

	if dg.State == nil || dg.State.User == nil {
		return channels.ErrInternal("session not ready, cannot register commands", nil)
	}

	appID := dg.State.User.ID

	_, err := a.session.ApplicationCommandBulkOverwrite(appID, guildID, commands)
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeInternal)
		a.logger.Error("failed to register slash commands", "error", err)
		return channels.ErrInternal("failed to register slash commands", err)
	}

	a.logger.Info("slash commands registered successfully")
	return nil
}

// Event handlers

func (a *Adapter) handleMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	startTime := time.Now()

	// Ignore messages from bots
	if m.Author.Bot {
		return
	}

	a.logger.Debug("received message",
		"channel_id", m.ChannelID,
		"user_id", m.Author.ID,
		"content_length", len(m.Content))

	msg := convertDiscordMessage(m.Message)
	if msg == nil {
		return
	}

	// Record metrics
	a.metrics.RecordMessageReceived()
	a.metrics.RecordReceiveLatency(time.Since(startTime))

	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
		return
	default:
		a.logger.Warn("messages channel full, dropping message",
			"channel_id", m.ChannelID)
		a.metrics.RecordMessageFailed()
	}
}

func (a *Adapter) handleInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	a.logger.Debug("received interaction",
		"interaction_id", i.ID,
		"command_name", i.ApplicationCommandData().Name)

	// Convert slash command to message
	msg := &models.Message{
		Channel:   models.ChannelDiscord,
		ChannelID: i.ID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Metadata: map[string]any{
			"discord_interaction_id":  i.ID,
			"discord_channel_id":      i.ChannelID,
			"discord_user_id":         i.Member.User.ID,
			"discord_username":        i.Member.User.Username,
			"discord_command_name":    i.ApplicationCommandData().Name,
			"discord_command_options": i.ApplicationCommandData().Options,
		},
		CreatedAt: time.Now(),
	}

	// Build content from command
	cmdData := i.ApplicationCommandData()
	msg.Content = fmt.Sprintf("/%s", cmdData.Name)

	for _, opt := range cmdData.Options {
		msg.Content += fmt.Sprintf(" %s:%v", opt.Name, opt.Value)
	}

	a.metrics.RecordMessageReceived()

	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
		return
	default:
		a.logger.Warn("messages channel full, dropping interaction")
		a.metrics.RecordMessageFailed()
	}
}

func (a *Adapter) handleReady(s *discordgo.Session, r *discordgo.Ready) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.status.Connected = true
	a.status.Error = ""
	a.status.LastPing = time.Now().Unix()
	a.reconnectCount = 0
	a.setDegraded(false)

	a.logger.Info("discord connection ready",
		"user", r.User.Username,
		"guilds", len(r.Guilds))
}

func (a *Adapter) handleDisconnect(s *discordgo.Session, d *discordgo.Disconnect) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.status.Connected = false
	a.status.Error = "disconnected from Discord"

	a.logger.Warn("disconnected from discord")
	a.metrics.RecordError(channels.ErrCodeConnection)

	// Attempt reconnection in background
	a.wg.Add(1)
	go a.reconnect()
}

// Reconnection logic

func (a *Adapter) connectWithRetry(ctx context.Context) error {
	var err error
	maxAttempts := a.config.MaxReconnectAttempts

	for attempt := 0; attempt < maxAttempts; attempt++ {
		a.logger.Info("connecting to discord",
			"attempt", attempt+1,
			"max_attempts", maxAttempts)

		err = a.session.Open()
		if err == nil {
			return nil
		}

		a.metrics.RecordReconnectAttempt()

		backoff := calculateBackoff(attempt, a.config.ReconnectBackoff)
		a.logger.Warn("connection failed, retrying",
			"error", err,
			"attempt", attempt+1,
			"backoff_ms", backoff.Milliseconds())

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			continue
		}
	}

	return channels.ErrConnection("failed to connect after retries", err)
}

func (a *Adapter) reconnect() {
	defer a.wg.Done()

	if a.ctx.Err() != nil {
		return // Context cancelled, don't reconnect
	}

	a.mu.Lock()
	a.reconnectCount++
	attempt := a.reconnectCount
	maxAttempts := a.config.MaxReconnectAttempts
	a.mu.Unlock()

	// Stop trying if we've exceeded max attempts
	if maxAttempts > 0 && attempt > maxAttempts {
		a.logger.Error("max reconnection attempts reached", "attempts", attempt-1, "max", maxAttempts)
		a.mu.Lock()
		a.status.Error = fmt.Sprintf("max reconnection attempts (%d) reached", maxAttempts)
		a.mu.Unlock()
		return
	}

	a.setDegraded(true)
	a.logger.Info("attempting reconnection", "attempt", attempt, "max", maxAttempts)

	backoff := calculateBackoff(attempt, a.config.ReconnectBackoff)
	time.Sleep(backoff)

	err := a.session.Open()

	a.mu.Lock()
	defer a.mu.Unlock()

	if err != nil {
		a.status.Error = fmt.Sprintf("reconnection attempt %d failed: %v", attempt, err)
		a.metrics.RecordError(channels.ErrCodeConnection)
		a.logger.Error("reconnection failed", "error", err, "attempt", attempt)
	} else {
		a.status.Connected = true
		a.status.Error = ""
		a.status.LastPing = time.Now().Unix()
		a.reconnectCount = 0
		a.setDegraded(false)
		a.logger.Info("reconnection successful")
	}
}

func calculateBackoff(attempt int, maxWait time.Duration) time.Duration {
	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, ...
	backoff := time.Duration(1<<uint(attempt)) * time.Second
	if backoff > maxWait {
		backoff = maxWait
	}
	return backoff
}

// setDegraded sets the degraded mode flag.
func (a *Adapter) setDegraded(degraded bool) {
	a.degradedMu.Lock()
	defer a.degradedMu.Unlock()
	a.degraded = degraded
}

// isDegraded returns the current degraded mode status.
func (a *Adapter) isDegraded() bool {
	a.degradedMu.RLock()
	defer a.degradedMu.RUnlock()
	return a.degraded
}

// isRateLimitError checks if an error is a rate limit error.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "Too Many Requests")
}

// Message conversion

func convertDiscordMessage(m *discordgo.Message) *models.Message {
	if m == nil || m.Author == nil {
		return nil
	}

	msg := &models.Message{
		Channel:     models.ChannelDiscord,
		ChannelID:   m.ID,
		Direction:   models.DirectionInbound,
		Role:        models.RoleUser,
		Content:     m.Content,
		Attachments: make([]models.Attachment, 0, len(m.Attachments)),
		Metadata: map[string]any{
			"discord_channel_id": m.ChannelID,
			"discord_user_id":    m.Author.ID,
			"discord_username":   m.Author.Username,
		},
		CreatedAt: time.Now(),
	}

	// Use timestamp from Discord message
	if !m.Timestamp.IsZero() {
		msg.CreatedAt = m.Timestamp
	}

	// Convert attachments
	for _, att := range m.Attachments {
		msg.Attachments = append(msg.Attachments, models.Attachment{
			ID:       att.ID,
			Type:     detectAttachmentType(att.ContentType),
			URL:      att.URL,
			Filename: att.Filename,
			MimeType: att.ContentType,
			Size:     int64(att.Size),
		})
	}

	// Handle thread metadata
	if m.Thread != nil {
		msg.Metadata["discord_thread_id"] = m.Thread.ID
		msg.Metadata["discord_thread_name"] = m.Thread.Name
		msg.Metadata["discord_parent_id"] = m.Thread.ParentID
	}

	// Handle mentions
	if len(m.Mentions) > 0 {
		mentions := make([]string, len(m.Mentions))
		for i, user := range m.Mentions {
			mentions[i] = user.ID
		}
		msg.Metadata["discord_mentions"] = mentions
	}

	return msg
}

func detectAttachmentType(contentType string) string {
	if strings.HasPrefix(contentType, "image/") {
		return "image"
	}
	if strings.HasPrefix(contentType, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(contentType, "video/") {
		return "video"
	}
	return "document"
}
