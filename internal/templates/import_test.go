package templates

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultImportOptions(t *testing.T) {
	opts := DefaultImportOptions()

	if opts.Overwrite {
		t.Error("Overwrite should be false by default")
	}
	if !opts.Validate {
		t.Error("Validate should be true by default")
	}
	if opts.Source != SourceLocal {
		t.Errorf("Source = %v, want %v", opts.Source, SourceLocal)
	}
}

func TestNewImporter(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	importer := NewImporter(registry)
	if importer == nil {
		t.Fatal("NewImporter() returned nil")
	}
	if importer.registry != registry {
		t.Error("importer.registry not set correctly")
	}
}

func TestImporterImportFromFile(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	t.Run("JSON file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "template.json")
		data := `{"name": "test-json", "version": "1.0.0", "description": "Test template"}`
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		tmpl, err := importer.ImportFromFile(path, DefaultImportOptions())
		if err != nil {
			t.Fatalf("ImportFromFile() error = %v", err)
		}
		if tmpl.Name != "test-json" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "test-json")
		}
	})

	t.Run("YAML file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "template.yaml")
		data := "name: test-yaml\nversion: 1.0.0\ndescription: Test template"
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		tmpl, err := importer.ImportFromFile(path, DefaultImportOptions())
		if err != nil {
			t.Fatalf("ImportFromFile() error = %v", err)
		}
		if tmpl.Name != "test-yaml" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "test-yaml")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := importer.ImportFromFile("/nonexistent/path.json", DefaultImportOptions())
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

func TestImporterImportFromDirectory(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	t.Run("valid directory", func(t *testing.T) {
		dir := t.TempDir()
		templateFile := filepath.Join(dir, TemplateFilename)
		content := `---
name: dir-template
version: 1.0.0
description: Directory template
---
System prompt content here.
`
		if err := os.WriteFile(templateFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write template file: %v", err)
		}

		tmpl, err := importer.ImportFromDirectory(dir, DefaultImportOptions())
		if err != nil {
			t.Fatalf("ImportFromDirectory() error = %v", err)
		}
		if tmpl.Name != "dir-template" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "dir-template")
		}
	})

	t.Run("missing TEMPLATE.md", func(t *testing.T) {
		dir := t.TempDir()
		_, err := importer.ImportFromDirectory(dir, DefaultImportOptions())
		if err == nil {
			t.Error("expected error for missing TEMPLATE.md")
		}
	})
}

func TestImporterImportFromReader(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	t.Run("JSON reader", func(t *testing.T) {
		data := `{"name": "reader-json", "version": "1.0.0", "description": "Test template"}`
		reader := bytes.NewReader([]byte(data))
		opts := DefaultImportOptions()

		tmpl, err := importer.ImportFromReader(reader, ".json", opts)
		if err != nil {
			t.Fatalf("ImportFromReader() error = %v", err)
		}
		if tmpl.Name != "reader-json" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "reader-json")
		}
	})

	t.Run("YAML reader", func(t *testing.T) {
		data := "name: reader-yaml\nversion: 1.0.0\ndescription: Test template"
		reader := bytes.NewReader([]byte(data))
		opts := DefaultImportOptions()

		tmpl, err := importer.ImportFromReader(reader, ".yaml", opts)
		if err != nil {
			t.Fatalf("ImportFromReader() error = %v", err)
		}
		if tmpl.Name != "reader-yaml" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "reader-yaml")
		}
	})
}

func TestImporterImportFromJSON(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	t.Run("valid JSON", func(t *testing.T) {
		data := []byte(`{"name": "json-template", "version": "1.0.0"}`)
		tmpl, err := importer.ImportFromJSON(data, DefaultImportOptions())
		if err != nil {
			t.Fatalf("ImportFromJSON() error = %v", err)
		}
		if tmpl.Name != "json-template" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "json-template")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		data := []byte(`{invalid json}`)
		_, err := importer.ImportFromJSON(data, DefaultImportOptions())
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestImporterImportFromYAML(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	t.Run("valid YAML", func(t *testing.T) {
		data := []byte("name: yaml-template\nversion: 1.0.0")
		tmpl, err := importer.ImportFromYAML(data, DefaultImportOptions())
		if err != nil {
			t.Fatalf("ImportFromYAML() error = %v", err)
		}
		if tmpl.Name != "yaml-template" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "yaml-template")
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		data := []byte(`invalid: [yaml: broken`)
		_, err := importer.ImportFromYAML(data, DefaultImportOptions())
		if err == nil {
			t.Error("expected error for invalid YAML")
		}
	})
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		contentType string
		url         string
		expected    string
	}{
		{"application/json", "", ".json"},
		{"application/json; charset=utf-8", "", ".json"},
		{"application/yaml", "", ".yaml"},
		{"text/yaml", "", ".yaml"},
		{"", "https://example.com/template.json", ".json"},
		{"", "https://example.com/template.yaml", ".yaml"},
		{"", "https://example.com/template.yml", ".yml"},
		{"", "https://example.com/template.md", ".md"},
		{"", "https://example.com/template", ""},
	}

	for _, tt := range tests {
		t.Run(tt.contentType+"_"+tt.url, func(t *testing.T) {
			result := detectFormat(tt.contentType, tt.url)
			if result != tt.expected {
				t.Errorf("detectFormat(%q, %q) = %q, want %q", tt.contentType, tt.url, result, tt.expected)
			}
		})
	}
}

func TestAutoDetectAndParse(t *testing.T) {
	t.Run("JSON detection", func(t *testing.T) {
		data := []byte(`{"name": "auto-json", "version": "1.0.0"}`)
		tmpl, err := autoDetectAndParse(data, "")
		if err != nil {
			t.Fatalf("autoDetectAndParse() error = %v", err)
		}
		if tmpl.Name != "auto-json" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "auto-json")
		}
	})

	t.Run("YAML detection", func(t *testing.T) {
		data := []byte("name: auto-yaml\nversion: 1.0.0")
		tmpl, err := autoDetectAndParse(data, "")
		if err != nil {
			t.Fatalf("autoDetectAndParse() error = %v", err)
		}
		if tmpl.Name != "auto-yaml" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "auto-yaml")
		}
	})

	t.Run("markdown detection", func(t *testing.T) {
		data := []byte(`---
name: auto-markdown
version: 1.0.0
description: Test markdown template
---
Content`)
		tmpl, err := autoDetectAndParse(data, "")
		if err != nil {
			t.Fatalf("autoDetectAndParse() error = %v", err)
		}
		if tmpl.Name != "auto-markdown" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "auto-markdown")
		}
	})

	t.Run("undetectable format", func(t *testing.T) {
		data := []byte("random content without structure")
		_, err := autoDetectAndParse(data, "")
		if err == nil {
			t.Error("expected error for undetectable format")
		}
	})
}

func TestIsTemplateFile(t *testing.T) {
	t.Run("directory with template.yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "template.yaml")
		if err := os.WriteFile(path, []byte("name: test"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		if !isTemplateFile(dir) {
			t.Error("isTemplateFile() = false, want true")
		}
	})

	t.Run("directory with template.json", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "template.json")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		if !isTemplateFile(dir) {
			t.Error("isTemplateFile() = false, want true")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		if isTemplateFile(dir) {
			t.Error("isTemplateFile() = true, want false")
		}
	})
}

func TestImporterImportBatch(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	t.Run("batch import", func(t *testing.T) {
		dir := t.TempDir()

		// Create first template directory
		tmpl1Dir := filepath.Join(dir, "template1")
		if err := os.MkdirAll(tmpl1Dir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		content1 := `---
name: batch-tmpl-1
version: 1.0.0
description: Batch template 1
---
Content 1`
		if err := os.WriteFile(filepath.Join(tmpl1Dir, TemplateFilename), []byte(content1), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		// Create second template directory
		tmpl2Dir := filepath.Join(dir, "template2")
		if err := os.MkdirAll(tmpl2Dir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		content2 := `---
name: batch-tmpl-2
version: 1.0.0
description: Batch template 2
---
Content 2`
		if err := os.WriteFile(filepath.Join(tmpl2Dir, TemplateFilename), []byte(content2), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		templates, err := importer.ImportBatch(dir, DefaultImportOptions())
		if err != nil {
			t.Fatalf("ImportBatch() error = %v", err)
		}
		if len(templates) != 2 {
			t.Errorf("len(templates) = %d, want 2", len(templates))
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		templates, err := importer.ImportBatch(dir, DefaultImportOptions())
		if err != nil {
			t.Fatalf("ImportBatch() error = %v", err)
		}
		if len(templates) != 0 {
			t.Errorf("len(templates) = %d, want 0", len(templates))
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		_, err := importer.ImportBatch("/nonexistent/path", DefaultImportOptions())
		if err == nil {
			t.Error("expected error for nonexistent directory")
		}
	})
}

func TestImporterCloneTemplate(t *testing.T) {
	t.Run("successful clone", func(t *testing.T) {
		registry, err := NewRegistry(nil, "")
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}
		importer := NewImporter(registry)

		// Create and register a source template
		source := &AgentTemplate{
			Name:        "source-template",
			Version:     "1.0.0",
			Description: "Source template",
			Tags:        []string{"tag1", "tag2"},
			Content:     "System prompt",
			Agent: AgentTemplateSpec{
				Tools: []string{"tool1"},
			},
		}
		if err := registry.Register(source); err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		clone, err := importer.CloneTemplate("source-template", "cloned-template", DefaultImportOptions())
		if err != nil {
			t.Fatalf("CloneTemplate() error = %v", err)
		}
		if clone.Name != "cloned-template" {
			t.Errorf("Name = %q, want %q", clone.Name, "cloned-template")
		}
		if clone.Version != source.Version {
			t.Errorf("Version = %q, want %q", clone.Version, source.Version)
		}
		if clone.Content != source.Content {
			t.Errorf("Content = %q, want %q", clone.Content, source.Content)
		}
		if len(clone.Tags) != len(source.Tags) {
			t.Errorf("len(Tags) = %d, want %d", len(clone.Tags), len(source.Tags))
		}
	})

	t.Run("nonexistent source", func(t *testing.T) {
		registry, err := NewRegistry(nil, "")
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}
		importer := NewImporter(registry)

		_, err = importer.CloneTemplate("nonexistent", "new-name", DefaultImportOptions())
		if err == nil {
			t.Error("expected error for nonexistent source")
		}
	})
}

func TestImporterRegisterTemplate_Overwrite(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	// Register first template
	tmpl1 := &AgentTemplate{
		Name:        "overwrite-test",
		Version:     "1.0.0",
		Description: "First version",
	}
	opts := DefaultImportOptions()
	if err := importer.registerTemplate(tmpl1, opts); err != nil {
		t.Fatalf("first register error = %v", err)
	}

	// Try to register again without overwrite
	tmpl2 := &AgentTemplate{
		Name:        "overwrite-test",
		Version:     "2.0.0",
		Description: "Second version",
	}
	err = importer.registerTemplate(tmpl2, opts)
	if err == nil {
		t.Error("expected error without overwrite")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists': %v", err)
	}

	// Register with overwrite
	opts.Overwrite = true
	if err := importer.registerTemplate(tmpl2, opts); err != nil {
		t.Fatalf("overwrite register error = %v", err)
	}

	// Verify the new version
	result, ok := registry.Get("overwrite-test")
	if !ok {
		t.Fatal("template not found after overwrite")
	}
	if result.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", result.Version, "2.0.0")
	}
}

func TestImporterImportFromGit_EmptyURL(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	importer := NewImporter(registry)

	_, err = importer.ImportFromGit("", "", "", DefaultImportOptions())
	if err == nil {
		t.Error("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention 'required': %v", err)
	}
}

func TestParseJSON(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		data := []byte(`{"name": "json-test", "version": "1.0.0", "description": "Test"}`)
		tmpl, err := parseJSON(data)
		if err != nil {
			t.Fatalf("parseJSON() error = %v", err)
		}
		if tmpl.Name != "json-test" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "json-test")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		data := []byte(`{invalid`)
		_, err := parseJSON(data)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestParseYAML(t *testing.T) {
	t.Run("valid YAML", func(t *testing.T) {
		data := []byte("name: yaml-test\nversion: 1.0.0\ndescription: Test")
		tmpl, err := parseYAML(data)
		if err != nil {
			t.Fatalf("parseYAML() error = %v", err)
		}
		if tmpl.Name != "yaml-test" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "yaml-test")
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		data := []byte("name: [broken: yaml")
		_, err := parseYAML(data)
		if err == nil {
			t.Error("expected error for invalid YAML")
		}
	})
}

func TestImportOptionsStruct(t *testing.T) {
	opts := ImportOptions{
		Overwrite: true,
		Validate:  false,
		Source:    SourceRegistry,
		BasePath:  "/base/path",
	}

	if !opts.Overwrite {
		t.Error("Overwrite should be true")
	}
	if opts.Validate {
		t.Error("Validate should be false")
	}
	if opts.Source != SourceRegistry {
		t.Errorf("Source = %v, want %v", opts.Source, SourceRegistry)
	}
	if opts.BasePath != "/base/path" {
		t.Errorf("BasePath = %q", opts.BasePath)
	}
}
