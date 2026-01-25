package hooks

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GmailNotification represents a Gmail push notification
type GmailNotification struct {
	// MessageID is the Gmail message ID
	MessageID string `json:"message_id"`

	// HistoryID is the Gmail history ID for incremental sync
	HistoryID uint64 `json:"history_id"`

	// EmailDate is the email's date header
	EmailDate string `json:"email_date,omitempty"`

	// Account is the email account this notification is for
	Account string `json:"account,omitempty"`

	// Timestamp is when the notification was received
	Timestamp time.Time `json:"timestamp"`
}

// PubSubMessage represents a Google Pub/Sub push message
type PubSubMessage struct {
	Message struct {
		Data        string            `json:"data"`
		MessageID   string            `json:"messageId"`
		PublishTime string            `json:"publishTime"`
		Attributes  map[string]string `json:"attributes"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

// GmailPushData represents the decoded Gmail push notification data
type GmailPushData struct {
	EmailAddress string `json:"emailAddress"`
	HistoryID    uint64 `json:"historyId"`
}

// GmailHookHandler handles incoming Gmail pub/sub notifications
type GmailHookHandler struct {
	Config    *GmailHookRuntimeConfig
	OnMessage func(notification *GmailNotification) error
	Logger    *slog.Logger
}

// NewGmailHookHandler creates a new Gmail hook handler
func NewGmailHookHandler(cfg *GmailHookRuntimeConfig, onMessage func(*GmailNotification) error, logger *slog.Logger) *GmailHookHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &GmailHookHandler{
		Config:    cfg,
		OnMessage: onMessage,
		Logger:    logger.With("component", "gmail-hook"),
	}
}

// ServeHTTP implements http.Handler
func (h *GmailHookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		h.Logger.Debug("rejected non-POST request", "method", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate token from query parameter
	token := r.URL.Query().Get("token")
	if !h.ValidateToken(token) {
		h.Logger.Warn("invalid push token received")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Read body with size limit
	maxBytes := int64(h.Config.MaxBytes)
	if maxBytes <= 0 {
		maxBytes = DefaultGmailMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		h.Logger.Error("failed to read request body", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if int64(len(body)) > maxBytes {
		h.Logger.Warn("request body exceeds max bytes", "size", len(body), "max", maxBytes)
		http.Error(w, "Request entity too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Parse Pub/Sub message
	var pubsubMsg PubSubMessage
	if err := json.Unmarshal(body, &pubsubMsg); err != nil {
		h.Logger.Error("failed to parse pubsub message", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Decode the base64 data
	data, err := base64.StdEncoding.DecodeString(pubsubMsg.Message.Data)
	if err != nil {
		h.Logger.Error("failed to decode message data", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Parse Gmail push data
	var pushData GmailPushData
	if err := json.Unmarshal(data, &pushData); err != nil {
		h.Logger.Error("failed to parse gmail push data", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	h.Logger.Info("received gmail notification",
		"email", pushData.EmailAddress,
		"history_id", pushData.HistoryID,
		"message_id", pubsubMsg.Message.MessageID)

	// Create notification
	notification := &GmailNotification{
		MessageID: pubsubMsg.Message.MessageID,
		HistoryID: pushData.HistoryID,
		Account:   pushData.EmailAddress,
		Timestamp: time.Now(),
	}

	// Dispatch to handler
	if h.OnMessage != nil {
		if err := h.OnMessage(notification); err != nil {
			h.Logger.Error("message handler error", "error", err)
			// Still return 200 to acknowledge the message to prevent retries
		}
	}

	// Trigger hook event
	event := NewEvent(EventGmailReceived, "push")
	event.Context["notification"] = notification
	event.Context["email"] = pushData.EmailAddress
	event.Context["history_id"] = pushData.HistoryID

	if err := Trigger(r.Context(), event); err != nil {
		h.Logger.Warn("failed to trigger gmail hook event", "error", err)
	}

	// Acknowledge the message
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ValidateToken validates the push token
func (h *GmailHookHandler) ValidateToken(token string) bool {
	if h.Config == nil || h.Config.PushToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.Config.PushToken)) == 1
}

// GmailHookServer manages the Gmail hook HTTP server
type GmailHookServer struct {
	Config    *GmailHookRuntimeConfig
	Handler   *GmailHookHandler
	Server    *http.Server
	Logger    *slog.Logger
	OnMessage func(*GmailNotification) error
}

// NewGmailHookServer creates a new Gmail hook server
func NewGmailHookServer(cfg *GmailHookRuntimeConfig, logger *slog.Logger) *GmailHookServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &GmailHookServer{
		Config: cfg,
		Logger: logger.With("component", "gmail-hook-server"),
	}
}

// SetOnMessage sets the message callback
func (s *GmailHookServer) SetOnMessage(handler func(*GmailNotification) error) {
	s.OnMessage = handler
}

// Start starts the Gmail hook server
func (s *GmailHookServer) Start(ctx context.Context) error {
	if s.Config == nil {
		return fmt.Errorf("gmail hook config is nil")
	}

	s.Handler = NewGmailHookHandler(s.Config, s.OnMessage, s.Logger)

	mux := http.NewServeMux()
	path := s.Config.Serve.Path
	if path == "" {
		path = DefaultGmailServePath
	}
	mux.Handle(path, s.Handler)

	addr := fmt.Sprintf("%s:%d", s.Config.Serve.Bind, s.Config.Serve.Port)
	s.Server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	s.Logger.Info("starting gmail hook server",
		"addr", addr,
		"path", path,
		"account", s.Config.Account)

	go func() {
		if err := s.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.Logger.Error("gmail hook server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully stops the Gmail hook server
func (s *GmailHookServer) Stop(ctx context.Context) error {
	if s.Server == nil {
		return nil
	}

	s.Logger.Info("stopping gmail hook server")

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return s.Server.Shutdown(shutdownCtx)
}

// GmailWakeHandler creates a hook handler that wakes the agent on Gmail notifications
func GmailWakeHandler(wakeFunc func(msg string)) Handler {
	return func(ctx context.Context, event *Event) error {
		if event.Type != EventGmailReceived {
			return nil
		}

		notification, ok := event.Context["notification"].(*GmailNotification)
		if !ok {
			return nil
		}

		msg := fmt.Sprintf("New email notification (history_id: %d)", notification.HistoryID)
		if notification.Account != "" {
			msg = fmt.Sprintf("New email for %s (history_id: %d)", notification.Account, notification.HistoryID)
		}

		wakeFunc(msg)
		return nil
	}
}

// ParseHistoryID parses a history ID string to uint64
func ParseHistoryID(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty history ID")
	}
	return strconv.ParseUint(s, 10, 64)
}

// FormatHistoryID formats a history ID as a string
func FormatHistoryID(id uint64) string {
	return strconv.FormatUint(id, 10)
}
