// Package format provides formatting utilities.
package format

import (
	"fmt"
	"math"
	"strings"
)

// DurationSecondsOptions configures FormatDurationSeconds output.
type DurationSecondsOptions struct {
	// Decimals is the number of decimal places (default: 1)
	Decimals int
	// Unit is the suffix to use: "s" or "seconds" (default: "s")
	Unit string
}

// FormatDurationSeconds formats milliseconds as a seconds string.
// Returns "unknown" for non-finite values.
func FormatDurationSeconds(ms float64, opts *DurationSecondsOptions) string {
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return "unknown"
	}

	decimals := 1
	unit := "s"
	if opts != nil {
		if opts.Decimals > 0 {
			decimals = opts.Decimals
		}
		if opts.Unit == "seconds" {
			unit = " seconds"
		}
	}

	// Ensure non-negative
	if ms < 0 {
		ms = 0
	}

	seconds := ms / 1000

	// Format with specified decimals
	format := fmt.Sprintf("%%.%df", decimals)
	formatted := fmt.Sprintf(format, seconds)

	// Trim trailing zeros after decimal point
	formatted = trimTrailingZeros(formatted)

	return formatted + unit
}

// DurationMsOptions configures FormatDurationMs output.
type DurationMsOptions struct {
	// Decimals is the number of decimal places for seconds (default: 2)
	Decimals int
	// Unit is the suffix to use for seconds: "s" or "seconds" (default: "s")
	Unit string
}

// FormatDurationMs formats milliseconds as either "Xms" or seconds string.
// Values under 1000ms are formatted as "Xms", otherwise as seconds.
// Returns "unknown" for non-finite values.
func FormatDurationMs(ms float64, opts *DurationMsOptions) string {
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return "unknown"
	}

	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}

	decimals := 2
	unit := "s"
	if opts != nil {
		if opts.Decimals > 0 {
			decimals = opts.Decimals
		}
		if opts.Unit == "seconds" {
			unit = "seconds"
		}
	}

	return FormatDurationSeconds(ms, &DurationSecondsOptions{
		Decimals: decimals,
		Unit:     unit,
	})
}

// FormatDurationMsInt is a convenience wrapper for integer milliseconds.
func FormatDurationMsInt(ms int64) string {
	return FormatDurationMs(float64(ms), nil)
}

// trimTrailingZeros removes trailing zeros after the decimal point.
// e.g., "1.50" -> "1.5", "2.00" -> "2"
func trimTrailingZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}

	// Remove trailing zeros
	s = strings.TrimRight(s, "0")
	// Remove trailing decimal point if no decimals left
	s = strings.TrimRight(s, ".")

	return s
}
