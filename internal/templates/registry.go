package templates

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Registry manages template discovery, loading, and access.
type Registry struct {
	sources []DiscoverySource
	config  *TemplatesConfig
	logger  *slog.Logger

	// All discovered templates
	templates   map[string]*AgentTemplate
	templatesMu sync.RWMutex

	// File watcher
	watcher       *fsnotify.Watcher
	watchPaths    map[string]struct{}
	watchMu       sync.Mutex
	watchWg       sync.WaitGroup
	watchCancel   context.CancelFunc
	watchDebounce time.Duration
}

// NewRegistry creates a new template registry.
func NewRegistry(cfg *TemplatesConfig, workspacePath string) (*Registry, error) {
	if cfg == nil {
		cfg = &TemplatesConfig{}
	}

	// Build default sources
	homeDir, _ := os.UserHomeDir()
	localPath := filepath.Join(homeDir, ".nexus", "templates")

	var extraDirs []string
	if cfg.Load != nil {
		extraDirs = cfg.Load.ExtraDirs
	}

	sources := BuildDefaultSources(workspacePath, localPath, extraDirs)

	// Add configured sources
	for _, srcCfg := range cfg.Sources {
		switch srcCfg.Type {
		case SourceLocal, SourceExtra:
			sources = append(sources, NewLocalSource(srcCfg.Path, srcCfg.Type, PriorityExtra))
			// TODO: Add git and registry sources
		}
	}

	watchDebounce := 250 * time.Millisecond
	if cfg.Load != nil && cfg.Load.WatchDebounceMs > 0 {
		watchDebounce = time.Duration(cfg.Load.WatchDebounceMs) * time.Millisecond
	}

	return &Registry{
		sources:       sources,
		config:        cfg,
		logger:        slog.Default().With("component", "templates"),
		templates:     make(map[string]*AgentTemplate),
		watchDebounce: watchDebounce,
	}, nil
}

// AddSource adds a discovery source to the registry.
func (r *Registry) AddSource(source DiscoverySource) {
	r.sources = append(r.sources, source)
}

// Discover scans all sources for templates.
func (r *Registry) Discover(ctx context.Context) error {
	templates, err := DiscoverAll(ctx, r.sources)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	r.templatesMu.Lock()
	r.templates = make(map[string]*AgentTemplate)
	for _, tmpl := range templates {
		// Check if enabled
		if r.config != nil && !tmpl.IsEnabled(r.config.Entries) {
			r.logger.Debug("template disabled", "name", tmpl.Name)
			continue
		}
		r.templates[tmpl.Name] = tmpl
	}
	r.templatesMu.Unlock()

	r.logger.Info("discovered templates", "count", len(r.templates))

	if err := r.refreshWatches(); err != nil {
		r.logger.Warn("refresh template watches failed", "error", err)
	}

	return nil
}

// Get returns a template by name.
func (r *Registry) Get(name string) (*AgentTemplate, bool) {
	r.templatesMu.RLock()
	defer r.templatesMu.RUnlock()
	tmpl, ok := r.templates[name]
	return tmpl, ok
}

// List returns all discovered templates.
func (r *Registry) List() []*AgentTemplate {
	r.templatesMu.RLock()
	defer r.templatesMu.RUnlock()

	result := make([]*AgentTemplate, 0, len(r.templates))
	for _, tmpl := range r.templates {
		result = append(result, tmpl)
	}
	sortTemplates(result)
	return result
}

// ListSnapshots returns lightweight snapshots of all templates.
func (r *Registry) ListSnapshots() []*TemplateSnapshot {
	templates := r.List()
	snapshots := make([]*TemplateSnapshot, len(templates))
	for i, tmpl := range templates {
		snapshots[i] = tmpl.ToSnapshot()
	}
	return snapshots
}

// ListByTag returns templates that have the specified tag.
func (r *Registry) ListByTag(tag string) []*AgentTemplate {
	r.templatesMu.RLock()
	defer r.templatesMu.RUnlock()

	var result []*AgentTemplate
	for _, tmpl := range r.templates {
		for _, t := range tmpl.Tags {
			if t == tag {
				result = append(result, tmpl)
				break
			}
		}
	}
	sortTemplates(result)
	return result
}

// Search returns templates matching the query.
func (r *Registry) Search(query string) []*AgentTemplate {
	r.templatesMu.RLock()
	defer r.templatesMu.RUnlock()

	var result []*AgentTemplate
	query = normalizeSearchQuery(query)

	for _, tmpl := range r.templates {
		if matchesQuery(tmpl, query) {
			result = append(result, tmpl)
		}
	}
	sortTemplates(result)
	return result
}

// LoadContent loads the full content of a template (lazy loading).
func (r *Registry) LoadContent(name string) (string, error) {
	tmpl, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("template not found: %s", name)
	}

	// Already loaded
	if tmpl.Content != "" {
		return tmpl.Content, nil
	}

	// Read from file
	templateFile := filepath.Join(tmpl.Path, TemplateFilename)
	parsed, err := ParseTemplateFile(templateFile)
	if err != nil {
		return "", fmt.Errorf("parse template file: %w", err)
	}

	// Update cached content
	r.templatesMu.Lock()
	tmpl.Content = parsed.Content
	r.templatesMu.Unlock()

	return tmpl.Content, nil
}

// Register adds or updates a template in the registry.
func (r *Registry) Register(tmpl *AgentTemplate) error {
	if err := ValidateTemplate(tmpl); err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}

	r.templatesMu.Lock()
	r.templates[tmpl.Name] = tmpl
	r.templatesMu.Unlock()

	r.logger.Debug("registered template", "name", tmpl.Name)
	return nil
}

// Unregister removes a template from the registry.
func (r *Registry) Unregister(name string) bool {
	r.templatesMu.Lock()
	defer r.templatesMu.Unlock()

	if _, ok := r.templates[name]; !ok {
		return false
	}

	delete(r.templates, name)
	r.logger.Debug("unregistered template", "name", name)
	return true
}

// StartWatching enables file watching for template changes.
func (r *Registry) StartWatching(ctx context.Context) error {
	if r.config == nil || r.config.Load == nil || !r.config.Load.Watch {
		return nil
	}

	r.watchMu.Lock()
	if r.watcher != nil {
		r.watchMu.Unlock()
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.watchMu.Unlock()
		return err
	}
	r.watcher = watcher
	if r.watchPaths == nil {
		r.watchPaths = make(map[string]struct{})
	}
	watchCtx, cancel := context.WithCancel(ctx)
	r.watchCancel = cancel
	debounce := r.watchDebounce
	r.watchMu.Unlock()

	if err := r.refreshWatches(); err != nil {
		r.logger.Warn("initial template watch refresh failed", "error", err)
	}

	r.watchWg.Add(1)
	go r.watchLoop(watchCtx, debounce)
	return nil
}

// Close stops any active watchers.
func (r *Registry) Close() error {
	r.watchMu.Lock()
	if r.watchCancel != nil {
		r.watchCancel()
		r.watchCancel = nil
	}
	watcher := r.watcher
	r.watcher = nil
	r.watchMu.Unlock()

	if watcher != nil {
		_ = watcher.Close()
	}
	r.watchWg.Wait()
	return nil
}

func (r *Registry) watchLoop(ctx context.Context, debounce time.Duration) {
	defer r.watchWg.Done()
	r.watchMu.Lock()
	watcher := r.watcher
	r.watchMu.Unlock()
	if watcher == nil {
		return
	}

	if debounce <= 0 {
		debounce = 250 * time.Millisecond
	}

	var mu sync.Mutex
	var timer *time.Timer
	scheduleRefresh := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, func() {
			if err := r.Discover(context.Background()); err != nil {
				r.logger.Warn("template discovery failed during watch refresh", "error", err)
			}
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = r.addWatchPath(event.Name)
					}
				}
				scheduleRefresh()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			r.logger.Warn("template watch error", "error", err)
		}
	}
}

func (r *Registry) refreshWatches() error {
	r.watchMu.Lock()
	watcher := r.watcher
	r.watchMu.Unlock()
	if watcher == nil {
		return nil
	}

	desired := r.computeWatchPaths()
	desiredSet := make(map[string]struct{}, len(desired))
	for _, path := range desired {
		desiredSet[path] = struct{}{}
	}

	r.watchMu.Lock()
	defer r.watchMu.Unlock()

	for path := range desiredSet {
		if _, ok := r.watchPaths[path]; ok {
			continue
		}
		if err := watcher.Add(path); err != nil {
			r.logger.Debug("failed to watch templates path", "path", path, "error", err)
			continue
		}
		r.watchPaths[path] = struct{}{}
	}

	for path := range r.watchPaths {
		if _, ok := desiredSet[path]; ok {
			continue
		}
		if err := watcher.Remove(path); err != nil {
			r.logger.Debug("failed to unwatch templates path", "path", path, "error", err)
		}
		delete(r.watchPaths, path)
	}

	return nil
}

func (r *Registry) addWatchPath(path string) error {
	cleaned, ok := normalizeWatchPath(path)
	if !ok {
		return nil
	}
	r.watchMu.Lock()
	watcher := r.watcher
	if watcher == nil {
		r.watchMu.Unlock()
		return nil
	}
	if _, exists := r.watchPaths[cleaned]; exists {
		r.watchMu.Unlock()
		return nil
	}
	r.watchMu.Unlock()

	if err := watcher.Add(cleaned); err != nil {
		return err
	}

	r.watchMu.Lock()
	r.watchPaths[cleaned] = struct{}{}
	r.watchMu.Unlock()
	return nil
}

func (r *Registry) computeWatchPaths() []string {
	paths := make(map[string]struct{})
	for _, source := range r.sources {
		if watchable, ok := source.(WatchableSource); ok {
			for _, path := range watchable.WatchPaths() {
				if cleaned, ok := normalizeWatchPath(path); ok {
					paths[cleaned] = struct{}{}
				}
			}
		}
	}
	r.templatesMu.RLock()
	for _, tmpl := range r.templates {
		if cleaned, ok := normalizeWatchPath(tmpl.Path); ok {
			paths[cleaned] = struct{}{}
		}
	}
	r.templatesMu.RUnlock()

	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func normalizeWatchPath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return filepath.Clean(path), true
}

// sortTemplates sorts templates alphabetically by name.
func sortTemplates(templates []*AgentTemplate) {
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
}

// normalizeSearchQuery normalizes a search query for matching.
func normalizeSearchQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// matchesQuery checks if a template matches the search query.
func matchesQuery(tmpl *AgentTemplate, query string) bool {
	// Check name
	if strings.Contains(strings.ToLower(tmpl.Name), query) {
		return true
	}

	// Check description
	if strings.Contains(strings.ToLower(tmpl.Description), query) {
		return true
	}

	// Check tags
	for _, tag := range tmpl.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}

	return false
}
