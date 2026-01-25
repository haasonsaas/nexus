// Package links provides link understanding capabilities for extracting and processing URLs from messages.
package links

import (
	"net/url"
	"regexp"
	"strings"
)

// Default limits
const (
	DefaultMaxLinks = 5
)

// URL detection regex patterns
var (
	// Match http/https URLs
	httpURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)
	// Common link shorteners to expand
	shorteners = []string{"bit.ly", "t.co", "goo.gl", "tinyurl.com", "ow.ly", "is.gd", "buff.ly", "j.mp", "rb.gy", "cutt.ly"}
)

// ExtractLinksFromMessage extracts URLs from message text.
func ExtractLinksFromMessage(message string, maxLinks int) []string {
	if maxLinks <= 0 {
		maxLinks = DefaultMaxLinks
	}

	matches := httpURLPattern.FindAllString(message, -1)

	// Deduplicate and limit
	seen := make(map[string]bool)
	var urls []string
	for _, match := range matches {
		// Clean trailing punctuation
		match = strings.TrimRight(match, ".,;:!?)")

		if !seen[match] && len(urls) < maxLinks {
			seen[match] = true
			urls = append(urls, match)
		}
	}

	return urls
}

// IsShortenerURL checks if a URL is from a known shortener.
func IsShortenerURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Host)
	// Remove www. prefix if present
	host = strings.TrimPrefix(host, "www.")

	for _, shortener := range shorteners {
		if host == shortener {
			return true
		}
	}

	return false
}

// NormalizeURL normalizes a URL for deduplication.
// It lowercases the scheme and host, removes trailing slashes,
// and sorts query parameters.
func NormalizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	// Lowercase scheme and host
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	// Remove default ports
	host := parsed.Host
	if strings.HasSuffix(host, ":80") && parsed.Scheme == "http" {
		parsed.Host = strings.TrimSuffix(host, ":80")
	}
	if strings.HasSuffix(host, ":443") && parsed.Scheme == "https" {
		parsed.Host = strings.TrimSuffix(host, ":443")
	}

	// Remove trailing slash from path (unless it's the root)
	if parsed.Path != "/" && strings.HasSuffix(parsed.Path, "/") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}

	// Sort query parameters for consistent comparison
	if parsed.RawQuery != "" {
		values := parsed.Query()
		parsed.RawQuery = values.Encode()
	}

	// Remove fragment for deduplication purposes
	parsed.Fragment = ""

	return parsed.String()
}

// ExtractDomain extracts the domain from a URL.
func ExtractDomain(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}

	host := strings.ToLower(parsed.Host)
	// Remove port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	return host
}

// IsValidURL checks if a string is a valid HTTP/HTTPS URL.
func IsValidURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}

	scheme := strings.ToLower(parsed.Scheme)
	return (scheme == "http" || scheme == "https") && parsed.Host != ""
}
