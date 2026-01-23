package sandbox

import (
	"github.com/haasonsaas/nexus/internal/config"
)

// SandboxMode determines which agents use sandboxing.
type SandboxMode string

const (
	// ModeOff disables sandboxing entirely.
	ModeOff SandboxMode = "off"
	// ModeAll sandboxes all agents.
	ModeAll SandboxMode = "all"
	// ModeNonMain sandboxes only non-main agents (main agent unsandboxed).
	ModeNonMain SandboxMode = "non-main"
)

// SandboxScope determines isolation level for sandboxes.
type SandboxScope string

const (
	// ScopeAgent creates one sandbox per agent (default).
	ScopeAgent SandboxScope = "agent"
	// ScopeSession creates one sandbox per session.
	ScopeSession SandboxScope = "session"
	// ScopeShared uses a single sandbox for all agents.
	ScopeShared SandboxScope = "shared"
)

// ModeConfig holds resolved sandbox mode configuration.
type ModeConfig struct {
	Mode  SandboxMode
	Scope SandboxScope
}

// ResolveModeConfig extracts mode and scope from config with defaults.
func ResolveModeConfig(cfg config.SandboxConfig) ModeConfig {
	mc := ModeConfig{
		Mode:  ModeOff,
		Scope: ScopeAgent,
	}

	if !cfg.Enabled {
		return mc
	}

	switch SandboxMode(cfg.Mode) {
	case ModeAll, ModeNonMain:
		mc.Mode = SandboxMode(cfg.Mode)
	default:
		mc.Mode = ModeAll // Default to all when enabled
	}

	switch SandboxScope(cfg.Scope) {
	case ScopeSession, ScopeShared:
		mc.Scope = SandboxScope(cfg.Scope)
	default:
		mc.Scope = ScopeAgent // Default to per-agent isolation
	}

	return mc
}

// ShouldSandbox determines if a given agent should be sandboxed based on mode.
func (mc ModeConfig) ShouldSandbox(agentID string, isMainAgent bool) bool {
	switch mc.Mode {
	case ModeOff:
		return false
	case ModeAll:
		return true
	case ModeNonMain:
		return !isMainAgent
	default:
		return false
	}
}

// SandboxKey generates a key for sandbox isolation based on scope.
func (mc ModeConfig) SandboxKey(agentID, sessionID string) string {
	switch mc.Scope {
	case ScopeSession:
		return "session:" + sessionID
	case ScopeShared:
		return "shared"
	case ScopeAgent:
		fallthrough
	default:
		return "agent:" + agentID
	}
}
