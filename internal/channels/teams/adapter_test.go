package teams

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func validConfig() Config {
	return Config{
		TenantID:     "tenant-123",
		ClientID:     "client-456",
		AccessToken:  "test-token",
		PollInterval: 5 * time.Second,
		Logger:       testLogger(),
	}
}

func TestNewAdapter(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := validConfig()
		adapter, err := NewAdapter(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatal("adapter is nil")
		}
		if adapter.accessToken != "test-token" {
			t.Errorf("accessToken = %q, want %q", adapter.accessToken, "test-token")
		}
	})

	t.Run("invalid config missing tenant", func(t *testing.T) {
		cfg := Config{
			ClientID:    "client-456",
			AccessToken: "test-token",
			Logger:      testLogger(),
		}
		_, err := NewAdapter(cfg)
		if err == nil {
			t.Error("expected error for invalid config")
		}
	})

	t.Run("invalid config missing auth", func(t *testing.T) {
		cfg := Config{
			TenantID: "tenant-123",
			ClientID: "client-456",
			Logger:   testLogger(),
		}
		_, err := NewAdapter(cfg)
		if err == nil {
			t.Error("expected error for missing auth")
		}
	})
}

func TestAdapter_Type(t *testing.T) {
	cfg := validConfig()
	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := adapter.Type(); got != models.ChannelTeams {
		t.Errorf("Type() = %v, want %v", got, models.ChannelTeams)
	}
}

func TestAdapter_Messages(t *testing.T) {
	cfg := validConfig()
	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := adapter.Messages()
	if ch == nil {
		t.Error("Messages() returned nil channel")
	}
}

func TestAdapter_Status(t *testing.T) {
	cfg := validConfig()
	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initial status should be disconnected
	status := adapter.Status()
	if status.Connected {
		t.Error("initial status should be disconnected")
	}

	// Test setStatus
	adapter.setStatus(true, "")
	status = adapter.Status()
	if !status.Connected {
		t.Error("status should be connected after setStatus(true, \"\")")
	}
	if status.LastPing == 0 {
		t.Error("LastPing should be set when connected")
	}

	// Test error status
	adapter.setStatus(false, "test error")
	status = adapter.Status()
	if status.Connected {
		t.Error("status should be disconnected")
	}
	if status.Error != "test error" {
		t.Errorf("Error = %q, want %q", status.Error, "test error")
	}
}

func TestAdapter_getMode(t *testing.T) {
	t.Run("polling mode (no webhook)", func(t *testing.T) {
		cfg := validConfig()
		adapter, err := NewAdapter(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mode := adapter.getMode(); mode != "polling" {
			t.Errorf("getMode() = %q, want %q", mode, "polling")
		}
	})

	t.Run("webhook mode", func(t *testing.T) {
		cfg := validConfig()
		cfg.WebhookURL = "https://example.com/webhook"
		adapter, err := NewAdapter(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if mode := adapter.getMode(); mode != "webhook" {
			t.Errorf("getMode() = %q, want %q", mode, "webhook")
		}
	})
}

func TestAdapter_getAccessToken(t *testing.T) {
	cfg := validConfig()
	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token := adapter.getAccessToken(); token != "test-token" {
		t.Errorf("getAccessToken() = %q, want %q", token, "test-token")
	}
}

func TestAdapter_SendTypingIndicator(t *testing.T) {
	cfg := validConfig()
	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SendTypingIndicator is a no-op for Teams
	err = adapter.SendTypingIndicator(context.Background(), &models.Message{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAdapter_Metrics(t *testing.T) {
	cfg := validConfig()
	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := adapter.Metrics()
	if metrics.MessagesSent != 0 {
		t.Errorf("MessagesSent = %d, want 0", metrics.MessagesSent)
	}
	if metrics.MessagesReceived != 0 {
		t.Errorf("MessagesReceived = %d, want 0", metrics.MessagesReceived)
	}
}

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "simple tag",
			input:    "<p>Hello</p>",
			expected: "Hello",
		},
		{
			name:     "nested tags",
			input:    "<div><p>Hello</p><p>World</p></div>",
			expected: "HelloWorld",
		},
		{
			name:     "tag with attributes",
			input:    "<a href=\"https://example.com\">Link</a>",
			expected: "Link",
		},
		{
			name:     "self-closing tag",
			input:    "Hello<br/>World",
			expected: "HelloWorld",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only tags",
			input:    "<div><span></span></div>",
			expected: "",
		},
		{
			name:     "mixed content",
			input:    "Start <b>bold</b> middle <i>italic</i> end",
			expected: "Start bold middle italic end",
		},
		{
			name:     "unclosed tag",
			input:    "<p>Hello",
			expected: "Hello",
		},
		{
			name:     "HTML entities preserved",
			input:    "<p>&amp; &lt; &gt;</p>",
			expected: "&amp; &lt; &gt;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripHTMLTags(tt.input)
			if result != tt.expected {
				t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAdapter_extractContent(t *testing.T) {
	cfg := validConfig()
	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name        string
		msg         TeamsMessage
		expected    string
	}{
		{
			name: "plain text message",
			msg: TeamsMessage{
				Body: struct {
					ContentType string `json:"contentType"`
					Content     string `json:"content"`
				}{
					ContentType: "text",
					Content:     "Hello World",
				},
			},
			expected: "Hello World",
		},
		{
			name: "html message",
			msg: TeamsMessage{
				Body: struct {
					ContentType string `json:"contentType"`
					Content     string `json:"content"`
				}{
					ContentType: "html",
					Content:     "<p>Hello <b>World</b></p>",
				},
			},
			expected: "Hello World",
		},
		{
			name: "whitespace trimming",
			msg: TeamsMessage{
				Body: struct {
					ContentType string `json:"contentType"`
					Content     string `json:"content"`
				}{
					ContentType: "text",
					Content:     "  Hello World  ",
				},
			},
			expected: "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.extractContent(&tt.msg)
			if result != tt.expected {
				t.Errorf("extractContent() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestChat_Struct(t *testing.T) {
	chat := Chat{
		ID:        "chat-123",
		Topic:     "Test Chat",
		ChatType:  "group",
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	if chat.ID != "chat-123" {
		t.Errorf("ID = %q, want %q", chat.ID, "chat-123")
	}
	if chat.ChatType != "group" {
		t.Errorf("ChatType = %q, want %q", chat.ChatType, "group")
	}
}

func TestTeamsMessage_Struct(t *testing.T) {
	now := time.Now()
	msg := TeamsMessage{
		ID:              "msg-123",
		CreatedDateTime: now,
	}
	msg.Body.ContentType = "text"
	msg.Body.Content = "Hello"
	msg.From.User.ID = "user-456"
	msg.From.User.DisplayName = "Test User"

	if msg.ID != "msg-123" {
		t.Errorf("ID = %q, want %q", msg.ID, "msg-123")
	}
	if msg.Body.Content != "Hello" {
		t.Errorf("Body.Content = %q, want %q", msg.Body.Content, "Hello")
	}
	if msg.From.User.ID != "user-456" {
		t.Errorf("From.User.ID = %q, want %q", msg.From.User.ID, "user-456")
	}
}
