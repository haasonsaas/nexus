// Package exec provides executable safety validation utilities.
package exec

import (
	"errors"
	"regexp"
	"strings"
)

// Pattern definitions for executable safety validation.
var (
	// ShellMetachars matches shell metacharacters that could enable command injection.
	ShellMetachars = regexp.MustCompile(`[;&|` + "`" + `$<>]`)

	// ControlChars matches control characters like newlines and carriage returns.
	ControlChars = regexp.MustCompile(`[\r\n]`)

	// QuoteChars matches quote characters that could enable argument injection.
	QuoteChars = regexp.MustCompile(`["']`)

	// BareNamePattern matches safe bare executable names without paths.
	BareNamePattern = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

	// WindowsDriveLetter matches Windows drive letter paths (e.g., C:\).
	WindowsDriveLetter = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

// Common errors for executable safety validation.
var (
	ErrEmptyValue           = errors.New("executable value is empty")
	ErrNullByte             = errors.New("executable value contains null byte")
	ErrControlChar          = errors.New("executable value contains control characters")
	ErrShellMetachar        = errors.New("executable value contains shell metacharacters")
	ErrQuoteChar            = errors.New("executable value contains quote characters")
	ErrOptionInjection      = errors.New("executable value starts with dash (option injection)")
	ErrInvalidBareNameChars = errors.New("executable value contains invalid characters for bare name")
)

// IsLikelyPath checks if the value appears to be a file path rather than a bare name.
// It returns true for values starting with . ~ / \ or matching Windows drive letters.
func IsLikelyPath(value string) bool {
	if value == "" {
		return false
	}

	// Check for common path prefixes
	if strings.HasPrefix(value, ".") || strings.HasPrefix(value, "~") {
		return true
	}

	// Check for path separators
	if strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return true
	}

	// Check for Windows drive letter (e.g., C:\)
	return WindowsDriveLetter.MatchString(value)
}

// IsSafeExecutableValue validates that an executable name or path is safe to use.
// It checks for:
// 1. Empty or nil values (rejected)
// 2. Null bytes (rejected)
// 3. Control characters like newlines (rejected)
// 4. Shell metacharacters ;&|`$<> (rejected)
// 5. Quote characters "' (rejected)
// 6. Paths starting with . ~ / \ or drive letters (allowed)
// 7. Values starting with - (rejected for option injection)
// 8. Bare names matching [A-Za-z0-9._+-]+ (allowed)
func IsSafeExecutableValue(value string) bool {
	if value == "" {
		return false
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}

	// Check for null bytes
	if strings.Contains(trimmed, "\x00") {
		return false
	}

	// Check for control characters (newlines, carriage returns)
	if ControlChars.MatchString(trimmed) {
		return false
	}

	// Check for shell metacharacters
	if ShellMetachars.MatchString(trimmed) {
		return false
	}

	// Check for quote characters
	if QuoteChars.MatchString(trimmed) {
		return false
	}

	// If it looks like a path, allow it (paths have already passed the above checks)
	if IsLikelyPath(trimmed) {
		return true
	}

	// For bare names, reject option injection
	if strings.HasPrefix(trimmed, "-") {
		return false
	}

	// Validate bare name pattern
	return BareNamePattern.MatchString(trimmed)
}

// SanitizeExecutableValue validates the executable value and returns it trimmed if safe.
// Returns an error describing why the value is unsafe if validation fails.
func SanitizeExecutableValue(value string) (string, error) {
	if value == "" {
		return "", ErrEmptyValue
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ErrEmptyValue
	}

	// Check for null bytes
	if strings.Contains(trimmed, "\x00") {
		return "", ErrNullByte
	}

	// Check for control characters (newlines, carriage returns)
	if ControlChars.MatchString(trimmed) {
		return "", ErrControlChar
	}

	// Check for shell metacharacters
	if ShellMetachars.MatchString(trimmed) {
		return "", ErrShellMetachar
	}

	// Check for quote characters
	if QuoteChars.MatchString(trimmed) {
		return "", ErrQuoteChar
	}

	// If it looks like a path, allow it (paths have already passed the above checks)
	if IsLikelyPath(trimmed) {
		return trimmed, nil
	}

	// For bare names, reject option injection
	if strings.HasPrefix(trimmed, "-") {
		return "", ErrOptionInjection
	}

	// Validate bare name pattern
	if !BareNamePattern.MatchString(trimmed) {
		return "", ErrInvalidBareNameChars
	}

	return trimmed, nil
}
