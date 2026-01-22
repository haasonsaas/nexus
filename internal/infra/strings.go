package infra

import (
	"unicode/utf16"
	"unicode/utf8"
)

// SliceUTF16Safe slices a string safely without breaking UTF-16 surrogate pairs.
// This is important when dealing with emoji and other characters that use
// surrogate pairs in UTF-16 encoding (which is how string lengths are often
// counted in JavaScript and other platforms).
func SliceUTF16Safe(input string, start, end int) string {
	if input == "" {
		return ""
	}

	// Convert to runes for safe indexing
	runes := []rune(input)

	// Convert rune indices to UTF-16 indices
	utf16Start := runeIndexToUTF16(runes, start)
	utf16End := runeIndexToUTF16(runes, end)

	// Clamp indices
	length := len(runes)
	if utf16Start < 0 {
		utf16Start = 0
	}
	if utf16End > length {
		utf16End = length
	}
	if utf16Start > utf16End {
		utf16Start, utf16End = utf16End, utf16Start
	}

	return string(runes[utf16Start:utf16End])
}

// TruncateUTF16Safe truncates a string to a maximum length without breaking
// UTF-16 surrogate pairs. The length is measured in UTF-16 code units.
func TruncateUTF16Safe(input string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}

	runes := []rune(input)
	utf16Len := 0

	for i, r := range runes {
		// Check if this rune would be a surrogate pair in UTF-16
		codeUnits := 1
		if r > 0xFFFF {
			codeUnits = 2
		}

		if utf16Len+codeUnits > maxLen {
			return string(runes[:i])
		}
		utf16Len += codeUnits
	}

	return input
}

// TruncateRunes truncates a string to a maximum number of runes (Unicode code points).
// This is generally safer than truncating by byte count.
func TruncateRunes(input string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}

	runes := []rune(input)
	if len(runes) <= maxRunes {
		return input
	}

	return string(runes[:maxRunes])
}

// TruncateBytes truncates a string to a maximum number of bytes without
// breaking UTF-8 encoding. If truncation would occur mid-character,
// the entire character is omitted.
func TruncateBytes(input string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}

	if len(input) <= maxBytes {
		return input
	}

	// Find the last valid UTF-8 boundary before maxBytes
	end := maxBytes
	for end > 0 && !utf8.RuneStart(input[end]) {
		end--
	}

	return input[:end]
}

// TruncateWithEllipsis truncates a string and appends an ellipsis if truncated.
func TruncateWithEllipsis(input string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}

	runes := []rune(input)
	if len(runes) <= maxRunes {
		return input
	}

	// Reserve space for ellipsis
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}

	return string(runes[:maxRunes-1]) + "…"
}

// UTF16Length returns the length of a string in UTF-16 code units.
// This matches how string.length works in JavaScript.
func UTF16Length(input string) int {
	count := 0
	for _, r := range input {
		if r > 0xFFFF {
			count += 2 // Surrogate pair
		} else {
			count++
		}
	}
	return count
}

// RuneCount returns the number of runes (Unicode code points) in a string.
// This is equivalent to len([]rune(input)) but more efficient.
func RuneCount(input string) int {
	return utf8.RuneCountInString(input)
}

// runeIndexToUTF16 converts a rune index to a UTF-16 code unit index.
func runeIndexToUTF16(runes []rune, index int) int {
	if index <= 0 {
		return 0
	}
	if index >= len(runes) {
		return len(runes)
	}

	utf16Index := 0
	for i := 0; i < index && i < len(runes); i++ {
		if runes[i] > 0xFFFF {
			utf16Index += 2
		} else {
			utf16Index++
		}
	}
	return utf16Index
}

// IsHighSurrogate returns true if the code unit is a high surrogate.
func IsHighSurrogate(codeUnit uint16) bool {
	return codeUnit >= 0xD800 && codeUnit <= 0xDBFF
}

// IsLowSurrogate returns true if the code unit is a low surrogate.
func IsLowSurrogate(codeUnit uint16) bool {
	return codeUnit >= 0xDC00 && codeUnit <= 0xDFFF
}

// EncodeUTF16 encodes a string to UTF-16 code units.
func EncodeUTF16(input string) []uint16 {
	return utf16.Encode([]rune(input))
}

// DecodeUTF16 decodes UTF-16 code units to a string.
func DecodeUTF16(input []uint16) string {
	return string(utf16.Decode(input))
}

// ClampInt clamps an integer to a range.
func ClampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampInt64 clamps a 64-bit integer to a range.
func ClampInt64(value, min, max int64) int64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampFloat64 clamps a float64 to a range.
func ClampFloat64(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// Min returns the minimum of two integers.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Max returns the maximum of two integers.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MinInt64 returns the minimum of two int64 values.
func MinInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// MaxInt64 returns the maximum of two int64 values.
func MaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
