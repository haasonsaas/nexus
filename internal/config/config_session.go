package config

import "time"

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
