package config

// PluginValidator allows external packages to inject config validation.
// It should return a slice of issue strings suitable for ConfigValidationError.
type PluginValidator func(*Config) []string

var pluginValidator PluginValidator

// RegisterPluginValidator registers a plugin-aware validator.
// Only one validator may be registered; later calls overwrite earlier ones.
func RegisterPluginValidator(fn PluginValidator) {
	pluginValidator = fn
}

func pluginValidationIssues(cfg *Config) []string {
	if pluginValidator == nil || cfg == nil {
		return nil
	}
	return pluginValidator(cfg)
}
