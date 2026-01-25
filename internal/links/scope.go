package links

import (
	"strings"
)

// Scope mode constants.
const (
	ScopeModeAll       = "all"
	ScopeModeAllowlist = "allowlist"
	ScopeModeDenylist  = "denylist"
)

// Decision constants.
const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

// resolveScopeDecision determines whether link processing should be allowed
// based on the scope configuration and message context.
func resolveScopeDecision(scope *ScopeConfig, msgCtx *MsgContext) string {
	// No scope config means allow all
	if scope == nil {
		return DecisionAllow
	}

	// No context means we can't check, default to allow
	if msgCtx == nil {
		return DecisionAllow
	}

	mode := strings.ToLower(strings.TrimSpace(scope.Mode))

	switch mode {
	case ScopeModeAll, "":
		// Allow all channels
		return DecisionAllow

	case ScopeModeAllowlist:
		// Only allow channels in the allowlist
		if matchesScope(msgCtx, scope.Allowlist) {
			return DecisionAllow
		}
		return DecisionDeny

	case ScopeModeDenylist:
		// Deny channels in the denylist
		if matchesScope(msgCtx, scope.Denylist) {
			return DecisionDeny
		}
		return DecisionAllow

	default:
		// Unknown mode, default to allow
		return DecisionAllow
	}
}

// matchesScope checks if the message context matches any of the scope entries.
// Scope entries can be:
// - Channel names: "telegram", "discord", "slack"
// - Channel:PeerID: "telegram:123456", "discord:guild_id"
// - Wildcard: "*"
func matchesScope(msgCtx *MsgContext, entries []string) bool {
	if msgCtx == nil {
		return false
	}

	channel := strings.ToLower(strings.TrimSpace(msgCtx.Channel))
	peerID := strings.TrimSpace(msgCtx.PeerID)

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		// Wildcard matches everything
		if entry == "*" {
			return true
		}

		entryLower := strings.ToLower(entry)

		// Check for channel:peer_id format
		if strings.Contains(entry, ":") {
			parts := strings.SplitN(entry, ":", 2)
			if len(parts) == 2 {
				entryChannel := strings.ToLower(strings.TrimSpace(parts[0]))
				entryPeerID := strings.TrimSpace(parts[1])

				if entryChannel == channel {
					// Wildcard peer ID
					if entryPeerID == "*" {
						return true
					}
					// Exact peer ID match
					if entryPeerID == peerID {
						return true
					}
				}
			}
			continue
		}

		// Simple channel match
		if entryLower == channel {
			return true
		}
	}

	return false
}

// IsScopeAllowed is a convenience function to check if a channel is allowed.
func IsScopeAllowed(scope *ScopeConfig, channel, peerID string) bool {
	msgCtx := &MsgContext{
		Channel: channel,
		PeerID:  peerID,
	}
	return resolveScopeDecision(scope, msgCtx) == DecisionAllow
}

// NewAllowlistScope creates a scope config that only allows specific channels.
func NewAllowlistScope(channels ...string) *ScopeConfig {
	return &ScopeConfig{
		Mode:      ScopeModeAllowlist,
		Allowlist: channels,
	}
}

// NewDenylistScope creates a scope config that denies specific channels.
func NewDenylistScope(channels ...string) *ScopeConfig {
	return &ScopeConfig{
		Mode:     ScopeModeDenylist,
		Denylist: channels,
	}
}
