package format

import (
	"math"
	"testing"
)

func TestFormatDurationSeconds(t *testing.T) {
	tests := []struct {
		name     string
		ms       float64
		opts     *DurationSecondsOptions
		expected string
	}{
		// Basic cases
		{"zero", 0, nil, "0s"},
		{"one second", 1000, nil, "1s"},
		{"one and half seconds", 1500, nil, "1.5s"},
		{"ten seconds", 10000, nil, "10s"},

		// Sub-second
		{"100ms", 100, nil, "0.1s"},
		{"50ms", 50, nil, "0.1s"}, // rounds to 0.1 with default 1 decimal
		{"10ms", 10, nil, "0s"},   // rounds to 0.0, then trimmed

		// Trimming trailing zeros
		{"1000ms exact", 1000, nil, "1s"},
		{"2000ms exact", 2000, nil, "2s"},
		{"1100ms", 1100, nil, "1.1s"},

		// Custom decimals
		{"2 decimals", 1234, &DurationSecondsOptions{Decimals: 2}, "1.23s"},
		{"3 decimals", 1234, &DurationSecondsOptions{Decimals: 3}, "1.234s"},
		{"0 decimals treated as 1", 1500, &DurationSecondsOptions{Decimals: 0}, "1.5s"},

		// Unit options
		{"unit seconds", 5000, &DurationSecondsOptions{Unit: "seconds"}, "5 seconds"},
		{"unit s (explicit)", 5000, &DurationSecondsOptions{Unit: "s"}, "5s"},

		// Negative values
		{"negative becomes zero", -1000, nil, "0s"},

		// Large values
		{"one minute", 60000, nil, "60s"},
		{"one hour", 3600000, nil, "3600s"},

		// Non-finite values
		{"NaN", math.NaN(), nil, "unknown"},
		{"positive infinity", math.Inf(1), nil, "unknown"},
		{"negative infinity", math.Inf(-1), nil, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDurationSeconds(tt.ms, tt.opts)
			if result != tt.expected {
				t.Errorf("FormatDurationSeconds(%v, %+v) = %q, want %q",
					tt.ms, tt.opts, result, tt.expected)
			}
		})
	}
}

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		name     string
		ms       float64
		opts     *DurationMsOptions
		expected string
	}{
		// Under 1000ms - should format as ms
		{"zero", 0, nil, "0ms"},
		{"100ms", 100, nil, "100ms"},
		{"500ms", 500, nil, "500ms"},
		{"999ms", 999, nil, "999ms"},

		// At and over 1000ms - should format as seconds
		{"exactly 1000ms", 1000, nil, "1s"},
		{"1234ms", 1234, nil, "1.23s"},
		{"1500ms", 1500, nil, "1.5s"},
		{"2000ms", 2000, nil, "2s"},

		// Larger values
		{"10 seconds", 10000, nil, "10s"},
		{"one minute", 60000, nil, "60s"},

		// Custom decimals for seconds
		{"custom decimals", 1234, &DurationMsOptions{Decimals: 3}, "1.234s"},

		// Unit option
		{"unit seconds", 5000, &DurationMsOptions{Unit: "seconds"}, "5 seconds"},

		// Non-finite values
		{"NaN", math.NaN(), nil, "unknown"},
		{"infinity", math.Inf(1), nil, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDurationMs(tt.ms, tt.opts)
			if result != tt.expected {
				t.Errorf("FormatDurationMs(%v, %+v) = %q, want %q",
					tt.ms, tt.opts, result, tt.expected)
			}
		})
	}
}

func TestFormatDurationMsInt(t *testing.T) {
	tests := []struct {
		name     string
		ms       int64
		expected string
	}{
		{"zero", 0, "0ms"},
		{"under second", 500, "500ms"},
		{"one second", 1000, "1s"},
		{"over second", 1500, "1.5s"},
		{"large value", 65432, "65.43s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDurationMsInt(tt.ms)
			if result != tt.expected {
				t.Errorf("FormatDurationMsInt(%d) = %q, want %q",
					tt.ms, result, tt.expected)
			}
		})
	}
}

func TestTrimTrailingZeros(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.50", "1.5"},
		{"2.00", "2"},
		{"3.000", "3"},
		{"1.234", "1.234"},
		{"1.230", "1.23"},
		{"100", "100"},
		{"0.0", "0"},
		{"0.10", "0.1"},
		{"10.0", "10"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := trimTrailingZeros(tt.input)
			if result != tt.expected {
				t.Errorf("trimTrailingZeros(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatDurationSeconds_EdgeCases(t *testing.T) {
	// Test very small values
	result := FormatDurationSeconds(0.1, nil)
	if result != "0s" {
		t.Errorf("very small value: got %q, want %q", result, "0s")
	}

	// Test very large values
	result = FormatDurationSeconds(86400000, nil) // 24 hours in ms
	if result != "86400s" {
		t.Errorf("very large value: got %q, want %q", result, "86400s")
	}

	// Test with high decimals
	result = FormatDurationSeconds(1111, &DurationSecondsOptions{Decimals: 5})
	if result != "1.111s" { // Should trim trailing zeros
		t.Errorf("high decimals: got %q, want %q", result, "1.111s")
	}
}

func TestFormatDurationMs_BoundaryConditions(t *testing.T) {
	// Test boundary at exactly 1000ms
	result := FormatDurationMs(999.9, nil)
	if result != "1000ms" { // Rounds to 1000, but still under 1000 threshold? Actually rounds to 1000
		// Note: 999.9 rounds to 1000 in "%.0fms" formatting
		if result != "1000ms" {
			t.Errorf("boundary 999.9: got %q", result)
		}
	}

	result = FormatDurationMs(1000.1, nil)
	if result != "1s" {
		t.Errorf("boundary 1000.1: got %q, want %q", result, "1s")
	}
}

func BenchmarkFormatDurationSeconds(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FormatDurationSeconds(12345.67, nil)
	}
}

func BenchmarkFormatDurationMs(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FormatDurationMs(12345.67, nil)
	}
}

func BenchmarkFormatDurationMsInt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FormatDurationMsInt(12345)
	}
}
