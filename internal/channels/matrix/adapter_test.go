package matrix

import (
	"log/slog"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError bool
	}{
		{
			name:      "empty config",
			config:    Config{},
			wantError: true,
		},
		{
			name: "missing homeserver",
			config: Config{
				UserID:      "@bot:matrix.org",
				AccessToken: "test-token",
			},
			wantError: true,
		},
		{
			name: "missing user_id",
			config: Config{
				Homeserver:  "https://matrix.org",
				AccessToken: "test-token",
			},
			wantError: true,
		},
		{
			name: "missing access_token",
			config: Config{
				Homeserver: "https://matrix.org",
				UserID:     "@bot:matrix.org",
			},
			wantError: true,
		},
		{
			name: "valid config",
			config: Config{
				Homeserver:  "https://matrix.org",
				UserID:      "@bot:matrix.org",
				AccessToken: "test-token",
			},
			wantError: false,
		},
		{
			name: "valid config with optional fields",
			config: Config{
				Homeserver:   "https://matrix.org",
				UserID:       "@bot:matrix.org",
				AccessToken:  "test-token",
				DeviceID:     "DEVICE123",
				AllowedRooms: []string{"!room1:matrix.org"},
				AllowedUsers: []string{"@user1:matrix.org"},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigValidateDefaults(t *testing.T) {
	cfg := Config{
		Homeserver:  "https://matrix.org",
		UserID:      "@bot:matrix.org",
		AccessToken: "test-token",
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Check defaults are applied
	if cfg.SyncTimeout != 30*time.Second {
		t.Errorf("expected SyncTimeout 30s, got %v", cfg.SyncTimeout)
	}
	if cfg.MaxReconnectAttempts != 5 {
		t.Errorf("expected MaxReconnectAttempts 5, got %d", cfg.MaxReconnectAttempts)
	}
	if cfg.ReconnectBackoff != 60*time.Second {
		t.Errorf("expected ReconnectBackoff 60s, got %v", cfg.ReconnectBackoff)
	}
	if cfg.RateLimit != 5 {
		t.Errorf("expected RateLimit 5, got %f", cfg.RateLimit)
	}
	if cfg.RateBurst != 10 {
		t.Errorf("expected RateBurst 10, got %d", cfg.RateBurst)
	}
	if cfg.Logger == nil {
		t.Error("expected Logger to be set")
	}
	if !cfg.IgnoreOwnMessages {
		t.Error("expected IgnoreOwnMessages to be true")
	}
}

func TestMarkdownToHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "bold text",
			input:    "**bold**",
			expected: "<strong>bold<strong>",
		},
		{
			name:     "code block",
			input:    "```code```",
			expected: "<pre><code>code<pre><code>",
		},
		{
			name:     "mixed",
			input:    "**bold** and ```code```",
			expected: "<strong>bold<strong> and <pre><code>code<pre><code>",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := markdownToHTML(tt.input)
			if result != tt.expected {
				t.Errorf("markdownToHTML(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAdapterType(t *testing.T) {
	cfg := Config{
		Homeserver:  "https://matrix.org",
		UserID:      "@bot:matrix.org",
		AccessToken: "test-token",
		Logger:      slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if adapter.Type() != models.ChannelType("matrix") {
		t.Errorf("Type() = %v, want matrix", adapter.Type())
	}
}

func TestAdapterStatus(t *testing.T) {
	cfg := Config{
		Homeserver:  "https://matrix.org",
		UserID:      "@bot:matrix.org",
		AccessToken: "test-token",
		Logger:      slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	status := adapter.Status()

	// Before Start(), status should show not connected
	if status.Connected {
		t.Errorf("expected Connected=false before Start(), got %v", status.Connected)
	}
}

func TestAdapterChannels(t *testing.T) {
	cfg := Config{
		Homeserver:  "https://matrix.org",
		UserID:      "@bot:matrix.org",
		AccessToken: "test-token",
		Logger:      slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Messages channel should be non-nil
	msgs := adapter.Messages()
	if msgs == nil {
		t.Error("Messages() returned nil channel")
	}

	// Errors channel should be non-nil
	errs := adapter.Errors()
	if errs == nil {
		t.Error("Errors() returned nil channel")
	}
}

func TestAdapterAllowedRoomsFilter(t *testing.T) {
	cfg := Config{
		Homeserver:   "https://matrix.org",
		UserID:       "@bot:matrix.org",
		AccessToken:  "test-token",
		AllowedRooms: []string{"!allowed:matrix.org"},
		Logger:       slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Check that allowed rooms map is populated
	if adapter.allowedRooms == nil {
		t.Fatal("expected allowedRooms map to be populated")
	}
	if !adapter.allowedRooms["!allowed:matrix.org"] {
		t.Error("expected !allowed:matrix.org to be in allowedRooms")
	}
	if adapter.allowedRooms["!disallowed:matrix.org"] {
		t.Error("expected !disallowed:matrix.org to NOT be in allowedRooms")
	}
}

func TestAdapterAllowedUsersFilter(t *testing.T) {
	cfg := Config{
		Homeserver:   "https://matrix.org",
		UserID:       "@bot:matrix.org",
		AccessToken:  "test-token",
		AllowedUsers: []string{"@allowed:matrix.org"},
		Logger:       slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Check that allowed users map is populated
	if adapter.allowedUsers == nil {
		t.Fatal("expected allowedUsers map to be populated")
	}
	if !adapter.allowedUsers["@allowed:matrix.org"] {
		t.Error("expected @allowed:matrix.org to be in allowedUsers")
	}
	if adapter.allowedUsers["@disallowed:matrix.org"] {
		t.Error("expected @disallowed:matrix.org to NOT be in allowedUsers")
	}
}

func TestAdapterNoFilters(t *testing.T) {
	cfg := Config{
		Homeserver:  "https://matrix.org",
		UserID:      "@bot:matrix.org",
		AccessToken: "test-token",
		Logger:      slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Empty filter slices should result in nil maps (allow all)
	if adapter.allowedRooms != nil {
		t.Error("expected allowedRooms to be nil (allow all)")
	}
	if adapter.allowedUsers != nil {
		t.Error("expected allowedUsers to be nil (allow all)")
	}
}

func TestAdapterStopNotStarted(t *testing.T) {
	cfg := Config{
		Homeserver:  "https://matrix.org",
		UserID:      "@bot:matrix.org",
		AccessToken: "test-token",
		Logger:      slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Stop on a non-started adapter should be a no-op
	err = adapter.Stop(nil)
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}
}

func TestAdapterStartStopIdempotent(t *testing.T) {
	cfg := Config{
		Homeserver:  "https://matrix.org",
		UserID:      "@bot:matrix.org",
		AccessToken: "test-token",
		Logger:      slog.Default(),
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Multiple stops should be safe
	for i := 0; i < 3; i++ {
		err = adapter.Stop(nil)
		if err != nil {
			t.Errorf("Stop() iteration %d error = %v", i, err)
		}
	}
}
