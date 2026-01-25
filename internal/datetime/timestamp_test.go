package datetime

import (
	"math"
	"testing"
	"time"
)

func TestNormalizeTimestamp(t *testing.T) {
	// Reference time for testing
	refTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	refMs := refTime.UnixMilli() // 1705314600000

	tests := []struct {
		name    string
		input   any
		wantMs  int64
		wantNil bool
	}{
		// nil and invalid inputs
		{
			name:    "nil input",
			input:   nil,
			wantNil: true,
		},

		// time.Time inputs
		{
			name:   "time.Time",
			input:  refTime,
			wantMs: refMs,
		},
		{
			name:   "pointer to time.Time",
			input:  &refTime,
			wantMs: refMs,
		},
		{
			name:    "nil pointer to time.Time",
			input:   (*time.Time)(nil),
			wantNil: true,
		},

		// int64 inputs (seconds)
		{
			name:   "int64 seconds",
			input:  int64(1705314600),
			wantMs: 1705314600000,
		},
		{
			name:   "int64 milliseconds",
			input:  int64(1705314600000),
			wantMs: 1705314600000,
		},

		// int inputs
		{
			name:   "int seconds",
			input:  1705314600,
			wantMs: 1705314600000,
		},

		// float64 inputs
		{
			name:   "float64 seconds",
			input:  1705314600.5,
			wantMs: 1705314600500,
		},
		{
			name:   "float64 seconds whole number",
			input:  float64(1705314600),
			wantMs: 1705314600000,
		},
		{
			name:    "float64 NaN",
			input:   math.NaN(),
			wantNil: true,
		},
		{
			name:    "float64 Inf",
			input:   math.Inf(1),
			wantNil: true,
		},

		// string inputs - numeric
		{
			name:   "string seconds",
			input:  "1705314600",
			wantMs: 1705314600000,
		},
		{
			name:   "string milliseconds",
			input:  "1705314600000",
			wantMs: 1705314600000,
		},
		{
			name:   "string with decimal (seconds)",
			input:  "1705314600.5",
			wantMs: 1705314600500,
		},
		{
			name:   "string with whitespace",
			input:  "  1705314600  ",
			wantMs: 1705314600000,
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "whitespace only string",
			input:   "   ",
			wantNil: true,
		},

		// string inputs - ISO dates
		{
			name:   "ISO date RFC3339",
			input:  "2024-01-15T10:30:00Z",
			wantMs: refMs,
		},
		{
			name:   "ISO date RFC3339 with timezone",
			input:  "2024-01-15T05:30:00-05:00",
			wantMs: refMs,
		},
		{
			name:   "ISO date with nanoseconds",
			input:  "2024-01-15T10:30:00.123456789Z",
			wantMs: refMs + 123, // 123 ms
		},
		{
			name:   "date only",
			input:  "2024-01-15",
			wantMs: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		{
			name:   "datetime with space separator",
			input:  "2024-01-15 10:30:00",
			wantMs: refMs,
		},

		// Invalid string inputs
		{
			name:    "invalid date string",
			input:   "not-a-date",
			wantNil: true,
		},
		{
			name:    "partial date string",
			input:   "2024-01",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTimestamp(tt.input)

			if tt.wantNil {
				if got != nil {
					t.Errorf("NormalizeTimestamp(%v) = %+v, want nil", tt.input, got)
				}
				return
			}

			if got == nil {
				t.Errorf("NormalizeTimestamp(%v) = nil, want %d ms", tt.input, tt.wantMs)
				return
			}

			if got.TimestampMs != tt.wantMs {
				t.Errorf("NormalizeTimestamp(%v).TimestampMs = %d, want %d",
					tt.input, got.TimestampMs, tt.wantMs)
			}

			// Verify TimestampUTC is valid
			if got.TimestampUTC == "" {
				t.Errorf("NormalizeTimestamp(%v).TimestampUTC is empty", tt.input)
			}

			// Verify round-trip
			parsed, err := time.Parse(time.RFC3339Nano, got.TimestampUTC)
			if err != nil {
				t.Errorf("Failed to parse TimestampUTC %q: %v", got.TimestampUTC, err)
			}
			if parsed.UnixMilli() != got.TimestampMs {
				t.Errorf("TimestampUTC round-trip mismatch: got %d, want %d",
					parsed.UnixMilli(), got.TimestampMs)
			}
		})
	}
}

func TestNormalizeTimestampStringPointer(t *testing.T) {
	s := "1705314600"
	got := NormalizeTimestamp(&s)
	if got == nil {
		t.Fatal("NormalizeTimestamp(*string) returned nil")
	}
	if got.TimestampMs != 1705314600000 {
		t.Errorf("NormalizeTimestamp(*string) = %d, want 1705314600000", got.TimestampMs)
	}

	// Test nil string pointer
	var nilPtr *string
	gotNil := NormalizeTimestamp(nilPtr)
	if gotNil != nil {
		t.Errorf("NormalizeTimestamp(nil *string) = %+v, want nil", gotNil)
	}
}

func TestWithNormalizedTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		value     map[string]any
		raw       any
		wantMs    int64
		wantUTC   string
		wantNil   bool
		preserved bool // whether existing values should be preserved
	}{
		{
			name:    "adds timestamp to empty map",
			value:   map[string]any{},
			raw:     int64(1705314600),
			wantMs:  1705314600000,
			wantNil: false,
		},
		{
			name:    "adds timestamp to existing map",
			value:   map[string]any{"foo": "bar"},
			raw:     int64(1705314600),
			wantMs:  1705314600000,
			wantNil: false,
		},
		{
			name:      "preserves existing timestampMs",
			value:     map[string]any{"timestampMs": int64(9999999999999)},
			raw:       int64(1705314600),
			wantMs:    9999999999999,
			preserved: true,
		},
		{
			name:      "preserves existing timestampUtc",
			value:     map[string]any{"timestampUtc": "existing-value"},
			raw:       int64(1705314600),
			wantUTC:   "existing-value",
			preserved: true,
		},
		{
			name:    "nil raw timestamp returns original",
			value:   map[string]any{"foo": "bar"},
			raw:     nil,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WithNormalizedTimestamp(tt.value, tt.raw)

			if tt.wantNil {
				// Should return original map unchanged
				if len(got) != len(tt.value) {
					t.Errorf("Expected original map, got different length")
				}
				return
			}

			// Check timestampMs
			if tt.preserved && tt.value["timestampMs"] != nil {
				if got["timestampMs"] != tt.wantMs {
					t.Errorf("timestampMs = %v, want %d (preserved)", got["timestampMs"], tt.wantMs)
				}
			} else if !tt.preserved && tt.wantMs != 0 {
				if ms, ok := got["timestampMs"].(int64); !ok || ms != tt.wantMs {
					t.Errorf("timestampMs = %v, want %d", got["timestampMs"], tt.wantMs)
				}
			}

			// Check timestampUtc
			if tt.preserved && tt.value["timestampUtc"] != nil {
				if got["timestampUtc"] != tt.wantUTC {
					t.Errorf("timestampUtc = %v, want %q (preserved)", got["timestampUtc"], tt.wantUTC)
				}
			} else if !tt.preserved {
				if _, ok := got["timestampUtc"].(string); !ok {
					t.Errorf("timestampUtc not set or not a string: %v", got["timestampUtc"])
				}
			}

			// Verify original map not modified
			if tt.value["foo"] == "bar" {
				if got["foo"] != "bar" {
					t.Error("Original value not preserved in result")
				}
			}
		})
	}
}

func TestNormalizeTimestampThreshold(t *testing.T) {
	// Test the threshold between seconds and milliseconds
	// Values less than 1e12 are treated as seconds

	tests := []struct {
		input  int64
		wantMs int64
	}{
		// Just under threshold - treated as seconds
		{input: 999999999999, wantMs: 999999999999000},
		// At threshold - treated as milliseconds
		{input: 1000000000000, wantMs: 1000000000000},
		// Well above threshold - treated as milliseconds
		{input: 1705314600000, wantMs: 1705314600000},
	}

	for _, tt := range tests {
		got := NormalizeTimestamp(tt.input)
		if got == nil {
			t.Errorf("NormalizeTimestamp(%d) = nil", tt.input)
			continue
		}
		if got.TimestampMs != tt.wantMs {
			t.Errorf("NormalizeTimestamp(%d).TimestampMs = %d, want %d",
				tt.input, got.TimestampMs, tt.wantMs)
		}
	}
}
