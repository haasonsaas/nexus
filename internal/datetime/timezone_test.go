package datetime

import (
	"testing"
)

func TestResolveUserTimezone(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		wantValid  bool // whether we expect a valid timezone back
	}{
		{
			name:       "valid timezone",
			configured: "America/New_York",
			wantValid:  true,
		},
		{
			name:       "valid timezone with spaces",
			configured: "  Europe/London  ",
			wantValid:  true,
		},
		{
			name:       "UTC timezone",
			configured: "UTC",
			wantValid:  true,
		},
		{
			name:       "invalid timezone falls back",
			configured: "Invalid/Timezone",
			wantValid:  true, // falls back to host or UTC
		},
		{
			name:       "empty string falls back",
			configured: "",
			wantValid:  true, // falls back to host or UTC
		},
		{
			name:       "whitespace only falls back",
			configured: "   ",
			wantValid:  true, // falls back to host or UTC
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveUserTimezone(tt.configured)
			if got == "" {
				t.Errorf("ResolveUserTimezone(%q) returned empty string", tt.configured)
			}
			// For valid configured timezones, we expect them back
			if tt.configured == "America/New_York" && got != "America/New_York" {
				t.Errorf("ResolveUserTimezone(%q) = %q, want %q", tt.configured, got, "America/New_York")
			}
			if tt.configured == "  Europe/London  " && got != "Europe/London" {
				t.Errorf("ResolveUserTimezone(%q) = %q, want %q", tt.configured, got, "Europe/London")
			}
		})
	}
}

func TestResolveUserTimeFormat(t *testing.T) {
	// Clear cache before tests
	ClearCachedTimeFormat()

	tests := []struct {
		name       string
		preference TimeFormatPreference
		want       ResolvedTimeFormat
	}{
		{
			name:       "explicit 12-hour",
			preference: TimeFormat12,
			want:       Resolved12Hour,
		},
		{
			name:       "explicit 24-hour",
			preference: TimeFormat24,
			want:       Resolved24Hour,
		},
		{
			name:       "auto detection",
			preference: TimeFormatAuto,
			// Result depends on system, just verify it returns a valid value
		},
		{
			name:       "empty preference acts as auto",
			preference: "",
			// Result depends on system, just verify it returns a valid value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache for each test
			ClearCachedTimeFormat()

			got := ResolveUserTimeFormat(tt.preference)

			if tt.preference == TimeFormat12 || tt.preference == TimeFormat24 {
				if got != tt.want {
					t.Errorf("ResolveUserTimeFormat(%q) = %q, want %q", tt.preference, got, tt.want)
				}
			} else {
				// For auto, just verify we get a valid response
				if got != Resolved12Hour && got != Resolved24Hour {
					t.Errorf("ResolveUserTimeFormat(%q) = %q, want either %q or %q",
						tt.preference, got, Resolved12Hour, Resolved24Hour)
				}
			}
		})
	}
}

func TestResolveUserTimeFormatCaching(t *testing.T) {
	ClearCachedTimeFormat()

	// First call - should detect and cache
	first := ResolveUserTimeFormat(TimeFormatAuto)

	// Second call - should return cached value
	second := ResolveUserTimeFormat(TimeFormatAuto)

	if first != second {
		t.Errorf("Cached value changed: first=%q, second=%q", first, second)
	}

	// Explicit preferences should still work
	explicit12 := ResolveUserTimeFormat(TimeFormat12)
	if explicit12 != Resolved12Hour {
		t.Errorf("Explicit 12-hour returned %q, want %q", explicit12, Resolved12Hour)
	}

	explicit24 := ResolveUserTimeFormat(TimeFormat24)
	if explicit24 != Resolved24Hour {
		t.Errorf("Explicit 24-hour returned %q, want %q", explicit24, Resolved24Hour)
	}
}

func TestIsValidTimezone(t *testing.T) {
	tests := []struct {
		tz   string
		want bool
	}{
		{"UTC", true},
		{"America/New_York", true},
		{"Europe/London", true},
		{"Asia/Tokyo", true},
		{"Pacific/Auckland", true},
		{"Invalid/Zone", false},
		{"NotATimezone", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.tz, func(t *testing.T) {
			got := isValidTimezone(tt.tz)
			if got != tt.want {
				t.Errorf("isValidTimezone(%q) = %v, want %v", tt.tz, got, tt.want)
			}
		})
	}
}
