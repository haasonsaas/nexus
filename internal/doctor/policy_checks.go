package doctor

import "github.com/haasonsaas/nexus/internal/config"

// CheckChannelPolicies validates channel-specific config and returns warnings.
func CheckChannelPolicies(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var warnings []string
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.BotToken == "" {
		warnings = append(warnings, "telegram enabled but bot_token is empty")
	}
	if cfg.Channels.Discord.Enabled {
		if cfg.Channels.Discord.BotToken == "" {
			warnings = append(warnings, "discord enabled but bot_token is empty")
		}
		if cfg.Channels.Discord.AppID == "" {
			warnings = append(warnings, "discord enabled but app_id is empty")
		}
	}
	if cfg.Channels.Slack.Enabled {
		if cfg.Channels.Slack.BotToken == "" {
			warnings = append(warnings, "slack enabled but bot_token is empty")
		}
		if cfg.Channels.Slack.AppToken == "" {
			warnings = append(warnings, "slack enabled but app_token is empty")
		}
		if cfg.Channels.Slack.SigningSecret == "" {
			warnings = append(warnings, "slack enabled but signing_secret is empty")
		}
	}
	return warnings
}
