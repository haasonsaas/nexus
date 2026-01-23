package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultExportOptions(t *testing.T) {
	opts := DefaultExportOptions()

	if opts.Format != FormatJSON {
		t.Errorf("Format = %v, want %v", opts.Format, FormatJSON)
	}
	if !opts.IncludeContent {
		t.Error("IncludeContent should be true")
	}
	if !opts.Pretty {
		t.Error("Pretty should be true")
	}
	if opts.StripMetadata {
		t.Error("StripMetadata should be false")
	}
}

func TestExportFormat_Constants(t *testing.T) {
	if FormatJSON != "json" {
		t.Errorf("FormatJSON = %q, want %q", FormatJSON, "json")
	}
	if FormatYAML != "yaml" {
		t.Errorf("FormatYAML = %q, want %q", FormatYAML, "yaml")
	}
	if FormatMarkdown != "markdown" {
		t.Errorf("FormatMarkdown = %q, want %q", FormatMarkdown, "markdown")
	}
}

func TestExportTemplate_JSON(t *testing.T) {
	tmpl := &AgentTemplate{
		Name:        "test-template",
		Version:     "1.0.0",
		Description: "Test description",
		Author:      "Test Author",
		Content:     "Test content",
	}

	t.Run("pretty JSON", func(t *testing.T) {
		opts := ExportOptions{
			Format:         FormatJSON,
			IncludeContent: true,
			Pretty:         true,
		}

		data, err := ExportTemplate(tmpl, opts)
		if err != nil {
			t.Fatalf("ExportTemplate error: %v", err)
		}

		// Verify it's valid JSON
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}

		if parsed["name"] != "test-template" {
			t.Errorf("name = %v, want %q", parsed["name"], "test-template")
		}
		if parsed["content"] != "Test content" {
			t.Errorf("content = %v, want %q", parsed["content"], "Test content")
		}

		// Pretty should have indentation
		if !strings.Contains(string(data), "\n  ") {
			t.Error("expected pretty-printed JSON with indentation")
		}
	})

	t.Run("compact JSON", func(t *testing.T) {
		opts := ExportOptions{
			Format:         FormatJSON,
			IncludeContent: true,
			Pretty:         false,
		}

		data, err := ExportTemplate(tmpl, opts)
		if err != nil {
			t.Fatalf("ExportTemplate error: %v", err)
		}

		// Compact JSON should not have indentation (no newlines mid-content)
		if strings.Contains(string(data), "\n  ") {
			t.Error("compact JSON should not have indentation")
		}
	})

	t.Run("without content", func(t *testing.T) {
		opts := ExportOptions{
			Format:         FormatJSON,
			IncludeContent: false,
			Pretty:         true,
		}

		data, err := ExportTemplate(tmpl, opts)
		if err != nil {
			t.Fatalf("ExportTemplate error: %v", err)
		}

		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}

		if _, exists := parsed["content"]; exists && parsed["content"] != "" {
			t.Error("content should be empty when IncludeContent is false")
		}
	})

	t.Run("strip metadata", func(t *testing.T) {
		tmplWithMeta := &AgentTemplate{
			Name:   "test",
			Source: SourceLocal,
			Path:   "/some/path",
		}
		opts := ExportOptions{
			Format:        FormatJSON,
			StripMetadata: true,
			Pretty:        true,
		}

		data, err := ExportTemplate(tmplWithMeta, opts)
		if err != nil {
			t.Fatalf("ExportTemplate error: %v", err)
		}

		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}

		if _, exists := parsed["source"]; exists && parsed["source"] != "" {
			t.Error("source should be stripped")
		}
		if _, exists := parsed["path"]; exists && parsed["path"] != "" {
			t.Error("path should be stripped")
		}
	})
}

func TestExportTemplate_YAML(t *testing.T) {
	tmpl := &AgentTemplate{
		Name:        "test-template",
		Version:     "1.0.0",
		Description: "Test description",
		Content:     "Test content",
	}

	t.Run("pretty YAML", func(t *testing.T) {
		opts := ExportOptions{
			Format:         FormatYAML,
			IncludeContent: true,
			Pretty:         true,
		}

		data, err := ExportTemplate(tmpl, opts)
		if err != nil {
			t.Fatalf("ExportTemplate error: %v", err)
		}

		// Verify it's valid YAML
		var parsed map[string]any
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("invalid YAML: %v", err)
		}

		if parsed["name"] != "test-template" {
			t.Errorf("name = %v, want %q", parsed["name"], "test-template")
		}
	})
}

func TestExportTemplate_Markdown(t *testing.T) {
	tmpl := &AgentTemplate{
		Name:        "test-template",
		Version:     "1.0.0",
		Description: "Test description",
		Content:     "This is the system prompt content.",
	}

	opts := ExportOptions{
		Format:         FormatMarkdown,
		IncludeContent: true,
		Pretty:         true,
	}

	data, err := ExportTemplate(tmpl, opts)
	if err != nil {
		t.Fatalf("ExportTemplate error: %v", err)
	}

	content := string(data)

	// Should have frontmatter delimiters
	if !strings.HasPrefix(content, "---\n") {
		t.Error("markdown should start with frontmatter delimiter")
	}
	if !strings.Contains(content, "\n---\n") {
		t.Error("markdown should have closing frontmatter delimiter")
	}

	// Should contain the content
	if !strings.Contains(content, "This is the system prompt content.") {
		t.Error("markdown should contain the template content")
	}
}

func TestExportTemplate_UnsupportedFormat(t *testing.T) {
	tmpl := &AgentTemplate{Name: "test"}
	opts := ExportOptions{Format: ExportFormat("unknown")}

	_, err := ExportTemplate(tmpl, opts)
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestNewExporter(t *testing.T) {
	reg, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry error: %v", err)
	}
	exp := NewExporter(reg)

	if exp == nil {
		t.Fatal("expected non-nil exporter")
	}
	if exp.registry != reg {
		t.Error("exporter should reference the registry")
	}
}

func TestExporter_Export_NotFound(t *testing.T) {
	reg, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry error: %v", err)
	}
	exp := NewExporter(reg)

	_, exportErr := exp.Export("nonexistent", DefaultExportOptions())
	if exportErr == nil {
		t.Error("expected error for nonexistent template")
	}
}
