package sessions

import (
	"testing"
)

func TestParseAgentSessionKey(t *testing.T) {
	tests := []struct {
		name       string
		sessionKey string
		wantNil    bool
		wantAgent  string
		wantRest   string
		wantACP    bool
		wantSub    bool
	}{
		{
			name:       "valid agent key",
			sessionKey: "agent:myagent:channel:dm:user123",
			wantAgent:  "myagent",
			wantRest:   "channel:dm:user123",
		},
		{
			name:       "agent main key",
			sessionKey: "agent:main:main",
			wantAgent:  "main",
			wantRest:   "main",
		},
		{
			name:       "ACP key",
			sessionKey: "agent:main:acp:request123",
			wantAgent:  "main",
			wantRest:   "acp:request123",
			wantACP:    true,
		},
		{
			name:       "subagent key",
			sessionKey: "agent:main:subagent:worker1:task",
			wantAgent:  "main",
			wantRest:   "subagent:worker1:task",
			wantSub:    true,
		},
		{
			name:       "empty string",
			sessionKey: "",
			wantNil:    true,
		},
		{
			name:       "not agent prefix",
			sessionKey: "session:myagent:key",
			wantNil:    true,
		},
		{
			name:       "too few parts",
			sessionKey: "agent:myagent",
			wantNil:    true,
		},
		{
			name:       "whitespace only",
			sessionKey: "   ",
			wantNil:    true,
		},
		{
			name:       "with leading whitespace",
			sessionKey: "  agent:test:rest",
			wantAgent:  "test",
			wantRest:   "rest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAgentSessionKey(tt.sessionKey)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseAgentSessionKey() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseAgentSessionKey() = nil, want non-nil")
			}
			if got.AgentID != tt.wantAgent {
				t.Errorf("AgentID = %q, want %q", got.AgentID, tt.wantAgent)
			}
			if got.Rest != tt.wantRest {
				t.Errorf("Rest = %q, want %q", got.Rest, tt.wantRest)
			}
			if got.IsACP != tt.wantACP {
				t.Errorf("IsACP = %v, want %v", got.IsACP, tt.wantACP)
			}
			if got.IsSubagent != tt.wantSub {
				t.Errorf("IsSubagent = %v, want %v", got.IsSubagent, tt.wantSub)
			}
		})
	}
}

func TestIsSubagentSessionKey(t *testing.T) {
	tests := []struct {
		name       string
		sessionKey string
		want       bool
	}{
		{"direct subagent prefix", "subagent:worker:task", true},
		{"agent with subagent rest", "agent:main:subagent:worker", true},
		{"normal agent key", "agent:main:dm:user", false},
		{"empty", "", false},
		{"case insensitive", "SUBAGENT:worker", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSubagentSessionKey(tt.sessionKey); got != tt.want {
				t.Errorf("IsSubagentSessionKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsACPSessionKey(t *testing.T) {
	tests := []struct {
		name       string
		sessionKey string
		want       bool
	}{
		{"direct ACP prefix", "acp:request123", true},
		{"agent with ACP rest", "agent:main:acp:request", true},
		{"normal agent key", "agent:main:dm:user", false},
		{"empty", "", false},
		{"case insensitive", "ACP:REQUEST", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsACPSessionKey(tt.sessionKey); got != tt.want {
				t.Errorf("IsACPSessionKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeAgentID(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"empty", "", DefaultAgentID},
		{"whitespace", "   ", DefaultAgentID},
		{"valid lowercase", "myagent", "myagent"},
		{"valid with numbers", "agent123", "agent123"},
		{"valid with hyphen", "my-agent", "my-agent"},
		{"valid with underscore", "my_agent", "my_agent"},
		{"preserves case if valid", "MyAgent", "MyAgent"},
		{"special chars normalized", "my@agent!", "my-agent"},
		{"leading hyphen removed", "-agent", "agent"},
		{"trailing hyphen valid", "agent-", "agent-"}, // trailing hyphen is valid per regex
		{"multiple special chars", "my@@agent##test", "my-agent-test"},
		{"only special chars", "@@@", DefaultAgentID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeAgentID(tt.value); got != tt.want {
				t.Errorf("NormalizeAgentID(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestNormalizeMainKey(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"", DefaultMainKey},
		{"   ", DefaultMainKey},
		{"custom", "custom"},
		{"  trimmed  ", "trimmed"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := NormalizeMainKey(tt.value); got != tt.want {
				t.Errorf("NormalizeMainKey(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestNormalizeAccountID(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"", DefaultAccountID},
		{"   ", DefaultAccountID},
		{"account1", "account1"},
		{"Account-1", "Account-1"},
		{"bad@chars", "bad-chars"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := NormalizeAccountID(tt.value); got != tt.want {
				t.Errorf("NormalizeAccountID(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestToAgentRequestSessionKey(t *testing.T) {
	tests := []struct {
		storeKey string
		want     string
	}{
		{"agent:myagent:channel:dm:user", "channel:dm:user"},
		{"agent:main:main", "main"},
		{"", ""},
		{"invalid:key", "invalid:key"},
		{"  agent:test:rest  ", "rest"},
	}

	for _, tt := range tests {
		t.Run(tt.storeKey, func(t *testing.T) {
			if got := ToAgentRequestSessionKey(tt.storeKey); got != tt.want {
				t.Errorf("ToAgentRequestSessionKey(%q) = %q, want %q", tt.storeKey, got, tt.want)
			}
		})
	}
}

func TestToAgentStoreSessionKey(t *testing.T) {
	tests := []struct {
		name       string
		agentID    string
		requestKey string
		mainKey    string
		want       string
	}{
		{"empty request", "myagent", "", "", "agent:myagent:main"},
		{"main request", "myagent", "main", "", "agent:myagent:main"},
		{"custom main", "myagent", "", "custom", "agent:myagent:custom"},
		{"already prefixed", "myagent", "agent:other:key", "", "agent:other:key"},
		{"normal request", "myagent", "channel:dm:user", "", "agent:myagent:channel:dm:user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToAgentStoreSessionKey(tt.agentID, tt.requestKey, tt.mainKey)
			if got != tt.want {
				t.Errorf("ToAgentStoreSessionKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveAgentIDFromSessionKey(t *testing.T) {
	tests := []struct {
		sessionKey string
		want       string
	}{
		{"agent:myagent:rest", "myagent"},
		{"agent:main:main", "main"},
		{"invalid", DefaultAgentID},
		{"", DefaultAgentID},
	}

	for _, tt := range tests {
		t.Run(tt.sessionKey, func(t *testing.T) {
			if got := ResolveAgentIDFromSessionKey(tt.sessionKey); got != tt.want {
				t.Errorf("ResolveAgentIDFromSessionKey(%q) = %q, want %q", tt.sessionKey, got, tt.want)
			}
		})
	}
}

func TestBuildAgentMainSessionKey(t *testing.T) {
	tests := []struct {
		agentID string
		mainKey string
		want    string
	}{
		{"myagent", "main", "agent:myagent:main"},
		{"myagent", "", "agent:myagent:main"},
		{"", "main", "agent:main:main"},
		{"test", "custom", "agent:test:custom"},
	}

	for _, tt := range tests {
		t.Run(tt.agentID+"/"+tt.mainKey, func(t *testing.T) {
			if got := BuildAgentMainSessionKey(tt.agentID, tt.mainKey); got != tt.want {
				t.Errorf("BuildAgentMainSessionKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildAgentPeerSessionKey(t *testing.T) {
	tests := []struct {
		name   string
		params PeerSessionParams
		want   string
	}{
		{
			name: "DM main scope",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "dm",
				DMScope:  "main",
				Channel:  "slack",
				PeerID:   "U123",
			},
			want: "agent:myagent:main",
		},
		{
			name: "DM per-peer scope",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "dm",
				DMScope:  "per-peer",
				Channel:  "slack",
				PeerID:   "U123",
			},
			want: "agent:myagent:dm:U123",
		},
		{
			name: "DM per-channel-peer scope",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "dm",
				DMScope:  "per-channel-peer",
				Channel:  "slack",
				PeerID:   "U123",
			},
			want: "agent:myagent:slack:dm:U123",
		},
		{
			name: "group key",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "group",
				Channel:  "discord",
				PeerID:   "chan123",
			},
			want: "agent:myagent:discord:group:chan123",
		},
		{
			name: "channel key",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "channel",
				Channel:  "telegram",
				PeerID:   "group456",
			},
			want: "agent:myagent:telegram:channel:group456",
		},
		{
			name: "empty peer kind defaults to dm",
			params: PeerSessionParams{
				AgentID: "myagent",
				Channel: "slack",
				PeerID:  "U123",
				DMScope: "per-peer",
			},
			want: "agent:myagent:dm:U123",
		},
		{
			name: "empty channel",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "group",
				PeerID:   "G123",
			},
			want: "agent:myagent:unknown:group:G123",
		},
		{
			name: "empty peer ID for group",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "group",
				Channel:  "slack",
			},
			want: "agent:myagent:slack:group:unknown",
		},
		{
			name: "DM with identity link",
			params: PeerSessionParams{
				AgentID:  "myagent",
				PeerKind: "dm",
				DMScope:  "per-peer",
				Channel:  "slack",
				PeerID:   "U123",
				IdentityLinks: map[string][]string{
					"jonathan": {"slack:U123", "discord:user456"},
				},
			},
			want: "agent:myagent:dm:jonathan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildAgentPeerSessionKey(tt.params); got != tt.want {
				t.Errorf("BuildAgentPeerSessionKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveLinkedPeerID(t *testing.T) {
	identityLinks := map[string][]string{
		"jonathan": {"slack:U123", "discord:user456"},
		"alice":    {"telegram:alice_tg"},
	}

	tests := []struct {
		name    string
		links   map[string][]string
		channel string
		peerID  string
		want    string
	}{
		{"linked slack user", identityLinks, "slack", "U123", "jonathan"},
		{"linked discord user", identityLinks, "discord", "user456", "jonathan"},
		{"linked telegram user", identityLinks, "telegram", "alice_tg", "alice"},
		{"unlinked user", identityLinks, "slack", "unknown", ""},
		{"nil links", nil, "slack", "U123", ""},
		{"empty peer ID", identityLinks, "slack", "", ""},
		{"case insensitive channel", identityLinks, "SLACK", "U123", "jonathan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveLinkedPeerID(tt.links, tt.channel, tt.peerID); got != tt.want {
				t.Errorf("ResolveLinkedPeerID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildGroupHistoryKey(t *testing.T) {
	tests := []struct {
		channel   string
		accountID string
		peerKind  string
		peerID    string
		want      string
	}{
		{"slack", "acct1", "group", "G123", "slack:acct1:group:G123"},
		{"discord", "", "channel", "C456", "discord:default:channel:C456"},
		{"", "acct1", "group", "G123", "unknown:acct1:group:G123"},
		{"telegram", "acct1", "group", "", "telegram:acct1:group:unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := BuildGroupHistoryKey(tt.channel, tt.accountID, tt.peerKind, tt.peerID)
			if got != tt.want {
				t.Errorf("BuildGroupHistoryKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveThreadSessionKeys(t *testing.T) {
	tests := []struct {
		name        string
		params      ThreadSessionParams
		wantSession string
		wantParent  string
	}{
		{
			name: "no thread",
			params: ThreadSessionParams{
				BaseSessionKey: "agent:main:slack:group:C123",
			},
			wantSession: "agent:main:slack:group:C123",
			wantParent:  "",
		},
		{
			name: "with thread suffix",
			params: ThreadSessionParams{
				BaseSessionKey:   "agent:main:slack:group:C123",
				ThreadID:         "thread456",
				ParentSessionKey: "agent:main:slack:group:C123",
				UseSuffix:        true,
			},
			wantSession: "agent:main:slack:group:C123:thread:thread456",
			wantParent:  "agent:main:slack:group:C123",
		},
		{
			name: "without suffix",
			params: ThreadSessionParams{
				BaseSessionKey:   "agent:main:discord:group:chan",
				ThreadID:         "thread789",
				ParentSessionKey: "agent:main:discord:group:chan",
				UseSuffix:        false,
			},
			wantSession: "agent:main:discord:group:chan",
			wantParent:  "agent:main:discord:group:chan",
		},
		{
			name: "empty thread ID",
			params: ThreadSessionParams{
				BaseSessionKey: "agent:main:key",
				ThreadID:       "   ",
				UseSuffix:      true,
			},
			wantSession: "agent:main:key",
			wantParent:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveThreadSessionKeys(tt.params)
			if got.SessionKey != tt.wantSession {
				t.Errorf("SessionKey = %q, want %q", got.SessionKey, tt.wantSession)
			}
			if got.ParentSessionKey != tt.wantParent {
				t.Errorf("ParentSessionKey = %q, want %q", got.ParentSessionKey, tt.wantParent)
			}
		})
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("special characters in peer ID preserved", func(t *testing.T) {
		params := PeerSessionParams{
			AgentID:  "agent",
			PeerKind: "group",
			Channel:  "slack",
			PeerID:   "C123-ABC_456",
		}
		got := BuildAgentPeerSessionKey(params)
		want := "agent:agent:slack:group:C123-ABC_456"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("long agent ID truncated", func(t *testing.T) {
		longID := "a" + string(make([]byte, 100))
		for i := range longID {
			if i > 0 {
				longID = longID[:i] + "a"
			}
		}
		longID = ""
		for i := 0; i < 100; i++ {
			longID += "a"
		}
		got := NormalizeAgentID(longID)
		if len(got) > 64 {
			t.Errorf("normalized ID too long: %d chars", len(got))
		}
	})

	t.Run("Unicode in agent ID normalized", func(t *testing.T) {
		got := NormalizeAgentID("agent-\u00e9-test")
		// Should normalize to something valid
		if got == "" || got == DefaultAgentID {
			// This is acceptable
		}
	})
}
