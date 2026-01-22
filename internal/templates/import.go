package templates

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ImportOptions configures template import behavior.
type ImportOptions struct {
	// Overwrite allows overwriting existing templates.
	Overwrite bool

	// Validate performs strict validation on import.
	Validate bool

	// Source sets the source type for imported templates.
	Source SourceType

	// BasePath is used for relative path resolution.
	BasePath string
}

// DefaultImportOptions returns the default import options.
func DefaultImportOptions() ImportOptions {
	return ImportOptions{
		Overwrite: false,
		Validate:  true,
		Source:    SourceLocal,
	}
}

// Importer handles template import operations.
type Importer struct {
	registry *Registry
}

// NewImporter creates a new template importer.
func NewImporter(registry *Registry) *Importer {
	return &Importer{registry: registry}
}

// ImportFromFile imports a template from a file.
func (i *Importer) ImportFromFile(path string, opts ImportOptions) (*AgentTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	tmpl, err := i.parseByFormat(data, ext, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}

	// Set source metadata
	tmpl.Source = opts.Source
	tmpl.Path = filepath.Dir(path)

	if err := i.registerTemplate(tmpl, opts); err != nil {
		return nil, err
	}

	return tmpl, nil
}

// ImportFromDirectory imports a template from a directory containing TEMPLATE.md.
func (i *Importer) ImportFromDirectory(dir string, opts ImportOptions) (*AgentTemplate, error) {
	templateFile := filepath.Join(dir, TemplateFilename)

	if _, err := os.Stat(templateFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("no %s found in directory", TemplateFilename)
	}

	tmpl, err := ParseTemplateFile(templateFile)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	// Set source metadata
	tmpl.Source = opts.Source
	tmpl.Path = dir

	if err := i.registerTemplate(tmpl, opts); err != nil {
		return nil, err
	}

	return tmpl, nil
}

// ImportFromURL imports a template from a URL.
func (i *Importer) ImportFromURL(url string, opts ImportOptions) (*AgentTemplate, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Determine format from Content-Type or URL
	format := detectFormat(resp.Header.Get("Content-Type"), url)
	tmpl, err := i.parseByFormat(data, format, "")
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Set source metadata
	tmpl.Source = SourceRegistry
	tmpl.Path = url

	if err := i.registerTemplate(tmpl, opts); err != nil {
		return nil, err
	}

	return tmpl, nil
}

// ImportFromReader imports a template from an io.Reader.
func (i *Importer) ImportFromReader(r io.Reader, format string, opts ImportOptions) (*AgentTemplate, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}

	tmpl, err := i.parseByFormat(data, format, opts.BasePath)
	if err != nil {
		return nil, fmt.Errorf("parse data: %w", err)
	}

	// Set source metadata
	tmpl.Source = opts.Source

	if err := i.registerTemplate(tmpl, opts); err != nil {
		return nil, err
	}

	return tmpl, nil
}

// ImportFromJSON imports a template from JSON data.
func (i *Importer) ImportFromJSON(data []byte, opts ImportOptions) (*AgentTemplate, error) {
	return i.parseByFormat(data, ".json", opts.BasePath)
}

// ImportFromYAML imports a template from YAML data.
func (i *Importer) ImportFromYAML(data []byte, opts ImportOptions) (*AgentTemplate, error) {
	return i.parseByFormat(data, ".yaml", opts.BasePath)
}

// parseByFormat parses template data based on file format.
func (i *Importer) parseByFormat(data []byte, format, basePath string) (*AgentTemplate, error) {
	switch strings.ToLower(format) {
	case ".json", "json", "application/json":
		return parseJSON(data)
	case ".yaml", ".yml", "yaml", "application/yaml", "application/x-yaml", "text/yaml":
		return parseYAML(data)
	case ".md", "md", "markdown", "text/markdown":
		return ParseTemplate(data, basePath)
	default:
		// Try to auto-detect
		return autoDetectAndParse(data, basePath)
	}
}

// parseJSON parses a template from JSON.
func parseJSON(data []byte) (*AgentTemplate, error) {
	var tmpl AgentTemplate
	if err := json.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return &tmpl, nil
}

// parseYAML parses a template from YAML.
func parseYAML(data []byte) (*AgentTemplate, error) {
	var tmpl AgentTemplate
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	return &tmpl, nil
}

// autoDetectAndParse tries to auto-detect the format and parse.
func autoDetectAndParse(data []byte, basePath string) (*AgentTemplate, error) {
	content := strings.TrimSpace(string(data))

	// Check for markdown frontmatter
	if strings.HasPrefix(content, "---") {
		return ParseTemplate(data, basePath)
	}

	// Check for JSON
	if strings.HasPrefix(content, "{") {
		return parseJSON(data)
	}

	// Try YAML
	tmpl, err := parseYAML(data)
	if err == nil && tmpl.Name != "" {
		return tmpl, nil
	}

	return nil, fmt.Errorf("unable to detect format")
}

// detectFormat detects the format from content type or URL.
func detectFormat(contentType, url string) string {
	// Check content type
	contentType = strings.ToLower(contentType)
	if strings.Contains(contentType, "json") {
		return ".json"
	}
	if strings.Contains(contentType, "yaml") {
		return ".yaml"
	}

	// Check URL extension
	ext := filepath.Ext(url)
	if ext != "" {
		return ext
	}

	return ""
}

// registerTemplate registers a template with validation.
func (i *Importer) registerTemplate(tmpl *AgentTemplate, opts ImportOptions) error {
	// Validate if requested
	if opts.Validate {
		if err := ValidateTemplate(tmpl); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	// Check for existing template
	if existing, ok := i.registry.Get(tmpl.Name); ok {
		if !opts.Overwrite {
			return fmt.Errorf("template already exists: %s (source: %s)", tmpl.Name, existing.Source)
		}
	}

	// Register the template
	return i.registry.Register(tmpl)
}

// ImportBatch imports multiple templates from a directory.
func (i *Importer) ImportBatch(dir string, opts ImportOptions) ([]*AgentTemplate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var templates []*AgentTemplate
	var errors []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		templateDir := filepath.Join(dir, entry.Name())
		tmpl, err := i.ImportFromDirectory(templateDir, opts)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}
		templates = append(templates, tmpl)
	}

	if len(errors) > 0 && len(templates) == 0 {
		return nil, fmt.Errorf("all imports failed:\n%s", strings.Join(errors, "\n"))
	}

	return templates, nil
}

// ImportFromGit imports templates from a git repository (placeholder).
func (i *Importer) ImportFromGit(repoURL, branch, subPath string, opts ImportOptions) ([]*AgentTemplate, error) {
	// This would clone the repo to a temp directory and import
	// For now, return an error indicating it's not implemented
	return nil, fmt.Errorf("git import not yet implemented")
}

// CloneTemplate creates a copy of an existing template with a new name.
func (i *Importer) CloneTemplate(sourceName, newName string, opts ImportOptions) (*AgentTemplate, error) {
	source, ok := i.registry.Get(sourceName)
	if !ok {
		return nil, fmt.Errorf("source template not found: %s", sourceName)
	}

	// Load content if needed
	if source.Content == "" {
		content, err := i.registry.LoadContent(sourceName)
		if err != nil {
			return nil, fmt.Errorf("load content: %w", err)
		}
		source.Content = content
	}

	// Create a deep copy
	clone := &AgentTemplate{
		Name:           newName,
		Version:        source.Version,
		Description:    source.Description,
		Author:         source.Author,
		Homepage:       source.Homepage,
		Tags:           append([]string{}, source.Tags...),
		Variables:      append([]TemplateVariable{}, source.Variables...),
		Agent:          source.Agent, // Note: nested pointers may need deep copy
		Content:        source.Content,
		Source:         opts.Source,
		SourcePriority: PriorityLocal,
	}

	// Deep copy agent tools
	if source.Agent.Tools != nil {
		clone.Agent.Tools = append([]string{}, source.Agent.Tools...)
	}

	// Deep copy agent metadata
	if source.Agent.Metadata != nil {
		clone.Agent.Metadata = make(map[string]any)
		for k, v := range source.Agent.Metadata {
			clone.Agent.Metadata[k] = v
		}
	}

	if err := i.registerTemplate(clone, opts); err != nil {
		return nil, err
	}

	return clone, nil
}
