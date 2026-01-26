package config

import (
	"fmt"
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
}

// GatewayConfig configures gateway-level message routing and processing.
type GatewayConfig struct {
	Broadcast BroadcastConfig `yaml:"broadcast"`
}

// ClusterConfig controls multi-gateway behavior.
type ClusterConfig struct {
	// Enabled turns on cluster-aware behavior.
	Enabled bool `yaml:"enabled"`

	// NodeID uniquely identifies this gateway instance.
	NodeID string `yaml:"node_id"`

	// AllowMultipleGateways bypasses the singleton gateway lock.
	AllowMultipleGateways bool `yaml:"allow_multiple_gateways"`

	// SessionLocks controls distributed session locking.
	SessionLocks SessionLockConfig `yaml:"session_locks"`
}

// SessionLockConfig configures distributed session locks.
type SessionLockConfig struct {
	// Enabled uses DB-backed session locks.
	Enabled bool `yaml:"enabled"`

	// TTL is the lock lease duration.
	TTL time.Duration `yaml:"ttl"`

	// RefreshInterval is how often leases are renewed.
	RefreshInterval time.Duration `yaml:"refresh_interval"`

	// AcquireTimeout is how long to wait for a lock.
	AcquireTimeout time.Duration `yaml:"acquire_timeout"`

	// PollInterval controls backoff when lock is held by another owner.
	PollInterval time.Duration `yaml:"poll_interval"`
}

// AttentionConfig controls the attention feed integration.
type AttentionConfig struct {
	// Enabled turns on the attention feed and tools.
	Enabled bool `yaml:"enabled"`
	// InjectInPrompt adds a summary of active items to the system prompt.
	InjectInPrompt bool `yaml:"inject_in_prompt"`
	// MaxItems limits how many items are injected into the prompt.
	MaxItems int `yaml:"max_items"`
}

// SteeringConfig controls conditional prompt injection rules.
type SteeringConfig struct {
	// Enabled toggles steering rule evaluation.
	Enabled bool `yaml:"enabled"`
	// Rules define conditional prompt injections.
	Rules []SteeringRule `yaml:"rules"`
}

// SteeringRule defines a conditional prompt injection.
type SteeringRule struct {
	// ID is an optional stable identifier for the rule.
	ID string `yaml:"id"`
	// Name is a human-readable label for observability.
	Name string `yaml:"name"`
	// Prompt is the injected text when the rule matches.
	Prompt string `yaml:"prompt"`
	// Enabled toggles this rule. Defaults to true when omitted.
	Enabled *bool `yaml:"enabled"`
	// Priority controls ordering when multiple rules match (higher first).
	Priority int `yaml:"priority"`
	// Roles restrict matches to specific message roles.
	Roles []string `yaml:"roles"`
	// Channels restrict matches to specific channel types.
	Channels []string `yaml:"channels"`
	// Agents restrict matches to specific agent IDs.
	Agents []string `yaml:"agents"`
	// Tags restrict matches to metadata tags (any match).
	Tags []string `yaml:"tags"`
	// Contains restricts matches to messages containing any of the substrings.
	Contains []string `yaml:"contains"`
	// Metadata requires specific metadata key/value pairs.
	Metadata map[string]string `yaml:"metadata"`
	// TimeWindow restricts matches to a time range.
	TimeWindow SteeringTimeWindow `yaml:"time_window"`
}

// SteeringTimeWindow restricts rule matching by absolute time.
type SteeringTimeWindow struct {
	// After is an RFC3339 timestamp; now must be after this to match.
	After string `yaml:"after"`
	// Before is an RFC3339 timestamp; now must be before this to match.
	Before string `yaml:"before"`
}

// CommandsConfig configures gateway command handling.
type CommandsConfig struct {
	// Enabled toggles text command handling. Defaults to true.
	Enabled *bool `yaml:"enabled"`

	// AllowFrom restricts command-only messages by channel/provider.
	// Example: {"telegram": ["12345", "67890"], "discord": ["*"]}
	AllowFrom map[string][]string `yaml:"allow_from"`

	// InlineAllowFrom restricts inline command shortcuts by channel/provider.
	// When empty, inline commands are disabled by default.
	InlineAllowFrom map[string][]string `yaml:"inline_allow_from"`

	// InlineCommands lists command names that can run inline (without leading slash).
	InlineCommands []string `yaml:"inline_commands"`
}

// BroadcastConfig configures broadcast groups for message routing.
type BroadcastConfig struct {
	// Strategy defines how messages are processed: "parallel" or "sequential".
	Strategy string `yaml:"strategy"`

	// Groups maps peer_id to a list of agent_ids that should process messages.
	// When a message arrives from a peer in this map, it will be routed to all
	// specified agents instead of the default single agent.
	Groups map[string][]string `yaml:"groups"`
}

type ServerConfig struct {
	Host        string `yaml:"host"`
	GRPCPort    int    `yaml:"grpc_port"`
	HTTPPort    int    `yaml:"http_port"`
	MetricsPort int    `yaml:"metrics_port"`
}

// CanvasHostConfig configures the dedicated canvas host.
type CanvasHostConfig struct {
	Enabled      *bool  `yaml:"enabled"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Root         string `yaml:"root"`
	Namespace    string `yaml:"namespace"`
	LiveReload   *bool  `yaml:"live_reload"`
	InjectClient *bool  `yaml:"inject_client"`
	AutoIndex    *bool  `yaml:"auto_index"`
	A2UIRoot     string `yaml:"a2ui_root"`
}

// CanvasConfig configures canvas persistence and retention.
type CanvasConfig struct {
	Retention CanvasRetentionConfig `yaml:"retention"`
	Tokens    CanvasTokenConfig     `yaml:"tokens"`
	Actions   CanvasActionConfig    `yaml:"actions"`
	Audit     audit.Config          `yaml:"audit"`
}

// CanvasRetentionConfig controls how long canvas state and events are retained.
type CanvasRetentionConfig struct {
	StateMaxAge   time.Duration `yaml:"state_max_age"`
	EventMaxAge   time.Duration `yaml:"event_max_age"`
	StateMaxBytes int64         `yaml:"state_max_bytes"`
	EventMaxBytes int64         `yaml:"event_max_bytes"`
}

// CanvasTokenConfig controls signed canvas access tokens.
type CanvasTokenConfig struct {
	Secret string        `yaml:"secret"`
	TTL    time.Duration `yaml:"ttl"`
}

// CanvasActionConfig configures canvas UI action handling.
type CanvasActionConfig struct {
	RateLimit   ratelimit.Config `yaml:"rate_limit"`
	DefaultRole string           `yaml:"default_role"`
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
	DefaultAgentID string               `yaml:"default_agent_id"`
	SlackScope     string               `yaml:"slack_scope"`
	DiscordScope   string               `yaml:"discord_scope"`
	Memory         MemoryConfig         `yaml:"memory"`
	Heartbeat      HeartbeatConfig      `yaml:"heartbeat"`
	MemoryFlush    MemoryFlushConfig    `yaml:"memory_flush"`
	ContextPruning ContextPruningConfig `yaml:"context_pruning"`
	Scoping        SessionScopeConfig   `yaml:"scoping"`
}

// SessionScopeConfig controls advanced session scoping behavior.
type SessionScopeConfig struct {
	// DMScope controls how DM sessions are scoped:
	// - "main": all DMs share one session (default)
	// - "per-peer": separate session per peer
	// - "per-channel-peer": separate session per channel+peer combination
	DMScope string `yaml:"dm_scope"`

	// IdentityLinks maps canonical IDs to platform-specific peer IDs.
	// Format: canonical_id -> ["provider:peer_id", "provider:peer_id", ...]
	// This allows cross-channel identity resolution for unified sessions.
	IdentityLinks map[string][]string `yaml:"identity_links"`

	// Reset configures default session reset behavior.
	Reset ResetConfig `yaml:"reset"`

	// ResetByType configures reset behavior per conversation type (dm, group, thread).
	ResetByType map[string]ResetConfig `yaml:"reset_by_type"`

	// ResetByChannel configures reset behavior per channel (slack, discord, etc).
	ResetByChannel map[string]ResetConfig `yaml:"reset_by_channel"`
}

// ResetConfig controls when sessions are automatically reset.
type ResetConfig struct {
	// Mode is the reset mode: "daily", "idle", "daily+idle", or "never" (default).
	Mode string `yaml:"mode"`

	// AtHour is the hour (0-23) to reset sessions when mode includes "daily".
	AtHour int `yaml:"at_hour"`

	// IdleMinutes is the number of minutes of inactivity before reset when mode includes "idle".
	IdleMinutes int `yaml:"idle_minutes"`
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

// ContextPruningConfig controls in-memory tool result pruning for sessions.
type ContextPruningConfig struct {
	Mode                 string                  `yaml:"mode"`
	TTL                  *time.Duration          `yaml:"ttl"`
	KeepLastAssistants   *int                    `yaml:"keep_last_assistants"`
	SoftTrimRatio        *float64                `yaml:"soft_trim_ratio"`
	HardClearRatio       *float64                `yaml:"hard_clear_ratio"`
	MinPrunableToolChars *int                    `yaml:"min_prunable_tool_chars"`
	Tools                ContextPruningToolMatch `yaml:"tools"`
	SoftTrim             ContextPruningSoftTrim  `yaml:"soft_trim"`
	HardClear            ContextPruningHardClear `yaml:"hard_clear"`
}

// ContextPruningToolMatch selects which tool results can be trimmed.
type ContextPruningToolMatch struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

// ContextPruningSoftTrim configures soft trimming of tool result content.
type ContextPruningSoftTrim struct {
	MaxChars  *int `yaml:"max_chars"`
	HeadChars *int `yaml:"head_chars"`
	TailChars *int `yaml:"tail_chars"`
}

// ContextPruningHardClear configures hard clearing of tool result content.
type ContextPruningHardClear struct {
	Enabled     *bool  `yaml:"enabled"`
	Placeholder string `yaml:"placeholder"`
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
	Teams    TeamsConfig    `yaml:"teams"`
	Email    EmailConfig    `yaml:"email"`
}

type ChannelPolicyConfig struct {
	// Policy controls access: "open", "allowlist", "pairing", or "disabled".
	Policy string `yaml:"policy"`
	// AllowFrom is a list of sender identifiers allowed for this policy.
	AllowFrom []string `yaml:"allow_from"`
}

// ChannelMarkdownConfig configures markdown processing for a channel.
type ChannelMarkdownConfig struct {
	// Tables specifies how to handle markdown tables: "off", "bullets", or "code".
	// - "off": Leave tables unchanged (for channels that support markdown tables)
	// - "bullets": Convert tables to bullet lists (for channels like Signal, WhatsApp)
	// - "code": Wrap tables in code blocks (for channels like Slack, Discord)
	// Default depends on channel type.
	Tables string `yaml:"tables"`
}

type WhatsAppConfig struct {
	Enabled      bool   `yaml:"enabled"`
	SessionPath  string `yaml:"session_path"`
	MediaPath    string `yaml:"media_path"`
	SyncContacts bool   `yaml:"sync_contacts"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Presence WhatsAppPresenceConfig `yaml:"presence"`
	Markdown ChannelMarkdownConfig  `yaml:"markdown"`
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

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Presence SignalPresenceConfig  `yaml:"presence"`
	Markdown ChannelMarkdownConfig `yaml:"markdown"`
}

type SignalPresenceConfig struct {
	SendReadReceipts bool `yaml:"send_read_receipts"`
	SendTyping       bool `yaml:"send_typing"`
}

type IMessageConfig struct {
	Enabled      bool   `yaml:"enabled"`
	DatabasePath string `yaml:"database_path"`
	PollInterval string `yaml:"poll_interval"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
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

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	Webhook  string `yaml:"webhook"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Markdown ChannelMarkdownConfig `yaml:"markdown"`
}

type DiscordConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	AppID    string `yaml:"app_id"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Markdown ChannelMarkdownConfig `yaml:"markdown"`
}

type SlackConfig struct {
	Enabled       bool   `yaml:"enabled"`
	BotToken      string `yaml:"bot_token"`
	AppToken      string `yaml:"app_token"`
	SigningSecret string `yaml:"signing_secret"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Markdown ChannelMarkdownConfig `yaml:"markdown"`
	Canvas   SlackCanvasConfig     `yaml:"canvas"`
}

type SlackCanvasConfig struct {
	Enabled           bool                         `yaml:"enabled"`
	Command           string                       `yaml:"command"`
	ShortcutCallback  string                       `yaml:"shortcut_callback"`
	AllowedWorkspaces []string                     `yaml:"allowed_workspaces"`
	Role              string                       `yaml:"role"`
	DefaultRole       string                       `yaml:"default_role"`
	WorkspaceRoles    map[string]string            `yaml:"workspace_roles"`
	UserRoles         map[string]map[string]string `yaml:"user_roles"`
}

type TeamsConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TenantID     string `yaml:"tenant_id"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// WebhookURL is the public URL for receiving Teams notifications
	WebhookURL string `yaml:"webhook_url"`
	// PollInterval for checking messages when webhooks unavailable (default: 5s)
	PollInterval string `yaml:"poll_interval"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type EmailConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TenantID     string `yaml:"tenant_id"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// UserEmail is the email address to monitor (for app-only auth)
	UserEmail string `yaml:"user_email"`
	// FolderID specifies which folder to monitor (default: inbox)
	FolderID string `yaml:"folder_id"`
	// IncludeRead determines whether to process already-read messages
	IncludeRead bool `yaml:"include_read"`
	// AutoMarkRead marks messages as read after processing
	AutoMarkRead bool `yaml:"auto_mark_read"`
	// PollInterval for checking new emails (default: 30s)
	PollInterval string `yaml:"poll_interval"`
}

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

// LLMRoutingConfig configures provider routing rules.
type LLMRoutingConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Classifier  string        `yaml:"classifier"`
	PreferLocal bool          `yaml:"prefer_local"`
	Rules       []RoutingRule `yaml:"rules"`
	Fallback    RoutingTarget `yaml:"fallback"`
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

type LLMProviderProfileConfig struct {
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
	BaseURL      string `yaml:"base_url"`
}

type LLMProviderConfig struct {
	APIKey       string                              `yaml:"api_key"`
	DefaultModel string                              `yaml:"default_model"`
	BaseURL      string                              `yaml:"base_url"`
	Profiles     map[string]LLMProviderProfileConfig `yaml:"profiles"`
}

type ToolsConfig struct {
	Sandbox      SandboxConfig       `yaml:"sandbox"`
	Browser      BrowserConfig       `yaml:"browser"`
	ComputerUse  ComputerUseConfig   `yaml:"computer_use"`
	WebSearch    WebSearchConfig     `yaml:"websearch"`
	WebFetch     WebFetchConfig      `yaml:"web_fetch"`
	MemorySearch MemorySearchConfig  `yaml:"memory_search"`
	FactExtract  FactExtractConfig   `yaml:"fact_extraction"`
	Links        LinksConfig         `yaml:"links"`
	Notes        string              `yaml:"notes"`
	NotesFile    string              `yaml:"notes_file"`
	Execution    ToolExecutionConfig `yaml:"execution"`
	Elevated     ElevatedConfig      `yaml:"elevated"`
	Jobs         ToolJobsConfig      `yaml:"jobs"`
	ServiceNow   ServiceNowConfig    `yaml:"servicenow"`
}

type ServiceNowConfig struct {
	Enabled     bool   `yaml:"enabled"`
	InstanceURL string `yaml:"instance_url"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
}

// FactExtractConfig controls the structured fact extraction tool.
type FactExtractConfig struct {
	Enabled  bool `yaml:"enabled"`
	MaxFacts int  `yaml:"max_facts"`
}

// ComputerUseConfig controls the Claude computer use tool routing.
type ComputerUseConfig struct {
	// Enabled registers the computer use tool in the runtime.
	Enabled bool `yaml:"enabled"`
	// EdgeID selects the default edge to target for computer use.
	EdgeID string `yaml:"edge_id"`
	// DisplayWidthPx overrides the display width in pixels when metadata is unavailable.
	DisplayWidthPx int `yaml:"display_width_px"`
	// DisplayHeightPx overrides the display height in pixels when metadata is unavailable.
	DisplayHeightPx int `yaml:"display_height_px"`
	// DisplayNumber overrides the display number (0-based) when metadata is unavailable.
	DisplayNumber int `yaml:"display_number"`
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
	MaxIterations   int                   `yaml:"max_iterations"`
	Parallelism     int                   `yaml:"parallelism"`
	Timeout         time.Duration         `yaml:"timeout"`
	MaxAttempts     int                   `yaml:"max_attempts"`
	RetryBackoff    time.Duration         `yaml:"retry_backoff"`
	DisableEvents   bool                  `yaml:"disable_events"`
	MaxToolCalls    int                   `yaml:"max_tool_calls"`
	RequireApproval []string              `yaml:"require_approval"`
	Async           []string              `yaml:"async"`
	Approval        ApprovalConfig        `yaml:"approval"`
	ResultGuard     ToolResultGuardConfig `yaml:"result_guard"`
}

// ApprovalConfig controls tool approval behavior.
type ApprovalConfig struct {
	// Profile is a pre-configured tool access level.
	// Valid profiles: "coding", "messaging", "readonly", "full", "minimal".
	// When set, the profile's default tools are included in the allowlist.
	Profile string `yaml:"profile"`

	// Allowlist contains tools that are always allowed (no approval needed).
	// Supports patterns like "mcp:*", "read_*", "*" (all).
	// Also supports group references like "group:fs", "group:runtime".
	Allowlist []string `yaml:"allowlist"`

	// Denylist contains tools that are always denied.
	// Supports patterns and group references like Allowlist.
	Denylist []string `yaml:"denylist"`

	// SafeBins are stdin-only tools that are safe to auto-allow.
	SafeBins []string `yaml:"safe_bins"`

	// SkillAllowlist auto-allows tools defined by enabled skills.
	SkillAllowlist *bool `yaml:"skill_allowlist"`

	// AskFallback queues approval when UI is unavailable instead of denying.
	AskFallback *bool `yaml:"ask_fallback"`

	// DefaultDecision when no rule matches: "allowed", "denied", or "pending".
	DefaultDecision string `yaml:"default_decision"`

	// RequestTTL is how long approval requests remain valid.
	RequestTTL time.Duration `yaml:"request_ttl"`
}

// ToolResultGuardConfig controls redaction of tool results before persistence.
type ToolResultGuardConfig struct {
	Enabled         bool     `yaml:"enabled"`
	MaxChars        int      `yaml:"max_chars"`
	Denylist        []string `yaml:"denylist"`
	RedactPatterns  []string `yaml:"redact_patterns"`
	RedactionText   string   `yaml:"redaction_text"`
	TruncateSuffix  string   `yaml:"truncate_suffix"`
	SanitizeSecrets bool     `yaml:"sanitize_secrets"` // Applies builtin secret detection patterns
}

// ElevatedConfig controls elevated tool execution behavior and allowlists.
type ElevatedConfig struct {
	// Enabled gates elevated execution. When nil, elevated is disabled by default.
	Enabled *bool `yaml:"enabled"`

	// AllowFrom maps channel/provider to allowed sender identifiers.
	// Example: {"telegram": ["12345", "67890"], "discord": ["*"]}
	AllowFrom map[string][]string `yaml:"allow_from"`

	// Tools lists tool patterns that elevated-full can bypass approvals for.
	// If empty, defaults to ["execute_code"] in gateway logic.
	Tools []string `yaml:"tools"`
}

type SandboxConfig struct {
	Enabled        bool                  `yaml:"enabled"`
	Backend        string                `yaml:"backend"`
	PoolSize       int                   `yaml:"pool_size"`
	MaxPoolSize    int                   `yaml:"max_pool_size"`
	MinIdle        int                   `yaml:"min_idle"`
	MaxIdleTime    time.Duration         `yaml:"max_idle_time"`
	Timeout        time.Duration         `yaml:"timeout"`
	NetworkEnabled bool                  `yaml:"network_enabled"`
	Limits         ResourceLimits        `yaml:"limits"`
	Snapshots      SandboxSnapshotConfig `yaml:"snapshots"`

	// Mode controls which agents use sandboxing:
	// - "off": sandboxing disabled (default when enabled=false)
	// - "all": all agents use sandboxing
	// - "non-main": only non-main agents use sandboxing (main agent unsandboxed)
	Mode string `yaml:"mode"`

	// Scope controls sandbox isolation level:
	// - "agent": one sandbox container per agent (default)
	// - "session": one sandbox per session
	// - "shared": all agents share one sandbox
	Scope string `yaml:"scope"`

	// WorkspaceRoot is the root directory for sandboxed workspaces.
	WorkspaceRoot string `yaml:"workspace_root"`

	// WorkspaceAccess controls workspace access mode: "readonly" or "readwrite".
	WorkspaceAccess string `yaml:"workspace_access"`
}

// SandboxSnapshotConfig controls Firecracker snapshot behavior.
type SandboxSnapshotConfig struct {
	Enabled         bool          `yaml:"enabled"`
	RefreshInterval time.Duration `yaml:"refresh_interval"`
	MaxAge          time.Duration `yaml:"max_age"`
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

// TasksConfig configures the scheduled tasks system.
type TasksConfig struct {
	// Enabled enables the scheduled tasks scheduler.
	Enabled bool `yaml:"enabled"`

	// WorkerID uniquely identifies this scheduler instance for distributed locking.
	// Defaults to a generated UUID if empty.
	WorkerID string `yaml:"worker_id"`

	// PollInterval is how often the scheduler checks for due tasks.
	// Defaults to 10 seconds.
	PollInterval time.Duration `yaml:"poll_interval"`

	// AcquireInterval is how often the scheduler tries to acquire pending executions.
	// Defaults to 1 second.
	AcquireInterval time.Duration `yaml:"acquire_interval"`

	// LockDuration is how long an execution lock is held.
	// Should be longer than the maximum expected execution time.
	// Defaults to 10 minutes.
	LockDuration time.Duration `yaml:"lock_duration"`

	// MaxConcurrency is the maximum number of concurrent task executions.
	// Defaults to 5.
	MaxConcurrency int `yaml:"max_concurrency"`

	// CleanupInterval is how often stale executions are cleaned up.
	// Defaults to 1 minute.
	CleanupInterval time.Duration `yaml:"cleanup_interval"`

	// StaleTimeout is how long an execution can run before being marked stale.
	// Defaults to 30 minutes.
	StaleTimeout time.Duration `yaml:"stale_timeout"`

	// DefaultTimeout is the default timeout for task execution if not specified on the task.
	// Defaults to 5 minutes.
	DefaultTimeout time.Duration `yaml:"default_timeout"`
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

type WebFetchConfig struct {
	Enabled  bool `yaml:"enabled"`
	MaxChars int  `yaml:"max_chars"`
}

// LinksConfig configures link understanding for extracting and processing URLs.
type LinksConfig struct {
	// Enabled enables link understanding.
	Enabled bool `yaml:"enabled"`

	// MaxLinks is the maximum number of links to extract from a message.
	// Default: 5.
	MaxLinks int `yaml:"max_links"`

	// TimeoutSeconds is the default timeout for link processing.
	// Default: 30.
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// Models are the link processing model configurations.
	Models []LinkModelConfig `yaml:"models"`

	// Scope controls which channels can use link understanding.
	Scope *LinkScopeConfig `yaml:"scope"`
}

// LinkModelConfig defines a link processing model.
type LinkModelConfig struct {
	// Type is the model type: "cli".
	Type string `yaml:"type"`

	// Command is the CLI command to execute.
	Command string `yaml:"command"`

	// Args are the command arguments. Supports template variables:
	// {{LinkUrl}}, {{URL}}, {{url}} - the URL to process
	// {{Channel}}, {{SessionID}}, {{PeerID}}, {{AgentID}} - context info
	Args []string `yaml:"args"`

	// TimeoutSeconds overrides the default timeout for this model.
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// LinkScopeConfig controls which channels can use link understanding.
type LinkScopeConfig struct {
	// Mode is the scope mode: "all", "allowlist", "denylist".
	// Default: "all".
	Mode string `yaml:"mode"`

	// Allowlist is the list of channels to allow when mode is "allowlist".
	// Supports channel names ("telegram"), channel:peer_id ("telegram:123"), or "*".
	Allowlist []string `yaml:"allowlist"`

	// Denylist is the list of channels to deny when mode is "denylist".
	Denylist []string `yaml:"denylist"`
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

// ObservabilityConfig configures tracing and other observability features.
type ObservabilityConfig struct {
	Tracing TracingConfig `yaml:"tracing"`
}

// TracingConfig controls OpenTelemetry tracing.
type TracingConfig struct {
	Enabled        bool              `yaml:"enabled"`
	Endpoint       string            `yaml:"endpoint"`
	ServiceName    string            `yaml:"service_name"`
	ServiceVersion string            `yaml:"service_version"`
	Environment    string            `yaml:"environment"`
	SamplingRate   float64           `yaml:"sampling_rate"`
	Insecure       bool              `yaml:"insecure"`
	Attributes     map[string]string `yaml:"attributes"`
}

// SecurityConfig configures security features.
type SecurityConfig struct {
	Posture SecurityPostureConfig `yaml:"posture"`
}

// SecurityPostureConfig controls continuous security posture auditing.
type SecurityPostureConfig struct {
	Enabled            bool                   `yaml:"enabled"`
	Interval           time.Duration          `yaml:"interval"`
	IncludeFilesystem  *bool                  `yaml:"include_filesystem"`
	IncludeGateway     *bool                  `yaml:"include_gateway"`
	IncludeConfig      *bool                  `yaml:"include_config"`
	CheckSymlinks      *bool                  `yaml:"check_symlinks"`
	AllowGroupReadable bool                   `yaml:"allow_group_readable"`
	EmitEvents         *bool                  `yaml:"emit_events"`
	AutoRemediation    SecurityRemediationCfg `yaml:"auto_remediation"`
}

// SecurityRemediationCfg configures posture remediation behavior.
type SecurityRemediationCfg struct {
	Enabled bool   `yaml:"enabled"`
	Mode    string `yaml:"mode"` // lockdown | warn_only
}

// RAGConfig configures the Retrieval-Augmented Generation pipeline.
type RAGConfig struct {
	// Enabled enables the RAG system.
	Enabled bool `yaml:"enabled"`

	// Store configures the document store backend.
	Store RAGStoreConfig `yaml:"store"`

	// Chunking configures document chunking.
	Chunking RAGChunkingConfig `yaml:"chunking"`

	// Embeddings configures the embedding provider.
	Embeddings RAGEmbeddingsConfig `yaml:"embeddings"`

	// Search configures default search behavior.
	Search RAGSearchConfig `yaml:"search"`

	// ContextInjection configures automatic context injection.
	ContextInjection RAGContextInjectionConfig `yaml:"context_injection"`
}

// RAGStoreConfig configures the RAG document store.
type RAGStoreConfig struct {
	// Backend is the storage backend: "pgvector"
	Backend string `yaml:"backend"`

	// DSN is the PostgreSQL connection string (for pgvector).
	// If empty and UseDatabaseURL is true, uses the main database.url.
	DSN string `yaml:"dsn"`

	// UseDatabaseURL uses the main database.url for pgvector storage.
	UseDatabaseURL bool `yaml:"use_database_url"`

	// Dimension is the embedding vector dimension.
	// Default: 1536 (OpenAI text-embedding-3-small)
	Dimension int `yaml:"dimension"`

	// RunMigrations controls whether to run migrations on startup.
	RunMigrations *bool `yaml:"run_migrations"`
}

// RAGChunkingConfig configures document chunking.
type RAGChunkingConfig struct {
	// ChunkSize is the target chunk size in characters.
	// Default: 1000
	ChunkSize int `yaml:"chunk_size"`

	// ChunkOverlap is the overlap between chunks in characters.
	// Default: 200
	ChunkOverlap int `yaml:"chunk_overlap"`

	// MinChunkSize is the minimum chunk size to keep.
	// Default: 100
	MinChunkSize int `yaml:"min_chunk_size"`
}

// RAGEmbeddingsConfig configures the embedding provider for RAG.
type RAGEmbeddingsConfig struct {
	// Provider is the embedding provider: "openai", "ollama"
	Provider string `yaml:"provider"`

	// APIKey is the API key for the provider.
	APIKey string `yaml:"api_key"`

	// BaseURL is the API base URL (optional).
	BaseURL string `yaml:"base_url"`

	// Model is the embedding model to use.
	// Default: "text-embedding-3-small" for OpenAI
	Model string `yaml:"model"`

	// BatchSize is the maximum texts per embedding batch.
	// Default: 100
	BatchSize int `yaml:"batch_size"`
}

// RAGSearchConfig configures default search behavior.
type RAGSearchConfig struct {
	// DefaultLimit is the default number of results.
	// Default: 5
	DefaultLimit int `yaml:"default_limit"`

	// DefaultThreshold is the default similarity threshold (0-1).
	// Default: 0.7
	DefaultThreshold float32 `yaml:"default_threshold"`

	// MaxResults is the maximum results allowed.
	// Default: 20
	MaxResults int `yaml:"max_results"`
}

// RAGContextInjectionConfig configures automatic context injection.
type RAGContextInjectionConfig struct {
	// Enabled enables automatic RAG context injection.
	Enabled bool `yaml:"enabled"`

	// MaxChunks is the maximum chunks to inject.
	// Default: 5
	MaxChunks int `yaml:"max_chunks"`

	// MaxTokens is the maximum tokens to inject.
	// Default: 2000
	MaxTokens int `yaml:"max_tokens"`

	// MinScore is the minimum similarity score for inclusion.
	// Default: 0.7
	MinScore float32 `yaml:"min_score"`

	// Scope limits retrieval: "global", "agent", "session", "channel"
	// Default: "global"
	Scope string `yaml:"scope"`
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

// EdgeConfig configures the edge protocol for remote tool execution.
type EdgeConfig struct {
	// Enabled enables the edge service for remote edge daemons.
	Enabled bool `yaml:"enabled"`

	// AuthMode controls how edges authenticate: "token", "tofu", or "dev".
	// token: Pre-shared tokens (production)
	// tofu: Trust-On-First-Use with manual approval
	// dev: Accept all connections (development only)
	AuthMode string `yaml:"auth_mode"`

	// Tokens maps edge IDs to pre-shared authentication tokens.
	// Only used when AuthMode is "token".
	Tokens map[string]string `yaml:"tokens"`

	// HeartbeatInterval is how often edges should send heartbeats.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`

	// HeartbeatTimeout is how long before an edge is considered disconnected.
	HeartbeatTimeout time.Duration `yaml:"heartbeat_timeout"`

	// DefaultToolTimeout is the default timeout for tool execution.
	DefaultToolTimeout time.Duration `yaml:"default_tool_timeout"`

	// MaxConcurrentTools limits concurrent tool executions per edge.
	MaxConcurrentTools int `yaml:"max_concurrent_tools"`

	// EventBufferSize is the buffer size for edge events.
	EventBufferSize int `yaml:"event_buffer_size"`
}

// ArtifactConfig configures artifact storage and retention.
type ArtifactConfig struct {
	// Backend specifies storage backend: "local", "s3", or "minio".
	Backend string `yaml:"backend"`

	// LocalPath is the directory for local storage.
	LocalPath string `yaml:"local_path"`

	// MetadataPath is the file path for artifact metadata persistence.
	MetadataPath string `yaml:"metadata_path"`

	// MetadataBackend selects where artifact metadata is stored: "file" or "database".
	MetadataBackend string `yaml:"metadata_backend"`

	// S3Bucket is the bucket name for S3/MinIO storage.
	S3Bucket string `yaml:"s3_bucket"`

	// S3Endpoint is the endpoint URL for MinIO or S3-compatible storage.
	S3Endpoint string `yaml:"s3_endpoint"`

	// S3Region is the AWS region for S3.
	S3Region string `yaml:"s3_region"`

	// S3Prefix is an optional path prefix for all S3 objects.
	S3Prefix string `yaml:"s3_prefix"`

	// S3AccessKeyID is the AWS access key ID for S3 authentication.
	S3AccessKeyID string `yaml:"s3_access_key_id"`

	// S3SecretAccessKey is the AWS secret access key for S3 authentication.
	S3SecretAccessKey string `yaml:"s3_secret_access_key"`

	// TTLs configures retention period by artifact type.
	TTLs map[string]time.Duration `yaml:"ttls"`

	// PruneInterval is how often to cleanup expired artifacts.
	PruneInterval time.Duration `yaml:"prune_interval"`

	// MaxStorageSize is the total quota in bytes (0 = unlimited).
	MaxStorageSize int64 `yaml:"max_storage_size"`

	// Redaction configures rules for sensitive artifacts.
	Redaction ArtifactRedactionConfig `yaml:"redaction"`
}

// ArtifactRedactionConfig controls artifact redaction behavior.
type ArtifactRedactionConfig struct {
	// Enabled toggles redaction.
	Enabled bool `yaml:"enabled"`

	// Types lists artifact types to redact (case-insensitive).
	Types []string `yaml:"types"`

	// MimeTypes lists MIME types to redact (supports wildcards like "image/*").
	MimeTypes []string `yaml:"mime_types"`

	// FilenamePatterns are regex patterns to match against filenames.
	FilenamePatterns []string `yaml:"filename_patterns"`
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
