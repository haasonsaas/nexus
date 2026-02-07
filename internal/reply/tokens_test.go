package reply

import (
	"testing"
)

func TestEscapeRegex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special characters",
			input:    "HEARTBEAT_OK",
			expected: "HEARTBEAT_OK",
		},
		{
			name:     "dot",
			input:    "test.value",
			expected: `test\.value`,
		},
		{
			name:     "asterisk",
			input:    "test*value",
			expected: `test\*value`,
		},
		{
			name:     "plus",
			input:    "test+value",
			expected: `test\+value`,
		},
		{
			name:     "question mark",
			input:    "test?value",
			expected: `test\?value`,
		},
		{
			name:     "caret",
			input:    "^start",
			expected: `\^start`,
		},
		{
			name:     "dollar",
			input:    "end$",
			expected: `end\$`,
		},
		{
			name:     "braces",
			input:    "test{1,3}",
			expected: `test\{1,3\}`,
		},
		{
			name:     "parentheses",
			input:    "(group)",
			expected: `\(group\)`,
		},
		{
			name:     "pipe",
			input:    "a|b",
			expected: `a\|b`,
		},
		{
			name:     "brackets",
			input:    "[abc]",
			expected: `\[abc\]`,
		},
		{
			name:     "backslash",
			input:    `a\b`,
			expected: `a\\b`,
		},
		{
			name:     "multiple special chars",
			input:    ".*+?^${}()|[]\\",
			expected: `\.\*\+\?\^\$\{\}\(\)\|\[\]\\`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeRegex(tt.input)
			if got != tt.expected {
				t.Errorf("EscapeRegex(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsSilentReplyText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		token    string
		expected bool
	}{
		// Token at start of text
		{
			name:     "token at start",
			text:     "NO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "token at start with space after",
			text:     "NO_REPLY some message",
			token:    "",
			expected: true,
		},
		{
			name:     "token at start with leading whitespace",
			text:     "  NO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "token at start with leading tabs",
			text:     "\t\tNO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "token at start with leading newline",
			text:     "\nNO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "token at start followed by punctuation",
			text:     "NO_REPLY: I have nothing to say",
			token:    "",
			expected: true,
		},
		{
			name:     "token at start followed by newline",
			text:     "NO_REPLY\nSome other content",
			token:    "",
			expected: true,
		},

		// Token at end of text
		{
			name:     "token at end",
			text:     "Some message NO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "token at end with trailing period",
			text:     "Some message NO_REPLY.",
			token:    "",
			expected: true,
		},
		{
			name:     "token at end with trailing exclamation",
			text:     "Some message NO_REPLY!",
			token:    "",
			expected: true,
		},
		{
			name:     "token at end with trailing spaces",
			text:     "Some message NO_REPLY   ",
			token:    "",
			expected: true,
		},
		{
			name:     "token at end with multiple trailing punctuation",
			text:     "Some message NO_REPLY...",
			token:    "",
			expected: true,
		},

		// Token in middle of text (should NOT match)
		{
			name:     "token in middle without word boundary",
			text:     "prefixNO_REPLYsuffix",
			token:    "",
			expected: false,
		},
		{
			name:     "token in middle with spaces",
			text:     "Some NO_REPLY message here",
			token:    "",
			expected: false,
		},
		{
			name:     "token embedded in word at start",
			text:     "NO_REPLYFOO bar",
			token:    "",
			expected: false,
		},

		// Custom token tests
		{
			name:     "custom token at start",
			text:     "HEARTBEAT_OK",
			token:    "HEARTBEAT_OK",
			expected: true,
		},
		{
			name:     "custom token at end",
			text:     "Status: HEARTBEAT_OK",
			token:    "HEARTBEAT_OK",
			expected: true,
		},
		{
			name:     "custom token not present",
			text:     "NO_REPLY",
			token:    "HEARTBEAT_OK",
			expected: false,
		},

		// Case sensitivity tests
		{
			name:     "lowercase token should not match",
			text:     "no_reply",
			token:    "",
			expected: false,
		},
		{
			name:     "mixed case token should not match",
			text:     "No_Reply",
			token:    "",
			expected: false,
		},

		// Empty and whitespace text
		{
			name:     "empty text",
			text:     "",
			token:    "",
			expected: false,
		},
		{
			name:     "whitespace only text",
			text:     "   ",
			token:    "",
			expected: false,
		},
		{
			name:     "newlines only",
			text:     "\n\n\n",
			token:    "",
			expected: false,
		},

		// Multiple tokens
		{
			name:     "both start and end tokens",
			text:     "NO_REPLY some message NO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "different tokens",
			text:     "HEARTBEAT_OK message NO_REPLY",
			token:    "HEARTBEAT_OK",
			expected: true,
		},

		// Token with special regex characters (edge case)
		{
			name:     "token with dot",
			text:     "TOKEN.HERE",
			token:    "TOKEN.HERE",
			expected: true,
		},
		{
			name:     "token with dot at end",
			text:     "prefix TOKEN.HERE",
			token:    "TOKEN.HERE",
			expected: true,
		},

		// Just the token (edge cases)
		{
			name:     "just the default token",
			text:     "NO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "just the token with empty token param",
			text:     "NO_REPLY",
			token:    "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			if tt.token == "" {
				got = IsSilentReplyText(tt.text)
			} else {
				got = IsSilentReplyText(tt.text, tt.token)
			}
			if got != tt.expected {
				t.Errorf("IsSilentReplyText(%q, %q) = %v, want %v", tt.text, tt.token, got, tt.expected)
			}
		})
	}
}

func TestHasHeartbeatToken(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "heartbeat token at start",
			text:     "HEARTBEAT_OK",
			expected: true,
		},
		{
			name:     "heartbeat token at start with message",
			text:     "HEARTBEAT_OK system is running",
			expected: true,
		},
		{
			name:     "heartbeat token at end",
			text:     "Status check: HEARTBEAT_OK",
			expected: true,
		},
		{
			name:     "heartbeat token at end with punctuation",
			text:     "System status: HEARTBEAT_OK!",
			expected: true,
		},
		{
			name:     "no heartbeat token",
			text:     "NO_REPLY",
			expected: false,
		},
		{
			name:     "heartbeat token in middle",
			text:     "Status HEARTBEAT_OK message",
			expected: false,
		},
		{
			name:     "empty text",
			text:     "",
			expected: false,
		},
		{
			name:     "partial heartbeat token",
			text:     "HEARTBEAT",
			expected: false,
		},
		{
			name:     "lowercase heartbeat token",
			text:     "heartbeat_ok",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasHeartbeatToken(tt.text)
			if got != tt.expected {
				t.Errorf("HasHeartbeatToken(%q) = %v, want %v", tt.text, got, tt.expected)
			}
		})
	}
}

func TestStripSilentToken(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "token at start",
			text:     "NO_REPLY",
			expected: "",
		},
		{
			name:     "token at start with message",
			text:     "NO_REPLY some message",
			expected: "some message",
		},
		{
			name:     "token at start with leading whitespace",
			text:     "  NO_REPLY some message",
			expected: "some message",
		},
		{
			name:     "token at end",
			text:     "some message NO_REPLY",
			expected: "some message",
		},
		{
			name:     "token at end with trailing punctuation",
			text:     "some message NO_REPLY.",
			expected: "some message",
		},
		{
			name:     "token at end with trailing spaces",
			text:     "some message NO_REPLY   ",
			expected: "some message",
		},
		{
			name:     "token at both start and end",
			text:     "NO_REPLY message NO_REPLY",
			expected: "message",
		},
		{
			name:     "no token present",
			text:     "just a regular message",
			expected: "just a regular message",
		},
		{
			name:     "empty text",
			text:     "",
			expected: "",
		},
		{
			name:     "whitespace only",
			text:     "   ",
			expected: "",
		},
		{
			name:     "token in middle not stripped",
			text:     "start NO_REPLY end",
			expected: "start NO_REPLY end",
		},
		{
			name:     "token with colon after",
			text:     "NO_REPLY: I have nothing to say",
			expected: ": I have nothing to say",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSilentToken(tt.text)
			if got != tt.expected {
				t.Errorf("StripSilentToken(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestStripHeartbeatToken(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "token at start",
			text:     "HEARTBEAT_OK",
			expected: "",
		},
		{
			name:     "token at start with message",
			text:     "HEARTBEAT_OK system running",
			expected: "system running",
		},
		{
			name:     "token at start with leading whitespace",
			text:     "  HEARTBEAT_OK system running",
			expected: "system running",
		},
		{
			name:     "token at end",
			text:     "system status: HEARTBEAT_OK",
			expected: "system status:",
		},
		{
			name:     "token at end with trailing punctuation",
			text:     "system status: HEARTBEAT_OK!",
			expected: "system status:",
		},
		{
			name:     "no token present",
			text:     "just a regular message",
			expected: "just a regular message",
		},
		{
			name:     "empty text",
			text:     "",
			expected: "",
		},
		{
			name:     "only token with whitespace",
			text:     "  HEARTBEAT_OK  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripHeartbeatToken(tt.text)
			if got != tt.expected {
				t.Errorf("StripHeartbeatToken(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	if HeartbeatToken != "HEARTBEAT_OK" {
		t.Errorf("HeartbeatToken = %q, want %q", HeartbeatToken, "HEARTBEAT_OK")
	}
	if SilentReplyToken != "NO_REPLY" {
		t.Errorf("SilentReplyToken = %q, want %q", SilentReplyToken, "NO_REPLY")
	}
}

func TestStripSilentToken_MultipleOccurrences(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "token at start and end with content between",
			text:     "NO_REPLY important message NO_REPLY",
			expected: "important message",
		},
		{
			name:     "token at start and end with punctuation",
			text:     "NO_REPLY: some content. NO_REPLY.",
			expected: ": some content.",
		},
		{
			name:     "token at start and end with newlines between",
			text:     "NO_REPLY\nline1\nline2\nNO_REPLY",
			expected: "line1\nline2",
		},
		{
			name:     "token at start and end with extra whitespace",
			text:     "  NO_REPLY   middle text   NO_REPLY  ",
			expected: "middle text",
		},
		{
			name:     "token only at start, not at end",
			text:     "NO_REPLY the actual message",
			expected: "the actual message",
		},
		{
			name:     "token only at end, not at start",
			text:     "the actual message NO_REPLY",
			expected: "the actual message",
		},
		{
			name:     "double token with nothing between",
			text:     "NO_REPLY NO_REPLY",
			expected: "",
		},
		{
			name:     "token at start, middle embedded (no word boundary), and end",
			text:     "NO_REPLY xNO_REPLYx NO_REPLY",
			expected: "xNO_REPLYx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSilentToken(tt.text)
			if got != tt.expected {
				t.Errorf("StripSilentToken(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestIsSilentReplyText_UnicodeAdjacent(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		token    string
		expected bool
	}{
		{
			name:     "token with leading unicode emoji",
			text:     "\U0001F600 NO_REPLY",
			token:    "",
			expected: true, // Go regexp: emoji is \W so suffix pattern \bTOKEN\b\W*$ matches
		},
		{
			name:     "token at end after unicode text",
			text:     "\u00e9l\u00e8ve NO_REPLY",
			token:    "",
			expected: true, // token at end with word boundary before it
		},
		{
			name:     "token at start followed by unicode",
			text:     "NO_REPLY \u00fcber",
			token:    "",
			expected: true, // token at start, space is non-word
		},
		{
			name:     "token surrounded by CJK characters",
			text:     "\u4f60\u597dNO_REPLY\u4e16\u754c",
			token:    "",
			expected: true, // Go regexp: CJK chars are \W, so \b exists at CJK/word boundary
		},
		{
			name:     "token at start with trailing CJK",
			text:     "NO_REPLY\u4f60\u597d",
			token:    "",
			expected: true, // CJK char is non-word, matches (?:$|\W)
		},
		{
			name:     "token at end after CJK and space",
			text:     "\u4f60\u597d NO_REPLY",
			token:    "",
			expected: true,
		},
		{
			name:     "unicode token name",
			text:     "CITT\u00c0_OK at start",
			token:    "CITT\u00c0_OK",
			expected: true,
		},
		{
			name:     "token at end with trailing unicode punctuation",
			text:     "message NO_REPLY\u2026", // ellipsis character
			token:    "",
			expected: true, // \u2026 is non-word
		},
		{
			name:     "empty text with unicode token",
			text:     "",
			token:    "\u00fcber",
			expected: false,
		},
		{
			name:     "only the token with zero-width spaces around it",
			text:     "\u200bNO_REPLY\u200b",
			token:    "",
			expected: true, // zero-width spaces are non-word chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			if tt.token == "" {
				got = IsSilentReplyText(tt.text)
			} else {
				got = IsSilentReplyText(tt.text, tt.token)
			}
			if got != tt.expected {
				t.Errorf("IsSilentReplyText(%q, %q) = %v, want %v", tt.text, tt.token, got, tt.expected)
			}
		})
	}
}
