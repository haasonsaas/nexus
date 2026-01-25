package heartbeat

import "strings"

// VisibilityMode defines how heartbeats are displayed.
type VisibilityMode string

const (
	// VisibilityTyping shows typing indicator during heartbeats.
	VisibilityTyping VisibilityMode = "typing"
	// VisibilityPresence shows presence/online status during heartbeats.
	VisibilityPresence VisibilityMode = "presence"
	// VisibilityNone shows no visibility indicator.
	VisibilityNone VisibilityMode = "none"
)

// channelDefaults maps channel types to their default visibility modes.
var channelDefaults = map[string]VisibilityMode{
	"slack":    VisibilityTyping,
	"discord":  VisibilityTyping,
	"telegram": VisibilityTyping,
	"matrix":   VisibilityTyping,
	"web":      VisibilityPresence,
	"api":      VisibilityNone,
	"cli":      VisibilityNone,
	"personal": VisibilityNone,
}

// ResolveVisibilityMode resolves visibility from config string and channel type.
// If mode is empty or invalid, uses the channel's default.
// If channel has no default, defaults to VisibilityNone.
func ResolveVisibilityMode(mode string, channel string) VisibilityMode {
	// If explicit mode is provided, use it
	normalizedMode := strings.ToLower(strings.TrimSpace(mode))
	switch normalizedMode {
	case "typing":
		return VisibilityTyping
	case "presence":
		return VisibilityPresence
	case "none":
		return VisibilityNone
	}

	// Fall back to channel default
	normalizedChannel := strings.ToLower(strings.TrimSpace(channel))
	if defaultMode, ok := channelDefaults[normalizedChannel]; ok {
		return defaultMode
	}

	return VisibilityNone
}

// ShouldSendTyping returns true if typing indicators should be sent.
func ShouldSendTyping(mode VisibilityMode) bool {
	return mode == VisibilityTyping
}

// ShouldSendPresence returns true if presence should be updated.
func ShouldSendPresence(mode VisibilityMode) bool {
	return mode == VisibilityPresence || mode == VisibilityTyping
}

// ParseVisibilityMode parses a string into a VisibilityMode.
// Returns VisibilityNone for unrecognized values.
func ParseVisibilityMode(s string) VisibilityMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "typing":
		return VisibilityTyping
	case "presence":
		return VisibilityPresence
	case "none", "":
		return VisibilityNone
	default:
		return VisibilityNone
	}
}

// String returns the string representation of a VisibilityMode.
func (m VisibilityMode) String() string {
	return string(m)
}

// IsValid returns true if the mode is a recognized value.
func (m VisibilityMode) IsValid() bool {
	switch m {
	case VisibilityTyping, VisibilityPresence, VisibilityNone:
		return true
	default:
		return false
	}
}
