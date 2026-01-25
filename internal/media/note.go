package media

import (
	"fmt"
	"strings"
)

// MediaAttachment represents a single media attachment with path, type, and URL.
type MediaAttachment struct {
	// Path is the file path or identifier for the attachment
	Path string

	// Type is the media type (e.g., "image", "audio", "video")
	Type string

	// URL is the optional URL for the attachment
	URL string
}

// MediaContext holds the context for building media notes.
// It supports both array-based and single-value attachment specifications.
type MediaContext struct {
	// MediaPaths is a list of file paths for multiple attachments
	MediaPaths []string

	// MediaUrls is a list of URLs corresponding to MediaPaths
	MediaUrls []string

	// MediaTypes is a list of types corresponding to MediaPaths
	MediaTypes []string

	// MediaPath is a single file path (used when MediaPaths is empty)
	MediaPath string

	// MediaUrl is a single URL (used when MediaUrls is empty)
	MediaUrl string

	// MediaType is a single type (used when MediaTypes is empty)
	MediaType string

	// SuppressedIndices contains indices of attachments that should be excluded
	// (e.g., already processed via media understanding)
	SuppressedIndices map[int]bool
}

// FormatMediaAttachedLine formats a single media attachment line.
// If index and total are provided (both > 0), it includes the index/total prefix.
// Format: "[media attached: path (type) | url]" or "[media attached 1/3: path (type) | url]"
func FormatMediaAttachedLine(path, mediaType, url string, index, total int) string {
	var prefix string
	if index > 0 && total > 0 {
		prefix = fmt.Sprintf("[media attached %d/%d: ", index, total)
	} else {
		prefix = "[media attached: "
	}

	var typePart string
	if trimmedType := strings.TrimSpace(mediaType); trimmedType != "" {
		typePart = fmt.Sprintf(" (%s)", trimmedType)
	}

	var urlPart string
	if trimmedURL := strings.TrimSpace(url); trimmedURL != "" {
		urlPart = fmt.Sprintf(" | %s", trimmedURL)
	}

	return fmt.Sprintf("%s%s%s%s]", prefix, path, typePart, urlPart)
}

// attachmentEntry holds processed attachment data for internal use.
type attachmentEntry struct {
	path      string
	mediaType string
	url       string
	origIndex int
}

// BuildMediaNote constructs a media note from the given context.
// It handles arrays of paths/urls/types, single values, and suppressed indices.
// Returns empty string if no attachments are present or all are suppressed.
//
// For a single attachment: "[media attached: path]"
// For multiple attachments:
//
//	"[media attached: N files]
//	[media attached 1/N: path1]
//	[media attached 2/N: path2]
//	..."
func BuildMediaNote(ctx MediaContext) string {
	// Build paths list from arrays or single value
	var paths []string
	if len(ctx.MediaPaths) > 0 {
		paths = ctx.MediaPaths
	} else if trimmed := strings.TrimSpace(ctx.MediaPath); trimmed != "" {
		paths = []string{trimmed}
	}

	if len(paths) == 0 {
		return ""
	}

	// Get URLs array if it matches paths length
	var urls []string
	if len(ctx.MediaUrls) == len(paths) {
		urls = ctx.MediaUrls
	}

	// Get types array if it matches paths length
	var types []string
	if len(ctx.MediaTypes) == len(paths) {
		types = ctx.MediaTypes
	}

	// Build entries, filtering out suppressed ones
	var entries []attachmentEntry
	for i, path := range paths {
		if ctx.SuppressedIndices != nil && ctx.SuppressedIndices[i] {
			continue
		}

		entry := attachmentEntry{
			path:      path,
			origIndex: i,
		}

		// Get type from array or fall back to single value
		if types != nil && i < len(types) {
			entry.mediaType = types[i]
		} else {
			entry.mediaType = ctx.MediaType
		}

		// Get URL from array or fall back to single value
		if urls != nil && i < len(urls) {
			entry.url = urls[i]
		} else {
			entry.url = ctx.MediaUrl
		}

		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return ""
	}

	// Single attachment: simple format without index
	if len(entries) == 1 {
		e := entries[0]
		return FormatMediaAttachedLine(e.path, e.mediaType, e.url, 0, 0)
	}

	// Multiple attachments: header line plus indexed entries
	count := len(entries)
	lines := make([]string, 0, count+1)
	lines = append(lines, fmt.Sprintf("[media attached: %d files]", count))

	for idx, entry := range entries {
		lines = append(lines, FormatMediaAttachedLine(
			entry.path,
			entry.mediaType,
			entry.url,
			idx+1, // 1-indexed
			count,
		))
	}

	return strings.Join(lines, "\n")
}
