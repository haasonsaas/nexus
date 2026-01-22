// Package parser provides document parsing interfaces and implementations
// for the RAG (Retrieval-Augmented Generation) system.
package parser

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

// MockParser implements Parser for testing
type MockParser struct {
	name       string
	types      []string
	extensions []string
	parseFunc  func(ctx context.Context, reader io.Reader, docMeta *models.DocumentMetadata) (*ParseResult, error)
}

func (m *MockParser) Name() string {
	return m.name
}

func (m *MockParser) SupportedTypes() []string {
	return m.types
}

func (m *MockParser) SupportedExtensions() []string {
	return m.extensions
}

func (m *MockParser) Parse(ctx context.Context, reader io.Reader, docMeta *models.DocumentMetadata) (*ParseResult, error) {
	if m.parseFunc != nil {
		return m.parseFunc(ctx, reader, docMeta)
	}
	return &ParseResult{Content: "mock content"}, nil
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if r.parsersByType == nil {
		t.Error("parsersByType map should be initialized")
	}
	if r.parsersByExt == nil {
		t.Error("parsersByExt map should be initialized")
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	parser := &MockParser{
		name:       "test",
		types:      []string{"text/test", "application/test"},
		extensions: []string{".test", ".tst"},
	}

	r.Register(parser)

	// Check by type
	if p, ok := r.GetByType("text/test"); !ok || p.Name() != "test" {
		t.Error("parser should be registered for text/test")
	}
	if p, ok := r.GetByType("application/test"); !ok || p.Name() != "test" {
		t.Error("parser should be registered for application/test")
	}

	// Check by extension
	if p, ok := r.GetByExtension(".test"); !ok || p.Name() != "test" {
		t.Error("parser should be registered for .test")
	}
	if p, ok := r.GetByExtension(".tst"); !ok || p.Name() != "test" {
		t.Error("parser should be registered for .tst")
	}
}

func TestRegistry_Register_CaseInsensitive(t *testing.T) {
	r := NewRegistry()

	parser := &MockParser{
		name:       "test",
		types:      []string{"TEXT/TEST"},
		extensions: []string{".TEST"},
	}

	r.Register(parser)

	// Should find with lowercase
	if p, ok := r.GetByType("text/test"); !ok || p.Name() != "test" {
		t.Error("parser lookup should be case-insensitive for types")
	}
	if p, ok := r.GetByExtension(".test"); !ok || p.Name() != "test" {
		t.Error("parser lookup should be case-insensitive for extensions")
	}
}

func TestRegistry_Register_ExtensionWithoutDot(t *testing.T) {
	r := NewRegistry()

	parser := &MockParser{
		name:       "test",
		types:      []string{},
		extensions: []string{"nodot"},
	}

	r.Register(parser)

	// Should work without dot prefix
	if p, ok := r.GetByExtension("nodot"); !ok || p.Name() != "test" {
		t.Error("parser should be found for extension without dot")
	}
	// Should also work with dot prefix
	if p, ok := r.GetByExtension(".nodot"); !ok || p.Name() != "test" {
		t.Error("parser should be found for extension with dot")
	}
}

func TestRegistry_SetDefault(t *testing.T) {
	r := NewRegistry()

	defaultParser := &MockParser{name: "default"}
	r.SetDefault(defaultParser)

	// Unknown type/extension should fall back to default
	p, err := r.Get("unknown/type", ".unknown")
	if err != nil {
		t.Errorf("Get() should return default parser, got error: %v", err)
	}
	if p.Name() != "default" {
		t.Errorf("Get() = %q, want 'default'", p.Name())
	}
}

func TestRegistry_GetByType(t *testing.T) {
	r := NewRegistry()

	parser := &MockParser{
		name:  "html",
		types: []string{"text/html"},
	}
	r.Register(parser)

	tests := []struct {
		name     string
		mimeType string
		wantOK   bool
	}{
		{"exact match", "text/html", true},
		{"with charset", "text/html; charset=utf-8", true},
		{"uppercase", "TEXT/HTML", true},
		{"not registered", "text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := r.GetByType(tt.mimeType)
			if ok != tt.wantOK {
				t.Errorf("GetByType(%q) ok = %v, want %v", tt.mimeType, ok, tt.wantOK)
			}
			if ok && p.Name() != "html" {
				t.Errorf("GetByType(%q) returned wrong parser", tt.mimeType)
			}
		})
	}
}

func TestRegistry_GetByExtension(t *testing.T) {
	r := NewRegistry()

	parser := &MockParser{
		name:       "markdown",
		extensions: []string{".md", ".markdown"},
	}
	r.Register(parser)

	tests := []struct {
		name   string
		ext    string
		wantOK bool
	}{
		{"with dot", ".md", true},
		{"without dot", "md", true},
		{"uppercase", ".MD", true},
		{"alternative ext", ".markdown", true},
		{"not registered", ".txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := r.GetByExtension(tt.ext)
			if ok != tt.wantOK {
				t.Errorf("GetByExtension(%q) ok = %v, want %v", tt.ext, ok, tt.wantOK)
			}
			if ok && p.Name() != "markdown" {
				t.Errorf("GetByExtension(%q) returned wrong parser", tt.ext)
			}
		})
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()

	mdParser := &MockParser{
		name:       "markdown",
		types:      []string{"text/markdown"},
		extensions: []string{".md"},
	}
	txtParser := &MockParser{
		name:       "text",
		types:      []string{"text/plain"},
		extensions: []string{".txt"},
	}
	defaultParser := &MockParser{name: "default"}

	r.Register(mdParser)
	r.Register(txtParser)
	r.SetDefault(defaultParser)

	tests := []struct {
		name        string
		contentType string
		ext         string
		wantName    string
		wantErr     bool
	}{
		{
			name:        "match by content type",
			contentType: "text/markdown",
			ext:         "",
			wantName:    "markdown",
		},
		{
			name:        "match by extension",
			contentType: "",
			ext:         ".md",
			wantName:    "markdown",
		},
		{
			name:        "content type takes precedence",
			contentType: "text/markdown",
			ext:         ".txt",
			wantName:    "markdown",
		},
		{
			name:        "fallback to extension",
			contentType: "unknown/type",
			ext:         ".txt",
			wantName:    "text",
		},
		{
			name:        "fallback to default",
			contentType: "unknown/type",
			ext:         ".unknown",
			wantName:    "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := r.Get(tt.contentType, tt.ext)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && p.Name() != tt.wantName {
				t.Errorf("Get() = %q, want %q", p.Name(), tt.wantName)
			}
		})
	}
}

func TestRegistry_Get_NoDefault(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("unknown/type", ".unknown")
	if err == nil {
		t.Error("Get() should return error when no parser found and no default")
	}
	if !strings.Contains(err.Error(), "no parser found") {
		t.Errorf("error message should mention 'no parser found', got: %v", err)
	}
}

func TestMergeMeta(t *testing.T) {
	tests := []struct {
		name      string
		base      *models.DocumentMetadata
		extracted *models.DocumentMetadata
		check     func(t *testing.T, result *models.DocumentMetadata)
	}{
		{
			name:      "nil base",
			base:      nil,
			extracted: &models.DocumentMetadata{Title: "Extracted"},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Title != "Extracted" {
					t.Errorf("title = %q, want 'Extracted'", result.Title)
				}
			},
		},
		{
			name:      "nil extracted",
			base:      &models.DocumentMetadata{Title: "Base"},
			extracted: nil,
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Title != "Base" {
					t.Errorf("title = %q, want 'Base'", result.Title)
				}
			},
		},
		{
			name: "base takes precedence",
			base: &models.DocumentMetadata{
				Title:  "Base Title",
				Author: "Base Author",
			},
			extracted: &models.DocumentMetadata{
				Title:  "Extracted Title",
				Author: "Extracted Author",
			},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Title != "Base Title" {
					t.Errorf("title = %q, want 'Base Title'", result.Title)
				}
				if result.Author != "Base Author" {
					t.Errorf("author = %q, want 'Base Author'", result.Author)
				}
			},
		},
		{
			name:      "empty base uses extracted",
			base:      &models.DocumentMetadata{},
			extracted: &models.DocumentMetadata{Title: "Extracted", Author: "Author"},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Title != "Extracted" {
					t.Errorf("title = %q, want 'Extracted'", result.Title)
				}
				if result.Author != "Author" {
					t.Errorf("author = %q, want 'Author'", result.Author)
				}
			},
		},
		{
			name:      "merge description",
			base:      &models.DocumentMetadata{},
			extracted: &models.DocumentMetadata{Description: "Desc"},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Description != "Desc" {
					t.Errorf("description = %q, want 'Desc'", result.Description)
				}
			},
		},
		{
			name:      "merge language",
			base:      &models.DocumentMetadata{},
			extracted: &models.DocumentMetadata{Language: "en"},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Language != "en" {
					t.Errorf("language = %q, want 'en'", result.Language)
				}
			},
		},
		{
			name:      "merge tags",
			base:      &models.DocumentMetadata{},
			extracted: &models.DocumentMetadata{Tags: []string{"a", "b"}},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if len(result.Tags) != 2 {
					t.Errorf("tags len = %d, want 2", len(result.Tags))
				}
			},
		},
		{
			name:      "base tags preserved",
			base:      &models.DocumentMetadata{Tags: []string{"base"}},
			extracted: &models.DocumentMetadata{Tags: []string{"extracted"}},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if len(result.Tags) != 1 || result.Tags[0] != "base" {
					t.Errorf("tags = %v, want ['base']", result.Tags)
				}
			},
		},
		{
			name:      "merge custom fields",
			base:      &models.DocumentMetadata{Custom: map[string]any{"a": 1}},
			extracted: &models.DocumentMetadata{Custom: map[string]any{"b": 2}},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Custom["a"] != 1 {
					t.Error("custom field 'a' should be preserved")
				}
				if result.Custom["b"] != 2 {
					t.Error("custom field 'b' should be added")
				}
			},
		},
		{
			name:      "base custom preserved",
			base:      &models.DocumentMetadata{Custom: map[string]any{"key": "base"}},
			extracted: &models.DocumentMetadata{Custom: map[string]any{"key": "extracted"}},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Custom["key"] != "base" {
					t.Errorf("custom['key'] = %v, want 'base'", result.Custom["key"])
				}
			},
		},
		{
			name:      "nil base custom, extracted has custom",
			base:      &models.DocumentMetadata{},
			extracted: &models.DocumentMetadata{Custom: map[string]any{"new": "value"}},
			check: func(t *testing.T, result *models.DocumentMetadata) {
				if result.Custom == nil || result.Custom["new"] != "value" {
					t.Error("custom should be created with extracted values")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeMeta(tt.base, tt.extracted)
			if result == nil {
				t.Fatal("MergeMeta() returned nil")
			}
			tt.check(t, result)
		})
	}
}

func TestParse_ConvenienceFunction(t *testing.T) {
	// Save and restore default registry
	original := DefaultRegistry
	defer func() { DefaultRegistry = original }()

	DefaultRegistry = NewRegistry()

	mockParser := &MockParser{
		name:       "mock",
		types:      []string{"test/type"},
		extensions: []string{".mock"},
		parseFunc: func(ctx context.Context, reader io.Reader, docMeta *models.DocumentMetadata) (*ParseResult, error) {
			data, _ := io.ReadAll(reader)
			return &ParseResult{Content: string(data)}, nil
		},
	}
	DefaultRegistry.Register(mockParser)

	reader := strings.NewReader("test content")
	result, err := Parse(context.Background(), reader, "test/type", "", nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Content != "test content" {
		t.Errorf("Parse() content = %q, want 'test content'", result.Content)
	}
}

func TestParse_NoParser(t *testing.T) {
	// Save and restore default registry
	original := DefaultRegistry
	defer func() { DefaultRegistry = original }()

	DefaultRegistry = NewRegistry()

	_, err := Parse(context.Background(), strings.NewReader("content"), "unknown/type", ".unknown", nil)
	if err == nil {
		t.Error("Parse() should return error when no parser found")
	}
}

func TestParse_WithError(t *testing.T) {
	// Save and restore default registry
	original := DefaultRegistry
	defer func() { DefaultRegistry = original }()

	DefaultRegistry = NewRegistry()

	parseErr := errors.New("parse error")
	mockParser := &MockParser{
		name:  "failing",
		types: []string{"test/type"},
		parseFunc: func(ctx context.Context, reader io.Reader, docMeta *models.DocumentMetadata) (*ParseResult, error) {
			return nil, parseErr
		},
	}
	DefaultRegistry.Register(mockParser)

	_, err := Parse(context.Background(), strings.NewReader("content"), "test/type", "", nil)
	if err == nil {
		t.Error("Parse() should return error from parser")
	}
}

func TestSection_Fields(t *testing.T) {
	s := Section{
		Title:       "Test Section",
		Level:       2,
		Content:     "Section content here",
		StartOffset: 100,
		EndOffset:   200,
	}

	if s.Title != "Test Section" {
		t.Errorf("Title = %q, want 'Test Section'", s.Title)
	}
	if s.Level != 2 {
		t.Errorf("Level = %d, want 2", s.Level)
	}
	if s.Content != "Section content here" {
		t.Errorf("Content = %q, want 'Section content here'", s.Content)
	}
	if s.StartOffset != 100 {
		t.Errorf("StartOffset = %d, want 100", s.StartOffset)
	}
	if s.EndOffset != 200 {
		t.Errorf("EndOffset = %d, want 200", s.EndOffset)
	}
}

func TestParseResult_Fields(t *testing.T) {
	meta := &models.DocumentMetadata{Title: "Test"}
	sections := []Section{{Title: "Section 1"}, {Title: "Section 2"}}

	result := &ParseResult{
		Content:  "Full content",
		Metadata: meta,
		Sections: sections,
	}

	if result.Content != "Full content" {
		t.Errorf("Content = %q, want 'Full content'", result.Content)
	}
	if result.Metadata.Title != "Test" {
		t.Errorf("Metadata.Title = %q, want 'Test'", result.Metadata.Title)
	}
	if len(result.Sections) != 2 {
		t.Errorf("Sections len = %d, want 2", len(result.Sections))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	// Concurrent registration and lookup
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			parser := &MockParser{
				name:  "parser",
				types: []string{"type/" + string(rune('a'+i%26))},
			}
			r.Register(parser)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			r.GetByType("type/a")
			r.GetByExtension(".test")
			r.Get("type/a", ".test")
		}
		done <- true
	}()

	// Default setter goroutine
	go func() {
		for i := 0; i < 100; i++ {
			r.SetDefault(&MockParser{name: "default"})
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done
}

// Benchmark tests
func BenchmarkRegistry_GetByType(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 10; i++ {
		parser := &MockParser{
			name:  "parser",
			types: []string{"type/" + string(rune('a'+i))},
		}
		r.Register(parser)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.GetByType("type/e")
	}
}

func BenchmarkRegistry_GetByExtension(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 10; i++ {
		parser := &MockParser{
			name:       "parser",
			extensions: []string{"." + string(rune('a'+i))},
		}
		r.Register(parser)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.GetByExtension(".e")
	}
}

func BenchmarkRegistry_Get(b *testing.B) {
	r := NewRegistry()
	r.Register(&MockParser{
		name:       "parser",
		types:      []string{"text/plain"},
		extensions: []string{".txt"},
	})
	r.SetDefault(&MockParser{name: "default"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get("text/plain", ".txt")
	}
}

func BenchmarkMergeMeta(b *testing.B) {
	base := &models.DocumentMetadata{
		Title:  "Base Title",
		Author: "Base Author",
		Tags:   []string{"tag1", "tag2"},
		Custom: map[string]any{"key1": "value1"},
	}
	extracted := &models.DocumentMetadata{
		Description: "Extracted Description",
		Language:    "en",
		Custom:      map[string]any{"key2": "value2"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MergeMeta(base, extracted)
	}
}
