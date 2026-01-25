// Package usage provides formatting utilities for usage data.
package usage

import "fmt"

// FormatPercentage formats a percentage value.
func FormatPercentage(value float64) string {
	if value < 1 {
		return fmt.Sprintf("%.2f%%", value)
	}
	if value < 10 {
		return fmt.Sprintf("%.1f%%", value)
	}
	return fmt.Sprintf("%.0f%%", value)
}

// FormatDurationMs formats a duration in milliseconds.
func FormatDurationMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	if ms < 3600000 {
		return fmt.Sprintf("%.1fm", float64(ms)/60000)
	}
	return fmt.Sprintf("%.1fh", float64(ms)/3600000)
}
