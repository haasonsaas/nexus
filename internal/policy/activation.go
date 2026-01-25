package policy

import (
	"regexp"
	"strings"
)

// GroupActivationMode represents how a bot should be activated in group chats.
type GroupActivationMode string

const (
	// ActivationMention activates only when mentioned.
	ActivationMention GroupActivationMode = "mention"
	// ActivationAlways activates on all messages.
	ActivationAlways GroupActivationMode = "always"
)

// NormalizeGroupActivation normalizes a raw string to a GroupActivationMode.
// Returns nil if the value is not recognized.
func NormalizeGroupActivation(raw string) *GroupActivationMode {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "mention":
		result := ActivationMention
		return &result
	case "always":
		result := ActivationAlways
		return &result
	default:
		return nil
	}
}

// ActivationCommandResult holds the result of parsing an /activation command.
type ActivationCommandResult struct {
	// HasCommand indicates whether a valid /activation command was detected.
	HasCommand bool
	// Mode is the parsed mode. Only set if HasCommand is true and a valid mode was provided.
	// Can be "mention" or "always".
	Mode *GroupActivationMode
}

// activationCommandRegex matches /activation with optional mode argument.
// Supports both space-separated (/activation mention) and colon-separated (/activation: mention) syntax.
var activationCommandRegex = regexp.MustCompile(`(?i)^/activation(?:\s*:\s*|\s+)?([a-zA-Z]*)\s*$`)

// ParseActivationCommand parses a raw message to detect an /activation command.
// Returns HasCommand=true if the message is a valid /activation command.
// Mode will be set if a recognized mode (mention/always) was provided.
func ParseActivationCommand(raw string) ActivationCommandResult {
	if raw == "" {
		return ActivationCommandResult{HasCommand: false}
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ActivationCommandResult{HasCommand: false}
	}

	// Normalize colon syntax to space syntax
	normalized := normalizeCommandBody(trimmed)

	match := activationCommandRegex.FindStringSubmatch(normalized)
	if match == nil {
		return ActivationCommandResult{HasCommand: false}
	}

	mode := NormalizeGroupActivation(match[1])
	return ActivationCommandResult{HasCommand: true, Mode: mode}
}
