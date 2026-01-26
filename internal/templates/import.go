package templates

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// execCommand is a variable to allow mocking in tests.
var execCommand = exec.Command

var maxTemplateImportBytes int64 = 10 * 1024 * 1024

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

	if maxTemplateImportBytes > 0 && resp.ContentLength > maxTemplateImportBytes {
		return nil, fmt.Errorf("template exceeds max size of %d bytes", maxTemplateImportBytes)
	}

	data, err := readAllWithLimit(resp.Body, maxTemplateImportBytes)
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
	data, err := readAllWithLimit(r, maxTemplateImportBytes)
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

func readAllWithLimit(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(r)
	}

	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("data exceeds max size of %d bytes", maxBytes)
	}
	return data, nil
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

// ImportFromGit imports templates from a git repository.
func (i *Importer) ImportFromGit(repoURL, branch, subPath string, opts ImportOptions) ([]*AgentTemplate, error) {
	if repoURL == "" {
		return nil, fmt.Errorf("repository URL is required")
	}

	// Create temporary directory for clone
	tempDir, err := os.MkdirTemp("", "nexus-template-*")
	if err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Build git clone command arguments
	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, tempDir)

	// Execute git clone
	if err := runGitCommand(args...); err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}

	// Determine import directory
	importDir := tempDir
	if subPath != "" {
		importDir = filepath.Join(tempDir, subPath)
		if info, err := os.Stat(importDir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("subpath %q not found in repository", subPath)
		}
	}

	// Check if it's a single template or a directory of templates
	if isTemplateFile(importDir) {
		// Single template directory
		tmpl, err := i.ImportFromDirectory(importDir, opts)
		if err != nil {
			return nil, err
		}
		return []*AgentTemplate{tmpl}, nil
	}

	// Import all templates from directory
	return i.ImportBatch(importDir, opts)
}

// runGitCommand executes a git command and returns any error.
func runGitCommand(args ...string) error {
	// Use os/exec to run git
	cmd := execCommand("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(output))
	}
	return nil
}

// isTemplateFile checks if a directory contains a template.yaml file.
func isTemplateFile(dir string) bool {
	for _, name := range []string{"template.yaml", "template.yml", "template.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
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
