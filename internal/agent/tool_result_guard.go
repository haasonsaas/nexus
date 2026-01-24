package agent

import (
	"regexp"
	"strings"

	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ToolResultGuard controls how tool results are redacted before persistence.
type ToolResultGuard struct {
	Enabled        bool
	MaxChars       int
	Denylist       []string
	RedactPatterns []string
	RedactionText  string
	TruncateSuffix string
}

func (g ToolResultGuard) active() bool {
	return g.Enabled || g.MaxChars > 0 || len(g.Denylist) > 0 || len(g.RedactPatterns) > 0 || g.RedactionText != "" || g.TruncateSuffix != ""
}

func (g ToolResultGuard) Apply(toolName string, result models.ToolResult, resolver *policy.Resolver) models.ToolResult {
	if !g.active() {
		return result
	}

	redaction := strings.TrimSpace(g.RedactionText)
	if redaction == "" {
		redaction = "[redacted]"
	}
	truncateSuffix := strings.TrimSpace(g.TruncateSuffix)
	if truncateSuffix == "" {
		truncateSuffix = "...[truncated]"
	}

	if len(g.Denylist) > 0 && matchesToolPatterns(g.Denylist, toolName, resolver) {
		result.Content = redaction
		return result
	}

	if len(g.RedactPatterns) > 0 && result.Content != "" {
		content := result.Content
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
		result.Content = content
	}

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
