package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExportFormat specifies the export format.
type ExportFormat string

const (
	// FormatJSON exports as JSON.
	FormatJSON ExportFormat = "json"

	// FormatYAML exports as YAML.
	FormatYAML ExportFormat = "yaml"

	// FormatMarkdown exports as TEMPLATE.md format.
	FormatMarkdown ExportFormat = "markdown"
)

// ExportOptions configures template export behavior.
type ExportOptions struct {
	// Format is the output format (json, yaml, markdown).
	Format ExportFormat

	// IncludeContent includes the system prompt content.
	IncludeContent bool

	// Pretty enables pretty-printing for JSON/YAML.
	Pretty bool

	// StripMetadata removes source/path metadata.
	StripMetadata bool
}

// DefaultExportOptions returns the default export options.
func DefaultExportOptions() ExportOptions {
	return ExportOptions{
		Format:         FormatJSON,
		IncludeContent: true,
		Pretty:         true,
		StripMetadata:  false,
	}
}

// Exporter handles template export operations.
type Exporter struct {
	registry *Registry
}

// NewExporter creates a new template exporter.
func NewExporter(registry *Registry) *Exporter {
	return &Exporter{registry: registry}
}

// Export exports a template to the specified format.
func (e *Exporter) Export(name string, opts ExportOptions) ([]byte, error) {
	tmpl, ok := e.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("template not found: %s", name)
	}

	// Load content if needed
	if opts.IncludeContent && tmpl.Content == "" {
		content, err := e.registry.LoadContent(name)
		if err != nil {
			return nil, fmt.Errorf("load content: %w", err)
		}
		tmpl.Content = content
	}

	return ExportTemplate(tmpl, opts)
}

// ExportTemplate exports a template object to the specified format.
func ExportTemplate(tmpl *AgentTemplate, opts ExportOptions) ([]byte, error) {
	// Create a copy for export
	export := prepareForExport(tmpl, opts)

	switch opts.Format {
	case FormatJSON:
		return exportJSON(export, opts.Pretty)
	case FormatYAML:
		return exportYAML(export, opts.Pretty)
	case FormatMarkdown:
		return exportMarkdown(tmpl, opts)
	default:
		return nil, fmt.Errorf("unsupported format: %s", opts.Format)
	}
}

// ExportToFile exports a template to a file.
func (e *Exporter) ExportToFile(name, path string, opts ExportOptions) error {
	data, err := e.Export(name, opts)
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// ExportAll exports all templates to the specified format.
func (e *Exporter) ExportAll(opts ExportOptions) ([]byte, error) {
	templates := e.registry.List()

	// Load content for all templates if needed
	if opts.IncludeContent {
		for _, tmpl := range templates {
			if tmpl.Content == "" {
				content, err := e.registry.LoadContent(tmpl.Name)
				if err != nil {
					continue // Skip templates that can't be loaded
				}
				tmpl.Content = content
			}
		}
	}

	// Prepare for export
	exports := make([]*templateExport, 0, len(templates))
	for _, tmpl := range templates {
		exports = append(exports, prepareForExport(tmpl, opts))
	}

	switch opts.Format {
	case FormatJSON:
		return exportJSON(exports, opts.Pretty)
	case FormatYAML:
		return exportYAML(exports, opts.Pretty)
	default:
		return nil, fmt.Errorf("format %s not supported for bulk export", opts.Format)
	}
}

// templateExport is the export structure for a template.
type templateExport struct {
	Name        string             `json:"name" yaml:"name"`
	Version     string             `json:"version,omitempty" yaml:"version,omitempty"`
	Description string             `json:"description" yaml:"description"`
	Author      string             `json:"author,omitempty" yaml:"author,omitempty"`
	Homepage    string             `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Tags        []string           `json:"tags,omitempty" yaml:"tags,omitempty"`
	Variables   []TemplateVariable `json:"variables,omitempty" yaml:"variables,omitempty"`
	Agent       AgentTemplateSpec  `json:"agent" yaml:"agent"`
	Content     string             `json:"content,omitempty" yaml:"content,omitempty"`
	Source      SourceType         `json:"source,omitempty" yaml:"source,omitempty"`
	Path        string             `json:"path,omitempty" yaml:"path,omitempty"`
}

// prepareForExport creates an export-friendly copy of a template.
func prepareForExport(tmpl *AgentTemplate, opts ExportOptions) *templateExport {
	export := &templateExport{
		Name:        tmpl.Name,
		Version:     tmpl.Version,
		Description: tmpl.Description,
		Author:      tmpl.Author,
		Homepage:    tmpl.Homepage,
		Tags:        tmpl.Tags,
		Variables:   tmpl.Variables,
		Agent:       tmpl.Agent,
	}

	if opts.IncludeContent {
		export.Content = tmpl.Content
	}

	if !opts.StripMetadata {
		export.Source = tmpl.Source
		export.Path = tmpl.Path
	}

	return export
}

// exportJSON exports to JSON format.
func exportJSON(v any, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(v, "", "  ")
	}
	return json.Marshal(v)
}

// exportYAML exports to YAML format.
func exportYAML(v any, pretty bool) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	if pretty {
		encoder.SetIndent(2)
	}
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// exportMarkdown exports to TEMPLATE.md format.
func exportMarkdown(tmpl *AgentTemplate, opts ExportOptions) ([]byte, error) {
	var buf bytes.Buffer

	// Write frontmatter
	buf.WriteString("---\n")

	// Create frontmatter structure (without content)
	frontmatter := struct {
		Name        string             `yaml:"name"`
		Version     string             `yaml:"version,omitempty"`
		Description string             `yaml:"description"`
		Author      string             `yaml:"author,omitempty"`
		Homepage    string             `yaml:"homepage,omitempty"`
		Tags        []string           `yaml:"tags,omitempty"`
		Variables   []TemplateVariable `yaml:"variables,omitempty"`
		Agent       AgentTemplateSpec  `yaml:"agent"`
	}{
		Name:        tmpl.Name,
		Version:     tmpl.Version,
		Description: tmpl.Description,
		Author:      tmpl.Author,
		Homepage:    tmpl.Homepage,
		Tags:        tmpl.Tags,
		Variables:   tmpl.Variables,
		Agent:       tmpl.Agent,
	}

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(frontmatter); err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}

	buf.WriteString("---\n\n")

	// Write content
	if opts.IncludeContent && tmpl.Content != "" {
		buf.WriteString(tmpl.Content)
		if !strings.HasSuffix(tmpl.Content, "\n") {
			buf.WriteString("\n")
		}
	}

	return buf.Bytes(), nil
}

// ExportToDirectory exports a template to a directory as TEMPLATE.md.
func (e *Exporter) ExportToDirectory(name, dir string, opts ExportOptions) error {
	opts.Format = FormatMarkdown
	opts.IncludeContent = true

	data, err := e.Export(name, opts)
	if err != nil {
		return err
	}

	// Create directory
	templateDir := filepath.Join(dir, name)
	if err := os.MkdirAll(templateDir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write TEMPLATE.md
	path := filepath.Join(templateDir, TemplateFilename)
	return os.WriteFile(path, data, 0644)
}

// ExportAllToDirectory exports all templates to a directory.
func (e *Exporter) ExportAllToDirectory(dir string, opts ExportOptions) error {
	templates := e.registry.List()

	for _, tmpl := range templates {
		if err := e.ExportToDirectory(tmpl.Name, dir, opts); err != nil {
			return fmt.Errorf("export %s: %w", tmpl.Name, err)
		}
	}

	return nil
}
