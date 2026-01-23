package reminders

import (
	"testing"
	"time"
)

func TestParseWhen_RelativeTime(t *testing.T) {
	tests := []struct {
		input    string
		minDelta time.Duration
		maxDelta time.Duration
	}{
		{"in 5 minutes", 4 * time.Minute, 6 * time.Minute},
		{"in 1 hour", 59 * time.Minute, 61 * time.Minute},
		{"in 30 seconds", 25 * time.Second, 35 * time.Second},
		{"in 2 hours", 119 * time.Minute, 121 * time.Minute},
		{"in 1 day", 23 * time.Hour, 25 * time.Hour},
		{"in 10 mins", 9 * time.Minute, 11 * time.Minute},
		{"in 2 hrs", 119 * time.Minute, 121 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseWhen(tt.input)
			if err != nil {
				t.Fatalf("parseWhen(%q) failed: %v", tt.input, err)
			}

			delta := time.Until(result)
			if delta < tt.minDelta || delta > tt.maxDelta {
				t.Errorf("parseWhen(%q) = %v from now, want between %v and %v", tt.input, delta, tt.minDelta, tt.maxDelta)
			}
		})
	}
}

func TestParseWhen_InvalidInput(t *testing.T) {
	tests := []string{
		"",
		"now",
		"yesterday",
		"in",
		"in 5",
		"in minutes",
		"5 minutes",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := parseWhen(input)
			if err == nil {
				t.Errorf("parseWhen(%q) should have failed", input)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{30 * time.Second, "30 seconds"},
		{1 * time.Minute, "1 minute"},
		{5 * time.Minute, "5 minutes"},
		{1 * time.Hour, "1 hour"},
		{2 * time.Hour, "2.0 hours"},
		{24 * time.Hour, "1 day"},
		{48 * time.Hour, "2.0 days"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.input)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatReminderName(t *testing.T) {
	tests := []struct {
		title    string
		message  string
		expected string
	}{
		{"", "Short message", "Reminder: Short message"},
		{"Custom Title", "Any message", "Reminder: Custom Title"},
		{"", "This is a very long message that exceeds fifty characters and should be truncated", "Reminder: This is a very long message that exceeds fifty ..."},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatReminderName(tt.title, tt.message)
			if result != tt.expected {
				t.Errorf("formatReminderName(%q, %q) = %q, want %q", tt.title, tt.message, result, tt.expected)
			}
		})
	}
}
