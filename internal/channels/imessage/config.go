// Package imessage provides an iMessage channel adapter for macOS.
//go:build darwin
// +build darwin

package imessage

import (
	"fmt"
	"time"

	"github.com/haasonsaas/nexus/internal/channels/personal"
)

// Config holds iMessage adapter configuration.
type Config struct {
	// Enabled controls whether the iMessage adapter is active.
	Enabled bool `yaml:"enabled"`

	// DatabasePath is the path to the iMessage SQLite database.
	// Defaults to ~/Library/Messages/chat.db
	DatabasePath string `yaml:"database_path"`

	// Personal contains shared personal channel settings.
	Personal personal.Config `yaml:"personal"`

	// PollInterval is how often to poll for new messages.
	PollInterval string `yaml:"poll_interval"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:      false,
		DatabasePath: "~/Library/Messages/chat.db",
		PollInterval: "1s",
		Personal: personal.Config{
			SyncOnStart: true,
			Presence: personal.PresenceConfig{
				SendReadReceipts: false, // Not supported via database access
				SendTyping:       false, // Not supported via database access
			},
		},
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.DatabasePath == "" {
		return fmt.Errorf("imessage: database_path is required")
	}

	if c.PollInterval != "" {
		if _, err := time.ParseDuration(c.PollInterval); err != nil {
			return fmt.Errorf("imessage: invalid poll_interval %q: %w", c.PollInterval, err)
		}
	}

	return nil
}
