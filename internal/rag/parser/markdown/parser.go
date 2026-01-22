// Package markdown provides a parser for Markdown documents with frontmatter support.
package markdown

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"

	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/pkg/models"
	"gopkg.in/yaml.v3"
)

// Parser parses Markdown documents, extracting content, frontmatter, and structure.
type Parser struct{}

// New creates a new Markdown parser.
func New() *Parser {
	return &Parser{}
}

// Name returns the parser name.
func (p *Parser) Name() string {
	return "markdown"
}

// SupportedTypes returns the MIME types this parser handles.
func (p *Parser) SupportedTypes() []string {
	return []string{
		"text/markdown",
		"text/x-markdown",
	}
}

// SupportedExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedExtensions() []string {
	return []string{".md", ".markdown", ".mdown", ".mkd"}
}

// Parse extracts content and metadata from a Markdown document.
func (p *Parser) Parse(ctx context.Context, reader io.Reader, docMeta *models.DocumentMetadata) (*parser.ParseResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	content := string(data)
	var extractedMeta *models.DocumentMetadata

	// Extract frontmatter if present
	frontmatter, body := extractFrontmatter(content)
	if frontmatter != "" {
		if meta, err := parseFrontmatter(frontmatter); err == nil {
			extractedMeta = meta
		}
	}
	content = body

	// If no title in frontmatter, try to extract from first heading
	if extractedMeta == nil {
		extractedMeta = &models.DocumentMetadata{}
	}
	if extractedMeta.Title == "" {
		extractedMeta.Title = extractFirstHeading(content)
	}

	// Extract sections for structure-aware processing
	sections := extractSections(content)

	// Merge extracted metadata with provided metadata
	mergedMeta := parser.MergeMeta(docMeta, extractedMeta)

	return &parser.ParseResult{
		Content:  strings.TrimSpace(content),
		Metadata: mergedMeta,
		Sections: sections,
	}, nil
}

// extractFrontmatter separates YAML frontmatter from content.
// Frontmatter must be at the start of the document, delimited by "---".
func extractFrontmatter(content string) (frontmatter, body string) {
	content = strings.TrimSpace(content)

	// Check for frontmatter delimiter
	if !strings.HasPrefix(content, "---") {
		return "", content
	}

	// Find the closing delimiter
	lines := strings.SplitN(content, "\n", -1)
	if len(lines) < 3 {
		return "", content
	}

	endIndex := -1
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" || trimmed == "..." {
			endIndex = i
			break
		}
	}

	if endIndex == -1 {
		return "", content
	}

	frontmatterLines := lines[1:endIndex]
	bodyLines := lines[endIndex+1:]

	return strings.Join(frontmatterLines, "\n"), strings.Join(bodyLines, "\n")
}

// frontmatterData represents the structure of YAML frontmatter.
type frontmatterData struct {
	Title       string   `yaml:"title"`
	Author      string   `yaml:"author"`
	Description string   `yaml:"description"`
	Summary     string   `yaml:"summary"`
	Tags        []string `yaml:"tags"`
	Keywords    []string `yaml:"keywords"`
	Language    string   `yaml:"language"`
	Lang        string   `yaml:"lang"`
	Date        string   `yaml:"date"`
}

// parseFrontmatter parses YAML frontmatter into DocumentMetadata.
func parseFrontmatter(fm string) (*models.DocumentMetadata, error) {
	var data frontmatterData
	if err := yaml.Unmarshal([]byte(fm), &data); err != nil {
		return nil, err
	}

	meta := &models.DocumentMetadata{
		Title:       data.Title,
		Author:      data.Author,
		Description: data.Description,
	}

	// Use summary as description if description is empty
	if meta.Description == "" && data.Summary != "" {
		meta.Description = data.Summary
	}

	// Merge tags and keywords
	tags := make([]string, 0, len(data.Tags)+len(data.Keywords))
	tags = append(tags, data.Tags...)
	tags = append(tags, data.Keywords...)
	if len(tags) > 0 {
		meta.Tags = tags
	}

	// Use either language or lang field
	if data.Language != "" {
		meta.Language = data.Language
	} else if data.Lang != "" {
		meta.Language = data.Lang
	}

	// Store additional fields in Custom
	if data.Date != "" {
		if meta.Custom == nil {
			meta.Custom = make(map[string]any)
		}
		meta.Custom["date"] = data.Date
	}

	return meta, nil
}

// headingRegex matches Markdown headings (# style).
var headingRegex = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)

// extractFirstHeading finds the first heading in the content.
func extractFirstHeading(content string) string {
	scanner := bufio.NewScanner(bytes.NewBufferString(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if matches := headingRegex.FindStringSubmatch(line); len(matches) == 3 {
			return strings.TrimSpace(matches[2])
		}
	}
	return ""
}

// extractSections identifies logical sections based on headings.
func extractSections(content string) []parser.Section {
	var sections []parser.Section
	var currentSection *parser.Section
	var currentContent strings.Builder
	offset := 0

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineLen := len(line) + 1 // +1 for newline
		if i == len(lines)-1 {
			lineLen = len(line) // no newline at end
		}

		if matches := headingRegex.FindStringSubmatch(strings.TrimSpace(line)); len(matches) == 3 {
			// Close previous section
			if currentSection != nil {
				currentSection.Content = strings.TrimSpace(currentContent.String())
				currentSection.EndOffset = offset
				sections = append(sections, *currentSection)
				currentContent.Reset()
			}

			// Start new section
			level := len(matches[1])
			title := strings.TrimSpace(matches[2])
			currentSection = &parser.Section{
				Title:       title,
				Level:       level,
				StartOffset: offset,
			}
		} else if currentSection != nil {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}

		offset += lineLen
	}

	// Close final section
	if currentSection != nil {
		currentSection.Content = strings.TrimSpace(currentContent.String())
		currentSection.EndOffset = offset
		sections = append(sections, *currentSection)
	}

	return sections
}

// Register registers the Markdown parser with the default registry.
func Register() {
	parser.DefaultRegistry.Register(New())
}
