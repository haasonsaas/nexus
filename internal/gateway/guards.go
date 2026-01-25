// Package gateway provides the main Nexus gateway server.
//
// guards.go contains security guards for sanitizing tool results,
// detecting secrets, and enforcing size limits.
package gateway

import (
	"regexp"
	"strings"
)

// MaxToolResultSize is the maximum allowed size for tool results (64KB).
// Results exceeding this limit are truncated to prevent memory exhaustion
// and excessive storage costs.
const MaxToolResultSize = 64 * 1024

// SecretPattern represents a compiled regex pattern for detecting secrets.
type SecretPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// secretPatterns contains pre-compiled patterns for common secret types.
// These patterns are designed to catch accidental secret leakage in tool results.
var secretPatterns = []SecretPattern{
	{
		Name:    "api_key",
		Pattern: regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?[\w-]{20,}['"]?`),
	},
	{
		Name:    "bearer_token",
		Pattern: regexp.MustCompile(`(?i)bearer\s+[\w-\.]+`),
	},
	{
		Name:    "aws_key",
		Pattern: regexp.MustCompile(`(?i)(aws|amazon).*?(key|secret|token)\s*[:=]\s*['"]?[\w/+=]{20,}['"]?`),
	},
	{
		Name:    "generic_secret",
		Pattern: regexp.MustCompile(`(?i)(password|passwd|secret|token)\s*[:=]\s*['"]?[^\s'"]{8,}['"]?`),
	},
	{
		Name:    "private_key",
		Pattern: regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`),
	},
}

// SanitizeToolResult sanitizes a tool result string by:
// 1. Truncating if over MaxToolResultSize
// 2. Redacting detected secrets with [REDACTED]
//
// Returns the sanitized result string.
func SanitizeToolResult(result string) string {
	// Truncate if over size limit
	if len(result) > MaxToolResultSize {
		result = result[:MaxToolResultSize] + "\n...[truncated]"
	}

	// Redact secrets
	for _, sp := range secretPatterns {
		result = sp.Pattern.ReplaceAllString(result, "[REDACTED]")
	}

	return result
}

// DetectSecrets scans content for potential secrets and returns
// a list of matched pattern names. This is useful for logging
// or alerting on potential secret exposure.
//
// Returns an empty slice if no secrets are detected.
func DetectSecrets(content string) []string {
	if content == "" {
		return nil
	}

	var matches []string
	seen := make(map[string]bool)

	for _, sp := range secretPatterns {
		if sp.Pattern.MatchString(content) {
			if !seen[sp.Name] {
				matches = append(matches, sp.Name)
				seen[sp.Name] = true
			}
		}
	}

	return matches
}

// RedactSecrets replaces all detected secrets in content with the
// provided replacement string. If replacement is empty, "[REDACTED]"
// is used as the default.
func RedactSecrets(content, replacement string) string {
	if content == "" {
		return content
	}

	if replacement == "" {
		replacement = "[REDACTED]"
	}

	for _, sp := range secretPatterns {
		content = sp.Pattern.ReplaceAllString(content, replacement)
	}

	return content
}

// ContainsPrivateKey checks if content contains a private key header.
// This is a fast check for the most sensitive type of secret.
func ContainsPrivateKey(content string) bool {
	return strings.Contains(content, "-----BEGIN") &&
		strings.Contains(content, "PRIVATE KEY-----")
}

// TruncateWithSuffix truncates content to maxLen and appends suffix.
// If content is already within limit, it's returned unchanged.
func TruncateWithSuffix(content string, maxLen int, suffix string) string {
	if len(content) <= maxLen {
		return content
	}

	// Ensure we have room for the suffix
	cutoff := maxLen - len(suffix)
	if cutoff < 0 {
		cutoff = 0
	}

	return content[:cutoff] + suffix
}
