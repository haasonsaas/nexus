package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	var _ channels.StreamingAdapter = (*Adapter)(nil)
	var _ channels.MessageActionsAdapter = (*Adapter)(nil)
	var _ channels.EditableAdapter = (*Adapter)(nil)
	var _ channels.DeletableAdapter = (*Adapter)(nil)
	var _ channels.ReactableAdapter = (*Adapter)(nil)
	var _ channels.PinnableAdapter = (*Adapter)(nil)
}

func TestAdapter_Capabilities(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	caps := adapter.Capabilities()

	if !caps.Send {
		t.Error("Expected Send capability to be true")
	}
	if !caps.Edit {
		t.Error("Expected Edit capability to be true")
	}
	if !caps.Delete {
		t.Error("Expected Delete capability to be true")
	}
	if !caps.React {
		t.Error("Expected React capability to be true")
	}
	if !caps.Reply {
		t.Error("Expected Reply capability to be true")
	}
	if !caps.Pin {
		t.Error("Expected Pin capability to be true")
	}
	if !caps.Typing {
		t.Error("Expected Typing capability to be true")
	}
	if !caps.Attachments {
		t.Error("Expected Attachments capability to be true")
	}
	if !caps.RichText {
		t.Error("Expected RichText capability to be true")
	}
	if !caps.Threads {
		t.Error("Expected Threads capability to be true")
	}
	if caps.MaxMessageLength != 2000 {
		t.Errorf("MaxMessageLength = %d, want 2000", caps.MaxMessageLength)
	}
	if caps.MaxAttachmentSize != 8<<20 {
		t.Errorf("MaxAttachmentSize = %d, want %d", caps.MaxAttachmentSize, 8<<20)
	}
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

	if adapter.health == nil {
		t.Error("adapter.health is nil")
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

func TestNewAdapterSimple_InvalidToken(t *testing.T) {
	// NewAdapterSimple returns nil on invalid config (empty token)
	adapter := NewAdapterSimple("")
	if adapter != nil {
		t.Error("NewAdapterSimple with empty token should return nil")
	}
}

func TestTryNewAdapterSimple(t *testing.T) {
	adapter, err := TryNewAdapterSimple("test-token")

	if err != nil {
		t.Fatalf("TryNewAdapterSimple returned error: %v", err)
	}

	if adapter == nil {
		t.Fatal("TryNewAdapterSimple returned nil adapter")
	}

	if adapter.Type() != models.ChannelDiscord {
		t.Errorf("Expected channel type %s, got %s", models.ChannelDiscord, adapter.Type())
	}
}

func TestTryNewAdapterSimple_Error(t *testing.T) {
	adapter, err := TryNewAdapterSimple("")

	if err == nil {
		t.Error("TryNewAdapterSimple with empty token should return error")
	}

	if adapter != nil {
		t.Error("TryNewAdapterSimple with empty token should return nil adapter")
	}
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
	adapter.updateStatus(true, "")

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
	adapter.updateStatus(true, "")
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
	// Reduce retry settings for faster test
	adapter.config.MaxReconnectAttempts = 2
	adapter.config.ReconnectBackoff = 10 * time.Millisecond

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
			adapter.updateStatus(true, "")

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
	adapter.updateStatus(true, "")

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
	adapter.updateStatus(true, "")

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
	adapter.updateStatus(true, "")

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
	adapter.updateStatus(true, "")

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

func (m *mockDiscordSession) ChannelMessageEdit(channelID, messageID, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return &discordgo.Message{
		ID:        messageID,
		ChannelID: channelID,
		Content:   content,
	}, nil
}

func (m *mockDiscordSession) ChannelTyping(channelID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *mockDiscordSession) MessageReactionAdd(channelID, messageID, emoji string, options ...discordgo.RequestOption) error {
	if m.messageReactionAddFn != nil {
		return m.messageReactionAddFn(channelID, messageID, emoji)
	}
	return nil
}

func (m *mockDiscordSession) ChannelMessageDelete(channelID, messageID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *mockDiscordSession) MessageReactionRemove(channelID, messageID, emoji, userID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *mockDiscordSession) ChannelMessagePin(channelID, messageID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *mockDiscordSession) ChannelMessageUnpin(channelID, messageID string, options ...discordgo.RequestOption) error {
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

// =============================================================================
// Extended Mock Session for Enhanced Testing
// =============================================================================

type extendedMockDiscordSession struct {
	mockDiscordSession

	// Track method calls for verification
	channelMessageSendCalls        []channelMessageSendCall
	channelMessageSendComplexCalls []channelMessageSendComplexCall
	reactionCalls                  []reactionCall
	threadStartCalls               []threadStartCall

	// Error injection
	channelMessageSendComplexFn func(channelID string, data *discordgo.MessageSend) (*discordgo.Message, error)

	// Mutex for concurrent access
	mu sync.Mutex
}

type channelMessageSendCall struct {
	ChannelID string
	Content   string
}

type channelMessageSendComplexCall struct {
	ChannelID string
	Data      *discordgo.MessageSend
}

type reactionCall struct {
	ChannelID string
	MessageID string
	Emoji     string
}

type threadStartCall struct {
	ChannelID       string
	Name            string
	ArchiveDuration int
}

func (m *extendedMockDiscordSession) ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	m.mu.Lock()
	m.channelMessageSendCalls = append(m.channelMessageSendCalls, channelMessageSendCall{
		ChannelID: channelID,
		Content:   content,
	})
	m.mu.Unlock()

	if m.channelMessageSendFn != nil {
		return m.channelMessageSendFn(channelID, content, options...)
	}
	return &discordgo.Message{
		ID:        "test-msg-id",
		ChannelID: channelID,
		Content:   content,
	}, nil
}

func (m *extendedMockDiscordSession) ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	m.mu.Lock()
	m.channelMessageSendComplexCalls = append(m.channelMessageSendComplexCalls, channelMessageSendComplexCall{
		ChannelID: channelID,
		Data:      data,
	})
	m.mu.Unlock()

	if m.channelMessageSendComplexFn != nil {
		return m.channelMessageSendComplexFn(channelID, data)
	}
	return &discordgo.Message{
		ID:        "test-msg-id",
		ChannelID: channelID,
		Content:   data.Content,
		Embeds:    data.Embeds,
	}, nil
}

func (m *extendedMockDiscordSession) MessageReactionAdd(channelID, messageID, emoji string, options ...discordgo.RequestOption) error {
	m.mu.Lock()
	m.reactionCalls = append(m.reactionCalls, reactionCall{
		ChannelID: channelID,
		MessageID: messageID,
		Emoji:     emoji,
	})
	m.mu.Unlock()

	if m.messageReactionAddFn != nil {
		return m.messageReactionAddFn(channelID, messageID, emoji)
	}
	return nil
}

func (m *extendedMockDiscordSession) ChannelMessageDelete(channelID, messageID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *extendedMockDiscordSession) MessageReactionRemove(channelID, messageID, emoji, userID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *extendedMockDiscordSession) ChannelMessagePin(channelID, messageID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *extendedMockDiscordSession) ChannelMessageUnpin(channelID, messageID string, options ...discordgo.RequestOption) error {
	return nil
}

func (m *extendedMockDiscordSession) ThreadStart(channelID, name string, typ discordgo.ChannelType, archiveDuration int, options ...discordgo.RequestOption) (*discordgo.Channel, error) {
	m.mu.Lock()
	m.threadStartCalls = append(m.threadStartCalls, threadStartCall{
		ChannelID:       channelID,
		Name:            name,
		ArchiveDuration: archiveDuration,
	})
	m.mu.Unlock()

	if m.threadStartFn != nil {
		return m.threadStartFn(channelID, name, archiveDuration)
	}
	return &discordgo.Channel{
		ID:   "test-thread-id",
		Name: name,
		Type: discordgo.ChannelTypeGuildPublicThread,
	}, nil
}

// =============================================================================
// Message Handler Tests
// =============================================================================

func TestAdapter_HandleMessageCreate_BotMessage(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Create a bot message (should be ignored)
	botMsg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "bot-msg-123",
			ChannelID: "channel-456",
			Content:   "Bot message",
			Author: &discordgo.User{
				ID:       "bot-user-789",
				Username: "TestBot",
				Bot:      true,
			},
		},
	}

	// Call the handler directly
	adapter.handleMessageCreate(nil, botMsg)

	// Bot messages should be ignored - no message in channel
	select {
	case msg := <-adapter.messages:
		t.Errorf("Expected bot message to be ignored, but got: %v", msg)
	default:
		// Expected behavior - message was ignored
	}
}

func TestAdapter_HandleMessageCreate_UserMessage(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Create a user message
	userMsg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "user-msg-123",
			ChannelID: "channel-456",
			Content:   "Hello from user",
			Author: &discordgo.User{
				ID:       "user-789",
				Username: "TestUser",
				Bot:      false,
			},
			Timestamp: time.Now(),
		},
	}

	// Call the handler
	adapter.handleMessageCreate(nil, userMsg)

	// Should receive the message
	select {
	case msg := <-adapter.messages:
		if msg.Content != "Hello from user" {
			t.Errorf("Expected content 'Hello from user', got %q", msg.Content)
		}
		if msg.Metadata["discord_user_id"] != "user-789" {
			t.Errorf("Expected user ID 'user-789', got %v", msg.Metadata["discord_user_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive user message, but timed out")
	}
}

func TestAdapter_HandleMessageCreate_ChannelFull(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Fill the messages channel
	for i := 0; i < 100; i++ {
		adapter.messages <- &models.Message{Content: fmt.Sprintf("fill-%d", i)}
	}

	// Create a user message
	userMsg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "user-msg-overflow",
			ChannelID: "channel-456",
			Content:   "This should be dropped",
			Author: &discordgo.User{
				ID:       "user-789",
				Username: "TestUser",
				Bot:      false,
			},
			Timestamp: time.Now(),
		},
	}

	// Call the handler - should not block
	done := make(chan struct{})
	go func() {
		adapter.handleMessageCreate(nil, userMsg)
		close(done)
	}()

	select {
	case <-done:
		// Good - handler didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("handleMessageCreate blocked when channel was full")
	}
}

func TestAdapter_HandleMessageCreate_ContextCancelled(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Cancel context before sending message
	cancel()

	// Fill the channel so the message would block
	for i := 0; i < 100; i++ {
		adapter.messages <- &models.Message{Content: fmt.Sprintf("fill-%d", i)}
	}

	userMsg := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "user-msg-123",
			ChannelID: "channel-456",
			Content:   "Test",
			Author: &discordgo.User{
				ID:       "user-789",
				Username: "TestUser",
				Bot:      false,
			},
			Timestamp: time.Now(),
		},
	}

	// Should not block due to cancelled context
	done := make(chan struct{})
	go func() {
		adapter.handleMessageCreate(nil, userMsg)
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("handleMessageCreate blocked with cancelled context")
	}
}

func TestAdapter_HandleInteractionCreate_SlashCommand(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Create a slash command interaction
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "interaction-123",
			Type:      discordgo.InteractionApplicationCommand,
			ChannelID: "channel-456",
			Member: &discordgo.Member{
				User: &discordgo.User{
					ID:       "user-789",
					Username: "TestUser",
				},
			},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "help",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{
						Name:  "topic",
						Value: "commands",
					},
				},
			},
		},
	}

	adapter.handleInteractionCreate(nil, interaction)

	select {
	case msg := <-adapter.messages:
		if msg.Metadata["discord_command_name"] != "help" {
			t.Errorf("Expected command name 'help', got %v", msg.Metadata["discord_command_name"])
		}
		if msg.Content != "/help topic:commands" {
			t.Errorf("Expected content '/help topic:commands', got %q", msg.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive interaction message")
	}
}

func TestAdapter_HandleInteractionCreate_NonCommand(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Create a non-command interaction (should be ignored)
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:   "interaction-123",
			Type: discordgo.InteractionMessageComponent, // Not a command
		},
	}

	adapter.handleInteractionCreate(nil, interaction)

	// Should be ignored
	select {
	case msg := <-adapter.messages:
		t.Errorf("Expected non-command interaction to be ignored, got: %v", msg)
	default:
		// Expected
	}
}

func TestAdapter_HandleReady(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(false, "")
	adapter.reconnectCount = 5
	adapter.setDegraded(true)

	readyEvent := &discordgo.Ready{
		User: &discordgo.User{
			ID:       "bot-123",
			Username: "TestBot",
		},
		Guilds: []*discordgo.Guild{
			{ID: "guild-1"},
			{ID: "guild-2"},
		},
	}

	adapter.handleReady(nil, readyEvent)

	status := adapter.Status()
	if !status.Connected {
		t.Error("Expected status.Connected to be true after handleReady")
	}
	if status.Error != "" {
		t.Errorf("Expected status.Error to be empty, got %q", status.Error)
	}
	if adapter.reconnectCount != 0 {
		t.Errorf("Expected reconnectCount to be 0, got %d", adapter.reconnectCount)
	}
	if adapter.isDegraded() {
		t.Error("Expected degraded to be false after handleReady")
	}
}

func TestAdapter_HandleDisconnect(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel

	disconnectEvent := &discordgo.Disconnect{}

	// Cancel context to prevent actual reconnection attempt
	cancel()

	adapter.handleDisconnect(nil, disconnectEvent)

	status := adapter.Status()
	if status.Connected {
		t.Error("Expected status.Connected to be false after handleDisconnect")
	}
	if status.Error != "disconnected from Discord" {
		t.Errorf("Expected status.Error 'disconnected from Discord', got %q", status.Error)
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestAdapter_ConcurrentSends(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	ctx := context.Background()
	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := &models.Message{
				Content: fmt.Sprintf("Message %d", n),
				Metadata: map[string]any{
					"discord_channel_id": "channel-123",
				},
			}
			_ = adapter.Send(ctx, msg)
		}(i)
	}

	wg.Wait()

	// Verify all messages were sent
	mock.mu.Lock()
	callCount := len(mock.channelMessageSendCalls)
	mock.mu.Unlock()

	if callCount != numGoroutines {
		t.Errorf("Expected %d sends, got %d", numGoroutines, callCount)
	}
}

func TestAdapter_ConcurrentStatusReads(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	var wg sync.WaitGroup
	numReaders := 100

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := adapter.Status()
			_ = status.Connected
		}()
	}

	wg.Wait()
	// Test passes if no race condition panics
}

func TestAdapter_ConcurrentDegradedAccess(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

	var wg sync.WaitGroup
	numOps := 100

	// Concurrent writers
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			adapter.setDegraded(n%2 == 0)
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = adapter.isDegraded()
		}()
	}

	wg.Wait()
	// Test passes if no race condition
}

func TestAdapter_ConcurrentHealthChecks(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	ctx := context.Background()
	var wg sync.WaitGroup
	numChecks := 50

	for i := 0; i < numChecks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			health := adapter.HealthCheck(ctx)
			_ = health.Healthy
		}()
	}

	wg.Wait()
}

// =============================================================================
// Embed/Attachment Tests
// =============================================================================

func TestAdapter_SendEmbed_TitleOnly(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "Fallback content",
		Metadata: map[string]any{
			"discord_channel_id":  "channel-123",
			"discord_embed_title": "Important Notice",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	mock.mu.Lock()
	calls := mock.channelMessageSendComplexCalls
	mock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("Expected 1 complex send call, got %d", len(calls))
	}

	if len(calls[0].Data.Embeds) != 1 {
		t.Fatalf("Expected 1 embed, got %d", len(calls[0].Data.Embeds))
	}

	embed := calls[0].Data.Embeds[0]
	if embed.Title != "Important Notice" {
		t.Errorf("Expected embed title 'Important Notice', got %q", embed.Title)
	}
	if embed.Description != "Fallback content" {
		t.Errorf("Expected embed description to fallback to content, got %q", embed.Description)
	}
}

func TestAdapter_SendEmbed_ColorOnly(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "Colored message",
		Metadata: map[string]any{
			"discord_channel_id":  "channel-123",
			"discord_embed_color": 0xFF5500,
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	mock.mu.Lock()
	calls := mock.channelMessageSendComplexCalls
	mock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("Expected 1 complex send call, got %d", len(calls))
	}

	embed := calls[0].Data.Embeds[0]
	if embed.Color != 0xFF5500 {
		t.Errorf("Expected embed color 0xFF5500, got 0x%X", embed.Color)
	}
}

func TestAdapter_SendEmbed_DescriptionOverridesContent(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "This should be ignored",
		Metadata: map[string]any{
			"discord_channel_id":        "channel-123",
			"discord_embed_title":       "Title",
			"discord_embed_description": "Custom description",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	mock.mu.Lock()
	calls := mock.channelMessageSendComplexCalls
	mock.mu.Unlock()

	embed := calls[0].Data.Embeds[0]
	if embed.Description != "Custom description" {
		t.Errorf("Expected embed description 'Custom description', got %q", embed.Description)
	}
}

func TestAdapter_SendEmbed_ComplexError(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{
		channelMessageSendComplexFn: func(channelID string, data *discordgo.MessageSend) (*discordgo.Message, error) {
			return nil, errors.New("embed send failed")
		},
	}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"discord_channel_id":  "channel-123",
			"discord_embed_title": "Title",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when embed send fails")
	}
}

func TestAdapter_SendNoContent(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	// Message with empty content and no embed
	msg := &models.Message{
		Content: "",
		Metadata: map[string]any{
			"discord_channel_id": "channel-123",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Should succeed but not send anything
	if err != nil {
		t.Errorf("Expected no error for empty message, got: %v", err)
	}

	mock.mu.Lock()
	simpleCalls := len(mock.channelMessageSendCalls)
	complexCalls := len(mock.channelMessageSendComplexCalls)
	mock.mu.Unlock()

	if simpleCalls != 0 || complexCalls != 0 {
		t.Errorf("Expected no sends for empty content, got simple=%d complex=%d", simpleCalls, complexCalls)
	}
}

// =============================================================================
// Thread Tests
// =============================================================================

func TestAdapter_SendThreadCreate_Error(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	mock.threadStartFn = func(channelID, name string, archiveDuration int) (*discordgo.Channel, error) {
		return nil, errors.New("thread creation failed")
	}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "Thread message",
		Metadata: map[string]any{
			"discord_channel_id":    "channel-123",
			"discord_create_thread": true,
			"discord_thread_name":   "Test Thread",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when thread creation fails")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInternal {
			t.Errorf("Expected ErrCodeInternal, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendThreadCreate_DefaultName(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "Thread message",
		Metadata: map[string]any{
			"discord_channel_id":    "channel-123",
			"discord_create_thread": true,
			// No thread name - should default to "Discussion"
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	mock.mu.Lock()
	threadCalls := mock.threadStartCalls
	mock.mu.Unlock()

	if len(threadCalls) != 1 {
		t.Fatalf("Expected 1 thread start call, got %d", len(threadCalls))
	}

	if threadCalls[0].Name != "Discussion" {
		t.Errorf("Expected default thread name 'Discussion', got %q", threadCalls[0].Name)
	}
}

func TestAdapter_SendThreadCreate_EmptyName(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "Thread message",
		Metadata: map[string]any{
			"discord_channel_id":    "channel-123",
			"discord_create_thread": true,
			"discord_thread_name":   "", // Empty should default
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	mock.mu.Lock()
	threadCalls := mock.threadStartCalls
	mock.mu.Unlock()

	if threadCalls[0].Name != "Discussion" {
		t.Errorf("Expected default thread name 'Discussion', got %q", threadCalls[0].Name)
	}
}

// =============================================================================
// Reaction Tests
// =============================================================================

func TestAdapter_SendReaction_Success(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "",
		Metadata: map[string]any{
			"discord_channel_id":      "channel-123",
			"discord_reaction_emoji":  "thumbsup",
			"discord_reaction_msg_id": "target-msg-456",
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	mock.mu.Lock()
	reactionCalls := mock.reactionCalls
	mock.mu.Unlock()

	if len(reactionCalls) != 1 {
		t.Fatalf("Expected 1 reaction call, got %d", len(reactionCalls))
	}

	if reactionCalls[0].ChannelID != "channel-123" {
		t.Errorf("Expected channel 'channel-123', got %q", reactionCalls[0].ChannelID)
	}
	if reactionCalls[0].MessageID != "target-msg-456" {
		t.Errorf("Expected message ID 'target-msg-456', got %q", reactionCalls[0].MessageID)
	}
	if reactionCalls[0].Emoji != "thumbsup" {
		t.Errorf("Expected emoji 'thumbsup', got %q", reactionCalls[0].Emoji)
	}
}

func TestAdapter_SendReaction_MissingMessageID(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &extendedMockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	// Has emoji but no message ID
	msg := &models.Message{
		Content: "Some content",
		Metadata: map[string]any{
			"discord_channel_id":     "channel-123",
			"discord_reaction_emoji": "thumbsup",
			// Missing discord_reaction_msg_id
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Should fall through to regular message send
	mock.mu.Lock()
	reactionCalls := len(mock.reactionCalls)
	messageCalls := len(mock.channelMessageSendCalls)
	mock.mu.Unlock()

	if reactionCalls != 0 {
		t.Errorf("Expected no reaction calls, got %d", reactionCalls)
	}
	if messageCalls != 1 {
		t.Errorf("Expected 1 message call, got %d", messageCalls)
	}
}

// =============================================================================
// Error Recovery Tests
// =============================================================================

func TestAdapter_ConnectWithRetry_Success(t *testing.T) {
	adapter := NewAdapterSimple("test-token")

	attemptCount := 0
	mock := &mockDiscordSession{
		openErr: nil,
	}
	// First attempt succeeds
	mock.openErr = nil
	adapter.session = mock

	ctx := context.Background()
	err := adapter.connectWithRetry(ctx)

	if err != nil {
		t.Errorf("Expected successful connection, got error: %v", err)
	}
	if !mock.openCalled {
		t.Error("Expected Open to be called")
	}

	_ = attemptCount
}

func TestAdapter_ConnectWithRetry_FailsThenSucceeds(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	adapter.config.MaxReconnectAttempts = 5
	adapter.config.ReconnectBackoff = 10 * time.Millisecond

	attemptCount := 0
	mock := &retryMockDiscordSession{
		failUntilAttempt: 3,
		attemptCount:     &attemptCount,
	}
	adapter.session = mock

	ctx := context.Background()
	err := adapter.connectWithRetry(ctx)

	if err != nil {
		t.Errorf("Expected successful connection after retries, got: %v", err)
	}
	if attemptCount != 3 {
		t.Errorf("Expected 3 attempts, got %d", attemptCount)
	}
}

// retryMockDiscordSession is a mock that fails until a certain attempt count
type retryMockDiscordSession struct {
	mockDiscordSession
	failUntilAttempt int
	attemptCount     *int
}

func (m *retryMockDiscordSession) Open() error {
	*m.attemptCount++
	if *m.attemptCount < m.failUntilAttempt {
		return errors.New("connection failed")
	}
	return nil
}

func TestAdapter_ConnectWithRetry_AllAttemptsFail(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	adapter.config.MaxReconnectAttempts = 2
	adapter.config.ReconnectBackoff = 50 * time.Millisecond

	mock := &mockDiscordSession{
		openErr: errors.New("persistent connection failure"),
	}
	adapter.session = mock

	ctx := context.Background()
	err := adapter.connectWithRetry(ctx)

	if err == nil {
		t.Error("Expected error when all connection attempts fail")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeConnection {
			t.Errorf("Expected ErrCodeConnection, got %v", chErr.Code)
		}
	}
}

func TestAdapter_ConnectWithRetry_ContextCancelled(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	adapter.config.MaxReconnectAttempts = 5
	adapter.config.ReconnectBackoff = 5 * time.Second

	mock := &mockDiscordSession{
		openErr: errors.New("connection failed"),
	}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := adapter.connectWithRetry(ctx)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestAdapter_Reconnect_ContextCancelled(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel

	// Cancel immediately
	cancel()

	// Reconnect should return early
	adapter.wg.Add(1)
	go adapter.reconnect()
	adapter.wg.Wait()
	// Test passes if it doesn't hang
}

func TestAdapter_Reconnect_Success(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.config.ReconnectBackoff = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(false, "")
	adapter.setDegraded(true)

	adapter.wg.Add(1)
	go adapter.reconnect()
	adapter.wg.Wait()

	status := adapter.Status()
	if !status.Connected {
		t.Error("Expected status.Connected to be true after successful reconnect")
	}
	if adapter.isDegraded() {
		t.Error("Expected degraded to be false after successful reconnect")
	}
	if adapter.reconnectCount != 0 {
		t.Errorf("Expected reconnectCount to be 0, got %d", adapter.reconnectCount)
	}
}

func TestAdapter_Reconnect_Failure(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{
		openErr: errors.New("reconnect failed"),
	}
	adapter.session = mock
	adapter.config.ReconnectBackoff = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(false, "")

	adapter.wg.Add(1)
	go adapter.reconnect()
	adapter.wg.Wait()

	status := adapter.Status()
	if status.Connected {
		t.Error("Expected status.Connected to be false after failed reconnect")
	}
	if status.Error == "" {
		t.Error("Expected status.Error to be set after failed reconnect")
	}
}

// =============================================================================
// Rate Limit Tests
// =============================================================================

func TestAdapter_Send_RateLimiterContextCancelled(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	// Create a very restrictive rate limiter
	adapter.rateLimiter = channels.NewRateLimiter(0.001, 1) // Very slow

	// Use up the burst
	ctx1 := context.Background()
	msg1 := &models.Message{
		Content: "First",
		Metadata: map[string]any{
			"discord_channel_id": "channel-123",
		},
	}
	_ = adapter.Send(ctx1, msg1)

	// Now cancel the context for the second send
	ctx2, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	msg2 := &models.Message{
		Content: "Second",
		Metadata: map[string]any{
			"discord_channel_id": "channel-123",
		},
	}

	err := adapter.Send(ctx2, msg2)

	if err == nil {
		t.Error("Expected error when rate limiter wait is cancelled")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeTimeout {
			t.Errorf("Expected ErrCodeTimeout, got %v", chErr.Code)
		}
	}
}

// =============================================================================
// Stop Tests
// =============================================================================

func TestAdapter_Stop_CloseError(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{
		closeErr: errors.New("close failed"),
	}
	adapter.session = mock

	ctx := context.Background()

	// Start first
	err := adapter.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should return error
	err = adapter.Stop(ctx)
	if err == nil {
		t.Error("Expected error when Close fails")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeConnection {
			t.Errorf("Expected ErrCodeConnection, got %v", chErr.Code)
		}
	}
}

func TestAdapter_Stop_Timeout(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx := context.Background()
	err := adapter.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Add a goroutine that will block
	adapter.wg.Add(1)
	go func() {
		time.Sleep(5 * time.Second) // Long sleep
		adapter.wg.Done()
	}()

	// Stop with short timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Should not block forever
	done := make(chan error)
	go func() {
		done <- adapter.Stop(stopCtx)
	}()

	select {
	case <-done:
		// Good - Stop returned
	case <-time.After(1 * time.Second):
		t.Error("Stop blocked for too long")
	}
}

// =============================================================================
// Metrics Tests
// =============================================================================

func TestAdapter_MetricsAfterOperations(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx := context.Background()
	err := adapter.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Send a message
	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"discord_channel_id": "channel-123",
		},
	}
	_ = adapter.Send(ctx, msg)

	metrics := adapter.Metrics()

	if metrics.MessagesSent == 0 {
		t.Error("Expected MessagesSent > 0")
	}
	if metrics.ConnectionsOpened == 0 {
		t.Error("Expected ConnectionsOpened > 0")
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestAdapter_SendWithNilMetadata(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content:  "Test",
		Metadata: nil,
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	// Should fail because discord_channel_id is missing
	if err == nil {
		t.Error("Expected error when metadata is nil")
	}
}

func TestAdapter_SendWithWrongChannelIDType(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock
	adapter.updateStatus(true, "")

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"discord_channel_id": 12345, // Wrong type - should be string
		},
	}

	ctx := context.Background()
	err := adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when channel ID is wrong type")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInvalidInput {
			t.Errorf("Expected ErrCodeInvalidInput, got %v", chErr.Code)
		}
	}
}

func TestAdapter_HandleInteractionCreate_ChannelFull(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Fill the messages channel
	for i := 0; i < 100; i++ {
		adapter.messages <- &models.Message{Content: fmt.Sprintf("fill-%d", i)}
	}

	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "interaction-123",
			Type:      discordgo.InteractionApplicationCommand,
			ChannelID: "channel-456",
			Member: &discordgo.Member{
				User: &discordgo.User{
					ID:       "user-789",
					Username: "TestUser",
				},
			},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "test",
			},
		},
	}

	// Should not block
	done := make(chan struct{})
	go func() {
		adapter.handleInteractionCreate(nil, interaction)
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("handleInteractionCreate blocked when channel was full")
	}
}

func TestAdapter_HandleInteractionCreate_ContextCancelled(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	// Cancel context
	cancel()

	// Fill the channel
	for i := 0; i < 100; i++ {
		adapter.messages <- &models.Message{Content: fmt.Sprintf("fill-%d", i)}
	}

	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "interaction-123",
			Type:      discordgo.InteractionApplicationCommand,
			ChannelID: "channel-456",
			Member: &discordgo.Member{
				User: &discordgo.User{
					ID:       "user-789",
					Username: "TestUser",
				},
			},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "test",
			},
		},
	}

	// Should not block
	done := make(chan struct{})
	go func() {
		adapter.handleInteractionCreate(nil, interaction)
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("handleInteractionCreate blocked with cancelled context")
	}
}

// =============================================================================
// Multiple Command Options Test
// =============================================================================

func TestAdapter_HandleInteractionCreate_MultipleOptions(t *testing.T) {
	adapter := NewAdapterSimple("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx, cancel := context.WithCancel(context.Background())
	adapter.ctx = ctx
	adapter.cancel = cancel
	adapter.updateStatus(true, "")

	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "interaction-123",
			Type:      discordgo.InteractionApplicationCommand,
			ChannelID: "channel-456",
			Member: &discordgo.Member{
				User: &discordgo.User{
					ID:       "user-789",
					Username: "TestUser",
				},
			},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "search",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "query", Value: "hello world"},
					{Name: "limit", Value: 10},
					{Name: "sort", Value: "date"},
				},
			},
		},
	}

	adapter.handleInteractionCreate(nil, interaction)

	select {
	case msg := <-adapter.messages:
		expectedContent := "/search query:hello world limit:10 sort:date"
		if msg.Content != expectedContent {
			t.Errorf("Expected content %q, got %q", expectedContent, msg.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive interaction message")
	}
}
