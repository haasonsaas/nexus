package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/haasonsaas/nexus/internal/channels"
	nexusmodels "github.com/haasonsaas/nexus/pkg/models"
)

// Mode represents the operation mode of the Telegram adapter.
type Mode string

const (
	// ModeLongPolling uses long polling to receive updates from Telegram
	ModeLongPolling Mode = "long_polling"

	// ModeWebhook uses webhooks to receive updates from Telegram
	ModeWebhook Mode = "webhook"
)

// Config holds configuration for the Telegram adapter.
type Config struct {
	// Token is the bot token from @BotFather (required)
	Token string

	// Mode determines whether to use long polling or webhooks
	Mode Mode

	// WebhookURL is the HTTPS URL for webhook mode (required if Mode is ModeWebhook)
	WebhookURL string

	// ListenAddr is the address for webhook server, e.g., ":8443"
	ListenAddr string

	// MaxReconnectAttempts is the maximum number of reconnection attempts
	MaxReconnectAttempts int

	// ReconnectDelay is the delay between reconnection attempts
	ReconnectDelay time.Duration

	// RateLimit configures rate limiting for API calls (operations per second)
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

	if c.Mode == "" {
		c.Mode = ModeLongPolling
	}

	if c.Mode == ModeWebhook && c.WebhookURL == "" {
		return channels.ErrConfig("webhook_url is required for webhook mode", nil)
	}

	if c.MaxReconnectAttempts == 0 {
		c.MaxReconnectAttempts = 5
	}

	if c.ReconnectDelay == 0 {
		c.ReconnectDelay = 5 * time.Second
	}

	if c.RateLimit == 0 {
		c.RateLimit = 30 // Telegram's limit is ~30 messages per second
	}

	if c.RateBurst == 0 {
		c.RateBurst = 20
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	return nil
}

// Adapter implements the channels.Adapter interface for Telegram.
// It provides production-ready message handling with structured logging,
// metrics collection, rate limiting, and graceful degradation.
type Adapter struct {
	config      Config
	bot         *bot.Bot
	messages    chan *nexusmodels.Message
	status      channels.Status
	statusMu    sync.RWMutex
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	rateLimiter *channels.RateLimiter
	metrics     *channels.Metrics
	logger      *slog.Logger
	degraded    bool
	degradedMu  sync.RWMutex
}

// NewAdapter creates a new Telegram adapter with the given configuration.
// It validates the configuration and initializes all internal components.
func NewAdapter(config Config) (*Adapter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	a := &Adapter{
		config:      config,
		messages:    make(chan *nexusmodels.Message, 100),
		status:      channels.Status{Connected: false},
		rateLimiter: channels.NewRateLimiter(config.RateLimit, config.RateBurst),
		metrics:     channels.NewMetrics(nexusmodels.ChannelTelegram),
		logger:      config.Logger.With("adapter", "telegram"),
	}

	return a, nil
}

// Start begins listening for messages from Telegram.
// It establishes the bot connection and starts the message receiving loop.
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	a.logger.Info("starting telegram adapter",
		"mode", a.config.Mode,
		"rate_limit", a.config.RateLimit)

	// Initialize bot
	opts := []bot.Option{}
	b, err := bot.New(a.config.Token, opts...)
	if err != nil {
		a.updateStatus(false, fmt.Sprintf("failed to create bot: %v", err))
		a.metrics.RecordError(channels.ErrCodeAuthentication)
		return channels.ErrAuthentication("failed to create bot", err)
	}

	a.bot = b
	a.metrics.RecordConnectionOpened()

	// Start message handler
	a.wg.Add(1)
	go a.runWithReconnection(ctx)

	a.logger.Info("telegram adapter started successfully")
	return nil
}

// runWithReconnection handles the main message loop with automatic reconnection.
func (a *Adapter) runWithReconnection(ctx context.Context) {
	defer a.wg.Done()
	defer close(a.messages)

	attempts := 0
	maxAttempts := a.config.MaxReconnectAttempts

	for {
		select {
		case <-ctx.Done():
			a.updateStatus(false, "")
			a.logger.Info("telegram adapter stopped")
			return
		default:
		}

		// Try to start the bot
		if err := a.run(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				a.updateStatus(false, "")
				return
			}

			attempts++
			a.metrics.RecordReconnectAttempt()

			errMsg := fmt.Sprintf("bot error (attempt %d/%d)", attempts, maxAttempts)
			a.updateStatus(false, errMsg)
			a.logger.Error("telegram bot error",
				"error", err,
				"attempt", attempts,
				"max_attempts", maxAttempts)

			if attempts >= maxAttempts {
				a.logger.Error("max reconnection attempts reached, stopping adapter")
				a.metrics.RecordError(channels.ErrCodeConnection)
				return
			}

			// Enter degraded mode
			a.setDegraded(true)

			// Wait before reconnecting
			select {
			case <-ctx.Done():
				a.updateStatus(false, "")
				return
			case <-time.After(a.config.ReconnectDelay):
				a.logger.Info("attempting to reconnect")
			}
			continue
		}

		// Successful run, exit degraded mode
		a.setDegraded(false)
		a.updateStatus(false, "")
		return
	}
}

// run handles the actual bot execution based on mode.
func (a *Adapter) run(ctx context.Context) error {
	a.updateStatus(true, "")

	if a.config.Mode == ModeWebhook {
		return a.runWebhook(ctx)
	}
	return a.runLongPolling(ctx)
}

// runLongPolling runs the bot in long polling mode.
func (a *Adapter) runLongPolling(ctx context.Context) error {
	a.logger.Info("starting long polling mode")

	// Register message handler
	a.bot.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, a.handleMessage)

	// Start bot (this blocks until context is cancelled)
	a.bot.Start(ctx)

	return nil
}

// runWebhook runs the bot in webhook mode.
func (a *Adapter) runWebhook(ctx context.Context) error {
	a.logger.Info("starting webhook mode", "url", a.config.WebhookURL)

	// Set webhook
	_, err := a.bot.SetWebhook(ctx, &bot.SetWebhookParams{
		URL: a.config.WebhookURL,
	})
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeConnection)
		return channels.ErrConnection("failed to set webhook", err)
	}

	// Register message handler
	a.bot.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, a.handleMessage)

	// Start webhook server
	go a.bot.StartWebhook(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	return nil
}

// handleMessage processes incoming Telegram messages.
func (a *Adapter) handleMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	startTime := time.Now()

	if update.Message == nil {
		return
	}

	a.logger.Debug("received message",
		"chat_id", update.Message.Chat.ID,
		"user_id", update.Message.From.ID,
		"text", update.Message.Text)

	msg := a.convertMessage(update.Message)

	// Record metrics
	a.metrics.RecordMessageReceived()
	a.metrics.RecordReceiveLatency(time.Since(startTime))

	select {
	case a.messages <- msg:
		a.updateLastPing()
	case <-ctx.Done():
		return
	default:
		a.logger.Warn("messages channel full, dropping message",
			"chat_id", update.Message.Chat.ID)
		a.metrics.RecordMessageFailed()
	}
}

// convertMessage converts a Telegram message to the unified format.
func (a *Adapter) convertMessage(msg *models.Message) *nexusmodels.Message {
	return convertTelegramMessage(&telegramMessageAdapter{msg})
}

// Stop gracefully shuts down the adapter.
// It waits for pending operations to complete or the context to timeout.
func (a *Adapter) Stop(ctx context.Context) error {
	a.logger.Info("stopping telegram adapter")

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
		a.metrics.RecordConnectionClosed()
		a.logger.Info("telegram adapter stopped gracefully")
		return nil
	case <-ctx.Done():
		a.metrics.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("stop timeout", ctx.Err())
	}
}

// Send delivers a message to Telegram with rate limiting and error handling.
func (a *Adapter) Send(ctx context.Context, msg *nexusmodels.Message) error {
	startTime := time.Now()

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.logger.Warn("rate limit wait cancelled", "error", err)
		a.metrics.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	// Check if bot is initialized
	if a.bot == nil {
		a.metrics.RecordMessageFailed()
		a.metrics.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal("bot not initialized", nil)
	}

	// Extract chat ID
	chatID, err := a.extractChatID(msg)
	if err != nil {
		a.logger.Error("failed to extract chat ID", "error", err)
		a.metrics.RecordMessageFailed()
		a.metrics.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("failed to extract chat ID", err)
	}

	a.logger.Debug("sending message",
		"chat_id", chatID,
		"content_length", len(msg.Content))

	// Handle message with inline keyboard if present
	params := &bot.SendMessageParams{
		ChatID: chatID,
		Text:   msg.Content,
	}

	// Check for inline keyboard in metadata
	if keyboard, ok := msg.Metadata["inline_keyboard"]; ok {
		params.ReplyMarkup = keyboard
	}

	// Check for reply to message
	if replyToID, ok := msg.Metadata["reply_to_message_id"]; ok {
		if id, ok := replyToID.(int); ok {
			params.ReplyParameters = &models.ReplyParameters{
				MessageID: id,
			}
		}
	}

	// Send the message
	sentMsg, err := a.bot.SendMessage(ctx, params)
	if err != nil {
		a.logger.Error("failed to send message",
			"error", err,
			"chat_id", chatID)
		a.metrics.RecordMessageFailed()

		// Classify the error
		if isRateLimitError(err) {
			a.metrics.RecordError(channels.ErrCodeRateLimit)
			return channels.ErrRateLimit("telegram rate limit exceeded", err)
		}

		a.metrics.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal("failed to send message", err)
	}

	// Update message with sent message ID
	msg.ChannelID = strconv.FormatInt(int64(sentMsg.ID), 10)

	// Handle attachments
	if err := a.sendAttachments(ctx, chatID, msg.Attachments); err != nil {
		a.logger.Error("failed to send attachments",
			"error", err,
			"chat_id", chatID)
		// Don't fail the whole send operation for attachment errors
	}

	// Record success metrics
	a.metrics.RecordMessageSent()
	a.metrics.RecordSendLatency(time.Since(startTime))

	a.logger.Debug("message sent successfully",
		"chat_id", chatID,
		"message_id", sentMsg.ID,
		"latency_ms", time.Since(startTime).Milliseconds())

	return nil
}

// sendAttachments sends message attachments.
func (a *Adapter) sendAttachments(ctx context.Context, chatID int64, attachments []nexusmodels.Attachment) error {
	for _, attachment := range attachments {
		// Apply rate limiting per attachment
		if err := a.rateLimiter.Wait(ctx); err != nil {
			return err
		}

		switch attachment.Type {
		case "image":
			if err := a.sendPhoto(ctx, chatID, attachment); err != nil {
				return err
			}
		case "document":
			if err := a.sendDocument(ctx, chatID, attachment); err != nil {
				return err
			}
		case "audio":
			if err := a.sendAudio(ctx, chatID, attachment); err != nil {
				return err
			}
		}
	}
	return nil
}

// sendPhoto sends a photo attachment.
func (a *Adapter) sendPhoto(ctx context.Context, chatID int64, attachment nexusmodels.Attachment) error {
	_, err := a.bot.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID: chatID,
		Photo: &models.InputFileString{
			Data: attachment.URL,
		},
	})
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeInternal)
	}
	return err
}

// sendDocument sends a document attachment.
func (a *Adapter) sendDocument(ctx context.Context, chatID int64, attachment nexusmodels.Attachment) error {
	_, err := a.bot.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileString{
			Data: attachment.URL,
		},
	})
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeInternal)
	}
	return err
}

// sendAudio sends an audio attachment.
func (a *Adapter) sendAudio(ctx context.Context, chatID int64, attachment nexusmodels.Attachment) error {
	_, err := a.bot.SendAudio(ctx, &bot.SendAudioParams{
		ChatID: chatID,
		Audio: &models.InputFileString{
			Data: attachment.URL,
		},
	})
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeInternal)
	}
	return err
}

// extractChatID extracts the chat ID from a message.
func (a *Adapter) extractChatID(msg *nexusmodels.Message) (int64, error) {
	// Try metadata first
	if chatID, ok := msg.Metadata["chat_id"]; ok {
		switch v := chatID.(type) {
		case int64:
			return v, nil
		case int:
			return int64(v), nil
		case string:
			return strconv.ParseInt(v, 10, 64)
		}
	}

	// Try to parse from SessionID (format: "telegram:chatid")
	if msg.SessionID != "" {
		var chatID int64
		_, err := fmt.Sscanf(msg.SessionID, "telegram:%d", &chatID)
		if err == nil {
			return chatID, nil
		}
	}

	return 0, errors.New("chat_id not found in message")
}

// Messages returns a channel of inbound messages.
func (a *Adapter) Messages() <-chan *nexusmodels.Message {
	return a.messages
}

// Type returns the channel type.
func (a *Adapter) Type() nexusmodels.ChannelType {
	return nexusmodels.ChannelTelegram
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

// HealthCheck performs a connectivity check with Telegram's API.
// It calls getMe to verify authentication and connectivity.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	startTime := time.Now()

	health := channels.HealthStatus{
		LastCheck: startTime,
		Healthy:   false,
	}

	// Check if bot is initialized
	if a.bot == nil {
		health.Message = "bot not initialized"
		health.Latency = time.Since(startTime)
		return health
	}

	// Call getMe to verify connectivity
	// This is a lightweight operation that verifies authentication
	_, err := a.bot.GetMe(ctx)
	health.Latency = time.Since(startTime)

	if err != nil {
		health.Message = fmt.Sprintf("health check failed: %v", err)
		a.logger.Warn("health check failed", "error", err, "latency_ms", health.Latency.Milliseconds())
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

// updateStatus updates the connection status thread-safely.
func (a *Adapter) updateStatus(connected bool, errMsg string) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.status.Connected = connected
	a.status.Error = errMsg
}

// updateLastPing updates the last ping timestamp.
func (a *Adapter) updateLastPing() {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.status.LastPing = time.Now().Unix()
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
	// Telegram returns "Too Many Requests" errors
	return errors.Is(err, context.DeadlineExceeded) ||
		(err.Error() != "" && (
			errors.Is(err, errors.New("Too Many Requests")) ||
			errors.Is(err, errors.New("429"))))
}

// telegramMessageInterface is an interface for converting messages in tests
type telegramMessageInterface interface {
	GetMessageID() int64
	GetChatID() int64
	GetText() string
	GetFrom() userInterface
	GetDate() int64
	HasPhoto() bool
	GetPhotoID() string
	HasDocument() bool
	GetDocumentID() string
	GetDocumentName() string
	GetDocumentMimeType() string
	HasAudio() bool
	GetAudioID() string
	HasVoice() bool
	GetVoiceID() string
	GetVoiceDuration() int
	GetVoiceMimeType() string
}

type userInterface interface {
	GetID() int64
	GetFirstName() string
	GetLastName() string
}

// telegramMessageAdapter adapts the Telegram message type to our interface
type telegramMessageAdapter struct {
	*models.Message
}

func (t *telegramMessageAdapter) GetMessageID() int64 {
	return int64(t.ID)
}

func (t *telegramMessageAdapter) GetChatID() int64 {
	return t.Chat.ID
}

func (t *telegramMessageAdapter) GetText() string {
	return t.Text
}

func (t *telegramMessageAdapter) GetFrom() userInterface {
	if t.From == nil {
		return &userAdapter{}
	}
	return &userAdapter{t.From}
}

func (t *telegramMessageAdapter) GetDate() int64 {
	return int64(t.Date)
}

func (t *telegramMessageAdapter) HasPhoto() bool {
	return len(t.Photo) > 0
}

func (t *telegramMessageAdapter) GetPhotoID() string {
	if len(t.Photo) > 0 {
		return t.Photo[0].FileID
	}
	return ""
}

func (t *telegramMessageAdapter) HasDocument() bool {
	return t.Document != nil
}

func (t *telegramMessageAdapter) GetDocumentID() string {
	if t.Document != nil {
		return t.Document.FileID
	}
	return ""
}

func (t *telegramMessageAdapter) GetDocumentName() string {
	if t.Document != nil {
		return t.Document.FileName
	}
	return ""
}

func (t *telegramMessageAdapter) GetDocumentMimeType() string {
	if t.Document != nil {
		return t.Document.MimeType
	}
	return ""
}

func (t *telegramMessageAdapter) HasAudio() bool {
	return t.Audio != nil
}

func (t *telegramMessageAdapter) GetAudioID() string {
	if t.Audio != nil {
		return t.Audio.FileID
	}
	return ""
}

func (t *telegramMessageAdapter) HasVoice() bool {
	return t.Voice != nil
}

func (t *telegramMessageAdapter) GetVoiceID() string {
	if t.Voice != nil {
		return t.Voice.FileID
	}
	return ""
}

func (t *telegramMessageAdapter) GetVoiceDuration() int {
	if t.Voice != nil {
		return t.Voice.Duration
	}
	return 0
}

func (t *telegramMessageAdapter) GetVoiceMimeType() string {
	if t.Voice != nil {
		return t.Voice.MimeType
	}
	// Default for Telegram voice messages
	return "audio/ogg"
}

type userAdapter struct {
	*models.User
}

func (u *userAdapter) GetID() int64 {
	if u.User == nil {
		return 0
	}
	return u.User.ID
}

func (u *userAdapter) GetFirstName() string {
	if u.User == nil {
		return ""
	}
	return u.User.FirstName
}

func (u *userAdapter) GetLastName() string {
	if u.User == nil {
		return ""
	}
	return u.User.LastName
}

// convertTelegramMessage converts a Telegram message to unified format.
// This function is extracted for testing purposes.
func convertTelegramMessage(msg telegramMessageInterface) *nexusmodels.Message {
	user := msg.GetFrom()

	m := &nexusmodels.Message{
		ID:        fmt.Sprintf("tg_%d", msg.GetMessageID()),
		SessionID: fmt.Sprintf("telegram:%d", msg.GetChatID()),
		Channel:   nexusmodels.ChannelTelegram,
		ChannelID: strconv.FormatInt(msg.GetMessageID(), 10),
		Direction: nexusmodels.DirectionInbound,
		Role:      nexusmodels.RoleUser,
		Content:   msg.GetText(),
		Metadata: map[string]any{
			"chat_id":    msg.GetChatID(),
			"user_id":    user.GetID(),
			"user_first": user.GetFirstName(),
			"user_last":  user.GetLastName(),
		},
		CreatedAt: time.Unix(msg.GetDate(), 0),
	}

	// Handle attachments
	var attachments []nexusmodels.Attachment

	if msg.HasPhoto() {
		attachments = append(attachments, nexusmodels.Attachment{
			ID:   msg.GetPhotoID(),
			Type: "image",
			URL:  msg.GetPhotoID(),
		})
	}

	if msg.HasDocument() {
		attachments = append(attachments, nexusmodels.Attachment{
			ID:       msg.GetDocumentID(),
			Type:     "document",
			URL:      msg.GetDocumentID(),
			Filename: msg.GetDocumentName(),
			MimeType: msg.GetDocumentMimeType(),
		})
	}

	if msg.HasAudio() {
		attachments = append(attachments, nexusmodels.Attachment{
			ID:   msg.GetAudioID(),
			Type: "audio",
			URL:  msg.GetAudioID(),
		})
	}

	// Voice messages are distinct from audio in Telegram API
	// They are typically OGG files recorded as voice notes
	if msg.HasVoice() {
		attachments = append(attachments, nexusmodels.Attachment{
			ID:       msg.GetVoiceID(),
			Type:     "voice",
			URL:      msg.GetVoiceID(),
			MimeType: msg.GetVoiceMimeType(),
		})
		// Mark message as containing voice for transcription handling
		m.Metadata["has_voice"] = true
		m.Metadata["voice_duration"] = msg.GetVoiceDuration()
		m.Metadata["voice_file_id"] = msg.GetVoiceID()
	}

	if len(attachments) > 0 {
		m.Attachments = attachments
	}

	return m
}
