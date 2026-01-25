package datetime

import (
	"testing"
	"time"
)

func TestOrdinalSuffix(t *testing.T) {
	tests := []struct {
		day  int
		want string
	}{
		// Standard cases
		{1, "st"},
		{2, "nd"},
		{3, "rd"},
		{4, "th"},
		{5, "th"},
		{9, "th"},
		{10, "th"},

		// Special cases: 11, 12, 13 always use "th"
		{11, "th"},
		{12, "th"},
		{13, "th"},

		// 21, 22, 23
		{21, "st"},
		{22, "nd"},
		{23, "rd"},
		{24, "th"},

		// 31
		{31, "st"},

		// Edge cases
		{0, "th"},
		{100, "th"},
		{101, "st"},
		{111, "th"}, // 111 ends in 11
		{112, "th"}, // 112 ends in 12
		{113, "th"}, // 113 ends in 13
	}

	for _, tt := range tests {
		t.Run(string(rune('0'+tt.day%10)), func(t *testing.T) {
			got := OrdinalSuffix(tt.day)
			if got != tt.want {
				t.Errorf("OrdinalSuffix(%d) = %q, want %q", tt.day, got, tt.want)
			}
		})
	}
}

func TestFormatUserTime(t *testing.T) {
	// Reference time: Friday, January 24, 2025, 14:30 UTC
	refTime := time.Date(2025, 1, 24, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		time     time.Time
		timezone string
		format   ResolvedTimeFormat
		want     string
	}{
		{
			name:     "24-hour format UTC",
			time:     refTime,
			timezone: "UTC",
			format:   Resolved24Hour,
			want:     "Friday, January 24th, 2025 - 14:30",
		},
		{
			name:     "12-hour format UTC",
			time:     refTime,
			timezone: "UTC",
			format:   Resolved12Hour,
			want:     "Friday, January 24th, 2025 - 2:30 PM",
		},
		{
			name:     "different timezone",
			time:     refTime,
			timezone: "America/New_York",
			format:   Resolved24Hour,
			want:     "Friday, January 24th, 2025 - 09:30", // UTC-5
		},
		{
			name:     "1st day ordinal",
			time:     time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC),
			timezone: "UTC",
			format:   Resolved24Hour,
			want:     "Wednesday, January 1st, 2025 - 10:00",
		},
		{
			name:     "2nd day ordinal",
			time:     time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC),
			timezone: "UTC",
			format:   Resolved24Hour,
			want:     "Thursday, January 2nd, 2025 - 10:00",
		},
		{
			name:     "3rd day ordinal",
			time:     time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC),
			timezone: "UTC",
			format:   Resolved24Hour,
			want:     "Friday, January 3rd, 2025 - 10:00",
		},
		{
			name:     "11th day ordinal",
			time:     time.Date(2025, 1, 11, 10, 0, 0, 0, time.UTC),
			timezone: "UTC",
			format:   Resolved24Hour,
			want:     "Saturday, January 11th, 2025 - 10:00",
		},
		{
			name:     "midnight 12-hour",
			time:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			timezone: "UTC",
			format:   Resolved12Hour,
			want:     "Wednesday, January 1st, 2025 - 12:00 AM",
		},
		{
			name:     "noon 12-hour",
			time:     time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
			timezone: "UTC",
			format:   Resolved12Hour,
			want:     "Wednesday, January 1st, 2025 - 12:00 PM",
		},
		{
			name:     "morning 12-hour",
			time:     time.Date(2025, 1, 1, 9, 15, 0, 0, time.UTC),
			timezone: "UTC",
			format:   Resolved12Hour,
			want:     "Wednesday, January 1st, 2025 - 9:15 AM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatUserTime(tt.time, tt.timezone, tt.format)
			if got != tt.want {
				t.Errorf("FormatUserTime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatUserTimeInvalidTimezone(t *testing.T) {
	refTime := time.Date(2025, 1, 24, 14, 30, 0, 0, time.UTC)
	got := FormatUserTime(refTime, "Invalid/Timezone", Resolved24Hour)
	if got != "" {
		t.Errorf("FormatUserTime with invalid timezone = %q, want empty string", got)
	}
}

func TestFormatUserTimeWithTimezone(t *testing.T) {
	refTime := time.Date(2025, 1, 24, 14, 30, 0, 0, time.UTC)

	got := FormatUserTimeWithTimezone(refTime, "UTC", Resolved24Hour)
	want := "Friday, January 24th, 2025 - 14:30 (UTC)"
	if got != want {
		t.Errorf("FormatUserTimeWithTimezone() = %q, want %q", got, want)
	}

	// Test with invalid timezone
	got = FormatUserTimeWithTimezone(refTime, "Invalid/Zone", Resolved24Hour)
	if got != "" {
		t.Errorf("FormatUserTimeWithTimezone with invalid timezone = %q, want empty", got)
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Date(2025, 1, 24, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		time time.Time
		want string
	}{
		// Past times
		{
			name: "just now",
			time: now.Add(-30 * time.Second),
			want: "just now",
		},
		{
			name: "1 minute ago",
			time: now.Add(-1 * time.Minute),
			want: "1 minute ago",
		},
		{
			name: "5 minutes ago",
			time: now.Add(-5 * time.Minute),
			want: "5 minutes ago",
		},
		{
			name: "1 hour ago",
			time: now.Add(-1 * time.Hour),
			want: "1 hour ago",
		},
		{
			name: "3 hours ago",
			time: now.Add(-3 * time.Hour),
			want: "3 hours ago",
		},
		{
			name: "yesterday",
			time: now.Add(-24 * time.Hour),
			want: "yesterday",
		},
		{
			name: "3 days ago",
			time: now.Add(-3 * 24 * time.Hour),
			want: "3 days ago",
		},
		{
			name: "1 week ago",
			time: now.Add(-7 * 24 * time.Hour),
			want: "1 week ago",
		},
		{
			name: "2 weeks ago",
			time: now.Add(-14 * 24 * time.Hour),
			want: "2 weeks ago",
		},
		{
			name: "1 month ago",
			time: now.Add(-30 * 24 * time.Hour),
			want: "1 month ago",
		},
		{
			name: "6 months ago",
			time: now.Add(-180 * 24 * time.Hour),
			want: "6 months ago",
		},
		{
			name: "1 year ago",
			time: now.Add(-365 * 24 * time.Hour),
			want: "1 year ago",
		},
		{
			name: "2 years ago",
			time: now.Add(-730 * 24 * time.Hour),
			want: "2 years ago",
		},

		// Future times
		{
			name: "in a moment",
			time: now.Add(30 * time.Second),
			want: "in a moment",
		},
		{
			name: "in 1 minute",
			time: now.Add(1 * time.Minute),
			want: "in 1 minute",
		},
		{
			name: "in 5 minutes",
			time: now.Add(5 * time.Minute),
			want: "in 5 minutes",
		},
		{
			name: "in 1 hour",
			time: now.Add(1 * time.Hour),
			want: "in 1 hour",
		},
		{
			name: "tomorrow",
			time: now.Add(24 * time.Hour),
			want: "tomorrow",
		},
		{
			name: "in 3 days",
			time: now.Add(3 * 24 * time.Hour),
			want: "in 3 days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRelativeTime(tt.time, now)
			if got != tt.want {
				t.Errorf("FormatRelativeTime() = %q, want %q", got, tt.want)
			}
		})
	}
}
