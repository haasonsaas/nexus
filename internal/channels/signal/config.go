// Package signal provides a Signal channel adapter using signal-cli.
package signal

import (
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
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:       false,
		SignalCLIPath: "signal-cli",
		ConfigDir:     "~/.config/signal-cli",
		Personal: personal.Config{
			SyncOnStart: true,
			Presence: personal.PresenceConfig{
				SendReadReceipts: true,
				SendTyping:       true,
			},
		},
	}
}
