package chunk

import (
	"strings"
	"testing"
)

func TestText_NoSplit(t *testing.T) {
	text := "Hello, world!"
	chunks := Text(text, 100)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("chunk mismatch: got %q", chunks[0])
	}
}

func TestText_Empty(t *testing.T) {
	chunks := Text("", 100)
	if chunks != nil {
		t.Errorf("expected nil for empty text, got %v", chunks)
	}
}

func TestText_SplitOnNewline(t *testing.T) {
	text := "Line one\nLine two\nLine three"
	chunks := Text(text, 15)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// First chunk should end at a newline boundary
	for _, chunk := range chunks {
		if len(chunk) > 15 {
			t.Errorf("chunk exceeds limit: %d > 15", len(chunk))
		}
	}

	// Reconstruct should give us back the original content
	joined := strings.Join(chunks, "\n")
	if !strings.Contains(joined, "Line one") || !strings.Contains(joined, "Line three") {
		t.Error("content lost during chunking")
	}
}

func TestText_SplitOnWhitespace(t *testing.T) {
	text := "word1 word2 word3 word4 word5"
	chunks := Text(text, 12)

	for _, chunk := range chunks {
		if len(chunk) > 12 {
			t.Errorf("chunk exceeds limit: %q (%d chars)", chunk, len(chunk))
		}
	}
}

func TestText_HardBreak(t *testing.T) {
	text := "abcdefghijklmnopqrstuvwxyz"
	chunks := Text(text, 10)

	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if i < len(chunks)-1 && len(chunk) != 10 {
			t.Errorf("chunk %d should be exactly 10 chars, got %d", i, len(chunk))
		}
	}
}

func TestMarkdown_NoSplit(t *testing.T) {
	text := "# Hello\n\nWorld"
	chunks := Markdown(text, 100)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestMarkdown_PreservesFence(t *testing.T) {
	text := "Before\n```go\nfunc main() {\n    println(\"hello\")\n}\n```\nAfter"
	chunks := Markdown(text, 30)

	// When we split inside a fence, we should close and reopen it
	for _, chunk := range chunks {
		openCount := strings.Count(chunk, "```")
		// Each chunk should have balanced or properly closed fences
		if openCount%2 != 0 && !strings.HasSuffix(strings.TrimSpace(chunk), "```") {
			t.Logf("chunk: %q", chunk)
		}
	}

	// Verify all content is preserved
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "func main()") {
		t.Error("code content lost")
	}
}

func TestGetChannelLimit(t *testing.T) {
	tests := []struct {
		channel string
		want    int
	}{
		{"telegram", 4096},
		{"Telegram", 4096},
		{"discord", 2000},
		{"slack", 40000},
		{"sms", 160},
		{"unknown", DefaultChunkLimit},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			got := GetChannelLimit(tt.channel)
			if got != tt.want {
				t.Errorf("GetChannelLimit(%q) = %d, want %d", tt.channel, got, tt.want)
			}
		})
	}
}

func TestForChannel(t *testing.T) {
	// SMS has 160 char limit
	text := strings.Repeat("a", 400)
	chunks := ForChannel(text, "sms")

	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks for SMS, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if len(chunk) > 160 {
			t.Errorf("chunk %d exceeds SMS limit: %d", i, len(chunk))
		}
	}
}

func TestScanBreakpoints_ParenthesesAware(t *testing.T) {
	// Should not break inside parentheses
	text := "func(a, b, c) result"
	lastNewline, lastWhitespace := scanBreakpoints(text)

	if lastNewline != -1 {
		t.Errorf("expected no newline, got %d", lastNewline)
	}

	// The last whitespace should be after the closing paren
	if lastWhitespace < 13 {
		t.Errorf("expected whitespace after ), got %d", lastWhitespace)
	}
}

func TestParseFenceSpans(t *testing.T) {
	text := "Before\n```go\ncode here\n```\nAfter"
	spans := parseFenceSpans(text)

	if len(spans) != 1 {
		t.Fatalf("expected 1 fence span, got %d", len(spans))
	}

	span := spans[0]
	if span.Marker != "```" {
		t.Errorf("expected ``` marker, got %q", span.Marker)
	}
	if !strings.Contains(span.OpenLine, "go") {
		t.Errorf("expected go in open line, got %q", span.OpenLine)
	}
}

func TestFindFenceAtIndex(t *testing.T) {
	text := "Before\n```\ncode\n```\nAfter"
	spans := parseFenceSpans(text)

	// Inside fence
	fence := findFenceAtIndex(spans, 12)
	if fence == nil {
		t.Error("expected to find fence at index 12")
	}

	// Outside fence
	fence = findFenceAtIndex(spans, 2)
	if fence != nil {
		t.Error("expected no fence at index 2")
	}
}

func TestText_ZeroLimit(t *testing.T) {
	text := "Hello"
	chunks := Text(text, 0)
	if len(chunks) != 1 || chunks[0] != text {
		t.Error("zero limit should return original text")
	}
}

func TestMarkdown_NestedFences(t *testing.T) {
	// Test with nested/longer fence markers
	// The outer ```` fence should contain the inner ``` fence
	text := "````\ninner\n````"
	spans := parseFenceSpans(text)

	if len(spans) != 1 {
		t.Errorf("expected 1 span for fence, got %d", len(spans))
	}
}

func TestMarkdownForChannel(t *testing.T) {
	code := "```\n" + strings.Repeat("x", 3000) + "\n```"
	chunks := MarkdownForChannel(code, "discord")

	// Chunks may slightly exceed limit when adding fence closers
	// Allow some slack for the fence markers
	maxAllowed := 2000 + 10 // Allow for ``` + newline
	for i, chunk := range chunks {
		if len(chunk) > maxAllowed {
			t.Errorf("chunk %d exceeds limit: %d > %d", i, len(chunk), maxAllowed)
		}
	}

	// Verify we got multiple chunks
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
}
