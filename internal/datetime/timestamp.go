package datetime

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TimestampResult holds the normalized timestamp in both milliseconds and UTC ISO string.
type TimestampResult struct {
	TimestampMs  int64  `json:"timestampMs"`
	TimestampUTC string `json:"timestampUtc"`
}

// numericPattern matches numeric strings with optional decimal.
var numericPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

// NormalizeTimestamp converts various timestamp formats to a normalized result.
// Supported inputs:
//   - int64: treated as seconds if < 1e12, otherwise milliseconds
//   - float64: treated as seconds (fractional), converted to milliseconds
//   - string: numeric string (seconds or ms based on length/decimal),
//     or ISO 8601 date string
//
// Returns nil if the input cannot be parsed or is nil/empty.
func NormalizeTimestamp(raw any) *TimestampResult {
	if raw == nil {
		return nil
	}

	var timestampMs int64
	var ok bool

	switch v := raw.(type) {
	case time.Time:
		timestampMs = v.UnixMilli()
		ok = true

	case *time.Time:
		if v != nil {
			timestampMs = v.UnixMilli()
			ok = true
		}

	case int64:
		timestampMs = normalizeNumericToMs(float64(v), false)
		ok = true

	case int:
		timestampMs = normalizeNumericToMs(float64(v), false)
		ok = true

	case int32:
		timestampMs = normalizeNumericToMs(float64(v), false)
		ok = true

	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		timestampMs = normalizeNumericToMs(v, true)
		ok = true

	case float32:
		f64 := float64(v)
		if math.IsNaN(f64) || math.IsInf(f64, 0) {
			return nil
		}
		timestampMs = normalizeNumericToMs(f64, true)
		ok = true

	case string:
		result := parseStringTimestamp(v)
		if result != nil {
			return result
		}

	case *string:
		if v != nil {
			result := parseStringTimestamp(*v)
			if result != nil {
				return result
			}
		}
	}

	if !ok {
		return nil
	}

	return &TimestampResult{
		TimestampMs:  timestampMs,
		TimestampUTC: time.UnixMilli(timestampMs).UTC().Format(time.RFC3339Nano),
	}
}

// normalizeNumericToMs converts a numeric value to milliseconds.
// If the value is less than 1e12, it's treated as seconds.
// If isFloat is true and the value has fractional part, it's treated as seconds.
func normalizeNumericToMs(v float64, isFloat bool) int64 {
	const msThreshold = 1_000_000_000_000 // 1e12

	if v < msThreshold {
		// Treat as seconds, convert to milliseconds
		return int64(math.Round(v * 1000))
	}
	// Already in milliseconds
	return int64(math.Round(v))
}

// parseStringTimestamp parses a string timestamp.
func parseStringTimestamp(s string) *TimestampResult {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}

	// Check if it's a numeric string
	if numericPattern.MatchString(trimmed) {
		return parseNumericString(trimmed)
	}

	// Try parsing as ISO 8601 / RFC 3339 date
	return parseISODate(trimmed)
}

// parseNumericString parses a numeric string timestamp.
func parseNumericString(s string) *TimestampResult {
	if strings.Contains(s, ".") {
		// Has decimal - treat as seconds with fractional part
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		ms := int64(math.Round(f * 1000))
		return &TimestampResult{
			TimestampMs:  ms,
			TimestampUTC: time.UnixMilli(ms).UTC().Format(time.RFC3339Nano),
		}
	}

	// Integer string - check length to determine if seconds or ms
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}

	var ms int64
	if len(s) >= 13 {
		// 13+ digits - treat as milliseconds
		ms = num
	} else {
		// Less than 13 digits - treat as seconds
		ms = num * 1000
	}

	return &TimestampResult{
		TimestampMs:  ms,
		TimestampUTC: time.UnixMilli(ms).UTC().Format(time.RFC3339Nano),
	}
}

// parseISODate tries to parse a string as an ISO 8601 date.
func parseISODate(s string) *TimestampResult {
	// Try common formats
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z0700",
		"2006-01-02T15:04:05Z0700",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		t, err := time.Parse(format, s)
		if err == nil {
			return &TimestampResult{
				TimestampMs:  t.UnixMilli(),
				TimestampUTC: t.UTC().Format(time.RFC3339Nano),
			}
		}
	}

	return nil
}

// WithNormalizedTimestamp augments a map with normalized timestamp fields.
// If the map already has valid timestampMs or timestampUtc values, they are preserved.
// Returns a new map with the added fields.
func WithNormalizedTimestamp(value map[string]any, rawTimestamp any) map[string]any {
	normalized := NormalizeTimestamp(rawTimestamp)
	if normalized == nil {
		return value
	}

	// Create a copy of the map
	result := make(map[string]any, len(value)+2)
	for k, v := range value {
		result[k] = v
	}

	// Only set timestampMs if not already present and valid
	if existing, ok := result["timestampMs"]; !ok || !isValidTimestampMs(existing) {
		result["timestampMs"] = normalized.TimestampMs
	}

	// Only set timestampUtc if not already present and valid
	if existing, ok := result["timestampUtc"]; !ok || !isValidTimestampUTC(existing) {
		result["timestampUtc"] = normalized.TimestampUTC
	}

	return result
}

// isValidTimestampMs checks if a value is a valid timestamp in milliseconds.
func isValidTimestampMs(v any) bool {
	switch n := v.(type) {
	case int64:
		return true
	case int:
		return true
	case float64:
		return !math.IsNaN(n) && !math.IsInf(n, 0)
	default:
		return false
	}
}

// isValidTimestampUTC checks if a value is a valid UTC timestamp string.
func isValidTimestampUTC(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(s) != ""
}
