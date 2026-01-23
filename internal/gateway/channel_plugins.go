package gateway

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/discord"
	"github.com/haasonsaas/nexus/internal/channels/slack"
	"github.com/haasonsaas/nexus/internal/channels/teams"
	"github.com/haasonsaas/nexus/internal/channels/telegram"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
	"log/slog"
)

type ChannelPluginManifest struct {
	ID          models.ChannelType
	Name        string
	Description string
	Version     string
}

type ChannelPlugin interface {
	Manifest() ChannelPluginManifest
	Enabled(cfg *config.Config) bool
	Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error)
}

type channelPluginEntry struct {
	plugin  ChannelPlugin
	once    sync.Once
	adapter channels.Adapter
	err     error
}

func (e *channelPluginEntry) Load(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	e.once.Do(func() {
		e.adapter, e.err = e.plugin.Build(cfg, logger)
	})
	return e.adapter, e.err
}

type channelPluginRegistry struct {
	plugins map[models.ChannelType]*channelPluginEntry
}

func newChannelPluginRegistry() *channelPluginRegistry {
	return &channelPluginRegistry{
		plugins: make(map[models.ChannelType]*channelPluginEntry),
	}
}

func (r *channelPluginRegistry) Register(plugin ChannelPlugin) {
	manifest := plugin.Manifest()
	r.plugins[manifest.ID] = &channelPluginEntry{plugin: plugin}
}

func (r *channelPluginRegistry) LoadEnabled(cfg *config.Config, registry *channels.Registry, logger *slog.Logger) error {
	for _, entry := range r.plugins {
		if !entry.plugin.Enabled(cfg) {
			continue
		}
		adapter, err := entry.Load(cfg, logger)
		if err != nil {
			return err
		}
		registry.Register(adapter)
	}
	return nil
}

func registerBuiltinChannelPlugins(registry *channelPluginRegistry) {
	registry.Register(telegramPlugin{})
	registry.Register(discordPlugin{})
	registry.Register(slackPlugin{})
	registry.Register(teamsPlugin{})
}

type telegramPlugin struct{}

func (telegramPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelTelegram,
		Name: "Telegram",
	}
}

func (telegramPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.Telegram.Enabled
}

func (telegramPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	if cfg.Channels.Telegram.BotToken == "" {
		return nil, errors.New("telegram bot token is required")
	}
	mode := telegram.ModeLongPolling
	webhookURL := strings.TrimSpace(cfg.Channels.Telegram.Webhook)
	if webhookURL != "" {
		mode = telegram.ModeWebhook
	}
	return telegram.NewAdapter(telegram.Config{
		Token:      cfg.Channels.Telegram.BotToken,
		Mode:       mode,
		WebhookURL: webhookURL,
		Logger:     logger,
	})
}

type discordPlugin struct{}

func (discordPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelDiscord,
		Name: "Discord",
	}
}

func (discordPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.Discord.Enabled
}

func (discordPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	if cfg.Channels.Discord.BotToken == "" {
		return nil, errors.New("discord bot token is required")
	}
	return discord.NewAdapter(discord.Config{
		Token:  cfg.Channels.Discord.BotToken,
		Logger: logger,
	})
}

type slackPlugin struct{}

func (slackPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelSlack,
		Name: "Slack",
	}
}

func (slackPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.Slack.Enabled
}

func (slackPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	if cfg.Channels.Slack.BotToken == "" || cfg.Channels.Slack.AppToken == "" {
		return nil, errors.New("slack bot token and app token are required")
	}
	return slack.NewAdapter(slack.Config{
		BotToken: cfg.Channels.Slack.BotToken,
		AppToken: cfg.Channels.Slack.AppToken,
		Logger:   logger,
	})
}

type teamsPlugin struct{}

func (teamsPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelTeams,
		Name: "Microsoft Teams",
	}
}

func (teamsPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.Teams.Enabled
}

func (teamsPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	if cfg.Channels.Teams.TenantID == "" {
		return nil, errors.New("teams tenant_id is required")
	}
	if cfg.Channels.Teams.ClientID == "" {
		return nil, errors.New("teams client_id is required")
	}
	if cfg.Channels.Teams.ClientSecret == "" {
		return nil, errors.New("teams client_secret is required")
	}

	pollInterval := 5 * time.Second
	if cfg.Channels.Teams.PollInterval != "" {
		if d, err := time.ParseDuration(cfg.Channels.Teams.PollInterval); err == nil {
			pollInterval = d
		}
	}

	return teams.NewAdapter(teams.Config{
		TenantID:     cfg.Channels.Teams.TenantID,
		ClientID:     cfg.Channels.Teams.ClientID,
		ClientSecret: cfg.Channels.Teams.ClientSecret,
		WebhookURL:   strings.TrimSpace(cfg.Channels.Teams.WebhookURL),
		PollInterval: pollInterval,
		Logger:       logger,
	})
}
