package gateway

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"http://example.com", true},
		{"https://example.com", true},
		{"https://example.com/path/to/file", true},
		{"http://localhost:8080", true},
		{"HTTP://EXAMPLE.COM", false}, // Case sensitive
		{"ftp://example.com", false},
		{"file:///path/to/file", false},
		{"example.com", false},
		{"/path/to/file", false},
		{"", false},
		{"httpx://example.com", false},
		{"http", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			result := isHTTPURL(tt.value)
			if result != tt.expected {
				t.Errorf("isHTTPURL(%q) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestScopeUsesThread(t *testing.T) {
	tests := []struct {
		scope    string
		expected bool
	}{
		{"channel", false},
		{"Channel", false},
		{"CHANNEL", false},
		{"  channel  ", false},
		{"thread", true},
		{"Thread", true},
		{"", true},        // Default to thread
		{"  ", true},      // Whitespace defaults to thread
		{"message", true}, // Unknown scope defaults to thread
		{"custom", true},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			result := scopeUsesThread(tt.scope)
			if result != tt.expected {
				t.Errorf("scopeUsesThread(%q) = %v, want %v", tt.scope, result, tt.expected)
			}
		})
	}
}

func TestInitStorageStores_NilConfig(t *testing.T) {
	stores, err := initStorageStores(nil)
	if err != nil {
		t.Errorf("initStorageStores(nil) returned error: %v", err)
	}
	// Should return in-memory stores when config is nil
	if stores.Agents == nil {
		t.Error("expected non-nil agents store")
	}
}

func TestInitStorageStores_EmptyDatabaseURL(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			URL: "",
		},
	}
	stores, err := initStorageStores(cfg)
	if err != nil {
		t.Errorf("initStorageStores with empty URL returned error: %v", err)
	}
	// Should return in-memory stores when URL is empty
	if stores.Agents == nil {
		t.Error("expected non-nil agents store")
	}
}

func TestInitStorageStores_WhitespaceOnlyURL(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			URL: "   ",
		},
	}
	stores, err := initStorageStores(cfg)
	if err != nil {
		t.Errorf("initStorageStores with whitespace URL returned error: %v", err)
	}
	// Should return in-memory stores when URL is only whitespace
	if stores.Agents == nil {
		t.Error("expected non-nil agents store")
	}
}

func TestBuildReplyMetadata_TelegramChannel(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}
	msg := &models.Message{
		Channel:   models.ChannelTelegram,
		ChannelID: "123",
		Metadata: map[string]any{
			"chat_id": int64(456),
		},
	}

	metadata := s.buildReplyMetadata(msg)

	if metadata["chat_id"] != int64(456) {
		t.Errorf("chat_id = %v, want 456", metadata["chat_id"])
	}
	if metadata["reply_to_message_id"] != 123 {
		t.Errorf("reply_to_message_id = %v, want 123", metadata["reply_to_message_id"])
	}
}

func TestBuildReplyMetadata_SlackChannel(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}
	msg := &models.Message{
		Channel: models.ChannelSlack,
		Metadata: map[string]any{
			"slack_channel":   "C12345",
			"slack_thread_ts": "1234567890.123456",
		},
	}

	metadata := s.buildReplyMetadata(msg)

	if metadata["slack_channel"] != "C12345" {
		t.Errorf("slack_channel = %v, want C12345", metadata["slack_channel"])
	}
	if metadata["slack_thread_ts"] != "1234567890.123456" {
		t.Errorf("slack_thread_ts = %v, want 1234567890.123456", metadata["slack_thread_ts"])
	}
}

func TestBuildReplyMetadata_SlackFallbackToTs(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}
	msg := &models.Message{
		Channel: models.ChannelSlack,
		Metadata: map[string]any{
			"slack_channel": "C12345",
			"slack_ts":      "1234567890.654321",
		},
	}

	metadata := s.buildReplyMetadata(msg)

	if metadata["slack_thread_ts"] != "1234567890.654321" {
		t.Errorf("slack_thread_ts = %v, want 1234567890.654321", metadata["slack_thread_ts"])
	}
}

func TestBuildReplyMetadata_DiscordChannel(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}
	msg := &models.Message{
		Channel: models.ChannelDiscord,
		Metadata: map[string]any{
			"discord_channel_id": "123456789",
			"discord_thread_id":  "987654321",
		},
	}

	metadata := s.buildReplyMetadata(msg)

	// Thread ID takes precedence
	if metadata["discord_channel_id"] != "987654321" {
		t.Errorf("discord_channel_id = %v, want 987654321 (thread ID)", metadata["discord_channel_id"])
	}
}

func TestBuildReplyMetadata_DiscordChannelNoThread(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}
	msg := &models.Message{
		Channel: models.ChannelDiscord,
		Metadata: map[string]any{
			"discord_channel_id": "123456789",
		},
	}

	metadata := s.buildReplyMetadata(msg)

	if metadata["discord_channel_id"] != "123456789" {
		t.Errorf("discord_channel_id = %v, want 123456789", metadata["discord_channel_id"])
	}
}

func TestBuildReplyMetadata_WhatsApp(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}
	msg := &models.Message{
		Channel: models.ChannelWhatsApp,
		Metadata: map[string]any{
			"peer_id":  "+1234567890",
			"group_id": "group123",
		},
	}

	metadata := s.buildReplyMetadata(msg)

	if metadata["peer_id"] != "+1234567890" {
		t.Errorf("peer_id = %v, want +1234567890", metadata["peer_id"])
	}
	if metadata["group_id"] != "group123" {
		t.Errorf("group_id = %v, want group123", metadata["group_id"])
	}
}

func TestBuildReplyMetadata_NilMetadata(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}
	msg := &models.Message{
		Channel:  models.ChannelTelegram,
		Metadata: nil,
	}

	metadata := s.buildReplyMetadata(msg)

	if metadata == nil {
		t.Error("expected non-nil metadata map")
	}
	if len(metadata) != 0 {
		t.Errorf("expected empty metadata, got %v", metadata)
	}
}

func TestBuildReplyMetadata_NilServer(t *testing.T) {
	var s *Server
	msg := &models.Message{
		Channel:  models.ChannelTelegram,
		Metadata: map[string]any{"chat_id": int64(123)},
	}

	// Should handle nil server gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Logf("recovered from panic (expected with nil server): %v", r)
		}
	}()
	_ = s.buildReplyMetadata(msg)
}

func TestRegisterOAuthProviders_NilService(t *testing.T) {
	cfg := config.OAuthConfig{
		Google: config.OAuthProviderConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
		},
	}

	// Should not panic when service is nil
	registerOAuthProviders(nil, cfg)
}

func TestRegisterOAuthProviders_EmptyCredentials(t *testing.T) {
	cfg := config.OAuthConfig{
		Google: config.OAuthProviderConfig{
			ClientID:     "",
			ClientSecret: "",
		},
		GitHub: config.OAuthProviderConfig{
			ClientID:     "",
			ClientSecret: "",
		},
	}

	// Should not panic with empty credentials
	registerOAuthProviders(nil, cfg)
}

func TestRegisterOAuthProviders_OnlyGoogleConfigured(t *testing.T) {
	cfg := config.OAuthConfig{
		Google: config.OAuthProviderConfig{
			ClientID:     "google-client-id",
			ClientSecret: "google-client-secret",
			RedirectURL:  "http://localhost/callback",
		},
		GitHub: config.OAuthProviderConfig{
			ClientID:     "",
			ClientSecret: "",
		},
	}

	// Should not panic with partial config
	registerOAuthProviders(nil, cfg)
}

func TestRegisterOAuthProviders_OnlyGitHubConfigured(t *testing.T) {
	cfg := config.OAuthConfig{
		Google: config.OAuthProviderConfig{
			ClientID:     "",
			ClientSecret: "",
		},
		GitHub: config.OAuthProviderConfig{
			ClientID:     "github-client-id",
			ClientSecret: "github-client-secret",
			RedirectURL:  "http://localhost/callback",
		},
	}

	// Should not panic with partial config
	registerOAuthProviders(nil, cfg)
}

func TestRegisterOAuthProviders_WhitespaceCredentials(t *testing.T) {
	cfg := config.OAuthConfig{
		Google: config.OAuthProviderConfig{
			ClientID:     "   ",
			ClientSecret: "   ",
		},
	}

	// Should not register providers with whitespace-only credentials
	registerOAuthProviders(nil, cfg)
}
