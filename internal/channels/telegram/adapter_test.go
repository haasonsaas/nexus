package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/haasonsaas/nexus/internal/channels"
	nexusmodels "github.com/haasonsaas/nexus/pkg/models"
)

// =============================================================================
// Mock BotClient Implementation
// =============================================================================

// mockBotClient implements BotClient for testing.
type mockBotClient struct {
	mu sync.Mutex

	// Configurable responses
	sendMessageFunc  func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
	sendPhotoFunc    func(ctx context.Context, params *bot.SendPhotoParams) (*models.Message, error)
	sendDocumentFunc func(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error)
	sendAudioFunc    func(ctx context.Context, params *bot.SendAudioParams) (*models.Message, error)
	sendChatActionFn func(ctx context.Context, params *bot.SendChatActionParams) (bool, error)
	getFileFunc      func(ctx context.Context, params *bot.GetFileParams) (*models.File, error)
	getMeFunc        func(ctx context.Context) (*models.User, error)
	setWebhookFunc   func(ctx context.Context, params *bot.SetWebhookParams) (bool, error)
	registerHandlers []bot.HandlerFunc
	startFunc        func(ctx context.Context)
	startWebhookFunc func(ctx context.Context)

	// Call tracking
	sendMessageCalls    int
	sendPhotoCalls      int
	sendDocumentCalls   int
	sendAudioCalls      int
	sendChatActionCalls int
	getFileCalls        int
	getMeCalls          int
	setWebhookCalls     int
}

func newMockBotClient() *mockBotClient {
	return &mockBotClient{
		registerHandlers: make([]bot.HandlerFunc, 0),
	}
}

func (m *mockBotClient) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	m.mu.Lock()
	m.sendMessageCalls++
	m.mu.Unlock()

	if m.sendMessageFunc != nil {
		return m.sendMessageFunc(ctx, params)
	}
	return &models.Message{ID: 12345}, nil
}

func (m *mockBotClient) SendPhoto(ctx context.Context, params *bot.SendPhotoParams) (*models.Message, error) {
	m.mu.Lock()
	m.sendPhotoCalls++
	m.mu.Unlock()

	if m.sendPhotoFunc != nil {
		return m.sendPhotoFunc(ctx, params)
	}
	return &models.Message{ID: 12346}, nil
}

func (m *mockBotClient) SendDocument(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	m.mu.Lock()
	m.sendDocumentCalls++
	m.mu.Unlock()

	if m.sendDocumentFunc != nil {
		return m.sendDocumentFunc(ctx, params)
	}
	return &models.Message{ID: 12347}, nil
}

func (m *mockBotClient) SendAudio(ctx context.Context, params *bot.SendAudioParams) (*models.Message, error) {
	m.mu.Lock()
	m.sendAudioCalls++
	m.mu.Unlock()

	if m.sendAudioFunc != nil {
		return m.sendAudioFunc(ctx, params)
	}
	return &models.Message{ID: 12348}, nil
}

func (m *mockBotClient) GetFile(ctx context.Context, params *bot.GetFileParams) (*models.File, error) {
	m.mu.Lock()
	m.getFileCalls++
	m.mu.Unlock()

	if m.getFileFunc != nil {
		return m.getFileFunc(ctx, params)
	}
	return &models.File{FileID: params.FileID, FilePath: "path/to/file.txt"}, nil
}

func (m *mockBotClient) GetMe(ctx context.Context) (*models.User, error) {
	m.mu.Lock()
	m.getMeCalls++
	m.mu.Unlock()

	if m.getMeFunc != nil {
		return m.getMeFunc(ctx)
	}
	return &models.User{ID: 123456, Username: "test_bot"}, nil
}

func (m *mockBotClient) SetWebhook(ctx context.Context, params *bot.SetWebhookParams) (bool, error) {
	m.mu.Lock()
	m.setWebhookCalls++
	m.mu.Unlock()

	if m.setWebhookFunc != nil {
		return m.setWebhookFunc(ctx, params)
	}
	return true, nil
}

func (m *mockBotClient) RegisterHandler(handlerType bot.HandlerType, pattern string, matchType bot.MatchType, handler bot.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerHandlers = append(m.registerHandlers, handler)
}

func (m *mockBotClient) RegisterHandlerMatchFunc(matchFunc bot.MatchFunc, handler bot.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerHandlers = append(m.registerHandlers, handler)
}

func (m *mockBotClient) Start(ctx context.Context) {
	if m.startFunc != nil {
		m.startFunc(ctx)
		return
	}
	<-ctx.Done()
}

func (m *mockBotClient) StartWebhook(ctx context.Context) {
	if m.startWebhookFunc != nil {
		m.startWebhookFunc(ctx)
		return
	}
	<-ctx.Done()
}

func (m *mockBotClient) SendChatAction(ctx context.Context, params *bot.SendChatActionParams) (bool, error) {
	m.mu.Lock()
	m.sendChatActionCalls++
	m.mu.Unlock()

	if m.sendChatActionFn != nil {
		return m.sendChatActionFn(ctx, params)
	}
	return true, nil
}

func (m *mockBotClient) EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	return &models.Message{ID: int(params.MessageID)}, nil
}

func (m *mockBotClient) getSendMessageCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendMessageCalls
}

func (m *mockBotClient) getSendPhotoCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendPhotoCalls
}

func (m *mockBotClient) getSendDocumentCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendDocumentCalls
}

func (m *mockBotClient) getSendAudioCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendAudioCalls
}

func (m *mockBotClient) getGetMeCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getMeCalls
}

// =============================================================================
// Configuration Tests
// =============================================================================

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid long polling config",
			cfg: Config{
				Token: "valid-token",
				Mode:  ModeLongPolling,
			},
			wantErr: false,
		},
		{
			name: "valid webhook config",
			cfg: Config{
				Token:      "valid-token",
				Mode:       ModeWebhook,
				WebhookURL: "https://example.com/webhook",
			},
			wantErr: false,
		},
		{
			name: "missing token",
			cfg: Config{
				Mode: ModeLongPolling,
			},
			wantErr: true,
			errMsg:  "token is required",
		},
		{
			name: "webhook without URL",
			cfg: Config{
				Token: "valid-token",
				Mode:  ModeWebhook,
			},
			wantErr: true,
			errMsg:  "webhook_url is required",
		},
		{
			name: "empty mode defaults to long polling",
			cfg: Config{
				Token: "valid-token",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				var chErr *channels.Error
				if errors.As(err, &chErr) {
					if chErr.Code != channels.ErrCodeConfig {
						t.Errorf("Expected ErrCodeConfig, got %v", chErr.Code)
					}
				}
			}
		})
	}
}

func TestConfig_DefaultValues(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Check default values were applied
	if cfg.MaxReconnectAttempts != 5 {
		t.Errorf("MaxReconnectAttempts = %d, want 5", cfg.MaxReconnectAttempts)
	}

	if cfg.ReconnectDelay != 5*time.Second {
		t.Errorf("ReconnectDelay = %v, want 5s", cfg.ReconnectDelay)
	}

	if cfg.RateLimit != 30 {
		t.Errorf("RateLimit = %f, want 30", cfg.RateLimit)
	}

	if cfg.RateBurst != 20 {
		t.Errorf("RateBurst = %d, want 20", cfg.RateBurst)
	}

	if cfg.Logger == nil {
		t.Error("Logger should not be nil after validation")
	}
}

func TestConfig_CustomValues(t *testing.T) {
	logger := slog.Default()
	cfg := Config{
		Token:                "test-token",
		Mode:                 ModeWebhook,
		WebhookURL:           "https://example.com/webhook",
		MaxReconnectAttempts: 10,
		ReconnectDelay:       10 * time.Second,
		RateLimit:            50,
		RateBurst:            30,
		Logger:               logger,
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Custom values should be preserved
	if cfg.MaxReconnectAttempts != 10 {
		t.Errorf("MaxReconnectAttempts = %d, want 10", cfg.MaxReconnectAttempts)
	}

	if cfg.ReconnectDelay != 10*time.Second {
		t.Errorf("ReconnectDelay = %v, want 10s", cfg.ReconnectDelay)
	}

	if cfg.RateLimit != 50 {
		t.Errorf("RateLimit = %f, want 50", cfg.RateLimit)
	}

	if cfg.RateBurst != 30 {
		t.Errorf("RateBurst = %d, want 30", cfg.RateBurst)
	}
}

// =============================================================================
// Adapter Interface Tests
// =============================================================================

func TestAdapter_Type(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if got := adapter.Type(); got != nexusmodels.ChannelTelegram {
		t.Errorf("Type() = %v, want %v", got, nexusmodels.ChannelTelegram)
	}
}

func TestAdapter_Status(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Initially not connected
	status := adapter.Status()
	if status.Connected {
		t.Error("Status().Connected = true, want false")
	}
	if status.Error != "" {
		t.Errorf("Status().Error = %q, want empty", status.Error)
	}
}

func TestAdapter_Messages(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	msgChan := adapter.Messages()
	if msgChan == nil {
		t.Error("Messages() returned nil channel")
	}
}

func TestAdapter_Metrics(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	metrics := adapter.Metrics()
	if metrics.ChannelType != nexusmodels.ChannelTelegram {
		t.Errorf("Metrics().ChannelType = %v, want %v", metrics.ChannelType, nexusmodels.ChannelTelegram)
	}
}

func TestAdapter_InterfaceCompliance(t *testing.T) {
	// Verify Adapter implements all expected interfaces
	var _ channels.Adapter = (*Adapter)(nil)
	var _ channels.LifecycleAdapter = (*Adapter)(nil)
	var _ channels.OutboundAdapter = (*Adapter)(nil)
	var _ channels.InboundAdapter = (*Adapter)(nil)
	var _ channels.HealthAdapter = (*Adapter)(nil)
	var _ channels.AttachmentDownloader = (*Adapter)(nil)
}

// =============================================================================
// NewAdapter Tests
// =============================================================================

func TestNewAdapter_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "empty token",
			cfg:  Config{Token: "", Mode: ModeLongPolling},
		},
		{
			name: "webhook without URL",
			cfg:  Config{Token: "test", Mode: ModeWebhook},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := NewAdapter(tt.cfg)
			if err == nil {
				t.Error("NewAdapter() expected error, got nil")
			}
			if adapter != nil {
				t.Error("NewAdapter() expected nil adapter on error")
			}
		})
	}
}

func TestNewAdapter_ValidConfig(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if adapter == nil {
		t.Fatal("NewAdapter() returned nil adapter")
	}

	// Verify internal state
	if adapter.config.Token != "test-token" {
		t.Errorf("adapter.config.Token = %q, want %q", adapter.config.Token, "test-token")
	}

	if adapter.messages == nil {
		t.Error("adapter.messages channel is nil")
	}

	if adapter.rateLimiter == nil {
		t.Error("adapter.rateLimiter is nil")
	}

	if adapter.health == nil {
		t.Error("adapter.health is nil")
	}

	if adapter.logger == nil {
		t.Error("adapter.logger is nil")
	}
}

// =============================================================================
// Send Tests with Mock BotClient
// =============================================================================

func TestAdapter_SendWithMock(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if mock.getSendMessageCalls() != 1 {
		t.Errorf("SendMessage called %d times, want 1", mock.getSendMessageCalls())
	}
}

func TestAdapter_SendWithAttachments(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Test with attachments",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
		Attachments: []nexusmodels.Attachment{
			{Type: "image", URL: "https://example.com/photo.jpg"},
			{Type: "document", URL: "https://example.com/doc.pdf"},
			{Type: "audio", URL: "https://example.com/audio.mp3"},
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if mock.getSendMessageCalls() != 1 {
		t.Errorf("SendMessage called %d times, want 1", mock.getSendMessageCalls())
	}
	if mock.getSendPhotoCalls() != 1 {
		t.Errorf("SendPhoto called %d times, want 1", mock.getSendPhotoCalls())
	}
	if mock.getSendDocumentCalls() != 1 {
		t.Errorf("SendDocument called %d times, want 1", mock.getSendDocumentCalls())
	}
	if mock.getSendAudioCalls() != 1 {
		t.Errorf("SendAudio called %d times, want 1", mock.getSendAudioCalls())
	}
}

func TestAdapter_SendError(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		return nil, errors.New("network error")
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInternal {
			t.Errorf("Expected ErrCodeInternal, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendRateLimitError(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		return nil, context.DeadlineExceeded
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeRateLimit {
			t.Errorf("Expected ErrCodeRateLimit, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendWithoutBot(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	msg := &nexusmodels.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when bot is not initialized")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInternal {
			t.Errorf("Expected ErrCodeInternal, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendWithInvalidChatID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content:  "Test message",
		Metadata: map[string]any{},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when chat_id is missing")
	}
}

func TestAdapter_SendWithReplyTo(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	var capturedParams *bot.SendMessageParams
	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		capturedParams = params
		return &models.Message{ID: 12345}, nil
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Reply message",
		Metadata: map[string]any{
			"chat_id":             int64(123456),
			"reply_to_message_id": 999,
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if capturedParams.ReplyParameters == nil {
		t.Error("ReplyParameters should be set")
	} else if capturedParams.ReplyParameters.MessageID != 999 {
		t.Errorf("ReplyParameters.MessageID = %d, want 999", capturedParams.ReplyParameters.MessageID)
	}
}

func TestAdapter_SendWithThreadID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	var capturedParams *bot.SendMessageParams
	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		capturedParams = params
		return &models.Message{ID: 12345}, nil
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Threaded message",
		Metadata: map[string]any{
			"chat_id":           int64(123456),
			"message_thread_id": 77,
		},
	}

	if err := adapter.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if capturedParams == nil {
		t.Fatal("expected SendMessageParams to be captured")
	}
	if capturedParams.MessageThreadID != 77 {
		t.Errorf("MessageThreadID = %d, want 77", capturedParams.MessageThreadID)
	}
}

func TestAdapter_SendWithGeneralTopicThreadID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	var capturedParams *bot.SendMessageParams
	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		capturedParams = params
		return &models.Message{ID: 12345}, nil
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "General topic message",
		Metadata: map[string]any{
			"chat_id":           int64(123456),
			"message_thread_id": 1,
		},
	}

	if err := adapter.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if capturedParams == nil {
		t.Fatal("expected SendMessageParams to be captured")
	}
	if capturedParams.MessageThreadID != 0 {
		t.Errorf("MessageThreadID = %d, want 0", capturedParams.MessageThreadID)
	}
}

func TestAdapter_SendTypingIndicatorWithThreadID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	var capturedParams *bot.SendChatActionParams
	mock := newMockBotClient()
	mock.sendChatActionFn = func(ctx context.Context, params *bot.SendChatActionParams) (bool, error) {
		capturedParams = params
		return true, nil
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Metadata: map[string]any{
			"chat_id":           int64(123456),
			"message_thread_id": 1,
		},
	}

	if err := adapter.SendTypingIndicator(context.Background(), msg); err != nil {
		t.Fatalf("SendTypingIndicator() error = %v", err)
	}

	if capturedParams == nil {
		t.Fatal("expected SendChatActionParams to be captured")
	}
	if capturedParams.MessageThreadID != 1 {
		t.Errorf("MessageThreadID = %d, want 1", capturedParams.MessageThreadID)
	}
}

// =============================================================================
// Health Check Tests with Mock
// =============================================================================

func TestAdapter_HealthCheckWithMock(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if !health.Healthy {
		t.Error("Expected Healthy = true")
	}
	if health.Message != "healthy" {
		t.Errorf("Expected message 'healthy', got %q", health.Message)
	}
	if mock.getGetMeCalls() != 1 {
		t.Errorf("GetMe called %d times, want 1", mock.getGetMeCalls())
	}
}

func TestAdapter_HealthCheckError(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.getMeFunc = func(ctx context.Context) (*models.User, error) {
		return nil, errors.New("connection error")
	}
	adapter.SetBotClient(mock)

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if health.Healthy {
		t.Error("Expected Healthy = false")
	}
	if !strings.Contains(health.Message, "health check failed") {
		t.Errorf("Expected message to contain 'health check failed', got %q", health.Message)
	}
}

func TestAdapter_HealthCheckWithoutBot(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if health.Healthy {
		t.Error("Expected Healthy = false when bot is not initialized")
	}
	if health.Message != "bot not initialized (start adapter)" {
		t.Errorf("Expected message 'bot not initialized (start adapter)', got %q", health.Message)
	}
	if health.Latency < 0 {
		t.Error("Expected Latency >= 0")
	}
}

func TestAdapter_HealthCheckDegraded(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)
	adapter.setDegraded(true)

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if !health.Healthy {
		t.Error("Expected Healthy = true even in degraded mode")
	}
	if !health.Degraded {
		t.Error("Expected Degraded = true")
	}
	if health.Message != "operating in degraded mode" {
		t.Errorf("Expected message 'operating in degraded mode', got %q", health.Message)
	}
}

// =============================================================================
// Message Conversion Tests
// =============================================================================

func TestConvertTelegramMessage_TextMessage(t *testing.T) {
	tests := []struct {
		name     string
		teleMsg  *mockTelegramMessage
		wantText string
		wantRole nexusmodels.Role
	}{
		{
			name: "simple text message",
			teleMsg: &mockTelegramMessage{
				messageID: 123,
				chatID:    456789,
				text:      "Hello, world!",
				fromID:    111,
				fromFirst: "John",
				fromLast:  "Doe",
				date:      time.Now().Unix(),
			},
			wantText: "Hello, world!",
			wantRole: nexusmodels.RoleUser,
		},
		{
			name: "empty text message",
			teleMsg: &mockTelegramMessage{
				messageID: 124,
				chatID:    456789,
				text:      "",
				fromID:    111,
				fromFirst: "John",
				date:      time.Now().Unix(),
			},
			wantText: "",
			wantRole: nexusmodels.RoleUser,
		},
		{
			name: "message with unicode",
			teleMsg: &mockTelegramMessage{
				messageID: 125,
				chatID:    456789,
				text:      "Hello! How are you?",
				fromID:    111,
				fromFirst: "Alice",
				date:      time.Now().Unix(),
			},
			wantText: "Hello! How are you?",
			wantRole: nexusmodels.RoleUser,
		},
		{
			name: "long message",
			teleMsg: &mockTelegramMessage{
				messageID: 126,
				chatID:    456789,
				text:      "This is a very long message that spans multiple lines and contains a lot of text. " + "It should be converted correctly without any truncation or modification.",
				fromID:    111,
				fromFirst: "Bob",
				date:      time.Now().Unix(),
			},
			wantText: "This is a very long message that spans multiple lines and contains a lot of text. " + "It should be converted correctly without any truncation or modification.",
			wantRole: nexusmodels.RoleUser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTelegramMessage(tt.teleMsg)

			if got.Content != tt.wantText {
				t.Errorf("Content = %q, want %q", got.Content, tt.wantText)
			}

			if got.Role != tt.wantRole {
				t.Errorf("Role = %v, want %v", got.Role, tt.wantRole)
			}

			if got.Channel != nexusmodels.ChannelTelegram {
				t.Errorf("Channel = %v, want %v", got.Channel, nexusmodels.ChannelTelegram)
			}

			if got.Direction != nexusmodels.DirectionInbound {
				t.Errorf("Direction = %v, want %v", got.Direction, nexusmodels.DirectionInbound)
			}
		})
	}
}

func TestConvertTelegramMessage_Metadata(t *testing.T) {
	timestamp := time.Now().Unix()
	teleMsg := &mockTelegramMessage{
		messageID:       123,
		chatID:          456789,
		text:            "Test message",
		fromID:          111,
		fromFirst:       "John",
		fromLast:        "Doe",
		date:            timestamp,
		messageThreadID: 99,
	}

	got := convertTelegramMessage(teleMsg)

	// Check metadata extraction
	if got.Metadata == nil {
		t.Fatal("Metadata is nil")
	}

	if got.Metadata["chat_id"] != int64(456789) {
		t.Errorf("Metadata[chat_id] = %v, want %v", got.Metadata["chat_id"], int64(456789))
	}

	if got.Metadata["user_id"] != int64(111) {
		t.Errorf("Metadata[user_id] = %v, want %v", got.Metadata["user_id"], int64(111))
	}

	if got.Metadata["user_first"] != "John" {
		t.Errorf("Metadata[user_first] = %v, want %v", got.Metadata["user_first"], "John")
	}

	if got.Metadata["user_last"] != "Doe" {
		t.Errorf("Metadata[user_last] = %v, want %v", got.Metadata["user_last"], "Doe")
	}
	if got.Metadata["message_thread_id"] != 99 {
		t.Errorf("Metadata[message_thread_id] = %v, want %v", got.Metadata["message_thread_id"], 99)
	}

	// Check session ID format
	expectedSessionID := fmt.Sprintf("telegram:%d", 456789)
	if got.SessionID != expectedSessionID {
		t.Errorf("SessionID = %v, want %v", got.SessionID, expectedSessionID)
	}

	// Check ID format
	expectedID := fmt.Sprintf("tg_%d", 123)
	if got.ID != expectedID {
		t.Errorf("ID = %v, want %v", got.ID, expectedID)
	}
}

func TestConvertTelegramMessage_WithAttachments(t *testing.T) {
	tests := []struct {
		name            string
		teleMsg         *mockTelegramMessage
		wantAttachType  string
		wantAttachCount int
		checkMetadata   func(t *testing.T, metadata map[string]any)
	}{
		{
			name: "photo attachment",
			teleMsg: &mockTelegramMessage{
				messageID: 125,
				chatID:    456789,
				text:      "Check this photo",
				fromID:    111,
				fromFirst: "John",
				date:      time.Now().Unix(),
				hasPhoto:  true,
				photoID:   "photo123",
			},
			wantAttachType:  "image",
			wantAttachCount: 1,
		},
		{
			name: "document attachment",
			teleMsg: &mockTelegramMessage{
				messageID: 126,
				chatID:    456789,
				text:      "Here's a document",
				fromID:    111,
				fromFirst: "John",
				date:      time.Now().Unix(),
				hasDoc:    true,
				docID:     "doc123",
				docName:   "report.pdf",
				docMime:   "application/pdf",
			},
			wantAttachType:  "document",
			wantAttachCount: 1,
		},
		{
			name: "audio attachment",
			teleMsg: &mockTelegramMessage{
				messageID: 127,
				chatID:    456789,
				fromID:    111,
				fromFirst: "John",
				date:      time.Now().Unix(),
				hasAudio:  true,
				audioID:   "audio123",
			},
			wantAttachType:  "audio",
			wantAttachCount: 1,
		},
		{
			name: "voice attachment",
			teleMsg: &mockTelegramMessage{
				messageID:     128,
				chatID:        456789,
				fromID:        111,
				fromFirst:     "John",
				date:          time.Now().Unix(),
				hasVoice:      true,
				voiceID:       "voice123",
				voiceDuration: 15,
				voiceMimeType: "audio/ogg",
			},
			wantAttachType:  "voice",
			wantAttachCount: 1,
			checkMetadata: func(t *testing.T, metadata map[string]any) {
				if metadata["has_voice"] != true {
					t.Errorf("has_voice = %v, want true", metadata["has_voice"])
				}
				if metadata["voice_duration"] != 15 {
					t.Errorf("voice_duration = %v, want 15", metadata["voice_duration"])
				}
				if metadata["voice_file_id"] != "voice123" {
					t.Errorf("voice_file_id = %v, want voice123", metadata["voice_file_id"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTelegramMessage(tt.teleMsg)

			if len(got.Attachments) != tt.wantAttachCount {
				t.Fatalf("Attachments count = %d, want %d", len(got.Attachments), tt.wantAttachCount)
			}

			if got.Attachments[0].Type != tt.wantAttachType {
				t.Errorf("Attachment type = %v, want %v", got.Attachments[0].Type, tt.wantAttachType)
			}

			if tt.checkMetadata != nil {
				tt.checkMetadata(t, got.Metadata)
			}
		})
	}
}

func TestConvertTelegramMessage_MultipleAttachments(t *testing.T) {
	teleMsg := &mockTelegramMessage{
		messageID: 130,
		chatID:    456789,
		text:      "Multiple attachments",
		fromID:    111,
		fromFirst: "John",
		date:      time.Now().Unix(),
		hasPhoto:  true,
		photoID:   "photo123",
		hasDoc:    true,
		docID:     "doc123",
		docName:   "file.txt",
		docMime:   "text/plain",
	}

	got := convertTelegramMessage(teleMsg)

	if len(got.Attachments) != 2 {
		t.Fatalf("Attachments count = %d, want 2", len(got.Attachments))
	}

	// Check photo attachment
	hasPhoto := false
	hasDoc := false
	for _, att := range got.Attachments {
		if att.Type == "image" && att.ID == "photo123" {
			hasPhoto = true
		}
		if att.Type == "document" && att.ID == "doc123" {
			hasDoc = true
		}
	}

	if !hasPhoto {
		t.Error("Photo attachment not found")
	}
	if !hasDoc {
		t.Error("Document attachment not found")
	}
}

func TestConvertTelegramMessage_DocumentDetails(t *testing.T) {
	teleMsg := &mockTelegramMessage{
		messageID: 126,
		chatID:    456789,
		text:      "Document",
		fromID:    111,
		fromFirst: "John",
		date:      time.Now().Unix(),
		hasDoc:    true,
		docID:     "doc123",
		docName:   "report.pdf",
		docMime:   "application/pdf",
	}

	got := convertTelegramMessage(teleMsg)

	if len(got.Attachments) != 1 {
		t.Fatalf("Expected 1 attachment, got %d", len(got.Attachments))
	}

	att := got.Attachments[0]
	if att.ID != "doc123" {
		t.Errorf("Attachment ID = %q, want %q", att.ID, "doc123")
	}
	if att.Filename != "report.pdf" {
		t.Errorf("Attachment Filename = %q, want %q", att.Filename, "report.pdf")
	}
	if att.MimeType != "application/pdf" {
		t.Errorf("Attachment MimeType = %q, want %q", att.MimeType, "application/pdf")
	}
}

// =============================================================================
// Extract Chat ID Tests
// =============================================================================

func TestExtractChatID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	tests := []struct {
		name    string
		msg     *nexusmodels.Message
		wantID  int64
		wantErr bool
	}{
		{
			name: "chat_id as int64 in metadata",
			msg: &nexusmodels.Message{
				Metadata: map[string]any{
					"chat_id": int64(123456),
				},
			},
			wantID:  123456,
			wantErr: false,
		},
		{
			name: "chat_id as int in metadata",
			msg: &nexusmodels.Message{
				Metadata: map[string]any{
					"chat_id": 123456,
				},
			},
			wantID:  123456,
			wantErr: false,
		},
		{
			name: "chat_id as string in metadata",
			msg: &nexusmodels.Message{
				Metadata: map[string]any{
					"chat_id": "123456",
				},
			},
			wantID:  123456,
			wantErr: false,
		},
		{
			name: "chat_id from session ID",
			msg: &nexusmodels.Message{
				SessionID: "telegram:789012",
				Metadata:  map[string]any{},
			},
			wantID:  789012,
			wantErr: false,
		},
		{
			name: "no chat_id available",
			msg: &nexusmodels.Message{
				SessionID: "invalid-format",
				Metadata:  map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "nil metadata",
			msg: &nexusmodels.Message{
				SessionID: "telegram:456789",
			},
			wantID:  456789,
			wantErr: false,
		},
		{
			name:    "empty message",
			msg:     &nexusmodels.Message{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := adapter.extractChatID(tt.msg)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractChatID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantID {
				t.Errorf("extractChatID() = %v, want %v", got, tt.wantID)
			}
		})
	}
}

// =============================================================================
// Rate Limit Error Detection Tests
// =============================================================================

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRateLimitError(tt.err); got != tt.want {
				t.Errorf("isRateLimitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Status Update Tests
// =============================================================================

func TestAdapter_StatusUpdate(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	// Test updateStatus
	adapter.updateStatus(true, "")
	status := adapter.Status()
	if !status.Connected {
		t.Error("Expected Connected = true")
	}
	if status.Error != "" {
		t.Errorf("Expected empty error, got %q", status.Error)
	}

	// Test with error
	adapter.updateStatus(false, "connection lost")
	status = adapter.Status()
	if status.Connected {
		t.Error("Expected Connected = false")
	}
	if status.Error != "connection lost" {
		t.Errorf("Expected error 'connection lost', got %q", status.Error)
	}
}

func TestAdapter_LastPingUpdate(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	before := time.Now().Unix()
	adapter.updateLastPing()
	after := time.Now().Unix()

	status := adapter.Status()
	if status.LastPing < before || status.LastPing > after {
		t.Errorf("LastPing = %d, expected between %d and %d", status.LastPing, before, after)
	}
}

// =============================================================================
// Degraded Mode Tests
// =============================================================================

func TestAdapter_DegradedMode(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	// Initially not degraded
	if adapter.isDegraded() {
		t.Error("Expected not degraded initially")
	}

	// Set degraded
	adapter.setDegraded(true)
	if !adapter.isDegraded() {
		t.Error("Expected degraded after setDegraded(true)")
	}

	// Clear degraded
	adapter.setDegraded(false)
	if adapter.isDegraded() {
		t.Error("Expected not degraded after setDegraded(false)")
	}
}

// =============================================================================
// Lifecycle Tests
// =============================================================================

func TestAdapter_Lifecycle(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start should not block (but will fail without real token)
	errChan := make(chan error, 1)
	go func() {
		errChan <- adapter.Start(ctx)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop should work
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Wait for start to complete
	select {
	case <-errChan:
		// Expected to complete after stop
	case <-time.After(3 * time.Second):
		t.Error("Start() did not return after Stop()")
	}
}

func TestAdapter_StopTimeout(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Stop with already-cancelled context should handle gracefully
	err := adapter.Stop(ctx)
	// May or may not return error depending on timing
	_ = err
}

// =============================================================================
// Download Attachment Tests
// =============================================================================

func TestAdapter_DownloadAttachmentWithMock(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.getFileFunc = func(ctx context.Context, params *bot.GetFileParams) (*models.File, error) {
		return &models.File{
			FileID:   params.FileID,
			FilePath: "photos/file_123.jpg",
		}, nil
	}
	adapter.SetBotClient(mock)

	// Create a test server for file download
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake image data"))
	}))
	defer server.Close()

	// Override httpClient to use the test server
	adapter.httpClient = server.Client()

	msg := &nexusmodels.Message{}
	att := &nexusmodels.Attachment{ID: "file123", MimeType: "image/jpeg"}

	ctx := context.Background()
	// Note: This will fail because httpClient doesn't point to our test server
	// In a real scenario, we'd need to mock the HTTP client more thoroughly
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, att)
	// This will fail due to URL construction, but at least we test the GetFile call
	_ = err

	if mock.getFileCalls != 1 {
		t.Errorf("GetFile called %d times, want 1", mock.getFileCalls)
	}
}

func TestAdapter_DownloadAttachmentWithoutBot(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	msg := &nexusmodels.Message{}
	att := &nexusmodels.Attachment{ID: "file123"}

	ctx := context.Background()
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, att)

	if err == nil {
		t.Error("Expected error when bot is not initialized")
	}
}

func TestAdapter_DownloadAttachmentNilAttachment(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{}

	ctx := context.Background()
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, nil)

	if err == nil {
		t.Error("Expected error for nil attachment")
	}
}

func TestAdapter_DownloadAttachmentMissingFileID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{Metadata: map[string]any{}}
	att := &nexusmodels.Attachment{ID: ""} // Empty ID

	ctx := context.Background()
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, att)

	if err == nil {
		t.Error("Expected error for missing file ID")
	}
}

func TestAdapter_DownloadAttachmentVoiceFileID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.getFileFunc = func(ctx context.Context, params *bot.GetFileParams) (*models.File, error) {
		if params.FileID != "voice_file_123" {
			t.Errorf("Expected FileID 'voice_file_123', got %q", params.FileID)
		}
		return &models.File{
			FileID:   params.FileID,
			FilePath: "voice/voice_123.ogg",
		}, nil
	}
	adapter.SetBotClient(mock)

	// Attachment ID is empty but voice_file_id is in metadata
	msg := &nexusmodels.Message{
		Metadata: map[string]any{
			"voice_file_id": "voice_file_123",
		},
	}
	att := &nexusmodels.Attachment{ID: ""} // Empty ID

	ctx := context.Background()
	// This will still fail at HTTP download but tests the voice_file_id fallback
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, att)
	_ = err

	if mock.getFileCalls != 1 {
		t.Errorf("GetFile should have been called")
	}
}

// =============================================================================
// Mode Constants Tests
// =============================================================================

func TestModeConstants(t *testing.T) {
	if ModeLongPolling != "long_polling" {
		t.Errorf("ModeLongPolling = %q, want %q", ModeLongPolling, "long_polling")
	}

	if ModeWebhook != "webhook" {
		t.Errorf("ModeWebhook = %q, want %q", ModeWebhook, "webhook")
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestAdapter_ConcurrentSend(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling, RateLimit: 100, RateBurst: 100}
	adapter, _ := NewAdapter(cfg)

	var callCount int64
	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		atomic.AddInt64(&callCount, 1)
		time.Sleep(10 * time.Millisecond) // Simulate some latency
		return &models.Message{ID: int(atomic.LoadInt64(&callCount))}, nil
	}
	adapter.SetBotClient(mock)

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := &nexusmodels.Message{
				Content: fmt.Sprintf("Message %d", i),
				Metadata: map[string]any{
					"chat_id": int64(123456),
				},
			}
			if err := adapter.Send(context.Background(), msg); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent Send() error: %v", err)
	}

	if atomic.LoadInt64(&callCount) != numGoroutines {
		t.Errorf("SendMessage called %d times, want %d", atomic.LoadInt64(&callCount), numGoroutines)
	}
}

func TestAdapter_ConcurrentStatusReads(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	const numReaders = 10
	const numUpdates = 100

	var wg sync.WaitGroup

	// Start readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numUpdates; j++ {
				_ = adapter.Status()
			}
		}()
	}

	// Start writers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < numUpdates; j++ {
			adapter.updateStatus(j%2 == 0, "")
			adapter.updateLastPing()
		}
	}()

	wg.Wait()
}

func TestAdapter_ConcurrentDegradedMode(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	const numGoroutines = 10
	const numOps = 100

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				if j%2 == 0 {
					adapter.setDegraded(true)
				} else {
					adapter.setDegraded(false)
				}
				_ = adapter.isDegraded()
			}
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestAdapter_SendLargeMessage(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	var capturedContent string
	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		capturedContent = params.Text
		return &models.Message{ID: 12345}, nil
	}
	adapter.SetBotClient(mock)

	// Create a 100KB+ message
	largeContent := strings.Repeat("A", 100*1024)
	msg := &nexusmodels.Message{
		Content: largeContent,
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if len(capturedContent) != len(largeContent) {
		t.Errorf("Message length = %d, want %d", len(capturedContent), len(largeContent))
	}
}

func TestAdapter_SendUnicodeEmoji(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	var capturedContent string
	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		capturedContent = params.Text
		return &models.Message{ID: 12345}, nil
	}
	adapter.SetBotClient(mock)

	// Unicode and emoji content
	unicodeContent := "Hello! Bonjour! Hallo! Ciao! Testing emojis: Message"
	msg := &nexusmodels.Message{
		Content: unicodeContent,
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if capturedContent != unicodeContent {
		t.Errorf("Unicode content not preserved. Got %q", capturedContent)
	}
}

func TestAdapter_SendEmptyMessage(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Empty messages should still be sent (Telegram will handle validation)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestAdapter_SendNilMetadata(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content:  "Test",
		Metadata: nil,
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Should fail because chat_id is required
	if err == nil {
		t.Error("Expected error for nil metadata")
	}
}

func TestConvertTelegramMessage_NilUser(t *testing.T) {
	teleMsg := &mockTelegramMessage{
		messageID: 123,
		chatID:    456789,
		text:      "Test",
		fromID:    0, // No user ID
		fromFirst: "",
		fromLast:  "",
		date:      time.Now().Unix(),
	}

	got := convertTelegramMessage(teleMsg)

	if got == nil {
		t.Fatal("Expected non-nil message")
	}

	// User metadata should have default values
	if got.Metadata["user_id"] != int64(0) {
		t.Errorf("Expected user_id = 0, got %v", got.Metadata["user_id"])
	}
}

func TestConvertTelegramMessage_SpecialCharacters(t *testing.T) {
	specialContent := `Special chars: <>&"'` + "`" + `\n\t\r`
	teleMsg := &mockTelegramMessage{
		messageID: 123,
		chatID:    456789,
		text:      specialContent,
		fromID:    111,
		fromFirst: "John",
		date:      time.Now().Unix(),
	}

	got := convertTelegramMessage(teleMsg)

	if got.Content != specialContent {
		t.Errorf("Special characters not preserved. Got %q, want %q", got.Content, specialContent)
	}
}

// =============================================================================
// Webhook Handler Tests
// =============================================================================

func TestWebhookUpdateParsing(t *testing.T) {
	// Test parsing of Telegram Update JSON
	updateJSON := `{
		"update_id": 123456789,
		"message": {
			"message_id": 123,
			"from": {
				"id": 111,
				"first_name": "John",
				"last_name": "Doe"
			},
			"chat": {
				"id": 456789,
				"type": "private"
			},
			"date": 1234567890,
			"text": "Hello, bot!"
		}
	}`

	var update models.Update
	err := json.Unmarshal([]byte(updateJSON), &update)
	if err != nil {
		t.Fatalf("Failed to unmarshal update: %v", err)
	}

	if update.ID != 123456789 {
		t.Errorf("UpdateID = %d, want 123456789", update.ID)
	}

	if update.Message == nil {
		t.Fatal("Message is nil")
	}

	if update.Message.Text != "Hello, bot!" {
		t.Errorf("Message.Text = %q, want %q", update.Message.Text, "Hello, bot!")
	}

	if update.Message.Chat.ID != 456789 {
		t.Errorf("Chat.ID = %d, want 456789", update.Message.Chat.ID)
	}
}

func TestWebhookUpdateParsing_MalformedJSON(t *testing.T) {
	malformedJSON := `{"update_id": 123, "message": {invalid`

	var update models.Update
	err := json.Unmarshal([]byte(malformedJSON), &update)

	if err == nil {
		t.Error("Expected error for malformed JSON")
	}
}

func TestWebhookUpdateParsing_MissingFields(t *testing.T) {
	// Update with no message
	updateJSON := `{"update_id": 123456789}`

	var update models.Update
	err := json.Unmarshal([]byte(updateJSON), &update)
	if err != nil {
		t.Fatalf("Failed to unmarshal update: %v", err)
	}

	if update.Message != nil {
		t.Error("Expected nil message")
	}
}

func TestWebhookUpdateParsing_CallbackQuery(t *testing.T) {
	// Test callback query update (for inline keyboard buttons)
	updateJSON := `{
		"update_id": 123456790,
		"callback_query": {
			"id": "callback123",
			"from": {
				"id": 111,
				"first_name": "John"
			},
			"message": {
				"message_id": 456,
				"chat": {
					"id": 456789
				}
			},
			"data": "button_clicked"
		}
	}`

	var update models.Update
	err := json.Unmarshal([]byte(updateJSON), &update)
	if err != nil {
		t.Fatalf("Failed to unmarshal update: %v", err)
	}

	if update.Message != nil {
		t.Error("Expected nil message for callback query")
	}

	if update.CallbackQuery == nil {
		t.Fatal("Expected CallbackQuery to be set")
	}

	if update.CallbackQuery.Data != "button_clicked" {
		t.Errorf("CallbackQuery.Data = %q, want %q", update.CallbackQuery.Data, "button_clicked")
	}
}

// =============================================================================
// Rate Limit Recovery Tests
// =============================================================================

func TestAdapter_RateLimitRecovery(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling, RateLimit: 100, RateBurst: 100}
	adapter, _ := NewAdapter(cfg)

	callCount := 0
	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		callCount++
		if callCount <= 2 {
			return nil, context.DeadlineExceeded // Simulate rate limit
		}
		return &models.Message{ID: callCount}, nil
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx := context.Background()

	// First two calls should return rate limit error
	err1 := adapter.Send(ctx, msg)
	if err1 == nil {
		t.Error("Expected rate limit error on first call")
	}

	err2 := adapter.Send(ctx, msg)
	if err2 == nil {
		t.Error("Expected rate limit error on second call")
	}

	// Third call should succeed
	err3 := adapter.Send(ctx, msg)
	if err3 != nil {
		t.Errorf("Expected success on third call, got error: %v", err3)
	}
}

func TestAdapter_SendContextCancellation(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.sendMessageFunc = func(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			time.Sleep(100 * time.Millisecond)
			return &models.Message{ID: 12345}, nil
		}
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := adapter.Send(ctx, msg)
	if err == nil {
		t.Error("Expected error when context is cancelled")
	}
}

// =============================================================================
// Attachment Sending Tests
// =============================================================================

func TestAdapter_SendPhotoError(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.sendPhotoFunc = func(ctx context.Context, params *bot.SendPhotoParams) (*models.Message, error) {
		return nil, errors.New("photo upload failed")
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Photo",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
		Attachments: []nexusmodels.Attachment{
			{Type: "image", URL: "https://example.com/photo.jpg"},
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Message send should succeed, attachment failure is logged but not fatal
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestAdapter_SendDocumentError(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.sendDocumentFunc = func(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
		return nil, errors.New("document upload failed")
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Document",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
		Attachments: []nexusmodels.Attachment{
			{Type: "document", URL: "https://example.com/doc.pdf"},
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Message send should succeed, attachment failure is logged but not fatal
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestAdapter_SendAudioError(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	mock.sendAudioFunc = func(ctx context.Context, params *bot.SendAudioParams) (*models.Message, error) {
		return nil, errors.New("audio upload failed")
	}
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Audio",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
		Attachments: []nexusmodels.Attachment{
			{Type: "audio", URL: "https://example.com/audio.mp3"},
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Message send should succeed, attachment failure is logged but not fatal
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

// =============================================================================
// Mock Implementation
// =============================================================================

// mockTelegramMessage simulates a Telegram message for testing
type mockTelegramMessage struct {
	messageID       int64
	chatID          int64
	chatType        string
	text            string
	fromID          int64
	fromFirst       string
	fromLast        string
	date            int64
	messageThreadID int

	// Attachments
	hasPhoto bool
	photoID  string

	hasDoc  bool
	docID   string
	docName string
	docMime string

	hasAudio bool
	audioID  string

	hasVoice      bool
	voiceID       string
	voiceDuration int
	voiceMimeType string
}

func (m *mockTelegramMessage) GetMessageID() int64 {
	return m.messageID
}

func (m *mockTelegramMessage) GetChatID() int64 {
	return m.chatID
}

func (m *mockTelegramMessage) GetChatType() string {
	return m.chatType
}

func (m *mockTelegramMessage) GetMessageThreadID() int {
	return m.messageThreadID
}

func (m *mockTelegramMessage) GetText() string {
	return m.text
}

func (m *mockTelegramMessage) GetFrom() userInterface {
	return &mockUser{
		id:        m.fromID,
		firstName: m.fromFirst,
		lastName:  m.fromLast,
	}
}

func (m *mockTelegramMessage) GetDate() int64 {
	return m.date
}

func (m *mockTelegramMessage) HasPhoto() bool {
	return m.hasPhoto
}

func (m *mockTelegramMessage) GetPhotoID() string {
	return m.photoID
}

func (m *mockTelegramMessage) HasDocument() bool {
	return m.hasDoc
}

func (m *mockTelegramMessage) GetDocumentID() string {
	return m.docID
}

func (m *mockTelegramMessage) GetDocumentName() string {
	return m.docName
}

func (m *mockTelegramMessage) GetDocumentMimeType() string {
	return m.docMime
}

func (m *mockTelegramMessage) HasAudio() bool {
	return m.hasAudio
}

func (m *mockTelegramMessage) GetAudioID() string {
	return m.audioID
}

func (m *mockTelegramMessage) HasVoice() bool {
	return m.hasVoice
}

func (m *mockTelegramMessage) GetVoiceID() string {
	return m.voiceID
}

func (m *mockTelegramMessage) GetVoiceDuration() int {
	return m.voiceDuration
}

func (m *mockTelegramMessage) GetVoiceMimeType() string {
	if m.voiceMimeType != "" {
		return m.voiceMimeType
	}
	return "audio/ogg"
}

type mockUser struct {
	id        int64
	firstName string
	lastName  string
}

func (u *mockUser) GetID() int64 {
	return u.id
}

func (u *mockUser) GetFirstName() string {
	return u.firstName
}

func (u *mockUser) GetLastName() string {
	return u.lastName
}

// =============================================================================
// Real Telegram Message Adapter Tests
// =============================================================================

func TestTelegramMessageAdapter_WithRealModels(t *testing.T) {
	// Test with a real models.Message
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From: &models.User{
			ID:        111,
			FirstName: "John",
			LastName:  "Doe",
		},
		Date: 1234567890,
		Text: "Hello from Telegram!",
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if adapter.GetMessageID() != 12345 {
		t.Errorf("GetMessageID() = %d, want 12345", adapter.GetMessageID())
	}

	if adapter.GetChatID() != 67890 {
		t.Errorf("GetChatID() = %d, want 67890", adapter.GetChatID())
	}

	if adapter.GetText() != "Hello from Telegram!" {
		t.Errorf("GetText() = %q, want %q", adapter.GetText(), "Hello from Telegram!")
	}

	if adapter.GetDate() != 1234567890 {
		t.Errorf("GetDate() = %d, want 1234567890", adapter.GetDate())
	}

	user := adapter.GetFrom()
	if user.GetID() != 111 {
		t.Errorf("GetFrom().GetID() = %d, want 111", user.GetID())
	}

	if user.GetFirstName() != "John" {
		t.Errorf("GetFrom().GetFirstName() = %q, want %q", user.GetFirstName(), "John")
	}

	if user.GetLastName() != "Doe" {
		t.Errorf("GetFrom().GetLastName() = %q, want %q", user.GetLastName(), "Doe")
	}
}

func TestTelegramMessageAdapter_NilFrom(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From: nil, // No user
		Date: 1234567890,
		Text: "Anonymous message",
	}

	adapter := &telegramMessageAdapter{telegramMsg}
	user := adapter.GetFrom()

	// Should return empty userAdapter with default values
	if user.GetID() != 0 {
		t.Errorf("Expected GetID() = 0 for nil user, got %d", user.GetID())
	}
	if user.GetFirstName() != "" {
		t.Errorf("Expected GetFirstName() = \"\" for nil user, got %q", user.GetFirstName())
	}
	if user.GetLastName() != "" {
		t.Errorf("Expected GetLastName() = \"\" for nil user, got %q", user.GetLastName())
	}
}

func TestTelegramMessageAdapter_WithPhoto(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From: &models.User{ID: 111, FirstName: "John"},
		Date: 1234567890,
		Text: "Check this photo",
		Photo: []models.PhotoSize{
			{FileID: "photo_file_123", Width: 100, Height: 100},
		},
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if !adapter.HasPhoto() {
		t.Error("Expected HasPhoto() = true")
	}

	if adapter.GetPhotoID() != "photo_file_123" {
		t.Errorf("GetPhotoID() = %q, want %q", adapter.GetPhotoID(), "photo_file_123")
	}
}

func TestTelegramMessageAdapter_NoPhoto(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From:  &models.User{ID: 111, FirstName: "John"},
		Date:  1234567890,
		Text:  "No photo",
		Photo: []models.PhotoSize{}, // Empty photo array
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if adapter.HasPhoto() {
		t.Error("Expected HasPhoto() = false for empty photo array")
	}

	if adapter.GetPhotoID() != "" {
		t.Errorf("Expected GetPhotoID() = \"\" for no photo, got %q", adapter.GetPhotoID())
	}
}

func TestTelegramMessageAdapter_WithDocument(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From: &models.User{ID: 111, FirstName: "John"},
		Date: 1234567890,
		Text: "Here's a document",
		Document: &models.Document{
			FileID:   "doc_file_123",
			FileName: "report.pdf",
			MimeType: "application/pdf",
		},
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if !adapter.HasDocument() {
		t.Error("Expected HasDocument() = true")
	}

	if adapter.GetDocumentID() != "doc_file_123" {
		t.Errorf("GetDocumentID() = %q, want %q", adapter.GetDocumentID(), "doc_file_123")
	}

	if adapter.GetDocumentName() != "report.pdf" {
		t.Errorf("GetDocumentName() = %q, want %q", adapter.GetDocumentName(), "report.pdf")
	}

	if adapter.GetDocumentMimeType() != "application/pdf" {
		t.Errorf("GetDocumentMimeType() = %q, want %q", adapter.GetDocumentMimeType(), "application/pdf")
	}
}

func TestTelegramMessageAdapter_NoDocument(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From:     &models.User{ID: 111, FirstName: "John"},
		Date:     1234567890,
		Text:     "No document",
		Document: nil,
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if adapter.HasDocument() {
		t.Error("Expected HasDocument() = false")
	}

	if adapter.GetDocumentID() != "" {
		t.Errorf("Expected GetDocumentID() = \"\" for no document, got %q", adapter.GetDocumentID())
	}

	if adapter.GetDocumentName() != "" {
		t.Errorf("Expected GetDocumentName() = \"\" for no document, got %q", adapter.GetDocumentName())
	}

	if adapter.GetDocumentMimeType() != "" {
		t.Errorf("Expected GetDocumentMimeType() = \"\" for no document, got %q", adapter.GetDocumentMimeType())
	}
}

func TestTelegramMessageAdapter_WithAudio(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From: &models.User{ID: 111, FirstName: "John"},
		Date: 1234567890,
		Text: "Audio file",
		Audio: &models.Audio{
			FileID: "audio_file_123",
		},
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if !adapter.HasAudio() {
		t.Error("Expected HasAudio() = true")
	}

	if adapter.GetAudioID() != "audio_file_123" {
		t.Errorf("GetAudioID() = %q, want %q", adapter.GetAudioID(), "audio_file_123")
	}
}

func TestTelegramMessageAdapter_NoAudio(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From:  &models.User{ID: 111, FirstName: "John"},
		Date:  1234567890,
		Text:  "No audio",
		Audio: nil,
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if adapter.HasAudio() {
		t.Error("Expected HasAudio() = false")
	}

	if adapter.GetAudioID() != "" {
		t.Errorf("Expected GetAudioID() = \"\" for no audio, got %q", adapter.GetAudioID())
	}
}

func TestTelegramMessageAdapter_WithVoice(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From: &models.User{ID: 111, FirstName: "John"},
		Date: 1234567890,
		Voice: &models.Voice{
			FileID:   "voice_file_123",
			Duration: 30,
			MimeType: "audio/ogg",
		},
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if !adapter.HasVoice() {
		t.Error("Expected HasVoice() = true")
	}

	if adapter.GetVoiceID() != "voice_file_123" {
		t.Errorf("GetVoiceID() = %q, want %q", adapter.GetVoiceID(), "voice_file_123")
	}

	if adapter.GetVoiceDuration() != 30 {
		t.Errorf("GetVoiceDuration() = %d, want 30", adapter.GetVoiceDuration())
	}

	if adapter.GetVoiceMimeType() != "audio/ogg" {
		t.Errorf("GetVoiceMimeType() = %q, want %q", adapter.GetVoiceMimeType(), "audio/ogg")
	}
}

func TestTelegramMessageAdapter_NoVoice(t *testing.T) {
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From:  &models.User{ID: 111, FirstName: "John"},
		Date:  1234567890,
		Text:  "No voice",
		Voice: nil,
	}

	adapter := &telegramMessageAdapter{telegramMsg}

	if adapter.HasVoice() {
		t.Error("Expected HasVoice() = false")
	}

	if adapter.GetVoiceID() != "" {
		t.Errorf("Expected GetVoiceID() = \"\" for no voice, got %q", adapter.GetVoiceID())
	}

	if adapter.GetVoiceDuration() != 0 {
		t.Errorf("Expected GetVoiceDuration() = 0 for no voice, got %d", adapter.GetVoiceDuration())
	}

	// Default mime type should still be audio/ogg for voice messages
	if adapter.GetVoiceMimeType() != "audio/ogg" {
		t.Errorf("Expected default GetVoiceMimeType() = \"audio/ogg\", got %q", adapter.GetVoiceMimeType())
	}
}

func TestConvertTelegramMessage_WithRealAdapter(t *testing.T) {
	// Test the actual conversion using telegramMessageAdapter
	telegramMsg := &models.Message{
		ID: 12345,
		Chat: models.Chat{
			ID:   67890,
			Type: "private",
		},
		From: &models.User{
			ID:        111,
			FirstName: "John",
			LastName:  "Doe",
		},
		Date: 1234567890,
		Text: "Hello!",
		Photo: []models.PhotoSize{
			{FileID: "photo123", Width: 100, Height: 100},
		},
		Document: &models.Document{
			FileID:   "doc123",
			FileName: "file.txt",
			MimeType: "text/plain",
		},
	}

	adapter := &telegramMessageAdapter{telegramMsg}
	msg := convertTelegramMessage(adapter)

	// Verify the conversion
	if msg.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", msg.Content, "Hello!")
	}

	if msg.Channel != nexusmodels.ChannelTelegram {
		t.Errorf("Channel = %v, want %v", msg.Channel, nexusmodels.ChannelTelegram)
	}

	if msg.SessionID != "telegram:67890" {
		t.Errorf("SessionID = %q, want %q", msg.SessionID, "telegram:67890")
	}

	if len(msg.Attachments) != 2 {
		t.Fatalf("Expected 2 attachments, got %d", len(msg.Attachments))
	}
}

// =============================================================================
// User Adapter Tests
// =============================================================================

func TestUserAdapter_NilUser(t *testing.T) {
	adapter := &userAdapter{nil}

	if adapter.GetID() != 0 {
		t.Errorf("GetID() = %d, want 0 for nil user", adapter.GetID())
	}

	if adapter.GetFirstName() != "" {
		t.Errorf("GetFirstName() = %q, want \"\" for nil user", adapter.GetFirstName())
	}

	if adapter.GetLastName() != "" {
		t.Errorf("GetLastName() = %q, want \"\" for nil user", adapter.GetLastName())
	}
}

func TestUserAdapter_WithUser(t *testing.T) {
	user := &models.User{
		ID:        123,
		FirstName: "Alice",
		LastName:  "Smith",
	}
	adapter := &userAdapter{user}

	if adapter.GetID() != 123 {
		t.Errorf("GetID() = %d, want 123", adapter.GetID())
	}

	if adapter.GetFirstName() != "Alice" {
		t.Errorf("GetFirstName() = %q, want \"Alice\"", adapter.GetFirstName())
	}

	if adapter.GetLastName() != "Smith" {
		t.Errorf("GetLastName() = %q, want \"Smith\"", adapter.GetLastName())
	}
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestAdapter_SendUnknownAttachmentType(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	msg := &nexusmodels.Message{
		Content: "Unknown attachment",
		Metadata: map[string]any{
			"chat_id": int64(123456),
		},
		Attachments: []nexusmodels.Attachment{
			{Type: "unknown_type", URL: "https://example.com/file"},
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Unknown attachment type should not cause an error (just logged)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// Only message should be sent, not the unknown attachment
	if mock.getSendMessageCalls() != 1 {
		t.Errorf("SendMessage called %d times, want 1", mock.getSendMessageCalls())
	}
	if mock.getSendPhotoCalls() != 0 {
		t.Errorf("SendPhoto called %d times, want 0", mock.getSendPhotoCalls())
	}
}

func TestAdapter_ConcurrentHealthCheck(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	const numGoroutines = 10
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			health := adapter.HealthCheck(ctx)
			if !health.Healthy {
				t.Errorf("Expected healthy status")
			}
		}()
	}

	wg.Wait()
}

func TestAdapter_MetricsConcurrent(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	mock := newMockBotClient()
	adapter.SetBotClient(mock)

	const numGoroutines = 10
	var wg sync.WaitGroup

	// Concurrent sends and metrics reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			msg := &nexusmodels.Message{
				Content: fmt.Sprintf("Message %d", i),
				Metadata: map[string]any{
					"chat_id": int64(123456),
				},
			}
			_ = adapter.Send(context.Background(), msg)
		}(i)
		go func() {
			defer wg.Done()
			_ = adapter.Metrics()
		}()
	}

	wg.Wait()
}
