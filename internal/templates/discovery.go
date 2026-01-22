package templates

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// DiscoverySource discovers templates from a specific source.
type DiscoverySource interface {
	// Type returns the source type identifier.
	Type() SourceType

	// Priority returns the source priority (higher wins in conflicts).
	Priority() int

	// Discover scans for templates and returns found entries.
	Discover(ctx context.Context) ([]*AgentTemplate, error)
}

// WatchableSource exposes paths for file watching.
type WatchableSource interface {
	WatchPaths() []string
}

// LocalSource discovers templates from a local directory.
type LocalSource struct {
	path       string
	sourceType SourceType
	priority   int
	logger     *slog.Logger
}

// NewLocalSource creates a local directory discovery source.
func NewLocalSource(path string, sourceType SourceType, priority int) *LocalSource {
	return &LocalSource{
		path:       path,
		sourceType: sourceType,
		priority:   priority,
		logger:     slog.Default().With("component", "templates", "source", sourceType),
	}
}

func (s *LocalSource) Type() SourceType {
	return s.sourceType
}

func (s *LocalSource) Priority() int {
	return s.priority
}

func (s *LocalSource) Discover(ctx context.Context) ([]*AgentTemplate, error) {
	// Check if directory exists
	info, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		s.logger.Debug("templates directory does not exist", "path", s.path)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", s.path)
	}

	// List subdirectories (each is a potential template)
	entries, err := os.ReadDir(s.path)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var templates []*AgentTemplate
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return templates, ctx.Err()
		default:
		}

		if !entry.IsDir() {
			continue
		}

		templatePath := filepath.Join(s.path, entry.Name())
		templateFile := filepath.Join(templatePath, TemplateFilename)

		// Check if TEMPLATE.md exists
		if _, err := os.Stat(templateFile); os.IsNotExist(err) {
			continue
		}

		// Parse template file
		tmpl, err := ParseTemplateFile(templateFile)
		if err != nil {
			s.logger.Warn("failed to parse template",
				"path", templatePath,
				"error", err)
			continue
		}

		// Set source metadata
		tmpl.Source = s.sourceType
		tmpl.SourcePriority = s.priority

		// Validate
		if err := ValidateTemplate(tmpl); err != nil {
			s.logger.Warn("invalid template",
				"path", templatePath,
				"error", err)
			continue
		}

		templates = append(templates, tmpl)
		s.logger.Debug("discovered template",
			"name", tmpl.Name,
			"path", templatePath)
	}

	s.logger.Info("discovered templates",
		"count", len(templates),
		"path", s.path)

	return templates, nil
}

// WatchPaths returns the directory to watch for template changes.
func (s *LocalSource) WatchPaths() []string {
	return []string{s.path}
}

// EmbeddedSource discovers templates from an embedded filesystem.
type EmbeddedSource struct {
	fs         fs.FS
	sourceType SourceType
	priority   int
	logger     *slog.Logger
}

// NewEmbeddedSource creates an embedded filesystem discovery source.
func NewEmbeddedSource(fsys fs.FS, sourceType SourceType, priority int) *EmbeddedSource {
	return &EmbeddedSource{
		fs:         fsys,
		sourceType: sourceType,
		priority:   priority,
		logger:     slog.Default().With("component", "templates", "source", sourceType),
	}
}

func (s *EmbeddedSource) Type() SourceType {
	return s.sourceType
}

func (s *EmbeddedSource) Priority() int {
	return s.priority
}

func (s *EmbeddedSource) Discover(ctx context.Context) ([]*AgentTemplate, error) {
	var templates []*AgentTemplate

	// Walk the embedded filesystem
	err := fs.WalkDir(s.fs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip non-directories and the root
		if !d.IsDir() || path == "." {
			return nil
		}

		// Check for TEMPLATE.md in this directory
		templateFile := filepath.Join(path, TemplateFilename)
		data, err := fs.ReadFile(s.fs, templateFile)
		if err != nil {
			// No TEMPLATE.md in this directory, continue walking
			return nil
		}

		// Parse the template
		tmpl, err := ParseTemplate(data, path)
		if err != nil {
			s.logger.Warn("failed to parse embedded template",
				"path", path,
				"error", err)
			return nil
		}

		// Set source metadata
		tmpl.Source = s.sourceType
		tmpl.SourcePriority = s.priority

		// Validate
		if err := ValidateTemplate(tmpl); err != nil {
			s.logger.Warn("invalid embedded template",
				"path", path,
				"error", err)
			return nil
		}

		templates = append(templates, tmpl)
		s.logger.Debug("discovered embedded template",
			"name", tmpl.Name,
			"path", path)

		// Don't descend into template directories
		return fs.SkipDir
	})

	if err != nil && err != fs.SkipDir {
		return nil, fmt.Errorf("walk embedded filesystem: %w", err)
	}

	s.logger.Info("discovered embedded templates", "count", len(templates))
	return templates, nil
}

// DiscoverAll discovers templates from multiple sources with precedence.
// Higher priority sources override lower priority ones on name conflicts.
func DiscoverAll(ctx context.Context, sources []DiscoverySource) ([]*AgentTemplate, error) {
	templateMap := make(map[string]*AgentTemplate)

	for _, source := range sources {
		templates, err := source.Discover(ctx)
		if err != nil {
			slog.Warn("template discovery failed",
				"source", source.Type(),
				"error", err)
			continue
		}

		for _, tmpl := range templates {
			existing, ok := templateMap[tmpl.Name]
			if !ok {
				templateMap[tmpl.Name] = tmpl
				continue
			}

			// Higher priority wins
			if tmpl.SourcePriority > existing.SourcePriority {
				slog.Debug("template override",
					"name", tmpl.Name,
					"oldSource", existing.Source,
					"newSource", tmpl.Source)
				templateMap[tmpl.Name] = tmpl
			}
		}
	}

	// Convert map to slice
	result := make([]*AgentTemplate, 0, len(templateMap))
	for _, tmpl := range templateMap {
		result = append(result, tmpl)
	}

	return result, nil
}

// DefaultSourcePriorities defines the default priority order.
// Higher numbers = higher priority (wins in conflicts).
const (
	PriorityExtra     = 10 // templates.load.extraDirs
	PriorityBuiltin   = 20 // Shipped with binary
	PriorityLocal     = 30 // ~/.nexus/templates/
	PriorityWorkspace = 40 // <workspace>/templates/
)

// BuildDefaultSources creates the default discovery sources.
func BuildDefaultSources(workspacePath, localPath string, extraDirs []string) []DiscoverySource {
	var sources []DiscoverySource

	// Extra directories (lowest priority)
	for _, dir := range extraDirs {
		sources = append(sources, NewLocalSource(dir, SourceExtra, PriorityExtra))
	}

	// Local templates (~/.nexus/templates/)
	if localPath != "" {
		sources = append(sources, NewLocalSource(localPath, SourceLocal, PriorityLocal))
	}

	// Workspace templates (highest priority)
	if workspacePath != "" {
		wsTemplates := filepath.Join(workspacePath, "templates")
		sources = append(sources, NewLocalSource(wsTemplates, SourceWorkspace, PriorityWorkspace))
	}

	return sources
}
