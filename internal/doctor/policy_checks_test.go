package doctor

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestCheckChannelPolicies(t *testing.T) {
	cfg := &config.Config{
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramConfig{Enabled: true},
			Discord:  config.DiscordConfig{Enabled: true},
			Slack:    config.SlackConfig{Enabled: true},
		},
	}
	warnings := CheckChannelPolicies(cfg)
	if len(warnings) < 3 {
		t.Fatalf("expected warnings, got %d", len(warnings))
	}
}
