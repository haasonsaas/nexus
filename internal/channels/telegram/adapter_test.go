package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

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

	if got := adapter.Type(); got != models.ChannelTelegram {
		t.Errorf("Type() = %v, want %v", got, models.ChannelTelegram)
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
	if metrics.ChannelType != models.ChannelTelegram {
		t.Errorf("Metrics().ChannelType = %v, want %v", metrics.ChannelType, models.ChannelTelegram)
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

	if adapter.metrics == nil {
		t.Error("adapter.metrics is nil")
	}

	if adapter.logger == nil {
		t.Error("adapter.logger is nil")
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
		wantRole models.Role
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
			wantRole: models.RoleUser,
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
			wantRole: models.RoleUser,
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
			wantRole: models.RoleUser,
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
			wantRole: models.RoleUser,
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

			if got.Channel != models.ChannelTelegram {
				t.Errorf("Channel = %v, want %v", got.Channel, models.ChannelTelegram)
			}

			if got.Direction != models.DirectionInbound {
				t.Errorf("Direction = %v, want %v", got.Direction, models.DirectionInbound)
			}
		})
	}
}

func TestConvertTelegramMessage_Metadata(t *testing.T) {
	timestamp := time.Now().Unix()
	teleMsg := &mockTelegramMessage{
		messageID: 123,
		chatID:    456789,
		text:      "Test message",
		fromID:    111,
		fromFirst: "John",
		fromLast:  "Doe",
		date:      timestamp,
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
		msg     *models.Message
		wantID  int64
		wantErr bool
	}{
		{
			name: "chat_id as int64 in metadata",
			msg: &models.Message{
				Metadata: map[string]any{
					"chat_id": int64(123456),
				},
			},
			wantID:  123456,
			wantErr: false,
		},
		{
			name: "chat_id as int in metadata",
			msg: &models.Message{
				Metadata: map[string]any{
					"chat_id": 123456,
				},
			},
			wantID:  123456,
			wantErr: false,
		},
		{
			name: "chat_id as string in metadata",
			msg: &models.Message{
				Metadata: map[string]any{
					"chat_id": "123456",
				},
			},
			wantID:  123456,
			wantErr: false,
		},
		{
			name: "chat_id from session ID",
			msg: &models.Message{
				SessionID: "telegram:789012",
				Metadata:  map[string]any{},
			},
			wantID:  789012,
			wantErr: false,
		},
		{
			name: "no chat_id available",
			msg: &models.Message{
				SessionID: "invalid-format",
				Metadata:  map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "nil metadata",
			msg: &models.Message{
				SessionID: "telegram:456789",
			},
			wantID:  456789,
			wantErr: false,
		},
		{
			name:    "empty message",
			msg:     &models.Message{},
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
// Health Check Tests
// =============================================================================

func TestAdapter_HealthCheckWithoutBot(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if health.Healthy {
		t.Error("Expected Healthy = false when bot is not initialized")
	}
	if health.Message != "bot not initialized" {
		t.Errorf("Expected message 'bot not initialized', got %q", health.Message)
	}
	// Latency can be very small (nanoseconds) which is still >= 0
	if health.Latency < 0 {
		t.Error("Expected Latency >= 0")
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
// Send Tests (without bot)
// =============================================================================

func TestAdapter_SendWithoutBot(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	msg := &models.Message{
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
	// Simulate bot being set (even though it's nil, the check happens after chat ID extraction)
	adapter.bot = nil

	msg := &models.Message{
		Content:  "Test message",
		Metadata: map[string]any{},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when chat_id is missing")
	}
}

// =============================================================================
// Download Attachment Tests
// =============================================================================

func TestAdapter_DownloadAttachmentWithoutBot(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	msg := &models.Message{}
	att := &models.Attachment{ID: "file123"}

	ctx := context.Background()
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, att)

	if err == nil {
		t.Error("Expected error when bot is not initialized")
	}
}

func TestAdapter_DownloadAttachmentNilAttachment(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	msg := &models.Message{}

	ctx := context.Background()
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, nil)

	if err == nil {
		t.Error("Expected error for nil attachment")
	}
}

func TestAdapter_DownloadAttachmentMissingFileID(t *testing.T) {
	cfg := Config{Token: "test-token", Mode: ModeLongPolling}
	adapter, _ := NewAdapter(cfg)

	msg := &models.Message{Metadata: map[string]any{}}
	att := &models.Attachment{ID: ""} // Empty ID

	ctx := context.Background()
	_, _, _, err := adapter.DownloadAttachment(ctx, msg, att)

	if err == nil {
		t.Error("Expected error for missing file ID")
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
// Mock Implementation
// =============================================================================

// mockTelegramMessage simulates a Telegram message for testing
type mockTelegramMessage struct {
	messageID int64
	chatID    int64
	text      string
	fromID    int64
	fromFirst string
	fromLast  string
	date      int64

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
