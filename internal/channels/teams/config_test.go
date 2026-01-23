package teams

import (
	"strings"
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with client secret",
			config: Config{
				TenantID:     "tenant-123",
				ClientID:     "client-456",
				ClientSecret: "secret-789",
			},
			wantErr: false,
		},
		{
			name: "valid config with access token",
			config: Config{
				TenantID:    "tenant-123",
				ClientID:    "client-456",
				AccessToken: "token-abc",
			},
			wantErr: false,
		},
		{
			name: "missing tenant ID",
			config: Config{
				ClientID:     "client-456",
				ClientSecret: "secret-789",
			},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name: "missing client ID",
			config: Config{
				TenantID:     "tenant-123",
				ClientSecret: "secret-789",
			},
			wantErr: true,
			errMsg:  "client_id is required",
		},
		{
			name: "missing both secret and token",
			config: Config{
				TenantID: "tenant-123",
				ClientID: "client-456",
			},
			wantErr: true,
			errMsg:  "client_secret or access_token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate_Defaults(t *testing.T) {
	cfg := Config{
		TenantID:     "tenant-123",
		ClientID:     "client-456",
		ClientSecret: "secret-789",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check defaults are applied
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 5*time.Second)
	}
	if cfg.MaxReconnectAttempts != 5 {
		t.Errorf("MaxReconnectAttempts = %d, want 5", cfg.MaxReconnectAttempts)
	}
	if cfg.ReconnectDelay != 5*time.Second {
		t.Errorf("ReconnectDelay = %v, want %v", cfg.ReconnectDelay, 5*time.Second)
	}
	if cfg.RateLimit != 10 {
		t.Errorf("RateLimit = %v, want 10", cfg.RateLimit)
	}
	if cfg.RateBurst != 20 {
		t.Errorf("RateBurst = %d, want 20", cfg.RateBurst)
	}
	if cfg.Logger == nil {
		t.Error("Logger should be set to default")
	}
}

func TestConfig_Validate_PreservesCustomValues(t *testing.T) {
	cfg := Config{
		TenantID:             "tenant-123",
		ClientID:             "client-456",
		ClientSecret:         "secret-789",
		PollInterval:         10 * time.Second,
		MaxReconnectAttempts: 10,
		ReconnectDelay:       30 * time.Second,
		RateLimit:            50,
		RateBurst:            100,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Custom values should be preserved
	if cfg.PollInterval != 10*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 10*time.Second)
	}
	if cfg.MaxReconnectAttempts != 10 {
		t.Errorf("MaxReconnectAttempts = %d, want 10", cfg.MaxReconnectAttempts)
	}
	if cfg.ReconnectDelay != 30*time.Second {
		t.Errorf("ReconnectDelay = %v, want %v", cfg.ReconnectDelay, 30*time.Second)
	}
	if cfg.RateLimit != 50 {
		t.Errorf("RateLimit = %v, want 50", cfg.RateLimit)
	}
	if cfg.RateBurst != 100 {
		t.Errorf("RateBurst = %d, want 100", cfg.RateBurst)
	}
}

func TestConfig_TokenEndpoint(t *testing.T) {
	cfg := Config{TenantID: "test-tenant-id"}
	expected := "https://login.microsoftonline.com/test-tenant-id/oauth2/v2.0/token"

	if got := cfg.TokenEndpoint(); got != expected {
		t.Errorf("TokenEndpoint() = %q, want %q", got, expected)
	}
}

func TestConfig_AuthorizeEndpoint(t *testing.T) {
	cfg := Config{TenantID: "test-tenant-id"}
	expected := "https://login.microsoftonline.com/test-tenant-id/oauth2/v2.0/authorize"

	if got := cfg.AuthorizeEndpoint(); got != expected {
		t.Errorf("AuthorizeEndpoint() = %q, want %q", got, expected)
	}
}

func TestRequiredScopes(t *testing.T) {
	scopes := RequiredScopes()

	expectedScopes := []string{
		"https://graph.microsoft.com/Chat.ReadWrite",
		"https://graph.microsoft.com/ChatMessage.Send",
		"https://graph.microsoft.com/ChannelMessage.Send",
		"https://graph.microsoft.com/User.Read",
		"offline_access",
	}

	if len(scopes) != len(expectedScopes) {
		t.Errorf("len(scopes) = %d, want %d", len(scopes), len(expectedScopes))
	}

	for i, expected := range expectedScopes {
		if i < len(scopes) && scopes[i] != expected {
			t.Errorf("scopes[%d] = %q, want %q", i, scopes[i], expected)
		}
	}
}
