package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/internal/skills"
	"gopkg.in/yaml.v3"
)

// Config is the main configuration structure for Nexus.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Auth          AuthConfig          `yaml:"auth"`
	Session       SessionConfig       `yaml:"session"`
	Workspace     WorkspaceConfig     `yaml:"workspace"`
	Identity      IdentityConfig      `yaml:"identity"`
	User          UserConfig          `yaml:"user"`
	Plugins       PluginsConfig       `yaml:"plugins"`
	Skills        skills.SkillsConfig `yaml:"skills"`
	VectorMemory  memory.Config       `yaml:"vector_memory"`
	MCP           mcp.Config          `yaml:"mcp"`
	Channels      ChannelsConfig      `yaml:"channels"`
	LLM           LLMConfig           `yaml:"llm"`
	Tools         ToolsConfig         `yaml:"tools"`
	Cron          CronConfig          `yaml:"cron"`
	Logging       LoggingConfig       `yaml:"logging"`
	Transcription TranscriptionConfig `yaml:"transcription"`
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
	JWTSecret   string         `yaml:"jwt_secret"`
	TokenExpiry time.Duration  `yaml:"token_expiry"`
	APIKeys     []APIKeyConfig `yaml:"api_keys"`
	OAuth       OAuthConfig    `yaml:"oauth"`
}

type APIKeyConfig struct {
	Key    string `yaml:"key"`
	UserID string `yaml:"user_id"`
	Email  string `yaml:"email"`
	Name   string `yaml:"name"`
}

type SessionConfig struct {
	DefaultAgentID string            `yaml:"default_agent_id"`
	SlackScope     string            `yaml:"slack_scope"`
	DiscordScope   string            `yaml:"discord_scope"`
	Memory         MemoryConfig      `yaml:"memory"`
	Heartbeat      HeartbeatConfig   `yaml:"heartbeat"`
	MemoryFlush    MemoryFlushConfig `yaml:"memory_flush"`
}

type MemoryConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Directory string `yaml:"directory"`
	MaxLines  int    `yaml:"max_lines"`
	Days      int    `yaml:"days"`
	Scope     string `yaml:"scope"`
}

type HeartbeatConfig struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
	Mode    string `yaml:"mode"`
}

type MemoryFlushConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Threshold int    `yaml:"threshold"`
	Prompt    string `yaml:"prompt"`
}

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
	Load    PluginLoadConfig             `yaml:"load"`
	Entries map[string]PluginEntryConfig `yaml:"entries"`
}

type PluginLoadConfig struct {
	Paths []string `yaml:"paths"`
}

type PluginEntryConfig struct {
	Enabled bool           `yaml:"enabled"`
	Path    string         `yaml:"path"`
	Config  map[string]any `yaml:"config"`
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
	WhatsApp WhatsAppConfig `yaml:"whatsapp"`
	Signal   SignalConfig   `yaml:"signal"`
	IMessage IMessageConfig `yaml:"imessage"`
	Matrix   MatrixConfig   `yaml:"matrix"`
}

type WhatsAppConfig struct {
	Enabled      bool   `yaml:"enabled"`
	SessionPath  string `yaml:"session_path"`
	MediaPath    string `yaml:"media_path"`
	SyncContacts bool   `yaml:"sync_contacts"`

	Presence WhatsAppPresenceConfig `yaml:"presence"`
}

type WhatsAppPresenceConfig struct {
	SendReadReceipts bool `yaml:"send_read_receipts"`
	SendTyping       bool `yaml:"send_typing"`
	BroadcastOnline  bool `yaml:"broadcast_online"`
}

type SignalConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Account       string `yaml:"account"`
	SignalCLIPath string `yaml:"signal_cli_path"`
	ConfigDir     string `yaml:"config_dir"`

	Presence SignalPresenceConfig `yaml:"presence"`
}

type SignalPresenceConfig struct {
	SendReadReceipts bool `yaml:"send_read_receipts"`
	SendTyping       bool `yaml:"send_typing"`
}

type IMessageConfig struct {
	Enabled      bool   `yaml:"enabled"`
	DatabasePath string `yaml:"database_path"`
	PollInterval string `yaml:"poll_interval"`
}

type MatrixConfig struct {
	Enabled      bool     `yaml:"enabled"`
	Homeserver   string   `yaml:"homeserver"`
	UserID       string   `yaml:"user_id"`
	AccessToken  string   `yaml:"access_token"`
	DeviceID     string   `yaml:"device_id"`
	AllowedRooms []string `yaml:"allowed_rooms"`
	AllowedUsers []string `yaml:"allowed_users"`
	JoinOnInvite bool     `yaml:"join_on_invite"`
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
	Enabled       bool   `yaml:"enabled"`
	BotToken      string `yaml:"bot_token"`
	AppToken      string `yaml:"app_token"`
	SigningSecret string `yaml:"signing_secret"`
}

type LLMConfig struct {
	DefaultProvider string                       `yaml:"default_provider"`
	Providers       map[string]LLMProviderConfig `yaml:"providers"`
}

type LLMProviderConfig struct {
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
	BaseURL      string `yaml:"base_url"`
}

type ToolsConfig struct {
	Sandbox      SandboxConfig       `yaml:"sandbox"`
	Browser      BrowserConfig       `yaml:"browser"`
	WebSearch    WebSearchConfig     `yaml:"websearch"`
	MemorySearch MemorySearchConfig  `yaml:"memory_search"`
	Notes        string              `yaml:"notes"`
	NotesFile    string              `yaml:"notes_file"`
	Execution    ToolExecutionConfig `yaml:"execution"`
	Jobs         ToolJobsConfig      `yaml:"jobs"`
}

// ToolJobsConfig controls async tool job persistence.
type ToolJobsConfig struct {
	// Retention is how long to keep completed jobs. Default: 24h.
	Retention time.Duration `yaml:"retention"`
	// PruneInterval is how often to prune old jobs. Default: 1h.
	PruneInterval time.Duration `yaml:"prune_interval"`
}

// ToolExecutionConfig controls runtime tool execution behavior.
type ToolExecutionConfig struct {
	MaxIterations   int           `yaml:"max_iterations"`
	Parallelism     int           `yaml:"parallelism"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxAttempts     int           `yaml:"max_attempts"`
	RetryBackoff    time.Duration `yaml:"retry_backoff"`
	DisableEvents   bool          `yaml:"disable_events"`
	MaxToolCalls    int           `yaml:"max_tool_calls"`
	RequireApproval []string      `yaml:"require_approval"`
	Async           []string      `yaml:"async"`
	Approval        ApprovalConfig `yaml:"approval"`
}

// ApprovalConfig controls tool approval behavior.
type ApprovalConfig struct {
	// Allowlist contains tools that are always allowed (no approval needed).
	// Supports patterns like "mcp:*", "read_*", "*" (all).
	Allowlist []string `yaml:"allowlist"`

	// Denylist contains tools that are always denied.
	Denylist []string `yaml:"denylist"`

	// SafeBins are stdin-only tools that are safe to auto-allow.
	SafeBins []string `yaml:"safe_bins"`

	// SkillAllowlist auto-allows tools defined by enabled skills.
	SkillAllowlist bool `yaml:"skill_allowlist"`

	// AskFallback queues approval when UI is unavailable instead of denying.
	AskFallback bool `yaml:"ask_fallback"`

	// DefaultDecision when no rule matches: "allowed", "denied", or "pending".
	DefaultDecision string `yaml:"default_decision"`

	// RequestTTL is how long approval requests remain valid.
	RequestTTL time.Duration `yaml:"request_ttl"`
}

type SandboxConfig struct {
	Enabled        bool           `yaml:"enabled"`
	Backend        string         `yaml:"backend"`
	PoolSize       int            `yaml:"pool_size"`
	MaxPoolSize    int            `yaml:"max_pool_size"`
	Timeout        time.Duration  `yaml:"timeout"`
	NetworkEnabled bool           `yaml:"network_enabled"`
	Limits         ResourceLimits `yaml:"limits"`
}

type ResourceLimits struct {
	MaxCPU    int    `yaml:"max_cpu"`
	MaxMemory string `yaml:"max_memory"`
}

// CronConfig configures scheduled jobs.
type CronConfig struct {
	Enabled bool            `yaml:"enabled"`
	Jobs    []CronJobConfig `yaml:"jobs"`
}

// CronJobConfig defines a scheduled job.
type CronJobConfig struct {
	ID       string             `yaml:"id"`
	Name     string             `yaml:"name"`
	Type     string             `yaml:"type"`
	Enabled  bool               `yaml:"enabled"`
	Schedule CronScheduleConfig `yaml:"schedule"`
	Message  *CronMessageConfig `yaml:"message,omitempty"`
	Webhook  *CronWebhookConfig `yaml:"webhook,omitempty"`
}

// CronScheduleConfig defines when a job runs.
type CronScheduleConfig struct {
	Cron     string        `yaml:"cron"`
	Every    time.Duration `yaml:"every"`
	At       string        `yaml:"at"`
	Timezone string        `yaml:"timezone"`
}

// CronMessageConfig defines a message job payload.
type CronMessageConfig struct {
	Channel   string `yaml:"channel"`
	ChannelID string `yaml:"channel_id"`
	Content   string `yaml:"content"`
}

// CronWebhookConfig defines a webhook job payload.
type CronWebhookConfig struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	Timeout time.Duration     `yaml:"timeout"`
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

type MemorySearchConfig struct {
	Enabled       bool                         `yaml:"enabled"`
	Directory     string                       `yaml:"directory"`
	MemoryFile    string                       `yaml:"memory_file"`
	MaxResults    int                          `yaml:"max_results"`
	MaxSnippetLen int                          `yaml:"max_snippet_len"`
	Mode          string                       `yaml:"mode"`
	Embeddings    MemorySearchEmbeddingsConfig `yaml:"embeddings"`
}

type MemorySearchEmbeddingsConfig struct {
	Provider string        `yaml:"provider"`
	APIKey   string        `yaml:"api_key"`
	BaseURL  string        `yaml:"base_url"`
	Model    string        `yaml:"model"`
	CacheDir string        `yaml:"cache_dir"`
	CacheTTL time.Duration `yaml:"cache_ttl"`
	Timeout  time.Duration `yaml:"timeout"`
}
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// TranscriptionConfig configures audio transcription.
type TranscriptionConfig struct {
	// Enabled enables/disables transcription globally
	Enabled bool `yaml:"enabled"`

	// Provider is the transcription provider (e.g., "openai")
	Provider string `yaml:"provider"`

	// APIKey is the API key for the transcription provider
	APIKey string `yaml:"api_key"`

	// BaseURL is an optional custom base URL for the API
	BaseURL string `yaml:"base_url"`

	// Model is the transcription model to use (e.g., "whisper-1")
	Model string `yaml:"model"`

	// Language is the default language for transcription (ISO 639-1)
	// If empty, the provider will auto-detect the language
	Language string `yaml:"language"`
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
	decoder := yaml.NewDecoder(strings.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("failed to parse config: expected single document")
	}

	applyEnvOverrides(&cfg)

	// Apply defaults
	applyDefaults(&cfg)

	// Validate config
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	applyServerDefaults(&cfg.Server)
	applyDatabaseDefaults(&cfg.Database)
	applyAuthDefaults(&cfg.Auth)
	applySessionDefaults(&cfg.Session)
	applyWorkspaceDefaults(&cfg.Workspace)
	applyToolsDefaults(cfg)
	applyLLMDefaults(&cfg.LLM)
	applyLoggingDefaults(&cfg.Logging)
	applyTranscriptionDefaults(&cfg.Transcription)
}

func applyServerDefaults(cfg *ServerConfig) {
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.GRPCPort == 0 {
		cfg.GRPCPort = 50051
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	if cfg.MetricsPort == 0 {
		cfg.MetricsPort = 9090
	}
}

func applyDatabaseDefaults(cfg *DatabaseConfig) {
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 25
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = 5 * time.Minute
	}
}

func applyAuthDefaults(cfg *AuthConfig) {
	if cfg.TokenExpiry == 0 {
		cfg.TokenExpiry = 24 * time.Hour
	}
}

func applySessionDefaults(cfg *SessionConfig) {
	if cfg.DefaultAgentID == "" {
		cfg.DefaultAgentID = "main"
	}
	if cfg.SlackScope == "" {
		cfg.SlackScope = "thread"
	}
	if cfg.DiscordScope == "" {
		cfg.DiscordScope = "thread"
	}
	if cfg.Memory.Directory == "" {
		cfg.Memory.Directory = "memory"
	}
	if cfg.Memory.MaxLines == 0 {
		cfg.Memory.MaxLines = 20
	}
	if cfg.Memory.Days == 0 {
		cfg.Memory.Days = 2
	}
	if cfg.Memory.Scope == "" {
		cfg.Memory.Scope = "session"
	}
	if cfg.Heartbeat.File == "" {
		cfg.Heartbeat.File = "HEARTBEAT.md"
	}
	if cfg.Heartbeat.Mode == "" {
		cfg.Heartbeat.Mode = "always"
	}
	if cfg.MemoryFlush.Threshold == 0 {
		cfg.MemoryFlush.Threshold = 80
	}
	if cfg.MemoryFlush.Prompt == "" {
		cfg.MemoryFlush.Prompt = "Session nearing compaction. If there are durable facts, store them in memory/YYYY-MM-DD.md or MEMORY.md. Reply NO_REPLY if nothing needs attention."
	}
}

func applyWorkspaceDefaults(cfg *WorkspaceConfig) {
	if cfg.Path == "" {
		cfg.Path = "."
	}
	if cfg.MaxChars == 0 {
		cfg.MaxChars = 20000
	}
	if cfg.AgentsFile == "" {
		cfg.AgentsFile = "AGENTS.md"
	}
	if cfg.SoulFile == "" {
		cfg.SoulFile = "SOUL.md"
	}
	if cfg.UserFile == "" {
		cfg.UserFile = "USER.md"
	}
	if cfg.IdentityFile == "" {
		cfg.IdentityFile = "IDENTITY.md"
	}
	if cfg.ToolsFile == "" {
		cfg.ToolsFile = "TOOLS.md"
	}
	if cfg.MemoryFile == "" {
		cfg.MemoryFile = "MEMORY.md"
	}
}

func applyToolsDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.Tools.MemorySearch.MaxResults == 0 {
		cfg.Tools.MemorySearch.MaxResults = 5
	}
	if cfg.Tools.MemorySearch.MaxSnippetLen == 0 {
		cfg.Tools.MemorySearch.MaxSnippetLen = 200
	}
	if cfg.Tools.MemorySearch.Mode == "" {
		cfg.Tools.MemorySearch.Mode = "hybrid"
	}
	if cfg.Tools.MemorySearch.Directory == "" {
		cfg.Tools.MemorySearch.Directory = cfg.Session.Memory.Directory
	}
	if cfg.Tools.MemorySearch.MemoryFile == "" {
		cfg.Tools.MemorySearch.MemoryFile = cfg.Workspace.MemoryFile
	}
	applyMemorySearchEmbeddingsDefaults(&cfg.Tools.MemorySearch.Embeddings)
	// Job persistence defaults
	if cfg.Tools.Jobs.Retention == 0 {
		cfg.Tools.Jobs.Retention = 24 * time.Hour
	}
	if cfg.Tools.Jobs.PruneInterval == 0 {
		cfg.Tools.Jobs.PruneInterval = 1 * time.Hour
	}
}

func applyMemorySearchEmbeddingsDefaults(cfg *MemorySearchEmbeddingsConfig) {
	if cfg == nil {
		return
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 24 * time.Hour
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if strings.TrimSpace(cfg.CacheDir) == "" {
		home, _ := os.UserHomeDir()
		if strings.TrimSpace(home) == "" {
			home = "."
		}
		cfg.CacheDir = filepath.Join(home, ".nexus", "cache", "embeddings")
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return
	}
	if cfg.BaseURL == "" {
		switch provider {
		case "openai":
			cfg.BaseURL = "https://api.openai.com/v1"
		case "openrouter":
			cfg.BaseURL = "https://openrouter.ai/api/v1"
		}
	}
	if cfg.Model == "" {
		switch provider {
		case "openai", "openrouter":
			cfg.Model = "text-embedding-3-small"
		}
	}
}

// DefaultWorkspaceConfig returns a workspace config with defaults applied.
func DefaultWorkspaceConfig() WorkspaceConfig {
	cfg := WorkspaceConfig{}
	applyWorkspaceDefaults(&cfg)
	return cfg
}

func applyLLMDefaults(cfg *LLMConfig) {
	if cfg.DefaultProvider == "" {
		cfg.DefaultProvider = "anthropic"
	}
}

func applyLoggingDefaults(cfg *LoggingConfig) {
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.Format == "" {
		cfg.Format = "json"
	}
}

func applyTranscriptionDefaults(cfg *TranscriptionConfig) {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.Model == "" {
		cfg.Model = "whisper-1"
	}
}

func applyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}

	if value := strings.TrimSpace(os.Getenv("NEXUS_HOST")); value != "" {
		cfg.Server.Host = value
	}
	if value := strings.TrimSpace(os.Getenv("NEXUS_GRPC_PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Server.GRPCPort = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("NEXUS_HTTP_PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Server.HTTPPort = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("NEXUS_METRICS_PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			cfg.Server.MetricsPort = parsed
		}
	}

	if value := strings.TrimSpace(os.Getenv("DATABASE_URL")); value != "" {
		cfg.Database.URL = value
	}

	if value := strings.TrimSpace(os.Getenv("JWT_SECRET")); value != "" {
		cfg.Auth.JWTSecret = value
	}
	if value := strings.TrimSpace(os.Getenv("NEXUS_JWT_SECRET")); value != "" {
		cfg.Auth.JWTSecret = value
	}
	if value := strings.TrimSpace(os.Getenv("NEXUS_TOKEN_EXPIRY")); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			cfg.Auth.TokenExpiry = parsed
		}
	}
}

type ConfigValidationError struct {
	Issues []string
}

func (e *ConfigValidationError) Error() string {
	return "config validation failed:\n- " + strings.Join(e.Issues, "\n- ")
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	var issues []string

	if !validScope(cfg.Session.SlackScope) {
		issues = append(issues, "session.slack_scope must be \"thread\" or \"channel\"")
	}
	if !validScope(cfg.Session.DiscordScope) {
		issues = append(issues, "session.discord_scope must be \"thread\" or \"channel\"")
	}
	if cfg.Session.Memory.MaxLines < 0 {
		issues = append(issues, "session.memory.max_lines must be >= 0")
	}
	if cfg.Session.Memory.Days < 0 {
		issues = append(issues, "session.memory.days must be >= 0")
	}
	if cfg.Session.Memory.Scope != "" && !validMemoryScope(cfg.Session.Memory.Scope) {
		issues = append(issues, "session.memory.scope must be \"session\", \"channel\", or \"global\"")
	}
	if cfg.Session.Heartbeat.Enabled && strings.TrimSpace(cfg.Session.Heartbeat.File) == "" {
		issues = append(issues, "session.heartbeat.file is required when heartbeat is enabled")
	}
	if cfg.Session.Heartbeat.Mode != "" && !validHeartbeatMode(cfg.Session.Heartbeat.Mode) {
		issues = append(issues, "session.heartbeat.mode must be \"always\" or \"on_demand\"")
	}
	if cfg.Session.MemoryFlush.Threshold < 0 {
		issues = append(issues, "session.memory_flush.threshold must be >= 0")
	}
	if cfg.Workspace.MaxChars < 0 {
		issues = append(issues, "workspace.max_chars must be >= 0")
	}

	defaultProvider := strings.ToLower(strings.TrimSpace(cfg.LLM.DefaultProvider))
	if defaultProvider != "" {
		if _, ok := cfg.LLM.Providers[defaultProvider]; !ok {
			if _, ok := cfg.LLM.Providers[cfg.LLM.DefaultProvider]; !ok {
				issues = append(issues, fmt.Sprintf("llm.providers missing entry for default_provider %q", cfg.LLM.DefaultProvider))
			}
		}
	}

	seenKeys := map[string]struct{}{}
	for i, entry := range cfg.Auth.APIKeys {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			issues = append(issues, fmt.Sprintf("auth.api_keys[%d].key must be set", i))
			continue
		}
		if _, ok := seenKeys[key]; ok {
			issues = append(issues, fmt.Sprintf("auth.api_keys[%d].key must be unique", i))
		} else {
			seenKeys[key] = struct{}{}
		}
	}

	if provider := strings.ToLower(strings.TrimSpace(cfg.Tools.WebSearch.Provider)); provider != "" {
		switch provider {
		case "searxng", "brave", "duckduckgo":
		default:
			issues = append(issues, "tools.websearch.provider must be \"searxng\", \"brave\", or \"duckduckgo\"")
		}
	}
	if cfg.Tools.MemorySearch.MaxResults < 0 {
		issues = append(issues, "tools.memory_search.max_results must be >= 0")
	}
	if cfg.Tools.MemorySearch.MaxSnippetLen < 0 {
		issues = append(issues, "tools.memory_search.max_snippet_len must be >= 0")
	}
	if mode := strings.ToLower(strings.TrimSpace(cfg.Tools.MemorySearch.Mode)); mode != "" {
		switch mode {
		case "lexical", "vector", "hybrid":
		default:
			issues = append(issues, "tools.memory_search.mode must be \"lexical\", \"vector\", or \"hybrid\"")
		}
	}
	if cfg.Tools.MemorySearch.Embeddings.CacheTTL < 0 {
		issues = append(issues, "tools.memory_search.embeddings.cache_ttl must be >= 0")
	}
	if cfg.Tools.MemorySearch.Embeddings.Timeout < 0 {
		issues = append(issues, "tools.memory_search.embeddings.timeout must be >= 0")
	}
	if cfg.Tools.Execution.MaxIterations < 0 {
		issues = append(issues, "tools.execution.max_iterations must be >= 0")
	}
	if cfg.Tools.Execution.Parallelism < 0 {
		issues = append(issues, "tools.execution.parallelism must be >= 0")
	}
	if cfg.Tools.Execution.Timeout < 0 {
		issues = append(issues, "tools.execution.timeout must be >= 0")
	}
	if cfg.Tools.Execution.MaxAttempts < 0 {
		issues = append(issues, "tools.execution.max_attempts must be >= 0")
	}
	if cfg.Tools.Execution.RetryBackoff < 0 {
		issues = append(issues, "tools.execution.retry_backoff must be >= 0")
	}
	if cfg.Tools.Execution.MaxToolCalls < 0 {
		issues = append(issues, "tools.execution.max_tool_calls must be >= 0")
	}

	if cfg.Cron.Enabled {
		for i, job := range cfg.Cron.Jobs {
			if strings.TrimSpace(job.ID) == "" {
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].id is required", i))
			}
			if strings.TrimSpace(job.Type) == "" {
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].type is required", i))
			}
			if strings.TrimSpace(job.Schedule.Cron) == "" && job.Schedule.Every == 0 && strings.TrimSpace(job.Schedule.At) == "" {
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].schedule is required", i))
			}
			switch strings.ToLower(strings.TrimSpace(job.Type)) {
			case "webhook":
				if job.Webhook == nil || strings.TrimSpace(job.Webhook.URL) == "" {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.url is required for webhook jobs", i))
				}
			case "message", "agent":
			default:
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].type must be message, agent, or webhook", i))
			}
		}
	}

	if len(issues) > 0 {
		return &ConfigValidationError{Issues: issues}
	}

	return nil
}

func validScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "thread", "channel":
		return true
	default:
		return false
	}
}

func validMemoryScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "session", "channel", "global":
		return true
	default:
		return false
	}
}

func validHeartbeatMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "on_demand":
		return true
	default:
		return false
	}
}
