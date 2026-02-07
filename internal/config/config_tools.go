package config

import "time"

type ToolsConfig struct {
	Sandbox      SandboxConfig       `yaml:"sandbox"`
	Browser      BrowserConfig       `yaml:"browser"`
	ComputerUse  ComputerUseConfig   `yaml:"computer_use"`
	WebSearch    WebSearchConfig     `yaml:"websearch"`
	WebFetch     WebFetchConfig      `yaml:"web_fetch"`
	MemorySearch MemorySearchConfig  `yaml:"memory_search"`
	FactExtract  FactExtractConfig   `yaml:"fact_extraction"`
	Links        LinksConfig         `yaml:"links"`
	Policies     ToolPoliciesConfig  `yaml:"policies"`
	Notes        string              `yaml:"notes"`
	NotesFile    string              `yaml:"notes_file"`
	Execution    ToolExecutionConfig `yaml:"execution"`
	Elevated     ElevatedConfig      `yaml:"elevated"`
	Jobs         ToolJobsConfig      `yaml:"jobs"`
	ServiceNow   ServiceNowConfig    `yaml:"servicenow"`
}

// ToolPoliciesConfig defines default allow/deny policies for tools.
type ToolPoliciesConfig struct {
	// Default policy behavior: "allow" or "deny".
	Default string `yaml:"default"`
	// Rules define per-tool allow/deny behavior.
	Rules []ToolPolicyRule `yaml:"rules"`
}

// ToolPolicyRule defines a policy action for a tool, optionally scoped by channel.
type ToolPolicyRule struct {
	Tool     string   `yaml:"tool"`
	Action   string   `yaml:"action"`   // "allow" | "deny"
	Channels []string `yaml:"channels"` // optional channel filters
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
	Daytona        SandboxDaytonaConfig  `yaml:"daytona"`

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

	// WorkspaceAccess controls workspace access mode: "readonly", "readwrite", "ro", "rw", or "none".
	WorkspaceAccess string `yaml:"workspace_access"`
}

// SandboxDaytonaConfig configures the Daytona sandbox backend.
type SandboxDaytonaConfig struct {
	APIKey         string         `yaml:"api_key"`
	JWTToken       string         `yaml:"jwt_token"`
	OrganizationID string         `yaml:"organization_id"`
	APIURL         string         `yaml:"api_url"`
	Target         string         `yaml:"target"`
	Snapshot       string         `yaml:"snapshot"`
	Image          string         `yaml:"image"`
	SandboxClass   string         `yaml:"class"`
	WorkspaceDir   string         `yaml:"workspace_dir"`
	NetworkAllow   string         `yaml:"network_allow_list"`
	ReuseSandbox   bool           `yaml:"reuse_sandbox"`
	AutoStop       *time.Duration `yaml:"auto_stop_interval"`
	AutoArchive    *time.Duration `yaml:"auto_archive_interval"`
	AutoDelete     *time.Duration `yaml:"auto_delete_interval"`
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

// FactExtractConfig controls the structured fact extraction tool.
type FactExtractConfig struct {
	Enabled  bool `yaml:"enabled"`
	MaxFacts int  `yaml:"max_facts"`
}

type BrowserConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Headless bool   `yaml:"headless"`
	URL      string `yaml:"url"`
}

type WebSearchConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Provider    string `yaml:"provider"`
	URL         string `yaml:"url"`
	BraveAPIKey string `yaml:"brave_api_key"`
}

type WebFetchConfig struct {
	Enabled  bool `yaml:"enabled"`
	MaxChars int  `yaml:"max_chars"`
}

// ToolJobsConfig controls async tool job persistence.
type ToolJobsConfig struct {
	// Retention is how long to keep completed jobs. Default: 24h.
	Retention time.Duration `yaml:"retention"`
	// PruneInterval is how often to prune old jobs. Default: 1h.
	PruneInterval time.Duration `yaml:"prune_interval"`
}

// LinksConfig configures link understanding for extracting and processing URLs.
type LinksConfig struct {
	// Enabled enables link understanding.
	Enabled bool `yaml:"enabled"`

	// MaxLinks is the maximum number of links to extract from a message.
	// Default: 5.
	MaxLinks int `yaml:"max_links"`

	// MaxOutputChars caps the number of characters injected into the prompt.
	// Default: 2000.
	MaxOutputChars int `yaml:"max_output_chars"`

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

type ServiceNowConfig struct {
	Enabled     bool   `yaml:"enabled"`
	InstanceURL string `yaml:"instance_url"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
}
