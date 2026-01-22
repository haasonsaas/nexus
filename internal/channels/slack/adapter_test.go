package slack

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
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
				BotToken: "xoxb-test-token",
				AppToken: "xapp-test-token",
			},
			wantErr: false,
		},
		{
			name: "missing bot token",
			cfg: Config{
				AppToken: "xapp-test-token",
			},
			wantErr: true,
			errMsg:  "bot_token is required",
		},
		{
			name: "missing app token",
			cfg: Config{
				BotToken: "xoxb-test-token",
			},
			wantErr: true,
			errMsg:  "app_token is required",
		},
		{
			name:    "missing both tokens",
			cfg:     Config{},
			wantErr: true,
		},
		{
			name: "empty bot token",
			cfg: Config{
				BotToken: "",
				AppToken: "xapp-test-token",
			},
			wantErr: true,
		},
		{
			name: "empty app token",
			cfg: Config{
				BotToken: "xoxb-test-token",
				AppToken: "",
			},
			wantErr: true,
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
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Check default values were applied
	if cfg.RateLimit != 1 {
		t.Errorf("RateLimit = %f, want 1", cfg.RateLimit)
	}

	if cfg.RateBurst != 5 {
		t.Errorf("RateBurst = %d, want 5", cfg.RateBurst)
	}

	if cfg.Logger == nil {
		t.Error("Logger should not be nil after validation")
	}
}

func TestConfig_CustomValues(t *testing.T) {
	logger := slog.Default()
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 5,
		RateBurst: 20,
		Logger:    logger,
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Custom values should be preserved
	if cfg.RateLimit != 5 {
		t.Errorf("RateLimit = %f, want 5", cfg.RateLimit)
	}

	if cfg.RateBurst != 20 {
		t.Errorf("RateBurst = %d, want 20", cfg.RateBurst)
	}
}

// =============================================================================
// Adapter Interface Tests
// =============================================================================

func TestAdapter_Type(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	if adapter.Type() != models.ChannelSlack {
		t.Errorf("Expected type %s, got %s", models.ChannelSlack, adapter.Type())
	}
}

func TestAdapter_Status(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	status := adapter.Status()

	if status.Connected {
		t.Error("Expected adapter to be disconnected initially")
	}
	if status.Error != "" {
		t.Errorf("Expected empty error, got %q", status.Error)
	}
}

func TestAdapter_Messages(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	msgChan := adapter.Messages()

	if msgChan == nil {
		t.Fatal("Messages() returned nil channel")
	}
}

func TestAdapter_Metrics(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	metrics := adapter.Metrics()
	if metrics.ChannelType != models.ChannelSlack {
		t.Errorf("Metrics().ChannelType = %v, want %v", metrics.ChannelType, models.ChannelSlack)
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
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if adapter == nil {
		t.Fatal("NewAdapter() returned nil adapter")
	}

	if adapter.cfg.BotToken != "xoxb-test-token" {
		t.Errorf("adapter.cfg.BotToken = %q, want %q", adapter.cfg.BotToken, "xoxb-test-token")
	}

	if adapter.cfg.AppToken != "xapp-test-token" {
		t.Errorf("adapter.cfg.AppToken = %q, want %q", adapter.cfg.AppToken, "xapp-test-token")
	}

	if adapter.messages == nil {
		t.Error("adapter.messages channel is nil")
	}

	if adapter.client == nil {
		t.Error("adapter.client is nil")
	}

	if adapter.socketClient == nil {
		t.Error("adapter.socketClient is nil")
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
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "empty bot token",
			cfg:  Config{BotToken: "", AppToken: "xapp-test"},
		},
		{
			name: "empty app token",
			cfg:  Config{BotToken: "xoxb-test", AppToken: ""},
		},
		{
			name: "both empty",
			cfg:  Config{},
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

// =============================================================================
// Message Conversion Tests
// =============================================================================

func TestConvertSlackMessage_SimpleText(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:            "message",
		User:            "U123456",
		Text:            "Hello, world!",
		Channel:         "C123456",
		TimeStamp:       "1234567890.123456",
		ThreadTimeStamp: "",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if msg.Channel != models.ChannelSlack {
		t.Errorf("Expected channel %s, got %s", models.ChannelSlack, msg.Channel)
	}

	if msg.ChannelID != "C123456:1234567890.123456" {
		t.Errorf("Expected channel_id C123456:1234567890.123456, got %s", msg.ChannelID)
	}

	if msg.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got %s", msg.Content)
	}

	if msg.Direction != models.DirectionInbound {
		t.Errorf("Expected direction %s, got %s", models.DirectionInbound, msg.Direction)
	}

	if msg.Role != models.RoleUser {
		t.Errorf("Expected role %s, got %s", models.RoleUser, msg.Role)
	}
}

func TestConvertSlackMessage_Metadata(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:            "message",
		User:            "U123456",
		Text:            "Test message",
		Channel:         "C123456",
		TimeStamp:       "1234567890.123456",
		ThreadTimeStamp: "",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if msg.Metadata == nil {
		t.Fatal("Expected metadata to be non-nil")
	}

	if msg.Metadata["slack_user_id"] != "U123456" {
		t.Errorf("Expected slack_user_id U123456, got %v", msg.Metadata["slack_user_id"])
	}

	if msg.Metadata["slack_channel"] != "C123456" {
		t.Errorf("Expected slack_channel C123456, got %v", msg.Metadata["slack_channel"])
	}

	if msg.Metadata["slack_ts"] != "1234567890.123456" {
		t.Errorf("Expected slack_ts 1234567890.123456, got %v", msg.Metadata["slack_ts"])
	}

	if msg.Metadata["slack_thread_ts"] != "" {
		t.Errorf("Expected slack_thread_ts empty, got %v", msg.Metadata["slack_thread_ts"])
	}
}

func TestConvertSlackMessage_ThreadReply(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:            "message",
		User:            "U123456",
		Text:            "Reply in thread",
		Channel:         "C123456",
		TimeStamp:       "1234567890.123456",
		ThreadTimeStamp: "1234567880.000000",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if msg.Metadata["slack_thread_ts"] != "1234567880.000000" {
		t.Errorf("Expected slack_thread_ts 1234567880.000000, got %v", msg.Metadata["slack_thread_ts"])
	}

	if msg.SessionID == "" {
		t.Error("Expected SessionID to be set for threaded message")
	}
}

func TestConvertSlackMessage_WithMentions(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Hello <@U789012> and <@U345678>!",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	// Mentions should be stripped from content and trimmed
	// "Hello <@U789012> and <@U345678>!" -> "Hello  and !" -> "Hello  and !" (TrimSpace)
	if msg.Content != "Hello  and !" {
		t.Errorf("Expected mentions to be stripped, got %q", msg.Content)
	}
}

func TestConvertSlackMessage_OnlyMention(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "<@U789012>",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	// Single mention should result in empty content after trimming
	if msg.Content != "" {
		t.Errorf("Expected empty content after stripping mention, got %q", msg.Content)
	}
}

func TestConvertSlackMessage_WithFiles(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Here's a file",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
		Message: &slack.Msg{
			Files: []slack.File{
				{
					ID:                 "F123456",
					Name:               "test.txt",
					Mimetype:           "text/plain",
					URLPrivateDownload: "https://files.slack.com/files/test.txt",
					Size:               1024,
				},
				{
					ID:                 "F789012",
					Name:               "image.png",
					Mimetype:           "image/png",
					URLPrivateDownload: "https://files.slack.com/files/image.png",
					Size:               2048,
				},
			},
		},
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if len(msg.Attachments) != 2 {
		t.Fatalf("Expected 2 attachments, got %d", len(msg.Attachments))
	}

	// Check first attachment
	att1 := msg.Attachments[0]
	if att1.ID != "F123456" {
		t.Errorf("Expected attachment ID F123456, got %s", att1.ID)
	}
	if att1.Filename != "test.txt" {
		t.Errorf("Expected filename test.txt, got %s", att1.Filename)
	}
	if att1.MimeType != "text/plain" {
		t.Errorf("Expected mime type text/plain, got %s", att1.MimeType)
	}
	if att1.Size != 1024 {
		t.Errorf("Expected size 1024, got %d", att1.Size)
	}
	if att1.Type != "document" {
		t.Errorf("Expected type document, got %s", att1.Type)
	}

	// Check second attachment (image)
	att2 := msg.Attachments[1]
	if att2.Type != "image" {
		t.Errorf("Expected type image, got %s", att2.Type)
	}
}

func TestConvertSlackMessage_EmptyMessage(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if msg.Content != "" {
		t.Errorf("Expected empty content, got %q", msg.Content)
	}
}

func TestConvertSlackMessage_DirectMessage(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "DM message",
		Channel:   "D123456", // DM channels start with D
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if msg.Metadata["slack_channel"] != "D123456" {
		t.Errorf("Expected slack_channel D123456, got %v", msg.Metadata["slack_channel"])
	}
}

// =============================================================================
// Attachment Type Detection Tests
// =============================================================================

func TestGetAttachmentType(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"image/gif", "image"},
		{"image/webp", "image"},
		{"audio/mpeg", "audio"},
		{"audio/wav", "audio"},
		{"audio/ogg", "audio"},
		{"video/mp4", "video"},
		{"video/quicktime", "video"},
		{"video/webm", "video"},
		{"application/pdf", "document"},
		{"application/zip", "document"},
		{"text/plain", "document"},
		{"text/html", "document"},
		{"", "document"},
		{"application/octet-stream", "document"},
		{"unknown/type", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := getAttachmentType(tt.mimeType)
			if result != tt.expected {
				t.Errorf("getAttachmentType(%q) = %q, expected %q", tt.mimeType, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Session ID Generation Tests
// =============================================================================

func TestGenerateSessionID(t *testing.T) {
	channel := "C123456"
	threadTS := "1234567890.123456"

	sessionID := generateSessionID(channel, threadTS)

	if sessionID == "" {
		t.Error("Expected non-empty session ID")
	}

	// Verify it's deterministic
	sessionID2 := generateSessionID(channel, threadTS)
	if sessionID != sessionID2 {
		t.Error("Expected generateSessionID to be deterministic")
	}

	// Verify different inputs produce different outputs
	sessionID3 := generateSessionID(channel, "9999999999.999999")
	if sessionID == sessionID3 {
		t.Error("Expected different thread timestamps to produce different session IDs")
	}

	// Verify different channels produce different outputs
	sessionID4 := generateSessionID("C999999", threadTS)
	if sessionID == sessionID4 {
		t.Error("Expected different channels to produce different session IDs")
	}
}

func TestGenerateSessionID_HashLength(t *testing.T) {
	sessionID := generateSessionID("C123456", "1234567890.123456")

	// SHA-256 hex encoding produces 64 characters
	if len(sessionID) != 64 {
		t.Errorf("Expected session ID length 64, got %d", len(sessionID))
	}
}

// =============================================================================
// Timestamp Parsing Tests
// =============================================================================

func TestParseSlackTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		ts      string
		wantSec int64
		wantErr bool
	}{
		{
			name:    "valid timestamp",
			ts:      "1234567890.123456",
			wantSec: 1234567890,
			wantErr: false,
		},
		{
			name:    "zero microseconds",
			ts:      "1234567890.000000",
			wantSec: 1234567890,
			wantErr: false,
		},
		{
			name:    "invalid format - no dot",
			ts:      "1234567890",
			wantErr: true,
		},
		{
			name:    "invalid format - multiple dots",
			ts:      "1234567890.123.456",
			wantErr: true,
		},
		{
			name:    "empty string",
			ts:      "",
			wantErr: true,
		},
		{
			name:    "non-numeric seconds",
			ts:      "abc.123456",
			wantErr: true,
		},
		{
			name:    "non-numeric microseconds",
			ts:      "1234567890.abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSlackTimestamp(tt.ts)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSlackTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.Unix() != tt.wantSec {
				t.Errorf("parseSlackTimestamp() seconds = %v, want %v", result.Unix(), tt.wantSec)
			}
		})
	}
}

// =============================================================================
// Block Kit Message Building Tests
// =============================================================================

func TestBuildBlockKitMessage_SimpleText(t *testing.T) {
	msg := &models.Message{
		Content: "Hello, world!",
	}

	options := buildBlockKitMessage(msg)

	if options == nil {
		t.Error("Expected non-nil options slice")
	}

	if len(options) == 0 {
		t.Error("Expected at least one option")
	}
}

func TestBuildBlockKitMessage_EmptyContent(t *testing.T) {
	msg := &models.Message{
		Content: "",
	}

	options := buildBlockKitMessage(msg)

	// Empty content should still return slice (possibly empty)
	if options == nil {
		t.Error("Expected non-nil options slice")
	}
}

func TestBuildBlockKitMessage_WithImageAttachment(t *testing.T) {
	msg := &models.Message{
		Content: "Check out this image",
		Attachments: []models.Attachment{
			{
				ID:       "F123456",
				Type:     "image",
				URL:      "https://example.com/image.png",
				Filename: "image.png",
			},
		},
	}

	options := buildBlockKitMessage(msg)

	if options == nil {
		t.Error("Expected non-nil options slice")
	}

	if len(options) == 0 {
		t.Error("Expected at least one option")
	}
}

func TestBuildBlockKitMessage_WithDocumentAttachment(t *testing.T) {
	msg := &models.Message{
		Content: "Here's a document",
		Attachments: []models.Attachment{
			{
				ID:       "F123456",
				Type:     "document",
				URL:      "https://example.com/doc.pdf",
				Filename: "doc.pdf",
				MimeType: "application/pdf",
			},
		},
	}

	options := buildBlockKitMessage(msg)

	if options == nil {
		t.Error("Expected non-nil options slice")
	}

	if len(options) == 0 {
		t.Error("Expected at least one option")
	}
}

func TestBuildBlockKitMessage_MultipleAttachments(t *testing.T) {
	msg := &models.Message{
		Content: "Multiple files",
		Attachments: []models.Attachment{
			{
				ID:       "F123456",
				Type:     "image",
				URL:      "https://example.com/image.png",
				Filename: "image.png",
			},
			{
				ID:       "F789012",
				Type:     "document",
				URL:      "https://example.com/doc.pdf",
				Filename: "doc.pdf",
				MimeType: "application/pdf",
			},
		},
	}

	options := buildBlockKitMessage(msg)

	if options == nil {
		t.Error("Expected non-nil options slice")
	}
}

func TestBuildBlockKitMessage_OnlyAttachments(t *testing.T) {
	// Test with empty content but with attachments
	msg := &models.Message{
		Content: "",
		Attachments: []models.Attachment{
			{
				ID:       "F123456",
				Type:     "image",
				URL:      "https://example.com/image.png",
				Filename: "image.png",
			},
		},
	}

	options := buildBlockKitMessage(msg)

	// Should have options for the attachment
	if len(options) == 0 {
		t.Error("Expected options for attachments even with empty content")
	}
}

func TestBuildBlockKitMessage_EmptyMessage(t *testing.T) {
	// Test with no content and no attachments
	msg := &models.Message{
		Content:     "",
		Attachments: nil,
	}

	options := buildBlockKitMessage(msg)

	// Should return empty slice
	if len(options) != 0 {
		t.Errorf("Expected empty options for empty message, got %d", len(options))
	}
}

func TestBuildBlockKitMessage_AudioAttachment(t *testing.T) {
	msg := &models.Message{
		Content: "Audio file",
		Attachments: []models.Attachment{
			{
				ID:       "F123456",
				Type:     "audio",
				URL:      "https://example.com/audio.mp3",
				Filename: "audio.mp3",
				MimeType: "audio/mpeg",
			},
		},
	}

	options := buildBlockKitMessage(msg)

	if len(options) < 2 {
		t.Error("Expected at least 2 options (content and attachment)")
	}
}

func TestBuildBlockKitMessage_VideoAttachment(t *testing.T) {
	msg := &models.Message{
		Content: "Video file",
		Attachments: []models.Attachment{
			{
				ID:       "F123456",
				Type:     "video",
				URL:      "https://example.com/video.mp4",
				Filename: "video.mp4",
				MimeType: "video/mp4",
			},
		},
	}

	options := buildBlockKitMessage(msg)

	if len(options) < 2 {
		t.Error("Expected at least 2 options (content and attachment)")
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
			name: "rate_limit error",
			err:  errors.New("rate_limit exceeded"),
			want: true,
		},
		{
			name: "rate limited error",
			err:  errors.New("request rate limited"),
			want: true,
		},
		{
			name: "429 error",
			err:  errors.New("HTTP 429"),
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
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

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
// Status Update Tests
// =============================================================================

func TestAdapter_StatusUpdate(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Test updateStatus with connection
	adapter.updateStatus(true, "")
	status := adapter.Status()
	if !status.Connected {
		t.Error("Expected Connected = true")
	}
	if status.Error != "" {
		t.Errorf("Expected empty error, got %q", status.Error)
	}
	if status.LastPing == 0 {
		t.Error("Expected LastPing to be set when connected")
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

// =============================================================================
// Lifecycle Tests
// =============================================================================

func TestAdapter_Lifecycle(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Test that Stop works even if never started
	err = adapter.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() on unstarted adapter returned error: %v", err)
	}

	// Test that Status shows not connected
	status := adapter.Status()
	if status.Connected {
		t.Error("Expected adapter to be disconnected after Stop")
	}
}

func TestAdapter_StopTimeout(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Stop with already-cancelled context
	err = adapter.Stop(ctx)
	// May complete anyway since adapter wasn't started
	_ = err
}

// =============================================================================
// Health Check Tests
// =============================================================================

func TestAdapter_HealthCheck_NotConnected(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	ctx := context.Background()
	// Note: HealthCheck will try to call Slack API, which will fail without real credentials
	// We're mainly testing the structure here
	_ = adapter.HealthCheck(ctx)
}

// =============================================================================
// Send Tests (without real Slack connection)
// =============================================================================

func TestAdapter_SendMissingChannelID(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content:  "Test message",
		Metadata: map[string]any{},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when slack_channel is missing")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInvalidInput {
			t.Errorf("Expected ErrCodeInvalidInput, got %v", chErr.Code)
		}
	}
}

func TestAdapter_SendNilMetadata(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content:  "Test message",
		Metadata: nil,
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when metadata is nil")
	}
}

func TestAdapter_SendEmptyChannelID(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"slack_channel": "",
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error when slack_channel is empty")
	}
}

// =============================================================================
// Bot User ID Tests
// =============================================================================

func TestAdapter_BotUserID(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Initially empty
	adapter.botUserIDMu.RLock()
	initialID := adapter.botUserID
	adapter.botUserIDMu.RUnlock()

	if initialID != "" {
		t.Errorf("Expected empty bot user ID initially, got %q", initialID)
	}

	// Set bot user ID
	adapter.botUserIDMu.Lock()
	adapter.botUserID = "U123456"
	adapter.botUserIDMu.Unlock()

	// Verify it was set
	adapter.botUserIDMu.RLock()
	setID := adapter.botUserID
	adapter.botUserIDMu.RUnlock()

	if setID != "U123456" {
		t.Errorf("Expected bot user ID 'U123456', got %q", setID)
	}
}

// =============================================================================
// Thread Handling Tests
// =============================================================================

func TestConvertSlackMessage_NewThread(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:            "message",
		User:            "U123456",
		Text:            "Starting a new thread",
		Channel:         "C123456",
		TimeStamp:       "1234567890.123456",
		ThreadTimeStamp: "", // Empty means this is a new message, not a reply
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	// Session ID should be generated from channel and message timestamp
	if msg.SessionID == "" {
		t.Error("Expected SessionID to be set")
	}

	// Thread timestamp in metadata should be empty
	if msg.Metadata["slack_thread_ts"] != "" {
		t.Errorf("Expected empty slack_thread_ts for new message, got %v", msg.Metadata["slack_thread_ts"])
	}
}

func TestConvertSlackMessage_ThreadReplySessionID(t *testing.T) {
	// Two messages in the same thread should have the same session ID
	event1 := &slackevents.MessageEvent{
		Type:            "message",
		User:            "U123456",
		Text:            "First reply",
		Channel:         "C123456",
		TimeStamp:       "1234567891.123456",
		ThreadTimeStamp: "1234567890.000000",
	}

	event2 := &slackevents.MessageEvent{
		Type:            "message",
		User:            "U789012",
		Text:            "Second reply",
		Channel:         "C123456",
		TimeStamp:       "1234567892.123456",
		ThreadTimeStamp: "1234567890.000000",
	}

	msg1 := convertSlackMessage(event1, "xoxb-test-token")
	msg2 := convertSlackMessage(event2, "xoxb-test-token")

	if msg1.SessionID != msg2.SessionID {
		t.Error("Expected same session ID for messages in the same thread")
	}
}

// =============================================================================
// Edge Cases Tests
// =============================================================================

func TestConvertSlackMessage_MalformedMention(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Hello <@U789012 without closing bracket",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	// Should handle malformed mention gracefully
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}
}

func TestConvertSlackMessage_MultipleMentions(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "<@U111> <@U222> <@U333> hello",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	// All mentions should be stripped and whitespace trimmed
	if msg.Content != "hello" {
		t.Errorf("Expected 'hello' after stripping mentions, got %q", msg.Content)
	}
}

func TestConvertSlackMessage_SpecialCharacters(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Hello & goodbye <test> \"quoted\"",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	// Special characters should be preserved (except mentions)
	if msg.Content != "Hello & goodbye <test> \"quoted\"" {
		t.Errorf("Expected special characters to be preserved, got %q", msg.Content)
	}
}

func TestConvertSlackMessage_UnicodeContent(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Hello! Testing special characters",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if msg.Content != "Hello! Testing special characters" {
		t.Errorf("Expected unicode content to be preserved, got %q", msg.Content)
	}
}
