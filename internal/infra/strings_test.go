package infra

import (
	"testing"
)

func TestTruncateUTF16Safe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "no truncation needed",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "simple truncation",
			input:    "hello world",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "with emoji (surrogate pair)",
			input:    "hello 😀 world",
			maxLen:   7,
			expected: "hello ",
		},
		{
			name:     "emoji at boundary",
			input:    "hi😀",
			maxLen:   3,
			expected: "hi",
		},
		{
			name:     "multiple emoji",
			input:    "😀😁😂",
			maxLen:   4,
			expected: "😀😁",
		},
		{
			name:     "zero length",
			input:    "hello",
			maxLen:   0,
			expected: "",
		},
		{
			name:     "negative length",
			input:    "hello",
			maxLen:   -1,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateUTF16Safe(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("TruncateUTF16Safe(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestUTF16Length(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"emoji", "😀", 2},           // Surrogate pair = 2 units
		{"mixed", "hi😀", 4},         // 2 ascii + 2 for emoji
		{"multiple emoji", "😀😁", 4}, // 2 * 2 = 4
		{"japanese", "日本語", 3},      // BMP characters = 1 each
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UTF16Length(tt.input)
			if got != tt.expected {
				t.Errorf("UTF16Length(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		expected string
	}{
		{"empty", "", 5, ""},
		{"no truncation", "hello", 10, "hello"},
		{"truncate ascii", "hello world", 5, "hello"},
		{"truncate with emoji", "hi😀bye", 4, "hi😀b"},
		{"zero", "hello", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateRunes(tt.input, tt.maxRunes)
			if got != tt.expected {
				t.Errorf("TruncateRunes(%q, %d) = %q, want %q", tt.input, tt.maxRunes, got, tt.expected)
			}
		})
	}
}

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		expected string
	}{
		{"empty", "", 5, ""},
		{"no truncation", "hello", 10, "hello"},
		{"truncate ascii", "hello world", 5, "hello"},
		{"truncate mid-utf8", "日本語", 4, "日"}, // 日 = 3 bytes, so 4 bytes only fits 1
		{"zero", "hello", 0, ""},
		{"negative", "hello", -1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateBytes(tt.input, tt.maxBytes)
			if got != tt.expected {
				t.Errorf("TruncateBytes(%q, %d) = %q, want %q", tt.input, tt.maxBytes, got, tt.expected)
			}
		})
	}
}

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		expected string
	}{
		{"empty", "", 5, ""},
		{"no truncation", "hello", 10, "hello"},
		{"with ellipsis", "hello world", 6, "hello…"},
		{"short max", "hello", 3, "hel"},
		{"exact length", "hello", 5, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateWithEllipsis(tt.input, tt.maxRunes)
			if got != tt.expected {
				t.Errorf("TruncateWithEllipsis(%q, %d) = %q, want %q", tt.input, tt.maxRunes, got, tt.expected)
			}
		})
	}
}

func TestRuneCount(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"emoji", "😀", 1},
		{"mixed", "hi😀", 3},
		{"japanese", "日本語", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RuneCount(tt.input)
			if got != tt.expected {
				t.Errorf("RuneCount(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsHighSurrogate(t *testing.T) {
	tests := []struct {
		codeUnit uint16
		expected bool
	}{
		{0xD800, true},
		{0xDBFF, true},
		{0xD900, true},
		{0xDC00, false},
		{0x0041, false},
	}

	for _, tt := range tests {
		got := IsHighSurrogate(tt.codeUnit)
		if got != tt.expected {
			t.Errorf("IsHighSurrogate(0x%X) = %v, want %v", tt.codeUnit, got, tt.expected)
		}
	}
}

func TestIsLowSurrogate(t *testing.T) {
	tests := []struct {
		codeUnit uint16
		expected bool
	}{
		{0xDC00, true},
		{0xDFFF, true},
		{0xDD00, true},
		{0xD800, false},
		{0x0041, false},
	}

	for _, tt := range tests {
		got := IsLowSurrogate(tt.codeUnit)
		if got != tt.expected {
			t.Errorf("IsLowSurrogate(0x%X) = %v, want %v", tt.codeUnit, got, tt.expected)
		}
	}
}

func TestEncodeDecodeUTF16(t *testing.T) {
	tests := []string{
		"",
		"hello",
		"日本語",
		"hello 😀 world",
		"😀😁😂",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			encoded := EncodeUTF16(input)
			decoded := DecodeUTF16(encoded)
			if decoded != input {
				t.Errorf("round-trip failed: %q -> %v -> %q", input, encoded, decoded)
			}
		})
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		value, min, max, expected int
	}{
		{5, 0, 10, 5},
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 10, 0},
		{10, 0, 10, 10},
	}

	for _, tt := range tests {
		got := ClampInt(tt.value, tt.min, tt.max)
		if got != tt.expected {
			t.Errorf("ClampInt(%d, %d, %d) = %d, want %d", tt.value, tt.min, tt.max, got, tt.expected)
		}
	}
}

func TestClampInt64(t *testing.T) {
	tests := []struct {
		value, min, max, expected int64
	}{
		{5, 0, 10, 5},
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
	}

	for _, tt := range tests {
		got := ClampInt64(tt.value, tt.min, tt.max)
		if got != tt.expected {
			t.Errorf("ClampInt64(%d, %d, %d) = %d, want %d", tt.value, tt.min, tt.max, got, tt.expected)
		}
	}
}

func TestClampFloat64(t *testing.T) {
	tests := []struct {
		value, min, max, expected float64
	}{
		{5.5, 0.0, 10.0, 5.5},
		{-5.5, 0.0, 10.0, 0.0},
		{15.5, 0.0, 10.0, 10.0},
	}

	for _, tt := range tests {
		got := ClampFloat64(tt.value, tt.min, tt.max)
		if got != tt.expected {
			t.Errorf("ClampFloat64(%f, %f, %f) = %f, want %f", tt.value, tt.min, tt.max, got, tt.expected)
		}
	}
}

func TestMinMax(t *testing.T) {
	if Min(3, 5) != 3 {
		t.Error("Min(3, 5) should be 3")
	}
	if Min(5, 3) != 3 {
		t.Error("Min(5, 3) should be 3")
	}
	if Max(3, 5) != 5 {
		t.Error("Max(3, 5) should be 5")
	}
	if Max(5, 3) != 5 {
		t.Error("Max(5, 3) should be 5")
	}
}

func TestMinMaxInt64(t *testing.T) {
	if MinInt64(3, 5) != 3 {
		t.Error("MinInt64(3, 5) should be 3")
	}
	if MaxInt64(3, 5) != 5 {
		t.Error("MaxInt64(3, 5) should be 5")
	}
}

func TestSliceUTF16Safe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		start    int
		end      int
		expected string
	}{
		{"empty", "", 0, 0, ""},
		{"full slice", "hello", 0, 5, "hello"},
		{"partial", "hello", 1, 4, "ell"},
		{"with emoji", "a😀b", 0, 3, "a😀b"}, // 3 runes, all included
		{"emoji only", "😀", 0, 1, "😀"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SliceUTF16Safe(tt.input, tt.start, tt.end)
			if got != tt.expected {
				t.Errorf("SliceUTF16Safe(%q, %d, %d) = %q, want %q", tt.input, tt.start, tt.end, got, tt.expected)
			}
		})
	}
}
