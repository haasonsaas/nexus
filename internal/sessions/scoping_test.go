package sessions

import (
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestSessionKeyBuilder_DMScopeMain(t *testing.T) {
	builder := NewSessionKeyBuilder(ScopeConfig{
		DMScope: DMScopeMain,
	})

	tests := []struct {
		name     string
		agentID  string
		channel  models.ChannelType
		peerID   string
		isGroup  bool
		threadID string
		expected string
	}{
		{
			name:     "DM from Slack",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			expected: "agent1:dm:main",
		},
		{
			name:     "DM from Discord",
			agentID:  "agent1",
			channel:  models.ChannelDiscord,
			peerID:   "user456",
			isGroup:  false,
			expected: "agent1:dm:main",
		},
		{
			name:     "DM from Telegram",
			agentID:  "agent2",
			channel:  models.ChannelTelegram,
			peerID:   "tg_user",
			isGroup:  false,
			expected: "agent2:dm:main",
		},
		{
			name:     "Group message should scope by group",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "C456",
			isGroup:  true,
			expected: "agent1:slack:group:C456",
		},
		{
			name:     "Group with thread",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "C456",
			isGroup:  true,
			threadID: "1700000.001",
			expected: "agent1:slack:group:C456:1700000.001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.BuildKey(tt.agentID, tt.channel, tt.peerID, tt.isGroup, tt.threadID)
			if got != tt.expected {
				t.Errorf("BuildKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSessionKeyBuilder_DMScopePerPeer(t *testing.T) {
	builder := NewSessionKeyBuilder(ScopeConfig{
		DMScope: DMScopePerPeer,
	})

	tests := []struct {
		name     string
		agentID  string
		channel  models.ChannelType
		peerID   string
		isGroup  bool
		expected string
	}{
		{
			name:     "DM from Slack user",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			expected: "agent1:dm:slack:U123",
		},
		{
			name:     "DM from Discord user",
			agentID:  "agent1",
			channel:  models.ChannelDiscord,
			peerID:   "user456",
			isGroup:  false,
			expected: "agent1:dm:discord:user456",
		},
		{
			name:     "Different agent same peer",
			agentID:  "agent2",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			expected: "agent2:dm:slack:U123",
		},
		{
			name:     "Group still scopes by group",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "C456",
			isGroup:  true,
			expected: "agent1:slack:group:C456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.BuildKey(tt.agentID, tt.channel, tt.peerID, tt.isGroup, "")
			if got != tt.expected {
				t.Errorf("BuildKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSessionKeyBuilder_DMScopePerChannelPeer(t *testing.T) {
	builder := NewSessionKeyBuilder(ScopeConfig{
		DMScope: DMScopePerChannelPeer,
	})

	tests := []struct {
		name     string
		agentID  string
		channel  models.ChannelType
		peerID   string
		isGroup  bool
		expected string
	}{
		{
			name:     "DM from Slack",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			expected: "agent1:slack:dm:U123",
		},
		{
			name:     "DM from Discord",
			agentID:  "agent1",
			channel:  models.ChannelDiscord,
			peerID:   "U123",
			isGroup:  false,
			expected: "agent1:discord:dm:U123",
		},
		{
			name:     "Same user different channels get different sessions",
			agentID:  "agent1",
			channel:  models.ChannelTelegram,
			peerID:   "U123",
			isGroup:  false,
			expected: "agent1:telegram:dm:U123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.BuildKey(tt.agentID, tt.channel, tt.peerID, tt.isGroup, "")
			if got != tt.expected {
				t.Errorf("BuildKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSessionKeyBuilder_IdentityLinks(t *testing.T) {
	builder := NewSessionKeyBuilder(ScopeConfig{
		DMScope: DMScopePerPeer,
		IdentityLinks: map[string][]string{
			"jonathan": {
				"slack:U123",
				"discord:user456",
				"telegram:tg_jonathan",
			},
			"alice": {
				"slack:U789",
				"discord:alice_discord",
			},
		},
	})

	tests := []struct {
		name     string
		agentID  string
		channel  models.ChannelType
		peerID   string
		isGroup  bool
		expected string
	}{
		{
			name:     "Jonathan from Slack resolves to canonical",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			expected: "agent1:dm:jonathan",
		},
		{
			name:     "Jonathan from Discord resolves to same session",
			agentID:  "agent1",
			channel:  models.ChannelDiscord,
			peerID:   "user456",
			isGroup:  false,
			expected: "agent1:dm:jonathan",
		},
		{
			name:     "Jonathan from Telegram resolves to same session",
			agentID:  "agent1",
			channel:  models.ChannelTelegram,
			peerID:   "tg_jonathan",
			isGroup:  false,
			expected: "agent1:dm:jonathan",
		},
		{
			name:     "Alice from Slack resolves to her canonical",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U789",
			isGroup:  false,
			expected: "agent1:dm:alice",
		},
		{
			name:     "Unknown user gets platform ID",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U_unknown",
			isGroup:  false,
			expected: "agent1:dm:slack:U_unknown",
		},
		{
			name:     "Groups not affected by identity links",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "C456",
			isGroup:  true,
			expected: "agent1:slack:group:C456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.BuildKey(tt.agentID, tt.channel, tt.peerID, tt.isGroup, "")
			if got != tt.expected {
				t.Errorf("BuildKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResolveIdentity(t *testing.T) {
	builder := NewSessionKeyBuilder(ScopeConfig{
		IdentityLinks: map[string][]string{
			"jonathan": {"slack:U123", "discord:user456"},
		},
	})

	tests := []struct {
		name     string
		channel  string
		peerID   string
		expected string
	}{
		{
			name:     "Linked Slack user",
			channel:  "slack",
			peerID:   "U123",
			expected: "jonathan",
		},
		{
			name:     "Linked Discord user",
			channel:  "discord",
			peerID:   "user456",
			expected: "jonathan",
		},
		{
			name:     "Unlinked user",
			channel:  "telegram",
			peerID:   "tg_user",
			expected: "telegram:tg_user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.ResolveIdentity(tt.channel, tt.peerID)
			if got != tt.expected {
				t.Errorf("ResolveIdentity() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResolveIdentityStatic(t *testing.T) {
	identityLinks := map[string][]string{
		"jonathan": {"slack:U123", "discord:user456"},
	}

	tests := []struct {
		name     string
		channel  string
		peerID   string
		expected string
	}{
		{
			name:     "Linked user",
			channel:  "slack",
			peerID:   "U123",
			expected: "jonathan",
		},
		{
			name:     "Unlinked user",
			channel:  "telegram",
			peerID:   "tg_user",
			expected: "telegram:tg_user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveIdentityStatic(tt.channel, tt.peerID, identityLinks)
			if got != tt.expected {
				t.Errorf("ResolveIdentityStatic() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResolveIdentityStatic_NilLinks(t *testing.T) {
	got := ResolveIdentityStatic("slack", "U123", nil)
	expected := "slack:U123"
	if got != expected {
		t.Errorf("ResolveIdentityStatic() = %q, want %q", got, expected)
	}
}

func TestBuildSessionKey(t *testing.T) {
	tests := []struct {
		name          string
		agentID       string
		channel       models.ChannelType
		peerID        string
		isGroup       bool
		dmScope       string
		identityLinks map[string][]string
		expected      string
	}{
		{
			name:     "Main scope",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			dmScope:  DMScopeMain,
			expected: "agent1:dm:main",
		},
		{
			name:     "Per-peer scope",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			dmScope:  DMScopePerPeer,
			expected: "agent1:dm:slack:U123",
		},
		{
			name:    "Per-peer with identity",
			agentID: "agent1",
			channel: models.ChannelSlack,
			peerID:  "U123",
			isGroup: false,
			dmScope: DMScopePerPeer,
			identityLinks: map[string][]string{
				"jonathan": {"slack:U123"},
			},
			expected: "agent1:dm:jonathan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSessionKey(tt.agentID, tt.channel, tt.peerID, tt.isGroup, tt.dmScope, tt.identityLinks)
			if got != tt.expected {
				t.Errorf("BuildSessionKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestBuildSessionKeyWithThread(t *testing.T) {
	got := BuildSessionKeyWithThread("agent1", models.ChannelSlack, "C123", true, "1700000.001", DMScopeMain, nil)
	expected := "agent1:slack:group:C123:1700000.001"
	if got != expected {
		t.Errorf("BuildSessionKeyWithThread() = %q, want %q", got, expected)
	}
}

func TestGetLinkedPeers(t *testing.T) {
	builder := NewSessionKeyBuilder(ScopeConfig{
		IdentityLinks: map[string][]string{
			"jonathan": {"slack:U123", "discord:user456"},
		},
	})

	peers := builder.GetLinkedPeers("jonathan")
	if len(peers) != 2 {
		t.Errorf("GetLinkedPeers() returned %d peers, want 2", len(peers))
	}

	// Unknown canonical ID
	peers = builder.GetLinkedPeers("unknown")
	if peers != nil {
		t.Errorf("GetLinkedPeers() for unknown should return nil, got %v", peers)
	}
}

func TestGetCanonicalID(t *testing.T) {
	builder := NewSessionKeyBuilder(ScopeConfig{
		IdentityLinks: map[string][]string{
			"jonathan": {"slack:U123", "discord:user456"},
		},
	})

	tests := []struct {
		name     string
		channel  string
		peerID   string
		expected string
	}{
		{
			name:     "Linked Slack user",
			channel:  "slack",
			peerID:   "U123",
			expected: "jonathan",
		},
		{
			name:     "Unlinked user",
			channel:  "telegram",
			peerID:   "tg_user",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.GetCanonicalID(tt.channel, tt.peerID)
			if got != tt.expected {
				t.Errorf("GetCanonicalID() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSessionKeyBuilder_DefaultScope(t *testing.T) {
	// Empty DMScope should default to main
	builder := NewSessionKeyBuilder(ScopeConfig{})

	got := builder.BuildKey("agent1", models.ChannelSlack, "U123", false, "")
	expected := "agent1:dm:main"
	if got != expected {
		t.Errorf("BuildKey() with empty DMScope = %q, want %q", got, expected)
	}
}

func TestSessionKeyBuilder_CaseInsensitiveScope(t *testing.T) {
	tests := []struct {
		dmScope  string
		expected string
	}{
		{"MAIN", "agent1:dm:main"},
		{"Main", "agent1:dm:main"},
		{"PER-PEER", "agent1:dm:slack:U123"},
		{"Per-Peer", "agent1:dm:slack:U123"},
		{"PER-CHANNEL-PEER", "agent1:slack:dm:U123"},
		{"Per-Channel-Peer", "agent1:slack:dm:U123"},
	}

	for _, tt := range tests {
		t.Run(tt.dmScope, func(t *testing.T) {
			builder := NewSessionKeyBuilder(ScopeConfig{
				DMScope: tt.dmScope,
			})
			got := builder.BuildKey("agent1", models.ChannelSlack, "U123", false, "")
			if got != tt.expected {
				t.Errorf("BuildKey() with DMScope=%q = %q, want %q", tt.dmScope, got, tt.expected)
			}
		})
	}
}
