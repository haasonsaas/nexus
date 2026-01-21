package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	ModeLongPolling Mode = "long_polling"
	ModeWebhook     Mode = "webhook"
)

// LogLevel represents the logging verbosity.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// Config holds configuration for the Telegram adapter.
type Config struct {
	Token      string
	Mode       Mode
	WebhookURL string // Required for webhook mode
	ListenAddr string // Address for webhook server, e.g., ":8443"
	LogLevel   LogLevel

	// Reconnection settings
	MaxReconnectAttempts int
	ReconnectDelay       time.Duration
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Token == "" {
		return errors.New("token is required")
	}

	if c.Mode == "" {
		c.Mode = ModeLongPolling
	}

	if c.Mode == ModeWebhook && c.WebhookURL == "" {
		return errors.New("webhook_url is required for webhook mode")
	}

	if c.LogLevel == "" {
		c.LogLevel = LogLevelInfo
	}

	if c.MaxReconnectAttempts == 0 {
		c.MaxReconnectAttempts = 5
	}

	if c.ReconnectDelay == 0 {
		c.ReconnectDelay = 5 * time.Second
	}

	return nil
}

// Adapter implements the channels.Adapter interface for Telegram.
type Adapter struct {
	config   Config
	bot      *bot.Bot
	messages chan *nexusmodels.Message
	status   channels.Status
	statusMu sync.RWMutex
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewAdapter creates a new Telegram adapter.
func NewAdapter(config Config) (*Adapter, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	a := &Adapter{
		config:   config,
		messages: make(chan *nexusmodels.Message, 100),
		status: channels.Status{
			Connected: false,
		},
	}

	return a, nil
}

// Start begins listening for messages from Telegram.
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Initialize bot
	opts := []bot.Option{}

	if a.config.LogLevel == LogLevelError {
		// Disable debug logging
		opts = append(opts, bot.WithDebug())
	}

	b, err := bot.New(a.config.Token, opts...)
	if err != nil {
		a.updateStatus(false, fmt.Sprintf("failed to create bot: %v", err))
		return fmt.Errorf("failed to create bot: %w", err)
	}

	a.bot = b

	// Start message handler
	a.wg.Add(1)
	go a.runWithReconnection(ctx)

	return nil
}

// runWithReconnection handles the main message loop with reconnection logic.
func (a *Adapter) runWithReconnection(ctx context.Context) {
	defer a.wg.Done()
	defer close(a.messages)

	attempts := 0
	maxAttempts := a.config.MaxReconnectAttempts

	for {
		select {
		case <-ctx.Done():
			a.updateStatus(false, "")
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
			errMsg := fmt.Sprintf("bot error (attempt %d/%d): %v", attempts, maxAttempts, err)
			a.updateStatus(false, errMsg)
			a.logError(errMsg)

			if attempts >= maxAttempts {
				a.logError("max reconnection attempts reached, stopping adapter")
				return
			}

			// Wait before reconnecting
			select {
			case <-ctx.Done():
				a.updateStatus(false, "")
				return
			case <-time.After(a.config.ReconnectDelay):
				a.logInfo("attempting to reconnect...")
			}
			continue
		}

		// Successful run, reset attempts
		attempts = 0
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
	a.logInfo("starting long polling mode")

	// Register message handler
	a.bot.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, a.handleMessage)

	// Start bot
	a.bot.Start(ctx)

	return nil
}

// runWebhook runs the bot in webhook mode.
func (a *Adapter) runWebhook(ctx context.Context) error {
	a.logInfo("starting webhook mode")

	// Set webhook
	_, err := a.bot.SetWebhook(ctx, &bot.SetWebhookParams{
		URL: a.config.WebhookURL,
	})
	if err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	// Register message handler
	a.bot.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, a.handleMessage)

	// Start webhook server
	listenAddr := a.config.ListenAddr
	if listenAddr == "" {
		listenAddr = ":8443"
	}

	go a.bot.StartWebhook(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	return nil
}

// handleMessage processes incoming Telegram messages.
func (a *Adapter) handleMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	msg := a.convertMessage(update.Message)

	select {
	case a.messages <- msg:
		a.updateLastPing()
	case <-ctx.Done():
		return
	}
}

// convertMessage converts a Telegram message to the unified format.
func (a *Adapter) convertMessage(msg *models.Message) *nexusmodels.Message {
	return convertTelegramMessage(&telegramMessageAdapter{msg})
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
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
		a.logInfo("adapter stopped gracefully")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop timeout: %w", ctx.Err())
	}
}

// Send delivers a message to Telegram.
func (a *Adapter) Send(ctx context.Context, msg *nexusmodels.Message) error {
	if a.bot == nil {
		return errors.New("bot not initialized")
	}

	chatID, err := a.extractChatID(msg)
	if err != nil {
		return fmt.Errorf("failed to extract chat ID: %w", err)
	}

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

	sentMsg, err := a.bot.SendMessage(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Update message with sent message ID
	msg.ChannelID = strconv.FormatInt(int64(sentMsg.ID), 10)

	// Handle attachments
	if err := a.sendAttachments(ctx, chatID, msg.Attachments); err != nil {
		a.logError(fmt.Sprintf("failed to send attachments: %v", err))
	}

	return nil
}

// sendAttachments sends message attachments.
func (a *Adapter) sendAttachments(ctx context.Context, chatID int64, attachments []nexusmodels.Attachment) error {
	for _, attachment := range attachments {
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
		// Simple parsing - in production, use proper format
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

// updateStatus updates the connection status.
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

// Logging helpers
func (a *Adapter) logInfo(msg string) {
	if a.config.LogLevel == LogLevelInfo || a.config.LogLevel == LogLevelDebug {
		log.Printf("[Telegram] INFO: %s", msg)
	}
}

func (a *Adapter) logError(msg string) {
	if a.config.LogLevel != "" {
		log.Printf("[Telegram] ERROR: %s", msg)
	}
}

// telegramMessageAdapter is an interface for converting messages in tests
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
	return t.Photo != nil && len(t.Photo) > 0
}

func (t *telegramMessageAdapter) GetPhotoID() string {
	if t.Photo != nil && len(t.Photo) > 0 {
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
			URL:  msg.GetPhotoID(), // In production, this would be resolved to actual URL
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

	if len(attachments) > 0 {
		m.Attachments = attachments
	}

	return m
}
