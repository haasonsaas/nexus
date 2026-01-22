package channels

import (
	"strings"
	"testing"
)

func TestMessageChunker_ShortText(t *testing.T) {
	chunker := NewMessageChunker(100)
	text := "Hello, world!"

	chunks := chunker.Chunk(text)

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("expected %q, got %q", text, chunks[0])
	}
}

func TestMessageChunker_EmptyText(t *testing.T) {
	chunker := NewMessageChunker(100)

	chunks := chunker.Chunk("")

	if chunks != nil {
		t.Errorf("expected nil for empty text, got %v", chunks)
	}
}

func TestMessageChunker_ParagraphBreak(t *testing.T) {
	chunker := NewMessageChunker(30)
	text := "First paragraph here.\n\nSecond paragraph here."

	chunks := chunker.Chunk(text)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
		return
	}
	if chunks[0] != "First paragraph here." {
		t.Errorf("first chunk = %q", chunks[0])
	}
}

func TestMessageChunker_SentenceBreak(t *testing.T) {
	chunker := NewMessageChunker(40)
	text := "First sentence here. Second sentence here."

	chunks := chunker.Chunk(text)

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "First sentence here." {
		t.Errorf("first chunk = %q", chunks[0])
	}
}

func TestMessageChunker_WordBreak(t *testing.T) {
	chunker := NewMessageChunker(15)
	text := "Hello world test"

	chunks := chunker.Chunk(text)

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestMessageChunker_HardBreak(t *testing.T) {
	chunker := NewMessageChunker(10)
	text := "abcdefghijklmnop"

	chunks := chunker.Chunk(text)

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d: %v", len(chunks), chunks)
	}
	if len(chunks[0]) != 10 {
		t.Errorf("first chunk length = %d, expected 10", len(chunks[0]))
	}
}

func TestMessageChunker_CodeBlockPreservation(t *testing.T) {
	chunker := NewMessageChunker(100)
	// Text with code block that fits in one chunk
	text := "Here is code:\n```go\nfunc main() {}\n```\nEnd."

	chunks := chunker.Chunk(text)

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunkMarkdown_SplitCodeBlock(t *testing.T) {
	chunker := NewMessageChunker(30)
	text := "Start\n```go\nline1\nline2\nline3\nline4\n```\nEnd"

	chunks := chunker.ChunkMarkdown(text)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
		return
	}

	// Content should be preserved across chunks
	combined := strings.Join(chunks, "\n")
	if !strings.Contains(combined, "line1") || !strings.Contains(combined, "line4") {
		t.Errorf("lost content when splitting: %s", combined)
	}
}

func TestChunkMarkdown_PreservesUnclosedBlock(t *testing.T) {
	chunker := NewMessageChunker(30)
	text := "```python\nprint('hello')\nprint('world')"

	chunks := chunker.ChunkMarkdown(text)

	// Should handle unclosed code block gracefully
	if len(chunks) == 0 {
		t.Error("expected at least 1 chunk")
	}
}

func TestSplitMessage_Convenience(t *testing.T) {
	chunks := SplitMessage("Hello world", 100)

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", chunks[0])
	}
}

func TestSplitMarkdownMessage_Convenience(t *testing.T) {
	chunks := SplitMarkdownMessage("```\ncode\n```", 100)

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunkerFromCapabilities(t *testing.T) {
	caps := Capabilities{MaxMessageLength: 500}
	chunker := ChunkerFromCapabilities(caps)

	if chunker.MaxSize != 500 {
		t.Errorf("expected MaxSize 500, got %d", chunker.MaxSize)
	}
}

func TestChunkerFromCapabilities_DefaultLimit(t *testing.T) {
	caps := Capabilities{MaxMessageLength: 0}
	chunker := ChunkerFromCapabilities(caps)

	if chunker.MaxSize != 4000 {
		t.Errorf("expected default MaxSize 4000, got %d", chunker.MaxSize)
	}
}

func TestMessageChunker_NewlineBreak(t *testing.T) {
	chunker := NewMessageChunker(30)
	text := "Line one here\nLine two here\nLine three"

	chunks := chunker.Chunk(text)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestMessageChunker_LongCodeBlock(t *testing.T) {
	chunker := NewMessageChunker(50)
	// A code block that exceeds the limit
	code := "```\n" + strings.Repeat("x", 100) + "\n```"

	chunks := chunker.ChunkMarkdown(code)

	// Should split but keep content
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for long code block, got %d", len(chunks))
	}
}

func TestMessageChunker_MixedContent(t *testing.T) {
	chunker := NewMessageChunker(100)
	text := `Here is some text.

And a paragraph.

` + "```go\nfunc test() {}\n```" + `

More text after code.`

	chunks := chunker.ChunkMarkdown(text)

	// Should handle mixed content
	combined := strings.Join(chunks, "")
	if !strings.Contains(combined, "func test()") {
		t.Error("lost code block content")
	}
	if !strings.Contains(combined, "More text") {
		t.Error("lost text after code block")
	}
}

func TestParseCodeBlockSpans(t *testing.T) {
	chunker := NewMessageChunker(1000)

	tests := []struct {
		name     string
		text     string
		expected int // number of spans
	}{
		{"no blocks", "just text", 0},
		{"one block", "```\ncode\n```", 1},
		{"two blocks", "```\na\n```\n\n```\nb\n```", 2},
		{"unclosed", "```\ncode", 1},
		{"tilde fence", "~~~\ncode\n~~~", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := chunker.parseCodeBlockSpans(tt.text)
			if len(spans) != tt.expected {
				t.Errorf("expected %d spans, got %d", tt.expected, len(spans))
			}
		})
	}
}

func TestFindCodeBlockState(t *testing.T) {
	chunker := NewMessageChunker(1000)

	tests := []struct {
		name    string
		text    string
		inBlock bool
	}{
		{"plain text", "just text", false},
		{"closed block", "```\ncode\n```", false},
		{"unclosed block", "```\ncode", true},
		{"text after block", "```\ncode\n```\nmore", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inBlock, _ := chunker.findCodeBlockState(tt.text)
			if inBlock != tt.inBlock {
				t.Errorf("expected inBlock=%v, got %v", tt.inBlock, inBlock)
			}
		})
	}
}
