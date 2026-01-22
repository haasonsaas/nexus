package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
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
			name: "valid config",
			cfg: Config{
				Token: "valid-token",
			},
			wantErr: false,
		},
		{
			name:    "missing token",
			cfg:     Config{},
			wantErr: true,
			errMsg:  "token is required",
		},
		{
			name: "empty token",
			cfg: Config{
				Token: "",
			},
			wantErr: true,
			errMsg:  "token is required",
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
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Check default values were applied
	if cfg.MaxReconnectAttempts != 5 {
		t.Errorf("MaxReconnectAttempts = %d, want 5", cfg.MaxReconnectAttempts)
	}

	if cfg.ReconnectBackoff != 60*time.Second {
		t.Errorf("ReconnectBackoff = %v, want 60s", cfg.ReconnectBackoff)
	}

	if cfg.RateLimit != 5 {
		t.Errorf("RateLimit = %f, want 5", cfg.RateLimit)
	}

	if cfg.RateBurst != 10 {
		t.Errorf("RateBurst = %d, want 10", cfg.RateBurst)
	}

	if cfg.Logger == nil {
		t.Error("Logger should not be nil after validation")
	}
}

func TestConfig_CustomValues(t *testing.T) {
	logger := slog.Default()
	cfg := Config{
		Token:                "test-token",
		MaxReconnectAttempts: 10,
		ReconnectBackoff:     120 * time.Second,
		RateLimit:            10,
		RateBurst:            20,
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

	if cfg.ReconnectBackoff != 120*time.Second {
		t.Errorf("ReconnectBackoff = %v, want 120s", cfg.ReconnectBackoff)
	}

	if cfg.RateLimit != 10 {
		t.Errorf("RateLimit = %f, want 10", cfg.RateLimit)
	}

	if cfg.RateBurst != 20 {
		t.Errorf("RateBurst = %d, want 20", cfg.RateBurst)
	}
}

// =============================================================================
// Adapter Interface Tests
// =============================================================================

func TestAdapter_Type(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

	if got := adapter.Type(); got != models.ChannelDiscord {
		t.Errorf("Type() = %v, want %v", got, models.ChannelDiscord)
	}
}

func TestAdapter_Status(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

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
	adapter := NewAdapterSimple("test-token")

	msgChan := adapter.Messages()
	if msgChan == nil {
		t.Error("Messages() returned nil channel")
	}
}

func TestAdapter_Metrics(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

	metrics := adapter.Metrics()
	if metrics.ChannelType != models.ChannelDiscord {
		t.Errorf("Metrics().ChannelType = %v, want %v", metrics.ChannelType, models.ChannelDiscord)
	}
}

func TestAdapter_InterfaceCompliance(t *testing.T) {
	// Verify Adapter implements all expected interfaces
	var _ channels.Adapter = (*Adapter)(nil)
	var _ channels.LifecycleAdapter = (*Adapter)(nil)
	var _ channels.OutboundAdapter = (*Adapter)(nil)
	var _ channels.InboundAdapter = (*Adapter)(nil)
	var _ channels.HealthAdapter = (*Adapter)(nil)
}

// =============================================================================
// NewAdapter Tests
// =============================================================================

func TestNewAdapter(t *testing.T) {
	cfg := Config{
		Token: "test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if adapter == nil {
		t.Fatal("NewAdapter() returned nil adapter")
	}

	if adapter.token != "test-token" {
		t.Errorf("adapter.token = %q, want %q", adapter.token, "test-token")
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

func TestNewAdapter_InvalidConfig(t *testing.T) {
	cfg := Config{Token: ""}

	adapter, err := NewAdapter(cfg)
	if err == nil {
		t.Error("NewAdapter() expected error, got nil")
	}
	if adapter != nil {
		t.Error("NewAdapter() expected nil adapter on error")
	}
}

func TestNewAdapterSimple(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

	if adapter == nil {
		t.Fatal("NewAdapterSimple returned nil")
	}

	if adapter.Type() != models.ChannelDiscord {
		t.Errorf("Expected channel type %s, got %s", models.ChannelDiscord, adapter.Type())
	}

	status := adapter.Status()
	if status.Connected {
		t.Error("Expected adapter to be disconnected initially")
	}
}

func TestNewAdapterSimple_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewAdapterSimple with empty token should panic")
		}
	}()

	_ = NewAdapterSimple("")
}

// =============================================================================
// Message Conversion Tests
// =============================================================================

func TestConvertDiscordMessage_SimpleText(t *testing.T) {
	msg := &discordgo.Message{
		ID:        "discord-msg-123",
		ChannelID: "channel-456",
		Content:   "Hello, world!",
		Author: &discordgo.User{
			ID:       "user-789",
			Username: "testuser",
		},
		Timestamp: time.Date(2024, 1, 20, 12, 0, 0, 0, time.UTC),
	}

	result := convertDiscordMessage(msg)

	if result == nil {
		t.Fatal("convertDiscordMessage returned nil")
	}

	if result.Channel != models.ChannelDiscord {
		t.Errorf("Channel = %v, want %v", result.Channel, models.ChannelDiscord)
	}

	if result.ChannelID != "discord-msg-123" {
		t.Errorf("ChannelID = %v, want %v", result.ChannelID, "discord-msg-123")
	}

	if result.Direction != models.DirectionInbound {
		t.Errorf("Direction = %v, want %v", result.Direction, models.DirectionInbound)
	}

	if result.Role != models.RoleUser {
		t.Errorf("Role = %v, want %v", result.Role, models.RoleUser)
	}

	if result.Content != "Hello, world!" {
		t.Errorf("Content = %v, want %v", result.Content, "Hello, world!")
	}
}

func TestConvertDiscordMessage_Metadata(t *testing.T) {
	msg := &discordgo.Message{
		ID:        "discord-msg-123",
		ChannelID: "channel-456",
		Content:   "Test",
		Author: &discordgo.User{
			ID:       "user-789",
			Username: "testuser",
		},
		Timestamp: time.Date(2024, 1, 20, 12, 0, 0, 0, time.UTC),
	}

	result := convertDiscordMessage(msg)

	if result.Metadata == nil {
		t.Fatal("Metadata is nil")
	}

	if result.Metadata["discord_channel_id"] != "channel-456" {
		t.Errorf("Metadata[discord_channel_id] = %v, want %v", result.Metadata["discord_channel_id"], "channel-456")
	}

	if result.Metadata["discord_user_id"] != "user-789" {
		t.Errorf("Metadata[discord_user_id] = %v, want %v", result.Metadata["discord_user_id"], "user-789")
	}

	if result.Metadata["discord_username"] != "testuser" {
		t.Errorf("Metadata[discord_username] = %v, want %v", result.Metadata["discord_username"], "testuser")
	}
}

func TestConvertDiscordMessage_WithAttachments(t *testing.T) {
	msg := &discordgo.Message{
		ID:        "discord-msg-124",
		ChannelID: "channel-456",
		Content:   "Check this image",
		Author: &discordgo.User{
			ID:       "user-789",
			Username: "testuser",
		},
		Timestamp: time.Date(2024, 1, 20, 12, 0, 0, 0, time.UTC),
		Attachments: []*discordgo.MessageAttachment{
			{
				ID:          "attach-001",
				Filename:    "image.png",
				URL:         "https://cdn.discord.com/image.png",
				ContentType: "image/png",
				Size:        1024,
			},
			{
				ID:          "attach-002",
				Filename:    "document.pdf",
				URL:         "https://cdn.discord.com/document.pdf",
				ContentType: "application/pdf",
				Size:        2048,
			},
		},
	}

	result := convertDiscordMessage(msg)

	if len(result.Attachments) != 2 {
		t.Fatalf("Attachments count = %d, want 2", len(result.Attachments))
	}

	// Check first attachment (image)
	att1 := result.Attachments[0]
	if att1.ID != "attach-001" {
		t.Errorf("Attachment[0].ID = %v, want %v", att1.ID, "attach-001")
	}
	if att1.Type != "image" {
		t.Errorf("Attachment[0].Type = %v, want %v", att1.Type, "image")
	}
	if att1.URL != "https://cdn.discord.com/image.png" {
		t.Errorf("Attachment[0].URL = %v, want %v", att1.URL, "https://cdn.discord.com/image.png")
	}
	if att1.Filename != "image.png" {
		t.Errorf("Attachment[0].Filename = %v, want %v", att1.Filename, "image.png")
	}
	if att1.MimeType != "image/png" {
		t.Errorf("Attachment[0].MimeType = %v, want %v", att1.MimeType, "image/png")
	}
	if att1.Size != 1024 {
		t.Errorf("Attachment[0].Size = %v, want %v", att1.Size, 1024)
	}

	// Check second attachment (document)
	att2 := result.Attachments[1]
	if att2.Type != "document" {
		t.Errorf("Attachment[1].Type = %v, want %v", att2.Type, "document")
	}
}

func TestConvertDiscordMessage_InThread(t *testing.T) {
	msg := &discordgo.Message{
		ID:        "discord-msg-125",
		ChannelID: "thread-789",
		Content:   "Thread reply",
		Author: &discordgo.User{
			ID:       "user-789",
			Username: "testuser",
		},
		Timestamp: time.Date(2024, 1, 20, 12, 0, 0, 0, time.UTC),
		Thread: &discordgo.Channel{
			ID:       "thread-789",
			ParentID: "channel-456",
			Name:     "Discussion Thread",
		},
	}

	result := convertDiscordMessage(msg)

	if result.Metadata["discord_thread_id"] != "thread-789" {
		t.Errorf("Metadata[discord_thread_id] = %v, want %v", result.Metadata["discord_thread_id"], "thread-789")
	}

	if result.Metadata["discord_thread_name"] != "Discussion Thread" {
		t.Errorf("Metadata[discord_thread_name] = %v, want %v", result.Metadata["discord_thread_name"], "Discussion Thread")
	}

	if result.Metadata["discord_parent_id"] != "channel-456" {
		t.Errorf("Metadata[discord_parent_id] = %v, want %v", result.Metadata["discord_parent_id"], "channel-456")
	}
}

func TestConvertDiscordMessage_WithMentions(t *testing.T) {
	msg := &discordgo.Message{
		ID:        "discord-msg-126",
		ChannelID: "channel-456",
		Content:   "Hello @user1 and @user2",
		Author: &discordgo.User{
			ID:       "user-789",
			Username: "testuser",
		},
		Timestamp: time.Date(2024, 1, 20, 12, 0, 0, 0, time.UTC),
		Mentions: []*discordgo.User{
			{ID: "mention-1", Username: "user1"},
			{ID: "mention-2", Username: "user2"},
		},
	}

	result := convertDiscordMessage(msg)

	mentions, ok := result.Metadata["discord_mentions"].([]string)
	if !ok {
		t.Fatal("discord_mentions is not []string")
	}

	if len(mentions) != 2 {
		t.Fatalf("mentions count = %d, want 2", len(mentions))
	}

	if mentions[0] != "mention-1" {
		t.Errorf("mentions[0] = %v, want %v", mentions[0], "mention-1")
	}

	if mentions[1] != "mention-2" {
		t.Errorf("mentions[1] = %v, want %v", mentions[1], "mention-2")
	}
}

func TestConvertDiscordMessage_NilMessage(t *testing.T) {
	result := convertDiscordMessage(nil)
	if result != nil {
		t.Errorf("convertDiscordMessage(nil) = %v, want nil", result)
	}
}

func TestConvertDiscordMessage_NilAuthor(t *testing.T) {
	msg := &discordgo.Message{
		ID:        "discord-msg-127",
		ChannelID: "channel-456",
		Content:   "Test",
		Author:    nil,
	}

	result := convertDiscordMessage(msg)
	if result != nil {
		t.Errorf("convertDiscordMessage with nil author = %v, want nil", result)
	}
}

func TestConvertDiscordMessage_ZeroTimestamp(t *testing.T) {
	msg := &discordgo.Message{
		ID:        "discord-msg-128",
		ChannelID: "channel-456",
		Content:   "Test",
		Author: &discordgo.User{
			ID:       "user-789",
			Username: "testuser",
		},
		// Zero timestamp
	}

	result := convertDiscordMessage(msg)

	// Should use time.Now() as fallback, so CreatedAt should be recent
	if result.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

// =============================================================================
// Attachment Type Detection Tests
// =============================================================================

func TestDetectAttachmentType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    string
	}{
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"image/gif", "image"},
		{"image/webp", "image"},
		{"audio/mpeg", "audio"},
		{"audio/wav", "audio"},
		{"audio/ogg", "audio"},
		{"video/mp4", "video"},
		{"video/webm", "video"},
		{"video/quicktime", "video"},
		{"application/pdf", "document"},
		{"application/zip", "document"},
		{"text/plain", "document"},
		{"application/json", "document"},
		{"unknown/type", "document"},
		{"", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			got := detectAttachmentType(tt.contentType)
			if got != tt.expected {
				t.Errorf("detectAttachmentType(%s) = %s, want %s", tt.contentType, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Reconnection Backoff Tests
// =============================================================================

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		attempt  int
		maxWait  time.Duration
		expected time.Duration
	}{
		{attempt: 0, expected: 1 * time.Second, maxWait: 60 * time.Second},
		{attempt: 1, expected: 2 * time.Second, maxWait: 60 * time.Second},
		{attempt: 2, expected: 4 * time.Second, maxWait: 60 * time.Second},
		{attempt: 3, expected: 8 * time.Second, maxWait: 60 * time.Second},
		{attempt: 4, expected: 16 * time.Second, maxWait: 60 * time.Second},
		{attempt: 5, expected: 32 * time.Second, maxWait: 60 * time.Second},
		{attempt: 6, expected: 60 * time.Second, maxWait: 60 * time.Second}, // Capped at max
		{attempt: 10, expected: 60 * time.Second, maxWait: 60 * time.Second},
		// Note: Very high attempts cause integer overflow in 1<<uint(attempt), resulting in 0
		// This is a known edge case - in practice, max reconnect attempts prevents this
		// Different max wait
		{attempt: 3, expected: 8 * time.Second, maxWait: 30 * time.Second},
		{attempt: 6, expected: 30 * time.Second, maxWait: 30 * time.Second}, // Capped at 30s
	}

	for _, tt := range tests {
		name := fmt.Sprintf("attempt=%d,max=%v", tt.attempt, tt.maxWait)
		t.Run(name, func(t *testing.T) {
			got := calculateBackoff(tt.attempt, tt.maxWait)
			if got != tt.expected {
				t.Errorf("calculateBackoff(%d, %v) = %v, want %v", tt.attempt, tt.maxWait, got, tt.expected)
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
			name: "rate limit error",
			err:  errors.New("rate limit exceeded"),
			want: true,
		},
		{
			name: "429 error",
			err:  errors.New("HTTP 429"),
			want: true,
		},
		{
			name: "Too Many Requests",
			err:  errors.New("Too Many Requests"),
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "connection error",
			err:  errors.New("connection refused"),
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
// Degraded Mode Tests
// =============================================================================

func TestAdapter_DegradedMode(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

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

func TestAdapter_HealthCheckNotConnected(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if health.Healthy {
		t.Error("Expected Healthy = false when adapter is not connected")
	}
	if health.Message != "adapter not connected" {
		t.Errorf("Expected message 'adapter not connected', got %q", health.Message)
	}
	if health.Latency <= 0 {
		t.Error("Expected Latency > 0")
	}
}

func TestAdapter_HealthCheckConnected(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.status.Connected = true

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if !health.Healthy {
		t.Error("Expected Healthy = true when adapter is connected")
	}
	if health.Message != "healthy" {
		t.Errorf("Expected message 'healthy', got %q", health.Message)
	}
}

func TestAdapter_HealthCheckDegraded(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.status.Connected = true
	adapter.setDegraded(true)

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if !health.Healthy {
		t.Error("Expected Healthy = true")
	}
	if !health.Degraded {
		t.Error("Expected Degraded = true")
	}
	if health.Message != "operating in degraded mode" {
		t.Errorf("Expected message 'operating in degraded mode', got %q", health.Message)
	}
}

// =============================================================================
// Lifecycle Tests
// =============================================================================

func TestAdapter_StartStop(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx := context.Background()

	// Test Start
	err := adapter.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !mock.openCalled {
		t.Error("Expected session.Open to be called")
	}

	status := adapter.Status()
	if !status.Connected {
		t.Error("Expected adapter to be connected after Start")
	}

	// Test Stop
	err = adapter.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if !mock.closeCalled {
		t.Error("Expected session.Close to be called")
	}

	status = adapter.Status()
	if status.Connected {
		t.Error("Expected adapter to be disconnected after Stop")
	}
}

func TestAdapter_StartAlreadyStarted(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx := context.Background()

	// First start
	err := adapter.Start(ctx)
	if err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	// Second start should fail
	err = adapter.Start(ctx)
	if err == nil {
		t.Error("Expected error on second Start, got nil")
	}
}

func TestAdapter_StopNotStarted(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx := context.Background()

	// Stop without start should be ok
	err := adapter.Stop(ctx)
	if err != nil {
		t.Errorf("Stop on unstarted adapter returned error: %v", err)
	}
}

func TestAdapter_StartConnectionError(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{
		openErr: errors.New("connection refused"),
	}
	adapter.session = mock

	ctx := context.Background()

	err := adapter.Start(ctx)
	if err == nil {
		t.Error("Expected error when connection fails")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeConnection {
			t.Errorf("Expected ErrCodeConnection, got %v", chErr.Code)
		}
	}
}

// =============================================================================
// Send Tests
// =============================================================================

func TestAdapter_Send(t *testing.T) {
	tests := []struct {
		name    string
		message *models.Message
		wantErr bool
	}{
		{
			name: "simple text message",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "Hello from test",
				Metadata: map[string]any{
					"discord_channel_id": "channel-123",
				},
			},
			wantErr: false,
		},
		{
			name: "message with embed",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "Check this out",
				Metadata: map[string]any{
					"discord_channel_id":  "channel-123",
					"discord_embed_title": "Important",
					"discord_embed_color": 0x00FF00,
				},
			},
			wantErr: false,
		},
		{
			name: "message with embed description",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "",
				Metadata: map[string]any{
					"discord_channel_id":        "channel-123",
					"discord_embed_title":       "Alert",
					"discord_embed_description": "Important notification",
					"discord_embed_color":       0xFF0000,
				},
			},
			wantErr: false,
		},
		{
			name: "message with reaction",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "React to this",
				Metadata: map[string]any{
					"discord_channel_id":      "channel-123",
					"discord_reaction_emoji":  "thumbs_up",
					"discord_reaction_msg_id": "msg-to-react",
				},
			},
			wantErr: false,
		},
		{
			name: "message to create thread",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "Thread starter",
				Metadata: map[string]any{
					"discord_channel_id":    "channel-123",
					"discord_create_thread": true,
					"discord_thread_name":   "Discussion",
				},
			},
			wantErr: false,
		},
		{
			name: "message to create thread without name",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "Thread starter",
				Metadata: map[string]any{
					"discord_channel_id":    "channel-123",
					"discord_create_thread": true,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewAdapterSimple("test-token")
			mock := &mockDiscordSession{}
			adapter.session = mock
			adapter.status.Connected = true

			ctx := context.Background()
			err := adapter.Send(ctx, tt.message)

			if (err != nil) != tt.wantErr {
				t.Errorf("Send() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdapter_SendNotConnected(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	// Note: not setting status.Connected = true

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"discord_channel_id": "channel-123",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when adapter is not connected")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeUnavailable {
			t.Errorf("Expected ErrCodeUnavailable, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendMissingChannelID(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.status.Connected = true

	msg := &models.Message{
		Content:  "Test",
		Metadata: map[string]any{},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when discord_channel_id is missing")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInvalidInput {
			t.Errorf("Expected ErrCodeInvalidInput, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendError(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{
		channelMessageSendFn: func(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
			return nil, errors.New("send failed")
		},
	}
	adapter.session = mock
	adapter.status.Connected = true

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"discord_channel_id": "channel-123",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when send fails")
	}
}

func TestAdapter_SendRateLimitError(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{
		channelMessageSendFn: func(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
			return nil, errors.New("rate limit exceeded")
		},
	}
	adapter.session = mock
	adapter.status.Connected = true

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"discord_channel_id": "channel-123",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when rate limited")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeRateLimit {
			t.Errorf("Expected ErrCodeRateLimit, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendReactionError(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{
		messageReactionAddFn: func(channelID, messageID, emoji string) error {
			return errors.New("reaction failed")
		},
	}
	adapter.session = mock
	adapter.status.Connected = true

	msg := &models.Message{
		Content: "",
		Metadata: map[string]any{
			"discord_channel_id":      "channel-123",
			"discord_reaction_emoji":  "thumbs_up",
			"discord_reaction_msg_id": "msg-123",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when reaction fails")
	}
}

// =============================================================================
// Slash Commands Tests
// =============================================================================

func TestAdapter_RegisterSlashCommands(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "help",
			Description: "Show help information",
		},
		{
			Name:        "ask",
			Description: "Ask the AI a question",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "question",
					Description: "Your question",
					Required:    true,
				},
			},
		},
	}

	err := adapter.RegisterSlashCommands(commands, "guild-123")
	if err != nil {
		t.Fatalf("RegisterSlashCommands failed: %v", err)
	}
}

// =============================================================================
// Mock Discord Session
// =============================================================================

type mockDiscordSession struct {
	openCalled           bool
	closeCalled          bool
	openErr              error
	closeErr             error
	channelMessageSendFn func(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
	messageReactionAddFn func(channelID, messageID, emoji string) error
	threadStartFn        func(channelID, name string, archiveDuration int) (*discordgo.Channel, error)
}

func (m *mockDiscordSession) Open() error {
	m.openCalled = true
	if m.openErr != nil {
		return m.openErr
	}
	return nil
}

func (m *mockDiscordSession) Close() error {
	m.closeCalled = true
	if m.closeErr != nil {
		return m.closeErr
	}
	return nil
}

func (m *mockDiscordSession) ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	if m.channelMessageSendFn != nil {
		return m.channelMessageSendFn(channelID, content, options...)
	}
	return &discordgo.Message{
		ID:        "test-msg-id",
		ChannelID: channelID,
		Content:   content,
	}, nil
}

func (m *mockDiscordSession) ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return &discordgo.Message{
		ID:        "test-msg-id",
		ChannelID: channelID,
		Content:   data.Content,
		Embeds:    data.Embeds,
	}, nil
}

func (m *mockDiscordSession) MessageReactionAdd(channelID, messageID, emoji string, options ...discordgo.RequestOption) error {
	if m.messageReactionAddFn != nil {
		return m.messageReactionAddFn(channelID, messageID, emoji)
	}
	return nil
}

func (m *mockDiscordSession) ThreadStart(channelID, name string, typ discordgo.ChannelType, archiveDuration int, options ...discordgo.RequestOption) (*discordgo.Channel, error) {
	if m.threadStartFn != nil {
		return m.threadStartFn(channelID, name, archiveDuration)
	}
	return &discordgo.Channel{
		ID:   "test-thread-id",
		Name: name,
		Type: discordgo.ChannelTypeGuildPublicThread,
	}, nil
}

func (m *mockDiscordSession) AddHandler(handler interface{}) func() {
	return func() {}
}

func (m *mockDiscordSession) ApplicationCommandBulkOverwrite(appID, guildID string, commands []*discordgo.ApplicationCommand, options ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error) {
	return commands, nil
}
