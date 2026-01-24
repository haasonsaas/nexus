package controlplane

import "context"

// ConfigSnapshot is a raw config snapshot with integrity hash.
type ConfigSnapshot struct {
	Path string `json:"path"`
	Raw  string `json:"raw"`
	Hash string `json:"hash"`
}

// ConfigApplyResult describes the outcome of a config apply.
type ConfigApplyResult struct {
	Applied         bool     `json:"applied"`
	RestartRequired bool     `json:"restart_required"`
	Warnings        []string `json:"warnings,omitempty"`
}

// ConfigManager provides config control plane operations.
type ConfigManager interface {
	ConfigSnapshot(ctx context.Context) (ConfigSnapshot, error)
	ConfigSchema(ctx context.Context) ([]byte, error)
	ApplyConfig(ctx context.Context, raw string, baseHash string) (*ConfigApplyResult, error)
}
