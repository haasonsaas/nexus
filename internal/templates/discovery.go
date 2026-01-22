package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// GitSource discovers templates from a Git repository.
type GitSource struct {
	URL             string
	Branch          string
	SubPath         string
	CacheDir        string
	RefreshInterval time.Duration

	priority   int
	logger     *slog.Logger
	mu         sync.Mutex
	lastPull   time.Time
}

// NewGitSource creates a Git repository discovery source.
func NewGitSource(repoURL, branch, subPath, cacheDir string, refreshInterval time.Duration, priority int) *GitSource {
	if branch == "" {
		branch = "main"
	}
	return &GitSource{
		URL:             repoURL,
		Branch:          branch,
		SubPath:         subPath,
		CacheDir:        cacheDir,
		RefreshInterval: refreshInterval,
		priority:        priority,
		logger:          slog.Default().With("component", "templates", "source", SourceGit),
	}
}

func (s *GitSource) Type() SourceType {
	return SourceGit
}

func (s *GitSource) Priority() int {
	return s.priority
}

// repoDir returns the local cache directory for this repository.
func (s *GitSource) repoDir() string {
	// Create a safe directory name from the URL
	safeName := strings.ReplaceAll(s.URL, "://", "_")
	safeName = strings.ReplaceAll(safeName, "/", "_")
	safeName = strings.ReplaceAll(safeName, ":", "_")
	safeName = strings.ReplaceAll(safeName, ".", "_")
	return filepath.Join(s.CacheDir, safeName)
}

// Discover clones or pulls the repository and scans for templates.
func (s *GitSource) Discover(ctx context.Context) ([]*AgentTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	repoPath := s.repoDir()

	// Check if repo exists
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Clone the repository
		if err := s.cloneRepo(ctx, repoPath); err != nil {
			return nil, fmt.Errorf("clone repository: %w", err)
		}
	} else {
		// Pull if refresh interval has passed
		if s.RefreshInterval > 0 && time.Since(s.lastPull) >= s.RefreshInterval {
			if err := s.pullRepo(ctx, repoPath); err != nil {
				s.logger.Warn("failed to pull repository, using cached version",
					"url", s.URL,
					"error", err)
			}
		}
	}

	// Determine the template directory to scan
	templateDir := repoPath
	if s.SubPath != "" {
		templateDir = filepath.Join(repoPath, s.SubPath)
	}

	// Check if directory exists
	info, err := os.Stat(templateDir)
	if os.IsNotExist(err) {
		s.logger.Debug("template directory does not exist in repository",
			"path", templateDir)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat template directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", templateDir)
	}

	// Scan for templates
	return s.scanTemplates(ctx, templateDir)
}

func (s *GitSource) cloneRepo(ctx context.Context, repoPath string) error {
	s.logger.Info("cloning git repository",
		"url", s.URL,
		"branch", s.Branch,
		"path", repoPath)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	args := []string{"clone", "--depth", "1", "--single-branch", "--branch", s.Branch, s.URL, repoPath}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

	s.lastPull = time.Now()
	return nil
}

func (s *GitSource) pullRepo(ctx context.Context, repoPath string) error {
	s.logger.Debug("pulling git repository",
		"url", s.URL,
		"path", repoPath)

	cmd := exec.CommandContext(ctx, "git", "pull", "--ff-only")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w: %s", err, string(output))
	}

	s.lastPull = time.Now()
	return nil
}

func (s *GitSource) scanTemplates(ctx context.Context, dir string) ([]*AgentTemplate, error) {
	entries, err := os.ReadDir(dir)
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

		templatePath := filepath.Join(dir, entry.Name())
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
		tmpl.Source = SourceGit
		tmpl.SourcePriority = s.priority

		// Validate
		if err := ValidateTemplate(tmpl); err != nil {
			s.logger.Warn("invalid template",
				"path", templatePath,
				"error", err)
			continue
		}

		templates = append(templates, tmpl)
		s.logger.Debug("discovered template from git",
			"name", tmpl.Name,
			"path", templatePath)
	}

	s.logger.Info("discovered templates from git repository",
		"count", len(templates),
		"url", s.URL)

	return templates, nil
}

// WatchPaths returns the cached repository directory.
func (s *GitSource) WatchPaths() []string {
	repoPath := s.repoDir()
	if s.SubPath != "" {
		return []string{filepath.Join(repoPath, s.SubPath)}
	}
	return []string{repoPath}
}

// RegistrySource discovers templates from an HTTP registry.
type RegistrySource struct {
	URL  string
	Auth string

	priority   int
	httpClient *http.Client
	logger     *slog.Logger
	cache      *registryTemplateCache
}

type registryTemplateCache struct {
	mu        sync.RWMutex
	templates []*AgentTemplate
	fetchedAt time.Time
	ttl       time.Duration
}

// RegistryTemplateMetadata represents template metadata from a registry.
type RegistryTemplateMetadata struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description"`
	Author      string   `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	DownloadURL string   `json:"download_url"`
}

// RegistryIndex represents the registry's template index.
type RegistryIndex struct {
	Templates []RegistryTemplateMetadata `json:"templates"`
	UpdatedAt time.Time                  `json:"updated_at,omitempty"`
}

// NewRegistrySource creates an HTTP registry discovery source.
func NewRegistrySource(registryURL, authToken string, priority int) *RegistrySource {
	return &RegistrySource{
		URL:      registryURL,
		Auth:     authToken,
		priority: priority,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: slog.Default().With("component", "templates", "source", SourceRegistry),
		cache: &registryTemplateCache{
			ttl: 15 * time.Minute,
		},
	}
}

func (s *RegistrySource) Type() SourceType {
	return SourceRegistry
}

func (s *RegistrySource) Priority() int {
	return s.priority
}

// Discover fetches template metadata from the registry and downloads template files.
func (s *RegistrySource) Discover(ctx context.Context) ([]*AgentTemplate, error) {
	// Check cache
	s.cache.mu.RLock()
	if s.cache.templates != nil && time.Since(s.cache.fetchedAt) < s.cache.ttl {
		templates := s.cache.templates
		s.cache.mu.RUnlock()
		s.logger.Debug("using cached registry templates", "registry", s.URL)
		return templates, nil
	}
	s.cache.mu.RUnlock()

	// Fetch registry index
	index, err := s.fetchIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch registry index: %w", err)
	}

	// Download and parse each template
	var templates []*AgentTemplate
	for _, meta := range index.Templates {
		select {
		case <-ctx.Done():
			return templates, ctx.Err()
		default:
		}

		tmpl, err := s.downloadTemplate(ctx, meta)
		if err != nil {
			s.logger.Warn("failed to download template",
				"name", meta.Name,
				"error", err)
			continue
		}

		// Set source metadata
		tmpl.Source = SourceRegistry
		tmpl.SourcePriority = s.priority

		// Validate
		if err := ValidateTemplate(tmpl); err != nil {
			s.logger.Warn("invalid template from registry",
				"name", meta.Name,
				"error", err)
			continue
		}

		templates = append(templates, tmpl)
		s.logger.Debug("discovered template from registry",
			"name", tmpl.Name)
	}

	// Update cache
	s.cache.mu.Lock()
	s.cache.templates = templates
	s.cache.fetchedAt = time.Now()
	s.cache.mu.Unlock()

	s.logger.Info("discovered templates from registry",
		"count", len(templates),
		"registry", s.URL)

	return templates, nil
}

func (s *RegistrySource) fetchIndex(ctx context.Context) (*RegistryIndex, error) {
	indexURL, err := url.JoinPath(s.URL, "templates", "index.json")
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nexus-templates/1.0")
	if s.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+s.Auth)
	}

	s.logger.Debug("fetching registry index", "url", indexURL)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch registry index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var index RegistryIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("decode registry index: %w", err)
	}

	return &index, nil
}

func (s *RegistrySource) downloadTemplate(ctx context.Context, meta RegistryTemplateMetadata) (*AgentTemplate, error) {
	if meta.DownloadURL == "" {
		return nil, fmt.Errorf("template %s has no download URL", meta.Name)
	}

	// Resolve relative URLs
	downloadURL := meta.DownloadURL
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		var err error
		downloadURL, err = url.JoinPath(s.URL, downloadURL)
		if err != nil {
			return nil, fmt.Errorf("resolve download URL: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "nexus-templates/1.0")
	if s.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+s.Auth)
	}

	s.logger.Debug("downloading template", "name", meta.Name, "url", downloadURL)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download template: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Limit download size (1MB max for a template)
	const maxSize = 1 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return nil, fmt.Errorf("read template content: %w", err)
	}

	// Parse the template
	tmpl, err := ParseTemplate(data, "")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	// Fill in metadata from registry if not present in template
	if tmpl.Version == "" {
		tmpl.Version = meta.Version
	}
	if tmpl.Author == "" {
		tmpl.Author = meta.Author
	}
	if tmpl.Homepage == "" {
		tmpl.Homepage = meta.Homepage
	}
	if len(tmpl.Tags) == 0 {
		tmpl.Tags = meta.Tags
	}

	return tmpl, nil
}
