// Package text provides a parser for plain text documents.
package text

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
}

func TestParser_Name(t *testing.T) {
	p := New()
	if name := p.Name(); name != "text" {
		t.Errorf("Name() = %q, want %q", name, "text")
	}
}

func TestParser_SupportedTypes(t *testing.T) {
	p := New()
	types := p.SupportedTypes()

	expected := []string{
		"text/plain",
		"text/csv",
		"text/tab-separated-values",
		"application/json",
		"application/xml",
		"text/xml",
	}

	if len(types) != len(expected) {
		t.Fatalf("SupportedTypes() returned %d types, want %d", len(types), len(expected))
	}

	for i, typ := range expected {
		if types[i] != typ {
			t.Errorf("SupportedTypes()[%d] = %q, want %q", i, types[i], typ)
		}
	}
}

func TestParser_SupportedExtensions(t *testing.T) {
	p := New()
	exts := p.SupportedExtensions()

	expected := []string{".txt", ".text", ".csv", ".tsv", ".json", ".xml", ".log"}
	if len(exts) != len(expected) {
		t.Fatalf("SupportedExtensions() returned %d extensions, want %d", len(exts), len(expected))
	}

	for i, ext := range expected {
		if exts[i] != ext {
			t.Errorf("SupportedExtensions()[%d] = %q, want %q", i, exts[i], ext)
		}
	}
}

func TestParser_Parse_BasicContent(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		expectedContent string
		expectedTitle   string
	}{
		{
			name:            "simple text",
			content:         "Hello, world!",
			expectedContent: "Hello, world!",
			expectedTitle:   "Hello, world!",
		},
		{
			name:            "multiline text",
			content:         "Line 1\nLine 2\nLine 3",
			expectedContent: "Line 1\nLine 2\nLine 3",
			expectedTitle:   "Line 1",
		},
		{
			name:            "empty content",
			content:         "",
			expectedContent: "",
			expectedTitle:   "",
		},
		{
			name:            "whitespace only",
			content:         "   \n\n   ",
			expectedContent: "",
			expectedTitle:   "",
		},
		{
			name:            "leading whitespace",
			content:         "\n\n  First actual line\nSecond line",
			expectedContent: "First actual line\nSecond line",
			expectedTitle:   "First actual line",
		},
		{
			name:            "trailing whitespace",
			content:         "Content here\n\n   ",
			expectedContent: "Content here",
			expectedTitle:   "Content here",
		},
	}

	p := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.content)
			result, err := p.Parse(context.Background(), reader, nil)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if result.Content != tt.expectedContent {
				t.Errorf("Parse() content = %q, want %q", result.Content, tt.expectedContent)
			}

			if result.Metadata.Title != tt.expectedTitle {
				t.Errorf("Parse() title = %q, want %q", result.Metadata.Title, tt.expectedTitle)
			}
		})
	}
}

func TestParser_Parse_TitleTruncation(t *testing.T) {
	// Test that long first lines are truncated for titles
	longLine := strings.Repeat("a", 150)
	content := longLine + "\nSecond line"

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Title should be truncated to 100 chars + "..."
	expectedTitle := strings.Repeat("a", 100) + "..."
	if result.Metadata.Title != expectedTitle {
		t.Errorf("title length = %d, want 103 (100 + '...')", len(result.Metadata.Title))
	}
}

func TestParser_Parse_MergeMetadata(t *testing.T) {
	content := "First line as title\n\nSome content"

	baseMeta := &models.DocumentMetadata{
		Title:  "Existing Title", // Should be preserved
		Author: "Test Author",
		Tags:   []string{"existing-tag"},
	}

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, baseMeta)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Title should remain from base (non-empty)
	if result.Metadata.Title != "Existing Title" {
		t.Errorf("title = %q, want 'Existing Title'", result.Metadata.Title)
	}
	// Author should remain from base
	if result.Metadata.Author != "Test Author" {
		t.Errorf("author = %q, want 'Test Author'", result.Metadata.Author)
	}
	// Tags should remain from base
	if len(result.Metadata.Tags) != 1 || result.Metadata.Tags[0] != "existing-tag" {
		t.Errorf("tags = %v, want ['existing-tag']", result.Metadata.Tags)
	}
}

func TestParser_Parse_Sections(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectedCount int
		checkSections func(t *testing.T, sections []parser.Section)
	}{
		{
			name:          "empty content",
			content:       "",
			expectedCount: 0,
		},
		{
			name:          "single paragraph",
			content:       "Just one paragraph with some text.",
			expectedCount: 1,
			checkSections: func(t *testing.T, sections []parser.Section) {
				if sections[0].Level != 1 {
					t.Errorf("section level = %d, want 1", sections[0].Level)
				}
				if sections[0].Content != "Just one paragraph with some text." {
					t.Errorf("section content = %q", sections[0].Content)
				}
			},
		},
		{
			name:          "multiple paragraphs",
			content:       "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.",
			expectedCount: 3,
		},
		{
			name:          "paragraph with multiple lines",
			content:       "Line 1\nLine 2\n\nNew paragraph",
			expectedCount: 2,
		},
		{
			name:          "whitespace only between paragraphs",
			content:       "Para 1\n\n   \n\nPara 2",
			expectedCount: 2,
		},
	}

	p := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.content)
			result, err := p.Parse(context.Background(), reader, nil)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if len(result.Sections) != tt.expectedCount {
				t.Errorf("got %d sections, want %d", len(result.Sections), tt.expectedCount)
			}

			if tt.checkSections != nil && len(result.Sections) == tt.expectedCount {
				tt.checkSections(t, result.Sections)
			}
		})
	}
}

func TestParser_Parse_SectionOffsets(t *testing.T) {
	content := "First paragraph.\n\nSecond paragraph."

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(result.Sections))
	}

	// First section should start at 0
	if result.Sections[0].StartOffset != 0 {
		t.Errorf("section[0].StartOffset = %d, want 0", result.Sections[0].StartOffset)
	}

	// First section end should be before second section start
	if result.Sections[0].EndOffset > result.Sections[1].StartOffset {
		t.Errorf("section[0].EndOffset (%d) should be <= section[1].StartOffset (%d)",
			result.Sections[0].EndOffset, result.Sections[1].StartOffset)
	}
}

func TestParser_Parse_SectionTitles(t *testing.T) {
	content := "Short title\n\nThis is a longer paragraph that should have its first line used as title but truncated if too long."

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(result.Sections))
	}

	// First section title should be the short content
	if result.Sections[0].Title != "Short title" {
		t.Errorf("section[0].Title = %q, want 'Short title'", result.Sections[0].Title)
	}

	// Second section title should be truncated
	if len(result.Sections[1].Title) > 53 { // 50 + "..."
		t.Errorf("section[1].Title should be truncated, got length %d", len(result.Sections[1].Title))
	}
}

func TestParser_Parse_ReadError(t *testing.T) {
	p := New()
	reader := &errorReader{err: io.ErrUnexpectedEOF}

	_, err := p.Parse(context.Background(), reader, nil)
	if err == nil {
		t.Error("expected error from reader, got nil")
	}
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

func TestExtractFirstLine(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "single line",
			content:  "Just one line",
			expected: "Just one line",
		},
		{
			name:     "multiple lines",
			content:  "First\nSecond\nThird",
			expected: "First",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "",
		},
		{
			name:     "leading empty lines",
			content:  "\n\nActual first",
			expected: "Actual first",
		},
		{
			name:     "whitespace line then content",
			content:  "   \nContent",
			expected: "Content",
		},
		{
			name:     "long first line",
			content:  strings.Repeat("x", 150),
			expected: strings.Repeat("x", 100) + "...",
		},
		{
			name:     "exactly 100 chars",
			content:  strings.Repeat("y", 100),
			expected: strings.Repeat("y", 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFirstLine(tt.content)
			if result != tt.expected {
				t.Errorf("extractFirstLine() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractParagraphSections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "empty",
			content:  "",
			expected: 0,
		},
		{
			name:     "whitespace only",
			content:  "   \n\n   ",
			expected: 0,
		},
		{
			name:     "single paragraph",
			content:  "Single paragraph here",
			expected: 1,
		},
		{
			name:     "two paragraphs",
			content:  "Para 1\n\nPara 2",
			expected: 2,
		},
		{
			name:     "multiple newlines",
			content:  "Para 1\n\n\n\nPara 2",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := extractParagraphSections(tt.content)
			if len(sections) != tt.expected {
				t.Errorf("extractParagraphSections() returned %d sections, want %d", len(sections), tt.expected)
			}
		})
	}
}

func TestSplitParagraphs(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "empty",
			content:  "",
			expected: 0,
		},
		{
			name:     "single paragraph",
			content:  "One paragraph",
			expected: 1,
		},
		{
			name:     "two paragraphs",
			content:  "Para 1\n\nPara 2",
			expected: 2,
		},
		{
			name:     "windows line endings",
			content:  "Para 1\r\n\r\nPara 2",
			expected: 2,
		},
		{
			name:     "mixed line endings",
			content:  "Para 1\r\n\nPara 2",
			expected: 2,
		},
		{
			name:     "empty paragraphs filtered",
			content:  "Para 1\n\n\n\n\nPara 2",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paragraphs := splitParagraphs(tt.content)
			if len(paragraphs) != tt.expected {
				t.Errorf("splitParagraphs() returned %d, want %d", len(paragraphs), tt.expected)
			}
		})
	}
}

func TestGetSectionTitle(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		index    int
		expected string
	}{
		{
			name:     "short content",
			content:  "Short",
			index:    1,
			expected: "Short",
		},
		{
			name:     "multiline content",
			content:  "First line\nSecond line",
			index:    1,
			expected: "First line",
		},
		{
			name:     "long first line",
			content:  strings.Repeat("z", 100) + "\nSecond",
			index:    1,
			expected: strings.Repeat("z", 50) + "...",
		},
		{
			name:     "empty content",
			content:  "",
			index:    1,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSectionTitle(tt.content, tt.index)
			if result != tt.expected {
				t.Errorf("getSectionTitle() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRegister(t *testing.T) {
	// Save original registry state
	originalDefault := parser.DefaultRegistry

	// Create a fresh registry for testing
	parser.DefaultRegistry = parser.NewRegistry()

	// Register the text parser
	Register()

	// Verify it was registered by type
	p, ok := parser.DefaultRegistry.GetByType("text/plain")
	if !ok {
		t.Error("text parser should be registered for text/plain")
	}
	if p.Name() != "text" {
		t.Errorf("registered parser name = %q, want 'text'", p.Name())
	}

	// Verify by extension
	p, ok = parser.DefaultRegistry.GetByExtension(".txt")
	if !ok {
		t.Error("text parser should be registered for .txt extension")
	}

	// Verify it's set as default
	defaultP, err := parser.DefaultRegistry.Get("unknown/type", ".unknown")
	if err != nil {
		t.Error("default parser should be set")
	}
	if defaultP.Name() != "text" {
		t.Errorf("default parser name = %q, want 'text'", defaultP.Name())
	}

	// Restore original registry
	parser.DefaultRegistry = originalDefault
}

func TestParser_Parse_CSVContent(t *testing.T) {
	content := "Name,Age,City\nAlice,30,New York\nBob,25,London"

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Content != content {
		t.Errorf("content mismatch for CSV")
	}

	// Title should be first line
	if result.Metadata.Title != "Name,Age,City" {
		t.Errorf("title = %q, want 'Name,Age,City'", result.Metadata.Title)
	}
}

func TestParser_Parse_JSONContent(t *testing.T) {
	content := `{"name": "test", "value": 123}`

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Content != content {
		t.Errorf("content mismatch for JSON")
	}
}

func TestParser_Parse_XMLContent(t *testing.T) {
	content := `<?xml version="1.0"?>
<root>
  <item>Value</item>
</root>`

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "<root>") {
		t.Error("content should contain XML tags")
	}
}

func TestParser_Parse_LogContent(t *testing.T) {
	content := `2024-01-15 10:30:00 INFO Starting application
2024-01-15 10:30:01 DEBUG Initializing database
2024-01-15 10:30:02 ERROR Connection failed`

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "ERROR Connection failed") {
		t.Error("content should contain log entries")
	}
}

func TestParser_Parse_NilMetadata(t *testing.T) {
	p := New()
	reader := strings.NewReader("Some content")

	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Metadata == nil {
		t.Error("Metadata should not be nil")
	}
}

func TestParser_Parse_EmptyMetadata(t *testing.T) {
	p := New()
	reader := strings.NewReader("Content with title")

	result, err := p.Parse(context.Background(), reader, &models.DocumentMetadata{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Title should be extracted since base was empty
	if result.Metadata.Title != "Content with title" {
		t.Errorf("title = %q, want 'Content with title'", result.Metadata.Title)
	}
}

func TestParser_Parse_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	p := New()
	reader := strings.NewReader("Content")

	// The parser doesn't check context during reading,
	// so this should still succeed (io.ReadAll doesn't respect context)
	result, err := p.Parse(ctx, reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v (context cancellation not checked during read)", err)
	}
	if result.Content != "Content" {
		t.Errorf("content = %q, want 'Content'", result.Content)
	}
}

// Benchmark tests
func BenchmarkParse_Simple(b *testing.B) {
	p := New()
	content := "Simple text content for parsing."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(content)
		_, _ = p.Parse(context.Background(), reader, nil)
	}
}

func BenchmarkParse_MultipleParagraphs(b *testing.B) {
	p := New()
	content := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.\n\nFourth paragraph."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(content)
		_, _ = p.Parse(context.Background(), reader, nil)
	}
}

func BenchmarkParse_LargeDocument(b *testing.B) {
	p := New()

	// Generate a larger document
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("This is paragraph ")
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString(" with some content that makes it longer.\n\n")
	}
	content := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(content)
		_, _ = p.Parse(context.Background(), reader, nil)
	}
}

func BenchmarkExtractFirstLine(b *testing.B) {
	content := "First line here\nSecond line\nThird line"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractFirstLine(content)
	}
}

func BenchmarkExtractParagraphSections(b *testing.B) {
	content := "Para 1\n\nPara 2\n\nPara 3\n\nPara 4\n\nPara 5"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractParagraphSections(content)
	}
}

func BenchmarkSplitParagraphs(b *testing.B) {
	content := "Para 1\n\nPara 2\n\nPara 3\n\nPara 4\n\nPara 5"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		splitParagraphs(content)
	}
}
