package email

import (
	"log/slog"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
)

// Config holds configuration for the Microsoft Graph Email adapter.
type Config struct {
	// TenantID is the Azure AD tenant ID (required)
	TenantID string

	// ClientID is the Azure AD application (client) ID (required)
	ClientID string

	// ClientSecret is the Azure AD application secret (required for app-only auth)
	ClientSecret string

	// AccessToken is an optional pre-configured access token
	// If provided, ClientSecret is not required
	AccessToken string

	// RefreshToken is used to obtain new access tokens
	RefreshToken string

	// UserEmail is the email address to monitor (required for app auth)
	// For delegated auth, uses the authenticated user's mailbox
	UserEmail string

	// PollInterval is the interval for polling new messages
	PollInterval time.Duration

	// MaxReconnectAttempts is the maximum number of reconnection attempts
	MaxReconnectAttempts int

	// ReconnectDelay is the delay between reconnection attempts
	ReconnectDelay time.Duration

	// RateLimit configures rate limiting for API calls (operations per second)
	RateLimit float64

	// RateBurst configures the burst capacity for rate limiting
	RateBurst int

	// FolderID specifies which folder to monitor (defaults to "inbox")
	FolderID string

	// IncludeRead determines whether to process already-read messages
	IncludeRead bool

	// AutoMarkRead marks messages as read after processing
	AutoMarkRead bool

	// Logger is an optional slog.Logger instance
	Logger *slog.Logger
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.TenantID == "" {
		return channels.ErrConfig("tenant_id is required", nil)
	}

	if c.ClientID == "" {
		return channels.ErrConfig("client_id is required", nil)
	}

	if c.ClientSecret == "" && c.AccessToken == "" {
		return channels.ErrConfig("client_secret or access_token is required", nil)
	}

	if c.PollInterval == 0 {
		c.PollInterval = 30 * time.Second
	}

	if c.MaxReconnectAttempts == 0 {
		c.MaxReconnectAttempts = 5
	}

	if c.ReconnectDelay == 0 {
		c.ReconnectDelay = 5 * time.Second
	}

	// Microsoft Graph general rate limit
	if c.RateLimit == 0 {
		c.RateLimit = 10
	}

	if c.RateBurst == 0 {
		c.RateBurst = 20
	}

	if c.FolderID == "" {
		c.FolderID = "inbox"
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	return nil
}

// TokenEndpoint returns the OAuth2 token endpoint for this tenant.
func (c *Config) TokenEndpoint() string {
	return "https://login.microsoftonline.com/" + c.TenantID + "/oauth2/v2.0/token"
}

// AuthorizeEndpoint returns the OAuth2 authorize endpoint for this tenant.
func (c *Config) AuthorizeEndpoint() string {
	return "https://login.microsoftonline.com/" + c.TenantID + "/oauth2/v2.0/authorize"
}

// RequiredScopes returns the Microsoft Graph API scopes needed for Email integration.
func RequiredScopes() []string {
	return []string{
		"https://graph.microsoft.com/Mail.Read",
		"https://graph.microsoft.com/Mail.ReadWrite",
		"https://graph.microsoft.com/Mail.Send",
		"https://graph.microsoft.com/User.Read",
		"offline_access",
	}
}
