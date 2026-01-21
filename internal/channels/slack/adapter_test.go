package slack

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

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

	// Check metadata
	if msg.Metadata == nil {
		t.Fatal("Expected metadata to be non-nil")
	}

	if msg.Metadata["slack_user_id"] != "U123456" {
		t.Errorf("Expected slack_user_id U123456, got %v", msg.Metadata["slack_user_id"])
	}

	if msg.Metadata["slack_channel"] != "C123456" {
		t.Errorf("Expected slack_channel C123456, got %v", msg.Metadata["slack_channel"])
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

	// Determine attachment type based on mime type
	expectedType1 := "document"
	if att1.Type != expectedType1 {
		t.Errorf("Expected type %s, got %s", expectedType1, att1.Type)
	}

	// Check second attachment (image)
	att2 := msg.Attachments[1]
	expectedType2 := "image"
	if att2.Type != expectedType2 {
		t.Errorf("Expected type %s, got %s", expectedType2, att2.Type)
	}
}

func TestBuildBlockKitMessage_SimpleText(t *testing.T) {
	msg := &models.Message{
		Content: "Hello, world!",
	}

	options := buildBlockKitMessage(msg)

	// We can't easily inspect MsgOption functions, so we'll just verify
	// the function doesn't panic and returns a non-nil slice
	if options == nil {
		t.Error("Expected non-nil options slice")
	}

	if len(options) == 0 {
		t.Error("Expected at least one option")
	}
}

func TestBuildBlockKitMessage_WithAttachments(t *testing.T) {
	msg := &models.Message{
		Content: "Check out these files",
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

func TestGetAttachmentType(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"image/gif", "image"},
		{"audio/mpeg", "audio"},
		{"audio/wav", "audio"},
		{"video/mp4", "video"},
		{"video/quicktime", "video"},
		{"application/pdf", "document"},
		{"text/plain", "document"},
		{"", "document"},
		{"application/octet-stream", "document"},
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
}

func TestAdapter_InterfaceCompliance(t *testing.T) {
	var _ channels.Adapter = (*Adapter)(nil)
}
