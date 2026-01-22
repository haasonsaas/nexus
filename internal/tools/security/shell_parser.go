// Package security provides security utilities for analyzing and validating tool inputs.
package security

import (
	"strings"
	"unicode"
)

// DangerousToken represents a potentially dangerous shell token found in a command.
type DangerousToken struct {
	// Token is the actual dangerous character or sequence found.
	Token string `json:"token"`

	// Position is the index where the token was found in the command.
	Position int `json:"position"`

	// Risk categorizes the type of danger this token represents.
	Risk string `json:"risk"`
}

// ShellAnalysis contains the results of analyzing a shell command for security risks.
type ShellAnalysis struct {
	// Command is the original command that was analyzed.
	Command string `json:"command"`

	// IsSafe indicates whether the command is considered safe to execute.
	IsSafe bool `json:"is_safe"`

	// DangerousTokens contains all dangerous tokens found in the command.
	DangerousTokens []DangerousToken `json:"dangerous_tokens,omitempty"`

	// Reason provides a human-readable explanation of why the command is unsafe.
	Reason string `json:"reason,omitempty"`
}

// dangerousPatterns maps shell metacharacters to their risk categories.
var dangerousPatterns = map[string]string{
	";":  "command_chain",
	"&&": "command_chain",
	"||": "command_chain",
	"|":  "pipe",
	">":  "redirect",
	">>": "redirect",
	"<":  "redirect",
	"`":  "subshell",
	"$(": "subshell",
	"&":  "background",
}

// riskDescriptions provides human-readable descriptions for each risk type.
var riskDescriptions = map[string]string{
	"command_chain": "command chaining allows execution of multiple commands",
	"pipe":          "pipes allow output to be redirected to another command",
	"redirect":      "redirects can overwrite files or read sensitive data",
	"subshell":      "subshells allow arbitrary command execution",
	"background":    "background execution can spawn persistent processes",
}

// AnalyzeCommand analyzes a shell command string for dangerous patterns.
// This is a simple analysis that does NOT respect quotes - use AnalyzeCommandQuoteAware
// for proper quote-aware parsing.
func AnalyzeCommand(cmd string) *ShellAnalysis {
	analysis := &ShellAnalysis{
		Command: cmd,
		IsSafe:  true,
	}

	if cmd == "" {
		return analysis
	}

	// Check for each dangerous pattern
	// Check longer patterns first to avoid false positives (e.g., >> before >)
	patterns := []string{">>", "&&", "||", "$(", ";", "|", ">", "<", "`", "&"}

	for _, pattern := range patterns {
		idx := 0
		for {
			pos := strings.Index(cmd[idx:], pattern)
			if pos == -1 {
				break
			}
			actualPos := idx + pos

			// Skip if this is part of a longer pattern we already handled
			if pattern == ">" && actualPos > 0 && cmd[actualPos-1:actualPos] == ">" {
				idx = actualPos + len(pattern)
				continue
			}
			if pattern == "&" && actualPos > 0 && cmd[actualPos-1:actualPos] == "&" {
				idx = actualPos + len(pattern)
				continue
			}
			if pattern == "|" && actualPos > 0 && cmd[actualPos-1:actualPos] == "|" {
				idx = actualPos + len(pattern)
				continue
			}
			if pattern == "&" && actualPos+1 < len(cmd) && cmd[actualPos+1:actualPos+2] == "&" {
				idx = actualPos + 1
				continue
			}
			if pattern == "|" && actualPos+1 < len(cmd) && cmd[actualPos+1:actualPos+2] == "|" {
				idx = actualPos + 1
				continue
			}

			risk := dangerousPatterns[pattern]
			analysis.DangerousTokens = append(analysis.DangerousTokens, DangerousToken{
				Token:    pattern,
				Position: actualPos,
				Risk:     risk,
			})
			analysis.IsSafe = false

			idx = actualPos + len(pattern)
		}
	}

	// Build reason string
	if !analysis.IsSafe && len(analysis.DangerousTokens) > 0 {
		risks := make(map[string]bool)
		for _, token := range analysis.DangerousTokens {
			risks[token.Risk] = true
		}

		var reasons []string
		for risk := range risks {
			if desc, ok := riskDescriptions[risk]; ok {
				reasons = append(reasons, desc)
			}
		}
		analysis.Reason = strings.Join(reasons, "; ")
	}

	return analysis
}

// AnalyzeCommandQuoteAware analyzes a shell command while respecting quoted strings.
// Characters inside single or double quotes are not considered dangerous.
func AnalyzeCommandQuoteAware(cmd string) *ShellAnalysis {
	analysis := &ShellAnalysis{
		Command: cmd,
		IsSafe:  true,
	}

	if cmd == "" {
		return analysis
	}

	// Track quote state
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	// Collect positions that are NOT inside quotes
	unquotedRanges := make([]bool, len(cmd))
	for i := range unquotedRanges {
		unquotedRanges[i] = true
	}

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]

		if escaped {
			escaped = false
			unquotedRanges[i] = false
			continue
		}

		if c == '\\' && !inSingleQuote {
			escaped = true
			continue
		}

		if c == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			unquotedRanges[i] = false
			continue
		}

		if c == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			unquotedRanges[i] = false
			continue
		}

		if inSingleQuote || inDoubleQuote {
			unquotedRanges[i] = false
		}
	}

	// Now check for dangerous patterns only in unquoted sections
	patterns := []string{">>", "&&", "||", "$(", ";", "|", ">", "<", "`", "&"}

	for _, pattern := range patterns {
		idx := 0
		for {
			pos := strings.Index(cmd[idx:], pattern)
			if pos == -1 {
				break
			}
			actualPos := idx + pos

			// Check if this position is inside quotes
			insideQuotes := false
			for i := actualPos; i < actualPos+len(pattern) && i < len(cmd); i++ {
				if !unquotedRanges[i] {
					insideQuotes = true
					break
				}
			}

			if insideQuotes {
				idx = actualPos + len(pattern)
				continue
			}

			// Skip if this is part of a longer pattern we already handled
			if pattern == ">" && actualPos > 0 && unquotedRanges[actualPos-1] && cmd[actualPos-1] == '>' {
				idx = actualPos + len(pattern)
				continue
			}
			if pattern == "&" && actualPos > 0 && unquotedRanges[actualPos-1] && cmd[actualPos-1] == '&' {
				idx = actualPos + len(pattern)
				continue
			}
			if pattern == "|" && actualPos > 0 && unquotedRanges[actualPos-1] && cmd[actualPos-1] == '|' {
				idx = actualPos + len(pattern)
				continue
			}
			if pattern == "&" && actualPos+1 < len(cmd) && unquotedRanges[actualPos+1] && cmd[actualPos+1] == '&' {
				idx = actualPos + 1
				continue
			}
			if pattern == "|" && actualPos+1 < len(cmd) && unquotedRanges[actualPos+1] && cmd[actualPos+1] == '|' {
				idx = actualPos + 1
				continue
			}

			risk := dangerousPatterns[pattern]
			analysis.DangerousTokens = append(analysis.DangerousTokens, DangerousToken{
				Token:    pattern,
				Position: actualPos,
				Risk:     risk,
			})
			analysis.IsSafe = false

			idx = actualPos + len(pattern)
		}
	}

	// Build reason string
	if !analysis.IsSafe && len(analysis.DangerousTokens) > 0 {
		risks := make(map[string]bool)
		for _, token := range analysis.DangerousTokens {
			risks[token.Risk] = true
		}

		var reasons []string
		for risk := range risks {
			if desc, ok := riskDescriptions[risk]; ok {
				reasons = append(reasons, desc)
			}
		}
		analysis.Reason = strings.Join(reasons, "; ")
	}

	return analysis
}

// IsSafeCommand is a convenience function that returns true if the command
// is considered safe for execution using quote-aware analysis.
func IsSafeCommand(cmd string) bool {
	return AnalyzeCommandQuoteAware(cmd).IsSafe
}

// ExtractUnsafeReason returns a brief explanation of why a command is unsafe,
// or an empty string if the command is safe.
func ExtractUnsafeReason(cmd string) string {
	analysis := AnalyzeCommandQuoteAware(cmd)
	return analysis.Reason
}

// SanitizeCommand attempts to make a command safe by escaping dangerous characters.
// This is a best-effort function and may not handle all edge cases.
// It's generally safer to reject unsafe commands than to try to sanitize them.
func SanitizeCommand(cmd string) string {
	// Simple approach: wrap the entire command in single quotes
	// and escape any existing single quotes
	if cmd == "" {
		return ""
	}

	// Check if command is already safe
	if IsSafeCommand(cmd) {
		return cmd
	}

	// Escape single quotes and wrap in single quotes
	escaped := strings.ReplaceAll(cmd, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

// ContainsShellMetacharacters checks if a string contains any shell metacharacters
// that could be interpreted by the shell (without quote awareness).
func ContainsShellMetacharacters(s string) bool {
	metacharacters := []rune{';', '&', '|', '>', '<', '`', '$', '(', ')', '{', '}', '[', ']', '*', '?', '!', '#', '~', '=', '%', '^'}

	for _, c := range s {
		for _, meta := range metacharacters {
			if c == meta {
				return true
			}
		}
	}
	return false
}

// IsValidFilename checks if a string is a valid, safe filename.
// It rejects names with path traversal attempts or shell metacharacters.
func IsValidFilename(name string) bool {
	if name == "" {
		return false
	}

	// Reject path separators
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}

	// Reject path traversal
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return false
	}

	// Reject shell metacharacters
	if ContainsShellMetacharacters(name) {
		return false
	}

	// Reject control characters
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}

	return true
}
