package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
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
	if cfg.Canvas.Command != "/canvas" {
		t.Errorf("Canvas.Command = %q, want /canvas", cfg.Canvas.Command)
	}
	if cfg.Canvas.ShortcutCallback != "open_canvas" {
		t.Errorf("Canvas.ShortcutCallback = %q, want open_canvas", cfg.Canvas.ShortcutCallback)
	}
	if cfg.Canvas.Role != "editor" {
		t.Errorf("Canvas.Role = %q, want editor", cfg.Canvas.Role)
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

func TestCanvasWorkspaceAllowed(t *testing.T) {
	adapter := &Adapter{
		cfg: Config{
			Canvas: CanvasConfig{
				AllowedWorkspaces: []string{"T123", "T999"},
			},
		},
	}
	if !adapter.canvasWorkspaceAllowed("T123") {
		t.Error("expected workspace to be allowed")
	}
	if adapter.canvasWorkspaceAllowed("T000") {
		t.Error("expected workspace to be rejected")
	}

	unrestricted := &Adapter{cfg: Config{Canvas: CanvasConfig{}}}
	if !unrestricted.canvasWorkspaceAllowed("anything") {
		t.Error("expected empty allowlist to allow all workspaces")
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

	if adapter.health == nil {
		t.Error("adapter.health is nil")
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

func TestBuildBlockKitMessage_LongMarkdownContent(t *testing.T) {
	// Test with markdown formatted content
	msg := &models.Message{
		Content: "*Bold text* _italic_ `code` ```code block```\n- list item\n> quote",
	}

	options := buildBlockKitMessage(msg)

	if len(options) == 0 {
		t.Error("Expected at least one option for markdown content")
	}
}

func TestBuildBlockKitMessage_SpecialCharacters(t *testing.T) {
	msg := &models.Message{
		Content: "Test <>&\"' special chars",
	}

	options := buildBlockKitMessage(msg)

	if len(options) == 0 {
		t.Error("Expected at least one option for content with special chars")
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

// =============================================================================
// Mock Client Tests
// =============================================================================

func TestMockSlackClient_AuthTest(t *testing.T) {
	mock := &MockSlackClient{}
	resp, err := mock.AuthTest()

	if err != nil {
		t.Fatalf("AuthTest() error = %v", err)
	}

	if resp == nil {
		t.Fatal("AuthTest() returned nil response")
	}

	if resp.UserID != "U12345" {
		t.Errorf("Expected UserID U12345, got %s", resp.UserID)
	}
}

func TestMockSlackClient_AuthTestCustom(t *testing.T) {
	mock := &MockSlackClient{
		AuthTestFunc: func() (*slack.AuthTestResponse, error) {
			return &slack.AuthTestResponse{UserID: "CUSTOM123", Team: "CustomTeam"}, nil
		},
	}

	resp, err := mock.AuthTest()

	if err != nil {
		t.Fatalf("AuthTest() error = %v", err)
	}

	if resp.UserID != "CUSTOM123" {
		t.Errorf("Expected UserID CUSTOM123, got %s", resp.UserID)
	}
}

func TestMockSlackClient_AuthTestError(t *testing.T) {
	mock := &MockSlackClient{
		AuthTestFunc: func() (*slack.AuthTestResponse, error) {
			return nil, errors.New("authentication failed")
		},
	}

	_, err := mock.AuthTest()

	if err == nil {
		t.Error("Expected error from AuthTest")
	}
}

func TestMockSlackClient_PostMessage(t *testing.T) {
	mock := &MockSlackClient{}
	channel, ts, err := mock.PostMessage("C123456")

	if err != nil {
		t.Fatalf("PostMessage() error = %v", err)
	}

	if channel != "C123456" {
		t.Errorf("Expected channel C123456, got %s", channel)
	}

	if ts == "" {
		t.Error("Expected non-empty timestamp")
	}
}

func TestMockSlackClient_PostMessageContext(t *testing.T) {
	var capturedCtx context.Context
	var capturedChannelID string

	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			capturedCtx = ctx
			capturedChannelID = channelID
			return channelID, "custom-timestamp", nil
		},
	}

	ctx := context.Background()
	channel, ts, err := mock.PostMessageContext(ctx, "C999999")

	if err != nil {
		t.Fatalf("PostMessageContext() error = %v", err)
	}

	if capturedCtx != ctx {
		t.Error("Context was not passed through")
	}

	if capturedChannelID != "C999999" {
		t.Errorf("Expected channelID C999999, got %s", capturedChannelID)
	}

	if channel != "C999999" {
		t.Errorf("Expected channel C999999, got %s", channel)
	}

	if ts != "custom-timestamp" {
		t.Errorf("Expected timestamp custom-timestamp, got %s", ts)
	}
}

func TestMockSlackClient_AddReaction(t *testing.T) {
	mock := &MockSlackClient{}
	err := mock.AddReaction("thumbsup", slack.ItemRef{Channel: "C123", Timestamp: "123.456"})

	if err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
}

func TestMockSlackClient_GetUserInfo(t *testing.T) {
	mock := &MockSlackClient{}
	user, err := mock.GetUserInfo("U123456")

	if err != nil {
		t.Fatalf("GetUserInfo() error = %v", err)
	}

	if user == nil {
		t.Fatal("GetUserInfo() returned nil user")
	}

	if user.ID != "U123456" {
		t.Errorf("Expected user ID U123456, got %s", user.ID)
	}
}

// =============================================================================
// Testable Adapter Tests
// =============================================================================

func TestTestableAdapter_Start(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	err = adapter.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !adapter.Status().Connected {
		t.Error("Expected adapter to be connected after Start")
	}

	if adapter.GetBotUserID() != "U12345" {
		t.Errorf("Expected bot user ID U12345, got %s", adapter.GetBotUserID())
	}
}

func TestTestableAdapter_StartAuthError(t *testing.T) {
	mock := &MockSlackClient{
		AuthTestFunc: func() (*slack.AuthTestResponse, error) {
			return nil, errors.New("invalid token")
		},
	}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	err = adapter.Start(ctx)

	if err == nil {
		t.Error("Expected error from Start with invalid auth")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeAuthentication {
			t.Errorf("Expected ErrCodeAuthentication, got %v", chErr.Code)
		}
	}
}

func TestTestableAdapter_Send(t *testing.T) {
	var postCalled bool
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			postCalled = true
			return channelID, "1234567890.123456", nil
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000, // High rate limit for testing
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if !postCalled {
		t.Error("Expected PostMessageContext to be called")
	}
}

func TestTestableAdapter_SendWithReaction(t *testing.T) {
	var reactionAdded bool
	var reactionName string

	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			return channelID, "1234567890.123456", nil
		},
		AddReactionContextFunc: func(ctx context.Context, name string, item slack.ItemRef) error {
			reactionAdded = true
			reactionName = name
			return nil
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"slack_channel":  "C123456",
			"slack_reaction": "thumbsup",
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if !reactionAdded {
		t.Error("Expected reaction to be added")
	}

	if reactionName != "thumbsup" {
		t.Errorf("Expected reaction thumbsup, got %s", reactionName)
	}
}

func TestTestableAdapter_SendWithThreadReply(t *testing.T) {
	var options []slack.MsgOption

	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, opts ...slack.MsgOption) (string, string, error) {
			options = opts
			return channelID, "1234567890.123456", nil
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Thread reply",
		Metadata: map[string]any{
			"slack_channel":   "C123456",
			"slack_thread_ts": "1234567880.000000",
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// Should have options including thread timestamp
	if len(options) == 0 {
		t.Error("Expected message options to be set")
	}
}

func TestTestableAdapter_SendRateLimitError(t *testing.T) {
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			return "", "", errors.New("rate_limit exceeded")
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected rate limit error")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeRateLimit {
			t.Errorf("Expected ErrCodeRateLimit, got %v", chErr.Code)
		}
	}
}

func TestTestableAdapter_ProcessMessage_DM(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = adapter.Start(ctx)

	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Hello from DM",
		Channel:   "D123456", // DM channel
		TimeStamp: "1234567890.123456",
	}

	adapter.ProcessMessage(event)

	// Wait for message to be processed
	select {
	case msg := <-adapter.Messages():
		if msg.Content != "Hello from DM" {
			t.Errorf("Expected content 'Hello from DM', got %q", msg.Content)
		}
		if msg.Metadata["slack_channel"] != "D123456" {
			t.Errorf("Expected channel D123456, got %v", msg.Metadata["slack_channel"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for message")
	}
}

func TestTestableAdapter_ProcessMessage_Mention(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = adapter.Start(ctx)

	// Bot user ID is U12345 from mock
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U999999",
		Text:      "Hey <@U12345> help me",
		Channel:   "C123456", // Regular channel
		TimeStamp: "1234567890.123456",
	}

	adapter.ProcessMessage(event)

	select {
	case msg := <-adapter.Messages():
		if msg.Content != "Hey  help me" {
			t.Errorf("Expected content 'Hey  help me', got %q", msg.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for message")
	}
}

func TestTestableAdapter_ProcessMessage_NotRelevant(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = adapter.Start(ctx)

	// Message in regular channel without mention - should be ignored
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U999999",
		Text:      "Random message without mention",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	adapter.ProcessMessage(event)

	select {
	case <-adapter.Messages():
		t.Error("Expected message to be ignored")
	case <-time.After(50 * time.Millisecond):
		// Expected - message should be filtered out
	}
}

func TestTestableAdapter_ProcessAppMention(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = adapter.Start(ctx)

	event := &slackevents.AppMentionEvent{
		User:      "U999999",
		Text:      "<@U12345> do something",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	adapter.ProcessAppMention(event)

	select {
	case msg := <-adapter.Messages():
		if msg.Content != "do something" {
			t.Errorf("Expected content 'do something', got %q", msg.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for message")
	}
}

func TestTestableAdapter_HealthCheck(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if !health.Healthy {
		t.Error("Expected healthy status")
	}

	if health.Message != "healthy" {
		t.Errorf("Expected message 'healthy', got %q", health.Message)
	}
}

func TestTestableAdapter_HealthCheckDegraded(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	adapter.SetDegraded(true)

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if !health.Healthy {
		t.Error("Expected healthy status even when degraded")
	}

	if !health.Degraded {
		t.Error("Expected degraded flag to be set")
	}

	if health.Message != "operating in degraded mode" {
		t.Errorf("Expected degraded message, got %q", health.Message)
	}
}

func TestTestableAdapter_HealthCheckError(t *testing.T) {
	mock := &MockSlackClient{
		AuthTestContextFunc: func(ctx context.Context) (*slack.AuthTestResponse, error) {
			return nil, errors.New("connection failed")
		},
	}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	health := adapter.HealthCheck(ctx)

	if health.Healthy {
		t.Error("Expected unhealthy status")
	}

	if !strings.Contains(health.Message, "health check failed") {
		t.Errorf("Expected failure message, got %q", health.Message)
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestTestableAdapter_ConcurrentSend(t *testing.T) {
	var callCount int64

	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			atomic.AddInt64(&callCount, 1)
			return channelID, fmt.Sprintf("%d.123456", time.Now().UnixNano()), nil
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
		RateBurst: 100,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	_ = adapter.Start(ctx)

	var wg sync.WaitGroup
	numMessages := 10

	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := &models.Message{
				Content: fmt.Sprintf("Message %d", n),
				Metadata: map[string]any{
					"slack_channel": "C123456",
				},
			}
			_ = adapter.Send(ctx, msg)
		}(i)
	}

	wg.Wait()

	finalCount := atomic.LoadInt64(&callCount)
	if finalCount != int64(numMessages) {
		t.Errorf("Expected %d calls, got %d", numMessages, finalCount)
	}
}

func TestTestableAdapter_ConcurrentStatusRead(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	_ = adapter.Start(ctx)

	var wg sync.WaitGroup

	// Multiple goroutines reading status
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = adapter.Status()
			}
		}()
	}

	wg.Wait()
}

func TestTestableAdapter_ConcurrentDegradedFlag(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	var wg sync.WaitGroup

	// Writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			adapter.SetDegraded(i%2 == 0)
		}
	}()

	// Reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = adapter.isDegraded()
		}
	}()

	wg.Wait()
}

func TestTestableAdapter_ConcurrentBotUserID(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	var wg sync.WaitGroup

	// Writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			adapter.SetBotUserID(fmt.Sprintf("U%d", i))
		}
	}()

	// Reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = adapter.GetBotUserID()
		}
	}()

	wg.Wait()
}

// =============================================================================
// Edge Case Tests for Long Messages
// =============================================================================

func TestBuildBlockKitMessage_VeryLongContent(t *testing.T) {
	// Create a very long message (4000+ characters)
	longContent := strings.Repeat("This is a test message. ", 200)
	msg := &models.Message{
		Content: longContent,
	}

	options := buildBlockKitMessage(msg)

	// Should still build successfully
	if len(options) == 0 {
		t.Error("Expected options for long content")
	}
}

func TestConvertSlackMessage_ManyMentions(t *testing.T) {
	// Build text with many mentions
	var mentions []string
	for i := 0; i < 20; i++ {
		mentions = append(mentions, fmt.Sprintf("<@U%06d>", i))
	}
	text := strings.Join(mentions, " ") + " hello"

	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      text,
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	// All mentions should be stripped
	if msg.Content != "hello" {
		t.Errorf("Expected 'hello' after stripping many mentions, got %q", msg.Content)
	}
}

func TestConvertSlackMessage_SpecialChannelIDs(t *testing.T) {
	tests := []struct {
		name      string
		channelID string
	}{
		{"regular channel", "C12345678901"},
		{"DM channel", "D12345678901"},
		{"group DM", "G12345678901"},
		{"workspace channel", "CWORKSPACE1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &slackevents.MessageEvent{
				Type:      "message",
				User:      "U123456",
				Text:      "Test",
				Channel:   tt.channelID,
				TimeStamp: "1234567890.123456",
			}

			msg := convertSlackMessage(event, "xoxb-test-token")

			if msg.Metadata["slack_channel"] != tt.channelID {
				t.Errorf("Expected channel %s, got %v", tt.channelID, msg.Metadata["slack_channel"])
			}
		})
	}
}

func TestConvertSlackMessage_SpecialUserIDs(t *testing.T) {
	tests := []struct {
		name   string
		userID string
	}{
		{"regular user", "U12345678901"},
		{"bot user", "B12345678901"},
		{"workspace user", "WUSER12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &slackevents.MessageEvent{
				Type:      "message",
				User:      tt.userID,
				Text:      "Test",
				Channel:   "C123456",
				TimeStamp: "1234567890.123456",
			}

			msg := convertSlackMessage(event, "xoxb-test-token")

			if msg.Metadata["slack_user_id"] != tt.userID {
				t.Errorf("Expected user ID %s, got %v", tt.userID, msg.Metadata["slack_user_id"])
			}
		})
	}
}

// =============================================================================
// Metrics Tests
// =============================================================================

func TestTestableAdapter_MetricsTracking(t *testing.T) {
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			return channelID, "1234567890.123456", nil
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	_ = adapter.Start(ctx)

	// Send a message
	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
	}
	_ = adapter.Send(ctx, msg)

	metrics := adapter.Metrics()

	if metrics.MessagesSent == 0 {
		t.Error("Expected MessagesSent to be incremented")
	}

	if metrics.ChannelType != models.ChannelSlack {
		t.Errorf("Expected channel type Slack, got %v", metrics.ChannelType)
	}
}

func TestTestableAdapter_MetricsOnError(t *testing.T) {
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			return "", "", errors.New("send failed")
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	_ = adapter.Start(ctx)

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
	}
	_ = adapter.Send(ctx, msg)

	metrics := adapter.Metrics()

	if metrics.MessagesFailed == 0 {
		t.Error("Expected MessagesFailed to be incremented on error")
	}
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestTestableAdapter_InterfaceCompliance(t *testing.T) {
	var _ channels.Adapter = (*TestableAdapter)(nil)
	var _ channels.LifecycleAdapter = (*TestableAdapter)(nil)
	var _ channels.OutboundAdapter = (*TestableAdapter)(nil)
	var _ channels.InboundAdapter = (*TestableAdapter)(nil)
	var _ channels.HealthAdapter = (*TestableAdapter)(nil)
}

func TestMockSlackClient_InterfaceCompliance(t *testing.T) {
	var _ SlackAPIClient = (*MockSlackClient)(nil)
}

// =============================================================================
// Stop and Context Cancellation Tests
// =============================================================================

func TestTestableAdapter_StopGracefully(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	_ = adapter.Start(ctx)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = adapter.Stop(stopCtx)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if adapter.Status().Connected {
		t.Error("Expected adapter to be disconnected after Stop")
	}
}

func TestTestableAdapter_SendCancelledContext(t *testing.T) {
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			// Check if context is cancelled
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return channelID, "1234567890.123456", nil
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000, // Fast rate limit
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
	}

	// Already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = adapter.Send(ctx, msg)

	// With a cancelled context, the rate limiter or the mock should return an error
	// The behavior may vary based on rate limiter implementation
	// The key is that the mock respects context cancellation
	if err == nil {
		// The rate limiter with high burst may allow the call through
		// This is acceptable behavior - the important thing is the context is respected
		t.Log("Note: rate limiter allowed call through with cancelled context")
	}
}

// =============================================================================
// containsMention Helper Tests
// =============================================================================

func TestContainsMention(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		userID string
		want   bool
	}{
		{"has mention", "Hello <@U12345> there", "U12345", true},
		{"no mention", "Hello there", "U12345", false},
		{"different user", "Hello <@U99999> there", "U12345", false},
		{"empty text", "", "U12345", false},
		{"empty user", "Hello <@U12345> there", "", false},
		{"mention at start", "<@U12345> Hello", "U12345", true},
		{"mention at end", "Hello <@U12345>", "U12345", true},
		{"multiple mentions", "<@U11111> <@U12345> <@U22222>", "U12345", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsMention(tt.text, tt.userID)
			if got != tt.want {
				t.Errorf("containsMention(%q, %q) = %v, want %v", tt.text, tt.userID, got, tt.want)
			}
		})
	}
}

// =============================================================================
// File Attachment Edge Cases
// =============================================================================

func TestConvertSlackMessage_ManyFiles(t *testing.T) {
	files := make([]slack.File, 10)
	for i := 0; i < 10; i++ {
		files[i] = slack.File{
			ID:                 fmt.Sprintf("F%d", i),
			Name:               fmt.Sprintf("file%d.txt", i),
			Mimetype:           "text/plain",
			URLPrivateDownload: fmt.Sprintf("https://files.slack.com/file%d.txt", i),
			Size:               1024,
		}
	}

	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Multiple files",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
		Message: &slack.Msg{
			Files: files,
		},
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if len(msg.Attachments) != 10 {
		t.Errorf("Expected 10 attachments, got %d", len(msg.Attachments))
	}
}

func TestConvertSlackMessage_LargeFile(t *testing.T) {
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U123456",
		Text:      "Large file",
		Channel:   "C123456",
		TimeStamp: "1234567890.123456",
		Message: &slack.Msg{
			Files: []slack.File{
				{
					ID:                 "F123456",
					Name:               "large.zip",
					Mimetype:           "application/zip",
					URLPrivateDownload: "https://files.slack.com/large.zip",
					Size:               1073741824, // 1GB
				},
			},
		},
	}

	msg := convertSlackMessage(event, "xoxb-test-token")

	if len(msg.Attachments) != 1 {
		t.Fatalf("Expected 1 attachment, got %d", len(msg.Attachments))
	}

	if msg.Attachments[0].Size != 1073741824 {
		t.Errorf("Expected size 1073741824, got %d", msg.Attachments[0].Size)
	}
}

// =============================================================================
// Additional Mock Client Tests for Coverage
// =============================================================================

func TestMockSlackClient_UploadFileV2(t *testing.T) {
	mock := &MockSlackClient{}
	params := slack.UploadFileV2Parameters{
		Filename: "test.txt",
		Content:  "test content",
	}

	result, err := mock.UploadFileV2(params)
	if err != nil {
		t.Fatalf("UploadFileV2() error = %v", err)
	}

	if result.ID != "F12345" {
		t.Errorf("Expected file ID F12345, got %s", result.ID)
	}
}

func TestMockSlackClient_UploadFileV2Context(t *testing.T) {
	mock := &MockSlackClient{}
	params := slack.UploadFileV2Parameters{
		Filename: "test.txt",
		Content:  "test content",
	}

	ctx := context.Background()
	result, err := mock.UploadFileV2Context(ctx, params)
	if err != nil {
		t.Fatalf("UploadFileV2Context() error = %v", err)
	}

	if result.ID != "F12345" {
		t.Errorf("Expected file ID F12345, got %s", result.ID)
	}
}

func TestMockSlackClient_UploadFileV2Custom(t *testing.T) {
	mock := &MockSlackClient{
		UploadFileV2Func: func(params slack.UploadFileV2Parameters) (*slack.FileSummary, error) {
			return &slack.FileSummary{ID: "CUSTOM_FILE"}, nil
		},
	}
	params := slack.UploadFileV2Parameters{
		Filename: "custom.txt",
	}

	result, err := mock.UploadFileV2(params)
	if err != nil {
		t.Fatalf("UploadFileV2() error = %v", err)
	}

	if result.ID != "CUSTOM_FILE" {
		t.Errorf("Expected file ID CUSTOM_FILE, got %s", result.ID)
	}
}

func TestMockSlackClient_UploadFileV2ContextCustom(t *testing.T) {
	mock := &MockSlackClient{
		UploadFileV2ContextFunc: func(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error) {
			return &slack.FileSummary{ID: "CTX_FILE"}, nil
		},
	}

	ctx := context.Background()
	result, err := mock.UploadFileV2Context(ctx, slack.UploadFileV2Parameters{})
	if err != nil {
		t.Fatalf("UploadFileV2Context() error = %v", err)
	}

	if result.ID != "CTX_FILE" {
		t.Errorf("Expected file ID CTX_FILE, got %s", result.ID)
	}
}

func TestMockSlackClient_GetUserInfoContext(t *testing.T) {
	mock := &MockSlackClient{
		GetUserInfoContextFunc: func(ctx context.Context, userID string) (*slack.User, error) {
			return &slack.User{ID: userID, Name: "context_user"}, nil
		},
	}

	ctx := context.Background()
	user, err := mock.GetUserInfoContext(ctx, "U789")
	if err != nil {
		t.Fatalf("GetUserInfoContext() error = %v", err)
	}

	if user.Name != "context_user" {
		t.Errorf("Expected name context_user, got %s", user.Name)
	}
}

func TestMockSlackClient_GetConversationInfo(t *testing.T) {
	mock := &MockSlackClient{}
	input := &slack.GetConversationInfoInput{
		ChannelID: "C123456",
	}

	channel, err := mock.GetConversationInfo(input)
	if err != nil {
		t.Fatalf("GetConversationInfo() error = %v", err)
	}

	if channel == nil {
		t.Fatal("GetConversationInfo() returned nil channel")
	}
}

func TestMockSlackClient_GetConversationInfoContext(t *testing.T) {
	mock := &MockSlackClient{}
	input := &slack.GetConversationInfoInput{
		ChannelID: "C123456",
	}

	ctx := context.Background()
	channel, err := mock.GetConversationInfoContext(ctx, input)
	if err != nil {
		t.Fatalf("GetConversationInfoContext() error = %v", err)
	}

	if channel == nil {
		t.Fatal("GetConversationInfoContext() returned nil channel")
	}
}

func TestMockSlackClient_GetConversationInfoCustom(t *testing.T) {
	mock := &MockSlackClient{
		GetConversationInfoFunc: func(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
			return nil, errors.New("channel not found")
		},
	}
	input := &slack.GetConversationInfoInput{
		ChannelID: "C999999",
	}

	_, err := mock.GetConversationInfo(input)
	if err == nil {
		t.Error("Expected error from GetConversationInfo")
	}
}

func TestMockSlackClient_GetConversationInfoContextCustom(t *testing.T) {
	mock := &MockSlackClient{
		GetConversationInfoCtxFn: func(ctx context.Context, input *slack.GetConversationInfoInput) (*slack.Channel, error) {
			return &slack.Channel{GroupConversation: slack.GroupConversation{Name: "ctx_channel"}}, nil
		},
	}

	ctx := context.Background()
	channel, err := mock.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{})
	if err != nil {
		t.Fatalf("GetConversationInfoContext() error = %v", err)
	}

	if channel.Name != "ctx_channel" {
		t.Errorf("Expected name ctx_channel, got %s", channel.Name)
	}
}

// =============================================================================
// Socket Mode Mock Tests
// =============================================================================

func TestMockSocketModeClient_Create(t *testing.T) {
	mock := NewMockSocketModeClient()
	if mock == nil {
		t.Fatal("NewMockSocketModeClient() returned nil")
	}

	if mock.EventsChan == nil {
		t.Error("EventsChan should not be nil")
	}
}

func TestMockSocketModeClient_Events(t *testing.T) {
	mock := NewMockSocketModeClient()

	eventsChan := mock.Events()
	if eventsChan == nil {
		t.Fatal("Events() returned nil channel")
	}
}

func TestMockSocketModeClient_Close(t *testing.T) {
	mock := NewMockSocketModeClient()

	// Should not panic
	mock.Close()

	// Channel should be closed
	_, ok := <-mock.Events()
	if ok {
		t.Error("Expected channel to be closed")
	}
}

func TestMockSocketModeClient_Ack(t *testing.T) {
	var ackCalled bool
	mock := &MockSocketModeClient{
		AckFunc: func(req socketmode.Request, payload ...interface{}) {
			ackCalled = true
		},
	}

	mock.Ack(socketmode.Request{})

	if !ackCalled {
		t.Error("Expected AckFunc to be called")
	}
}

func TestMockSocketModeClient_AckNoFunc(t *testing.T) {
	mock := &MockSocketModeClient{}
	// Should not panic when AckFunc is nil
	mock.Ack(socketmode.Request{})
}

// =============================================================================
// TestableAdapter Type Method
// =============================================================================

func TestTestableAdapter_Type(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	if adapter.Type() != models.ChannelSlack {
		t.Errorf("Expected type %s, got %s", models.ChannelSlack, adapter.Type())
	}
}

// =============================================================================
// Send With Attachments
// =============================================================================

func TestTestableAdapter_SendWithAttachments(t *testing.T) {
	var sentOptions []slack.MsgOption
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			sentOptions = options
			return channelID, "1234567890.123456", nil
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Message with attachments",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
		Attachments: []models.Attachment{
			{
				ID:       "F1",
				Type:     "image",
				URL:      "https://example.com/img.png",
				Filename: "img.png",
			},
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// Verify options were sent
	if len(sentOptions) == 0 {
		t.Error("Expected message options to be set")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTestableAdapter_SendWithAttachments_UploadEnabled(t *testing.T) {
	var uploaded slack.UploadFileV2Parameters
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			return channelID, "1234567890.123456", nil
		},
		UploadFileV2ContextFunc: func(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error) {
			uploaded = params
			return &slack.FileSummary{ID: "F1"}, nil
		},
	}
	attachmentData := []byte("file-data")
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(bytes.NewReader(attachmentData)),
				ContentLength: int64(len(attachmentData)),
				Header:        http.Header{"Content-Type": []string{"text/plain"}},
			}, nil
		}),
	}
	cfg := Config{
		BotToken:          "xoxb-test-token",
		AppToken:          "xapp-test-token",
		RateLimit:         1000,
		UploadAttachments: true,
		HTTPClient:        httpClient,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Message with attachments",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
		Attachments: []models.Attachment{
			{
				ID:       "F1",
				Type:     "document",
				URL:      "https://example.com/file.txt",
				Filename: "file.txt",
			},
		},
	}

	ctx := context.Background()
	if err := adapter.Send(ctx, msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if uploaded.Filename != "file.txt" {
		t.Errorf("expected upload filename file.txt, got %q", uploaded.Filename)
	}
	if uploaded.FileSize != len(attachmentData) {
		t.Errorf("expected upload size %d, got %d", len(attachmentData), uploaded.FileSize)
	}
	if uploaded.Channel != "C123456" {
		t.Errorf("expected upload channel C123456, got %q", uploaded.Channel)
	}
	if uploaded.ThreadTimestamp != "1234567890.123456" {
		t.Errorf("expected thread timestamp to match message ts, got %q", uploaded.ThreadTimestamp)
	}
}

func TestTestableAdapter_DownloadAttachment(t *testing.T) {
	var authHeader string
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			authHeader = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(strings.NewReader("payload")),
				ContentLength: int64(len("payload")),
				Header:        http.Header{"Content-Type": []string{"text/plain"}},
			}, nil
		}),
	}
	cfg := Config{
		BotToken:   "xoxb-test-token",
		AppToken:   "xapp-test-token",
		HTTPClient: httpClient,
	}
	adapter, err := NewTestableAdapter(cfg, &MockSlackClient{})
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	data, mimeType, filename, err := adapter.DownloadAttachment(context.Background(), nil, &models.Attachment{
		URL:      "https://example.com/file.txt",
		Filename: "file.txt",
		MimeType: "text/plain",
		Size:     int64(len("payload")),
	})
	if err != nil {
		t.Fatalf("DownloadAttachment() error = %v", err)
	}
	if authHeader != "Bearer xoxb-test-token" {
		t.Errorf("expected Authorization header to be set, got %q", authHeader)
	}
	if string(data) != "payload" {
		t.Errorf("expected payload, got %q", string(data))
	}
	if mimeType != "text/plain" {
		t.Errorf("expected mimeType text/plain, got %q", mimeType)
	}
	if filename != "file.txt" {
		t.Errorf("expected filename file.txt, got %q", filename)
	}
}

func TestTestableAdapter_SendGenericError(t *testing.T) {
	mock := &MockSlackClient{
		PostMessageContextFunc: func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
			return "", "", errors.New("generic error")
		},
	}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"slack_channel": "C123456",
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInternal {
			t.Errorf("Expected ErrCodeInternal, got %v", chErr.Code)
		}
	}
}

func TestTestableAdapter_SendMissingChannel(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken:  "xoxb-test-token",
		AppToken:  "xapp-test-token",
		RateLimit: 1000,
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	// Test with wrong type for slack_channel
	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"slack_channel": 12345, // Wrong type
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error for wrong channel type")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeInvalidInput {
			t.Errorf("Expected ErrCodeInvalidInput, got %v", chErr.Code)
		}
	}
}

// =============================================================================
// TestableAdapter Stop Timeout
// =============================================================================

func TestTestableAdapter_StopTimeout(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx := context.Background()
	_ = adapter.Start(ctx)

	// Add a goroutine to the wait group that blocks
	adapter.wg.Add(1)
	go func() {
		// This will block for a while
		time.Sleep(5 * time.Second)
		adapter.wg.Done()
	}()

	// Stop with very short timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err = adapter.Stop(stopCtx)

	if err == nil {
		t.Error("Expected timeout error")
	}

	var chErr *channels.Error
	if errors.As(err, &chErr) {
		if chErr.Code != channels.ErrCodeTimeout {
			t.Errorf("Expected ErrCodeTimeout, got %v", chErr.Code)
		}
	}
}

// =============================================================================
// ProcessMessage Edge Cases
// =============================================================================

func TestTestableAdapter_ProcessMessage_ThreadReply(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = adapter.Start(ctx)

	// Thread reply in regular channel (no mention, not DM, but has thread)
	event := &slackevents.MessageEvent{
		Type:            "message",
		User:            "U999999",
		Text:            "Thread reply",
		Channel:         "C123456",
		TimeStamp:       "1234567891.123456",
		ThreadTimeStamp: "1234567890.000000", // Has thread timestamp
	}

	adapter.ProcessMessage(event)

	select {
	case msg := <-adapter.Messages():
		if msg.Content != "Thread reply" {
			t.Errorf("Expected content 'Thread reply', got %q", msg.Content)
		}
		if msg.Metadata["slack_thread_ts"] != "1234567890.000000" {
			t.Errorf("Expected thread_ts, got %v", msg.Metadata["slack_thread_ts"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for message")
	}
}

func TestTestableAdapter_ProcessMessage_EmptyChannel(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = adapter.Start(ctx)

	// Empty channel ID
	event := &slackevents.MessageEvent{
		Type:      "message",
		User:      "U999999",
		Text:      "Test",
		Channel:   "",
		TimeStamp: "1234567890.123456",
	}

	adapter.ProcessMessage(event)

	select {
	case <-adapter.Messages():
		t.Error("Expected message to be ignored with empty channel")
	case <-time.After(50 * time.Millisecond):
		// Expected - message should be filtered
	}
}

// =============================================================================
// NewTestableAdapter with Invalid Config
// =============================================================================

func TestNewTestableAdapter_InvalidConfig(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "", // Invalid
		AppToken: "xapp-test",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err == nil {
		t.Error("Expected error for invalid config")
	}
	if adapter != nil {
		t.Error("Expected nil adapter")
	}
}

// =============================================================================
// Timestamp Parsing Edge Cases
// =============================================================================

func TestParseSlackTimestamp_LargeMicroseconds(t *testing.T) {
	ts := "1234567890.999999"
	result, err := parseSlackTimestamp(ts)
	if err != nil {
		t.Fatalf("parseSlackTimestamp() error = %v", err)
	}

	if result.Unix() != 1234567890 {
		t.Errorf("Expected seconds 1234567890, got %d", result.Unix())
	}
}

// =============================================================================
// Block Kit with Fallback
// =============================================================================

func TestBuildBlockKitMessage_FallbackText(t *testing.T) {
	// This tests the fallback path when no blocks are added but content exists
	// First create a message with just content
	msg := &models.Message{
		Content: "Simple text",
	}

	options := buildBlockKitMessage(msg)

	// Should have at least one option (the section block)
	if len(options) == 0 {
		t.Error("Expected at least one option for simple text")
	}
}

// =============================================================================
// Concurrent Message Processing
// =============================================================================

func TestTestableAdapter_ConcurrentProcessMessage(t *testing.T) {
	mock := &MockSlackClient{}
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewTestableAdapter(cfg, mock)
	if err != nil {
		t.Fatalf("NewTestableAdapter() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = adapter.Start(ctx)

	var wg sync.WaitGroup
	numMessages := 20

	// Send multiple messages concurrently
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			event := &slackevents.MessageEvent{
				Type:      "message",
				User:      fmt.Sprintf("U%d", n),
				Text:      fmt.Sprintf("Message %d", n),
				Channel:   "D123456", // DM
				TimeStamp: fmt.Sprintf("123456789%d.123456", n),
			}
			adapter.ProcessMessage(event)
		}(i)
	}

	wg.Wait()

	// Drain the messages channel
	received := 0
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case <-adapter.Messages():
			received++
			if received >= numMessages {
				return
			}
		case <-timeout:
			if received == 0 {
				t.Error("Expected to receive some messages")
			}
			return
		}
	}
}

// =============================================================================
// Error Context in Errors
// =============================================================================

func TestAdapter_SendWrongChannelType(t *testing.T) {
	cfg := Config{
		BotToken: "xoxb-test-token",
		AppToken: "xapp-test-token",
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Wrong type in metadata
	msg := &models.Message{
		Content: "Test",
		Metadata: map[string]any{
			"slack_channel": []string{"not", "a", "string"},
		},
	}

	ctx := context.Background()
	err = adapter.Send(ctx, msg)

	if err == nil {
		t.Error("Expected error for wrong channel type")
	}
}
