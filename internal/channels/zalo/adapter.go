package zalo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

const (
	// ZaloAPIBase is the Zalo Bot API endpoint.
	ZaloAPIBase = "https://bot-api.zaloplatforms.com"

	// DefaultMaxWebhookBodyBytes is the maximum allowed webhook payload size.
	DefaultMaxWebhookBodyBytes = 1 << 20

	// DefaultPollTimeout is the default long-polling timeout in seconds.
	DefaultPollTimeout = 30

	// TextChunkLimit is the maximum characters per message.
	TextChunkLimit = 2000
)

// ZaloAdapter implements the channel adapter for Zalo Bot API.
// Supports both webhook and polling modes for receiving messages.
//
// The adapter handles:
// - Text messages and image messages
// - Long polling for development/testing
// - Webhook for production deployments
//
// Thread Safety:
// ZaloAdapter is safe for concurrent use.
type ZaloAdapter struct {
	token         string
	webhookURL    string
	webhookSecret string
	webhookPath   string
	pollTimeout   int

	messages chan *models.Message
	client   *http.Client
	logger   *slog.Logger
	health   *channels.BaseHealthAdapter

	mu      sync.RWMutex
	running bool
	cancel  context.CancelFunc
	botID   string
	botName string
}

// ZaloConfig holds configuration for the Zalo adapter.
type ZaloConfig struct {
	// Token is the Zalo bot token (required)
	Token string

	// WebhookURL is the public URL for webhook callbacks (optional)
	// If set, webhook mode is used; otherwise polling mode
	WebhookURL string

	// WebhookSecret is the secret for validating webhook signatures
	WebhookSecret string

	// WebhookPath is the path for webhook endpoint (default: /webhook/zalo)
	WebhookPath string

	// PollTimeout is the long-polling timeout in seconds (default: 30)
	PollTimeout int

	// Logger is an optional logger for adapter diagnostics.
	Logger *slog.Logger
}

// NewZaloAdapter creates a new Zalo channel adapter.
func NewZaloAdapter(cfg ZaloConfig) (*ZaloAdapter, error) {
	if cfg.Token == "" {
		return nil, errors.New("zalo: token is required")
	}

	pollTimeout := cfg.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = DefaultPollTimeout
	}

	webhookPath := cfg.WebhookPath
	if webhookPath == "" {
		webhookPath = "/webhook/zalo"
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("adapter", "zalo")
	health := channels.NewBaseHealthAdapter(models.ChannelZalo, logger)

	return &ZaloAdapter{
		token:         cfg.Token,
		webhookURL:    cfg.WebhookURL,
		webhookSecret: cfg.WebhookSecret,
		webhookPath:   webhookPath,
		pollTimeout:   pollTimeout,
		messages:      make(chan *models.Message, 100),
		client: &http.Client{
			Timeout: time.Duration(pollTimeout+10) * time.Second,
		},
		logger: logger,
		health: health,
	}, nil
}

// Start begins receiving messages from Zalo.
func (a *ZaloAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("zalo: adapter already running")
	}
	a.running = true
	ctx, a.cancel = context.WithCancel(ctx)
	a.mu.Unlock()

	// Validate token and get bot info
	if err := a.validateToken(ctx); err != nil {
		if a.health != nil {
			a.health.SetStatus(false, err.Error())
			a.health.RecordError(channels.ErrCodeAuthentication)
		}
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		return fmt.Errorf("zalo: failed to validate token: %w", err)
	}

	if a.webhookURL != "" {
		// Webhook mode - set webhook and wait for HTTP callbacks
		if err := a.setWebhook(ctx); err != nil {
			if a.health != nil {
				a.health.SetStatus(false, err.Error())
				a.health.RecordError(channels.GetErrorCode(err))
			}
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
			return fmt.Errorf("zalo: failed to set webhook: %w", err)
		}
		// In webhook mode, messages come via HTTP handler
		if a.health != nil {
			a.health.SetStatus(true, "")
			a.health.RecordConnectionOpened()
		}
		return nil
	}

	// Polling mode
	if a.health != nil {
		a.health.SetStatus(true, "")
		a.health.RecordConnectionOpened()
	}
	go a.pollLoop(ctx)
	return nil
}

// Stop stops the adapter.
func (a *ZaloAdapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	if a.cancel != nil {
		a.cancel()
	}

	a.running = false
	if a.health != nil {
		a.health.SetStatus(false, "")
		a.health.RecordConnectionClosed()
	}
	return nil
}

// Send sends a message to Zalo.
func (a *ZaloAdapter) Send(ctx context.Context, msg *models.Message) error {
	if msg == nil {
		return errors.New("zalo: message is nil")
	}

	chatID := msg.ChannelID
	if chatID == "" {
		return errors.New("zalo: channel_id (chat_id) is required")
	}

	recordSend := func(err error, start time.Time) error {
		if a.health == nil {
			return err
		}
		if err != nil {
			a.health.RecordMessageFailed()
			a.health.RecordError(channels.GetErrorCode(err))
			return err
		}
		a.health.RecordMessageSent()
		a.health.RecordSendLatency(time.Since(start))
		return nil
	}

	// Handle media attachments
	for _, att := range msg.Attachments {
		if att.Type == "image" && att.URL != "" {
			start := time.Now()
			if err := recordSend(a.sendPhoto(ctx, chatID, att.URL, msg.Content), start); err != nil {
				return err
			}
			// If we sent a photo with caption, don't send text separately
			if msg.Content != "" {
				return nil
			}
		}
	}

	// Send text message
	if msg.Content != "" {
		start := time.Now()
		if err := recordSend(a.sendText(ctx, chatID, msg.Content), start); err != nil {
			return err
		}
	}

	return nil
}

// Messages returns the channel for receiving inbound messages.
func (a *ZaloAdapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *ZaloAdapter) Type() models.ChannelType {
	return models.ChannelZalo
}

// Status returns the current connection status.
func (a *ZaloAdapter) Status() channels.Status {
	if a.health == nil {
		return channels.Status{}
	}
	return a.health.Status()
}

// HealthCheck performs a connectivity check.
func (a *ZaloAdapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	if a.health == nil {
		return channels.HealthStatus{Healthy: false, Message: "health adapter unavailable", LastCheck: time.Now()}
	}
	return a.health.HealthCheck(ctx)
}

// Metrics returns the current metrics snapshot.
func (a *ZaloAdapter) Metrics() channels.MetricsSnapshot {
	if a.health == nil {
		return channels.MetricsSnapshot{ChannelType: models.ChannelZalo}
	}
	return a.health.Metrics()
}

// HandleWebhook processes incoming webhook requests.
// This should be called by an HTTP handler when webhooks are configured.
func (a *ZaloAdapter) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, DefaultMaxWebhookBodyBytes)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "Request entity too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Validate signature if secret is configured
	if a.webhookSecret != "" {
		signature := r.Header.Get("X-Zalo-Signature")
		if !a.validateSignature(body, signature) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var update ZaloUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if msg := a.parseUpdate(&update); msg != nil {
		if a.health != nil {
			a.health.RecordMessageReceived()
		}
		select {
		case a.messages <- msg:
		default:
			// Channel full, drop message
		}
	}

	w.WriteHeader(http.StatusOK)
}

// WebhookPath returns the path for webhook endpoint.
func (a *ZaloAdapter) WebhookPath() string {
	return a.webhookPath
}

// validateToken validates the bot token and retrieves bot info.
func (a *ZaloAdapter) validateToken(ctx context.Context) error {
	var resp ZaloAPIResponse[ZaloBotInfo]
	if err := a.callAPI(ctx, "getMe", nil, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("API error: %s", resp.Description)
	}

	if resp.Result != nil {
		a.botID = resp.Result.ID
		a.botName = resp.Result.Name
	}

	return nil
}

// setWebhook configures the webhook URL with Zalo.
func (a *ZaloAdapter) setWebhook(ctx context.Context) error {
	params := map[string]string{
		"url": a.webhookURL,
	}
	if a.webhookSecret != "" {
		params["secret_token"] = a.webhookSecret
	}

	var resp ZaloAPIResponse[bool]
	if err := a.callAPI(ctx, "setWebhook", params, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("failed to set webhook: %s", resp.Description)
	}

	return nil
}

// pollLoop continuously polls for updates.
func (a *ZaloAdapter) pollLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		update, err := a.getUpdates(ctx)
		if err != nil {
			// Check if it's a polling timeout (normal)
			if !isPollingTimeout(err) {
				// Log error and continue
				time.Sleep(time.Second)
			}
			continue
		}

		if msg := a.parseUpdate(update); msg != nil {
			if a.health != nil {
				a.health.RecordMessageReceived()
			}
			select {
			case a.messages <- msg:
			case <-ctx.Done():
				return
			}
		}
	}
}

// getUpdates fetches a single update using long polling.
func (a *ZaloAdapter) getUpdates(ctx context.Context) (*ZaloUpdate, error) {
	params := map[string]string{
		"timeout": fmt.Sprintf("%d", a.pollTimeout),
	}

	var resp ZaloAPIResponse[ZaloUpdate]
	if err := a.callAPI(ctx, "getUpdates", params, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, &ZaloAPIError{
			ErrorCode:   resp.ErrorCode,
			Description: resp.Description,
		}
	}

	return resp.Result, nil
}

// parseUpdate converts a Zalo update to a Message.
func (a *ZaloAdapter) parseUpdate(update *ZaloUpdate) *models.Message {
	if update == nil || update.Message == nil {
		return nil
	}

	// Only handle message events
	switch update.EventName {
	case "message.text.received", "message.image.received", "message.sticker.received":
		// Continue processing
	default:
		return nil
	}

	zm := update.Message

	msg := &models.Message{
		ID:        uuid.New().String(),
		Channel:   models.ChannelZalo,
		ChannelID: zm.Chat.ID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   zm.Text,
		Metadata: map[string]any{
			"zalo_message_id": zm.MessageID,
			"from_id":         zm.From.ID,
			"from_name":       zm.From.Name,
			"chat_type":       zm.Chat.ChatType,
		},
		CreatedAt: time.Unix(int64(zm.Date), 0),
	}

	// Handle image
	if zm.Photo != "" {
		msg.Attachments = append(msg.Attachments, models.Attachment{
			ID:   uuid.New().String(),
			Type: "image",
			URL:  zm.Photo,
		})
		if zm.Caption != "" {
			msg.Content = zm.Caption
		}
	}

	return msg
}

// sendText sends a text message.
func (a *ZaloAdapter) sendText(ctx context.Context, chatID, text string) error {
	// Chunk if necessary
	chunks := chunkText(text, TextChunkLimit)

	for _, chunk := range chunks {
		params := map[string]string{
			"chat_id": chatID,
			"text":    chunk,
		}

		var resp ZaloAPIResponse[ZaloMessage]
		if err := a.callAPI(ctx, "sendMessage", params, &resp); err != nil {
			return err
		}

		if !resp.OK {
			return fmt.Errorf("failed to send message: %s", resp.Description)
		}
	}

	return nil
}

// sendPhoto sends a photo message.
func (a *ZaloAdapter) sendPhoto(ctx context.Context, chatID, photoURL, caption string) error {
	params := map[string]string{
		"chat_id": chatID,
		"photo":   photoURL,
	}
	if caption != "" {
		if len(caption) > TextChunkLimit {
			caption = caption[:TextChunkLimit]
		}
		params["caption"] = caption
	}

	var resp ZaloAPIResponse[ZaloMessage]
	if err := a.callAPI(ctx, "sendPhoto", params, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("failed to send photo: %s", resp.Description)
	}

	return nil
}

// callAPI makes a request to the Zalo Bot API.
func (a *ZaloAdapter) callAPI(ctx context.Context, method string, params map[string]string, result any) error {
	url := fmt.Sprintf("%s/bot%s/%s", ZaloAPIBase, a.token, method)

	var body io.Reader
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	const maxAPIResponseBytes = 1 << 20
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes+1))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	if len(respBody) > maxAPIResponseBytes {
		return fmt.Errorf("response too large (%d bytes)", len(respBody))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	return nil
}

// validateSignature validates the webhook signature.
func (a *ZaloAdapter) validateSignature(body []byte, signature string) bool {
	if a.webhookSecret == "" {
		return true
	}

	mac := hmac.New(sha256.New, []byte(a.webhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected))
}

// chunkText splits text into chunks respecting the limit.
func chunkText(text string, limit int) []string {
	if text == "" {
		return nil
	}
	if limit <= 0 || len(text) <= limit {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > limit {
		window := remaining[:limit]

		// Find a good break point
		breakIdx := -1
		if idx := lastIndex(window, '\n'); idx > 0 {
			breakIdx = idx
		} else if idx := lastIndex(window, ' '); idx > 0 {
			breakIdx = idx
		}

		if breakIdx <= 0 {
			breakIdx = limit
		}

		chunk := trimRight(remaining[:breakIdx])
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}

		// Skip separator if we broke on one
		nextStart := breakIdx
		if breakIdx < len(remaining) && (remaining[breakIdx] == '\n' || remaining[breakIdx] == ' ') {
			nextStart++
		}
		remaining = trimLeft(remaining[nextStart:])
	}

	if len(remaining) > 0 {
		chunks = append(chunks, remaining)
	}

	return chunks
}

func lastIndex(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimRight(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func trimLeft(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

// isPollingTimeout checks if the error is a polling timeout.
func isPollingTimeout(err error) bool {
	var apiErr *ZaloAPIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == 408
	}
	return false
}

// ZaloAPIResponse represents a Zalo Bot API response.
type ZaloAPIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      *T     `json:"result,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// ZaloBotInfo contains bot information.
type ZaloBotInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

// ZaloMessage represents a Zalo message.
type ZaloMessage struct {
	MessageID string     `json:"message_id"`
	From      ZaloSender `json:"from"`
	Chat      ZaloChat   `json:"chat"`
	Date      int        `json:"date"`
	Text      string     `json:"text,omitempty"`
	Photo     string     `json:"photo,omitempty"`
	Caption   string     `json:"caption,omitempty"`
	Sticker   string     `json:"sticker,omitempty"`
}

// ZaloSender represents message sender info.
type ZaloSender struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// ZaloChat represents chat information.
type ZaloChat struct {
	ID       string `json:"id"`
	ChatType string `json:"chat_type"` // "PRIVATE" or "GROUP"
}

// ZaloUpdate represents an incoming update from Zalo.
type ZaloUpdate struct {
	EventName string       `json:"event_name"`
	Message   *ZaloMessage `json:"message,omitempty"`
}

// ZaloAPIError represents an error from the Zalo API.
type ZaloAPIError struct {
	ErrorCode   int
	Description string
}

func (e *ZaloAPIError) Error() string {
	return fmt.Sprintf("zalo API error %d: %s", e.ErrorCode, e.Description)
}
