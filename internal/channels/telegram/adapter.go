package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/haasonsaas/nexus/internal/channels"
	channelcontext "github.com/haasonsaas/nexus/internal/channels/context"
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

const telegramGeneralTopicID = 1

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
	botClient   BotClient // Interface for testability
	messages    chan *nexusmodels.Message
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	rateLimiter *channels.RateLimiter
	logger      *slog.Logger
	httpClient  *http.Client
	health      *channels.BaseHealthAdapter
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
		rateLimiter: channels.NewRateLimiter(config.RateLimit, config.RateBurst),
		logger:      config.Logger.With("adapter", "telegram"),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
	a.health = channels.NewBaseHealthAdapter(nexusmodels.ChannelTelegram, a.logger)

	return a, nil
}

// SetBotClient sets a custom BotClient implementation.
// This is primarily used for testing with mocks.
func (a *Adapter) SetBotClient(client BotClient) {
	a.botClient = client
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
		a.health.RecordError(channels.ErrCodeAuthentication)
		return channels.ErrAuthentication("failed to create bot", err)
	}

	a.bot = b
	a.botClient = newRealBotClient(b)
	a.health.RecordConnectionOpened()

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
	reconnector := &channels.Reconnector{
		Config: channels.ReconnectConfig{
			MaxAttempts:  a.config.MaxReconnectAttempts,
			InitialDelay: a.config.ReconnectDelay,
			MaxDelay:     30 * time.Second,
			Factor:       2,
			Jitter:       true,
		},
		Logger: a.logger,
		Health: a.health,
	}

	err := reconnector.Run(ctx, func(runCtx context.Context) error {
		if err := a.run(runCtx); err != nil {
			attempts++
			errMsg := fmt.Sprintf("bot error (attempt %d/%d)", attempts, a.config.MaxReconnectAttempts)
			a.updateStatus(false, errMsg)
			a.logger.Error("telegram bot error",
				"error", err,
				"attempt", attempts,
				"max_attempts", a.config.MaxReconnectAttempts)
			a.setDegraded(true)
			return err
		}

		// Successful run, exit degraded mode
		attempts = 0
		a.setDegraded(false)
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		a.logger.Error("telegram adapter stopped", "error", err)
		a.health.RecordError(channels.ErrCodeConnection)
	}
	a.updateStatus(false, "")
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

	// Register text message handler
	a.botClient.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, a.handleMessage)

	// Register handler for voice and media messages (these don't have text)
	a.botClient.RegisterHandlerMatchFunc(a.matchMediaMessage, a.handleMessage)

	// Start bot (this blocks until context is cancelled)
	a.botClient.Start(ctx)

	return nil
}

// runWebhook runs the bot in webhook mode.
func (a *Adapter) runWebhook(ctx context.Context) error {
	a.logger.Info("starting webhook mode", "url", a.config.WebhookURL)

	// Set webhook
	_, err := a.botClient.SetWebhook(ctx, &bot.SetWebhookParams{
		URL: a.config.WebhookURL,
	})
	if err != nil {
		a.health.RecordError(channels.ErrCodeConnection)
		return channels.ErrConnection("failed to set webhook", err)
	}

	// Register text message handler
	a.botClient.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, a.handleMessage)

	// Register handler for voice and media messages (these don't have text)
	a.botClient.RegisterHandlerMatchFunc(a.matchMediaMessage, a.handleMessage)

	// Start webhook server
	go a.botClient.StartWebhook(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	return nil
}

// matchMediaMessage is a custom match function that matches messages with media
// content (voice, photo, document, audio) but no text. This ensures we handle
// voice messages and other media that the text handler won't catch.
func (a *Adapter) matchMediaMessage(update *models.Update) bool {
	if update.Message == nil {
		return false
	}
	// Skip if there's text - the text handler will handle it
	if update.Message.Text != "" {
		return false
	}
	// Match if there's any media content
	return update.Message.Voice != nil ||
		update.Message.Audio != nil ||
		len(update.Message.Photo) > 0 ||
		update.Message.Document != nil
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
	a.health.RecordMessageReceived()
	a.health.RecordReceiveLatency(time.Since(startTime))

	select {
	case a.messages <- msg:
		a.updateLastPing()
	case <-ctx.Done():
		return
	default:
		a.logger.Warn("messages channel full, dropping message",
			"chat_id", update.Message.Chat.ID)
		a.health.RecordMessageFailed()
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
		a.health.RecordConnectionClosed()
		a.logger.Info("telegram adapter stopped gracefully")
		return nil
	case <-ctx.Done():
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("stop timeout", ctx.Err())
	}
}

// Send delivers a message to Telegram with rate limiting and error handling.
func (a *Adapter) Send(ctx context.Context, msg *nexusmodels.Message) error {
	startTime := time.Now()
	hasText := strings.TrimSpace(msg.Content) != ""

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.logger.Warn("rate limit wait cancelled", "error", err)
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	// Check if bot is initialized
	if a.botClient == nil {
		a.health.RecordMessageFailed()
		a.health.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal("bot not initialized", nil)
	}

	// Extract chat ID
	chatID, err := a.extractChatID(msg)
	if err != nil {
		a.logger.Error("failed to extract chat ID", "error", err)
		a.health.RecordMessageFailed()
		a.health.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("failed to extract chat ID", err)
	}
	threadID, hasThread := extractMessageThreadID(msg.Metadata)

	a.logger.Debug("sending message",
		"chat_id", chatID,
		"content_length", len(msg.Content),
		"attachments", len(msg.Attachments))

	if hasText {
		// Handle message with inline keyboard if present
		params := &bot.SendMessageParams{
			ChatID: chatID,
			Text:   msg.Content,
		}
		if hasThread {
			if sendThreadID, ok := threadIDForSend(threadID); ok {
				params.MessageThreadID = sendThreadID
			}
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
		sentMsg, err := a.botClient.SendMessage(ctx, params)
		if err != nil {
			a.logger.Error("failed to send message",
				"error", err,
				"chat_id", chatID)
			a.health.RecordMessageFailed()

			// Classify the error
			if isRateLimitError(err) {
				a.health.RecordError(channels.ErrCodeRateLimit)
				return channels.ErrRateLimit("telegram rate limit exceeded", err)
			}

			a.health.RecordError(channels.ErrCodeInternal)
			return channels.ErrInternal("failed to send message", err)
		}

		// Update message with sent message ID
		msg.ChannelID = strconv.FormatInt(int64(sentMsg.ID), 10)
	}

	// Handle attachments
	if err := a.sendAttachments(ctx, chatID, threadID, msg.Attachments); err != nil {
		a.logger.Error("failed to send attachments",
			"error", err,
			"chat_id", chatID)
		// Don't fail the whole send operation for attachment errors
	}

	// Record success metrics
	a.health.RecordMessageSent()
	a.health.RecordSendLatency(time.Since(startTime))

	a.logger.Debug("message sent successfully",
		"chat_id", chatID,
		"message_id", msg.ChannelID,
		"latency_ms", time.Since(startTime).Milliseconds())

	return nil
}

func inputFileForAttachment(att nexusmodels.Attachment) (models.InputFile, func(), error) {
	url := strings.TrimSpace(att.URL)
	if url == "" {
		return nil, nil, channels.ErrInvalidInput("attachment url is required", nil)
	}
	if strings.HasPrefix(url, "data:") {
		payload, mimeType, err := decodeDataURL(url)
		if err != nil {
			return nil, nil, err
		}
		filename := strings.TrimSpace(att.Filename)
		if filename == "" {
			filename = "attachment"
			if mimeType != "" {
				if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
					filename += exts[0]
				}
			}
		}
		return &models.InputFileUpload{
			Filename: filename,
			Data:     bytes.NewReader(payload),
		}, func() {}, nil
	}

	path := url
	if strings.HasPrefix(path, "file://") {
		path = strings.TrimPrefix(path, "file://")
	}
	if strings.TrimSpace(path) != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return nil, nil, channels.ErrInternal("open attachment file", err)
			}
			filename := strings.TrimSpace(att.Filename)
			if filename == "" {
				filename = filepath.Base(path)
			}
			cleanup := func() {
				_ = f.Close()
			}
			return &models.InputFileUpload{
				Filename: filename,
				Data:     f,
			}, cleanup, nil
		}
	}

	return &models.InputFileString{Data: url}, func() {}, nil
}

func decodeDataURL(raw string) ([]byte, string, error) {
	if !strings.HasPrefix(raw, "data:") {
		return nil, "", channels.ErrInvalidInput("data url must start with data:", nil)
	}
	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return nil, "", channels.ErrInvalidInput("invalid data url format", nil)
	}

	meta := strings.TrimPrefix(parts[0], "data:")
	payload := parts[1]

	mimeType := ""
	segments := strings.Split(meta, ";")
	if len(segments) > 0 {
		mimeType = strings.TrimSpace(segments[0])
	}
	base64Encoded := false
	for _, seg := range segments[1:] {
		if strings.EqualFold(strings.TrimSpace(seg), "base64") {
			base64Encoded = true
			break
		}
	}
	if !base64Encoded {
		return nil, mimeType, channels.ErrInvalidInput("data url must be base64 encoded", nil)
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, mimeType, channels.ErrInvalidInput("decode data url", err)
	}
	return decoded, mimeType, nil
}

// SendTypingIndicator shows a "typing" indicator in the chat.
// This is part of the StreamingAdapter interface.
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *nexusmodels.Message) error {
	if a.botClient == nil {
		return channels.ErrInternal("bot not initialized", nil)
	}

	chatID, err := a.extractChatID(msg)
	if err != nil {
		return channels.ErrInvalidInput("failed to extract chat ID", err)
	}

	params := &bot.SendChatActionParams{
		ChatID: chatID,
		Action: models.ChatActionTyping,
	}
	if threadID, ok := extractMessageThreadID(msg.Metadata); ok {
		params.MessageThreadID = threadID
	}

	_, err = a.botClient.SendChatAction(ctx, params)
	if err != nil {
		a.logger.Debug("failed to send typing indicator", "error", err, "chat_id", chatID)
		// Don't return error - typing indicators are best-effort
		return nil
	}

	return nil
}

// StartStreamingResponse sends an initial placeholder message and returns its ID.
// This is part of the StreamingAdapter interface.
func (a *Adapter) StartStreamingResponse(ctx context.Context, msg *nexusmodels.Message) (string, error) {
	if a.botClient == nil {
		return "", channels.ErrInternal("bot not initialized", nil)
	}

	chatID, err := a.extractChatID(msg)
	if err != nil {
		return "", channels.ErrInvalidInput("failed to extract chat ID", err)
	}
	threadID, hasThread := extractMessageThreadID(msg.Metadata)

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		return "", channels.ErrTimeout("rate limit wait cancelled", err)
	}

	// Send initial message with a placeholder that indicates processing
	params := &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "...",
	}
	if hasThread {
		if sendThreadID, ok := threadIDForSend(threadID); ok {
			params.MessageThreadID = sendThreadID
		}
	}
	sentMsg, err := a.botClient.SendMessage(ctx, params)
	if err != nil {
		a.logger.Error("failed to start streaming response", "error", err, "chat_id", chatID)
		a.health.RecordMessageFailed()
		return "", channels.ErrInternal("failed to send initial message", err)
	}

	a.health.RecordMessageSent()
	return strconv.Itoa(sentMsg.ID), nil
}

// UpdateStreamingResponse updates a previously sent message with new content.
// This is part of the StreamingAdapter interface.
func (a *Adapter) UpdateStreamingResponse(ctx context.Context, msg *nexusmodels.Message, messageID string, content string) error {
	if a.botClient == nil {
		return channels.ErrInternal("bot not initialized", nil)
	}

	chatID, err := a.extractChatID(msg)
	if err != nil {
		return channels.ErrInvalidInput("failed to extract chat ID", err)
	}

	msgID, err := strconv.Atoi(messageID)
	if err != nil {
		return channels.ErrInvalidInput("invalid message ID", err)
	}

	// Apply rate limiting for edits
	if err := a.rateLimiter.Wait(ctx); err != nil {
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	_, err = a.botClient.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: msgID,
		Text:      content,
	})
	if err != nil {
		// Check for "message is not modified" error which is expected
		// when content hasn't actually changed (common with streaming)
		if strings.Contains(err.Error(), "message is not modified") {
			return nil
		}
		a.logger.Debug("failed to update streaming response", "error", err, "chat_id", chatID, "message_id", msgID)
		return channels.ErrInternal("failed to edit message", err)
	}

	return nil
}

// DownloadAttachment fetches attachment bytes from Telegram without exposing the bot token.
func (a *Adapter) DownloadAttachment(ctx context.Context, msg *nexusmodels.Message, attachment *nexusmodels.Attachment) ([]byte, string, string, error) {
	if a.botClient == nil {
		return nil, "", "", channels.ErrInternal("telegram bot not initialized", nil)
	}
	if attachment == nil {
		return nil, "", "", channels.ErrInvalidInput("attachment is required", nil)
	}

	fileID := attachment.ID
	if fileID == "" && msg != nil && msg.Metadata != nil {
		if id, ok := msg.Metadata["voice_file_id"].(string); ok && id != "" {
			fileID = id
		}
	}
	if fileID == "" {
		return nil, "", "", channels.ErrInvalidInput("missing telegram file id", nil)
	}

	file, err := a.botClient.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, "", "", channels.ErrConnection("telegram getFile failed", err)
	}
	if file == nil || file.FilePath == "" {
		return nil, "", "", channels.ErrInvalidInput("telegram file path missing", nil)
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", a.config.Token, file.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", "", channels.ErrConnection("create request", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", "", channels.ErrConnection("download file", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", channels.ErrConnection(fmt.Sprintf("download failed: HTTP %d", resp.StatusCode), nil)
	}

	maxBytes := channelcontext.GetChannelInfo(string(nexusmodels.ChannelTelegram)).MaxAttachmentBytes
	if maxBytes <= 0 {
		maxBytes = 50 * 1024 * 1024
	}
	if attachment.Size > 0 && attachment.Size > maxBytes {
		return nil, "", "", channels.ErrInvalidInput(fmt.Sprintf("attachment too large (%d bytes)", attachment.Size), nil)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", "", channels.ErrInternal("read response", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", "", channels.ErrInvalidInput(fmt.Sprintf("attachment too large (%d bytes)", len(data)), nil)
	}

	mimeType := attachment.MimeType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(file.FilePath))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	filename := attachment.Filename
	if filename == "" {
		filename = filepath.Base(file.FilePath)
	}

	return data, mimeType, filename, nil
}

// sendAttachments sends message attachments.
func (a *Adapter) sendAttachments(ctx context.Context, chatID int64, threadID int, attachments []nexusmodels.Attachment) error {
	for _, attachment := range attachments {
		// Apply rate limiting per attachment
		if err := a.rateLimiter.Wait(ctx); err != nil {
			return err
		}

		switch attachment.Type {
		case "image":
			if err := a.sendPhoto(ctx, chatID, threadID, attachment); err != nil {
				return err
			}
		case "document":
			if err := a.sendDocument(ctx, chatID, threadID, attachment); err != nil {
				return err
			}
		case "audio":
			if err := a.sendAudio(ctx, chatID, threadID, attachment); err != nil {
				return err
			}
		}
	}
	return nil
}

// sendPhoto sends a photo attachment.
func (a *Adapter) sendPhoto(ctx context.Context, chatID int64, threadID int, attachment nexusmodels.Attachment) error {
	inputFile, cleanup, err := inputFileForAttachment(attachment)
	if err != nil {
		a.health.RecordError(channels.ErrCodeInvalidInput)
		return err
	}
	defer cleanup()

	params := &bot.SendPhotoParams{
		ChatID: chatID,
		Photo:  inputFile,
	}
	if sendThreadID, ok := threadIDForSend(threadID); ok {
		params.MessageThreadID = sendThreadID
	}
	_, err = a.botClient.SendPhoto(ctx, params)
	if err != nil {
		a.health.RecordError(channels.ErrCodeInternal)
	}
	return err
}

// sendDocument sends a document attachment.
func (a *Adapter) sendDocument(ctx context.Context, chatID int64, threadID int, attachment nexusmodels.Attachment) error {
	inputFile, cleanup, err := inputFileForAttachment(attachment)
	if err != nil {
		a.health.RecordError(channels.ErrCodeInvalidInput)
		return err
	}
	defer cleanup()

	params := &bot.SendDocumentParams{
		ChatID:   chatID,
		Document: inputFile,
	}
	if sendThreadID, ok := threadIDForSend(threadID); ok {
		params.MessageThreadID = sendThreadID
	}
	_, err = a.botClient.SendDocument(ctx, params)
	if err != nil {
		a.health.RecordError(channels.ErrCodeInternal)
	}
	return err
}

// sendAudio sends an audio attachment.
func (a *Adapter) sendAudio(ctx context.Context, chatID int64, threadID int, attachment nexusmodels.Attachment) error {
	inputFile, cleanup, err := inputFileForAttachment(attachment)
	if err != nil {
		a.health.RecordError(channels.ErrCodeInvalidInput)
		return err
	}
	defer cleanup()

	params := &bot.SendAudioParams{
		ChatID: chatID,
		Audio:  inputFile,
	}
	if sendThreadID, ok := threadIDForSend(threadID); ok {
		params.MessageThreadID = sendThreadID
	}
	_, err = a.botClient.SendAudio(ctx, params)
	if err != nil {
		a.health.RecordError(channels.ErrCodeInternal)
	}
	return err
}

func extractMessageThreadID(meta map[string]any) (int, bool) {
	if meta == nil {
		return 0, false
	}
	if raw, ok := meta["message_thread_id"]; ok {
		if id, ok := parseThreadID(raw); ok {
			return id, true
		}
	}
	if raw, ok := meta["thread_id"]; ok {
		if id, ok := parseThreadID(raw); ok {
			return id, true
		}
	}
	return 0, false
}

func parseThreadID(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v, true
		}
	case int64:
		if v > 0 {
			return int(v), true
		}
	case float64:
		if v > 0 {
			return int(v), true
		}
	case string:
		if v == "" {
			return 0, false
		}
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		if id, err := strconv.Atoi(trimmed); err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}

func threadIDForSend(threadID int) (int, bool) {
	if threadID <= 0 || threadID == telegramGeneralTopicID {
		return 0, false
	}
	return threadID, true
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
	if a.health == nil {
		return channels.Status{}
	}
	return a.health.Status()
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
	if a.botClient == nil {
		health.Message = "bot not initialized"
		health.Latency = time.Since(startTime)
		return health
	}

	// Call getMe to verify connectivity
	// This is a lightweight operation that verifies authentication
	_, err := a.botClient.GetMe(ctx)
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
	if a.health == nil {
		return channels.MetricsSnapshot{ChannelType: nexusmodels.ChannelTelegram}
	}
	return a.health.Metrics()
}

// updateStatus updates the connection status thread-safely.
func (a *Adapter) updateStatus(connected bool, errMsg string) {
	if a.health == nil {
		return
	}
	a.health.SetStatus(connected, errMsg)
}

// updateLastPing updates the last ping timestamp.
func (a *Adapter) updateLastPing() {
	if a.health == nil {
		return
	}
	a.health.UpdateLastPing()
}

// setDegraded sets the degraded mode flag.
func (a *Adapter) setDegraded(degraded bool) {
	if a.health == nil {
		return
	}
	a.health.SetDegraded(degraded)
}

// isDegraded returns the current degraded mode status.
func (a *Adapter) isDegraded() bool {
	if a.health == nil {
		return false
	}
	return a.health.IsDegraded()
}

// isRateLimitError checks if an error is a rate limit error.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	// Check for context deadline as a rate limit indicator
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Check error message for Telegram rate limit responses
	errStr := err.Error()
	return strings.Contains(errStr, "Too Many Requests") ||
		strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "FLOOD_WAIT") ||
		strings.Contains(errStr, "rate limit")
}

// telegramMessageInterface is an interface for converting messages in tests
type telegramMessageInterface interface {
	GetMessageID() int64
	GetChatID() int64
	GetChatType() string
	GetMessageThreadID() int
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

func (t *telegramMessageAdapter) GetChatType() string {
	return string(t.Chat.Type)
}

func (t *telegramMessageAdapter) GetMessageThreadID() int {
	return t.MessageThreadID
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
	threadID := msg.GetMessageThreadID()

	m := &nexusmodels.Message{
		ID:        fmt.Sprintf("tg_%d", msg.GetMessageID()),
		SessionID: fmt.Sprintf("telegram:%d", msg.GetChatID()),
		Channel:   nexusmodels.ChannelTelegram,
		ChannelID: strconv.FormatInt(msg.GetMessageID(), 10),
		Direction: nexusmodels.DirectionInbound,
		Role:      nexusmodels.RoleUser,
		Content:   msg.GetText(),
		Metadata: map[string]any{
			"chat_id":           msg.GetChatID(),
			"chat_type":         msg.GetChatType(),
			"user_id":           user.GetID(),
			"user_first":        user.GetFirstName(),
			"user_last":         user.GetLastName(),
			"sender_id":         strconv.FormatInt(user.GetID(), 10),
			"sender_name":       strings.TrimSpace(strings.TrimSpace(user.GetFirstName()) + " " + strings.TrimSpace(user.GetLastName())),
			"conversation_type": "group",
		},
		CreatedAt: time.Unix(msg.GetDate(), 0),
	}
	if strings.EqualFold(msg.GetChatType(), "private") || msg.GetChatType() == "" {
		m.Metadata["conversation_type"] = "dm"
	}
	if threadID > 0 {
		m.Metadata["message_thread_id"] = threadID
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
