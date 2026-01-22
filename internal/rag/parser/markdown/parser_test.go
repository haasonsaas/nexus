// Package markdown provides a parser for Markdown documents with frontmatter support.
package markdown

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
	if name := p.Name(); name != "markdown" {
		t.Errorf("Name() = %q, want %q", name, "markdown")
	}
}

func TestParser_SupportedTypes(t *testing.T) {
	p := New()
	types := p.SupportedTypes()

	expected := []string{"text/markdown", "text/x-markdown"}
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

	expected := []string{".md", ".markdown", ".mdown", ".mkd"}
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
			expectedTitle:   "",
		},
		{
			name:            "text with heading",
			content:         "# My Title\n\nSome content here.",
			expectedContent: "# My Title\n\nSome content here.",
			expectedTitle:   "My Title",
		},
		{
			name:            "multiple headings",
			content:         "# First\n\nText\n\n## Second\n\nMore text",
			expectedContent: "# First\n\nText\n\n## Second\n\nMore text",
			expectedTitle:   "First",
		},
		{
			name:            "heading with extra spaces",
			content:         "#   Spaced Title   \n\nContent",
			expectedContent: "#   Spaced Title   \n\nContent",
			expectedTitle:   "Spaced Title",
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

func TestParser_Parse_Frontmatter(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		expectTitle  string
		expectAuthor string
		expectDesc   string
		expectTags   []string
		expectLang   string
	}{
		{
			name: "basic frontmatter",
			content: `---
title: My Document
author: John Doe
---
Content here`,
			expectTitle:  "My Document",
			expectAuthor: "John Doe",
		},
		{
			name: "frontmatter with description",
			content: `---
title: Test
description: A test document
---
Content`,
			expectTitle: "Test",
			expectDesc:  "A test document",
		},
		{
			name: "frontmatter with summary (fallback to description)",
			content: `---
title: Test
summary: A summary
---
Content`,
			expectTitle: "Test",
			expectDesc:  "A summary",
		},
		{
			name: "frontmatter with tags",
			content: `---
title: Tagged
tags:
  - go
  - testing
---
Content`,
			expectTitle: "Tagged",
			expectTags:  []string{"go", "testing"},
		},
		{
			name: "frontmatter with keywords (merged with tags)",
			content: `---
title: Keywords
tags:
  - tag1
keywords:
  - keyword1
---
Content`,
			expectTitle: "Keywords",
			expectTags:  []string{"tag1", "keyword1"},
		},
		{
			name: "frontmatter with language",
			content: `---
title: Localized
language: en
---
Content`,
			expectTitle: "Localized",
			expectLang:  "en",
		},
		{
			name: "frontmatter with lang (alternative field)",
			content: `---
title: Localized Alt
lang: fr
---
Content`,
			expectTitle: "Localized Alt",
			expectLang:  "fr",
		},
		{
			name: "frontmatter closed with ...",
			content: `---
title: Dots Delimiter
...
Content`,
			expectTitle: "Dots Delimiter",
		},
		{
			name: "no frontmatter, title from heading",
			content: `# Heading Title

Content here`,
			expectTitle: "Heading Title",
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

			if result.Metadata.Title != tt.expectTitle {
				t.Errorf("title = %q, want %q", result.Metadata.Title, tt.expectTitle)
			}
			if result.Metadata.Author != tt.expectAuthor {
				t.Errorf("author = %q, want %q", result.Metadata.Author, tt.expectAuthor)
			}
			if result.Metadata.Description != tt.expectDesc {
				t.Errorf("description = %q, want %q", result.Metadata.Description, tt.expectDesc)
			}
			if tt.expectLang != "" && result.Metadata.Language != tt.expectLang {
				t.Errorf("language = %q, want %q", result.Metadata.Language, tt.expectLang)
			}
			if tt.expectTags != nil {
				if len(result.Metadata.Tags) != len(tt.expectTags) {
					t.Errorf("tags length = %d, want %d", len(result.Metadata.Tags), len(tt.expectTags))
				}
				for i, tag := range tt.expectTags {
					if i < len(result.Metadata.Tags) && result.Metadata.Tags[i] != tag {
						t.Errorf("tags[%d] = %q, want %q", i, result.Metadata.Tags[i], tag)
					}
				}
			}
		})
	}
}

func TestParser_Parse_FrontmatterWithDate(t *testing.T) {
	content := `---
title: Dated Document
date: 2024-01-15
---
Content`

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Metadata.Custom == nil {
		t.Fatal("expected Custom metadata to be set")
	}
	if date, ok := result.Metadata.Custom["date"]; !ok || date != "2024-01-15" {
		t.Errorf("Custom['date'] = %v, want '2024-01-15'", date)
	}
}

func TestParser_Parse_MergeMetadata(t *testing.T) {
	content := `---
title: From Frontmatter
---
Content`

	baseMeta := &models.DocumentMetadata{
		Title:  "", // Should be overwritten
		Author: "Base Author",
		Tags:   []string{"base-tag"},
	}

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, baseMeta)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Title should come from frontmatter since base is empty
	if result.Metadata.Title != "From Frontmatter" {
		t.Errorf("title = %q, want 'From Frontmatter'", result.Metadata.Title)
	}
	// Author should remain from base
	if result.Metadata.Author != "Base Author" {
		t.Errorf("author = %q, want 'Base Author'", result.Metadata.Author)
	}
	// Tags should remain from base (non-empty)
	if len(result.Metadata.Tags) != 1 || result.Metadata.Tags[0] != "base-tag" {
		t.Errorf("tags = %v, want ['base-tag']", result.Metadata.Tags)
	}
}

func TestParser_Parse_Sections(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		expectedCount  int
		checkSections  func(t *testing.T, sections []parser.Section)
	}{
		{
			name:          "no headings",
			content:       "Just some text without headings.",
			expectedCount: 0,
		},
		{
			name:          "single heading",
			content:       "# Introduction\n\nThis is the intro.",
			expectedCount: 1,
			checkSections: func(t *testing.T, sections []parser.Section) {
				if sections[0].Title != "Introduction" {
					t.Errorf("section title = %q, want 'Introduction'", sections[0].Title)
				}
				if sections[0].Level != 1 {
					t.Errorf("section level = %d, want 1", sections[0].Level)
				}
			},
		},
		{
			name: "multiple headings",
			content: `# First Section

Content for first section.

## Subsection

Subsection content.

# Second Section

Content for second section.`,
			expectedCount: 3,
			checkSections: func(t *testing.T, sections []parser.Section) {
				expected := []struct {
					title string
					level int
				}{
					{"First Section", 1},
					{"Subsection", 2},
					{"Second Section", 1},
				}
				for i, exp := range expected {
					if sections[i].Title != exp.title {
						t.Errorf("section[%d].Title = %q, want %q", i, sections[i].Title, exp.title)
					}
					if sections[i].Level != exp.level {
						t.Errorf("section[%d].Level = %d, want %d", i, sections[i].Level, exp.level)
					}
				}
			},
		},
		{
			name: "all heading levels",
			content: `# H1
## H2
### H3
#### H4
##### H5
###### H6`,
			expectedCount: 6,
			checkSections: func(t *testing.T, sections []parser.Section) {
				for i := 0; i < 6; i++ {
					if sections[i].Level != i+1 {
						t.Errorf("section[%d].Level = %d, want %d", i, sections[i].Level, i+1)
					}
				}
			},
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
	content := "# Section A\n\nContent A\n\n# Section B\n\nContent B"

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

	// Second section should start at or after first section ends
	if result.Sections[1].StartOffset < result.Sections[0].EndOffset {
		t.Errorf("section[1].StartOffset (%d) should be >= section[0].EndOffset (%d)",
			result.Sections[1].StartOffset, result.Sections[0].EndOffset)
	}

	// Sections should have content
	if result.Sections[0].Content == "" {
		t.Error("section[0].Content should not be empty")
	}
	if result.Sections[1].Content == "" {
		t.Error("section[1].Content should not be empty")
	}
}

func TestExtractFrontmatter(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectFM      string
		expectBody    string
	}{
		{
			name:       "no frontmatter",
			content:    "Just regular content",
			expectFM:   "",
			expectBody: "Just regular content",
		},
		{
			name:       "with frontmatter",
			content:    "---\ntitle: Test\n---\nBody content",
			expectFM:   "title: Test",
			expectBody: "Body content",
		},
		{
			name:       "frontmatter with dots closing",
			content:    "---\ntitle: Test\n...\nBody content",
			expectFM:   "title: Test",
			expectBody: "Body content",
		},
		{
			name:       "no closing delimiter",
			content:    "---\ntitle: Test\nNo closing",
			expectFM:   "",
			expectBody: "---\ntitle: Test\nNo closing",
		},
		{
			name:       "frontmatter not at start",
			content:    "content\n---\ntitle: Test\n---\nmore",
			expectFM:   "",
			expectBody: "content\n---\ntitle: Test\n---\nmore",
		},
		{
			name:       "empty frontmatter",
			content:    "---\n---\nBody",
			expectFM:   "",
			expectBody: "Body",
		},
		{
			name:       "whitespace before frontmatter gets trimmed",
			content:    "  \n---\ntitle: Test\n---\nBody",
			expectFM:   "title: Test",
			expectBody: "Body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body := extractFrontmatter(tt.content)
			if fm != tt.expectFM {
				t.Errorf("frontmatter = %q, want %q", fm, tt.expectFM)
			}
			if body != tt.expectBody {
				t.Errorf("body = %q, want %q", body, tt.expectBody)
			}
		})
	}
}

func TestExtractFirstHeading(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "no heading",
			content:  "Just text",
			expected: "",
		},
		{
			name:     "h1 heading",
			content:  "# My Title",
			expected: "My Title",
		},
		{
			name:     "h2 heading first",
			content:  "## Second Level\n# First Level",
			expected: "Second Level",
		},
		{
			name:     "heading with extra hashes",
			content:  "### Third Level Title",
			expected: "Third Level Title",
		},
		{
			name:     "heading after text",
			content:  "Some text\n# Later Heading",
			expected: "Later Heading",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "",
		},
		{
			name:     "only hash no text",
			content:  "#",
			expected: "",
		},
		{
			name:     "hash without space (not valid heading)",
			content:  "#NoSpace",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFirstHeading(tt.content)
			if result != tt.expected {
				t.Errorf("extractFirstHeading() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractSections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "empty content",
			content:  "",
			expected: 0,
		},
		{
			name:     "no headings",
			content:  "Just some text",
			expected: 0,
		},
		{
			name:     "one heading",
			content:  "# Title\nContent",
			expected: 1,
		},
		{
			name:     "multiple headings",
			content:  "# A\n## B\n### C",
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := extractSections(tt.content)
			if len(sections) != tt.expected {
				t.Errorf("extractSections() returned %d sections, want %d", len(sections), tt.expected)
			}
		})
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		wantErr     bool
		checkMeta   func(t *testing.T, meta *models.DocumentMetadata)
	}{
		{
			name:        "valid yaml",
			frontmatter: "title: Test\nauthor: John",
			wantErr:     false,
			checkMeta: func(t *testing.T, meta *models.DocumentMetadata) {
				if meta.Title != "Test" {
					t.Errorf("title = %q, want 'Test'", meta.Title)
				}
				if meta.Author != "John" {
					t.Errorf("author = %q, want 'John'", meta.Author)
				}
			},
		},
		{
			name:        "invalid yaml",
			frontmatter: "invalid: [unclosed",
			wantErr:     true,
		},
		{
			name:        "empty frontmatter",
			frontmatter: "",
			wantErr:     false,
		},
		{
			name:        "language field",
			frontmatter: "language: en",
			wantErr:     false,
			checkMeta: func(t *testing.T, meta *models.DocumentMetadata) {
				if meta.Language != "en" {
					t.Errorf("language = %q, want 'en'", meta.Language)
				}
			},
		},
		{
			name:        "lang field (alternative)",
			frontmatter: "lang: de",
			wantErr:     false,
			checkMeta: func(t *testing.T, meta *models.DocumentMetadata) {
				if meta.Language != "de" {
					t.Errorf("language = %q, want 'de'", meta.Language)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := parseFrontmatter(tt.frontmatter)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFrontmatter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkMeta != nil {
				tt.checkMeta(t, meta)
			}
		})
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

func TestParser_Parse_CodeBlocks(t *testing.T) {
	content := "# Code Example\n\n```go\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n```\n\nText after code."

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Content should include the code block
	if !strings.Contains(result.Content, "```go") {
		t.Error("content should include code fence")
	}
	if !strings.Contains(result.Content, "func main()") {
		t.Error("content should include code")
	}
}

func TestParser_Parse_Lists(t *testing.T) {
	content := `# Lists

- Item 1
- Item 2
  - Nested item

1. Numbered 1
2. Numbered 2`

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "- Item 1") {
		t.Error("content should include unordered list")
	}
	if !strings.Contains(result.Content, "1. Numbered 1") {
		t.Error("content should include ordered list")
	}
}

func TestParser_Parse_Links(t *testing.T) {
	content := `# Links

[Example Link](https://example.com)
[Reference Link][ref]

[ref]: https://example.org`

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "[Example Link](https://example.com)") {
		t.Error("content should include inline link")
	}
}

func TestParser_Parse_Images(t *testing.T) {
	content := "# Image\n\n![Alt text](image.png)"

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "![Alt text](image.png)") {
		t.Error("content should include image")
	}
}

func TestParser_Parse_Blockquotes(t *testing.T) {
	content := "# Quote\n\n> This is a blockquote\n> With multiple lines"

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "> This is a blockquote") {
		t.Error("content should include blockquote")
	}
}

func TestParser_Parse_HorizontalRules(t *testing.T) {
	content := "# Section 1\n\n---\n\n# Section 2"

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "---") {
		t.Error("content should include horizontal rule")
	}
}

func TestParser_Parse_InlineFormatting(t *testing.T) {
	content := "# Formatting\n\n**bold** *italic* `code` ~~strikethrough~~"

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "**bold**") {
		t.Error("content should include bold")
	}
	if !strings.Contains(result.Content, "*italic*") {
		t.Error("content should include italic")
	}
	if !strings.Contains(result.Content, "`code`") {
		t.Error("content should include inline code")
	}
}

func TestParser_Parse_Tables(t *testing.T) {
	content := `# Table

| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |`

	p := New()
	reader := strings.NewReader(content)
	result, err := p.Parse(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(result.Content, "| Header 1 |") {
		t.Error("content should include table")
	}
}

func TestRegister(t *testing.T) {
	// Save original registry state
	originalDefault := parser.DefaultRegistry

	// Create a fresh registry for testing
	parser.DefaultRegistry = parser.NewRegistry()

	// Register the markdown parser
	Register()

	// Verify it was registered by type
	p, ok := parser.DefaultRegistry.GetByType("text/markdown")
	if !ok {
		t.Error("markdown parser should be registered for text/markdown")
	}
	if p.Name() != "markdown" {
		t.Errorf("registered parser name = %q, want 'markdown'", p.Name())
	}

	// Verify by extension
	p, ok = parser.DefaultRegistry.GetByExtension(".md")
	if !ok {
		t.Error("markdown parser should be registered for .md extension")
	}

	// Restore original registry
	parser.DefaultRegistry = originalDefault
}

// Benchmark tests
func BenchmarkParse_Simple(b *testing.B) {
	p := New()
	content := "# Title\n\nSimple content with some text."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(content)
		_, _ = p.Parse(context.Background(), reader, nil)
	}
}

func BenchmarkParse_WithFrontmatter(b *testing.B) {
	p := New()
	content := `---
title: Benchmark Document
author: Test Author
tags:
  - benchmark
  - test
---

# Content

Some text here.`

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
	sb.WriteString("---\ntitle: Large Document\n---\n\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("## Section ")
		sb.WriteString(string(rune('A' + i%26)))
		sb.WriteString("\n\n")
		sb.WriteString("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ")
		sb.WriteString("Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.\n\n")
	}
	content := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(content)
		_, _ = p.Parse(context.Background(), reader, nil)
	}
}

func BenchmarkExtractFrontmatter(b *testing.B) {
	content := `---
title: Benchmark
author: Test
tags:
  - one
  - two
---
Content here`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractFrontmatter(content)
	}
}

func BenchmarkExtractSections(b *testing.B) {
	content := `# Section 1

Content for section 1.

## Subsection 1.1

More content.

# Section 2

Content for section 2.`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractSections(content)
	}
}
