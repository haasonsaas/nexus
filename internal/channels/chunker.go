package channels

import (
	"strings"
	"unicode"
)

// MessageChunker splits long messages into channel-appropriate sizes.
// It intelligently breaks on paragraph boundaries, sentences, and words
// while preserving markdown code blocks.
type MessageChunker struct {
	// MaxSize is the maximum chunk size in characters.
	MaxSize int

	// PreserveCodeBlocks keeps code blocks intact when possible.
	PreserveCodeBlocks bool
}

// NewMessageChunker creates a chunker with the given max size.
func NewMessageChunker(maxSize int) *MessageChunker {
	if maxSize <= 0 {
		maxSize = 2000 // Default to Discord's limit
	}
	return &MessageChunker{
		MaxSize:            maxSize,
		PreserveCodeBlocks: true,
	}
}

// ChunkerFromCapabilities creates a chunker based on channel capabilities.
func ChunkerFromCapabilities(caps Capabilities) *MessageChunker {
	maxSize := caps.MaxMessageLength
	if maxSize <= 0 {
		maxSize = 4000 // Default for channels without limits
	}
	return NewMessageChunker(maxSize)
}

// Chunk splits text into pieces that fit within MaxSize.
// It tries to break at natural boundaries in this order:
// 1. Paragraph breaks (double newlines)
// 2. Single newlines (outside code blocks if PreserveCodeBlocks is true)
// 3. Sentence endings (. ! ?)
// 4. Word boundaries (spaces)
// 5. Hard break at MaxSize if necessary
func (c *MessageChunker) Chunk(text string) []string {
	if text == "" {
		return nil
	}
	if len(text) <= c.MaxSize {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > c.MaxSize {
		breakIdx := c.findBreakPoint(remaining)
		if breakIdx <= 0 {
			breakIdx = c.MaxSize
		}

		chunk := strings.TrimRightFunc(remaining[:breakIdx], unicode.IsSpace)
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// Skip separator if we broke on whitespace
		remaining = strings.TrimLeftFunc(remaining[breakIdx:], unicode.IsSpace)
	}

	if remaining = strings.TrimSpace(remaining); remaining != "" {
		chunks = append(chunks, remaining)
	}

	return chunks
}

// findBreakPoint finds the best position to break the text.
func (c *MessageChunker) findBreakPoint(text string) int {
	if len(text) <= c.MaxSize {
		return len(text)
	}

	window := text[:c.MaxSize]

	// Track code block state for markdown-aware breaking
	var inCodeBlock bool
	var codeBlockStart int
	if c.PreserveCodeBlocks {
		inCodeBlock, codeBlockStart = c.findCodeBlockState(window)
	}

	// 1. Try paragraph break (double newline)
	if idx := c.lastIndexOf(window, "\n\n", inCodeBlock, codeBlockStart); idx > 0 {
		return idx + 1 // Include one newline, skip the other
	}

	// 2. Try single newline (outside code blocks)
	if idx := c.lastIndexOf(window, "\n", inCodeBlock, codeBlockStart); idx > 0 {
		return idx + 1
	}

	// 3. Try sentence ending
	for _, ending := range []string{". ", "! ", "? ", ".\n", "!\n", "?\n"} {
		if idx := strings.LastIndex(window, ending); idx > 0 {
			if !inCodeBlock || idx < codeBlockStart {
				return idx + 1 // Include the punctuation
			}
		}
	}

	// 4. Try word boundary
	if idx := strings.LastIndexFunc(window, unicode.IsSpace); idx > 0 {
		return idx
	}

	// 5. Hard break
	return c.MaxSize
}

// lastIndexOf finds the last occurrence of sep, respecting code block boundaries.
func (c *MessageChunker) lastIndexOf(text, sep string, inCodeBlock bool, codeBlockStart int) int {
	idx := strings.LastIndex(text, sep)
	if idx <= 0 {
		return -1
	}

	// If we're in a code block, only break before it started
	if inCodeBlock && idx >= codeBlockStart {
		// Try to find a break before the code block
		if codeBlockStart > 0 {
			return strings.LastIndex(text[:codeBlockStart], sep)
		}
		return -1
	}

	return idx
}

// findCodeBlockState determines if we're inside a code block at the end of text.
// Returns (inCodeBlock, startPosition).
func (c *MessageChunker) findCodeBlockState(text string) (bool, int) {
	var inBlock bool
	var blockStart int
	var i int

	for i < len(text) {
		// Check for code fence (``` or ~~~)
		if i+2 < len(text) {
			fence := text[i : i+3]
			if fence == "```" || fence == "~~~" {
				if !inBlock {
					inBlock = true
					blockStart = i
				} else {
					// Check if this is a closing fence (on its own line or at start)
					if i == 0 || text[i-1] == '\n' {
						inBlock = false
					}
				}
				// Skip past the fence marker
				for i < len(text) && text[i] != '\n' {
					i++
				}
				continue
			}
		}
		i++
	}

	return inBlock, blockStart
}

// ChunkMarkdown is like Chunk but handles markdown code blocks more carefully.
// If a code block would be split, it closes the block and reopens it in the next chunk.
func (c *MessageChunker) ChunkMarkdown(text string) []string {
	if text == "" {
		return nil
	}
	if len(text) <= c.MaxSize {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > c.MaxSize {
		// Parse code block spans in the current window
		spans := c.parseCodeBlockSpans(remaining)

		breakIdx := c.findMarkdownBreakPoint(remaining, spans)
		if breakIdx <= 0 {
			breakIdx = c.MaxSize
		}

		chunk := remaining[:breakIdx]

		// Check if we're breaking inside a code block
		var activeBlock *codeBlockSpan
		for i := range spans {
			if spans[i].start < breakIdx && (spans[i].end == -1 || spans[i].end >= breakIdx) {
				activeBlock = &spans[i]
				break
			}
		}

		if activeBlock != nil && activeBlock.end >= breakIdx {
			// We're splitting a code block - close it
			chunk = strings.TrimRightFunc(chunk, unicode.IsSpace)
			if !strings.HasSuffix(chunk, "\n") {
				chunk += "\n"
			}
			chunk += activeBlock.fence

			// Prepare to reopen the block
			remaining = activeBlock.openLine + "\n" + strings.TrimLeftFunc(remaining[breakIdx:], unicode.IsSpace)
		} else {
			remaining = strings.TrimLeftFunc(remaining[breakIdx:], unicode.IsSpace)
		}

		if chunk = strings.TrimSpace(chunk); chunk != "" {
			chunks = append(chunks, chunk)
		}
	}

	if remaining = strings.TrimSpace(remaining); remaining != "" {
		chunks = append(chunks, remaining)
	}

	return chunks
}

type codeBlockSpan struct {
	start    int    // Start position in text
	end      int    // End position (-1 if unclosed)
	fence    string // The fence marker (``` or ~~~)
	openLine string // The full opening line (e.g., "```go")
}

// parseCodeBlockSpans finds all code block spans in text.
func (c *MessageChunker) parseCodeBlockSpans(text string) []codeBlockSpan {
	var spans []codeBlockSpan
	var current *codeBlockSpan

	lines := strings.Split(text, "\n")
	pos := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if current == nil {
			// Look for opening fence
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				fence := trimmed[:3]
				current = &codeBlockSpan{
					start:    pos,
					end:      -1,
					fence:    fence,
					openLine: line,
				}
			}
		} else {
			// Look for closing fence
			if trimmed == current.fence || strings.HasPrefix(trimmed, current.fence) {
				current.end = pos + len(line)
				spans = append(spans, *current)
				current = nil
			}
		}

		pos += len(line) + 1 // +1 for newline
	}

	// Handle unclosed block
	if current != nil {
		current.end = len(text)
		spans = append(spans, *current)
	}

	return spans
}

// findMarkdownBreakPoint finds a break point that respects code blocks.
func (c *MessageChunker) findMarkdownBreakPoint(text string, spans []codeBlockSpan) int {
	if len(text) <= c.MaxSize {
		return len(text)
	}

	window := text[:c.MaxSize]

	// Find if we'd break inside a code block
	var activeSpan *codeBlockSpan
	for i := range spans {
		if spans[i].start < c.MaxSize && (spans[i].end == -1 || spans[i].end >= c.MaxSize) {
			activeSpan = &spans[i]
			break
		}
	}

	// If inside a code block, try to find a newline within it
	if activeSpan != nil {
		// Find last newline before max size within the code block content
		searchStart := activeSpan.start + len(activeSpan.openLine) + 1
		if searchStart < len(window) {
			if idx := strings.LastIndex(window[searchStart:], "\n"); idx > 0 {
				return searchStart + idx + 1
			}
		}
		// If no good break in code block, just break at max size
		return c.MaxSize
	}

	// Not in a code block, use normal break logic
	return c.findBreakPoint(text)
}

// SplitMessage is a convenience function for simple message splitting.
func SplitMessage(text string, maxLength int) []string {
	return NewMessageChunker(maxLength).Chunk(text)
}

// SplitMarkdownMessage splits a markdown message preserving code blocks.
func SplitMarkdownMessage(text string, maxLength int) []string {
	return NewMessageChunker(maxLength).ChunkMarkdown(text)
}
