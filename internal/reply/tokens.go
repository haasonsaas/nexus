// Package reply provides utilities for detecting and handling reply tokens.
package reply

import (
	"regexp"
	"strings"
)

// HeartbeatToken is used to indicate a successful heartbeat response.
const HeartbeatToken = "HEARTBEAT_OK"

// SilentReplyToken is used to indicate that no reply should be sent.
const SilentReplyToken = "NO_REPLY"

// regexSpecialChars contains characters that have special meaning in regular expressions.
var regexSpecialChars = regexp.MustCompile(`[.*+?^${}()|[\]\\]`)

// EscapeRegex escapes special regex characters in a string.
// This is equivalent to JavaScript's string.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").
func EscapeRegex(value string) string {
	return regexSpecialChars.ReplaceAllString(value, `\$0`)
}

// IsSilentReplyText checks if the given text starts or ends with the silent reply token.
// If no token is provided, SilentReplyToken is used as the default.
// The token must appear at the start (with optional leading whitespace) followed by
// end of string or a non-word character, or at the end preceded by a word boundary
// and optionally followed by non-word characters.
func IsSilentReplyText(text string, token ...string) bool {
	if text == "" {
		return false
	}

	t := SilentReplyToken
	if len(token) > 0 && token[0] != "" {
		t = token[0]
	}

	escaped := EscapeRegex(t)

	// Check prefix pattern: /^\s*TOKEN(?=$|\W)/
	prefixPattern := `^\s*` + escaped + `(?:$|\W)`
	prefixRe := regexp.MustCompile(prefixPattern)
	if prefixRe.MatchString(text) {
		return true
	}

	// Check suffix pattern: /\bTOKEN\b\W*$/
	suffixPattern := `\b` + escaped + `\b\W*$`
	suffixRe := regexp.MustCompile(suffixPattern)
	return suffixRe.MatchString(text)
}

// HasHeartbeatToken checks if the text contains the heartbeat token at the start or end.
func HasHeartbeatToken(text string) bool {
	return IsSilentReplyText(text, HeartbeatToken)
}

// StripSilentToken removes the silent reply token from the text.
// It removes the token from both the start and end of the text if present.
func StripSilentToken(text string) string {
	return stripToken(text, SilentReplyToken)
}

// StripHeartbeatToken removes the heartbeat token from the text.
// It removes the token from both the start and end of the text if present.
func StripHeartbeatToken(text string) string {
	return stripToken(text, HeartbeatToken)
}

// stripToken removes the specified token from both the start and end of the text.
func stripToken(text, token string) string {
	if text == "" {
		return text
	}

	escaped := EscapeRegex(token)

	// Remove from prefix: /^\s*TOKEN(?:$|\W)/
	// We need to be careful to only remove the token and leading whitespace, not trailing chars
	prefixPattern := `^\s*` + escaped + `\b\s*`
	prefixRe := regexp.MustCompile(prefixPattern)
	text = prefixRe.ReplaceAllString(text, "")

	// Remove from suffix: /\s*\bTOKEN\b\W*$/
	suffixPattern := `\s*\b` + escaped + `\b\W*$`
	suffixRe := regexp.MustCompile(suffixPattern)
	text = suffixRe.ReplaceAllString(text, "")

	return strings.TrimSpace(text)
}
