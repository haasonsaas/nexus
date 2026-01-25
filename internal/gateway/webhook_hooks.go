// Package gateway provides webhook hook handling for external integrations.
package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebhookConfig configures the webhook hook system.
type WebhookConfig struct {
	// Enabled turns on webhook handling.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// BasePath is the URL path prefix for webhooks (default: /hooks).
	BasePath string `json:"base_path" yaml:"base_path"`

	// Token is the required authentication token.
	Token string `json:"token" yaml:"token"`

	// MaxBodyBytes limits request body size (default: 256KB).
	MaxBodyBytes int64 `json:"max_body_bytes" yaml:"max_body_bytes"`

	// Mappings define webhook endpoints and their handlers.
	Mappings []WebhookMapping `json:"mappings" yaml:"mappings"`
}

// WebhookMapping defines a webhook endpoint.
type WebhookMapping struct {
	// Path is the endpoint path (appended to BasePath).
	Path string `json:"path" yaml:"path"`

	// Name is a human-readable name for this webhook.
	Name string `json:"name" yaml:"name"`

	// Handler is the handler type (agent, wake, custom).
	Handler string `json:"handler" yaml:"handler"`

	// AgentID targets a specific agent (optional).
	AgentID string `json:"agent_id" yaml:"agent_id"`

	// ChannelID targets a specific channel (optional).
	ChannelID string `json:"channel_id" yaml:"channel_id"`
}

const (
	defaultWebhookPath     = "/hooks"
	defaultMaxBodyBytes    = 256 * 1024
	webhookHandlerAgent    = "agent"
	webhookHandlerWake     = "wake"
	webhookHandlerCustom   = "custom"
)

// WebhookPayload represents the standard webhook request body.
type WebhookPayload struct {
	// Message is the text content to process.
	Message string `json:"message"`

	// Name is the sender name (default: "Webhook").
	Name string `json:"name,omitempty"`

	// SessionKey identifies the session (auto-generated if empty).
	SessionKey string `json:"session_key,omitempty"`

	// Channel targets a specific channel ("last" or channel ID).
	Channel string `json:"channel,omitempty"`

	// To targets a specific recipient.
	To string `json:"to,omitempty"`

	// Model overrides the default model.
	Model string `json:"model,omitempty"`

	// Thinking sets the thinking level.
	Thinking string `json:"thinking,omitempty"`

	// TimeoutSeconds sets the processing timeout.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// WakeMode controls when to process ("now" or "next-heartbeat").
	WakeMode string `json:"wake_mode,omitempty"`

	// Deliver controls whether to deliver the response (default: true).
	Deliver *bool `json:"deliver,omitempty"`

	// Metadata contains arbitrary key-value pairs.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// WebhookResponse is the standard webhook response.
type WebhookResponse struct {
	// OK indicates success.
	OK bool `json:"ok"`

	// RequestID is a unique identifier for this request.
	RequestID string `json:"request_id,omitempty"`

	// Message describes the result.
	Message string `json:"message,omitempty"`

	// Error contains error details if OK is false.
	Error string `json:"error,omitempty"`

	// Data contains handler-specific response data.
	Data map[string]any `json:"data,omitempty"`
}

// WebhookHandler processes webhook requests.
type WebhookHandler interface {
	Handle(ctx context.Context, payload *WebhookPayload, mapping *WebhookMapping) (*WebhookResponse, error)
}

// WebhookHandlerFunc is a function that implements WebhookHandler.
type WebhookHandlerFunc func(ctx context.Context, payload *WebhookPayload, mapping *WebhookMapping) (*WebhookResponse, error)

// Handle implements WebhookHandler.
func (f WebhookHandlerFunc) Handle(ctx context.Context, payload *WebhookPayload, mapping *WebhookMapping) (*WebhookResponse, error) {
	return f(ctx, payload, mapping)
}

// WebhookHooks manages webhook handlers and routing.
type WebhookHooks struct {
	mu       sync.RWMutex
	config   *WebhookConfig
	handlers map[string]WebhookHandler
	stats    *WebhookStats
}

// WebhookStats tracks webhook usage statistics.
type WebhookStats struct {
	mu             sync.Mutex
	TotalRequests  int64            `json:"total_requests"`
	TotalSuccesses int64            `json:"total_successes"`
	TotalErrors    int64            `json:"total_errors"`
	ByPath         map[string]int64 `json:"by_path"`
	LastRequestAt  time.Time        `json:"last_request_at"`
}

// NewWebhookHooks creates a new webhook hooks manager.
func NewWebhookHooks(config *WebhookConfig) (*WebhookHooks, error) {
	if config == nil || !config.Enabled {
		return nil, nil
	}

	if strings.TrimSpace(config.Token) == "" {
		return nil, fmt.Errorf("webhook hooks require a token")
	}

	// Normalize config
	if config.BasePath == "" {
		config.BasePath = defaultWebhookPath
	}
	if !strings.HasPrefix(config.BasePath, "/") {
		config.BasePath = "/" + config.BasePath
	}
	config.BasePath = strings.TrimSuffix(config.BasePath, "/")

	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaultMaxBodyBytes
	}

	return &WebhookHooks{
		config:   config,
		handlers: make(map[string]WebhookHandler),
		stats: &WebhookStats{
			ByPath: make(map[string]int64),
		},
	}, nil
}

// RegisterHandler registers a handler for a handler type.
func (h *WebhookHooks) RegisterHandler(handlerType string, handler WebhookHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[handlerType] = handler
}

// ServeHTTP implements http.Handler for webhook requests.
func (h *WebhookHooks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Record request
	h.stats.mu.Lock()
	h.stats.TotalRequests++
	h.stats.LastRequestAt = time.Now()
	h.stats.mu.Unlock()

	// Validate method
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract and validate token
	token := h.extractToken(r)
	if !h.validateToken(token) {
		h.respondError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Find matching mapping
	path := strings.TrimPrefix(r.URL.Path, h.config.BasePath)
	mapping := h.findMapping(path)
	if mapping == nil {
		h.respondError(w, http.StatusNotFound, "webhook not found")
		return
	}

	// Track by path
	h.stats.mu.Lock()
	h.stats.ByPath[path]++
	h.stats.mu.Unlock()

	// Read and parse body
	payload, err := h.readPayload(r)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Get handler
	h.mu.RLock()
	handler, ok := h.handlers[mapping.Handler]
	h.mu.RUnlock()

	if !ok {
		h.respondError(w, http.StatusNotImplemented, "handler not implemented: "+mapping.Handler)
		return
	}

	// Execute handler
	ctx := r.Context()
	if payload.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(payload.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	response, err := handler.Handle(ctx, payload, mapping)
	if err != nil {
		h.stats.mu.Lock()
		h.stats.TotalErrors++
		h.stats.mu.Unlock()
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.stats.mu.Lock()
	h.stats.TotalSuccesses++
	h.stats.mu.Unlock()

	h.respondJSON(w, http.StatusOK, response)
}

// extractToken extracts the auth token from the request.
func (h *WebhookHooks) extractToken(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}

	// Check custom header
	if token := r.Header.Get("X-Webhook-Token"); token != "" {
		return strings.TrimSpace(token)
	}

	// Check query parameter
	if token := r.URL.Query().Get("token"); token != "" {
		return strings.TrimSpace(token)
	}

	return ""
}

// validateToken validates the provided token.
func (h *WebhookHooks) validateToken(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.config.Token)) == 1
}

// findMapping finds the matching webhook mapping for a path.
func (h *WebhookHooks) findMapping(path string) *WebhookMapping {
	path = strings.TrimPrefix(path, "/")
	for i := range h.config.Mappings {
		mappingPath := strings.TrimPrefix(h.config.Mappings[i].Path, "/")
		if mappingPath == path {
			return &h.config.Mappings[i]
		}
	}
	return nil
}

// readPayload reads and parses the request body.
func (h *WebhookHooks) readPayload(r *http.Request) (*WebhookPayload, error) {
	// Limit body size
	r.Body = http.MaxBytesReader(nil, r.Body, h.config.MaxBodyBytes)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	if len(body) == 0 {
		return &WebhookPayload{}, nil
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Apply defaults
	if payload.Name == "" {
		payload.Name = "Webhook"
	}
	if payload.Channel == "" {
		payload.Channel = "last"
	}
	if payload.WakeMode == "" {
		payload.WakeMode = "now"
	}

	return &payload, nil
}

// respondError sends an error response.
func (h *WebhookHooks) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, &WebhookResponse{
		OK:    false,
		Error: message,
	})
}

// respondJSON sends a JSON response.
func (h *WebhookHooks) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// Stats returns webhook usage statistics.
func (h *WebhookHooks) Stats() *WebhookStats {
	h.stats.mu.Lock()
	defer h.stats.mu.Unlock()

	// Copy stats
	byPath := make(map[string]int64)
	for k, v := range h.stats.ByPath {
		byPath[k] = v
	}

	return &WebhookStats{
		TotalRequests:  h.stats.TotalRequests,
		TotalSuccesses: h.stats.TotalSuccesses,
		TotalErrors:    h.stats.TotalErrors,
		ByPath:         byPath,
		LastRequestAt:  h.stats.LastRequestAt,
	}
}

// Config returns the webhook configuration.
func (h *WebhookHooks) Config() *WebhookConfig {
	return h.config
}

// DefaultAgentHandler returns a handler that sends messages to agents.
func DefaultAgentHandler(sendFn func(ctx context.Context, agentID, message, sessionKey string) error) WebhookHandler {
	return WebhookHandlerFunc(func(ctx context.Context, payload *WebhookPayload, mapping *WebhookMapping) (*WebhookResponse, error) {
		if payload.Message == "" {
			return &WebhookResponse{
				OK:    false,
				Error: "message required",
			}, nil
		}

		agentID := mapping.AgentID
		if agentID == "" {
			agentID = "default"
		}

		if err := sendFn(ctx, agentID, payload.Message, payload.SessionKey); err != nil {
			return &WebhookResponse{
				OK:    false,
				Error: err.Error(),
			}, nil
		}

		return &WebhookResponse{
			OK:      true,
			Message: "message sent",
			Data: map[string]any{
				"agent_id":    agentID,
				"session_key": payload.SessionKey,
			},
		}, nil
	})
}

// DefaultWakeHandler returns a handler that triggers agent wakeup.
func DefaultWakeHandler(wakeFn func(ctx context.Context, agentID, text string, mode string) error) WebhookHandler {
	return WebhookHandlerFunc(func(ctx context.Context, payload *WebhookPayload, mapping *WebhookMapping) (*WebhookResponse, error) {
		if payload.Message == "" {
			return &WebhookResponse{
				OK:    false,
				Error: "message required",
			}, nil
		}

		agentID := mapping.AgentID
		if agentID == "" {
			agentID = "default"
		}

		mode := payload.WakeMode
		if mode != "now" && mode != "next-heartbeat" {
			mode = "now"
		}

		if err := wakeFn(ctx, agentID, payload.Message, mode); err != nil {
			return &WebhookResponse{
				OK:    false,
				Error: err.Error(),
			}, nil
		}

		return &WebhookResponse{
			OK:      true,
			Message: "wake triggered",
			Data: map[string]any{
				"agent_id": agentID,
				"mode":     mode,
			},
		}, nil
	})
}
