package config

import "sync"

// PluginValidator allows external packages to inject config validation.
// It should return a slice of issue strings suitable for ConfigValidationError.
type PluginValidator func(*Config) []string

var (
	pluginValidatorMu sync.RWMutex
	pluginValidator   PluginValidator
)

// RegisterPluginValidator registers a plugin-aware validator.
// Only one validator may be registered; later calls overwrite earlier ones.
func RegisterPluginValidator(fn PluginValidator) {
	pluginValidatorMu.Lock()
	defer pluginValidatorMu.Unlock()
	pluginValidator = fn
}

func pluginValidationIssues(cfg *Config) []string {
	pluginValidatorMu.RLock()
	validator := pluginValidator
	pluginValidatorMu.RUnlock()

	if validator == nil || cfg == nil {
		return nil
	}
	return validator(cfg)
}
