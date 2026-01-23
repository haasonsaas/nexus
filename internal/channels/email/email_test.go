package email

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestConfig_Validate(t *testing.T) {
	t.Run("requires tenant_id", func(t *testing.T) {
		cfg := &Config{}
		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing tenant_id")
		}
	})

	t.Run("requires client_id", func(t *testing.T) {
		cfg := &Config{TenantID: "tenant-123"}
		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing client_id")
		}
	})

	t.Run("requires client_secret or access_token", func(t *testing.T) {
		cfg := &Config{
			TenantID: "tenant-123",
			ClientID: "client-123",
		}
		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing client_secret or access_token")
		}
	})

	t.Run("accepts client_secret", func(t *testing.T) {
		cfg := &Config{
			TenantID:     "tenant-123",
			ClientID:     "client-123",
			ClientSecret: "secret-123",
		}
		err := cfg.Validate()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("accepts access_token without client_secret", func(t *testing.T) {
		cfg := &Config{
			TenantID:    "tenant-123",
			ClientID:    "client-123",
			AccessToken: "token-123",
		}
		err := cfg.Validate()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("applies defaults", func(t *testing.T) {
		cfg := &Config{
			TenantID:     "tenant-123",
			ClientID:     "client-123",
			ClientSecret: "secret-123",
		}
		err := cfg.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.PollInterval != 30*time.Second {
			t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 30*time.Second)
		}
		if cfg.MaxReconnectAttempts != 5 {
			t.Errorf("MaxReconnectAttempts = %d, want 5", cfg.MaxReconnectAttempts)
		}
		if cfg.ReconnectDelay != 5*time.Second {
			t.Errorf("ReconnectDelay = %v, want %v", cfg.ReconnectDelay, 5*time.Second)
		}
		if cfg.RateLimit != 10 {
			t.Errorf("RateLimit = %f, want 10", cfg.RateLimit)
		}
		if cfg.RateBurst != 20 {
			t.Errorf("RateBurst = %d, want 20", cfg.RateBurst)
		}
		if cfg.FolderID != "inbox" {
			t.Errorf("FolderID = %q, want %q", cfg.FolderID, "inbox")
		}
		if cfg.Logger == nil {
			t.Error("Logger should be set to default")
		}
	})

	t.Run("preserves custom values", func(t *testing.T) {
		cfg := &Config{
			TenantID:             "tenant-123",
			ClientID:             "client-123",
			ClientSecret:         "secret-123",
			PollInterval:         60 * time.Second,
			MaxReconnectAttempts: 10,
			ReconnectDelay:       10 * time.Second,
			RateLimit:            5,
			RateBurst:            10,
			FolderID:             "archive",
		}
		err := cfg.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.PollInterval != 60*time.Second {
			t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 60*time.Second)
		}
		if cfg.MaxReconnectAttempts != 10 {
			t.Errorf("MaxReconnectAttempts = %d, want 10", cfg.MaxReconnectAttempts)
		}
		if cfg.FolderID != "archive" {
			t.Errorf("FolderID = %q, want %q", cfg.FolderID, "archive")
		}
	})
}

func TestConfig_TokenEndpoint(t *testing.T) {
	cfg := &Config{TenantID: "my-tenant"}
	endpoint := cfg.TokenEndpoint()
	expected := "https://login.microsoftonline.com/my-tenant/oauth2/v2.0/token"
	if endpoint != expected {
		t.Errorf("TokenEndpoint() = %q, want %q", endpoint, expected)
	}
}

func TestConfig_AuthorizeEndpoint(t *testing.T) {
	cfg := &Config{TenantID: "my-tenant"}
	endpoint := cfg.AuthorizeEndpoint()
	expected := "https://login.microsoftonline.com/my-tenant/oauth2/v2.0/authorize"
	if endpoint != expected {
		t.Errorf("AuthorizeEndpoint() = %q, want %q", endpoint, expected)
	}
}

func TestRequiredScopes(t *testing.T) {
	scopes := RequiredScopes()
	if len(scopes) != 5 {
		t.Errorf("expected 5 scopes, got %d", len(scopes))
	}

	// Check for required scopes
	scopeMap := make(map[string]bool)
	for _, s := range scopes {
		scopeMap[s] = true
	}

	required := []string{
		"https://graph.microsoft.com/Mail.Read",
		"https://graph.microsoft.com/Mail.ReadWrite",
		"https://graph.microsoft.com/Mail.Send",
		"https://graph.microsoft.com/User.Read",
		"offline_access",
	}

	for _, r := range required {
		if !scopeMap[r] {
			t.Errorf("missing required scope: %s", r)
		}
	}
}

func TestNewAdapter(t *testing.T) {
	t.Run("returns error for invalid config", func(t *testing.T) {
		_, err := NewAdapter(Config{})
		if err == nil {
			t.Error("expected error for invalid config")
		}
	})

	t.Run("creates adapter with valid config", func(t *testing.T) {
		cfg := Config{
			TenantID:     "tenant-123",
			ClientID:     "client-123",
			ClientSecret: "secret-123",
		}
		adapter, err := NewAdapter(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatal("expected non-nil adapter")
		}
	})

	t.Run("initializes with access token", func(t *testing.T) {
		cfg := Config{
			TenantID:     "tenant-123",
			ClientID:     "client-123",
			AccessToken:  "token-123",
			RefreshToken: "refresh-123",
		}
		adapter, err := NewAdapter(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter.accessToken != "token-123" {
			t.Errorf("accessToken = %q, want %q", adapter.accessToken, "token-123")
		}
		if adapter.refreshToken != "refresh-123" {
			t.Errorf("refreshToken = %q, want %q", adapter.refreshToken, "refresh-123")
		}
	})
}

func TestAdapter_Type(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		AccessToken: "token-123",
	}
	adapter, _ := NewAdapter(cfg)

	if adapter.Type() != models.ChannelEmail {
		t.Errorf("Type() = %v, want %v", adapter.Type(), models.ChannelEmail)
	}
}

func TestAdapter_Status(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		AccessToken: "token-123",
	}
	adapter, _ := NewAdapter(cfg)

	status := adapter.Status()
	if status.Connected {
		t.Error("expected Connected = false before start")
	}
}

func TestAdapter_Messages(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		AccessToken: "token-123",
	}
	adapter, _ := NewAdapter(cfg)

	ch := adapter.Messages()
	if ch == nil {
		t.Error("Messages() should return non-nil channel")
	}
}

func TestAdapter_Metrics(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		AccessToken: "token-123",
	}
	adapter, _ := NewAdapter(cfg)

	metrics := adapter.Metrics()
	if metrics.ChannelType != models.ChannelEmail {
		t.Errorf("Metrics().ChannelType = %v, want %v", metrics.ChannelType, models.ChannelEmail)
	}
}

func TestAdapter_SendTypingIndicator(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		AccessToken: "token-123",
	}
	adapter, _ := NewAdapter(cfg)

	// SendTypingIndicator is a no-op for email
	err := adapter.SendTypingIndicator(context.Background(), &models.Message{})
	if err != nil {
		t.Errorf("SendTypingIndicator should return nil for email, got: %v", err)
	}
}

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text unchanged",
			input:    "Hello, World!",
			expected: "Hello, World!",
		},
		{
			name:     "removes simple tags",
			input:    "<p>Hello</p>",
			expected: "Hello",
		},
		{
			name:     "removes nested tags",
			input:    "<div><p><strong>Hello</strong></p></div>",
			expected: "Hello",
		},
		{
			name:     "handles attributes",
			input:    `<a href="http://example.com">Link</a>`,
			expected: "Link",
		},
		{
			name:     "handles mixed content",
			input:    "Start <b>bold</b> and <i>italic</i> end",
			expected: "Start bold and italic end",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only tags",
			input:    "<br><hr>",
			expected: "",
		},
		{
			name:     "complex HTML email",
			input:    "<!DOCTYPE html><html><head><title>Email</title></head><body><h1>Subject</h1><p>Body text here.</p></body></html>",
			expected: "EmailSubjectBody text here.",
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

func TestAdapter_setStatus(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		AccessToken: "token-123",
	}
	adapter, _ := NewAdapter(cfg)

	adapter.setStatus(true, "")
	status := adapter.Status()
	if !status.Connected {
		t.Error("expected Connected = true after setStatus(true)")
	}
	if status.Error != "" {
		t.Errorf("expected empty error, got %q", status.Error)
	}
	if status.LastPing == 0 {
		t.Error("expected LastPing to be set")
	}

	adapter.setStatus(false, "connection lost")
	status = adapter.Status()
	if status.Connected {
		t.Error("expected Connected = false after setStatus(false)")
	}
	if status.Error != "connection lost" {
		t.Errorf("Error = %q, want %q", status.Error, "connection lost")
	}
}

func TestAdapter_getAccessToken(t *testing.T) {
	cfg := Config{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		AccessToken: "initial-token",
	}
	adapter, _ := NewAdapter(cfg)

	token := adapter.getAccessToken()
	if token != "initial-token" {
		t.Errorf("getAccessToken() = %q, want %q", token, "initial-token")
	}

	// Update token directly
	adapter.tokenMu.Lock()
	adapter.accessToken = "updated-token"
	adapter.tokenMu.Unlock()

	token = adapter.getAccessToken()
	if token != "updated-token" {
		t.Errorf("getAccessToken() = %q, want %q", token, "updated-token")
	}
}

func TestEmailMessage_Struct(t *testing.T) {
	// Test that EmailMessage struct can be created and used
	msg := EmailMessage{
		ID:               "msg-123",
		ReceivedDateTime: time.Now(),
		Subject:          "Test Subject",
		IsRead:           false,
		ConversationID:   "conv-123",
		HasAttachments:   true,
	}
	msg.From.EmailAddress.Name = "John Doe"
	msg.From.EmailAddress.Address = "john@example.com"

	if msg.ID != "msg-123" {
		t.Errorf("ID = %q, want %q", msg.ID, "msg-123")
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Test Subject")
	}
	if msg.From.EmailAddress.Address != "john@example.com" {
		t.Errorf("From.EmailAddress.Address = %q, want %q", msg.From.EmailAddress.Address, "john@example.com")
	}
}

func TestAttachment_Struct(t *testing.T) {
	att := Attachment{
		ID:          "att-123",
		Name:        "document.pdf",
		ContentType: "application/pdf",
		Size:        1024,
		IsInline:    false,
	}

	if att.ID != "att-123" {
		t.Errorf("ID = %q, want %q", att.ID, "att-123")
	}
	if att.Name != "document.pdf" {
		t.Errorf("Name = %q, want %q", att.Name, "document.pdf")
	}
	if att.Size != 1024 {
		t.Errorf("Size = %d, want %d", att.Size, 1024)
	}
}
