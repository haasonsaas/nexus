package sessions

import (
	"regexp"
	"strings"
)

// Default constants for session key routing.
const (
	DefaultAgentID   = "main"
	DefaultMainKey   = "main"
	DefaultAccountID = "default"
)

// ParsedSessionKey represents a parsed agent session key.
type ParsedSessionKey struct {
	AgentID    string
	Rest       string
	IsACP      bool // Agent Communication Protocol key
	IsSubagent bool // Subagent session key
}

// ParseAgentSessionKey parses a session key like "agent:myagent:channel:dm:user123".
// Returns nil if the key is invalid or doesn't start with "agent:".
func ParseAgentSessionKey(sessionKey string) *ParsedSessionKey {
	raw := strings.TrimSpace(sessionKey)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ":")
	// Filter out empty parts
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) < 3 {
		return nil
	}

	if filtered[0] != "agent" {
		return nil
	}

	agentID := strings.TrimSpace(filtered[1])
	rest := strings.Join(filtered[2:], ":")
	if agentID == "" || rest == "" {
		return nil
	}

	// Check for ACP and subagent keys
	restLower := strings.ToLower(rest)
	isACP := strings.HasPrefix(restLower, "acp:")
	isSubagent := strings.HasPrefix(restLower, "subagent:")

	return &ParsedSessionKey{
		AgentID:    agentID,
		Rest:       rest,
		IsACP:      isACP,
		IsSubagent: isSubagent,
	}
}

// IsSubagentSessionKey checks if a session key is for a subagent.
func IsSubagentSessionKey(sessionKey string) bool {
	raw := strings.TrimSpace(sessionKey)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(raw), "subagent:") {
		return true
	}
	parsed := ParseAgentSessionKey(raw)
	if parsed == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(parsed.Rest), "subagent:")
}

// IsACPSessionKey checks if a session key is an Agent Communication Protocol key.
func IsACPSessionKey(sessionKey string) bool {
	raw := strings.TrimSpace(sessionKey)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(raw), "acp:") {
		return true
	}
	parsed := ParseAgentSessionKey(raw)
	if parsed == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(parsed.Rest), "acp:")
}

// agentIDRegex matches valid agent IDs: [a-z0-9][a-z0-9_-]{0,63}
var agentIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// invalidCharsRegex matches characters that aren't alphanumeric, underscore, or hyphen.
var invalidCharsRegex = regexp.MustCompile(`[^a-z0-9_-]+`)

// leadingHyphensRegex matches leading hyphens.
var leadingHyphensRegex = regexp.MustCompile(`^-+`)

// trailingHyphensRegex matches trailing hyphens.
var trailingHyphensRegex = regexp.MustCompile(`-+$`)

// NormalizeAgentID normalizes an agent ID to be path-safe and shell-friendly.
// Only allows [a-z0-9][a-z0-9_-]{0,63}.
func NormalizeAgentID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultAgentID
	}

	// If already valid, return as-is (case preserved for compatibility)
	if agentIDRegex.MatchString(trimmed) {
		return trimmed
	}

	// Best-effort fallback: collapse invalid characters to "-"
	normalized := strings.ToLower(trimmed)
	normalized = invalidCharsRegex.ReplaceAllString(normalized, "-")
	normalized = leadingHyphensRegex.ReplaceAllString(normalized, "")
	normalized = trailingHyphensRegex.ReplaceAllString(normalized, "")

	// Limit to 64 characters
	if len(normalized) > 64 {
		normalized = normalized[:64]
	}

	if normalized == "" {
		return DefaultAgentID
	}
	return normalized
}

// NormalizeMainKey normalizes a main session key.
func NormalizeMainKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultMainKey
	}
	return trimmed
}

// NormalizeAccountID normalizes an account ID.
func NormalizeAccountID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultAccountID
	}

	// If already valid, return as-is
	if agentIDRegex.MatchString(trimmed) {
		return trimmed
	}

	// Best-effort fallback
	normalized := strings.ToLower(trimmed)
	normalized = invalidCharsRegex.ReplaceAllString(normalized, "-")
	normalized = leadingHyphensRegex.ReplaceAllString(normalized, "")
	normalized = trailingHyphensRegex.ReplaceAllString(normalized, "")

	if len(normalized) > 64 {
		normalized = normalized[:64]
	}

	if normalized == "" {
		return DefaultAccountID
	}
	return normalized
}

// ToAgentRequestSessionKey extracts the request portion from a store key.
// For "agent:myagent:channel:dm:user123", returns "channel:dm:user123".
// Returns empty string if the key is invalid.
func ToAgentRequestSessionKey(storeKey string) string {
	raw := strings.TrimSpace(storeKey)
	if raw == "" {
		return ""
	}
	parsed := ParseAgentSessionKey(raw)
	if parsed != nil {
		return parsed.Rest
	}
	return raw
}

// ToAgentStoreSessionKey builds a full store key from agent ID and request key.
// If requestKey is empty or "main", builds the main session key.
// If requestKey already starts with "agent:", returns it as-is.
func ToAgentStoreSessionKey(agentID, requestKey, mainKey string) string {
	raw := strings.TrimSpace(requestKey)
	if raw == "" || raw == DefaultMainKey {
		return BuildAgentMainSessionKey(agentID, mainKey)
	}
	if strings.HasPrefix(raw, "agent:") {
		return raw
	}
	return "agent:" + NormalizeAgentID(agentID) + ":" + raw
}

// ResolveAgentIDFromSessionKey extracts agent ID from session key.
// Returns DefaultAgentID if the key is invalid.
func ResolveAgentIDFromSessionKey(sessionKey string) string {
	parsed := ParseAgentSessionKey(sessionKey)
	if parsed != nil {
		return NormalizeAgentID(parsed.AgentID)
	}
	return DefaultAgentID
}

// BuildAgentMainSessionKey builds "agent:{agentId}:{mainKey}".
func BuildAgentMainSessionKey(agentID, mainKey string) string {
	return "agent:" + NormalizeAgentID(agentID) + ":" + NormalizeMainKey(mainKey)
}

// PeerSessionParams for building peer-specific session keys.
type PeerSessionParams struct {
	AgentID       string
	MainKey       string
	Channel       string
	PeerKind      string // "dm", "group", "channel"
	PeerID        string
	IdentityLinks map[string][]string // canonical -> []linked IDs
	DMScope       string              // "main", "per-peer", "per-channel-peer"
}

// BuildAgentPeerSessionKey builds session keys for specific peers.
// For DM with per-channel-peer: "agent:{agentId}:{channel}:dm:{peerId}"
// For DM with per-peer: "agent:{agentId}:dm:{peerId}"
// For DM with main scope: "agent:{agentId}:{mainKey}"
// For groups/channels: "agent:{agentId}:{channel}:{peerKind}:{peerId}"
func BuildAgentPeerSessionKey(params PeerSessionParams) string {
	peerKind := params.PeerKind
	if peerKind == "" {
		peerKind = "dm"
	}

	if peerKind == "dm" {
		dmScope := params.DMScope
		if dmScope == "" {
			dmScope = "main"
		}

		peerID := strings.TrimSpace(params.PeerID)

		// Resolve identity links unless scope is main
		if dmScope != "main" {
			linkedPeerID := ResolveLinkedPeerID(params.IdentityLinks, params.Channel, peerID)
			if linkedPeerID != "" {
				peerID = linkedPeerID
			}
		}

		switch dmScope {
		case "per-channel-peer":
			if peerID != "" {
				channel := strings.ToLower(strings.TrimSpace(params.Channel))
				if channel == "" {
					channel = "unknown"
				}
				return "agent:" + NormalizeAgentID(params.AgentID) + ":" + channel + ":dm:" + peerID
			}
		case "per-peer":
			if peerID != "" {
				return "agent:" + NormalizeAgentID(params.AgentID) + ":dm:" + peerID
			}
		}

		// Default to main session
		return BuildAgentMainSessionKey(params.AgentID, params.MainKey)
	}

	// For group or channel peer kinds
	channel := strings.ToLower(strings.TrimSpace(params.Channel))
	if channel == "" {
		channel = "unknown"
	}
	peerID := strings.TrimSpace(params.PeerID)
	if peerID == "" {
		peerID = "unknown"
	}

	return "agent:" + NormalizeAgentID(params.AgentID) + ":" + channel + ":" + peerKind + ":" + peerID
}

// normalizeToken normalizes a token to lowercase and trimmed.
func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// ResolveLinkedPeerID resolves a peer ID through identity links.
// Returns canonical ID if linked, otherwise empty string.
func ResolveLinkedPeerID(identityLinks map[string][]string, channel, peerID string) string {
	if identityLinks == nil {
		return ""
	}

	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return ""
	}

	// Build candidate set for matching
	candidates := make(map[string]struct{})
	rawCandidate := normalizeToken(peerID)
	if rawCandidate != "" {
		candidates[rawCandidate] = struct{}{}
	}

	channel = normalizeToken(channel)
	if channel != "" {
		scopedCandidate := normalizeToken(channel + ":" + peerID)
		if scopedCandidate != "" {
			candidates[scopedCandidate] = struct{}{}
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Search through identity links
	for canonical, ids := range identityLinks {
		canonicalName := strings.TrimSpace(canonical)
		if canonicalName == "" {
			continue
		}

		for _, id := range ids {
			normalized := normalizeToken(id)
			if normalized == "" {
				continue
			}
			if _, exists := candidates[normalized]; exists {
				return canonicalName
			}
		}
	}

	return ""
}

// BuildGroupHistoryKey builds a key for group message history.
// Format: "{channel}:{accountId}:{peerKind}:{peerId}"
func BuildGroupHistoryKey(channel, accountID, peerKind, peerID string) string {
	ch := normalizeToken(channel)
	if ch == "" {
		ch = "unknown"
	}

	acct := NormalizeAccountID(accountID)

	pid := strings.TrimSpace(peerID)
	if pid == "" {
		pid = "unknown"
	}

	return ch + ":" + acct + ":" + peerKind + ":" + pid
}

// ThreadSessionParams for thread-based session key resolution.
type ThreadSessionParams struct {
	BaseSessionKey   string
	ThreadID         string
	ParentSessionKey string
	UseSuffix        bool
}

// ThreadSessionKeys result from ResolveThreadSessionKeys.
type ThreadSessionKeys struct {
	SessionKey       string
	ParentSessionKey string
}

// ResolveThreadSessionKeys handles thread-based session key resolution.
// If threadID is set and useSuffix is true: "{baseKey}:thread:{threadId}"
func ResolveThreadSessionKeys(params ThreadSessionParams) ThreadSessionKeys {
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return ThreadSessionKeys{
			SessionKey:       params.BaseSessionKey,
			ParentSessionKey: "",
		}
	}

	useSuffix := params.UseSuffix
	var sessionKey string
	if useSuffix {
		sessionKey = params.BaseSessionKey + ":thread:" + threadID
	} else {
		sessionKey = params.BaseSessionKey
	}

	return ThreadSessionKeys{
		SessionKey:       sessionKey,
		ParentSessionKey: params.ParentSessionKey,
	}
}
