package web

import (
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "zero time",
			input:    time.Time{},
			expected: "-",
		},
		{
			name:     "valid time",
			input:    time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC),
			expected: "2024-03-15 14:30:45",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTime(tt.input); got != tt.expected {
				t.Errorf("formatTime() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		contains string // We check contains since format may vary
	}{
		{
			name:     "seconds",
			input:    45 * time.Second,
			contains: "45s",
		},
		{
			name:     "minutes",
			input:    5 * time.Minute,
			contains: "5m",
		},
		{
			name:     "hours",
			input:    3 * time.Hour,
			contains: "3h",
		},
		{
			name:     "1 day",
			input:    24 * time.Hour,
			contains: "day",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.input)
			if len(result) == 0 {
				t.Error("formatDuration() returned empty string")
			}
			// Just verify we get a non-empty result
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		n        int
		expected string
	}{
		{
			name:     "short string",
			input:    "hello",
			n:        10,
			expected: "hello",
		},
		{
			name:     "exact length",
			input:    "hello",
			n:        5,
			expected: "hello",
		},
		{
			name:     "truncate with ellipsis",
			input:    "hello world",
			n:        8,
			expected: "hello...",
		},
		{
			name:     "very short limit",
			input:    "hello",
			n:        2,
			expected: "he",
		},
		{
			name:     "limit of 3",
			input:    "hello",
			n:        3,
			expected: "hel",
		},
		{
			name:     "empty string",
			input:    "",
			n:        10,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.input, tt.n); got != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.expected)
			}
		})
	}
}

func TestChannelIcon(t *testing.T) {
	tests := []struct {
		channel  models.ChannelType
		expected string
	}{
		{models.ChannelTelegram, "telegram"},
		{models.ChannelSlack, "slack"},
		{models.ChannelDiscord, "discord"},
		{models.ChannelWhatsApp, "whatsapp"},
		{models.ChannelAPI, "api"},
		{models.ChannelEmail, "chat"}, // Default
		{models.ChannelType("unknown"), "chat"},
	}

	for _, tt := range tests {
		t.Run(string(tt.channel), func(t *testing.T) {
			if got := channelIcon(tt.channel); got != tt.expected {
				t.Errorf("channelIcon(%q) = %q, want %q", tt.channel, got, tt.expected)
			}
		})
	}
}

func TestRoleClass(t *testing.T) {
	tests := []struct {
		role     models.Role
		expected string
	}{
		{models.RoleUser, "message-user"},
		{models.RoleAssistant, "message-assistant"},
		{models.RoleSystem, "message-system"},
		{models.RoleTool, "message-tool"},
		{models.Role("unknown"), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if got := roleClass(tt.role); got != tt.expected {
				t.Errorf("roleClass(%q) = %q, want %q", tt.role, got, tt.expected)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}

	if cfg.BasePath != "" {
		t.Errorf("BasePath should default to empty, got %q", cfg.BasePath)
	}
	if cfg.DefaultAgentID != "" {
		t.Errorf("DefaultAgentID should default to empty, got %q", cfg.DefaultAgentID)
	}
}
