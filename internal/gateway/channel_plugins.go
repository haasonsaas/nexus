package gateway

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/bluebubbles"
	"github.com/haasonsaas/nexus/internal/channels/discord"
	"github.com/haasonsaas/nexus/internal/channels/email"
	"github.com/haasonsaas/nexus/internal/channels/mattermost"
	"github.com/haasonsaas/nexus/internal/channels/nextcloudtalk"
	"github.com/haasonsaas/nexus/internal/channels/slack"
	"github.com/haasonsaas/nexus/internal/channels/teams"
	"github.com/haasonsaas/nexus/internal/channels/telegram"
	"github.com/haasonsaas/nexus/internal/channels/zalo"
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
	registry.Register(emailPlugin{})
	registry.Register(mattermostPlugin{})
	registry.Register(nextcloudTalkPlugin{})
	registry.Register(zaloPlugin{})
	registry.Register(blueBubblesPlugin{})
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
		Canvas: slack.CanvasConfig{
			Enabled:           cfg.Channels.Slack.Canvas.Enabled,
			Command:           cfg.Channels.Slack.Canvas.Command,
			ShortcutCallback:  cfg.Channels.Slack.Canvas.ShortcutCallback,
			AllowedWorkspaces: cfg.Channels.Slack.Canvas.AllowedWorkspaces,
			Role:              cfg.Channels.Slack.Canvas.Role,
			DefaultRole:       cfg.Channels.Slack.Canvas.DefaultRole,
			WorkspaceRoles:    cfg.Channels.Slack.Canvas.WorkspaceRoles,
			UserRoles:         cfg.Channels.Slack.Canvas.UserRoles,
		},
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

type emailPlugin struct{}

func (emailPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelEmail,
		Name: "Microsoft Graph Email",
	}
}

func (emailPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.Email.Enabled
}

func (emailPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	if cfg.Channels.Email.TenantID == "" {
		return nil, errors.New("email tenant_id is required")
	}
	if cfg.Channels.Email.ClientID == "" {
		return nil, errors.New("email client_id is required")
	}
	if cfg.Channels.Email.ClientSecret == "" {
		return nil, errors.New("email client_secret is required")
	}

	pollInterval := 30 * time.Second
	if cfg.Channels.Email.PollInterval != "" {
		if d, err := time.ParseDuration(cfg.Channels.Email.PollInterval); err == nil {
			pollInterval = d
		}
	}

	folderID := "inbox"
	if cfg.Channels.Email.FolderID != "" {
		folderID = cfg.Channels.Email.FolderID
	}

	return email.NewAdapter(email.Config{
		TenantID:     cfg.Channels.Email.TenantID,
		ClientID:     cfg.Channels.Email.ClientID,
		ClientSecret: cfg.Channels.Email.ClientSecret,
		UserEmail:    cfg.Channels.Email.UserEmail,
		FolderID:     folderID,
		IncludeRead:  cfg.Channels.Email.IncludeRead,
		AutoMarkRead: cfg.Channels.Email.AutoMarkRead,
		PollInterval: pollInterval,
		Logger:       logger,
	})
}

type mattermostPlugin struct{}

func (mattermostPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelMattermost,
		Name: "Mattermost",
	}
}

func (mattermostPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.Mattermost.Enabled
}

func (mattermostPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	return mattermost.NewAdapter(mattermost.Config{
		ServerURL: strings.TrimSpace(cfg.Channels.Mattermost.ServerURL),
		Token:     strings.TrimSpace(cfg.Channels.Mattermost.Token),
		Username:  cfg.Channels.Mattermost.Username,
		Password:  cfg.Channels.Mattermost.Password,
		TeamName:  cfg.Channels.Mattermost.TeamName,
		RateLimit: cfg.Channels.Mattermost.RateLimit,
		RateBurst: cfg.Channels.Mattermost.RateBurst,
		Logger:    logger,
	})
}

type nextcloudTalkPlugin struct{}

func (nextcloudTalkPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelNextcloudTalk,
		Name: "Nextcloud Talk",
	}
}

func (nextcloudTalkPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.NextcloudTalk.Enabled
}

func (nextcloudTalkPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	return nextcloudtalk.NewAdapter(nextcloudtalk.Config{
		BaseURL:     strings.TrimSpace(cfg.Channels.NextcloudTalk.BaseURL),
		BotSecret:   cfg.Channels.NextcloudTalk.BotSecret,
		WebhookPort: cfg.Channels.NextcloudTalk.WebhookPort,
		WebhookHost: cfg.Channels.NextcloudTalk.WebhookHost,
		WebhookPath: cfg.Channels.NextcloudTalk.WebhookPath,
		RateLimit:   cfg.Channels.NextcloudTalk.RateLimit,
		RateBurst:   cfg.Channels.NextcloudTalk.RateBurst,
		Logger:      logger,
	})
}

type zaloPlugin struct{}

func (zaloPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelZalo,
		Name: "Zalo",
	}
}

func (zaloPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.Zalo.Enabled
}

func (zaloPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	return zalo.NewZaloAdapter(zalo.ZaloConfig{
		Token:         cfg.Channels.Zalo.Token,
		WebhookURL:    strings.TrimSpace(cfg.Channels.Zalo.WebhookURL),
		WebhookSecret: cfg.Channels.Zalo.WebhookSecret,
		WebhookPath:   cfg.Channels.Zalo.WebhookPath,
		PollTimeout:   cfg.Channels.Zalo.PollTimeout,
		Logger:        logger,
	})
}

type blueBubblesPlugin struct{}

func (blueBubblesPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{
		ID:   models.ChannelBlueBubbles,
		Name: "BlueBubbles (iMessage)",
	}
}

func (blueBubblesPlugin) Enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Channels.BlueBubbles.Enabled
}

func (blueBubblesPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	timeout := time.Duration(0)
	if cfg.Channels.BlueBubbles.Timeout != "" {
		parsed, err := time.ParseDuration(cfg.Channels.BlueBubbles.Timeout)
		if err != nil {
			return nil, err
		}
		timeout = parsed
	}

	return bluebubbles.NewBlueBubblesAdapter(bluebubbles.BlueBubblesConfig{
		ServerURL:   strings.TrimSpace(cfg.Channels.BlueBubbles.ServerURL),
		Password:    cfg.Channels.BlueBubbles.Password,
		WebhookPath: cfg.Channels.BlueBubbles.WebhookPath,
		Timeout:     timeout,
		Logger:      logger,
	})
}
