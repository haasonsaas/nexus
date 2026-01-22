// Package parser provides document parsing interfaces and implementations
// for the RAG (Retrieval-Augmented Generation) system.
package parser

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Parser defines the interface for document parsers.
// Parsers extract text content and metadata from various document formats.
type Parser interface {
	// Parse extracts content and metadata from a document.
	// The reader provides the raw document bytes.
	// Metadata from the document (e.g., frontmatter) is merged with docMeta.
	Parse(ctx context.Context, reader io.Reader, docMeta *models.DocumentMetadata) (*ParseResult, error)

	// Name returns the parser name for logging and debugging.
	Name() string

	// SupportedTypes returns the MIME types this parser can handle.
	SupportedTypes() []string

	// SupportedExtensions returns the file extensions this parser can handle.
	SupportedExtensions() []string
}

// ParseResult contains the output of a parsing operation.
type ParseResult struct {
	// Content is the extracted text content.
	Content string

	// Metadata contains extracted and merged metadata.
	Metadata *models.DocumentMetadata

	// Sections contains identified document sections (for structure-aware chunking).
	Sections []Section
}

// Section represents a logical section of a document.
type Section struct {
	// Title is the section heading.
	Title string

	// Level is the heading level (1-6 for markdown).
	Level int

	// Content is the section content.
	Content string

	// StartOffset is the character offset where this section starts.
	StartOffset int

	// EndOffset is the character offset where this section ends.
	EndOffset int
}

// Registry manages available parsers.
type Registry struct {
	mu               sync.RWMutex
	parsersByType    map[string]Parser
	parsersByExt     map[string]Parser
	defaultParser    Parser
}

// NewRegistry creates a new parser registry.
func NewRegistry() *Registry {
	return &Registry{
		parsersByType: make(map[string]Parser),
		parsersByExt:  make(map[string]Parser),
	}
}

// Register adds a parser to the registry.
// The parser is registered for all its supported types and extensions.
func (r *Registry) Register(parser Parser) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, mimeType := range parser.SupportedTypes() {
		r.parsersByType[strings.ToLower(mimeType)] = parser
	}

	for _, ext := range parser.SupportedExtensions() {
		ext = strings.ToLower(strings.TrimPrefix(ext, "."))
		r.parsersByExt[ext] = parser
	}
}

// SetDefault sets the default parser used when no specific parser matches.
func (r *Registry) SetDefault(parser Parser) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultParser = parser
}

// GetByType returns the parser for a given MIME type.
func (r *Registry) GetByType(mimeType string) (Parser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Normalize MIME type (remove parameters like charset)
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	mimeType = strings.ToLower(mimeType)

	parser, ok := r.parsersByType[mimeType]
	return parser, ok
}

// GetByExtension returns the parser for a given file extension.
func (r *Registry) GetByExtension(ext string) (Parser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	parser, ok := r.parsersByExt[ext]
	return parser, ok
}

// Get returns the best parser for the given content type and extension.
// It first tries to match by content type, then by extension, then falls back to default.
func (r *Registry) Get(contentType, ext string) (Parser, error) {
	// Try content type first
	if contentType != "" {
		if parser, ok := r.GetByType(contentType); ok {
			return parser, nil
		}
	}

	// Try extension
	if ext != "" {
		if parser, ok := r.GetByExtension(ext); ok {
			return parser, nil
		}
	}

	// Fall back to default
	r.mu.RLock()
	defaultParser := r.defaultParser
	r.mu.RUnlock()

	if defaultParser != nil {
		return defaultParser, nil
	}

	return nil, fmt.Errorf("no parser found for content type %q, extension %q", contentType, ext)
}

// DefaultRegistry is a pre-configured registry with common parsers.
var DefaultRegistry = NewRegistry()

// Parse is a convenience function that uses the default registry.
func Parse(ctx context.Context, reader io.Reader, contentType, ext string, docMeta *models.DocumentMetadata) (*ParseResult, error) {
	parser, err := DefaultRegistry.Get(contentType, ext)
	if err != nil {
		return nil, err
	}
	return parser.Parse(ctx, reader, docMeta)
}

// MergeMeta merges extracted metadata into the base metadata.
// Extracted values only override if the base value is empty.
func MergeMeta(base *models.DocumentMetadata, extracted *models.DocumentMetadata) *models.DocumentMetadata {
	if base == nil {
		base = &models.DocumentMetadata{}
	}
	if extracted == nil {
		return base
	}

	result := *base

	if result.Title == "" && extracted.Title != "" {
		result.Title = extracted.Title
	}
	if result.Author == "" && extracted.Author != "" {
		result.Author = extracted.Author
	}
	if result.Description == "" && extracted.Description != "" {
		result.Description = extracted.Description
	}
	if result.Language == "" && extracted.Language != "" {
		result.Language = extracted.Language
	}
	if len(result.Tags) == 0 && len(extracted.Tags) > 0 {
		result.Tags = extracted.Tags
	}

	// Merge custom fields
	if len(extracted.Custom) > 0 {
		if result.Custom == nil {
			result.Custom = make(map[string]any)
		}
		for k, v := range extracted.Custom {
			if _, exists := result.Custom[k]; !exists {
				result.Custom[k] = v
			}
		}
	}

	return &result
}
