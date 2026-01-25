// Package policy provides send policy and group activation handling.
package policy

import (
	"regexp"
	"strings"
)

// SendPolicyOverride represents an explicit send policy override.
type SendPolicyOverride string

const (
	// SendPolicyAllow allows sending messages.
	SendPolicyAllow SendPolicyOverride = "allow"
	// SendPolicyDeny denies sending messages.
	SendPolicyDeny SendPolicyOverride = "deny"
)

// SendPolicyMode represents the result mode from parsing a /send command.
// It can be a SendPolicyOverride value or "inherit" to reset to default behavior.
type SendPolicyMode string

const (
	// SendPolicyInherit resets to inherit default behavior.
	SendPolicyInherit SendPolicyMode = "inherit"
)

// NormalizeSendPolicyOverride normalizes a raw string to a SendPolicyOverride.
// Returns nil if the value is not recognized.
// Maps: "allow", "on" -> SendPolicyAllow
// Maps: "deny", "off" -> SendPolicyDeny
func NormalizeSendPolicyOverride(raw string) *SendPolicyOverride {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return nil
	}
	switch value {
	case "allow", "on":
		result := SendPolicyAllow
		return &result
	case "deny", "off":
		result := SendPolicyDeny
		return &result
	default:
		return nil
	}
}

// SendPolicyCommandResult holds the result of parsing a /send command.
type SendPolicyCommandResult struct {
	// HasCommand indicates whether a valid /send command was detected.
	HasCommand bool
	// Mode is the parsed mode. Only set if HasCommand is true.
	// Can be "allow", "deny", or "inherit". Empty if command has no argument.
	Mode string
}

// sendPolicyCommandRegex matches /send with optional mode argument.
// Supports both space-separated (/send allow) and colon-separated (/send: allow) syntax.
var sendPolicyCommandRegex = regexp.MustCompile(`(?i)^/send(?:\s*:\s*|\s+)?([a-zA-Z]*)\s*$`)

// ParseSendPolicyCommand parses a raw message to detect a /send command.
// Returns HasCommand=true if the message is a valid /send command.
// Mode will be:
//   - "allow" for "allow" or "on"
//   - "deny" for "deny" or "off"
//   - "inherit" for "inherit", "default", or "reset"
//   - empty string if command has no argument
func ParseSendPolicyCommand(raw string) SendPolicyCommandResult {
	if raw == "" {
		return SendPolicyCommandResult{HasCommand: false}
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return SendPolicyCommandResult{HasCommand: false}
	}

	// Normalize colon syntax to space syntax
	normalized := normalizeCommandBody(trimmed)

	match := sendPolicyCommandRegex.FindStringSubmatch(normalized)
	if match == nil {
		return SendPolicyCommandResult{HasCommand: false}
	}

	token := strings.TrimSpace(strings.ToLower(match[1]))
	if token == "" {
		return SendPolicyCommandResult{HasCommand: true}
	}

	// Check for inherit/default/reset
	switch token {
	case "inherit", "default", "reset":
		return SendPolicyCommandResult{HasCommand: true, Mode: string(SendPolicyInherit)}
	}

	// Check for allow/deny
	override := NormalizeSendPolicyOverride(token)
	if override != nil {
		return SendPolicyCommandResult{HasCommand: true, Mode: string(*override)}
	}

	// Valid command but unrecognized mode
	return SendPolicyCommandResult{HasCommand: true}
}

// normalizeCommandBody handles colon-separated command syntax.
// Converts "/send: allow" to "/send allow".
func normalizeCommandBody(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "/") {
		return trimmed
	}

	// Handle newlines - only consider first line
	if idx := strings.Index(trimmed, "\n"); idx != -1 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}

	// Handle colon syntax: /command: args -> /command args
	colonMatch := regexp.MustCompile(`^/([^\s:]+)\s*:\s*(.*)$`).FindStringSubmatch(trimmed)
	if colonMatch != nil {
		command := colonMatch[1]
		rest := strings.TrimSpace(colonMatch[2])
		if rest != "" {
			return "/" + command + " " + rest
		}
		return "/" + command
	}

	return trimmed
}
