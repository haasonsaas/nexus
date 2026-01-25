package nextcloudtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Config holds configuration for the Nextcloud Talk adapter.
type Config struct {
	// BaseURL is the Nextcloud server base URL (required)
	BaseURL string

	// BotSecret is the bot secret for webhook verification (required)
	BotSecret string

	// WebhookPort is the port for webhook server (default: 8788)
	WebhookPort int

	// WebhookHost is the host for webhook server (default: 0.0.0.0)
	WebhookHost string

	// WebhookPath is the path for webhook endpoint (default: /nextcloud-talk-webhook)
	WebhookPath string

	// RateLimit configures rate limiting for API calls (operations per second)
	RateLimit float64

	// RateBurst configures the burst capacity for rate limiting
	RateBurst int

	// Logger is an optional slog.Logger instance
	Logger *slog.Logger
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.BaseURL == "" {
		return channels.ErrConfig("base_url is required", nil)
	}

	if c.BotSecret == "" {
		return channels.ErrConfig("bot_secret is required", nil)
	}

	if c.WebhookPort == 0 {
		c.WebhookPort = 8788
	}

	if c.WebhookHost == "" {
		c.WebhookHost = "0.0.0.0"
	}

	if c.WebhookPath == "" {
		c.WebhookPath = "/nextcloud-talk-webhook"
	}

	if c.RateLimit == 0 {
		c.RateLimit = 10
	}

	if c.RateBurst == 0 {
		c.RateBurst = 5
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	return nil
}

// WebhookPayload represents the Nextcloud Talk webhook payload structure.
type WebhookPayload struct {
	Type   string        `json:"type"`
	Actor  PayloadActor  `json:"actor"`
	Object PayloadObject `json:"object"`
	Target PayloadTarget `json:"target"`
}

// PayloadActor represents the actor in a webhook payload.
type PayloadActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PayloadObject represents the object (message) in a webhook payload.
type PayloadObject struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	MediaType string `json:"mediaType"`
}

// PayloadTarget represents the target (room) in a webhook payload.
type PayloadTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Adapter implements the channels.Adapter interface for Nextcloud Talk.
type Adapter struct {
	cfg         Config
	httpClient  *http.Client
	server      *http.Server
	messages    chan *models.Message
	status      channels.Status
	statusMu    sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	rateLimiter *channels.RateLimiter
	metrics     *channels.Metrics
	logger      *slog.Logger
	degraded    bool
	degradedMu  sync.RWMutex
}

// NewAdapter creates a new Nextcloud Talk adapter with the given configuration.
func NewAdapter(cfg Config) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Adapter{
		cfg:         cfg,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		messages:    make(chan *models.Message, 100),
		status:      channels.Status{Connected: false},
		rateLimiter: channels.NewRateLimiter(cfg.RateLimit, cfg.RateBurst),
		metrics:     channels.NewMetrics(models.ChannelNextcloudTalk),
		logger:      cfg.Logger.With("adapter", "nextcloud-talk"),
	}, nil
}

// Start begins listening for webhooks from Nextcloud Talk.
func (a *Adapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	a.logger.Info("starting nextcloud-talk adapter",
		"base_url", a.cfg.BaseURL,
		"webhook_port", a.cfg.WebhookPort)

	// Create HTTP server for webhooks
	mux := http.NewServeMux()
	mux.HandleFunc(a.cfg.WebhookPath, a.handleWebhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := fmt.Sprintf("%s:%d", a.cfg.WebhookHost, a.cfg.WebhookPort)
	a.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start server in goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.logger.Info("webhook server starting", "address", addr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("webhook server error", "error", err)
			a.metrics.RecordError(channels.ErrCodeConnection)
			a.updateStatus(false, fmt.Sprintf("webhook server error: %v", err))
		}
	}()

	a.updateStatus(true, "")
	a.metrics.RecordConnectionOpened()

	a.logger.Info("nextcloud-talk adapter started successfully",
		"webhook_path", a.cfg.WebhookPath)

	return nil
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	a.logger.Info("stopping nextcloud-talk adapter")

	if a.cancel != nil {
		a.cancel()
	}

	// Shutdown webhook server
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil {
			a.logger.Warn("webhook server shutdown error", "error", err)
		}
	}

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(a.messages)
		a.updateStatus(false, "")
		a.metrics.RecordConnectionClosed()
		a.logger.Info("nextcloud-talk adapter stopped gracefully")
		return nil
	case <-ctx.Done():
		close(a.messages)
		a.updateStatus(false, "shutdown timeout")
		a.logger.Warn("nextcloud-talk adapter stop timeout")
		a.metrics.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("shutdown timeout", ctx.Err())
	}
}

// handleWebhook processes incoming webhook requests from Nextcloud Talk.
func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	startTime := time.Now()

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.logger.Warn("failed to read webhook body", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify signature
	signature := r.Header.Get("X-Nextcloud-Talk-Signature")
	random := r.Header.Get("X-Nextcloud-Talk-Random")

	if !a.verifySignature(signature, random, body) {
		a.logger.Warn("invalid webhook signature")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse payload
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		a.logger.Warn("failed to parse webhook payload", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Only process Create events (new messages)
	if payload.Type != "Create" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Validate payload structure
	if payload.Actor.Type == "" || payload.Object.Type == "" || payload.Target.Type == "" {
		a.logger.Warn("incomplete webhook payload")
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	a.logger.Debug("processing webhook",
		"type", payload.Type,
		"room", payload.Target.ID,
		"sender", payload.Actor.ID)

	// Convert to unified message format
	msg := a.convertPayload(payload)

	// Record metrics
	a.metrics.RecordMessageReceived()
	a.metrics.RecordReceiveLatency(time.Since(startTime))

	// Respond immediately before processing
	w.WriteHeader(http.StatusOK)

	// Send to messages channel
	select {
	case a.messages <- msg:
		a.updateLastPing()
	case <-a.ctx.Done():
		return
	default:
		a.logger.Warn("messages channel full, dropping message",
			"room", payload.Target.ID)
		a.metrics.RecordMessageFailed()
	}
}

// verifySignature verifies the Nextcloud Talk webhook signature.
func (a *Adapter) verifySignature(signature, random string, body []byte) bool {
	if signature == "" || random == "" {
		return false
	}

	// Compute HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(a.cfg.BotSecret))
	mac.Write([]byte(random))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected))
}

// convertPayload converts a Nextcloud Talk webhook payload to unified message format.
func (a *Adapter) convertPayload(payload WebhookPayload) *models.Message {
	// Generate session ID from room
	sessionID := generateSessionID(payload.Target.ID)

	msg := &models.Message{
		ID:        payload.Object.ID,
		SessionID: sessionID,
		Channel:   models.ChannelNextcloudTalk,
		ChannelID: payload.Object.ID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   payload.Object.Content,
		Metadata: map[string]any{
			"nextcloud_room_token": payload.Target.ID,
			"nextcloud_room_name":  payload.Target.Name,
			"nextcloud_user_id":    payload.Actor.ID,
			"sender_id":            payload.Actor.ID,
			"sender_name":          payload.Actor.Name,
			"conversation_type":    "group",
		},
		CreatedAt: time.Now(),
	}

	// Check if it might be a DM (heuristic: room name equals sender name)
	if payload.Target.Name == payload.Actor.Name {
		msg.Metadata["conversation_type"] = "dm"
	}

	return msg
}

// Send delivers a message to Nextcloud Talk.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	startTime := time.Now()

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.logger.Warn("rate limit wait cancelled", "error", err)
		a.metrics.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	// Extract room token from message metadata
	roomToken, ok := msg.Metadata["nextcloud_room_token"].(string)
	if !ok || roomToken == "" {
		a.metrics.RecordMessageFailed()
		a.metrics.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("missing nextcloud_room_token in message metadata", nil)
	}

	a.logger.Debug("sending message",
		"room_token", roomToken,
		"content_length", len(msg.Content))

	// Build API URL
	apiURL := fmt.Sprintf("%s/ocs/v2.php/apps/spreed/api/v1/bot/%s/message",
		strings.TrimSuffix(a.cfg.BaseURL, "/"), roomToken)

	// Create request body
	reqBody := map[string]string{
		"message": msg.Content,
	}

	// Handle reply if specified
	if replyTo, ok := msg.Metadata["reply_to_message_id"].(string); ok && replyTo != "" {
		reqBody["replyTo"] = replyTo
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		a.metrics.RecordMessageFailed()
		return channels.ErrInternal("failed to marshal request body", err)
	}

	// Create and send request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		a.metrics.RecordMessageFailed()
		return channels.ErrInternal("failed to create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OCS-APIREQUEST", "true")
	req.Header.Set("X-Nextcloud-Talk-Bot-Random", generateRandom())
	req.Header.Set("X-Nextcloud-Talk-Bot-Signature", a.signMessage(bodyBytes))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.metrics.RecordMessageFailed()
		a.logger.Error("failed to send message", "error", err, "room_token", roomToken)
		return channels.ErrConnection("failed to send message", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		a.metrics.RecordMessageFailed()
		bodyBytes, _ := io.ReadAll(resp.Body)
		a.logger.Error("message send failed",
			"status", resp.StatusCode,
			"body", string(bodyBytes))
		return channels.ErrInternal(fmt.Sprintf("API error: %d", resp.StatusCode), nil)
	}

	// Record success metrics
	a.metrics.RecordMessageSent()
	a.metrics.RecordSendLatency(time.Since(startTime))

	a.logger.Debug("message sent successfully",
		"room_token", roomToken,
		"latency_ms", time.Since(startTime).Milliseconds())

	return nil
}

// signMessage creates a signature for outbound messages.
func (a *Adapter) signMessage(body []byte) string {
	random := generateRandom()
	mac := hmac.New(sha256.New, []byte(a.cfg.BotSecret))
	mac.Write([]byte(random))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Messages returns a channel of inbound messages.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelNextcloudTalk
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

// HealthCheck performs a connectivity check.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	startTime := time.Now()

	health := channels.HealthStatus{
		LastCheck: startTime,
		Healthy:   false,
	}

	// Check if server is running
	addr := fmt.Sprintf("http://%s:%d/healthz", a.cfg.WebhookHost, a.cfg.WebhookPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		health.Message = fmt.Sprintf("failed to create request: %v", err)
		health.Latency = time.Since(startTime)
		return health
	}

	resp, err := a.httpClient.Do(req)
	health.Latency = time.Since(startTime)

	if err != nil {
		health.Message = fmt.Sprintf("health check failed: %v", err)
		return health
	}
	defer resp.Body.Close()

	health.Healthy = resp.StatusCode == http.StatusOK
	health.Degraded = a.isDegraded()

	if health.Degraded {
		health.Message = "operating in degraded mode"
	} else if health.Healthy {
		health.Message = "healthy"
	} else {
		health.Message = fmt.Sprintf("unhealthy: status %d", resp.StatusCode)
	}

	return health
}

// Metrics returns the current metrics snapshot.
func (a *Adapter) Metrics() channels.MetricsSnapshot {
	return a.metrics.Snapshot()
}

// SendTypingIndicator is a no-op for Nextcloud Talk.
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	return nil
}

// StartStreamingResponse sends an initial placeholder message and returns its ID.
func (a *Adapter) StartStreamingResponse(ctx context.Context, msg *models.Message) (string, error) {
	// Create a placeholder message
	placeholderMsg := &models.Message{
		Content:  "...",
		Metadata: msg.Metadata,
	}

	if err := a.Send(ctx, placeholderMsg); err != nil {
		return "", err
	}

	// Note: Nextcloud Talk doesn't return message ID on send via bot API
	// This is a limitation - streaming updates may not work perfectly
	return "", nil
}

// UpdateStreamingResponse updates a previously sent message.
func (a *Adapter) UpdateStreamingResponse(ctx context.Context, msg *models.Message, messageID string, content string) error {
	// Nextcloud Talk bot API doesn't support message editing
	// Fall back to sending a new message
	return nil
}

// Helper functions

func (a *Adapter) updateStatus(connected bool, errMsg string) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.status.Connected = connected
	a.status.Error = errMsg
	if connected {
		a.status.LastPing = time.Now().Unix()
	}
}

func (a *Adapter) updateLastPing() {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.status.LastPing = time.Now().Unix()
}

func (a *Adapter) setDegraded(degraded bool) {
	a.degradedMu.Lock()
	defer a.degradedMu.Unlock()
	a.degraded = degraded
}

func (a *Adapter) isDegraded() bool {
	a.degradedMu.RLock()
	defer a.degradedMu.RUnlock()
	return a.degraded
}

func generateSessionID(roomToken string) string {
	data := fmt.Sprintf("nextcloud-talk:%s", roomToken)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func generateRandom() string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(hash[:16])
}
