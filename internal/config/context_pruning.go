package config

import (
	"strings"

	agentctx "github.com/haasonsaas/nexus/internal/agent/context"
)

// EffectiveContextPruningSettings converts config into runtime pruning settings.
// Returns nil when pruning is disabled.
func EffectiveContextPruningSettings(cfg ContextPruningConfig) *agentctx.ContextPruningSettings {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != string(agentctx.ContextPruningCacheTTL) {
		return nil
	}

	settings := agentctx.DefaultContextPruningSettings()
	settings.Mode = agentctx.ContextPruningCacheTTL

	if cfg.TTL != nil {
		settings.TTL = *cfg.TTL
	}
	if cfg.KeepLastAssistants != nil {
		settings.KeepLastAssistants = clampInt(*cfg.KeepLastAssistants, 0)
	}
	if cfg.SoftTrimRatio != nil {
		settings.SoftTrimRatio = clampFloat(*cfg.SoftTrimRatio, 0, 1)
	}
	if cfg.HardClearRatio != nil {
		settings.HardClearRatio = clampFloat(*cfg.HardClearRatio, 0, 1)
	}
	if cfg.MinPrunableToolChars != nil {
		settings.MinPrunableToolChars = clampInt(*cfg.MinPrunableToolChars, 0)
	}

	settings.Tools = agentctx.ContextPruningToolMatch{
		Allow: append([]string(nil), cfg.Tools.Allow...),
		Deny:  append([]string(nil), cfg.Tools.Deny...),
	}

	if cfg.SoftTrim.MaxChars != nil {
		settings.SoftTrim.MaxChars = clampInt(*cfg.SoftTrim.MaxChars, 0)
	}
	if cfg.SoftTrim.HeadChars != nil {
		settings.SoftTrim.HeadChars = clampInt(*cfg.SoftTrim.HeadChars, 0)
	}
	if cfg.SoftTrim.TailChars != nil {
		settings.SoftTrim.TailChars = clampInt(*cfg.SoftTrim.TailChars, 0)
	}

	if cfg.HardClear.Enabled != nil {
		settings.HardClear.Enabled = *cfg.HardClear.Enabled
	}
	if placeholder := strings.TrimSpace(cfg.HardClear.Placeholder); placeholder != "" {
		settings.HardClear.Placeholder = placeholder
	}

	return &settings
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampInt(value int, min int) int {
	if value < min {
		return min
	}
	return value
}
