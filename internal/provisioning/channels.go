// Package provisioning provides utilities for managing Nexus configuration.
package provisioning

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
	"gopkg.in/yaml.v3"
)

// ChannelProvisioner manages channel configuration.
type ChannelProvisioner struct {
	configPath string
	logger     *slog.Logger
}

// NewChannelProvisioner creates a channel provisioner.
func NewChannelProvisioner(configPath string, logger *slog.Logger) *ChannelProvisioner {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChannelProvisioner{
		configPath: configPath,
		logger:     logger,
	}
}

// ChannelInfo describes a configured channel.
type ChannelInfo struct {
	Type      models.ChannelType
	Enabled   bool
	HasToken  bool
	TokenHint string // Last 4 chars of token for identification
}

// ListChannels returns info about all configured channels.
func (p *ChannelProvisioner) ListChannels(ctx context.Context) ([]ChannelInfo, error) {
	cfg, err := config.Load(p.configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	var channels []ChannelInfo

	// Telegram
	if cfg.Channels.Telegram.BotToken != "" || cfg.Channels.Telegram.Enabled {
		channels = append(channels, ChannelInfo{
			Type:      models.ChannelTelegram,
			Enabled:   cfg.Channels.Telegram.Enabled,
			HasToken:  cfg.Channels.Telegram.BotToken != "",
			TokenHint: tokenHint(cfg.Channels.Telegram.BotToken),
		})
	}

	// Discord
	if cfg.Channels.Discord.BotToken != "" || cfg.Channels.Discord.Enabled {
		channels = append(channels, ChannelInfo{
			Type:      models.ChannelDiscord,
			Enabled:   cfg.Channels.Discord.Enabled,
			HasToken:  cfg.Channels.Discord.BotToken != "",
			TokenHint: tokenHint(cfg.Channels.Discord.BotToken),
		})
	}

	// Slack
	if cfg.Channels.Slack.BotToken != "" || cfg.Channels.Slack.Enabled {
		channels = append(channels, ChannelInfo{
			Type:      models.ChannelSlack,
			Enabled:   cfg.Channels.Slack.Enabled,
			HasToken:  cfg.Channels.Slack.BotToken != "",
			TokenHint: tokenHint(cfg.Channels.Slack.BotToken),
		})
	}

	return channels, nil
}

// ValidateChannel validates a channel's configuration.
func (p *ChannelProvisioner) ValidateChannel(ctx context.Context, channelType models.ChannelType) error {
	cfg, err := config.Load(p.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	switch channelType {
	case models.ChannelTelegram:
		if cfg.Channels.Telegram.BotToken == "" {
			return fmt.Errorf("telegram: bot_token is required")
		}
		if !strings.Contains(cfg.Channels.Telegram.BotToken, ":") {
			return fmt.Errorf("telegram: token format invalid (expected BOT_ID:TOKEN)")
		}
	case models.ChannelDiscord:
		if cfg.Channels.Discord.BotToken == "" {
			return fmt.Errorf("discord: bot_token is required")
		}
	case models.ChannelSlack:
		if cfg.Channels.Slack.BotToken == "" {
			return fmt.Errorf("slack: bot_token is required")
		}
		if cfg.Channels.Slack.AppToken == "" {
			return fmt.Errorf("slack: app_token is required for Socket Mode")
		}
	default:
		return fmt.Errorf("unsupported channel type: %s", channelType)
	}

	return nil
}

// EnableChannel enables a channel in the config.
func (p *ChannelProvisioner) EnableChannel(ctx context.Context, channelType models.ChannelType) error {
	return p.updateChannelEnabled(channelType, true)
}

// DisableChannel disables a channel in the config.
func (p *ChannelProvisioner) DisableChannel(ctx context.Context, channelType models.ChannelType) error {
	return p.updateChannelEnabled(channelType, false)
}

func (p *ChannelProvisioner) updateChannelEnabled(channelType models.ChannelType, enabled bool) error {
	// Load raw YAML to preserve formatting
	data, err := os.ReadFile(p.configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Navigate to channels.<type>.enabled
	channelKey := strings.ToLower(string(channelType))
	if err := setYAMLValue(&node, []string{"channels", channelKey, "enabled"}, enabled); err != nil {
		return fmt.Errorf("update config: %w", err)
	}

	// Write back
	output, err := yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := writeFilePreserveMode(p.configPath, output); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	p.logger.Info("channel updated",
		"type", channelType,
		"enabled", enabled)

	return nil
}

func writeFilePreserveMode(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// setYAMLValue sets a value at the given path in a YAML node.
func setYAMLValue(node *yaml.Node, path []string, value any) error {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return fmt.Errorf("empty document")
		}
		return setYAMLValue(node.Content[0], path, value)
	}

	if len(path) == 0 {
		// Set the value
		switch v := value.(type) {
		case bool:
			node.Kind = yaml.ScalarNode
			node.Tag = "!!bool"
			if v {
				node.Value = "true"
			} else {
				node.Value = "false"
			}
		case string:
			node.Kind = yaml.ScalarNode
			node.Tag = "!!str"
			node.Value = v
		default:
			return fmt.Errorf("unsupported value type: %T", value)
		}
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping at path %v", path)
	}

	key := path[0]
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return setYAMLValue(node.Content[i+1], path[1:], value)
		}
	}

	// Key not found, create it
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	valueNode := &yaml.Node{}
	if len(path) > 1 {
		valueNode.Kind = yaml.MappingNode
	}
	node.Content = append(node.Content, keyNode, valueNode)
	return setYAMLValue(valueNode, path[1:], value)
}

func tokenHint(token string) string {
	if len(token) < 4 {
		return ""
	}
	return "..." + token[len(token)-4:]
}
