package websearch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ContentExtractor extracts readable content from web pages.
type ContentExtractor struct {
	httpClient    *http.Client
	skipSSRFCheck bool // For testing only - allows localhost URLs
}

// NewContentExtractor creates a new content extractor.
func NewContentExtractor() *ContentExtractor {
	return &ContentExtractor{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		skipSSRFCheck: false,
	}
}

// NewContentExtractorForTesting creates a content extractor that allows localhost URLs.
// This should only be used in tests.
func NewContentExtractorForTesting() *ContentExtractor {
	return &ContentExtractor{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		skipSSRFCheck: true,
	}
}

// isPrivateOrReservedIP checks if an IP address is private, loopback, or reserved.
func isPrivateOrReservedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Check for loopback (127.x.x.x, ::1)
	if ip.IsLoopback() {
		return true
	}
	// Check for link-local (169.254.x.x, fe80::/10)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Check for private ranges (10.x.x.x, 172.16-31.x.x, 192.168.x.x, fc00::/7)
	if ip.IsPrivate() {
		return true
	}
	// Check for unspecified (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return true
	}
	// Check for multicast
	if ip.IsMulticast() {
		return true
	}
	// Check for cloud metadata endpoint (169.254.169.254)
	metadataIP := net.ParseIP("169.254.169.254")
	if ip.Equal(metadataIP) {
		return true
	}
	return false
}

// validateURLForSSRF validates a URL to prevent SSRF attacks.
func validateURLForSSRF(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http and https schemes
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got: %s", parsed.Scheme)
	}

	// Extract hostname (without port)
	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Block localhost variants
	lowerHost := strings.ToLower(hostname)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") {
		return fmt.Errorf("localhost URLs are not allowed")
	}

	// Resolve hostname to IP addresses
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// If we can't resolve, allow the request (DNS may be handled by proxy)
		return nil
	}

	// Check all resolved IPs
	for _, ip := range ips {
		if isPrivateOrReservedIP(ip) {
			return fmt.Errorf("URL resolves to private/reserved IP address")
		}
	}

	return nil
}

// Extract fetches and extracts readable content from a URL.
func (e *ContentExtractor) Extract(ctx context.Context, targetURL string) (string, error) {
	// Validate URL to prevent SSRF attacks (skip in test mode)
	if !e.skipSSRFCheck {
		if err := validateURLForSSRF(targetURL); err != nil {
			return "", fmt.Errorf("URL validation failed: %w", err)
		}
	}

	// Fetch the page
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NexusBot/1.0)")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "text/plain") {
		return "", fmt.Errorf("unsupported content type: %s", contentType)
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	// Extract content using readability-like algorithm
	content := e.extractReadableContent(string(body))

	// Trim to reasonable length (10k chars)
	if len(content) > 10000 {
		content = content[:10000] + "..."
	}

	return content, nil
}

// extractReadableContent implements a simplified readability algorithm.
func (e *ContentExtractor) extractReadableContent(html string) string {
	// Remove script and style tags
	html = e.removeTag(html, "script")
	html = e.removeTag(html, "style")
	html = e.removeTag(html, "noscript")
	html = e.removeTag(html, "iframe")
	html = e.removeTag(html, "nav")
	html = e.removeTag(html, "header")
	html = e.removeTag(html, "footer")
	html = e.removeTag(html, "aside")

	// Extract title
	title := e.extractTitle(html)

	// Extract meta description
	description := e.extractMetaDescription(html)

	// Extract main content from common content containers
	content := e.extractMainContent(html)

	// If we couldn't find content in containers, try to extract from body
	if content == "" {
		content = e.extractFromBody(html)
	}

	// Clean up the content
	content = e.cleanText(content)

	// Build final content
	var result strings.Builder
	if title != "" {
		result.WriteString("Title: ")
		result.WriteString(title)
		result.WriteString("\n\n")
	}
	if description != "" {
		result.WriteString("Description: ")
		result.WriteString(description)
		result.WriteString("\n\n")
	}
	result.WriteString(content)

	return result.String()
}

// removeTag removes all occurrences of a tag from HTML.
func (e *ContentExtractor) removeTag(html, tag string) string {
	re := regexp.MustCompile(`(?i)<` + tag + `[^>]*>.*?</` + tag + `>`)
	return re.ReplaceAllString(html, "")
}

// extractTitle extracts the page title.
func (e *ContentExtractor) extractTitle(html string) string {
	// Try <title> tag
	re := regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return e.cleanText(matches[1])
	}

	// Try og:title meta tag
	re = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:title["'][^>]*content=["']([^"']*)["']`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return e.cleanText(matches[1])
	}

	// Try h1 tag
	re = regexp.MustCompile(`(?i)<h1[^>]*>(.*?)</h1>`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return e.cleanText(matches[1])
	}

	return ""
}

// extractMetaDescription extracts the meta description.
func (e *ContentExtractor) extractMetaDescription(html string) string {
	// Try meta description
	re := regexp.MustCompile(`(?i)<meta[^>]*name=["']description["'][^>]*content=["']([^"']*)["']`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return e.cleanText(matches[1])
	}

	// Try og:description
	re = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:description["'][^>]*content=["']([^"']*)["']`)
	matches = re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return e.cleanText(matches[1])
	}

	return ""
}

// extractMainContent extracts content from common content containers.
func (e *ContentExtractor) extractMainContent(html string) string {
	// Common content container patterns (using dotall flag)
	patterns := []string{
		`(?is)<main[^>]*>(.*?)</main>`,
		`(?is)<article[^>]*>(.*?)</article>`,
		`(?is)<div[^>]*class=["'][^"']*content[^"']*["'][^>]*>(.*?)</div>`,
		`(?is)<div[^>]*class=["'][^"']*article[^"']*["'][^>]*>(.*?)</div>`,
		`(?is)<div[^>]*id=["']content["'][^>]*>(.*?)</div>`,
		`(?is)<div[^>]*id=["']main["'][^>]*>(.*?)</div>`,
		`(?is)<div[^>]*role=["']main["'][^>]*>(.*?)</div>`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) > 1 {
			content := matches[1]
			// Extract text from HTML
			text := e.extractText(content)
			if len(strings.TrimSpace(text)) > 200 { // Must have substantial content
				return text
			}
		}
	}

	return ""
}

// extractFromBody extracts content from the body tag.
func (e *ContentExtractor) extractFromBody(html string) string {
	re := regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return e.extractText(matches[1])
	}
	return ""
}

// extractText extracts plain text from HTML, preserving paragraph structure.
func (e *ContentExtractor) extractText(html string) string {
	// Replace block elements with newlines
	blockElements := []string{"p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "br"}
	for _, tag := range blockElements {
		re := regexp.MustCompile(`(?i)<` + tag + `[^>]*>`)
		html = re.ReplaceAllString(html, "\n")
		re = regexp.MustCompile(`(?i)</` + tag + `>`)
		html = re.ReplaceAllString(html, "\n")
	}

	// Remove all remaining HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	text := re.ReplaceAllString(html, "")

	return text
}

// cleanText cleans up extracted text.
func (e *ContentExtractor) cleanText(text string) string {
	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&apos;", "'")

	// Normalize whitespace within lines (but preserve newlines)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		re := regexp.MustCompile(`[^\S\n]+`)
		lines[i] = re.ReplaceAllString(line, " ")
		lines[i] = strings.TrimSpace(lines[i])
	}
	text = strings.Join(lines, "\n")

	// Normalize newlines (max 2 consecutive)
	re := regexp.MustCompile(`\n{3,}`)
	text = re.ReplaceAllString(text, "\n\n")

	// Trim whitespace
	text = strings.TrimSpace(text)

	return text
}

// maxBatchConcurrency limits concurrent extractions in ExtractBatch.
const maxBatchConcurrency = 5

// ExtractBatch extracts content from multiple URLs concurrently with a concurrency limit.
func (e *ContentExtractor) ExtractBatch(ctx context.Context, urls []string) map[string]string {
	results := make(map[string]string)
	resultsChan := make(chan struct {
		url     string
		content string
	}, len(urls))

	// Use semaphore to limit concurrency
	sem := make(chan struct{}, maxBatchConcurrency)

	// Extract concurrently with limit
	for _, u := range urls {
		sem <- struct{}{} // Acquire semaphore slot
		go func(targetURL string) {
			defer func() { <-sem }() // Release semaphore slot
			content, err := e.Extract(ctx, targetURL)
			if err == nil {
				resultsChan <- struct {
					url     string
					content string
				}{targetURL, content}
			} else {
				resultsChan <- struct {
					url     string
					content string
				}{targetURL, ""}
			}
		}(u)
	}

	// Collect results
	for i := 0; i < len(urls); i++ {
		result := <-resultsChan
		if result.content != "" {
			results[result.url] = result.content
		}
	}

	return results
}
