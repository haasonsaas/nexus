package config

import "time"

type LLMConfig struct {
	DefaultProvider string                       `yaml:"default_provider"`
	Providers       map[string]LLMProviderConfig `yaml:"providers"`

	// FallbackChain specifies provider IDs to try if the default provider fails.
	// Providers are tried in order until one succeeds.
	// Example: ["openai", "google"] - try OpenAI first, then Google.
	FallbackChain []string `yaml:"fallback_chain"`

	// Bedrock configures AWS Bedrock model discovery.
	Bedrock BedrockConfig `yaml:"bedrock"`

	// Routing configures intelligent provider routing.
	Routing LLMRoutingConfig `yaml:"routing"`

	// AutoDiscover configures local provider discovery.
	AutoDiscover LLMAutoDiscoverConfig `yaml:"auto_discover"`
}

type LLMProviderConfig struct {
	APIKey       string                              `yaml:"api_key"`
	DefaultModel string                              `yaml:"default_model"`
	BaseURL      string                              `yaml:"base_url"`
	APIVersion   string                              `yaml:"api_version"`
	Profiles     map[string]LLMProviderProfileConfig `yaml:"profiles"`
}

type LLMProviderProfileConfig struct {
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
	BaseURL      string `yaml:"base_url"`
	APIVersion   string `yaml:"api_version"`
}

// LLMRoutingConfig configures provider routing rules.
type LLMRoutingConfig struct {
	Enabled           bool          `yaml:"enabled"`
	Classifier        string        `yaml:"classifier"`
	PreferLocal       bool          `yaml:"prefer_local"`
	UnhealthyCooldown time.Duration `yaml:"unhealthy_cooldown"`
	Rules             []RoutingRule `yaml:"rules"`
	Fallback          RoutingTarget `yaml:"fallback"`
}

// RoutingRule defines a routing rule.
type RoutingRule struct {
	Name   string        `yaml:"name"`
	Match  RoutingMatch  `yaml:"match"`
	Target RoutingTarget `yaml:"target"`
}

// RoutingMatch defines rule matching criteria.
type RoutingMatch struct {
	Patterns []string `yaml:"patterns"`
	Tags     []string `yaml:"tags"`
}

// RoutingTarget defines a routing destination.
type RoutingTarget struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// LLMAutoDiscoverConfig configures local provider discovery.
type LLMAutoDiscoverConfig struct {
	Ollama OllamaDiscoverConfig `yaml:"ollama"`
}

// OllamaDiscoverConfig configures Ollama discovery.
type OllamaDiscoverConfig struct {
	Enabled        bool     `yaml:"enabled"`
	PreferLocal    bool     `yaml:"prefer_local"`
	ProbeLocations []string `yaml:"probe_locations"`
}

// BedrockConfig configures AWS Bedrock model discovery.
type BedrockConfig struct {
	// Enabled enables automatic discovery of Bedrock foundation models.
	Enabled bool `yaml:"enabled"`

	// Region is the AWS region to query for models. Default: us-east-1.
	Region string `yaml:"region"`

	// RefreshInterval is how often to refresh the model list (e.g., "1h", "30m").
	// Default: 1h. Set to "0" to disable caching.
	RefreshInterval string `yaml:"refresh_interval"`

	// ProviderFilter limits discovery to specific model providers.
	// Example: ["anthropic", "amazon", "meta"]
	// Empty means all providers.
	ProviderFilter []string `yaml:"provider_filter"`

	// DefaultContextWindow is used when the model doesn't report context size.
	// Default: 32000.
	DefaultContextWindow int `yaml:"default_context_window"`

	// DefaultMaxTokens is used when the model doesn't report max output.
	// Default: 4096.
	DefaultMaxTokens int `yaml:"default_max_tokens"`
}
