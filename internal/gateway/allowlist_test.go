package gateway

import (
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestNormalizeAllowToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"user123", "user123"},
		{"@user123", "user123"},
		{"#channel", "channel"},
		{"  @User123  ", "user123"},
		{"U:user123", "user123"},
		{"user:U12345", "u12345"},
		{"@User:prefix:value", "prefix:value"},
		{"*", "*"},
		{"UPPERCASE", "uppercase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeAllowToken(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeAllowToken(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSenderMatchesAllowlist(t *testing.T) {
	tests := []struct {
		name     string
		senderID string
		allow    []string
		expected bool
	}{
		{
			name:     "empty sender",
			senderID: "",
			allow:    []string{"user123"},
			expected: false,
		},
		{
			name:     "empty allowlist",
			senderID: "user123",
			allow:    []string{},
			expected: false,
		},
		{
			name:     "exact match",
			senderID: "user123",
			allow:    []string{"user123"},
			expected: true,
		},
		{
			name:     "wildcard match",
			senderID: "user123",
			allow:    []string{"*"},
			expected: true,
		},
		{
			name:     "case insensitive match",
			senderID: "User123",
			allow:    []string{"user123"},
			expected: true,
		},
		{
			name:     "no match",
			senderID: "user456",
			allow:    []string{"user123", "user789"},
			expected: false,
		},
		{
			name:     "match with @ prefix in allowlist",
			senderID: "user123",
			allow:    []string{"@user123"},
			expected: true,
		},
		{
			name:     "match with @ prefix in sender",
			senderID: "@user123",
			allow:    []string{"user123"},
			expected: true,
		},
		{
			name:     "skip empty entries in allowlist",
			senderID: "user123",
			allow:    []string{"", "  ", "user123"},
			expected: true,
		},
		{
			name:     "match with colon prefix",
			senderID: "U:user123",
			allow:    []string{"user123"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := senderMatchesAllowlist(tt.senderID, tt.allow)
			if result != tt.expected {
				t.Errorf("senderMatchesAllowlist(%q, %v) = %v, want %v", tt.senderID, tt.allow, result, tt.expected)
			}
		})
	}
}

func TestAllowlistForChannel(t *testing.T) {
	tests := []struct {
		name      string
		allowFrom map[string][]string
		channel   models.ChannelType
		expected  []string
	}{
		{
			name:      "nil map",
			allowFrom: nil,
			channel:   models.ChannelTelegram,
			expected:  nil,
		},
		{
			name:      "empty map",
			allowFrom: map[string][]string{},
			channel:   models.ChannelTelegram,
			expected:  nil,
		},
		{
			name: "channel specific allowlist",
			allowFrom: map[string][]string{
				"telegram": {"user1", "user2"},
				"slack":    {"user3"},
			},
			channel:  models.ChannelTelegram,
			expected: []string{"user1", "user2"},
		},
		{
			name: "fallback to default",
			allowFrom: map[string][]string{
				"slack":   {"user1"},
				"default": {"defaultuser"},
			},
			channel:  models.ChannelTelegram,
			expected: []string{"defaultuser"},
		},
		{
			name: "no match and no default",
			allowFrom: map[string][]string{
				"slack": {"user1"},
			},
			channel:  models.ChannelTelegram,
			expected: nil,
		},
		{
			name: "channel key is lowercase",
			allowFrom: map[string][]string{
				"telegram": {"user1"},
			},
			channel:  models.ChannelTelegram,
			expected: []string{"user1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := allowlistForChannel(tt.allowFrom, tt.channel)
			if len(result) != len(tt.expected) {
				t.Errorf("allowlistForChannel() = %v, want %v", result, tt.expected)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("allowlistForChannel()[%d] = %q, want %q", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestAllowlistMatches(t *testing.T) {
	tests := []struct {
		name      string
		allowFrom map[string][]string
		channel   models.ChannelType
		senderID  string
		expected  bool
	}{
		{
			name:      "empty sender returns false",
			allowFrom: map[string][]string{"telegram": {"user1"}},
			channel:   models.ChannelTelegram,
			senderID:  "",
			expected:  false,
		},
		{
			name:      "no allowlist returns false",
			allowFrom: map[string][]string{},
			channel:   models.ChannelTelegram,
			senderID:  "user1",
			expected:  false,
		},
		{
			name:      "matching sender returns true",
			allowFrom: map[string][]string{"telegram": {"user1", "user2"}},
			channel:   models.ChannelTelegram,
			senderID:  "user1",
			expected:  true,
		},
		{
			name:      "non-matching sender returns false",
			allowFrom: map[string][]string{"telegram": {"user1", "user2"}},
			channel:   models.ChannelTelegram,
			senderID:  "user3",
			expected:  false,
		},
		{
			name:      "wildcard matches any sender",
			allowFrom: map[string][]string{"telegram": {"*"}},
			channel:   models.ChannelTelegram,
			senderID:  "anyuser",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := allowlistMatches(tt.allowFrom, tt.channel, tt.senderID)
			if result != tt.expected {
				t.Errorf("allowlistMatches() = %v, want %v", result, tt.expected)
			}
		})
	}
}
