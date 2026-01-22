package sessions

import (
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestHierarchicalKey_String(t *testing.T) {
	tests := []struct {
		name     string
		key      HierarchicalKey
		expected string
	}{
		{
			name: "basic key",
			key: HierarchicalKey{
				AgentID:   "main",
				Channel:   models.ChannelTelegram,
				ChannelID: "123456",
			},
			expected: "agent:main:telegram:123456",
		},
		{
			name: "key with scope",
			key: HierarchicalKey{
				AgentID:   "research",
				Channel:   models.ChannelSlack,
				ChannelID: "C123",
				Scope:     "thread-456",
			},
			expected: "agent:research:slack:C123:thread-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.key.String()
			if result != tt.expected {
				t.Errorf("String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHierarchicalKey_MainKey(t *testing.T) {
	key := HierarchicalKey{
		AgentID:   "main",
		Channel:   models.ChannelTelegram,
		ChannelID: "123456",
		Scope:     "thread-1",
	}

	expected := "telegram:123456:thread-1"
	result := key.MainKey()
	if result != expected {
		t.Errorf("MainKey() = %v, want %v", result, expected)
	}
}

func TestParseHierarchicalKey(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    HierarchicalKey
		expectError bool
	}{
		{
			name:  "new format",
			input: "agent:main:telegram:123456",
			expected: HierarchicalKey{
				AgentID:   "main",
				Channel:   models.ChannelTelegram,
				ChannelID: "123456",
			},
		},
		{
			name:  "new format with scope",
			input: "agent:research:slack:C123:thread-456",
			expected: HierarchicalKey{
				AgentID:   "research",
				Channel:   models.ChannelSlack,
				ChannelID: "C123",
				Scope:     "thread-456",
			},
		},
		{
			name:  "legacy format",
			input: "main:telegram:123456",
			expected: HierarchicalKey{
				AgentID:   "main",
				Channel:   models.ChannelTelegram,
				ChannelID: "123456",
			},
		},
		{
			name:        "invalid format",
			input:       "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseHierarchicalKey(tt.input)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.AgentID != tt.expected.AgentID {
				t.Errorf("AgentID = %v, want %v", result.AgentID, tt.expected.AgentID)
			}
			if result.Channel != tt.expected.Channel {
				t.Errorf("Channel = %v, want %v", result.Channel, tt.expected.Channel)
			}
			if result.ChannelID != tt.expected.ChannelID {
				t.Errorf("ChannelID = %v, want %v", result.ChannelID, tt.expected.ChannelID)
			}
			if result.Scope != tt.expected.Scope {
				t.Errorf("Scope = %v, want %v", result.Scope, tt.expected.Scope)
			}
		})
	}
}

func TestHierarchicalKey_ForAgent(t *testing.T) {
	original := HierarchicalKey{
		AgentID:   "main",
		Channel:   models.ChannelTelegram,
		ChannelID: "123456",
		Scope:     "thread-1",
	}

	handoff := original.ForAgent("research")

	if handoff.AgentID != "research" {
		t.Errorf("AgentID = %v, want research", handoff.AgentID)
	}
	if handoff.Channel != original.Channel {
		t.Errorf("Channel changed unexpectedly")
	}
	if handoff.ChannelID != original.ChannelID {
		t.Errorf("ChannelID changed unexpectedly")
	}
	if handoff.Scope != original.Scope {
		t.Errorf("Scope changed unexpectedly")
	}
	if handoff.ParentKey != original.String() {
		t.Errorf("ParentKey = %v, want %v", handoff.ParentKey, original.String())
	}
}

func TestSessionKeyHierarchy_BuildKey(t *testing.T) {
	hierarchy := NewSessionKeyHierarchy("default-agent")

	// With specified agent
	key := hierarchy.BuildKey("custom", models.ChannelDiscord, "guild-123")
	expected := "agent:custom:discord:guild-123"
	if key != expected {
		t.Errorf("BuildKey() = %v, want %v", key, expected)
	}

	// Without agent (uses default)
	key = hierarchy.BuildKey("", models.ChannelTelegram, "chat-456")
	expected = "agent:default-agent:telegram:chat-456"
	if key != expected {
		t.Errorf("BuildKey() with default = %v, want %v", key, expected)
	}
}

func TestSessionKeyHierarchy_ExtractAgentID(t *testing.T) {
	hierarchy := NewSessionKeyHierarchy("main")

	agentID, err := hierarchy.ExtractAgentID("agent:research:telegram:123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "research" {
		t.Errorf("ExtractAgentID() = %v, want research", agentID)
	}
}

func TestSessionKeyHierarchy_TransformForHandoff(t *testing.T) {
	hierarchy := NewSessionKeyHierarchy("main")

	currentKey := "agent:main:telegram:123"
	newKey, err := hierarchy.TransformForHandoff(currentKey, "research")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The new key should have the new agent ID
	// Note: ParentKey is not encoded in the string representation,
	// it must be stored separately in session metadata
	expected := "agent:research:telegram:123"
	if newKey != expected {
		t.Errorf("TransformForHandoff() = %v, want %v", newKey, expected)
	}

	parsed, err := ParseHierarchicalKey(newKey)
	if err != nil {
		t.Fatalf("failed to parse new key: %v", err)
	}

	if parsed.AgentID != "research" {
		t.Errorf("new AgentID = %v, want research", parsed.AgentID)
	}
	// ParentKey is not preserved in string representation - it must be stored in metadata
}

func TestNewHierarchicalKey(t *testing.T) {
	key := NewHierarchicalKey("agent1", models.ChannelSlack, "channel1")

	if key.AgentID != "agent1" {
		t.Errorf("AgentID = %v, want agent1", key.AgentID)
	}
	if key.Channel != models.ChannelSlack {
		t.Errorf("Channel = %v, want slack", key.Channel)
	}
	if key.ChannelID != "channel1" {
		t.Errorf("ChannelID = %v, want channel1", key.ChannelID)
	}
}

func TestHierarchicalKey_WithScope(t *testing.T) {
	key := NewHierarchicalKey("main", models.ChannelSlack, "C123")
	scoped := key.WithScope("thread-456")

	// Original should be unchanged
	if key.Scope != "" {
		t.Error("original key scope should be empty")
	}

	// New key should have scope
	if scoped.Scope != "thread-456" {
		t.Errorf("scoped key Scope = %v, want thread-456", scoped.Scope)
	}
}

func TestHierarchicalKey_WithParent(t *testing.T) {
	key := NewHierarchicalKey("child", models.ChannelTelegram, "123")
	parented := key.WithParent("agent:parent:telegram:123")

	// Original should be unchanged
	if key.ParentKey != "" {
		t.Error("original key parent should be empty")
	}

	// New key should have parent
	if parented.ParentKey != "agent:parent:telegram:123" {
		t.Errorf("parented key ParentKey = %v, want agent:parent:telegram:123", parented.ParentKey)
	}
}
