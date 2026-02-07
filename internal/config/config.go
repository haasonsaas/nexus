package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/audit"
	"github.com/haasonsaas/nexus/internal/experiments"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/internal/ratelimit"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/haasonsaas/nexus/internal/templates"
	"github.com/haasonsaas/nexus/internal/tts"
)

// Config is the main configuration structure for Nexus.
type Config struct {
	Version       int                       `yaml:"version"`
	Server        ServerConfig              `yaml:"server"`
	CanvasHost    CanvasHostConfig          `yaml:"canvas_host"`
	Canvas        CanvasConfig              `yaml:"canvas"`
	Gateway       GatewayConfig             `yaml:"gateway"`
	Cluster       ClusterConfig             `yaml:"cluster"`
	Commands      CommandsConfig            `yaml:"commands"`
	Database      DatabaseConfig            `yaml:"database"`
	Auth          AuthConfig                `yaml:"auth"`
	Session       SessionConfig             `yaml:"session"`
	Workspace     WorkspaceConfig           `yaml:"workspace"`
	Identity      IdentityConfig            `yaml:"identity"`
	User          UserConfig                `yaml:"user"`
	Plugins       PluginsConfig             `yaml:"plugins"`
	Marketplace   MarketplaceConfig         `yaml:"marketplace"`
	Skills        skills.SkillsConfig       `yaml:"skills"`
	Templates     templates.TemplatesConfig `yaml:"templates"`
	Experiments   experiments.Config        `yaml:"experiments"`
	VectorMemory  memory.Config             `yaml:"vector_memory"`
	Attention     AttentionConfig           `yaml:"attention"`
	Steering      SteeringConfig            `yaml:"steering"`
	RAG           RAGConfig                 `yaml:"rag"`
	MCP           mcp.Config                `yaml:"mcp"`
	Edge          EdgeConfig                `yaml:"edge"`
	Artifacts     ArtifactConfig            `yaml:"artifacts"`
	Channels      ChannelsConfig            `yaml:"channels"`
	LLM           LLMConfig                 `yaml:"llm"`
	Tools         ToolsConfig               `yaml:"tools"`
	Cron          CronConfig                `yaml:"cron"`
	Tasks         TasksConfig               `yaml:"tasks"`
	Logging       LoggingConfig             `yaml:"logging"`
	Observability ObservabilityConfig       `yaml:"observability"`
	Security      SecurityConfig            `yaml:"security"`
	Transcription TranscriptionConfig       `yaml:"transcription"`
	TTS           tts.Config                `yaml:"tts"`
}



// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	raw, err := LoadRaw(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg, err := decodeRawConfig(raw)
	if err != nil {
		return nil, err
	}
	if err := ValidateVersion(cfg.Version); err != nil {
		return nil, err
	}

	applyEnvOverrides(cfg)

	// Apply defaults
	applyDefaults(cfg)

	// Validate config
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	applyServerDefaults(&cfg.Server)
	applyCanvasHostDefaults(&cfg.CanvasHost, cfg)
	applyCanvasDefaults(&cfg.Canvas)
	applyDatabaseDefaults(&cfg.Database)
	applyAuthDefaults(&cfg.Auth)
	applyClusterDefaults(&cfg.Cluster)
	applyChannelDefaults(&cfg.Channels)
	applyCommandsDefaults(&cfg.Commands)
	applySessionDefaults(&cfg.Session)
	applyWorkspaceDefaults(&cfg.Workspace)
	applyToolsDefaults(cfg)
	applyAttentionDefaults(&cfg.Attention)
	applySteeringDefaults(&cfg.Steering)
	applyLLMDefaults(&cfg.LLM)
	applyLoggingDefaults(&cfg.Logging)
	applyObservabilityDefaults(&cfg.Observability)
	applySecurityDefaults(&cfg.Security)
	applyTranscriptionDefaults(&cfg.Transcription)
	applyTTSDefaults(&cfg.TTS)
	applyMarketplaceDefaults(&cfg.Marketplace)
	applyRAGDefaults(&cfg.RAG)
	applyEdgeDefaults(&cfg.Edge)
	applyArtifactDefaults(&cfg.Artifacts)
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

func applyClusterDefaults(cfg *ClusterConfig) {
	if cfg.NodeID == "" {
		if host, err := os.Hostname(); err == nil && host != "" {
			cfg.NodeID = host
		} else {
			cfg.NodeID = fmt.Sprintf("gateway-%d", time.Now().UnixNano())
		}
	}
	if cfg.SessionLocks.TTL == 0 {
		cfg.SessionLocks.TTL = 2 * time.Minute
	}
	if cfg.SessionLocks.RefreshInterval == 0 {
		cfg.SessionLocks.RefreshInterval = 30 * time.Second
	}
	if cfg.SessionLocks.AcquireTimeout == 0 {
		cfg.SessionLocks.AcquireTimeout = 10 * time.Second
	}
	if cfg.SessionLocks.PollInterval == 0 {
		cfg.SessionLocks.PollInterval = 200 * time.Millisecond
	}
}

func applyCanvasHostDefaults(cfg *CanvasHostConfig, rootCfg *Config) {
	if cfg == nil {
		return
	}
	rootSpecified := strings.TrimSpace(cfg.Root) != ""
	if cfg.Host == "" {
		if rootCfg != nil && rootCfg.Server.Host != "" {
			cfg.Host = rootCfg.Server.Host
		} else {
			cfg.Host = "0.0.0.0"
		}
	}
	if cfg.Port == 0 {
		cfg.Port = 18793
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "/__nexus__"
	}
	base := strings.TrimSpace(cfg.Root)
	if base != "" {
		base = filepath.Dir(base)
	}
	if strings.TrimSpace(base) == "" {
		if rootCfg != nil && strings.TrimSpace(rootCfg.Workspace.Path) != "" {
			base = rootCfg.Workspace.Path
		}
		if strings.TrimSpace(base) == "" {
			if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
				base = filepath.Join(home, ".nexus")
			}
		}
		if strings.TrimSpace(base) == "" {
			base = "."
		}
	}
	if cfg.Root == "" {
		cfg.Root = filepath.Join(base, "canvas")
	}
	if cfg.A2UIRoot == "" {
		cfg.A2UIRoot = filepath.Join(base, "a2ui")
	}
	if cfg.LiveReload == nil {
		liveReload := true
		cfg.LiveReload = &liveReload
	}
	if cfg.InjectClient == nil {
		inject := cfg.LiveReload != nil && *cfg.LiveReload
		cfg.InjectClient = &inject
	}
	if cfg.AutoIndex == nil {
		autoIndex := true
		cfg.AutoIndex = &autoIndex
	}
	if cfg.Enabled == nil {
		enabled := false
		if rootSpecified {
			enabled = true
		} else if info, err := os.Stat(cfg.Root); err == nil && info.IsDir() {
			enabled = true
		}
		cfg.Enabled = &enabled
	}
}

func applyCanvasDefaults(cfg *CanvasConfig) {
	if cfg == nil {
		return
	}
	if cfg.Retention.StateMaxAge == 0 {
		cfg.Retention.StateMaxAge = 30 * 24 * time.Hour
	}
	if cfg.Retention.EventMaxAge == 0 {
		cfg.Retention.EventMaxAge = 7 * 24 * time.Hour
	}
	if cfg.Retention.StateMaxBytes == 0 {
		cfg.Retention.StateMaxBytes = 1 << 20 // 1 MiB
	}
	if cfg.Retention.EventMaxBytes == 0 {
		cfg.Retention.EventMaxBytes = 256 << 10 // 256 KiB
	}
	if cfg.Tokens.TTL == 0 {
		cfg.Tokens.TTL = 30 * time.Minute
	}
	applyCanvasActionDefaults(&cfg.Actions)
	applyAuditDefaults(&cfg.Audit)
}

func applyCanvasActionDefaults(cfg *CanvasActionConfig) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.DefaultRole) == "" {
		cfg.DefaultRole = "viewer"
	}
	if cfg.RateLimit.RequestsPerSecond == 0 && cfg.RateLimit.BurstSize == 0 && !cfg.RateLimit.Enabled {
		cfg.RateLimit = ratelimit.DefaultConfig()
		return
	}
	defaults := ratelimit.DefaultConfig()
	if cfg.RateLimit.RequestsPerSecond == 0 {
		cfg.RateLimit.RequestsPerSecond = defaults.RequestsPerSecond
	}
	if cfg.RateLimit.BurstSize == 0 {
		cfg.RateLimit.BurstSize = defaults.BurstSize
	}
}

func applyAuditDefaults(cfg *audit.Config) {
	if cfg == nil {
		return
	}
	if cfg.Level == "" {
		cfg.Level = audit.LevelInfo
	}
	if cfg.Format == "" {
		cfg.Format = audit.FormatJSON
	}
	if cfg.Output == "" {
		cfg.Output = "stdout"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1000
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.MaxFieldSize == 0 {
		cfg.MaxFieldSize = 1024
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

func applyChannelDefaults(cfg *ChannelsConfig) {
	applyChannelPolicyDefaults(&cfg.Telegram.DM)
	applyChannelPolicyDefaults(&cfg.Telegram.Group)
	applyChannelPolicyDefaults(&cfg.Discord.DM)
	applyChannelPolicyDefaults(&cfg.Discord.Group)
	applyChannelPolicyDefaults(&cfg.Slack.DM)
	applyChannelPolicyDefaults(&cfg.Slack.Group)
	applySlackCanvasDefaults(&cfg.Slack.Canvas)
	applyChannelPolicyDefaults(&cfg.WhatsApp.DM)
	applyChannelPolicyDefaults(&cfg.WhatsApp.Group)
	applyChannelPolicyDefaults(&cfg.Signal.DM)
	applyChannelPolicyDefaults(&cfg.Signal.Group)
	applyChannelPolicyDefaults(&cfg.IMessage.DM)
	applyChannelPolicyDefaults(&cfg.IMessage.Group)
	applyChannelPolicyDefaults(&cfg.Matrix.DM)
	applyChannelPolicyDefaults(&cfg.Matrix.Group)
	applyChannelPolicyDefaults(&cfg.Teams.DM)
	applyChannelPolicyDefaults(&cfg.Teams.Group)
}

func applyAttentionDefaults(cfg *AttentionConfig) {
	if cfg == nil {
		return
	}
	if cfg.MaxItems == 0 {
		cfg.MaxItems = 5
	}
}

func applySteeringDefaults(cfg *SteeringConfig) {
	if cfg == nil {
		return
	}
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		if rule.Enabled == nil {
			enabled := true
			rule.Enabled = &enabled
		}
	}
}

func applyChannelPolicyDefaults(cfg *ChannelPolicyConfig) {
	if strings.TrimSpace(cfg.Policy) == "" {
		cfg.Policy = "open"
	}
}

func applySlackCanvasDefaults(cfg *SlackCanvasConfig) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.Command) == "" {
		cfg.Command = "/canvas"
	}
	if strings.TrimSpace(cfg.ShortcutCallback) == "" {
		cfg.ShortcutCallback = "open_canvas"
	}
	if strings.TrimSpace(cfg.DefaultRole) == "" {
		if strings.TrimSpace(cfg.Role) != "" {
			cfg.DefaultRole = cfg.Role
		} else {
			cfg.DefaultRole = "editor"
		}
	}
	if strings.TrimSpace(cfg.Role) == "" {
		cfg.Role = cfg.DefaultRole
	}
}

func applyCommandsDefaults(cfg *CommandsConfig) {
	if cfg == nil {
		return
	}
	if cfg.Enabled == nil {
		enabled := true
		cfg.Enabled = &enabled
	}
	if len(cfg.InlineCommands) == 0 {
		cfg.InlineCommands = []string{"help", "commands", "status", "whoami", "id"}
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
	applySessionScopeDefaults(&cfg.Scoping)
}

func applySessionScopeDefaults(cfg *SessionScopeConfig) {
	if cfg.DMScope == "" {
		cfg.DMScope = "main"
	}
	if cfg.Reset.Mode == "" {
		cfg.Reset.Mode = "never"
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
	if cfg.Tools.WebFetch.MaxChars == 0 {
		cfg.Tools.WebFetch.MaxChars = 10000
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
	if cfg.Tools.FactExtract.MaxFacts == 0 {
		cfg.Tools.FactExtract.MaxFacts = 10
	}
	if strings.TrimSpace(cfg.Tools.Policies.Default) == "" {
		cfg.Tools.Policies.Default = "allow"
	}
	applyMemorySearchEmbeddingsDefaults(&cfg.Tools.MemorySearch.Embeddings)
	applyLinksDefaults(&cfg.Tools.Links)
	// Job persistence defaults
	if cfg.Tools.Jobs.Retention == 0 {
		cfg.Tools.Jobs.Retention = 24 * time.Hour
	}
	if cfg.Tools.Jobs.PruneInterval == 0 {
		cfg.Tools.Jobs.PruneInterval = 1 * time.Hour
	}
	if strings.TrimSpace(cfg.Tools.Execution.ResultGuard.RedactionText) == "" {
		cfg.Tools.Execution.ResultGuard.RedactionText = "[redacted]"
	}
	if strings.TrimSpace(cfg.Tools.Execution.ResultGuard.TruncateSuffix) == "" {
		cfg.Tools.Execution.ResultGuard.TruncateSuffix = "...[truncated]"
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
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
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

func applyLinksDefaults(cfg *LinksConfig) {
	if cfg == nil {
		return
	}
	if cfg.MaxLinks == 0 {
		cfg.MaxLinks = 5
	}
	if cfg.MaxOutputChars == 0 {
		cfg.MaxOutputChars = 2000
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 30
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
	if cfg.Routing.Classifier == "" {
		cfg.Routing.Classifier = "heuristic"
	}
	if cfg.AutoDiscover.Ollama.Enabled && len(cfg.AutoDiscover.Ollama.ProbeLocations) == 0 {
		cfg.AutoDiscover.Ollama.ProbeLocations = []string{
			"http://localhost:11434",
			"http://ollama:11434",
			"http://ollama.ollama.svc.cluster.local:11434",
		}
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

func applyObservabilityDefaults(cfg *ObservabilityConfig) {
	if cfg == nil {
		return
	}
	if cfg.Tracing.SamplingRate == 0 {
		cfg.Tracing.SamplingRate = 1.0
	}
	if cfg.Tracing.ServiceName == "" {
		cfg.Tracing.ServiceName = "nexus"
	}
	if cfg.Tracing.Attributes == nil {
		cfg.Tracing.Attributes = map[string]string{}
	}
}

func applySecurityDefaults(cfg *SecurityConfig) {
	if cfg == nil {
		return
	}
	if cfg.Posture.Interval == 0 {
		cfg.Posture.Interval = 10 * time.Minute
	}
	if cfg.Posture.IncludeFilesystem == nil {
		value := true
		cfg.Posture.IncludeFilesystem = &value
	}
	if cfg.Posture.IncludeGateway == nil {
		value := true
		cfg.Posture.IncludeGateway = &value
	}
	if cfg.Posture.IncludeConfig == nil {
		value := true
		cfg.Posture.IncludeConfig = &value
	}
	if cfg.Posture.CheckSymlinks == nil {
		value := true
		cfg.Posture.CheckSymlinks = &value
	}
	if cfg.Posture.EmitEvents == nil {
		value := true
		cfg.Posture.EmitEvents = &value
	}
	if cfg.Posture.AutoRemediation.Mode == "" {
		cfg.Posture.AutoRemediation.Mode = "warn_only"
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

func applyTTSDefaults(cfg *tts.Config) {
	if cfg == nil {
		return
	}
	if cfg.Provider == "" {
		cfg.Provider = tts.ProviderEdge
	}
	cfg.ApplyDefaults()
}

func applyMarketplaceDefaults(cfg *MarketplaceConfig) {
	if len(cfg.Registries) == 0 {
		cfg.Registries = []string{"https://plugins.nexus.dev"}
	}
	if cfg.CheckInterval == "" {
		cfg.CheckInterval = "24h"
	}
}

func applyRAGDefaults(cfg *RAGConfig) {
	// Store defaults
	if cfg.Store.Backend == "" {
		cfg.Store.Backend = "pgvector"
	}
	if cfg.Store.Dimension == 0 {
		cfg.Store.Dimension = 1536
	}

	// Chunking defaults
	if cfg.Chunking.ChunkSize == 0 {
		cfg.Chunking.ChunkSize = 1000
	}
	if cfg.Chunking.ChunkOverlap == 0 {
		cfg.Chunking.ChunkOverlap = 200
	}
	if cfg.Chunking.MinChunkSize == 0 {
		cfg.Chunking.MinChunkSize = 100
	}

	// Embeddings defaults
	if cfg.Embeddings.Provider == "" {
		cfg.Embeddings.Provider = "openai"
	}
	if cfg.Embeddings.Model == "" {
		cfg.Embeddings.Model = "text-embedding-3-small"
	}
	if cfg.Embeddings.BatchSize == 0 {
		cfg.Embeddings.BatchSize = 100
	}

	// Search defaults
	if cfg.Search.DefaultLimit == 0 {
		cfg.Search.DefaultLimit = 5
	}
	if cfg.Search.DefaultThreshold == 0 {
		cfg.Search.DefaultThreshold = 0.7
	}
	if cfg.Search.MaxResults == 0 {
		cfg.Search.MaxResults = 20
	}

	// Context injection defaults
	if cfg.ContextInjection.MaxChunks == 0 {
		cfg.ContextInjection.MaxChunks = 5
	}
	if cfg.ContextInjection.MaxTokens == 0 {
		cfg.ContextInjection.MaxTokens = 2000
	}
	if cfg.ContextInjection.MinScore == 0 {
		cfg.ContextInjection.MinScore = 0.7
	}
	if cfg.ContextInjection.Scope == "" {
		cfg.ContextInjection.Scope = "global"
	}
}

func applyEdgeDefaults(cfg *EdgeConfig) {
	if cfg.AuthMode == "" {
		cfg.AuthMode = "token"
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.HeartbeatTimeout == 0 {
		cfg.HeartbeatTimeout = 90 * time.Second
	}
	if cfg.DefaultToolTimeout == 0 {
		cfg.DefaultToolTimeout = 60 * time.Second
	}
	if cfg.MaxConcurrentTools == 0 {
		cfg.MaxConcurrentTools = 10
	}
	if cfg.EventBufferSize == 0 {
		cfg.EventBufferSize = 1000
	}
}

func applyArtifactDefaults(cfg *ArtifactConfig) {
	if cfg.Backend == "" {
		cfg.Backend = "local"
	}
	if cfg.LocalPath == "" {
		cfg.LocalPath = filepath.Join(os.TempDir(), "nexus-artifacts")
	}
	if cfg.MetadataBackend == "" {
		cfg.MetadataBackend = "file"
	}
	if cfg.MetadataPath == "" {
		cfg.MetadataPath = filepath.Join(cfg.LocalPath, "metadata.json")
	}
	if cfg.PruneInterval == 0 {
		cfg.PruneInterval = time.Hour
	}
	if cfg.TTLs == nil {
		cfg.TTLs = map[string]time.Duration{
			"screenshot": 7 * 24 * time.Hour,
			"recording":  30 * 24 * time.Hour,
			"file":       14 * 24 * time.Hour,
			"default":    24 * time.Hour,
		}
	}
}

func isTruthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func applyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}

	if isTruthyEnv(os.Getenv("NEXUS_SKIP_CANVAS_HOST")) || isTruthyEnv(os.Getenv("CLAWDBOT_SKIP_CANVAS_HOST")) {
		disabled := false
		cfg.CanvasHost.Enabled = &disabled
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

	validateChannelPolicy(&issues, "channels.telegram.dm", cfg.Channels.Telegram.DM)
	validateChannelPolicy(&issues, "channels.telegram.group", cfg.Channels.Telegram.Group)
	validateChannelPolicy(&issues, "channels.discord.dm", cfg.Channels.Discord.DM)
	validateChannelPolicy(&issues, "channels.discord.group", cfg.Channels.Discord.Group)
	validateChannelPolicy(&issues, "channels.slack.dm", cfg.Channels.Slack.DM)
	validateChannelPolicy(&issues, "channels.slack.group", cfg.Channels.Slack.Group)
	validateChannelPolicy(&issues, "channels.whatsapp.dm", cfg.Channels.WhatsApp.DM)
	validateChannelPolicy(&issues, "channels.whatsapp.group", cfg.Channels.WhatsApp.Group)
	validateChannelPolicy(&issues, "channels.signal.dm", cfg.Channels.Signal.DM)
	validateChannelPolicy(&issues, "channels.signal.group", cfg.Channels.Signal.Group)
	validateChannelPolicy(&issues, "channels.imessage.dm", cfg.Channels.IMessage.DM)
	validateChannelPolicy(&issues, "channels.imessage.group", cfg.Channels.IMessage.Group)
	validateChannelPolicy(&issues, "channels.matrix.dm", cfg.Channels.Matrix.DM)
	validateChannelPolicy(&issues, "channels.matrix.group", cfg.Channels.Matrix.Group)
	validateChannelPolicy(&issues, "channels.teams.dm", cfg.Channels.Teams.DM)
	validateChannelPolicy(&issues, "channels.teams.group", cfg.Channels.Teams.Group)
	validateChannelPolicy(&issues, "channels.mattermost.dm", cfg.Channels.Mattermost.DM)
	validateChannelPolicy(&issues, "channels.mattermost.group", cfg.Channels.Mattermost.Group)
	validateChannelPolicy(&issues, "channels.nextcloud_talk.dm", cfg.Channels.NextcloudTalk.DM)
	validateChannelPolicy(&issues, "channels.nextcloud_talk.group", cfg.Channels.NextcloudTalk.Group)
	validateChannelPolicy(&issues, "channels.zalo.dm", cfg.Channels.Zalo.DM)
	validateChannelPolicy(&issues, "channels.zalo.group", cfg.Channels.Zalo.Group)
	validateChannelPolicy(&issues, "channels.bluebubbles.dm", cfg.Channels.BlueBubbles.DM)
	validateChannelPolicy(&issues, "channels.bluebubbles.group", cfg.Channels.BlueBubbles.Group)
	if cfg.Channels.HomeAssistant.Enabled {
		baseURL := strings.TrimSpace(cfg.Channels.HomeAssistant.BaseURL)
		if baseURL == "" {
			issues = append(issues, "channels.homeassistant.base_url is required when enabled")
		} else if parsed, err := url.Parse(baseURL); err != nil || parsed == nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
			issues = append(issues, "channels.homeassistant.base_url must be a valid URL")
		} else if parsed.Scheme != "http" && parsed.Scheme != "https" {
			issues = append(issues, "channels.homeassistant.base_url scheme must be http or https")
		}
		if strings.TrimSpace(cfg.Channels.HomeAssistant.Token) == "" {
			issues = append(issues, "channels.homeassistant.token is required when enabled")
		}
		if cfg.Channels.HomeAssistant.Timeout < 0 {
			issues = append(issues, "channels.homeassistant.timeout must be >= 0")
		}
	}
	if cfg.Channels.Slack.Canvas.Enabled {
		command := strings.TrimSpace(cfg.Channels.Slack.Canvas.Command)
		if command == "" {
			issues = append(issues, "channels.slack.canvas.command is required when canvas is enabled")
		} else if !strings.HasPrefix(command, "/") {
			issues = append(issues, "channels.slack.canvas.command must start with \"/\"")
		}
		if !validCanvasRole(cfg.Channels.Slack.Canvas.DefaultRole) {
			issues = append(issues, "channels.slack.canvas.default_role must be \"viewer\", \"editor\", or \"admin\"")
		}
		if !validCanvasRole(cfg.Channels.Slack.Canvas.Role) {
			issues = append(issues, "channels.slack.canvas.role must be \"viewer\", \"editor\", or \"admin\"")
		}
		for workspaceID, role := range cfg.Channels.Slack.Canvas.WorkspaceRoles {
			if strings.TrimSpace(workspaceID) == "" {
				issues = append(issues, "channels.slack.canvas.workspace_roles has an empty workspace id")
				continue
			}
			if !validCanvasRole(role) {
				issues = append(issues, fmt.Sprintf("channels.slack.canvas.workspace_roles[%s] must be \"viewer\", \"editor\", or \"admin\"", workspaceID))
			}
		}
		for workspaceID, users := range cfg.Channels.Slack.Canvas.UserRoles {
			if strings.TrimSpace(workspaceID) == "" {
				issues = append(issues, "channels.slack.canvas.user_roles has an empty workspace id")
				continue
			}
			for userID, role := range users {
				if strings.TrimSpace(userID) == "" {
					issues = append(issues, fmt.Sprintf("channels.slack.canvas.user_roles[%s] has an empty user id", workspaceID))
					continue
				}
				if !validCanvasRole(role) {
					issues = append(issues, fmt.Sprintf("channels.slack.canvas.user_roles[%s][%s] must be \"viewer\", \"editor\", or \"admin\"", workspaceID, userID))
				}
			}
		}
	}

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
	validateSteeringConfig(&issues, cfg.Steering)
	if !validDMScope(cfg.Session.Scoping.DMScope) {
		issues = append(issues, "session.scoping.dm_scope must be \"main\", \"per-peer\", or \"per-channel-peer\"")
	}
	if !validResetMode(cfg.Session.Scoping.Reset.Mode) {
		issues = append(issues, "session.scoping.reset.mode must be \"never\", \"daily\", \"idle\", or \"daily+idle\"")
	}
	if cfg.Session.Scoping.Reset.AtHour < 0 || cfg.Session.Scoping.Reset.AtHour > 23 {
		issues = append(issues, "session.scoping.reset.at_hour must be between 0 and 23")
	}
	if cfg.Session.Scoping.Reset.IdleMinutes < 0 {
		issues = append(issues, "session.scoping.reset.idle_minutes must be >= 0")
	}
	for convType, resetCfg := range cfg.Session.Scoping.ResetByType {
		if !validConversationType(convType) {
			issues = append(issues, fmt.Sprintf("session.scoping.reset_by_type key %q must be \"dm\", \"group\", or \"thread\"", convType))
		}
		if !validResetMode(resetCfg.Mode) {
			issues = append(issues, fmt.Sprintf("session.scoping.reset_by_type[%s].mode must be \"never\", \"daily\", \"idle\", or \"daily+idle\"", convType))
		}
	}
	for channel, resetCfg := range cfg.Session.Scoping.ResetByChannel {
		if !validResetMode(resetCfg.Mode) {
			issues = append(issues, fmt.Sprintf("session.scoping.reset_by_channel[%s].mode must be \"never\", \"daily\", \"idle\", or \"daily+idle\"", channel))
		}
	}
	validateContextPruning(&issues, cfg.Session.ContextPruning)
	if cfg.Workspace.MaxChars < 0 {
		issues = append(issues, "workspace.max_chars must be >= 0")
	}
	if cfg.CanvasHost.Enabled != nil && *cfg.CanvasHost.Enabled {
		if cfg.CanvasHost.Port <= 0 || cfg.CanvasHost.Port > 65535 {
			issues = append(issues, "canvas_host.port must be between 1 and 65535")
		}
		if strings.TrimSpace(cfg.CanvasHost.Root) == "" {
			issues = append(issues, "canvas_host.root is required when canvas_host is enabled")
		}
		namespace := strings.TrimSpace(cfg.CanvasHost.Namespace)
		if namespace == "" {
			issues = append(issues, "canvas_host.namespace is required when canvas_host is enabled")
		} else if !strings.HasPrefix(namespace, "/") {
			issues = append(issues, "canvas_host.namespace must start with \"/\"")
		}
		if cfg.CanvasHost.LiveReload != nil && cfg.CanvasHost.InjectClient != nil {
			if *cfg.CanvasHost.InjectClient && !*cfg.CanvasHost.LiveReload {
				issues = append(issues, "canvas_host.inject_client requires canvas_host.live_reload to be true")
			}
		}
		if strings.TrimSpace(cfg.Canvas.Tokens.Secret) == "" && strings.TrimSpace(cfg.Auth.JWTSecret) == "" && len(cfg.Auth.APIKeys) == 0 {
			issues = append(issues, "canvas_host.enabled requires auth.jwt_secret, auth.api_keys, or canvas.tokens.secret")
		}
	}
	if cfg.Canvas.Retention.StateMaxAge < 0 {
		issues = append(issues, "canvas.retention.state_max_age must be >= 0")
	}
	if cfg.Canvas.Retention.EventMaxAge < 0 {
		issues = append(issues, "canvas.retention.event_max_age must be >= 0")
	}
	if cfg.Canvas.Retention.StateMaxBytes < 0 {
		issues = append(issues, "canvas.retention.state_max_bytes must be >= 0")
	}
	if cfg.Canvas.Retention.EventMaxBytes < 0 {
		issues = append(issues, "canvas.retention.event_max_bytes must be >= 0")
	}
	if strings.TrimSpace(cfg.Canvas.Actions.DefaultRole) != "" && !validCanvasRole(cfg.Canvas.Actions.DefaultRole) {
		issues = append(issues, "canvas.actions.default_role must be \"viewer\", \"editor\", or \"admin\"")
	}
	if cfg.Canvas.Tokens.TTL < 0 {
		issues = append(issues, "canvas.tokens.ttl must be >= 0")
	}
	if secret := strings.TrimSpace(cfg.Canvas.Tokens.Secret); secret != "" {
		if len(secret) < 32 {
			issues = append(issues, "canvas.tokens.secret must be at least 32 characters for security")
		}
	}
	if cfg.Canvas.Actions.RateLimit.RequestsPerSecond < 0 {
		issues = append(issues, "canvas.actions.rate_limit.requests_per_second must be >= 0")
	}
	if cfg.Canvas.Actions.RateLimit.BurstSize < 0 {
		issues = append(issues, "canvas.actions.rate_limit.burst_size must be >= 0")
	}
	if cfg.Canvas.Audit.SampleRate < 0 || cfg.Canvas.Audit.SampleRate > 1 {
		issues = append(issues, "canvas.audit.sample_rate must be between 0 and 1")
	}
	if cfg.Canvas.Audit.MaxFieldSize < 0 {
		issues = append(issues, "canvas.audit.max_field_size must be >= 0")
	}
	if cfg.Canvas.Audit.BufferSize < 0 {
		issues = append(issues, "canvas.audit.buffer_size must be >= 0")
	}
	if cfg.Canvas.Audit.FlushInterval < 0 {
		issues = append(issues, "canvas.audit.flush_interval must be >= 0")
	}

	if strings.TrimSpace(cfg.Artifacts.MetadataBackend) != "" {
		switch strings.ToLower(strings.TrimSpace(cfg.Artifacts.MetadataBackend)) {
		case "file", "database", "db":
		default:
			issues = append(issues, "artifacts.metadata_backend must be \"file\" or \"database\"")
		}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Artifacts.MetadataBackend), "database") || strings.EqualFold(strings.TrimSpace(cfg.Artifacts.MetadataBackend), "db") {
		if strings.TrimSpace(cfg.Database.URL) == "" {
			issues = append(issues, "artifacts.metadata_backend requires database.url to be set")
		}
	}
	if backend := strings.ToLower(strings.TrimSpace(cfg.Artifacts.Backend)); backend == "s3" || backend == "minio" {
		if strings.TrimSpace(cfg.Artifacts.S3Bucket) == "" {
			issues = append(issues, "artifacts.s3_bucket is required for s3/minio backends")
		}
	}

	defaultProvider := strings.TrimSpace(cfg.LLM.DefaultProvider)
	if defaultProvider != "" {
		providerID, profileID := splitProviderProfileID(defaultProvider)
		providerKey := strings.ToLower(strings.TrimSpace(providerID))
		providerCfg, ok := cfg.LLM.Providers[providerKey]
		if !ok {
			providerCfg, ok = cfg.LLM.Providers[providerID]
		}
		if !ok {
			issues = append(issues, fmt.Sprintf("llm.providers missing entry for default_provider %q", cfg.LLM.DefaultProvider))
		} else if profileID != "" {
			if providerCfg.Profiles == nil {
				issues = append(issues, fmt.Sprintf("llm.providers.%s.profiles missing profile %q", providerKey, profileID))
			} else if _, ok := providerCfg.Profiles[profileID]; !ok {
				issues = append(issues, fmt.Sprintf("llm.providers.%s.profiles missing profile %q", providerKey, profileID))
			}
		}
	}
	for i, entry := range cfg.LLM.FallbackChain {
		value := strings.TrimSpace(entry)
		if value == "" {
			continue
		}
		providerID, profileID := splitProviderProfileID(value)
		providerKey := strings.ToLower(strings.TrimSpace(providerID))
		providerCfg, ok := cfg.LLM.Providers[providerKey]
		if !ok {
			providerCfg, ok = cfg.LLM.Providers[providerID]
		}
		if !ok {
			issues = append(issues, fmt.Sprintf("llm.fallback_chain[%d] references unknown provider %q", i, entry))
			continue
		}
		if profileID != "" {
			if providerCfg.Profiles == nil {
				issues = append(issues, fmt.Sprintf("llm.fallback_chain[%d] references missing profile %q", i, profileID))
			} else if _, ok := providerCfg.Profiles[profileID]; !ok {
				issues = append(issues, fmt.Sprintf("llm.fallback_chain[%d] references missing profile %q", i, profileID))
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

	// JWT secret validation: require minimum 32 bytes when set
	if jwtSecret := strings.TrimSpace(cfg.Auth.JWTSecret); jwtSecret != "" {
		if len(jwtSecret) < 32 {
			issues = append(issues, "auth.jwt_secret must be at least 32 characters for security")
		}
	}

	if provider := strings.ToLower(strings.TrimSpace(cfg.Tools.WebSearch.Provider)); provider != "" {
		switch provider {
		case "searxng", "brave", "duckduckgo":
		default:
			issues = append(issues, "tools.websearch.provider must be \"searxng\", \"brave\", or \"duckduckgo\"")
		}
		switch provider {
		case "searxng":
			if strings.TrimSpace(cfg.Tools.WebSearch.URL) == "" {
				issues = append(issues, "tools.websearch.url is required when provider is \"searxng\"")
			}
		case "brave":
			if strings.TrimSpace(cfg.Tools.WebSearch.BraveAPIKey) == "" {
				issues = append(issues, "tools.websearch.brave_api_key is required when provider is \"brave\"")
			}
		}
	}
	if browserURL := strings.TrimSpace(cfg.Tools.Browser.URL); browserURL != "" {
		parsed, err := url.Parse(browserURL)
		if err != nil || parsed == nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
			issues = append(issues, "tools.browser.url must be a valid URL when set")
		} else {
			switch strings.ToLower(parsed.Scheme) {
			case "http", "https", "ws", "wss":
			default:
				issues = append(issues, "tools.browser.url scheme must be http, https, ws, or wss")
			}
		}
	}
	if cfg.Tools.WebFetch.MaxChars < 0 {
		issues = append(issues, "tools.web_fetch.max_chars must be >= 0")
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
	if cfg.Tools.Execution.ResultGuard.MaxChars < 0 {
		issues = append(issues, "tools.execution.result_guard.max_chars must be >= 0")
	}
	if profile := strings.ToLower(strings.TrimSpace(cfg.Tools.Execution.Approval.Profile)); profile != "" {
		switch profile {
		case "coding", "messaging", "readonly", "full", "minimal":
		default:
			issues = append(issues, "tools.execution.approval.profile must be \"coding\", \"messaging\", \"readonly\", \"full\", or \"minimal\"")
		}
	}

	if cfg.Gateway.WebhookHooks.Enabled {
		if strings.TrimSpace(cfg.Gateway.WebhookHooks.Token) == "" {
			issues = append(issues, "gateway.webhook_hooks.token is required when webhook hooks are enabled")
		}
		if cfg.Gateway.WebhookHooks.MaxBodyBytes < 0 {
			issues = append(issues, "gateway.webhook_hooks.max_body_bytes must be >= 0")
		}
		for i, mapping := range cfg.Gateway.WebhookHooks.Mappings {
			if strings.TrimSpace(mapping.Path) == "" {
				issues = append(issues, fmt.Sprintf("gateway.webhook_hooks.mappings[%d].path is required", i))
			}
			handler := strings.ToLower(strings.TrimSpace(mapping.Handler))
			switch handler {
			case "agent", "wake", "custom":
			default:
				issues = append(issues, fmt.Sprintf("gateway.webhook_hooks.mappings[%d].handler must be agent, wake, or custom", i))
			}
		}
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
			if job.Retry.MaxRetries < 0 {
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].retry.max_retries must be >= 0", i))
			}
			if job.Retry.Backoff < 0 {
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].retry.backoff must be >= 0", i))
			}
			if job.Retry.MaxBackoff < 0 {
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].retry.max_backoff must be >= 0", i))
			}
			switch strings.ToLower(strings.TrimSpace(job.Type)) {
			case "webhook":
				if job.Webhook == nil || strings.TrimSpace(job.Webhook.URL) == "" {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.url is required for webhook jobs", i))
				}
				if job.Webhook != nil && job.Webhook.Auth != nil {
					authType := strings.ToLower(strings.TrimSpace(job.Webhook.Auth.Type))
					switch authType {
					case "bearer":
						if strings.TrimSpace(job.Webhook.Auth.Token) == "" {
							issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.auth.token is required for bearer auth", i))
						}
					case "basic":
						if strings.TrimSpace(job.Webhook.Auth.User) == "" {
							issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.auth.user is required for basic auth", i))
						}
					case "api_key":
						if strings.TrimSpace(job.Webhook.Auth.Token) == "" {
							issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.auth.token is required for api_key auth", i))
						}
						if strings.TrimSpace(job.Webhook.Auth.Header) == "" {
							issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.auth.header is required for api_key auth", i))
						}
					case "":
						issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.auth.type is required", i))
					default:
						issues = append(issues, fmt.Sprintf("cron.jobs[%d].webhook.auth.type must be bearer, basic, or api_key", i))
					}
				}
			case "message":
				if job.Message == nil {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].message is required for message jobs", i))
					break
				}
				if strings.TrimSpace(job.Message.Channel) == "" || strings.TrimSpace(job.Message.ChannelID) == "" {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].message.channel and channel_id are required for message jobs", i))
				}
				if strings.TrimSpace(job.Message.Content) == "" && strings.TrimSpace(job.Message.Template) == "" {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].message.content or template is required for message jobs", i))
				}
				if len(job.Message.Tools) > 0 {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].message.tools only applies to agent jobs", i))
				}
			case "agent":
				if job.Message == nil {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].message is required for agent jobs", i))
					break
				}
				channel := strings.TrimSpace(job.Message.Channel)
				channelID := strings.TrimSpace(job.Message.ChannelID)
				if (channel == "" && channelID != "") || (channel != "" && channelID == "") {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].message.channel and channel_id must both be set or empty for agent jobs", i))
				}
				if strings.TrimSpace(job.Message.Content) == "" && strings.TrimSpace(job.Message.Template) == "" {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].message.content or template is required for agent jobs", i))
				}
			case "custom":
				if job.Custom == nil || strings.TrimSpace(job.Custom.Handler) == "" {
					issues = append(issues, fmt.Sprintf("cron.jobs[%d].custom.handler is required for custom jobs", i))
				}
			default:
				issues = append(issues, fmt.Sprintf("cron.jobs[%d].type must be message, agent, webhook, or custom", i))
			}
		}
	}
	if cfg.LLM.Routing.UnhealthyCooldown < 0 {
		issues = append(issues, "llm.routing.unhealthy_cooldown must be >= 0")
	}
	if cfg.Plugins.Isolation.Enabled {
		backend := strings.ToLower(strings.TrimSpace(cfg.Plugins.Isolation.Backend))
		if backend == "" {
			issues = append(issues, "plugins.isolation.backend is required when isolation is enabled")
		} else {
			switch backend {
			case "daytona":
				// Supported backend (credentials may be supplied via config/env).
			case "docker", "firecracker":
				issues = append(issues, fmt.Sprintf("plugins.isolation.backend %q is not implemented; disable plugins.isolation.enabled", backend))
			default:
				issues = append(issues, fmt.Sprintf("plugins.isolation.backend %q is not supported; choose daytona, docker, or firecracker", backend))
			}
		}
	}

	if pluginIssues := pluginValidationIssues(cfg); len(pluginIssues) > 0 {
		issues = append(issues, pluginIssues...)
	}

	if cfg.Observability.Tracing.Enabled {
		if cfg.Observability.Tracing.SamplingRate < 0 || cfg.Observability.Tracing.SamplingRate > 1 {
			issues = append(issues, "observability.tracing.sampling_rate must be between 0 and 1")
		}
		if strings.TrimSpace(cfg.Observability.Tracing.Endpoint) == "" {
			issues = append(issues, "observability.tracing.endpoint is required when tracing is enabled")
		}
	}
	if cfg.Security.Posture.Enabled && cfg.Security.Posture.AutoRemediation.Enabled {
		mode := strings.ToLower(strings.TrimSpace(cfg.Security.Posture.AutoRemediation.Mode))
		if mode != "" && mode != "lockdown" && mode != "warn_only" {
			issues = append(issues, "security.posture.auto_remediation.mode must be \"lockdown\" or \"warn_only\"")
		}
	}

	if len(issues) > 0 {
		return &ConfigValidationError{Issues: issues}
	}

	return nil
}

func validateChannelPolicy(issues *[]string, field string, cfg ChannelPolicyConfig) {
	policy := strings.ToLower(strings.TrimSpace(cfg.Policy))
	if policy == "" {
		return
	}
	if !validChannelPolicy(policy) {
		*issues = append(*issues, fmt.Sprintf("%s.policy must be \"open\", \"allowlist\", \"pairing\", or \"disabled\"", field))
	}
}

func validChannelPolicy(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "open", "allowlist", "pairing", "disabled":
		return true
	default:
		return false
	}
}

func splitProviderProfileID(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	for _, sep := range []string{":", "@", "/"} {
		if parts := strings.SplitN(value, sep, 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	return value, ""
}

func validCanvasRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "viewer", "editor", "admin":
		return true
	default:
		return false
	}
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

func validDMScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "main", "per-peer", "per-channel-peer":
		return true
	default:
		return false
	}
}

func validResetMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "never", "daily", "idle", "daily+idle":
		return true
	default:
		return false
	}
}

func validateSteeringConfig(issues *[]string, cfg SteeringConfig) {
	if !cfg.Enabled {
		return
	}
	for i, rule := range cfg.Rules {
		label := rule.ID
		if label == "" {
			label = fmt.Sprintf("index %d", i)
		}
		if strings.TrimSpace(rule.Prompt) == "" {
			*issues = append(*issues, fmt.Sprintf("steering.rules[%s].prompt is required", label))
		}
		if rule.TimeWindow.After != "" {
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(rule.TimeWindow.After)); err != nil {
				*issues = append(*issues, fmt.Sprintf("steering.rules[%s].time_window.after must be RFC3339", label))
			}
		}
		if rule.TimeWindow.Before != "" {
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(rule.TimeWindow.Before)); err != nil {
				*issues = append(*issues, fmt.Sprintf("steering.rules[%s].time_window.before must be RFC3339", label))
			}
		}
	}
}

func validateContextPruning(issues *[]string, cfg ContextPruningConfig) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != "" && mode != "off" && mode != "cache-ttl" {
		*issues = append(*issues, "session.context_pruning.mode must be \"off\" or \"cache-ttl\"")
	}
	if cfg.TTL != nil && *cfg.TTL < 0 {
		*issues = append(*issues, "session.context_pruning.ttl must be >= 0")
	}
	if cfg.KeepLastAssistants != nil && *cfg.KeepLastAssistants < 0 {
		*issues = append(*issues, "session.context_pruning.keep_last_assistants must be >= 0")
	}
	if cfg.SoftTrimRatio != nil && (*cfg.SoftTrimRatio < 0 || *cfg.SoftTrimRatio > 1) {
		*issues = append(*issues, "session.context_pruning.soft_trim_ratio must be between 0 and 1")
	}
	if cfg.HardClearRatio != nil && (*cfg.HardClearRatio < 0 || *cfg.HardClearRatio > 1) {
		*issues = append(*issues, "session.context_pruning.hard_clear_ratio must be between 0 and 1")
	}
	if cfg.MinPrunableToolChars != nil && *cfg.MinPrunableToolChars < 0 {
		*issues = append(*issues, "session.context_pruning.min_prunable_tool_chars must be >= 0")
	}
	if cfg.SoftTrim.MaxChars != nil && *cfg.SoftTrim.MaxChars < 0 {
		*issues = append(*issues, "session.context_pruning.soft_trim.max_chars must be >= 0")
	}
	if cfg.SoftTrim.HeadChars != nil && *cfg.SoftTrim.HeadChars < 0 {
		*issues = append(*issues, "session.context_pruning.soft_trim.head_chars must be >= 0")
	}
	if cfg.SoftTrim.TailChars != nil && *cfg.SoftTrim.TailChars < 0 {
		*issues = append(*issues, "session.context_pruning.soft_trim.tail_chars must be >= 0")
	}
}

func validConversationType(convType string) bool {
	switch strings.ToLower(strings.TrimSpace(convType)) {
	case "dm", "group", "thread":
		return true
	default:
		return false
	}
}

// GetMarkdownTableMode returns the configured markdown table mode for a channel.
// Returns the configured value, or the channel's default if not configured.
func (c *Config) GetMarkdownTableMode(channel string) string {
	ch := strings.ToLower(strings.TrimSpace(channel))

	var configured string
	switch ch {
	case "telegram":
		configured = c.Channels.Telegram.Markdown.Tables
	case "discord":
		configured = c.Channels.Discord.Markdown.Tables
	case "slack":
		configured = c.Channels.Slack.Markdown.Tables
	case "whatsapp":
		configured = c.Channels.WhatsApp.Markdown.Tables
	case "signal":
		configured = c.Channels.Signal.Markdown.Tables
	}

	if configured != "" {
		return configured
	}

	// Return channel-specific defaults
	switch ch {
	case "signal", "whatsapp", "sms":
		return "bullets"
	case "slack", "discord", "telegram", "matrix":
		return "code"
	default:
		return "off"
	}
}
