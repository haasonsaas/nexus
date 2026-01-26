package bluebubbles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

const (
	// DefaultWebhookPath is the default path for webhook callbacks.
	DefaultWebhookPath = "/webhook/bluebubbles"

	// DefaultMaxWebhookBodyBytes is the maximum allowed webhook payload size.
	DefaultMaxWebhookBodyBytes = 1 << 20

	// DefaultTimeout is the default HTTP timeout.
	DefaultTimeout = 10 * time.Second

	// TextChunkLimit is the maximum characters per message.
	TextChunkLimit = 4000
)

// BlueBubblesAdapter implements the channel adapter for BlueBubbles iMessage API.
// BlueBubbles is a macOS application that provides REST API access to iMessage.
//
// Features:
// - Direct and group iMessage conversations
// - Media attachments
// - Read receipts
// - Reactions
//
// Thread Safety:
// BlueBubblesAdapter is safe for concurrent use.
type BlueBubblesAdapter struct {
	serverURL   string
	password    string
	webhookPath string

	messages chan *models.Message
	client   *http.Client
	logger   *slog.Logger
	health   *channels.BaseHealthAdapter

	mu      sync.RWMutex
	running bool
	cancel  context.CancelFunc
}

// BlueBubblesConfig holds configuration for the BlueBubbles adapter.
type BlueBubblesConfig struct {
	// ServerURL is the BlueBubbles server URL (required)
	// Format: http://host:port
	ServerURL string

	// Password is the API password (required)
	Password string

	// WebhookPath is the path for webhook callbacks (default: /webhook/bluebubbles)
	WebhookPath string

	// Timeout is the HTTP timeout (default: 10s)
	Timeout time.Duration

	// Logger is an optional logger for adapter diagnostics.
	Logger *slog.Logger
}

// NewBlueBubblesAdapter creates a new BlueBubbles channel adapter.
func NewBlueBubblesAdapter(cfg BlueBubblesConfig) (*BlueBubblesAdapter, error) {
	if cfg.ServerURL == "" {
		return nil, errors.New("bluebubbles: serverURL is required")
	}

	if cfg.Password == "" {
		return nil, errors.New("bluebubbles: password is required")
	}

	webhookPath := cfg.WebhookPath
	if webhookPath == "" {
		webhookPath = DefaultWebhookPath
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	// Normalize server URL
	serverURL := strings.TrimRight(cfg.ServerURL, "/")
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "http://" + serverURL
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("adapter", "bluebubbles")
	health := channels.NewBaseHealthAdapter(models.ChannelBlueBubbles, logger)

	return &BlueBubblesAdapter{
		serverURL:   serverURL,
		password:    cfg.Password,
		webhookPath: webhookPath,
		messages:    make(chan *models.Message, 100),
		client: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
		health: health,
	}, nil
}

// Start begins receiving messages from BlueBubbles.
func (a *BlueBubblesAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("bluebubbles: adapter already running")
	}
	a.running = true
	ctx, a.cancel = context.WithCancel(ctx)
	a.mu.Unlock()

	// Verify connection by pinging the server
	if err := a.ping(ctx); err != nil {
		if a.health != nil {
			a.health.SetStatus(false, err.Error())
			a.health.RecordError(channels.ErrCodeConnection)
		}
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		return fmt.Errorf("bluebubbles: failed to connect: %w", err)
	}

	if a.health != nil {
		a.health.SetStatus(true, "")
		a.health.RecordConnectionOpened()
	}

	return nil
}

// Stop stops the adapter.
func (a *BlueBubblesAdapter) Stop() error {
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

// Send sends a message to BlueBubbles.
func (a *BlueBubblesAdapter) Send(ctx context.Context, msg *models.Message) error {
	if msg == nil {
		return errors.New("bluebubbles: message is nil")
	}

	target := msg.ChannelID
	if target == "" {
		return errors.New("bluebubbles: channel_id (target) is required")
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
		if att.URL != "" {
			start := time.Now()
			if err := recordSend(a.sendAttachment(ctx, target, att.URL, msg.Content), start); err != nil {
				return err
			}
			// If we sent media with caption, don't send text separately
			if msg.Content != "" {
				return nil
			}
		}
	}

	// Send text message
	if msg.Content != "" {
		start := time.Now()
		if err := recordSend(a.sendText(ctx, target, msg.Content), start); err != nil {
			return err
		}
	}

	return nil
}

// Messages returns the channel for receiving inbound messages.
func (a *BlueBubblesAdapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *BlueBubblesAdapter) Type() models.ChannelType {
	return models.ChannelBlueBubbles
}

// Status returns the current connection status.
func (a *BlueBubblesAdapter) Status() channels.Status {
	if a.health == nil {
		return channels.Status{}
	}
	return a.health.Status()
}

// HealthCheck performs a connectivity check.
func (a *BlueBubblesAdapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	if a.health == nil {
		return channels.HealthStatus{Healthy: false, Message: "health adapter unavailable", LastCheck: time.Now()}
	}
	return a.health.HealthCheck(ctx)
}

// Metrics returns the current metrics snapshot.
func (a *BlueBubblesAdapter) Metrics() channels.MetricsSnapshot {
	if a.health == nil {
		return channels.MetricsSnapshot{ChannelType: models.ChannelBlueBubbles}
	}
	return a.health.Metrics()
}

// HandleWebhook processes incoming webhook requests.
func (a *BlueBubblesAdapter) HandleWebhook(w http.ResponseWriter, r *http.Request) {
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

	// Validate password if present in query
	queryPassword := r.URL.Query().Get("password")
	headerPassword := r.Header.Get("X-Password")
	if queryPassword != "" && queryPassword != a.password {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if headerPassword != "" && headerPassword != a.password {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Only process message events
	switch payload.Type {
	case "new-message", "message":
		if msg := a.parseMessage(&payload); msg != nil {
			if a.health != nil {
				a.health.RecordMessageReceived()
			}
			select {
			case a.messages <- msg:
			default:
				// Channel full
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		_ = err
	}
}

// WebhookPath returns the path for webhook endpoint.
func (a *BlueBubblesAdapter) WebhookPath() string {
	return a.webhookPath
}

// ping checks connectivity to the BlueBubbles server.
func (a *BlueBubblesAdapter) ping(ctx context.Context) error {
	apiURL := a.buildURL("/api/v1/ping")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping failed: HTTP %d", resp.StatusCode)
	}

	if a.health != nil {
		a.health.UpdateLastPing()
	}

	return nil
}

// parseMessage converts a webhook payload to a Message.
func (a *BlueBubblesAdapter) parseMessage(payload *WebhookPayload) *models.Message {
	if payload.Data == nil {
		return nil
	}

	data := payload.Data

	// Skip messages from self
	if data.IsFromMe {
		return nil
	}

	// Extract sender info
	senderID := ""
	senderName := ""
	if data.Handle != nil {
		senderID = data.Handle.Address
		senderName = data.Handle.DisplayName
	}
	if senderID == "" {
		senderID = data.SenderID
	}
	if senderID == "" {
		return nil
	}

	// Determine chat target
	chatTarget := ""
	chatGUID := ""
	isGroup := false

	if data.ChatGUID != "" {
		chatGUID = data.ChatGUID
		chatTarget = "chat_guid:" + chatGUID
		// Check if group chat (format: iMessage;+;chat123 for group, iMessage;-;phone for DM)
		if strings.Contains(chatGUID, ";+;") {
			isGroup = true
		}
	} else if data.Chat != nil {
		chatGUID = data.Chat.GUID
		chatTarget = "chat_guid:" + chatGUID
		if strings.Contains(chatGUID, ";+;") {
			isGroup = true
		}
	}

	if chatTarget == "" {
		chatTarget = senderID
	}

	content := data.Text
	if content == "" {
		content = data.Subject
	}

	msg := &models.Message{
		ID:        uuid.New().String(),
		Channel:   models.ChannelBlueBubbles,
		ChannelID: chatTarget,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   content,
		Metadata: map[string]any{
			"bluebubbles_message_id": data.GUID,
			"sender_id":              senderID,
			"sender_name":            senderName,
			"chat_guid":              chatGUID,
			"is_group":               isGroup,
			"conversation_type":      "dm",
		},
		CreatedAt: time.Now(),
	}
	if isGroup {
		msg.Metadata["conversation_type"] = "group"
		if chatGUID != "" {
			msg.Metadata["group_id"] = chatGUID
		}
	}

	// Parse timestamp
	if data.DateCreated > 0 {
		// BlueBubbles timestamps can be in ms or s
		ts := data.DateCreated
		if ts > 1_000_000_000_000 {
			msg.CreatedAt = time.UnixMilli(ts)
		} else {
			msg.CreatedAt = time.Unix(ts, 0)
		}
	}

	// Handle attachments
	for _, att := range data.Attachments {
		if att.GUID == "" {
			continue
		}
		attachment := models.Attachment{
			ID:       att.GUID,
			Type:     categorizeAttachment(att.MimeType),
			MimeType: att.MimeType,
			Filename: att.TransferName,
		}
		if att.TotalBytes > 0 {
			attachment.Size = att.TotalBytes
		}
		msg.Attachments = append(msg.Attachments, attachment)
	}

	return msg
}

// sendText sends a text message.
func (a *BlueBubblesAdapter) sendText(ctx context.Context, target, text string) error {
	// Chunk if necessary
	chunks := chunkText(text, TextChunkLimit)

	for _, chunk := range chunks {
		if err := a.sendTextChunk(ctx, target, chunk); err != nil {
			return err
		}
	}

	return nil
}

// sendTextChunk sends a single text chunk.
func (a *BlueBubblesAdapter) sendTextChunk(ctx context.Context, target, text string) error {
	// Resolve target to chat GUID
	chatGUID, err := a.resolveTarget(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to resolve target: %w", err)
	}

	payload := map[string]any{
		"chatGuid": chatGUID,
		"tempGuid": uuid.New().String(),
		"message":  text,
	}

	return a.callAPI(ctx, "/api/v1/message/text", payload)
}

// sendAttachment sends a media attachment.
func (a *BlueBubblesAdapter) sendAttachment(ctx context.Context, target, attachmentURL, caption string) error {
	chatGUID, err := a.resolveTarget(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to resolve target: %w", err)
	}

	payload := map[string]any{
		"chatGuid": chatGUID,
		"tempGuid": uuid.New().String(),
	}

	// BlueBubbles expects a file path or URL
	if strings.HasPrefix(attachmentURL, "http://") || strings.HasPrefix(attachmentURL, "https://") {
		// For remote URLs, we'd need to download first
		// For now, just include the URL and hope BlueBubbles can handle it
		payload["attachment"] = attachmentURL
	} else {
		payload["attachment"] = attachmentURL
	}

	if caption != "" {
		payload["message"] = caption
	}

	return a.callAPI(ctx, "/api/v1/message/attachment", payload)
}

// resolveTarget converts a target string to a chat GUID.
func (a *BlueBubblesAdapter) resolveTarget(ctx context.Context, target string) (string, error) {
	// If already a chat GUID
	if strings.HasPrefix(target, "chat_guid:") {
		return strings.TrimPrefix(target, "chat_guid:"), nil
	}

	// If chat_id, look up the chat GUID
	if strings.HasPrefix(target, "chat_id:") {
		// Would need to query /api/v1/chat/query to resolve
		// For now, return an error
		return "", errors.New("chat_id targets not yet supported; use chat_guid: prefix")
	}

	// If chat_identifier
	if strings.HasPrefix(target, "chat_identifier:") {
		// Would need to query /api/v1/chat/query to resolve
		return "", errors.New("chat_identifier targets not yet supported; use chat_guid: prefix")
	}

	// Assume it's a phone number/email (handle)
	// Need to find or create a chat with this handle
	chatGUID, err := a.findOrCreateChat(ctx, target)
	if err != nil {
		return "", err
	}

	return chatGUID, nil
}

// findOrCreateChat finds an existing chat or creates one for a handle.
func (a *BlueBubblesAdapter) findOrCreateChat(ctx context.Context, handle string) (string, error) {
	// Query existing chats
	chats, err := a.queryChats(ctx, 0, 500)
	if err != nil {
		return "", err
	}

	normalizedHandle := normalizeHandle(handle)

	// Look for a matching chat
	for _, chat := range chats {
		if chat.GUID != "" {
			// Check if this is a DM with the target handle
			if strings.Contains(chat.GUID, ";-;") {
				// Extract handle from GUID
				parts := strings.Split(chat.GUID, ";")
				if len(parts) >= 3 {
					chatHandle := normalizeHandle(parts[2])
					if chatHandle == normalizedHandle {
						return chat.GUID, nil
					}
				}
			}

			// Check participants
			for _, p := range chat.Participants {
				if normalizeHandle(p.Address) == normalizedHandle {
					return chat.GUID, nil
				}
			}
		}
	}

	// If not found, create a new chat
	// BlueBubbles creates chats automatically when sending
	// Construct a GUID in the expected format
	service := "iMessage"
	if !strings.Contains(handle, "@") && !strings.HasPrefix(handle, "+") {
		service = "SMS"
	}
	return fmt.Sprintf("%s;-;%s", service, handle), nil
}

// queryChats fetches chats from BlueBubbles.
func (a *BlueBubblesAdapter) queryChats(ctx context.Context, offset, limit int) ([]ChatRecord, error) {
	apiURL := a.buildURL("/api/v1/chat/query")

	payload := map[string]any{
		"limit":  limit,
		"offset": offset,
		"with":   []string{"participants"},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query chats failed: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []ChatRecord `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// callAPI makes an API call to BlueBubbles.
func (a *BlueBubblesAdapter) callAPI(ctx context.Context, path string, payload map[string]any) error {
	apiURL := a.buildURL(path)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			return fmt.Errorf("API error (%d) (read body failed: %w)", resp.StatusCode, err)
		}
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// buildURL constructs an API URL with password.
func (a *BlueBubblesAdapter) buildURL(path string) string {
	u, err := url.Parse(a.serverURL + path)
	if err != nil {
		return a.serverURL + path + "?password=" + url.QueryEscape(a.password)
	}
	q := u.Query()
	q.Set("password", a.password)
	u.RawQuery = q.Encode()
	return u.String()
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

		breakIdx := -1
		if idx := strings.LastIndex(window, "\n"); idx > 0 {
			breakIdx = idx
		} else if idx := strings.LastIndex(window, " "); idx > 0 {
			breakIdx = idx
		}

		if breakIdx <= 0 {
			breakIdx = limit
		}

		chunk := strings.TrimSpace(remaining[:breakIdx])
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}

		nextStart := breakIdx
		if breakIdx < len(remaining) && (remaining[breakIdx] == '\n' || remaining[breakIdx] == ' ') {
			nextStart++
		}
		remaining = strings.TrimSpace(remaining[nextStart:])
	}

	if len(remaining) > 0 {
		chunks = append(chunks, remaining)
	}

	return chunks
}

// normalizeHandle normalizes a phone number or email.
func normalizeHandle(handle string) string {
	handle = strings.TrimSpace(handle)
	handle = strings.ToLower(handle)

	// Remove common prefixes
	handle = strings.TrimPrefix(handle, "tel:")
	handle = strings.TrimPrefix(handle, "mailto:")

	// Normalize phone numbers (basic)
	if !strings.Contains(handle, "@") {
		// Remove all non-digit characters except +
		var normalized strings.Builder
		for _, c := range handle {
			if c == '+' || (c >= '0' && c <= '9') {
				normalized.WriteRune(c)
			}
		}
		handle = normalized.String()
	}

	return handle
}

// categorizeAttachment determines the attachment type from mime type.
func categorizeAttachment(mimeType string) string {
	if strings.HasPrefix(mimeType, "image/") {
		return "image"
	}
	if strings.HasPrefix(mimeType, "video/") {
		return "video"
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return "audio"
	}
	return "document"
}

// WebhookPayload represents a BlueBubbles webhook event.
type WebhookPayload struct {
	Type string          `json:"type"`
	Data *WebhookMessage `json:"data"`
}

// WebhookMessage represents a message in a webhook payload.
type WebhookMessage struct {
	GUID        string              `json:"guid"`
	Text        string              `json:"text"`
	Subject     string              `json:"subject"`
	DateCreated int64               `json:"dateCreated"`
	IsFromMe    bool                `json:"isFromMe"`
	ChatGUID    string              `json:"chatGuid"`
	SenderID    string              `json:"senderId"`
	Handle      *WebhookHandle      `json:"handle"`
	Chat        *WebhookChat        `json:"chat"`
	Attachments []WebhookAttachment `json:"attachments"`
}

// WebhookHandle represents sender info.
type WebhookHandle struct {
	Address     string `json:"address"`
	DisplayName string `json:"displayName"`
}

// WebhookChat represents chat info.
type WebhookChat struct {
	GUID         string          `json:"guid"`
	DisplayName  string          `json:"displayName"`
	Participants []WebhookHandle `json:"participants"`
}

// WebhookAttachment represents an attachment.
type WebhookAttachment struct {
	GUID         string `json:"guid"`
	MimeType     string `json:"mimeType"`
	TransferName string `json:"transferName"`
	TotalBytes   int64  `json:"totalBytes"`
}

// ChatRecord represents a chat from the API.
type ChatRecord struct {
	ID           int             `json:"id"`
	GUID         string          `json:"guid"`
	DisplayName  string          `json:"displayName"`
	Participants []WebhookHandle `json:"participants"`
}
