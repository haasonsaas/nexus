package config

// GatewayConfig configures gateway-level message routing and processing.
type GatewayConfig struct {
	Broadcast BroadcastConfig `yaml:"broadcast"`
	// WebhookHooks configures inbound webhook handlers.
	WebhookHooks WebhookHooksConfig `yaml:"webhook_hooks"`
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

// WebhookHooksConfig configures inbound webhook hook handling.
type WebhookHooksConfig struct {
	// Enabled turns on webhook hooks.
	Enabled bool `yaml:"enabled"`

	// BasePath is the URL path prefix for webhook hooks (default: /hooks).
	BasePath string `yaml:"base_path"`

	// Token is the required authentication token.
	Token string `yaml:"token"`

	// MaxBodyBytes limits the request body size (default: 256KB).
	MaxBodyBytes int64 `yaml:"max_body_bytes"`

	// Mappings define webhook endpoints and their handlers.
	Mappings []WebhookHookMapping `yaml:"mappings"`
}

// WebhookHookMapping defines a webhook hook endpoint.
type WebhookHookMapping struct {
	// Path is the endpoint path (appended to BasePath).
	Path string `yaml:"path"`

	// Name is a human-readable name for this webhook.
	Name string `yaml:"name"`

	// Handler is the handler type (agent, wake, custom).
	Handler string `yaml:"handler"`

	// AgentID targets a specific agent (optional).
	AgentID string `yaml:"agent_id"`

	// ChannelID targets a specific channel (optional).
	ChannelID string `yaml:"channel_id"`
}
