package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContentExtractor_Extract_Success(t *testing.T) {
	htmlContent := `
<!DOCTYPE html>
<html>
<head>
    <title>Test Page Title</title>
    <meta name="description" content="This is a test page description">
</head>
<body>
    <header>
        <nav>Navigation menu</nav>
    </header>
    <main>
        <article>
            <h1>Main Article Title</h1>
            <p>This is the first paragraph of the article.</p>
            <p>This is the second paragraph with more content.</p>
            <p>And a third paragraph to ensure we have enough content.</p>
        </article>
    </main>
    <footer>Footer content</footer>
    <script>console.log("should be removed");</script>
</body>
</html>
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	content, err := extractor.Extract(context.Background(), server.URL)

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if content == "" {
		t.Fatal("extracted content is empty")
	}

	// Check that title is present
	if !strings.Contains(content, "Test Page Title") {
		t.Error("content should contain the page title")
	}

	// Check that description is present
	if !strings.Contains(content, "test page description") {
		t.Error("content should contain the meta description")
	}

	// Check that article content is present
	if !strings.Contains(content, "first paragraph") {
		t.Error("content should contain article text")
	}

	// Check that script content is not present
	if strings.Contains(content, "console.log") {
		t.Error("content should not contain script tags")
	}

	// Check that navigation is not present
	if strings.Contains(content, "Navigation menu") {
		t.Error("content should not contain navigation")
	}
}

func TestContentExtractor_Extract_NonHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key": "value"}`))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	_, err := extractor.Extract(context.Background(), server.URL)

	if err == nil {
		t.Error("expected error for non-HTML content")
	}

	if !strings.Contains(err.Error(), "unsupported content type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestContentExtractor_Extract_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	_, err := extractor.Extract(context.Background(), server.URL)

	if err == nil {
		t.Error("expected error for HTTP 404")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestContentExtractor_Extract_InvalidURL(t *testing.T) {
	extractor := NewContentExtractor()
	_, err := extractor.Extract(context.Background(), "not-a-valid-url")

	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestContentExtractor_Extract_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Second) // Longer than client timeout
		_, _ = w.Write([]byte("<html><body>Too slow</body></html>"))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := extractor.Extract(ctx, server.URL)

	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestContentExtractor_ExtractTitle(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "title tag",
			html:     `<html><head><title>Page Title</title></head></html>`,
			expected: "Page Title",
		},
		{
			name:     "og:title meta tag",
			html:     `<html><head><meta property="og:title" content="OG Title"></head></html>`,
			expected: "OG Title",
		},
		{
			name:     "h1 fallback",
			html:     `<html><body><h1>H1 Title</h1></body></html>`,
			expected: "H1 Title",
		},
		{
			name:     "no title",
			html:     `<html><body>No title here</body></html>`,
			expected: "",
		},
	}

	extractor := NewContentExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title := extractor.extractTitle(tt.html)
			if title != tt.expected {
				t.Errorf("expected title '%s', got '%s'", tt.expected, title)
			}
		})
	}
}

func TestContentExtractor_ExtractMetaDescription(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "meta description",
			html:     `<html><head><meta name="description" content="Page description"></head></html>`,
			expected: "Page description",
		},
		{
			name:     "og:description",
			html:     `<html><head><meta property="og:description" content="OG description"></head></html>`,
			expected: "OG description",
		},
		{
			name:     "no description",
			html:     `<html><head></head></html>`,
			expected: "",
		},
	}

	extractor := NewContentExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			description := extractor.extractMetaDescription(tt.html)
			if description != tt.expected {
				t.Errorf("expected description '%s', got '%s'", tt.expected, description)
			}
		})
	}
}

func TestContentExtractor_ExtractMainContent(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		shouldFind  bool
		containsText string
	}{
		{
			name: "main tag",
			html: `<html><body><main><p>Main content here with enough text to be substantial. This paragraph has more content to meet the minimum length requirement for extraction. We need at least 200 characters of text content to be extracted successfully by the content extraction algorithm.</p></main></body></html>`,
			shouldFind: true,
			containsText: "Main content",
		},
		{
			name: "article tag",
			html: `<html><body><article><p>Article content here with enough text to be substantial. This paragraph has more content to meet the minimum length requirement for extraction. We need at least 200 characters of text content to be extracted successfully by the content extraction algorithm.</p></article></body></html>`,
			shouldFind: true,
			containsText: "Article content",
		},
		{
			name: "content class",
			html: `<html><body><div class="content"><p>Div content here with enough text to be substantial enough. This paragraph has more content to meet the minimum length requirement for extraction. We need at least 200 characters of text to be extracted successfully.</p></div></body></html>`,
			shouldFind: true,
			containsText: "Div content",
		},
		{
			name: "too short content",
			html: `<html><body><main>Short</main></body></html>`,
			shouldFind: false,
			containsText: "",
		},
	}

	extractor := NewContentExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := extractor.extractMainContent(tt.html)

			if tt.shouldFind {
				if content == "" {
					t.Error("expected to find content but got empty string")
				}
				if tt.containsText != "" && !strings.Contains(content, tt.containsText) {
					t.Errorf("expected content to contain '%s', got: %s", tt.containsText, content)
				}
			} else {
				if content != "" {
					t.Errorf("expected no content but got: %s", content)
				}
			}
		})
	}
}

func TestContentExtractor_RemoveTag(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		tag      string
		expected string
	}{
		{
			name:     "remove script",
			html:     `<html><script>alert('test');</script><body>Content</body></html>`,
			tag:      "script",
			expected: `<html><body>Content</body></html>`,
		},
		{
			name:     "remove style",
			html:     `<html><style>body { color: red; }</style><body>Content</body></html>`,
			tag:      "style",
			expected: `<html><body>Content</body></html>`,
		},
		{
			name:     "remove nav",
			html:     `<html><nav>Menu</nav><body>Content</body></html>`,
			tag:      "nav",
			expected: `<html><body>Content</body></html>`,
		},
	}

	extractor := NewContentExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.removeTag(tt.html, tt.tag)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestContentExtractor_ExtractText(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		contains []string
		notContains []string
	}{
		{
			name: "paragraph text",
			html: `<div><p>First paragraph</p><p>Second paragraph</p></div>`,
			contains: []string{"First paragraph", "Second paragraph"},
			notContains: []string{"<p>", "</p>"},
		},
		{
			name: "heading text",
			html: `<div><h1>Title</h1><h2>Subtitle</h2><p>Content</p></div>`,
			contains: []string{"Title", "Subtitle", "Content"},
			notContains: []string{"<h1>", "<h2>"},
		},
		{
			name: "remove tags",
			html: `<div><span>Text with <strong>bold</strong> and <em>italic</em></span></div>`,
			contains: []string{"Text with bold and italic"},
			notContains: []string{"<strong>", "<em>"},
		},
	}

	extractor := NewContentExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := extractor.extractText(tt.html)

			for _, expected := range tt.contains {
				if !strings.Contains(text, expected) {
					t.Errorf("expected text to contain '%s', got: %s", expected, text)
				}
			}

			for _, unexpected := range tt.notContains {
				if strings.Contains(text, unexpected) {
					t.Errorf("expected text not to contain '%s', got: %s", unexpected, text)
				}
			}
		})
	}
}

func TestContentExtractor_CleanText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "HTML entities",
			input:    "Test &nbsp; &amp; &lt; &gt; &quot; &#39;",
			expected: "Test & < > \" '",
		},
		{
			name:     "multiple spaces",
			input:    "Text  with   multiple    spaces",
			expected: "Text with multiple spaces",
		},
		{
			name:     "multiple newlines",
			input:    "Line1\n\n\n\nLine2",
			expected: "Line1\n\nLine2",
		},
		{
			name:     "trim whitespace",
			input:    "  Text with leading and trailing spaces  ",
			expected: "Text with leading and trailing spaces",
		},
	}

	extractor := NewContentExtractor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.cleanText(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestContentExtractor_ExtractBatch(t *testing.T) {
	// Create multiple test servers
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Page 1</title></head><body><main><p>Content from page 1</p></main></body></html>`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Page 2</title></head><body><main><p>Content from page 2</p></main></body></html>`))
	}))
	defer server2.Close()

	server3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server3.Close()

	extractor := NewContentExtractor()
	urls := []string{server1.URL, server2.URL, server3.URL}

	results := extractor.ExtractBatch(context.Background(), urls)

	// Check that we got results for successful URLs
	if len(results) != 2 {
		t.Errorf("expected 2 successful extractions, got %d", len(results))
	}

	// Check content from server1
	content1, ok := results[server1.URL]
	if !ok {
		t.Error("expected result for server1")
	} else if !strings.Contains(content1, "Page 1") {
		t.Error("server1 content should contain 'Page 1'")
	}

	// Check content from server2
	content2, ok := results[server2.URL]
	if !ok {
		t.Error("expected result for server2")
	} else if !strings.Contains(content2, "Page 2") {
		t.Error("server2 content should contain 'Page 2'")
	}

	// Check that failed URL is not in results
	if _, ok := results[server3.URL]; ok {
		t.Error("should not have result for failed server3")
	}
}

func TestContentExtractor_LengthLimit(t *testing.T) {
	// Create very long content
	longContent := strings.Repeat("A", 15000)
	htmlContent := `<html><body><main><p>` + longContent + `</p></main></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	content, err := extractor.Extract(context.Background(), server.URL)

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Should be truncated to 10000 chars plus "..."
	if len(content) > 10100 {
		t.Errorf("content should be truncated to ~10000 chars, got %d", len(content))
	}

	if !strings.HasSuffix(content, "...") {
		t.Error("truncated content should end with '...'")
	}
}

func TestContentExtractor_RealWorldHTML(t *testing.T) {
	// Test with more realistic HTML structure
	htmlContent := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Real World Article</title>
    <meta name="description" content="An article about web scraping and content extraction">
    <meta property="og:title" content="Real World Article">
    <style>
        body { font-family: Arial; }
        .sidebar { display: none; }
    </style>
    <script>
        console.log("Analytics tracking");
    </script>
</head>
<body>
    <header>
        <nav>
            <ul>
                <li><a href="/">Home</a></li>
                <li><a href="/about">About</a></li>
            </ul>
        </nav>
    </header>

    <main>
        <article>
            <h1>Understanding Web Scraping</h1>

            <p>Web scraping is the process of extracting data from websites.
            It's a powerful technique used for data mining, research, and automation.</p>

            <h2>Why Content Extraction Matters</h2>

            <p>Content extraction helps focus on the main content of a page,
            removing navigation, ads, and other distractions. This is particularly
            useful for AI applications that need clean text input.</p>

            <h2>Best Practices</h2>

            <p>When implementing content extraction, consider:</p>
            <ul>
                <li>Respect robots.txt</li>
                <li>Rate limiting</li>
                <li>User agent identification</li>
            </ul>
        </article>
    </main>

    <aside class="sidebar">
        <h3>Related Articles</h3>
        <ul>
            <li>Article 1</li>
            <li>Article 2</li>
        </ul>
    </aside>

    <footer>
        <p>&copy; 2024 Example Corp</p>
    </footer>
</body>
</html>
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	content, err := extractor.Extract(context.Background(), server.URL)

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Should contain main content
	expectedPhrases := []string{
		"Real World Article",
		"Web scraping",
		"Content extraction",
		"Best Practices",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(content, phrase) {
			t.Errorf("content should contain '%s'", phrase)
		}
	}

	// Should not contain removed elements
	unexpectedPhrases := []string{
		"Analytics tracking",
		"console.log",
		"font-family",
		"Example Corp", // footer should be removed
	}

	for _, phrase := range unexpectedPhrases {
		if strings.Contains(content, phrase) {
			t.Errorf("content should not contain '%s'", phrase)
		}
	}
}
