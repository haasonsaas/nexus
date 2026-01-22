// Package text provides a parser for plain text documents.
package text

import (
	"bufio"
	"context"
	"io"
	"strings"

	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Parser parses plain text documents.
type Parser struct{}

// New creates a new plain text parser.
func New() *Parser {
	return &Parser{}
}

// Name returns the parser name.
func (p *Parser) Name() string {
	return "text"
}

// SupportedTypes returns the MIME types this parser handles.
func (p *Parser) SupportedTypes() []string {
	return []string{
		"text/plain",
		"text/csv",
		"text/tab-separated-values",
		"application/json",
		"application/xml",
		"text/xml",
	}
}

// SupportedExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedExtensions() []string {
	return []string{".txt", ".text", ".csv", ".tsv", ".json", ".xml", ".log"}
}

// Parse extracts content from a plain text document.
func (p *Parser) Parse(ctx context.Context, reader io.Reader, docMeta *models.DocumentMetadata) (*parser.ParseResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	content := string(data)

	// Try to extract a title from the first non-empty line
	extractedMeta := &models.DocumentMetadata{}
	extractedMeta.Title = extractFirstLine(content)

	// Extract sections based on paragraph breaks
	sections := extractParagraphSections(content)

	// Merge metadata
	mergedMeta := parser.MergeMeta(docMeta, extractedMeta)

	return &parser.ParseResult{
		Content:  strings.TrimSpace(content),
		Metadata: mergedMeta,
		Sections: sections,
	}, nil
}

// extractFirstLine gets the first non-empty line as a potential title.
func extractFirstLine(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			// Cap title length
			if len(line) > 100 {
				return line[:100] + "..."
			}
			return line
		}
	}
	return ""
}

// extractParagraphSections splits content into paragraph-based sections.
// This provides basic structure for documents without explicit headings.
func extractParagraphSections(content string) []parser.Section {
	var sections []parser.Section

	paragraphs := splitParagraphs(content)
	offset := 0

	for i, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// Find actual offset in content
		idx := strings.Index(content[offset:], para)
		if idx >= 0 {
			startOffset := offset + idx
			endOffset := startOffset + len(para)

			section := parser.Section{
				Title:       getSectionTitle(para, i+1),
				Level:       1,
				Content:     para,
				StartOffset: startOffset,
				EndOffset:   endOffset,
			}
			sections = append(sections, section)
			offset = endOffset
		}
	}

	return sections
}

// splitParagraphs splits content by double newlines.
func splitParagraphs(content string) []string {
	// Normalize line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")

	// Split on double newlines
	paragraphs := strings.Split(content, "\n\n")

	result := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// getSectionTitle creates a title for a paragraph section.
func getSectionTitle(content string, index int) string {
	// Use first line or first 50 chars as title
	firstLine := ""
	if idx := strings.Index(content, "\n"); idx > 0 {
		firstLine = strings.TrimSpace(content[:idx])
	} else {
		firstLine = content
	}

	if len(firstLine) > 50 {
		firstLine = firstLine[:50] + "..."
	}

	if firstLine == "" {
		return ""
	}
	return firstLine
}

// Register registers the text parser with the default registry.
// It also sets itself as the default parser for unknown types.
func Register() {
	p := New()
	parser.DefaultRegistry.Register(p)
	parser.DefaultRegistry.SetDefault(p)
}
