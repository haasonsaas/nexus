package gateway

import (
	"log/slog"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/models"
)

func buildBedrockDiscoveryConfig(cfg config.BedrockConfig, logger *slog.Logger) models.BedrockDiscoveryConfig {
	out := models.BedrockDiscoveryConfig{
		Enabled:              cfg.Enabled,
		Region:               strings.TrimSpace(cfg.Region),
		ProviderFilter:       cfg.ProviderFilter,
		DefaultContextWindow: cfg.DefaultContextWindow,
		DefaultMaxTokens:     cfg.DefaultMaxTokens,
	}
	if strings.TrimSpace(cfg.RefreshInterval) != "" {
		parsed, err := time.ParseDuration(cfg.RefreshInterval)
		if err != nil {
			if logger != nil {
				logger.Warn("invalid bedrock refresh_interval", "value", cfg.RefreshInterval, "error", err)
			}
		} else {
			out.RefreshInterval = parsed
		}
	}
	return out
}
