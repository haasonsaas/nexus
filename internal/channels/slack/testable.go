package slack

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// TestableAdapter is a variant of Adapter designed for testing.
// It allows injection of mock clients for isolated unit testing.
type TestableAdapter struct {
	cfg         Config
	apiClient   SlackAPIClient
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

// NewTestableAdapter creates a new testable Slack adapter with injected clients.
func NewTestableAdapter(cfg Config, apiClient SlackAPIClient) (*TestableAdapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	adapter := &TestableAdapter{
		cfg:         cfg,
		apiClient:   apiClient,
		messages:    make(chan *models.Message, 100),
		rateLimiter: channels.NewRateLimiter(cfg.RateLimit, cfg.RateBurst),
		logger:      cfg.Logger.With("adapter", "slack-testable"),
	}
	adapter.health = channels.NewBaseHealthAdapter(models.ChannelSlack, adapter.logger)
	return adapter, nil
}

// Type returns the channel type.
func (a *TestableAdapter) Type() models.ChannelType {
	return models.ChannelSlack
}

// Messages returns the inbound message channel.
func (a *TestableAdapter) Messages() <-chan *models.Message {
	return a.messages
}

// Status returns the current connection status.
func (a *TestableAdapter) Status() channels.Status {
	if a.health == nil {
		return channels.Status{}
	}
	return a.health.Status()
}

// Metrics returns the current metrics snapshot.
func (a *TestableAdapter) Metrics() channels.MetricsSnapshot {
	if a.health == nil {
		return channels.MetricsSnapshot{ChannelType: models.ChannelSlack}
	}
	return a.health.Metrics()
}

// Start begins the adapter with mock authentication.
func (a *TestableAdapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	authResp, err := a.apiClient.AuthTest()
	if err != nil {
		a.health.RecordError(channels.ErrCodeAuthentication)
		return channels.ErrAuthentication("failed to authenticate with Slack", err)
	}

	a.botUserIDMu.Lock()
	a.botUserID = authResp.UserID
	a.botUserIDMu.Unlock()

	a.updateStatus(true, "")
	a.health.RecordConnectionOpened()

	return nil
}

// Stop gracefully shuts down the adapter.
func (a *TestableAdapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}

	close(a.messages)

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.updateStatus(false, "")
		a.health.RecordConnectionClosed()
		return nil
	case <-ctx.Done():
		a.updateStatus(false, "shutdown timeout")
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("shutdown timeout", ctx.Err())
	}
}

// Send delivers a message to Slack via the mock client.
func (a *TestableAdapter) Send(ctx context.Context, msg *models.Message) error {
	startTime := time.Now()

	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	channelID, ok := msg.Metadata["slack_channel"].(string)
	if !ok {
		a.health.RecordMessageFailed()
		a.health.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("missing slack_channel in message metadata", nil)
	}

	options := buildBlockKitMessage(msg)

	if threadTS, ok := msg.Metadata["slack_thread_ts"].(string); ok && threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	channel, timestamp, err := a.apiClient.PostMessageContext(ctx, channelID, options...)
	if err != nil {
		a.health.RecordMessageFailed()

		if isRateLimitError(err) {
			a.health.RecordError(channels.ErrCodeRateLimit)
			return channels.ErrRateLimit("slack rate limit exceeded", err)
		}

		a.health.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal("failed to send Slack message", err)
	}

	a.health.RecordMessageSent()
	a.health.RecordSendLatency(time.Since(startTime))

	// Handle reactions if specified
	if reaction, ok := msg.Metadata["slack_reaction"].(string); ok && reaction != "" {
		msgRef := slack.ItemRef{
			Channel:   channel,
			Timestamp: timestamp,
		}
		if err := a.apiClient.AddReactionContext(ctx, reaction, msgRef); err != nil {
			a.health.RecordError(channels.ErrCodeInternal)
			a.logger.Warn("failed to add slack reaction", "error", err)
		}
	}

	return nil
}

// HealthCheck performs a connectivity check via the mock client.
func (a *TestableAdapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	startTime := time.Now()

	health := channels.HealthStatus{
		LastCheck: startTime,
		Healthy:   false,
	}

	_, err := a.apiClient.AuthTestContext(ctx)
	health.Latency = time.Since(startTime)

	if err != nil {
		health.Message = "health check failed: " + err.Error()
		return health
	}

	health.Healthy = true
	health.Degraded = a.isDegraded()

	if health.Degraded {
		health.Message = "operating in degraded mode"
	} else {
		health.Message = "healthy"
	}

	return health
}

// ProcessMessage simulates receiving a message for testing.
func (a *TestableAdapter) ProcessMessage(event *slackevents.MessageEvent) {
	startTime := time.Now()

	a.botUserIDMu.RLock()
	botUserID := a.botUserID
	a.botUserIDMu.RUnlock()

	isDM := len(event.Channel) > 0 && event.Channel[0] == 'D'
	isMention := botUserID != "" && containsMention(event.Text, botUserID)

	if !isDM && !isMention && event.ThreadTimeStamp == "" {
		return
	}

	msg := convertSlackMessage(event, a.cfg.BotToken)

	a.health.RecordMessageReceived()
	a.health.RecordReceiveLatency(time.Since(startTime))

	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
	default:
		a.health.RecordMessageFailed()
	}
}

// ProcessAppMention simulates receiving an app mention for testing.
func (a *TestableAdapter) ProcessAppMention(event *slackevents.AppMentionEvent) {
	msgEvent := &slackevents.MessageEvent{
		Type:            "message",
		User:            event.User,
		Text:            event.Text,
		Channel:         event.Channel,
		TimeStamp:       event.TimeStamp,
		ThreadTimeStamp: event.ThreadTimeStamp,
	}
	a.ProcessMessage(msgEvent)
}

// SetBotUserID sets the bot user ID for testing.
func (a *TestableAdapter) SetBotUserID(id string) {
	a.botUserIDMu.Lock()
	defer a.botUserIDMu.Unlock()
	a.botUserID = id
}

// GetBotUserID returns the bot user ID.
func (a *TestableAdapter) GetBotUserID() string {
	a.botUserIDMu.RLock()
	defer a.botUserIDMu.RUnlock()
	return a.botUserID
}

// SetDegraded sets the degraded mode flag for testing.
func (a *TestableAdapter) SetDegraded(degraded bool) {
	if a.health == nil {
		return
	}
	a.health.SetDegraded(degraded)
}

func (a *TestableAdapter) isDegraded() bool {
	if a.health == nil {
		return false
	}
	return a.health.IsDegraded()
}

func (a *TestableAdapter) updateStatus(connected bool, errMsg string) {
	if a.health == nil {
		return
	}
	a.health.SetStatus(connected, errMsg)
}

// containsMention checks if text contains a mention of the given user ID.
func containsMention(text, userID string) bool {
	mention := "<@" + userID + ">"
	return len(text) > 0 && len(userID) > 0 && contains(text, mention)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure TestableAdapter implements the required interfaces.
var _ channels.Adapter = (*TestableAdapter)(nil)
var _ channels.LifecycleAdapter = (*TestableAdapter)(nil)
var _ channels.OutboundAdapter = (*TestableAdapter)(nil)
var _ channels.InboundAdapter = (*TestableAdapter)(nil)
var _ channels.HealthAdapter = (*TestableAdapter)(nil)
