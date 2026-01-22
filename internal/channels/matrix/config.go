package matrix

import (
	"log/slog"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
)

// Config holds configuration for the Matrix adapter.
type Config struct {
	// Homeserver is the Matrix homeserver URL (required)
	Homeserver string

	// UserID is the bot's Matrix user ID (e.g., @bot:matrix.org) (required)
	UserID string

	// AccessToken is the access token for authentication (required)
	AccessToken string

	// DeviceID is the device ID for this client session
	DeviceID string

	// AllowedRooms limits which rooms the bot will respond in (empty = all)
	AllowedRooms []string

	// AllowedUsers limits which users can interact (empty = all)
	AllowedUsers []string

	// IgnoreOwnMessages ignores messages from the bot itself
	IgnoreOwnMessages bool

	// JoinOnInvite automatically joins rooms when invited
	JoinOnInvite bool

	// SyncTimeout is the timeout for sync requests
	SyncTimeout time.Duration

	// MaxReconnectAttempts is the maximum reconnection attempts
	MaxReconnectAttempts int

	// ReconnectBackoff is the maximum backoff duration
	ReconnectBackoff time.Duration

	// RateLimit configures rate limiting (messages per second)
	RateLimit float64

	// RateBurst configures burst capacity
	RateBurst int

	// Logger is an optional logger instance
	Logger *slog.Logger
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.Homeserver == "" {
		return channels.ErrConfig("homeserver is required", nil)
	}

	if c.UserID == "" {
		return channels.ErrConfig("user_id is required", nil)
	}

	if c.AccessToken == "" {
		return channels.ErrConfig("access_token is required", nil)
	}

	// Apply defaults
	if c.SyncTimeout == 0 {
		c.SyncTimeout = 30 * time.Second
	}

	if c.MaxReconnectAttempts == 0 {
		c.MaxReconnectAttempts = 5
	}

	if c.ReconnectBackoff == 0 {
		c.ReconnectBackoff = 60 * time.Second
	}

	if c.RateLimit == 0 {
		c.RateLimit = 5
	}

	if c.RateBurst == 0 {
		c.RateBurst = 10
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	// Default to ignoring own messages
	c.IgnoreOwnMessages = true

	return nil
}
