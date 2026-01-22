// Package imessage provides an iMessage channel adapter for macOS.
//go:build darwin
// +build darwin

package imessage

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("expected Enabled to be false by default")
	}
	if cfg.DatabasePath != "~/Library/Messages/chat.db" {
		t.Errorf("expected DatabasePath to be ~/Library/Messages/chat.db, got %s", cfg.DatabasePath)
	}
	if cfg.PollInterval != "1s" {
		t.Errorf("expected PollInterval to be 1s, got %s", cfg.PollInterval)
	}
	if !cfg.Personal.SyncOnStart {
		t.Error("expected SyncOnStart to be true by default")
	}
	if cfg.Personal.Presence.SendReadReceipts {
		t.Error("expected SendReadReceipts to be false (not supported)")
	}
	if cfg.Personal.Presence.SendTyping {
		t.Error("expected SendTyping to be false (not supported)")
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHome bool
	}{
		{
			name:     "tilde path",
			input:    "~/Library/Messages/chat.db",
			wantHome: true,
		},
		{
			name:     "absolute path",
			input:    "/var/db/messages.db",
			wantHome: false,
		},
		{
			name:     "relative path",
			input:    "messages.db",
			wantHome: false,
		},
		{
			name:     "tilde only",
			input:    "~",
			wantHome: false, // Only ~/ is expanded, not ~
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPath(tt.input)
			if tt.wantHome {
				if result == tt.input {
					t.Errorf("expected path to be expanded, got %s", result)
				}
				if result[0] == '~' {
					t.Errorf("expected tilde to be replaced, got %s", result)
				}
			} else {
				if tt.input[0] != '~' && result != tt.input {
					t.Errorf("expected path unchanged, got %s", result)
				}
			}
		})
	}
}

func TestEscapeAppleScript(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no escaping needed",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "escape quotes",
			input:    `Hello "World"`,
			expected: `Hello \"World\"`,
		},
		{
			name:     "escape backslashes",
			input:    `Hello\World`,
			expected: `Hello\\World`,
		},
		{
			name:     "escape both",
			input:    `Say "Hello\World"`,
			expected: `Say \"Hello\\World\"`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "multiple backslashes",
			input:    `a\\b`,
			expected: `a\\\\b`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeAppleScript(tt.input)
			if result != tt.expected {
				t.Errorf("escapeAppleScript(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAppleTimestampToTime(t *testing.T) {
	tests := []struct {
		name     string
		nano     int64
		expected time.Time
	}{
		{
			name:     "zero timestamp",
			nano:     0,
			expected: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one second after epoch",
			nano:     1_000_000_000, // 1 second in nanoseconds
			expected: time.Date(2001, 1, 1, 0, 0, 1, 0, time.UTC),
		},
		{
			name:     "one day after epoch",
			nano:     24 * 60 * 60 * 1_000_000_000, // 1 day in nanoseconds
			expected: time.Date(2001, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "specific timestamp",
			nano:     700_000_000_000_000_000, // roughly 22+ years
			expected: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(700_000_000_000_000_000) * time.Nanosecond),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appleTimestampToTime(tt.nano)
			if !result.Equal(tt.expected) {
				t.Errorf("appleTimestampToTime(%d) = %v, want %v", tt.nano, result, tt.expected)
			}
		})
	}
}

func TestNewAdapterNilConfig(t *testing.T) {
	// New should accept nil config and use defaults
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New(nil, nil) error = %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.pollInterval != time.Second {
		t.Errorf("expected default poll interval of 1s, got %v", adapter.pollInterval)
	}
}

func TestNewAdapterWithConfig(t *testing.T) {
	cfg := &Config{
		Enabled:      true,
		DatabasePath: "/custom/path.db",
		PollInterval: "5s",
	}

	adapter, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.pollInterval != 5*time.Second {
		t.Errorf("expected poll interval of 5s, got %v", adapter.pollInterval)
	}
	if adapter.config.DatabasePath != "/custom/path.db" {
		t.Errorf("expected DatabasePath /custom/path.db, got %s", adapter.config.DatabasePath)
	}
}

func TestNewAdapterInvalidPollInterval(t *testing.T) {
	cfg := &Config{
		DatabasePath: "/custom/path.db",
		PollInterval: "invalid",
	}

	adapter, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Should fallback to 1 second
	if adapter.pollInterval != time.Second {
		t.Errorf("expected fallback poll interval of 1s, got %v", adapter.pollInterval)
	}
}
