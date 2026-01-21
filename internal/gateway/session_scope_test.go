package gateway

import (
	"io"
	"log/slog"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestResolveConversationIDSlackScopeChannel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Session: config.SessionConfig{
			SlackScope: "channel",
		},
	}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	msg := &models.Message{
		Channel: models.ChannelSlack,
		Metadata: map[string]any{
			"slack_channel":   "C123",
			"slack_thread_ts": "1700000000.0001",
		},
	}

	id, err := server.resolveConversationID(msg)
	if err != nil {
		t.Fatalf("resolveConversationID() error = %v", err)
	}
	if id != "C123" {
		t.Fatalf("expected channel scope id C123, got %q", id)
	}
}

func TestResolveConversationIDSlackScopeThread(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Session: config.SessionConfig{
			SlackScope: "thread",
		},
	}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	msg := &models.Message{
		Channel: models.ChannelSlack,
		Metadata: map[string]any{
			"slack_channel":   "C123",
			"slack_thread_ts": "1700000000.0001",
		},
	}

	id, err := server.resolveConversationID(msg)
	if err != nil {
		t.Fatalf("resolveConversationID() error = %v", err)
	}
	if id != "C123:1700000000.0001" {
		t.Fatalf("expected thread scope id C123:1700000000.0001, got %q", id)
	}
}

func TestResolveConversationIDDiscordScopeChannel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Session: config.SessionConfig{
			DiscordScope: "channel",
		},
	}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	msg := &models.Message{
		Channel: models.ChannelDiscord,
		Metadata: map[string]any{
			"discord_channel_id": "chan-1",
			"discord_thread_id":  "thread-1",
		},
	}

	id, err := server.resolveConversationID(msg)
	if err != nil {
		t.Fatalf("resolveConversationID() error = %v", err)
	}
	if id != "chan-1" {
		t.Fatalf("expected channel scope id chan-1, got %q", id)
	}
}

func TestResolveConversationIDDiscordScopeThread(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Session: config.SessionConfig{
			DiscordScope: "thread",
		},
	}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	msg := &models.Message{
		Channel: models.ChannelDiscord,
		Metadata: map[string]any{
			"discord_channel_id": "chan-1",
			"discord_thread_id":  "thread-1",
		},
	}

	id, err := server.resolveConversationID(msg)
	if err != nil {
		t.Fatalf("resolveConversationID() error = %v", err)
	}
	if id != "thread-1" {
		t.Fatalf("expected thread scope id thread-1, got %q", id)
	}
}
