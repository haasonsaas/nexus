package sessions

import (
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

// DMScope constants for session scoping.
const (
	DMScopeMain           = "main"
	DMScopePerPeer        = "per-peer"
	DMScopePerChannelPeer = "per-channel-peer"
)

// ScopeConfig holds session scoping configuration.
// This mirrors config.SessionScopeConfig to avoid import cycles.
type ScopeConfig struct {
	// DMScope controls how DM sessions are scoped:
	// - "main": all DMs share one session (default)
	// - "per-peer": separate session per peer
	// - "per-channel-peer": separate session per channel+peer combination
	DMScope string

	// IdentityLinks maps canonical IDs to platform-specific peer IDs.
	// Format: canonical_id -> ["provider:peer_id", "provider:peer_id", ...]
	IdentityLinks map[string][]string

	// Reset configures default session reset behavior.
	Reset ResetConfig

	// ResetByType configures reset behavior per conversation type (dm, group, thread).
	ResetByType map[string]ResetConfig

	// ResetByChannel configures reset behavior per channel (slack, discord, etc).
	ResetByChannel map[string]ResetConfig
}

// ResetConfig controls when sessions are automatically reset.
type ResetConfig struct {
	// Mode is the reset mode: "daily", "idle", "daily+idle", or "never" (default).
	Mode string

	// AtHour is the hour (0-23) to reset sessions when mode includes "daily".
	AtHour int

	// IdleMinutes is the number of minutes of inactivity before reset when mode includes "idle".
	IdleMinutes int
}

// SessionKeyBuilder builds session keys based on scoping configuration.
type SessionKeyBuilder struct {
	cfg ScopeConfig
}

// NewSessionKeyBuilder creates a new SessionKeyBuilder with the given configuration.
func NewSessionKeyBuilder(cfg ScopeConfig) *SessionKeyBuilder {
	return &SessionKeyBuilder{cfg: cfg}
}

// BuildKey generates a session key based on the scoping configuration.
// Parameters:
//   - agentID: the agent identifier
//   - channel: the channel type (slack, discord, telegram, etc.)
//   - peerID: the peer identifier (user ID, chat ID, etc.)
//   - isGroup: whether this is a group conversation (vs DM)
//   - threadID: optional thread identifier for threaded conversations
func (b *SessionKeyBuilder) BuildKey(agentID string, channel models.ChannelType, peerID string, isGroup bool, threadID string) string {
	// For group conversations, always scope by channel + peer (group ID)
	if isGroup {
		if threadID != "" {
			return agentID + ":" + string(channel) + ":group:" + peerID + ":" + threadID
		}
		return agentID + ":" + string(channel) + ":group:" + peerID
	}

	// For DMs, apply the configured scoping
	resolvedPeerID := b.ResolveIdentity(string(channel), peerID)

	switch strings.ToLower(b.cfg.DMScope) {
	case DMScopeMain:
		// All DMs share one session per agent
		return agentID + ":dm:main"

	case DMScopePerPeer:
		// Separate session per peer (identity-resolved)
		return agentID + ":dm:" + resolvedPeerID

	case DMScopePerChannelPeer:
		// Separate session per channel+peer combination
		return agentID + ":" + string(channel) + ":dm:" + peerID

	default:
		// Default to main scope
		return agentID + ":dm:main"
	}
}

// ResolveIdentity maps a platform-specific peer ID to a canonical identity if configured.
// If no identity link is found, returns the original channel:peerID combination.
func (b *SessionKeyBuilder) ResolveIdentity(channel string, peerID string) string {
	if b.cfg.IdentityLinks == nil {
		return channel + ":" + peerID
	}

	platformID := channel + ":" + peerID

	// Search through identity links to find a canonical ID
	for canonicalID, linkedIDs := range b.cfg.IdentityLinks {
		for _, linkedID := range linkedIDs {
			if linkedID == platformID {
				return canonicalID
			}
		}
	}

	// No link found, return the platform-specific ID
	return platformID
}

// ResolveIdentityStatic resolves identity using a provided identity links map.
// This is useful for one-off resolutions without creating a builder.
func ResolveIdentityStatic(channel string, peerID string, identityLinks map[string][]string) string {
	if identityLinks == nil {
		return channel + ":" + peerID
	}

	platformID := channel + ":" + peerID

	for canonicalID, linkedIDs := range identityLinks {
		for _, linkedID := range linkedIDs {
			if linkedID == platformID {
				return canonicalID
			}
		}
	}

	return platformID
}

// BuildSessionKey is a convenience function for building session keys with all parameters.
// This matches the interface requested for direct usage.
func BuildSessionKey(agentID string, channel models.ChannelType, peerID string, isGroup bool, dmScope string, identityLinks map[string][]string) string {
	builder := &SessionKeyBuilder{
		cfg: ScopeConfig{
			DMScope:       dmScope,
			IdentityLinks: identityLinks,
		},
	}
	return builder.BuildKey(agentID, channel, peerID, isGroup, "")
}

// BuildSessionKeyWithThread builds a session key with thread support.
func BuildSessionKeyWithThread(agentID string, channel models.ChannelType, peerID string, isGroup bool, threadID string, dmScope string, identityLinks map[string][]string) string {
	builder := &SessionKeyBuilder{
		cfg: ScopeConfig{
			DMScope:       dmScope,
			IdentityLinks: identityLinks,
		},
	}
	return builder.BuildKey(agentID, channel, peerID, isGroup, threadID)
}

// GetLinkedPeers returns all platform-specific peer IDs linked to a canonical identity.
func (b *SessionKeyBuilder) GetLinkedPeers(canonicalID string) []string {
	if b.cfg.IdentityLinks == nil {
		return nil
	}
	return b.cfg.IdentityLinks[canonicalID]
}

// GetCanonicalID returns the canonical ID for a platform-specific peer, or empty if not linked.
func (b *SessionKeyBuilder) GetCanonicalID(channel string, peerID string) string {
	if b.cfg.IdentityLinks == nil {
		return ""
	}

	platformID := channel + ":" + peerID

	for canonicalID, linkedIDs := range b.cfg.IdentityLinks {
		for _, linkedID := range linkedIDs {
			if linkedID == platformID {
				return canonicalID
			}
		}
	}

	return ""
}
