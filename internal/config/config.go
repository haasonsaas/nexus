package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the main configuration structure for Nexus.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Channels ChannelsConfig `yaml:"channels"`
	LLM      LLMConfig      `yaml:"llm"`
	Tools    ToolsConfig    `yaml:"tools"`
	Logging  LoggingConfig  `yaml:"logging"`
}

type ServerConfig struct {
	Host        string `yaml:"host"`
	GRPCPort    int    `yaml:"grpc_port"`
	HTTPPort    int    `yaml:"http_port"`
	MetricsPort int    `yaml:"metrics_port"`
}

type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxConnections  int           `yaml:"max_connections"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

type AuthConfig struct {
	JWTSecret   string        `yaml:"jwt_secret"`
	TokenExpiry time.Duration `yaml:"token_expiry"`
	OAuth       OAuthConfig   `yaml:"oauth"`
}

type OAuthConfig struct {
	Google OAuthProviderConfig `yaml:"google"`
	GitHub OAuthProviderConfig `yaml:"github"`
}

type OAuthProviderConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
}

type ChannelsConfig struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Discord  DiscordConfig  `yaml:"discord"`
	Slack    SlackConfig    `yaml:"slack"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	Webhook  string `yaml:"webhook"`
}

type DiscordConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	AppID    string `yaml:"app_id"`
}

type SlackConfig struct {
	Enabled      bool   `yaml:"enabled"`
	BotToken     string `yaml:"bot_token"`
	AppToken     string `yaml:"app_token"`
	SigningSecret string `yaml:"signing_secret"`
}

type LLMConfig struct {
	DefaultProvider string                      `yaml:"default_provider"`
	Providers       map[string]LLMProviderConfig `yaml:"providers"`
}

type LLMProviderConfig struct {
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
	BaseURL      string `yaml:"base_url"`
}

type ToolsConfig struct {
	Sandbox   SandboxConfig   `yaml:"sandbox"`
	Browser   BrowserConfig   `yaml:"browser"`
	WebSearch WebSearchConfig `yaml:"websearch"`
}

type SandboxConfig struct {
	Enabled  bool          `yaml:"enabled"`
	PoolSize int           `yaml:"pool_size"`
	Timeout  time.Duration `yaml:"timeout"`
	Limits   ResourceLimits `yaml:"limits"`
}

type ResourceLimits struct {
	MaxCPU    int    `yaml:"max_cpu"`
	MaxMemory string `yaml:"max_memory"`
}

type BrowserConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Headless bool   `yaml:"headless"`
	URL      string `yaml:"url"`
}

type WebSearchConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
	URL      string `yaml:"url"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.GRPCPort == 0 {
		cfg.Server.GRPCPort = 50051
	}
	if cfg.Server.HTTPPort == 0 {
		cfg.Server.HTTPPort = 8080
	}
	if cfg.Server.MetricsPort == 0 {
		cfg.Server.MetricsPort = 9090
	}
	if cfg.Database.MaxConnections == 0 {
		cfg.Database.MaxConnections = 25
	}
	if cfg.Database.ConnMaxLifetime == 0 {
		cfg.Database.ConnMaxLifetime = 5 * time.Minute
	}
	if cfg.Auth.TokenExpiry == 0 {
		cfg.Auth.TokenExpiry = 24 * time.Hour
	}
	if cfg.LLM.DefaultProvider == "" {
		cfg.LLM.DefaultProvider = "anthropic"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
}
