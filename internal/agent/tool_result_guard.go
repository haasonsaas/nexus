package agent

import (
	"regexp"
	"strings"

	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

// DefaultMaxToolResultSize is the default maximum size for tool results (64KB).
// This prevents memory exhaustion and excessive storage costs.
const DefaultMaxToolResultSize = 64 * 1024

// builtinSecretPatterns contains pre-compiled patterns for detecting common secrets.
// These are always applied when SanitizeSecrets is enabled.
var builtinSecretPatterns = []*regexp.Regexp{
	// API keys: api_key=<key>, apiKey: <key>, etc.
	regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?[\w-]{20,}['"]?`),
	// Bearer tokens: Bearer eyJhbGc...
	regexp.MustCompile(`(?i)bearer\s+[\w-\.]+`),
	// AWS keys and secrets
	regexp.MustCompile(`(?i)(aws|amazon).*?(key|secret|token)\s*[:=]\s*['"]?[\w/+=]{20,}['"]?`),
	// Generic secrets: password=<value>, secret=<value>, token=<value>
	regexp.MustCompile(`(?i)(password|passwd|secret|token)\s*[:=]\s*['"]?[^\s'"]{8,}['"]?`),
	// Private keys (PEM format)
	regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`),
}

// ToolResultGuard controls how tool results are redacted before persistence.
type ToolResultGuard struct {
	Enabled         bool
	MaxChars        int
	Denylist        []string
	RedactPatterns  []string
	RedactionText   string
	TruncateSuffix  string
	SanitizeSecrets bool // When true, applies builtin secret detection patterns
}

func (g ToolResultGuard) active() bool {
	return g.Enabled || g.MaxChars > 0 || len(g.Denylist) > 0 || len(g.RedactPatterns) > 0 || g.RedactionText != "" || g.TruncateSuffix != "" || g.SanitizeSecrets
}

func (g ToolResultGuard) Apply(toolName string, result models.ToolResult, resolver *policy.Resolver) models.ToolResult {
	if !g.active() {
		return result
	}

	redaction := strings.TrimSpace(g.RedactionText)
	if redaction == "" {
		redaction = "[REDACTED]"
	}
	truncateSuffix := strings.TrimSpace(g.TruncateSuffix)
	if truncateSuffix == "" {
		truncateSuffix = "...[truncated]"
	}

	// Check tool denylist first - completely redact if matched
	if len(g.Denylist) > 0 && matchesToolPatterns(g.Denylist, toolName, resolver) {
		result.Content = redaction
		return result
	}

	content := result.Content

	// Apply builtin secret patterns when SanitizeSecrets is enabled
	if g.SanitizeSecrets && content != "" {
		for _, re := range builtinSecretPatterns {
			content = re.ReplaceAllString(content, redaction)
		}
	}

	// Apply custom redact patterns
	if len(g.RedactPatterns) > 0 && content != "" {
		for _, pattern := range g.RedactPatterns {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				continue
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			content = re.ReplaceAllString(content, redaction)
		}
	}

	result.Content = content

	// Truncate if over size limit
	if g.MaxChars > 0 && len(result.Content) > g.MaxChars {
		cutoff := g.MaxChars
		if cutoff < 0 {
			cutoff = 0
		}
		if cutoff > len(result.Content) {
			cutoff = len(result.Content)
		}
		result.Content = result.Content[:cutoff] + truncateSuffix
	}

	return result
}

// DetectSecrets scans content for potential secrets and returns
// a list of matched pattern descriptions. This is useful for logging
// or alerting on potential secret exposure.
func DetectSecrets(content string) []string {
	if content == "" {
		return nil
	}

	patternNames := []string{
		"api_key",
		"bearer_token",
		"aws_key",
		"generic_secret",
		"private_key",
	}

	var matches []string
	for i, re := range builtinSecretPatterns {
		if re.MatchString(content) {
			matches = append(matches, patternNames[i])
		}
	}
	return matches
}

// SanitizeToolResult applies default security sanitization to a tool result:
// 1. Truncates if over DefaultMaxToolResultSize (64KB)
// 2. Redacts detected secrets with [REDACTED]
//
// This is a convenience function for applying security defaults.
func SanitizeToolResult(result string) string {
	// Truncate if over size limit
	if len(result) > DefaultMaxToolResultSize {
		result = result[:DefaultMaxToolResultSize] + "\n...[truncated]"
	}

	// Redact secrets
	for _, re := range builtinSecretPatterns {
		result = re.ReplaceAllString(result, "[REDACTED]")
	}

	return result
}
