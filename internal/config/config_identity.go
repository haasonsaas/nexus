package config

import "time"

type WorkspaceConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Path         string `yaml:"path"`
	MaxChars     int    `yaml:"max_chars"`
	AgentsFile   string `yaml:"agents_file"`
	SoulFile     string `yaml:"soul_file"`
	UserFile     string `yaml:"user_file"`
	IdentityFile string `yaml:"identity_file"`
	ToolsFile    string `yaml:"tools_file"`
	MemoryFile   string `yaml:"memory_file"`
}

type IdentityConfig struct {
	Name     string `yaml:"name"`
	Creature string `yaml:"creature"`
	Vibe     string `yaml:"vibe"`
	Emoji    string `yaml:"emoji"`
}

type UserConfig struct {
	Name             string `yaml:"name"`
	PreferredAddress string `yaml:"preferred_address"`
	Pronouns         string `yaml:"pronouns"`
	Timezone         string `yaml:"timezone"`
	Notes            string `yaml:"notes"`
}

type PluginsConfig struct {
	Load      PluginLoadConfig             `yaml:"load"`
	Entries   map[string]PluginEntryConfig `yaml:"entries"`
	Isolation PluginIsolationConfig        `yaml:"isolation"`
}

type PluginLoadConfig struct {
	Paths []string `yaml:"paths"`
}

type PluginEntryConfig struct {
	Enabled bool           `yaml:"enabled"`
	Path    string         `yaml:"path"`
	Config  map[string]any `yaml:"config"`
}

// PluginIsolationConfig configures (future) out-of-process plugin execution.
type PluginIsolationConfig struct {
	Enabled        bool                 `yaml:"enabled"`
	Backend        string               `yaml:"backend"` // docker | firecracker | daytona
	NetworkEnabled bool                 `yaml:"network_enabled"`
	Timeout        time.Duration        `yaml:"timeout"`
	Limits         ResourceLimits       `yaml:"limits"`
	RunnerPath     string               `yaml:"runner_path"`
	Daytona        SandboxDaytonaConfig `yaml:"daytona"`
}

// MarketplaceConfig configures the plugin marketplace.
type MarketplaceConfig struct {
	// Enabled enables marketplace functionality.
	Enabled bool `yaml:"enabled"`

	// Registries are the registry URLs to search for plugins.
	Registries []string `yaml:"registries"`

	// TrustedKeys are the trusted signing keys (name -> base64 public key).
	TrustedKeys map[string]string `yaml:"trusted_keys"`

	// AutoUpdate enables automatic updates for plugins.
	AutoUpdate bool `yaml:"auto_update"`

	// CheckInterval is how often to check for updates (e.g., "24h").
	CheckInterval string `yaml:"check_interval"`

	// SkipVerify skips signature verification (not recommended).
	SkipVerify bool `yaml:"skip_verify"`
}
