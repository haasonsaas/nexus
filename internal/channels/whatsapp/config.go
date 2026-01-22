// Package whatsapp provides a WhatsApp channel adapter using whatsmeow.
package whatsapp

import (
	"fmt"

	"github.com/haasonsaas/nexus/internal/channels/personal"
)

// Config holds WhatsApp adapter configuration.
type Config struct {
	// Enabled controls whether the WhatsApp adapter is active.
	Enabled bool `yaml:"enabled"`

	// SessionPath is the path to the SQLite database for session persistence.
	SessionPath string `yaml:"session_path"`

	// MediaPath is the directory for downloaded/uploaded media.
	MediaPath string `yaml:"media_path"`

	// SyncContacts controls whether to sync contacts on startup.
	SyncContacts bool `yaml:"sync_contacts"`

	// Personal contains shared personal channel settings.
	Personal personal.Config `yaml:"personal"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:      false,
		SessionPath:  "~/.nexus/whatsapp/session.db",
		MediaPath:    "~/.nexus/whatsapp/media",
		SyncContacts: true,
		Personal: personal.Config{
			SyncOnStart: true,
			Presence: personal.PresenceConfig{
				SendReadReceipts: true,
				SendTyping:       true,
				BroadcastOnline:  false,
			},
		},
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.SessionPath == "" {
		return fmt.Errorf("whatsapp: session_path is required")
	}

	return nil
}
