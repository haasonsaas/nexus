package sessions

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

// HierarchicalKey represents a hierarchical session key following the Clawdbot pattern:
// agent:<agentId>:<mainKey>
//
// This structure enables:
//   - Multi-agent session isolation
//   - Efficient lookup by agent ID
//   - Parent-child session relationships
//   - Session inheritance for agent handoffs
type HierarchicalKey struct {
	// AgentID identifies the owning agent.
	AgentID string

	// Channel is the messaging platform.
	Channel models.ChannelType

	// ChannelID is the platform-specific conversation identifier.
	ChannelID string

	// Scope provides additional scoping (e.g., thread ID, user ID).
	Scope string

	// ParentKey links to a parent session for handoffs.
	ParentKey string
}

// String returns the full hierarchical key string.
// Format: agent:<agentId>:<channel>:<channelId>[:<scope>]
func (k HierarchicalKey) String() string {
	key := fmt.Sprintf("agent:%s:%s:%s", k.AgentID, k.Channel, k.ChannelID)
	if k.Scope != "" {
		key = key + ":" + k.Scope
	}
	return key
}

// MainKey returns the portion of the key without the agent prefix.
// This is useful for looking up sessions across agents.
func (k HierarchicalKey) MainKey() string {
	key := fmt.Sprintf("%s:%s", k.Channel, k.ChannelID)
	if k.Scope != "" {
		key = key + ":" + k.Scope
	}
	return key
}

// ParseHierarchicalKey parses a hierarchical key string.
// Supports both prior format (agentId:channel:channelId) and new format (agent:agentId:channel:channelId).
func ParseHierarchicalKey(key string) (HierarchicalKey, error) {
	parts := strings.Split(key, ":")
	if len(parts) < 3 {
		return HierarchicalKey{}, fmt.Errorf("invalid session key format: %s", key)
	}

	// Check for new format with "agent:" prefix
	if parts[0] == "agent" && len(parts) >= 4 {
		result := HierarchicalKey{
			AgentID:   parts[1],
			Channel:   models.ChannelType(parts[2]),
			ChannelID: parts[3],
		}
		if len(parts) >= 5 {
			result.Scope = strings.Join(parts[4:], ":")
		}
		return result, nil
	}

	// Prior format: agentId:channel:channelId
	result := HierarchicalKey{
		AgentID:   parts[0],
		Channel:   models.ChannelType(parts[1]),
		ChannelID: parts[2],
	}
	if len(parts) >= 4 {
		result.Scope = strings.Join(parts[3:], ":")
	}
	return result, nil
}

// NewHierarchicalKey creates a new hierarchical session key.
func NewHierarchicalKey(agentID string, channel models.ChannelType, channelID string) HierarchicalKey {
	return HierarchicalKey{
		AgentID:   agentID,
		Channel:   channel,
		ChannelID: channelID,
	}
}

// WithScope returns a copy of the key with the given scope.
func (k HierarchicalKey) WithScope(scope string) HierarchicalKey {
	k.Scope = scope
	return k
}

// WithParent returns a copy of the key with the given parent key.
func (k HierarchicalKey) WithParent(parentKey string) HierarchicalKey {
	k.ParentKey = parentKey
	return k
}

// ForAgent returns a copy of the key for a different agent (used in handoffs).
func (k HierarchicalKey) ForAgent(agentID string) HierarchicalKey {
	return HierarchicalKey{
		AgentID:   agentID,
		Channel:   k.Channel,
		ChannelID: k.ChannelID,
		Scope:     k.Scope,
		ParentKey: k.String(), // Link back to original session
	}
}

// SessionKeyHierarchy manages hierarchical session keys.
type SessionKeyHierarchy struct {
	// DefaultAgentID is the agent used when none is specified.
	DefaultAgentID string
}

// NewSessionKeyHierarchy creates a new session key hierarchy manager.
func NewSessionKeyHierarchy(defaultAgentID string) *SessionKeyHierarchy {
	if defaultAgentID == "" {
		defaultAgentID = "main"
	}
	return &SessionKeyHierarchy{
		DefaultAgentID: defaultAgentID,
	}
}

// BuildKey constructs a hierarchical session key.
func (h *SessionKeyHierarchy) BuildKey(agentID string, channel models.ChannelType, channelID string) string {
	if agentID == "" {
		agentID = h.DefaultAgentID
	}
	return NewHierarchicalKey(agentID, channel, channelID).String()
}

// BuildKeyWithScope constructs a hierarchical session key with additional scope.
func (h *SessionKeyHierarchy) BuildKeyWithScope(agentID string, channel models.ChannelType, channelID, scope string) string {
	if agentID == "" {
		agentID = h.DefaultAgentID
	}
	return NewHierarchicalKey(agentID, channel, channelID).WithScope(scope).String()
}

// ExtractAgentID extracts the agent ID from a session key.
func (h *SessionKeyHierarchy) ExtractAgentID(key string) (string, error) {
	parsed, err := ParseHierarchicalKey(key)
	if err != nil {
		return "", err
	}
	return parsed.AgentID, nil
}

// ExtractMainKey extracts the channel/channelID portion from a session key.
func (h *SessionKeyHierarchy) ExtractMainKey(key string) (string, error) {
	parsed, err := ParseHierarchicalKey(key)
	if err != nil {
		return "", err
	}
	return parsed.MainKey(), nil
}

// IsChildOf checks if a key is a child (handoff) of another key.
func (h *SessionKeyHierarchy) IsChildOf(childKey, parentKey string) bool {
	child, err := ParseHierarchicalKey(childKey)
	if err != nil {
		return false
	}
	return child.ParentKey == parentKey
}

// TransformForHandoff creates a new session key for an agent handoff.
func (h *SessionKeyHierarchy) TransformForHandoff(currentKey, targetAgentID string) (string, error) {
	parsed, err := ParseHierarchicalKey(currentKey)
	if err != nil {
		return "", err
	}
	return parsed.ForAgent(targetAgentID).String(), nil
}

// SessionMetadataKey constants for storing hierarchy information in session metadata.
const (
	MetaKeyParentSession   = "parent_session_key"
	MetaKeyChildSessions   = "child_session_keys"
	MetaKeyHandoffDepth    = "handoff_depth"
	MetaKeyOriginalAgentID = "original_agent_id"
	MetaKeyCompactionInfo  = "compaction_info"
	MetaKeyLastCompactedAt = "last_compacted_at"
	MetaKeyMessageCountPre = "message_count_pre_compaction"
)
