package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Manager manages skill discovery, loading, and eligibility.
type Manager struct {
	sources []DiscoverySource
	config  *SkillsConfig
	logger  *slog.Logger

	// All discovered skills
	skills   map[string]*SkillEntry
	skillsMu sync.RWMutex

	// Eligible skills (after gating)
	eligible   map[string]*SkillEntry
	eligibleMu sync.RWMutex

	// Gating context
	gatingCtx *GatingContext

	watcher       *fsnotify.Watcher
	watchPaths    map[string]struct{}
	watchMu       sync.Mutex
	watchWg       sync.WaitGroup
	watchCancel   context.CancelFunc
	watchDebounce time.Duration
}

// NewManager creates a new skill manager.
func NewManager(cfg *SkillsConfig, workspacePath string, configValues map[string]any) (*Manager, error) {
	if cfg == nil {
		cfg = &SkillsConfig{}
	}

	// Build default sources
	homeDir, _ := os.UserHomeDir()
	localPath := filepath.Join(homeDir, ".nexus", "skills")

	var extraDirs []string
	if cfg.Load != nil {
		extraDirs = cfg.Load.ExtraDirs
	}

	sources := BuildDefaultSources(workspacePath, localPath, "", extraDirs)

	// Add configured sources
	for _, srcCfg := range cfg.Sources {
		switch srcCfg.Type {
		case SourceLocal, SourceExtra:
			sources = append(sources, NewLocalSource(srcCfg.Path, srcCfg.Type, PriorityExtra))
		case SourceGit:
			sources = append(sources, NewGitSource(
				srcCfg.URL,
				srcCfg.Branch,
				srcCfg.SubPath,
				"", // Use default cache dir
				srcCfg.Refresh,
				PriorityExtra,
			))
		case SourceRegistry:
			sources = append(sources, NewRegistrySource(
				srcCfg.URL,
				srcCfg.Auth,
				PriorityExtra,
			))
		}
	}

	watchDebounce := 250 * time.Millisecond
	if cfg.Load != nil && cfg.Load.WatchDebounceMs > 0 {
		watchDebounce = time.Duration(cfg.Load.WatchDebounceMs) * time.Millisecond
	}

	// Create gating context
	gatingCtx := NewGatingContext(cfg.Entries, configValues)

	return &Manager{
		sources:       sources,
		config:        cfg,
		logger:        slog.Default().With("component", "skills"),
		skills:        make(map[string]*SkillEntry),
		eligible:      make(map[string]*SkillEntry),
		gatingCtx:     gatingCtx,
		watchDebounce: watchDebounce,
	}, nil
}

// Discover scans all sources for skills.
func (m *Manager) Discover(ctx context.Context) error {
	skills, err := DiscoverAll(ctx, m.sources)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	m.skillsMu.Lock()
	m.skills = make(map[string]*SkillEntry)
	for _, skill := range skills {
		m.skills[skill.Name] = skill
	}
	m.skillsMu.Unlock()

	m.logger.Info("discovered skills", "count", len(skills))

	// Refresh eligible list
	if err := m.RefreshEligible(); err != nil {
		return err
	}

	if err := m.refreshWatches(); err != nil {
		m.logger.Warn("refresh skill watches failed", "error", err)
	}

	return nil
}

// RefreshEligible updates the list of eligible skills based on gating.
func (m *Manager) RefreshEligible() error {
	m.skillsMu.RLock()
	allSkills := make([]*SkillEntry, 0, len(m.skills))
	for _, skill := range m.skills {
		allSkills = append(allSkills, skill)
	}
	m.skillsMu.RUnlock()

	eligible := FilterEligible(allSkills, m.gatingCtx)

	m.eligibleMu.Lock()
	m.eligible = make(map[string]*SkillEntry)
	for _, skill := range eligible {
		m.eligible[skill.Name] = skill
	}
	m.eligibleMu.Unlock()

	m.logger.Info("eligible skills",
		"eligible", len(eligible),
		"total", len(allSkills))

	return nil
}

// GetSkill returns a skill by name (from all discovered skills).
func (m *Manager) GetSkill(name string) (*SkillEntry, bool) {
	m.skillsMu.RLock()
	defer m.skillsMu.RUnlock()
	skill, ok := m.skills[name]
	return skill, ok
}

// GetEligible returns an eligible skill by name.
func (m *Manager) GetEligible(name string) (*SkillEntry, bool) {
	m.eligibleMu.RLock()
	defer m.eligibleMu.RUnlock()
	skill, ok := m.eligible[name]
	return skill, ok
}

// ListAll returns all discovered skills.
func (m *Manager) ListAll() []*SkillEntry {
	m.skillsMu.RLock()
	defer m.skillsMu.RUnlock()

	result := make([]*SkillEntry, 0, len(m.skills))
	for _, skill := range m.skills {
		result = append(result, skill)
	}
	sortSkills(result)
	return result
}

// ListEligible returns all eligible skills.
func (m *Manager) ListEligible() []*SkillEntry {
	m.eligibleMu.RLock()
	defer m.eligibleMu.RUnlock()

	result := make([]*SkillEntry, 0, len(m.eligible))
	for _, skill := range m.eligible {
		result = append(result, skill)
	}
	sortSkills(result)
	return result
}

// ListSnapshots returns lightweight snapshots of eligible skills.
func (m *Manager) ListSnapshots() []*SkillSnapshot {
	eligible := m.ListEligible()
	snapshots := make([]*SkillSnapshot, len(eligible))
	for i, skill := range eligible {
		snapshots[i] = skill.ToSnapshot()
	}
	return snapshots
}

// LoadContent loads the full content of a skill (lazy loading).
func (m *Manager) LoadContent(name string) (string, error) {
	skill, ok := m.GetSkill(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}

	// Already loaded
	if skill.Content != "" {
		return skill.Content, nil
	}

	// Read from file
	skillFile := filepath.Join(skill.Path, SkillFilename)
	parsed, err := ParseSkillFile(skillFile)
	if err != nil {
		return "", fmt.Errorf("parse skill file: %w", err)
	}

	// Update cached content
	m.skillsMu.Lock()
	skill.Content = parsed.Content
	m.skillsMu.Unlock()

	return skill.Content, nil
}

// CheckEligibility checks if a skill is eligible and returns the reason if not.
func (m *Manager) CheckEligibility(name string) (*EligibilityResult, error) {
	skill, ok := m.GetSkill(name)
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	result := skill.CheckEligibility(m.gatingCtx)
	return &result, nil
}

// GetIneligibleReasons returns reasons for all ineligible skills.
func (m *Manager) GetIneligibleReasons() map[string]string {
	allSkills := m.ListAll()
	return GetIneligibleReasons(allSkills, m.gatingCtx)
}

// InjectEnv injects skill environment variables for a session.
// Returns a function to restore the original environment.
func (m *Manager) InjectEnv(skillNames []string) (restore func()) {
	originalEnv := make(map[string]string)
	injected := make(map[string]string)

	for _, name := range skillNames {
		skill, ok := m.GetEligible(name)
		if !ok {
			continue
		}

		cfg, ok := m.config.Entries[skill.ConfigKey()]
		if !ok {
			continue
		}

		// API key shorthand
		if cfg.APIKey != "" && skill.Metadata != nil && skill.Metadata.PrimaryEnv != "" {
			envVar := skill.Metadata.PrimaryEnv
			if _, exists := os.LookupEnv(envVar); !exists {
				originalEnv[envVar] = ""
				injected[envVar] = cfg.APIKey
			}
		}

		// Explicit env overrides
		for k, v := range cfg.Env {
			if _, exists := os.LookupEnv(k); !exists {
				originalEnv[k] = ""
				injected[k] = v
			}
		}
	}

	// Apply injections
	for k, v := range injected {
		os.Setenv(k, v)
	}

	// Return restore function
	return func() {
		for k := range injected {
			if original, ok := originalEnv[k]; ok && original == "" {
				os.Unsetenv(k)
			}
		}
	}
}

// StartWatching enables file watching for skill changes.
func (m *Manager) StartWatching(ctx context.Context) error {
	if m.config == nil || m.config.Load == nil || !m.config.Load.Watch {
		return nil
	}

	m.watchMu.Lock()
	if m.watcher != nil {
		m.watchMu.Unlock()
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		m.watchMu.Unlock()
		return err
	}
	m.watcher = watcher
	if m.watchPaths == nil {
		m.watchPaths = make(map[string]struct{})
	}
	watchCtx, cancel := context.WithCancel(ctx)
	m.watchCancel = cancel
	debounce := m.watchDebounce
	m.watchMu.Unlock()

	if err := m.refreshWatches(); err != nil {
		m.logger.Warn("initial skill watch refresh failed", "error", err)
	}

	m.watchWg.Add(1)
	go m.watchLoop(watchCtx, debounce)
	return nil
}

// Close stops any active watchers.
func (m *Manager) Close() error {
	m.watchMu.Lock()
	if m.watchCancel != nil {
		m.watchCancel()
		m.watchCancel = nil
	}
	watcher := m.watcher
	m.watcher = nil
	m.watchMu.Unlock()

	if watcher != nil {
		_ = watcher.Close()
	}
	m.watchWg.Wait()
	return nil
}

func (m *Manager) watchLoop(ctx context.Context, debounce time.Duration) {
	defer m.watchWg.Done()
	m.watchMu.Lock()
	watcher := m.watcher
	m.watchMu.Unlock()
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
			if err := m.Discover(context.Background()); err != nil {
				m.logger.Warn("skill discovery failed during watch refresh", "error", err)
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
						_ = m.addWatchPath(event.Name)
					}
				}
				scheduleRefresh()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			m.logger.Warn("skill watch error", "error", err)
		}
	}
}

func (m *Manager) refreshWatches() error {
	m.watchMu.Lock()
	watcher := m.watcher
	m.watchMu.Unlock()
	if watcher == nil {
		return nil
	}

	desired := m.computeWatchPaths()
	desiredSet := make(map[string]struct{}, len(desired))
	for _, path := range desired {
		desiredSet[path] = struct{}{}
	}

	m.watchMu.Lock()
	defer m.watchMu.Unlock()

	for path := range desiredSet {
		if _, ok := m.watchPaths[path]; ok {
			continue
		}
		if err := watcher.Add(path); err != nil {
			m.logger.Debug("failed to watch skills path", "path", path, "error", err)
			continue
		}
		m.watchPaths[path] = struct{}{}
	}

	for path := range m.watchPaths {
		if _, ok := desiredSet[path]; ok {
			continue
		}
		if err := watcher.Remove(path); err != nil {
			m.logger.Debug("failed to unwatch skills path", "path", path, "error", err)
		}
		delete(m.watchPaths, path)
	}

	return nil
}

func (m *Manager) addWatchPath(path string) error {
	cleaned, ok := normalizeWatchPath(path)
	if !ok {
		return nil
	}
	m.watchMu.Lock()
	watcher := m.watcher
	if watcher == nil {
		m.watchMu.Unlock()
		return nil
	}
	if _, exists := m.watchPaths[cleaned]; exists {
		m.watchMu.Unlock()
		return nil
	}
	m.watchMu.Unlock()

	if err := watcher.Add(cleaned); err != nil {
		return err
	}

	m.watchMu.Lock()
	m.watchPaths[cleaned] = struct{}{}
	m.watchMu.Unlock()
	return nil
}

func (m *Manager) computeWatchPaths() []string {
	paths := make(map[string]struct{})
	for _, source := range m.sources {
		if watchable, ok := source.(WatchableSource); ok {
			for _, path := range watchable.WatchPaths() {
				if cleaned, ok := normalizeWatchPath(path); ok {
					paths[cleaned] = struct{}{}
				}
			}
		}
	}
	m.skillsMu.RLock()
	for _, skill := range m.skills {
		if cleaned, ok := normalizeWatchPath(skill.Path); ok {
			paths[cleaned] = struct{}{}
		}
	}
	m.skillsMu.RUnlock()

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

// sortSkills sorts skills alphabetically by name.
func sortSkills(skills []*SkillEntry) {
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
}
