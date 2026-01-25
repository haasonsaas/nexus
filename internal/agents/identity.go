// Package agents provides agent identity resolution utilities.
// These utilities resolve agent configuration for identity, messages, reactions, and human delay settings.
package agents

import (
	"strings"
)

// DefaultAckReaction is the default reaction emoji for acknowledgments.
const DefaultAckReaction = "\U0001F440" // eyes emoji

// IdentityConfig holds identity configuration for an agent.
type IdentityConfig struct {
	// Name is the display name for the agent.
	Name string `json:"name,omitempty" yaml:"name"`

	// Emoji is the emoji associated with the agent.
	Emoji string `json:"emoji,omitempty" yaml:"emoji"`

	// Description describes what this agent does.
	Description string `json:"description,omitempty" yaml:"description"`
}

// HumanDelayConfig configures delays to make responses feel more human.
type HumanDelayConfig struct {
	// Mode is the delay mode: "off", "fixed", "random".
	Mode string `json:"mode,omitempty" yaml:"mode"`

	// MinMs is the minimum delay in milliseconds.
	MinMs int `json:"min_ms,omitempty" yaml:"min_ms"`

	// MaxMs is the maximum delay in milliseconds.
	MaxMs int `json:"max_ms,omitempty" yaml:"max_ms"`
}

// MessagesConfig holds message prefix configuration.
type MessagesConfig struct {
	// MessagePrefix is prepended to outgoing messages.
	MessagePrefix string `json:"message_prefix,omitempty" yaml:"message_prefix"`

	// ResponsePrefix is prepended to responses (supports "auto" mode).
	ResponsePrefix string `json:"response_prefix,omitempty" yaml:"response_prefix"`

	// AckReaction is the reaction emoji for acknowledgments.
	AckReaction *string `json:"ack_reaction,omitempty" yaml:"ack_reaction"`
}

// AgentConfig represents per-agent configuration that can override defaults.
type AgentConfig struct {
	// Identity is the identity configuration for this agent.
	Identity *IdentityConfig `json:"identity,omitempty" yaml:"identity"`

	// HumanDelay configures human-like delays for this agent.
	HumanDelay *HumanDelayConfig `json:"human_delay,omitempty" yaml:"human_delay"`
}

// AgentsConfig holds the agents section of the configuration.
type AgentsConfig struct {
	// Defaults holds default configuration for all agents.
	Defaults *AgentConfig `json:"defaults,omitempty" yaml:"defaults"`

	// Agents maps agent IDs to their specific configurations.
	Agents map[string]*AgentConfig `json:"agents,omitempty" yaml:"agents"`
}

// Config represents the subset of config needed for identity resolution.
type Config struct {
	// Messages holds global message configuration.
	Messages *MessagesConfig `json:"messages,omitempty" yaml:"messages"`

	// Agents holds agent-specific configurations.
	Agents *AgentsConfig `json:"agents,omitempty" yaml:"agents"`
}

// ResolveAgentConfig returns the agent-specific configuration for the given agent ID.
func ResolveAgentConfig(cfg *Config, agentID string) *AgentConfig {
	if cfg == nil || cfg.Agents == nil || cfg.Agents.Agents == nil {
		return nil
	}
	return cfg.Agents.Agents[agentID]
}

// ResolveAgentIdentity returns the identity configuration for the given agent ID.
func ResolveAgentIdentity(cfg *Config, agentID string) *IdentityConfig {
	agentCfg := ResolveAgentConfig(cfg, agentID)
	if agentCfg == nil {
		return nil
	}
	return agentCfg.Identity
}

// ResolveAckReaction returns the acknowledgment reaction emoji.
// Priority: configured messages.ack_reaction > identity emoji > default.
func ResolveAckReaction(cfg *Config, agentID string) string {
	if cfg != nil && cfg.Messages != nil && cfg.Messages.AckReaction != nil {
		trimmed := strings.TrimSpace(*cfg.Messages.AckReaction)
		return trimmed
	}

	identity := ResolveAgentIdentity(cfg, agentID)
	if identity != nil {
		emoji := strings.TrimSpace(identity.Emoji)
		if emoji != "" {
			return emoji
		}
	}

	return DefaultAckReaction
}

// ResolveIdentityNamePrefix returns the identity name in bracket format, e.g. "[AgentName]".
// Returns empty string if no identity name is configured.
func ResolveIdentityNamePrefix(cfg *Config, agentID string) string {
	identity := ResolveAgentIdentity(cfg, agentID)
	if identity == nil {
		return ""
	}

	name := strings.TrimSpace(identity.Name)
	if name == "" {
		return ""
	}

	return "[" + name + "]"
}

// ResolveIdentityName returns just the identity name without brackets.
// Returns empty string if no identity name is configured.
func ResolveIdentityName(cfg *Config, agentID string) string {
	identity := ResolveAgentIdentity(cfg, agentID)
	if identity == nil {
		return ""
	}

	return strings.TrimSpace(identity.Name)
}

// MessagePrefixOptions contains options for resolving message prefix.
type MessagePrefixOptions struct {
	// Configured is an explicitly configured prefix that takes highest priority.
	Configured *string

	// HasAllowFrom indicates if the context has allow_from restrictions.
	// When true and no explicit prefix, returns empty string.
	HasAllowFrom bool

	// Fallback is used when no identity name prefix is available.
	Fallback string
}

// ResolveMessagePrefix resolves the message prefix using a priority chain:
// 1. Explicitly configured prefix (from options or config)
// 2. Empty string if hasAllowFrom is true
// 3. Identity name prefix (e.g., "[AgentName]")
// 4. Fallback (defaults to "[clawdbot]")
func ResolveMessagePrefix(cfg *Config, agentID string, opts *MessagePrefixOptions) string {
	// Check explicitly configured prefix from options first
	if opts != nil && opts.Configured != nil {
		return *opts.Configured
	}

	// Check configured prefix from config
	if cfg != nil && cfg.Messages != nil && cfg.Messages.MessagePrefix != "" {
		return cfg.Messages.MessagePrefix
	}

	// If has allow_from, return empty
	if opts != nil && opts.HasAllowFrom {
		return ""
	}

	// Try identity name prefix
	identityPrefix := ResolveIdentityNamePrefix(cfg, agentID)
	if identityPrefix != "" {
		return identityPrefix
	}

	// Use fallback
	if opts != nil && opts.Fallback != "" {
		return opts.Fallback
	}

	return "[clawdbot]"
}

// ResolveResponsePrefix resolves the response prefix.
// If configured to "auto", returns the identity name prefix.
// Returns empty string if not configured.
func ResolveResponsePrefix(cfg *Config, agentID string) string {
	if cfg == nil || cfg.Messages == nil {
		return ""
	}

	configured := cfg.Messages.ResponsePrefix
	if configured == "" {
		return ""
	}

	if configured == "auto" {
		return ResolveIdentityNamePrefix(cfg, agentID)
	}

	return configured
}

// EffectiveMessagesConfigOptions contains options for resolving effective messages config.
type EffectiveMessagesConfigOptions struct {
	// HasAllowFrom indicates if the context has allow_from restrictions.
	HasAllowFrom bool

	// FallbackMessagePrefix is the fallback for message prefix.
	FallbackMessagePrefix string
}

// EffectiveMessagesConfig contains the resolved message prefix configuration.
type EffectiveMessagesConfig struct {
	// MessagePrefix is the resolved message prefix.
	MessagePrefix string

	// ResponsePrefix is the resolved response prefix (may be empty).
	ResponsePrefix string
}

// ResolveEffectiveMessagesConfig combines message and response prefix resolution.
func ResolveEffectiveMessagesConfig(cfg *Config, agentID string, opts *EffectiveMessagesConfigOptions) EffectiveMessagesConfig {
	var prefixOpts *MessagePrefixOptions
	if opts != nil {
		prefixOpts = &MessagePrefixOptions{
			HasAllowFrom: opts.HasAllowFrom,
			Fallback:     opts.FallbackMessagePrefix,
		}
	}

	return EffectiveMessagesConfig{
		MessagePrefix:  ResolveMessagePrefix(cfg, agentID, prefixOpts),
		ResponsePrefix: ResolveResponsePrefix(cfg, agentID),
	}
}

// ResolveHumanDelayConfig merges default and agent-specific human delay configuration.
// Returns nil if no human delay configuration exists.
func ResolveHumanDelayConfig(cfg *Config, agentID string) *HumanDelayConfig {
	var defaults *HumanDelayConfig
	var overrides *HumanDelayConfig

	// Get defaults
	if cfg != nil && cfg.Agents != nil && cfg.Agents.Defaults != nil {
		defaults = cfg.Agents.Defaults.HumanDelay
	}

	// Get agent-specific overrides
	agentCfg := ResolveAgentConfig(cfg, agentID)
	if agentCfg != nil {
		overrides = agentCfg.HumanDelay
	}

	// If neither exists, return nil
	if defaults == nil && overrides == nil {
		return nil
	}

	// Merge: overrides take precedence over defaults
	result := &HumanDelayConfig{}

	// Mode
	if overrides != nil && overrides.Mode != "" {
		result.Mode = overrides.Mode
	} else if defaults != nil {
		result.Mode = defaults.Mode
	}

	// MinMs
	if overrides != nil && overrides.MinMs > 0 {
		result.MinMs = overrides.MinMs
	} else if defaults != nil {
		result.MinMs = defaults.MinMs
	}

	// MaxMs
	if overrides != nil && overrides.MaxMs > 0 {
		result.MaxMs = overrides.MaxMs
	} else if defaults != nil {
		result.MaxMs = defaults.MaxMs
	}

	return result
}
