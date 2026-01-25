package datetime

import (
	"fmt"
	"time"
)

// OrdinalSuffix returns the English ordinal suffix for a day number.
// Examples: 1 -> "st", 2 -> "nd", 3 -> "rd", 4 -> "th", 11 -> "th", 21 -> "st"
func OrdinalSuffix(day int) string {
	// Special case for numbers ending in 11, 12, 13 which always use "th"
	// This handles 11-13, 111-113, etc.
	lastTwoDigits := day % 100
	if lastTwoDigits >= 11 && lastTwoDigits <= 13 {
		return "th"
	}
	switch day % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}

// FormatUserTime formats a time in a user-friendly way with the specified timezone and format.
// Returns a string like "Friday, January 24th, 2025 - 14:30" (24-hour) or
// "Friday, January 24th, 2025 - 2:30 PM" (12-hour).
// Returns empty string if formatting fails.
func FormatUserTime(t time.Time, timeZone string, format ResolvedTimeFormat) string {
	// Load the timezone
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		return ""
	}

	// Convert to the specified timezone
	localTime := t.In(loc)

	// Get date parts
	weekday := localTime.Weekday().String()
	month := localTime.Month().String()
	day := localTime.Day()
	year := localTime.Year()
	suffix := OrdinalSuffix(day)

	// Format time part based on preference
	var timePart string
	if format == Resolved24Hour {
		timePart = localTime.Format("15:04")
	} else {
		// 12-hour format
		hour := localTime.Hour()
		minute := localTime.Minute()
		period := "AM"
		if hour >= 12 {
			period = "PM"
		}
		if hour == 0 {
			hour = 12
		} else if hour > 12 {
			hour -= 12
		}
		timePart = fmt.Sprintf("%d:%02d %s", hour, minute, period)
	}

	return fmt.Sprintf("%s, %s %d%s, %d - %s", weekday, month, day, suffix, year, timePart)
}

// FormatUserTimeWithTimezone formats time and appends the timezone name.
// Returns a string like "Friday, January 24th, 2025 - 14:30 (America/New_York)".
func FormatUserTimeWithTimezone(t time.Time, timeZone string, format ResolvedTimeFormat) string {
	base := FormatUserTime(t, timeZone, format)
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s (%s)", base, timeZone)
}

// FormatRelativeTime returns a human-readable relative time string.
// Examples: "just now", "5 minutes ago", "2 hours ago", "yesterday", "3 days ago"
func FormatRelativeTime(t time.Time, now time.Time) string {
	diff := now.Sub(t)

	if diff < 0 {
		// Future time
		diff = -diff
		return formatFuture(diff)
	}

	return formatPast(diff)
}

func formatPast(diff time.Duration) string {
	seconds := int64(diff.Seconds())

	if seconds < 60 {
		return "just now"
	}

	minutes := seconds / 60
	if minutes == 1 {
		return "1 minute ago"
	}
	if minutes < 60 {
		return fmt.Sprintf("%d minutes ago", minutes)
	}

	hours := minutes / 60
	if hours == 1 {
		return "1 hour ago"
	}
	if hours < 24 {
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := hours / 24
	if days == 1 {
		return "yesterday"
	}
	if days < 7 {
		return fmt.Sprintf("%d days ago", days)
	}

	weeks := days / 7
	if weeks == 1 {
		return "1 week ago"
	}
	if weeks < 4 {
		return fmt.Sprintf("%d weeks ago", weeks)
	}

	months := days / 30
	if months == 1 {
		return "1 month ago"
	}
	if months < 12 {
		return fmt.Sprintf("%d months ago", months)
	}

	years := days / 365
	if years == 1 {
		return "1 year ago"
	}
	return fmt.Sprintf("%d years ago", years)
}

func formatFuture(diff time.Duration) string {
	seconds := int64(diff.Seconds())

	if seconds < 60 {
		return "in a moment"
	}

	minutes := seconds / 60
	if minutes == 1 {
		return "in 1 minute"
	}
	if minutes < 60 {
		return fmt.Sprintf("in %d minutes", minutes)
	}

	hours := minutes / 60
	if hours == 1 {
		return "in 1 hour"
	}
	if hours < 24 {
		return fmt.Sprintf("in %d hours", hours)
	}

	days := hours / 24
	if days == 1 {
		return "tomorrow"
	}
	if days < 7 {
		return fmt.Sprintf("in %d days", days)
	}

	weeks := days / 7
	if weeks == 1 {
		return "in 1 week"
	}
	if weeks < 4 {
		return fmt.Sprintf("in %d weeks", weeks)
	}

	months := days / 30
	if months == 1 {
		return "in 1 month"
	}
	if months < 12 {
		return fmt.Sprintf("in %d months", months)
	}

	years := days / 365
	if years == 1 {
		return "in 1 year"
	}
	return fmt.Sprintf("in %d years", years)
}
