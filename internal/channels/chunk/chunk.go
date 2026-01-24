// Package chunk provides utilities for splitting outbound text into platform-sized
// chunks without breaking on newlines or inside code fences.
package chunk

import (
	"regexp"
	"strings"
	"unicode"
)

// DefaultChunkLimit is the default maximum chunk size in characters.
const DefaultChunkLimit = 4000

// ChannelLimits defines default message size limits for various platforms.
var ChannelLimits = map[string]int{
	"telegram": 4096,
	"discord":  2000,
	"slack":    40000,
	"whatsapp": 65536,
	"signal":   65536,
	"sms":      160,
	"matrix":   65536,
	"imessage": 20000,
}

// GetChannelLimit returns the message size limit for a channel.
func GetChannelLimit(channel string) int {
	if limit, ok := ChannelLimits[strings.ToLower(channel)]; ok {
		return limit
	}
	return DefaultChunkLimit
}

// Text splits text into chunks that fit within the specified limit.
// It prefers breaking at newlines, then whitespace, then hard breaks.
func Text(text string, limit int) []string {
	if text == "" {
		return nil
	}
	if limit <= 0 {
		return []string{text}
	}
	if len(text) <= limit {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > limit {
		window := remaining[:limit]

		// Find break points (prefer newline, then whitespace)
		lastNewline, lastWhitespace := scanBreakpoints(window)

		var breakIdx int
		if lastNewline > 0 {
			breakIdx = lastNewline
		} else if lastWhitespace > 0 {
			breakIdx = lastWhitespace
		} else {
			breakIdx = limit // Hard break
		}

		chunk := strings.TrimRight(remaining[:breakIdx], " \t")
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}

		// Skip the separator if we broke on whitespace/newline
		nextStart := breakIdx
		if breakIdx < len(remaining) && unicode.IsSpace(rune(remaining[breakIdx])) {
			nextStart++
		}
		remaining = strings.TrimLeft(remaining[nextStart:], " \t")
	}

	if len(remaining) > 0 {
		chunks = append(chunks, remaining)
	}

	return chunks
}

// Markdown splits markdown text into chunks, preserving code fence integrity.
// When splitting inside a code fence, it closes the fence and reopens it in the next chunk.
func Markdown(text string, limit int) []string {
	if text == "" {
		return nil
	}
	if limit <= 0 {
		return []string{text}
	}
	if len(text) <= limit {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > limit {
		spans := parseFenceSpans(remaining)
		window := remaining[:limit]

		// Find a safe break point
		breakIdx := pickSafeBreakIndex(window, spans)
		if breakIdx <= 0 {
			breakIdx = limit
		}

		// Check if we're breaking inside a fence
		fence := findFenceAtIndex(spans, breakIdx)

		chunk := remaining[:breakIdx]
		next := remaining[breakIdx:]

		if fence != nil && !isSafeBreak(spans, breakIdx) {
			// Close the fence in this chunk and reopen in next
			closeLine := fence.Indent + fence.Marker
			if !strings.HasSuffix(chunk, "\n") {
				chunk += "\n"
			}
			chunk += closeLine
			next = fence.OpenLine + "\n" + next
		} else {
			next = strings.TrimLeft(next, "\n")
		}

		chunks = append(chunks, chunk)
		remaining = next
	}

	if len(remaining) > 0 {
		chunks = append(chunks, remaining)
	}

	return chunks
}

// FenceSpan represents a code fence in markdown.
type FenceSpan struct {
	Start    int
	End      int
	Indent   string
	Marker   string
	OpenLine string
}

var fenceRegex = regexp.MustCompile("(?m)^([ \t]*)(```+|~~~+)([^\n]*)\n")

// parseFenceSpans finds all code fence spans in text.
func parseFenceSpans(text string) []FenceSpan {
	var spans []FenceSpan
	consumed := 0

	for consumed < len(text) {
		remaining := text[consumed:]
		match := fenceRegex.FindStringSubmatchIndex(remaining)
		if match == nil {
			break
		}

		if len(match) < 8 {
			consumed += match[1]
			continue
		}

		start := consumed + match[0]
		indent := remaining[match[2]:match[3]]
		marker := remaining[match[4]:match[5]]
		openLine := remaining[match[0] : match[1]-1] // Exclude trailing newline

		// Find the closing fence - must match same marker length
		searchStart := match[1]
		closePattern := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(indent) + regexp.QuoteMeta(marker) + "[ \t]*$")
		closeMatch := closePattern.FindStringIndex(remaining[searchStart:])

		var end int
		if closeMatch != nil {
			end = consumed + searchStart + closeMatch[1]
		} else {
			end = len(text)
		}

		spans = append(spans, FenceSpan{
			Start:    start,
			End:      end,
			Indent:   indent,
			Marker:   marker,
			OpenLine: openLine,
		})

		// Skip past this entire fence span
		consumed = end
	}

	return spans
}

// findFenceAtIndex returns the fence span containing the given index.
func findFenceAtIndex(spans []FenceSpan, idx int) *FenceSpan {
	for i := range spans {
		if idx >= spans[i].Start && idx < spans[i].End {
			return &spans[i]
		}
	}
	return nil
}

// isSafeBreak checks if breaking at idx is safe (not inside a fence or at fence boundary).
func isSafeBreak(spans []FenceSpan, idx int) bool {
	fence := findFenceAtIndex(spans, idx)
	if fence == nil {
		return true
	}
	// Safe if at the end of the fence
	return idx >= fence.End
}

// pickSafeBreakIndex finds a good break point that respects fence boundaries.
func pickSafeBreakIndex(window string, spans []FenceSpan) int {
	lastNewline := -1
	lastWhitespace := -1

	for i := 0; i < len(window); i++ {
		if !isSafeBreak(spans, i) {
			continue
		}
		c := window[i]
		if c == '\n' {
			lastNewline = i
		} else if unicode.IsSpace(rune(c)) {
			lastWhitespace = i
		}
	}

	if lastNewline > 0 {
		return lastNewline
	}
	if lastWhitespace > 0 {
		return lastWhitespace
	}
	return -1
}

// scanBreakpoints finds the last newline and whitespace positions in a window.
func scanBreakpoints(window string) (lastNewline, lastWhitespace int) {
	lastNewline = -1
	lastWhitespace = -1
	depth := 0 // Track parentheses depth

	for i := 0; i < len(window); i++ {
		c := window[i]
		switch c {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '\n':
			if depth == 0 {
				lastNewline = i
			}
		default:
			if depth == 0 && unicode.IsSpace(rune(c)) {
				lastWhitespace = i
			}
		}
	}

	return lastNewline, lastWhitespace
}

// ForChannel splits text for a specific channel using its default limit.
func ForChannel(text, channel string) []string {
	limit := GetChannelLimit(channel)
	return Text(text, limit)
}

// MarkdownForChannel splits markdown text for a specific channel.
func MarkdownForChannel(text, channel string) []string {
	limit := GetChannelLimit(channel)
	return Markdown(text, limit)
}
