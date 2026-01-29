// Package signal provides a Signal channel adapter using signal-cli.
package signal

import (
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
)

// Config holds Signal adapter configuration.
type Config struct {
	// Enabled controls whether the Signal adapter is active.
	Enabled bool `yaml:"enabled"`

	// Account is the phone number for the Signal account (e.g., +1234567890).
	Account string `yaml:"account"`

	// SignalCLIPath is the path to the signal-cli binary.
	SignalCLIPath string `yaml:"signal_cli_path"`

	// ConfigDir is the directory for signal-cli configuration.
	ConfigDir string `yaml:"config_dir"`

	// Personal contains shared personal channel settings.
	Personal personal.Config `yaml:"personal"`

	// AttachmentMaxAge controls how long cached attachments are retained.
	// Use a duration string like "168h". Leave empty to disable pruning.
	AttachmentMaxAge string `yaml:"attachment_max_age"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:          false,
		SignalCLIPath:    "signal-cli",
		ConfigDir:        "~/.config/signal-cli",
		AttachmentMaxAge: "168h",
		Personal: personal.Config{
			SyncOnStart: true,
			Presence: personal.PresenceConfig{
				SendReadReceipts: true,
				SendTyping:       true,
			},
		},
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Account == "" {
		return channels.ErrConfig("signal: account (phone number) is required", nil)
	}

	if c.SignalCLIPath == "" {
		return channels.ErrConfig("signal: signal_cli_path is required", nil)
	}

	if c.AttachmentMaxAge != "" {
		if _, err := time.ParseDuration(c.AttachmentMaxAge); err != nil {
			return channels.ErrConfig("signal: invalid attachment_max_age", err)
		}
	}

	return nil
}
