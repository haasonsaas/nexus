package heartbeat

import "testing"

func TestResolveVisibilityMode_ExplicitMode(t *testing.T) {
	tests := []struct {
		mode     string
		channel  string
		expected VisibilityMode
	}{
		{"typing", "any", VisibilityTyping},
		{"TYPING", "any", VisibilityTyping},
		{"  typing  ", "any", VisibilityTyping},
		{"presence", "any", VisibilityPresence},
		{"PRESENCE", "any", VisibilityPresence},
		{"none", "any", VisibilityNone},
		{"NONE", "any", VisibilityNone},
	}

	for _, tt := range tests {
		result := ResolveVisibilityMode(tt.mode, tt.channel)
		if result != tt.expected {
			t.Errorf("ResolveVisibilityMode(%q, %q) = %q, want %q",
				tt.mode, tt.channel, result, tt.expected)
		}
	}
}

func TestResolveVisibilityMode_ChannelDefaults(t *testing.T) {
	tests := []struct {
		channel  string
		expected VisibilityMode
	}{
		{"slack", VisibilityTyping},
		{"SLACK", VisibilityTyping},
		{"discord", VisibilityTyping},
		{"telegram", VisibilityTyping},
		{"matrix", VisibilityTyping},
		{"web", VisibilityPresence},
		{"api", VisibilityNone},
		{"cli", VisibilityNone},
		{"personal", VisibilityNone},
		{"unknown", VisibilityNone},
		{"", VisibilityNone},
	}

	for _, tt := range tests {
		result := ResolveVisibilityMode("", tt.channel)
		if result != tt.expected {
			t.Errorf("ResolveVisibilityMode('', %q) = %q, want %q",
				tt.channel, result, tt.expected)
		}
	}
}

func TestResolveVisibilityMode_InvalidMode(t *testing.T) {
	// Invalid mode should fall back to channel default
	result := ResolveVisibilityMode("invalid", "slack")
	if result != VisibilityTyping {
		t.Errorf("expected VisibilityTyping for invalid mode with slack channel, got %q", result)
	}

	result = ResolveVisibilityMode("invalid", "unknown")
	if result != VisibilityNone {
		t.Errorf("expected VisibilityNone for invalid mode with unknown channel, got %q", result)
	}
}

func TestShouldSendTyping(t *testing.T) {
	tests := []struct {
		mode     VisibilityMode
		expected bool
	}{
		{VisibilityTyping, true},
		{VisibilityPresence, false},
		{VisibilityNone, false},
		{VisibilityMode("invalid"), false},
	}

	for _, tt := range tests {
		result := ShouldSendTyping(tt.mode)
		if result != tt.expected {
			t.Errorf("ShouldSendTyping(%q) = %v, want %v", tt.mode, result, tt.expected)
		}
	}
}

func TestShouldSendPresence(t *testing.T) {
	tests := []struct {
		mode     VisibilityMode
		expected bool
	}{
		{VisibilityTyping, true},   // Typing implies presence
		{VisibilityPresence, true}, // Explicit presence
		{VisibilityNone, false},
		{VisibilityMode("invalid"), false},
	}

	for _, tt := range tests {
		result := ShouldSendPresence(tt.mode)
		if result != tt.expected {
			t.Errorf("ShouldSendPresence(%q) = %v, want %v", tt.mode, result, tt.expected)
		}
	}
}

func TestParseVisibilityMode(t *testing.T) {
	tests := []struct {
		input    string
		expected VisibilityMode
	}{
		{"typing", VisibilityTyping},
		{"TYPING", VisibilityTyping},
		{"  Typing  ", VisibilityTyping},
		{"presence", VisibilityPresence},
		{"PRESENCE", VisibilityPresence},
		{"none", VisibilityNone},
		{"NONE", VisibilityNone},
		{"", VisibilityNone},
		{"invalid", VisibilityNone},
		{"   ", VisibilityNone},
	}

	for _, tt := range tests {
		result := ParseVisibilityMode(tt.input)
		if result != tt.expected {
			t.Errorf("ParseVisibilityMode(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestVisibilityMode_String(t *testing.T) {
	tests := []struct {
		mode     VisibilityMode
		expected string
	}{
		{VisibilityTyping, "typing"},
		{VisibilityPresence, "presence"},
		{VisibilityNone, "none"},
	}

	for _, tt := range tests {
		if tt.mode.String() != tt.expected {
			t.Errorf("VisibilityMode(%q).String() = %q, want %q",
				tt.mode, tt.mode.String(), tt.expected)
		}
	}
}

func TestVisibilityMode_IsValid(t *testing.T) {
	tests := []struct {
		mode     VisibilityMode
		expected bool
	}{
		{VisibilityTyping, true},
		{VisibilityPresence, true},
		{VisibilityNone, true},
		{VisibilityMode(""), false},
		{VisibilityMode("invalid"), false},
		{VisibilityMode("TYPING"), false}, // Must be lowercase
	}

	for _, tt := range tests {
		if tt.mode.IsValid() != tt.expected {
			t.Errorf("VisibilityMode(%q).IsValid() = %v, want %v",
				tt.mode, tt.mode.IsValid(), tt.expected)
		}
	}
}

func TestChannelDefaults_AllKnownChannels(t *testing.T) {
	// Ensure all channel defaults are valid visibility modes
	for channel, mode := range channelDefaults {
		if !mode.IsValid() {
			t.Errorf("channel %q has invalid default mode: %q", channel, mode)
		}
	}
}

func TestVisibilityMode_Constants(t *testing.T) {
	// Verify constant values match expected strings
	if VisibilityTyping != "typing" {
		t.Errorf("VisibilityTyping = %q, want %q", VisibilityTyping, "typing")
	}
	if VisibilityPresence != "presence" {
		t.Errorf("VisibilityPresence = %q, want %q", VisibilityPresence, "presence")
	}
	if VisibilityNone != "none" {
		t.Errorf("VisibilityNone = %q, want %q", VisibilityNone, "none")
	}
}
