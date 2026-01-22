package slack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Config holds the configuration for the Slack adapter.
type Config struct {
	// BotToken is the xoxb- token for API calls (required)
	BotToken string

	// AppToken is the xapp- token for Socket Mode (required)
	AppToken string

	// RateLimit configures rate limiting for API calls (operations per second)
	// Slack's tier 1 rate limit is 1 request per second
	RateLimit float64

	// RateBurst configures the burst capacity for rate limiting
	RateBurst int

	// Logger is an optional slog.Logger instance
	Logger *slog.Logger
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.BotToken == "" {
		return channels.ErrConfig("bot_token is required", nil)
	}

	if c.AppToken == "" {
		return channels.ErrConfig("app_token is required", nil)
	}

	if c.RateLimit == 0 {
		c.RateLimit = 1 // Slack's default rate limit
	}

	if c.RateBurst == 0 {
		c.RateBurst = 5
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	return nil
}

// Adapter implements the channels.Adapter interface for Slack.
// It provides production-ready Slack integration using Socket Mode with
// structured logging, metrics collection, rate limiting, and graceful degradation.
type Adapter struct {
	cfg          Config
	client       *slack.Client
	socketClient *socketmode.Client
	messages     chan *models.Message
	status       channels.Status
	statusMu     sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	botUserID    string
	botUserIDMu  sync.RWMutex
	rateLimiter  *channels.RateLimiter
	metrics      *channels.Metrics
	logger       *slog.Logger
	degraded     bool
	degradedMu   sync.RWMutex
}

// NewAdapter creates a new Slack adapter with the given configuration.
// It validates the configuration and initializes all internal components.
func NewAdapter(cfg Config) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := slack.New(
		cfg.BotToken,
		slack.OptionAppLevelToken(cfg.AppToken),
	)

	socketClient := socketmode.New(
		client,
		socketmode.OptionDebug(false),
	)

	return &Adapter{
		cfg:          cfg,
		client:       client,
		socketClient: socketClient,
		messages:     make(chan *models.Message, 100),
		status: channels.Status{
			Connected: false,
		},
		rateLimiter: channels.NewRateLimiter(cfg.RateLimit, cfg.RateBurst),
		metrics:     channels.NewMetrics(models.ChannelSlack),
		logger:      cfg.Logger.With("adapter", "slack"),
	}, nil
}

// Start begins listening for messages from Slack via Socket Mode.
// It authenticates with Slack, retrieves bot information, and starts event processing.
func (a *Adapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	a.logger.Info("starting slack adapter", "rate_limit", a.cfg.RateLimit)

	// Get bot user ID for mention detection
	authResp, err := a.client.AuthTest()
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeAuthentication)
		return channels.ErrAuthentication("failed to authenticate with Slack", err)
	}

	a.botUserIDMu.Lock()
	a.botUserID = authResp.UserID
	a.botUserIDMu.Unlock()

	a.logger.Info("slack adapter authenticated",
		"bot_user_id", authResp.UserID,
		"team", authResp.Team)

	// Start event handler goroutine
	a.wg.Add(1)
	go a.handleEvents()

	// Start Socket Mode connection in a goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.socketClient.Run(); err != nil {
			a.updateStatus(false, fmt.Sprintf("Socket mode error: %v", err))
			a.logger.Error("socket mode error", "error", err)
			a.metrics.RecordError(channels.ErrCodeConnection)
		}
	}()

	a.updateStatus(true, "")
	a.metrics.RecordConnectionOpened()

	a.logger.Info("slack adapter started successfully")

	return nil
}

// Stop gracefully shuts down the adapter.
// It closes connections and waits for pending operations to complete.
func (a *Adapter) Stop(ctx context.Context) error {
	a.logger.Info("stopping slack adapter")

	if a.cancel != nil {
		a.cancel()
	}

	// Wait for goroutines to finish with timeout BEFORE closing messages channel
	// This prevents panics from sending on a closed channel
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Goroutines finished - safe to close messages channel now
		close(a.messages)
		a.updateStatus(false, "")
		a.metrics.RecordConnectionClosed()
		a.logger.Info("slack adapter stopped gracefully")
		return nil
	case <-ctx.Done():
		// Timeout - still close the channel but some messages may be lost
		close(a.messages)
		a.updateStatus(false, "shutdown timeout")
		a.logger.Warn("slack adapter stop timeout")
		a.metrics.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("shutdown timeout", ctx.Err())
	}
}

// Send delivers a message to Slack with rate limiting and error handling.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	startTime := time.Now()

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.logger.Warn("rate limit wait cancelled", "error", err)
		a.metrics.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	// Extract channel and thread timestamp from message metadata
	channelID, ok := msg.Metadata["slack_channel"].(string)
	if !ok {
		a.metrics.RecordMessageFailed()
		a.metrics.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("missing slack_channel in message metadata", nil)
	}

	a.logger.Debug("sending message",
		"channel_id", channelID,
		"content_length", len(msg.Content))

	options := buildBlockKitMessage(msg)

	// Add thread timestamp if this is a reply
	if threadTS, ok := msg.Metadata["slack_thread_ts"].(string); ok && threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	// Send the message
	channel, timestamp, err := a.client.PostMessageContext(ctx, channelID, options...)
	if err != nil {
		a.metrics.RecordMessageFailed()
		a.logger.Error("failed to send message",
			"error", err,
			"channel_id", channelID)

		// Classify the error
		if isRateLimitError(err) {
			a.metrics.RecordError(channels.ErrCodeRateLimit)
			return channels.ErrRateLimit("slack rate limit exceeded", err)
		}

		a.metrics.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal("failed to send Slack message", err)
	}

	// Record success metrics
	a.metrics.RecordMessageSent()
	a.metrics.RecordSendLatency(time.Since(startTime))

	a.logger.Debug("message sent successfully",
		"channel", channel,
		"timestamp", timestamp,
		"latency_ms", time.Since(startTime).Milliseconds())

	// Handle attachments (file uploads)
	if len(msg.Attachments) > 0 {
		for _, att := range msg.Attachments {
			// For file uploads, we would need the actual file data
			// This is a placeholder for the file upload logic
			a.logger.Debug("would upload file",
				"filename", att.Filename,
				"url", att.URL)
		}
	}

	// Handle reactions if specified in metadata
	if reaction, ok := msg.Metadata["slack_reaction"].(string); ok && reaction != "" {
		msgRef := slack.ItemRef{
			Channel:   channelID,
			Timestamp: timestamp,
		}
		if err := a.client.AddReactionContext(ctx, reaction, msgRef); err != nil {
			a.logger.Warn("failed to add reaction",
				"error", err,
				"reaction", reaction)
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
	return models.ChannelSlack
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

// HealthCheck performs a connectivity check with Slack's API.
// It calls the auth.test endpoint to verify authentication and connectivity.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	startTime := time.Now()

	health := channels.HealthStatus{
		LastCheck: startTime,
		Healthy:   false,
	}

	// Call auth.test to verify connectivity
	_, err := a.client.AuthTestContext(ctx)
	health.Latency = time.Since(startTime)

	if err != nil {
		health.Message = fmt.Sprintf("health check failed: %v", err)
		a.logger.Warn("health check failed",
			"error", err,
			"latency_ms", health.Latency.Milliseconds())
		return health
	}

	health.Healthy = true
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
	return a.metrics.Snapshot()
}

// handleEvents processes incoming Socket Mode events.
func (a *Adapter) handleEvents() {
	defer a.wg.Done()

	for {
		select {
		case <-a.ctx.Done():
			a.logger.Info("event handler stopped")
			return
		case event, ok := <-a.socketClient.Events:
			if !ok {
				a.logger.Info("socket client events channel closed")
				return
			}

			// Update last ping timestamp
			a.statusMu.Lock()
			a.status.LastPing = time.Now().Unix()
			a.statusMu.Unlock()

			switch event.Type {
			case socketmode.EventTypeConnecting:
				a.logger.Info("connecting to socket mode")

			case socketmode.EventTypeConnectionError:
				a.logger.Error("socket mode connection error", "data", event.Data)
				a.updateStatus(false, "connection error")
				a.metrics.RecordError(channels.ErrCodeConnection)
				a.setDegraded(true)

			case socketmode.EventTypeConnected:
				a.logger.Info("connected to socket mode")
				a.updateStatus(true, "")
				a.setDegraded(false)

			case socketmode.EventTypeEventsAPI:
				a.handleEventsAPI(event)

			case socketmode.EventTypeSlashCommand:
				// Acknowledge slash commands (can be implemented if needed)
				a.socketClient.Ack(*event.Request)
				a.logger.Debug("received slash command")

			case socketmode.EventTypeInteractive:
				// Acknowledge interactive events (can be implemented if needed)
				a.socketClient.Ack(*event.Request)
				a.logger.Debug("received interactive event")
			}
		}
	}
}

// handleEventsAPI processes Events API callbacks.
func (a *Adapter) handleEventsAPI(event socketmode.Event) {
	eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
	if !ok {
		a.logger.Warn("could not type cast event to EventsAPIEvent")
		a.socketClient.Ack(*event.Request)
		return
	}

	// Acknowledge the event
	a.socketClient.Ack(*event.Request)

	switch eventsAPIEvent.Type {
	case slackevents.CallbackEvent:
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			a.handleAppMention(ev)

		case *slackevents.MessageEvent:
			// Filter out bot messages and message subtypes we don't care about
			if ev.BotID != "" {
				return
			}
			if ev.SubType != "" && ev.SubType != "file_share" {
				return
			}

			a.handleMessage(ev)
		}
	}
}

// handleAppMention processes app mention events (@bot mentions).
func (a *Adapter) handleAppMention(event *slackevents.AppMentionEvent) {
	a.logger.Debug("received app mention",
		"user", event.User,
		"channel", event.Channel,
		"text_length", len(event.Text))

	// Convert to MessageEvent for unified handling
	msgEvent := &slackevents.MessageEvent{
		Type:            "message",
		User:            event.User,
		Text:            event.Text,
		Channel:         event.Channel,
		TimeStamp:       event.TimeStamp,
		ThreadTimeStamp: event.ThreadTimeStamp,
	}

	a.handleMessage(msgEvent)
}

// handleMessage processes incoming messages.
func (a *Adapter) handleMessage(event *slackevents.MessageEvent) {
	startTime := time.Now()

	// Check if this is a DM or if bot is mentioned
	a.botUserIDMu.RLock()
	botUserID := a.botUserID
	a.botUserIDMu.RUnlock()

	isDM := strings.HasPrefix(event.Channel, "D")
	isMention := strings.Contains(event.Text, fmt.Sprintf("<@%s>", botUserID))

	// Only process DMs, mentions, or thread replies
	if !isDM && !isMention && event.ThreadTimeStamp == "" {
		return
	}

	a.logger.Debug("processing message",
		"user", event.User,
		"channel", event.Channel,
		"is_dm", isDM,
		"is_mention", isMention)

	// Convert Slack message to unified format
	msg := convertSlackMessage(event, a.cfg.BotToken)

	// Record metrics
	a.metrics.RecordMessageReceived()
	a.metrics.RecordReceiveLatency(time.Since(startTime))

	// Send to messages channel
	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
	default:
		a.logger.Warn("messages channel full, dropping message",
			"channel", event.Channel)
		a.metrics.RecordMessageFailed()
	}
}

// updateStatus updates the connection status thread-safely.
func (a *Adapter) updateStatus(connected bool, errMsg string) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.status.Connected = connected
	a.status.Error = errMsg
	if connected {
		a.status.LastPing = time.Now().Unix()
	}
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
	return strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "rate limited") ||
		strings.Contains(errStr, "429")
}

// convertSlackMessage converts a Slack message event to the unified message format.
func convertSlackMessage(event *slackevents.MessageEvent, botToken string) *models.Message {
	// Remove bot mention from text
	text := event.Text
	// Remove <@USERID> mentions
	for strings.Contains(text, "<@") {
		start := strings.Index(text, "<@")
		end := strings.Index(text[start:], ">")
		if end == -1 {
			break
		}
		text = text[:start] + text[start+end+1:]
	}
	text = strings.TrimSpace(text)

	// Create message ID from channel and timestamp
	msgID := fmt.Sprintf("%s:%s", event.Channel, event.TimeStamp)

	// Generate session ID for threaded conversations
	sessionID := ""
	threadTS := event.ThreadTimeStamp
	if threadTS == "" {
		threadTS = event.TimeStamp // Use message timestamp if not in thread
	}
	sessionID = generateSessionID(event.Channel, threadTS)

	// Parse timestamp
	createdAt := time.Now()
	if ts, err := parseSlackTimestamp(event.TimeStamp); err == nil {
		createdAt = ts
	}

	msg := &models.Message{
		ID:        msgID,
		SessionID: sessionID,
		Channel:   models.ChannelSlack,
		ChannelID: msgID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   text,
		Metadata: map[string]any{
			"slack_user_id":   event.User,
			"slack_channel":   event.Channel,
			"slack_ts":        event.TimeStamp,
			"slack_thread_ts": event.ThreadTimeStamp,
		},
		CreatedAt: createdAt,
	}

	// Process file attachments from the Message field if present
	if event.Message != nil && len(event.Message.Files) > 0 {
		attachments := make([]models.Attachment, 0, len(event.Message.Files))
		for _, file := range event.Message.Files {
			att := models.Attachment{
				ID:       file.ID,
				Type:     getAttachmentType(file.Mimetype),
				URL:      file.URLPrivateDownload,
				Filename: file.Name,
				MimeType: file.Mimetype,
				Size:     int64(file.Size),
			}
			attachments = append(attachments, att)
		}
		msg.Attachments = attachments
	}

	return msg
}

// buildBlockKitMessage creates Block Kit formatted message options.
func buildBlockKitMessage(msg *models.Message) []slack.MsgOption {
	options := []slack.MsgOption{}

	// Add text content as a section block
	if msg.Content != "" {
		textBlock := slack.NewTextBlockObject("mrkdwn", msg.Content, false, false)
		sectionBlock := slack.NewSectionBlock(textBlock, nil, nil)
		options = append(options, slack.MsgOptionBlocks(sectionBlock))
	}

	// Add attachments as context blocks
	if len(msg.Attachments) > 0 {
		for _, att := range msg.Attachments {
			// For images, use image blocks
			if att.Type == "image" {
				imageBlock := slack.NewImageBlock(att.URL, att.Filename, "", nil)
				options = append(options, slack.MsgOptionBlocks(imageBlock))
			} else {
				// For other files, add as context
				contextText := fmt.Sprintf("Attachment: %s (%s)", att.Filename, att.MimeType)
				contextBlock := slack.NewContextBlock("",
					slack.NewTextBlockObject("mrkdwn", contextText, false, false),
				)
				options = append(options, slack.MsgOptionBlocks(contextBlock))
			}
		}
	}

	// If no blocks were added, fall back to simple text
	if len(options) == 0 && msg.Content != "" {
		options = append(options, slack.MsgOptionText(msg.Content, false))
	}

	return options
}

// getAttachmentType determines the attachment type from MIME type.
func getAttachmentType(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return "document"
	}
}

// generateSessionID creates a deterministic session ID for a Slack thread.
func generateSessionID(channel, threadTS string) string {
	data := fmt.Sprintf("slack:%s:%s", channel, threadTS)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// parseSlackTimestamp converts a Slack timestamp string to time.Time.
func parseSlackTimestamp(ts string) (time.Time, error) {
	// Slack timestamps are in the format "1234567890.123456"
	parts := strings.Split(ts, ".")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid timestamp format: %s", ts)
	}

	var sec, nsec int64
	_, err := fmt.Sscanf(ts, "%d.%d", &sec, &nsec)
	if err != nil {
		return time.Time{}, err
	}

	// Convert microseconds to nanoseconds
	nsec = nsec * 1000

	return time.Unix(sec, nsec), nil
}
