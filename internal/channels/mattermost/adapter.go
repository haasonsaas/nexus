package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/mattermost/mattermost/server/public/model"
)

// Config holds configuration for the Mattermost adapter.
type Config struct {
	// ServerURL is the Mattermost server URL (required)
	ServerURL string

	// Token is the bot token for API calls (required)
	// Either Token or (Username + Password) must be provided
	Token string

	// Username for login-based authentication (optional)
	Username string

	// Password for login-based authentication (optional)
	Password string

	// TeamName is the default team to operate in (optional)
	TeamName string

	// RateLimit configures rate limiting for API calls (operations per second)
	RateLimit float64

	// RateBurst configures the burst capacity for rate limiting
	RateBurst int

	// Logger is an optional slog.Logger instance
	Logger *slog.Logger
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return channels.ErrConfig("server_url is required", nil)
	}

	if c.Token == "" && (c.Username == "" || c.Password == "") {
		return channels.ErrConfig("either token or username/password is required", nil)
	}

	if c.RateLimit == 0 {
		c.RateLimit = 10 // Conservative default
	}

	if c.RateBurst == 0 {
		c.RateBurst = 5
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	return nil
}

// Adapter implements the channels.Adapter interface for Mattermost.
type Adapter struct {
	cfg         Config
	client      *model.Client4
	wsClient    *model.WebSocketClient
	messages    chan *models.Message
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	botUserID   string
	botUserIDMu sync.RWMutex
	rateLimiter *channels.RateLimiter
	logger      *slog.Logger
	health      *channels.BaseHealthAdapter
}

// NewAdapter creates a new Mattermost adapter with the given configuration.
func NewAdapter(cfg Config) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := model.NewAPIv4Client(cfg.ServerURL)
	if cfg.Token != "" {
		client.SetToken(cfg.Token)
	}

	adapter := &Adapter{
		cfg:         cfg,
		client:      client,
		messages:    make(chan *models.Message, 100),
		rateLimiter: channels.NewRateLimiter(cfg.RateLimit, cfg.RateBurst),
		logger:      cfg.Logger.With("adapter", "mattermost"),
	}
	adapter.health = channels.NewBaseHealthAdapter(models.ChannelMattermost, adapter.logger)
	return adapter, nil
}

// Start begins listening for messages from Mattermost via WebSocket.
func (a *Adapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	a.logger.Info("starting mattermost adapter", "server", a.cfg.ServerURL, "rate_limit", a.cfg.RateLimit)

	// Authenticate if using username/password
	if a.cfg.Token == "" {
		user, _, err := a.client.Login(ctx, a.cfg.Username, a.cfg.Password)
		if err != nil {
			a.health.RecordError(channels.ErrCodeAuthentication)
			return channels.ErrAuthentication("failed to login to Mattermost", err)
		}
		a.setBotUserID(user.Id)
		a.logger.Info("mattermost adapter logged in", "user_id", user.Id)
	} else {
		// Get bot user info
		me, _, err := a.client.GetMe(ctx, "")
		if err != nil {
			a.health.RecordError(channels.ErrCodeAuthentication)
			return channels.ErrAuthentication("failed to get bot user info", err)
		}
		a.setBotUserID(me.Id)
		a.logger.Info("mattermost adapter authenticated", "user_id", me.Id)
	}

	// Build WebSocket URL
	wsURL := buildWebSocketURL(a.cfg.ServerURL)
	a.logger.Debug("connecting to websocket", "url", wsURL)

	// Create WebSocket client
	var err error
	a.wsClient, err = model.NewWebSocketClient4(wsURL, a.client.AuthToken)
	if err != nil {
		a.health.RecordError(channels.ErrCodeConnection)
		return channels.ErrConnection("failed to connect to Mattermost WebSocket", err)
	}

	// Start listening for events
	a.wsClient.Listen()

	// Start event handler goroutine
	a.wg.Add(1)
	go a.handleEvents()

	a.updateStatus(true, "")
	a.health.RecordConnectionOpened()

	a.logger.Info("mattermost adapter started successfully")

	return nil
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	a.logger.Info("stopping mattermost adapter")

	if a.cancel != nil {
		a.cancel()
	}

	// Close WebSocket connection
	if a.wsClient != nil {
		a.wsClient.Close()
	}

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(a.messages)
		a.updateStatus(false, "")
		a.health.RecordConnectionClosed()
		a.logger.Info("mattermost adapter stopped gracefully")
		return nil
	case <-ctx.Done():
		close(a.messages)
		a.updateStatus(false, "shutdown timeout")
		a.logger.Warn("mattermost adapter stop timeout")
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("shutdown timeout", ctx.Err())
	}
}

// Send delivers a message to Mattermost with rate limiting and error handling.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	startTime := time.Now()

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.logger.Warn("rate limit wait cancelled", "error", err)
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	// Extract channel ID from message metadata
	channelID, ok := msg.Metadata["mattermost_channel"].(string)
	if !ok {
		a.health.RecordMessageFailed()
		a.health.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("missing mattermost_channel in message metadata", nil)
	}

	a.logger.Debug("sending message",
		"channel_id", channelID,
		"content_length", len(msg.Content))

	post := &model.Post{
		ChannelId: channelID,
		Message:   msg.Content,
	}

	// Add root_id if this is a reply
	if rootID, ok := msg.Metadata["mattermost_root_id"].(string); ok && rootID != "" {
		post.RootId = rootID
	}

	// Send the message
	sentPost, _, err := a.client.CreatePost(ctx, post)
	if err != nil {
		a.health.RecordMessageFailed()
		a.logger.Error("failed to send message",
			"error", err,
			"channel_id", channelID)

		if isRateLimitError(err) {
			a.health.RecordError(channels.ErrCodeRateLimit)
			return channels.ErrRateLimit("mattermost rate limit exceeded", err)
		}

		a.health.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal("failed to send Mattermost message", err)
	}

	// Record success metrics
	a.health.RecordMessageSent()
	a.health.RecordSendLatency(time.Since(startTime))

	a.logger.Debug("message sent successfully",
		"channel_id", channelID,
		"post_id", sentPost.Id,
		"latency_ms", time.Since(startTime).Milliseconds())

	// Handle file attachments
	if len(msg.Attachments) > 0 {
		for _, att := range msg.Attachments {
			a.logger.Debug("would upload file",
				"filename", att.Filename,
				"url", att.URL)
		}
	}

	return nil
}

// Messages returns a channel of inbound messages.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelMattermost
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	if a.health == nil {
		return channels.Status{}
	}
	return a.health.Status()
}

// HealthCheck performs a connectivity check with Mattermost's API.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	startTime := time.Now()

	health := channels.HealthStatus{
		LastCheck: startTime,
		Healthy:   false,
	}

	// Call ping endpoint
	pingResp, _, err := a.client.GetPing(ctx)
	health.Latency = time.Since(startTime)

	if err != nil {
		health.Message = fmt.Sprintf("health check failed: %v", err)
		a.logger.Warn("health check failed",
			"error", err,
			"latency_ms", health.Latency.Milliseconds())
		return health
	}

	health.Healthy = pingResp == "OK"
	health.Degraded = a.isDegraded()

	if health.Degraded {
		health.Message = "operating in degraded mode"
	} else {
		health.Message = "healthy"
	}

	a.logger.Debug("health check succeeded",
		"latency_ms", health.Latency.Milliseconds(),
		"degraded", health.Degraded)

	return health
}

// Metrics returns the current metrics snapshot.
func (a *Adapter) Metrics() channels.MetricsSnapshot {
	if a.health == nil {
		return channels.MetricsSnapshot{ChannelType: models.ChannelMattermost}
	}
	return a.health.Metrics()
}

// handleEvents processes incoming WebSocket events.
func (a *Adapter) handleEvents() {
	defer a.wg.Done()

	for {
		select {
		case <-a.ctx.Done():
			a.logger.Info("event handler stopped")
			return
		case event, ok := <-a.wsClient.EventChannel:
			if !ok {
				a.logger.Info("websocket event channel closed")
				return
			}

			a.updateLastPing()

			a.handleEvent(event)
		case _, ok := <-a.wsClient.ResponseChannel:
			if !ok {
				a.logger.Info("websocket response channel closed")
				return
			}
			// Handle response events if needed
		}
	}
}

// handleEvent processes a single WebSocket event.
func (a *Adapter) handleEvent(event *model.WebSocketEvent) {
	switch event.EventType() {
	case model.WebsocketEventPosted:
		a.handlePosted(event)
	case model.WebsocketEventHello:
		a.logger.Debug("websocket hello received")
		a.updateStatus(true, "")
		a.setDegraded(false)
	case model.WebsocketEventStatusChange:
		a.logger.Debug("websocket status change", "data", event.GetData())
	}
}

// handlePosted processes new post events.
func (a *Adapter) handlePosted(event *model.WebSocketEvent) {
	startTime := time.Now()

	postData := event.GetData()["post"]
	if postData == nil {
		return
	}

	postJSON, ok := postData.(string)
	if !ok {
		return
	}

	var post model.Post
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		a.logger.Warn("failed to parse post", "error", err)
		return
	}

	// Ignore bot's own messages
	if post.UserId == a.getBotUserID() {
		return
	}

	// Check if this is a direct message or mentions the bot
	channelType, ok := event.GetData()["channel_type"].(string)
	if !ok {
		channelType = ""
	}
	isDM := channelType == "D"
	isMention := strings.Contains(post.Message, "@"+a.getBotUserID())

	// Only process DMs, mentions, or thread replies
	if !isDM && !isMention && post.RootId == "" {
		return
	}

	a.logger.Debug("processing message",
		"user", post.UserId,
		"channel", post.ChannelId,
		"is_dm", isDM,
		"is_mention", isMention)

	// Convert to unified message format
	msg := a.convertPost(&post, event.GetData())

	// Record metrics
	a.health.RecordMessageReceived()
	a.health.RecordReceiveLatency(time.Since(startTime))

	// Send to messages channel
	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
	default:
		a.logger.Warn("messages channel full, dropping message",
			"channel", post.ChannelId)
		a.health.RecordMessageFailed()
	}
}

// convertPost converts a Mattermost post to unified message format.
func (a *Adapter) convertPost(post *model.Post, eventData map[string]any) *models.Message {
	// Use a thread-scoped conversation ID (channel + thread root).
	threadID := post.RootId
	if threadID == "" {
		threadID = post.Id
	}
	conversationID := fmt.Sprintf("%s:%s", post.ChannelId, threadID)

	// Create message
	msg := &models.Message{
		ID:        post.Id,
		Channel:   models.ChannelMattermost,
		ChannelID: conversationID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   post.Message,
		Metadata: map[string]any{
			"mattermost_channel": post.ChannelId,
			"mattermost_root_id": post.RootId,
			"mattermost_user_id": post.UserId,
			"sender_id":          post.UserId,
			"group_id":           post.ChannelId,
			"conversation_type":  "group",
		},
		CreatedAt: time.Unix(post.CreateAt/1000, 0),
	}

	// Set conversation type
	if channelType, ok := eventData["channel_type"].(string); ok && channelType == "D" {
		msg.Metadata["conversation_type"] = "dm"
	}

	// Add sender name if available
	if senderName, ok := eventData["sender_name"].(string); ok {
		msg.Metadata["sender_name"] = senderName
	}

	// Process file attachments
	if len(post.FileIds) > 0 {
		attachments := make([]models.Attachment, 0, len(post.FileIds))
		for _, fileID := range post.FileIds {
			att := models.Attachment{
				ID:  fileID,
				URL: fmt.Sprintf("%s/api/v4/files/%s", a.cfg.ServerURL, fileID),
			}
			attachments = append(attachments, att)
		}
		msg.Attachments = attachments
	}

	return msg
}

// SendTypingIndicator is a no-op for Mattermost (typing not widely supported via bot).
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	// Mattermost has typing events but they're not commonly used by bots
	return nil
}

// StartStreamingResponse sends an initial placeholder message and returns its ID.
func (a *Adapter) StartStreamingResponse(ctx context.Context, msg *models.Message) (string, error) {
	if a.client == nil {
		return "", channels.ErrInternal("client not initialized", nil)
	}

	channelID, ok := msg.Metadata["mattermost_channel"].(string)
	if !ok || channelID == "" {
		return "", channels.ErrInvalidInput("missing mattermost_channel in message metadata", nil)
	}

	if err := a.rateLimiter.Wait(ctx); err != nil {
		return "", channels.ErrTimeout("rate limit wait cancelled", err)
	}

	post := &model.Post{
		ChannelId: channelID,
		Message:   "...",
	}

	if rootID, ok := msg.Metadata["mattermost_root_id"].(string); ok && rootID != "" {
		post.RootId = rootID
	}

	sentPost, _, err := a.client.CreatePost(ctx, post)
	if err != nil {
		a.logger.Error("failed to start streaming response", "error", err, "channel_id", channelID)
		a.health.RecordMessageFailed()
		return "", channels.ErrInternal("failed to send initial message", err)
	}

	a.health.RecordMessageSent()
	return sentPost.Id, nil
}

// UpdateStreamingResponse updates a previously sent message with new content.
func (a *Adapter) UpdateStreamingResponse(ctx context.Context, msg *models.Message, messageID string, content string) error {
	if a.client == nil {
		return channels.ErrInternal("client not initialized", nil)
	}

	if err := a.rateLimiter.Wait(ctx); err != nil {
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	post := &model.Post{
		Id:      messageID,
		Message: content,
	}

	_, _, err := a.client.UpdatePost(ctx, messageID, post)
	if err != nil {
		a.logger.Debug("failed to update streaming response", "error", err, "post_id", messageID)
		return channels.ErrInternal("failed to edit message", err)
	}

	return nil
}

// Helper functions

func (a *Adapter) updateStatus(connected bool, errMsg string) {
	if a.health == nil {
		return
	}
	a.health.SetStatus(connected, errMsg)
}

func (a *Adapter) updateLastPing() {
	if a.health == nil {
		return
	}
	a.health.UpdateLastPing()
}

func (a *Adapter) setDegraded(degraded bool) {
	if a.health == nil {
		return
	}
	a.health.SetDegraded(degraded)
}

func (a *Adapter) isDegraded() bool {
	if a.health == nil {
		return false
	}
	return a.health.IsDegraded()
}

func (a *Adapter) setBotUserID(id string) {
	a.botUserIDMu.Lock()
	defer a.botUserIDMu.Unlock()
	a.botUserID = id
}

func (a *Adapter) getBotUserID() string {
	a.botUserIDMu.RLock()
	defer a.botUserIDMu.RUnlock()
	return a.botUserID
}

func buildWebSocketURL(serverURL string) string {
	// Convert http(s) to ws(s)
	wsURL := strings.Replace(serverURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	return wsURL
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "rate limited") ||
		strings.Contains(errStr, "429")
}
