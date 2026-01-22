package marketplace

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

const (
	// PluginsDirName is the name of the plugins directory.
	PluginsDirName = "plugins"

	// IndexFilename is the name of the plugin index file.
	IndexFilename = "index.json"
)

// Store manages the local plugin store at ~/.nexus/plugins/.
type Store struct {
	basePath string
	index    *pluginsdk.PluginIndex
	mu       sync.RWMutex
	logger   *slog.Logger
}

// StoreOption configures a Store.
type StoreOption func(*Store)

// WithBasePath sets the base path for the store.
func WithBasePath(path string) StoreOption {
	return func(s *Store) {
		s.basePath = path
	}
}

// WithStoreLogger sets the logger for the store.
func WithStoreLogger(logger *slog.Logger) StoreOption {
	return func(s *Store) {
		s.logger = logger
	}
}

// NewStore creates a new local plugin store.
func NewStore(opts ...StoreOption) (*Store, error) {
	// Default base path
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home directory: %w", err)
	}

	s := &Store{
		basePath: filepath.Join(home, ".nexus", PluginsDirName),
		logger:   slog.Default().With("component", "marketplace.store"),
	}

	for _, opt := range opts {
		opt(s)
	}

	// Ensure directory exists
	if err := os.MkdirAll(s.basePath, 0o755); err != nil {
		return nil, fmt.Errorf("create plugins directory: %w", err)
	}

	// Load or create index
	if err := s.loadIndex(); err != nil {
		return nil, err
	}

	return s, nil
}

// BasePath returns the store's base path.
func (s *Store) BasePath() string {
	return s.basePath
}

// IndexPath returns the path to the index file.
func (s *Store) IndexPath() string {
	return filepath.Join(s.basePath, IndexFilename)
}

// PluginPath returns the path for a plugin.
func (s *Store) PluginPath(id string) string {
	// Sanitize ID for filesystem
	safeID := sanitizeID(id)
	return filepath.Join(s.basePath, safeID)
}

// loadIndex loads the plugin index from disk.
func (s *Store) loadIndex() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	indexPath := s.IndexPath()
	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		s.index = pluginsdk.NewPluginIndex()
		s.logger.Debug("created new plugin index")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}

	var index pluginsdk.PluginIndex
	if err := json.Unmarshal(data, &index); err != nil {
		// Corrupted index, back it up before recreating
		corruptPath := fmt.Sprintf("%s.corrupt-%s", indexPath, time.Now().Format("20060102-150405"))
		if renameErr := os.Rename(indexPath, corruptPath); renameErr != nil {
			s.logger.Warn("failed to back up corrupted index", "error", renameErr)
		} else {
			s.logger.Warn("backed up corrupted index", "path", corruptPath)
		}
		s.logger.Warn("corrupted index, creating new one", "error", err)
		s.index = pluginsdk.NewPluginIndex()
		return nil
	}

	if index.Plugins == nil {
		index.Plugins = make(map[string]*pluginsdk.InstalledPlugin)
	}

	s.index = &index
	s.logger.Debug("loaded plugin index", "plugins", len(index.Plugins))
	return nil
}

// saveIndex saves the plugin index to disk.
func (s *Store) saveIndex() error {
	s.index.LastUpdated = time.Now()

	data, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	indexPath := s.IndexPath()
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	s.logger.Debug("saved plugin index", "plugins", len(s.index.Plugins))
	return nil
}

// List returns all installed plugins.
func (s *Store) List() []*pluginsdk.InstalledPlugin {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*pluginsdk.InstalledPlugin, 0, len(s.index.Plugins))
	for _, plugin := range s.index.Plugins {
		result = append(result, plugin)
	}
	return result
}

// Get returns an installed plugin by ID.
func (s *Store) Get(id string) (*pluginsdk.InstalledPlugin, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	plugin, ok := s.index.Plugins[id]
	return plugin, ok
}

// IsInstalled checks if a plugin is installed.
func (s *Store) IsInstalled(id string) bool {
	_, ok := s.Get(id)
	return ok
}

// Add adds a plugin to the store.
func (s *Store) Add(plugin *pluginsdk.InstalledPlugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin is nil")
	}
	if plugin.ID == "" {
		return fmt.Errorf("plugin ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.index.Plugins[plugin.ID] = plugin
	return s.saveIndex()
}

// Update updates a plugin in the store.
func (s *Store) Update(plugin *pluginsdk.InstalledPlugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.index.Plugins[plugin.ID]; !ok {
		return fmt.Errorf("plugin not found: %s", plugin.ID)
	}

	plugin.UpdatedAt = time.Now()
	s.index.Plugins[plugin.ID] = plugin
	return s.saveIndex()
}

// Remove removes a plugin from the store.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.index.Plugins[id]; !ok {
		return fmt.Errorf("plugin not found: %s", id)
	}

	delete(s.index.Plugins, id)
	return s.saveIndex()
}

// SetEnabled enables or disables a plugin.
func (s *Store) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	plugin, ok := s.index.Plugins[id]
	if !ok {
		return fmt.Errorf("plugin not found: %s", id)
	}

	plugin.Enabled = enabled
	plugin.UpdatedAt = time.Now()
	return s.saveIndex()
}

// SetAutoUpdate enables or disables auto-update for a plugin.
func (s *Store) SetAutoUpdate(id string, autoUpdate bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	plugin, ok := s.index.Plugins[id]
	if !ok {
		return fmt.Errorf("plugin not found: %s", id)
	}

	plugin.AutoUpdate = autoUpdate
	plugin.UpdatedAt = time.Now()
	return s.saveIndex()
}

// SetConfig sets the configuration for a plugin.
func (s *Store) SetConfig(id string, config map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	plugin, ok := s.index.Plugins[id]
	if !ok {
		return fmt.Errorf("plugin not found: %s", id)
	}

	plugin.Config = config
	plugin.UpdatedAt = time.Now()
	return s.saveIndex()
}

// GetRegistries returns the configured registries.
func (s *Store) GetRegistries() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.Registries
}

// SetRegistries sets the configured registries.
func (s *Store) SetRegistries(registries []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.index.Registries = registries
	return s.saveIndex()
}

// Reload reloads the index from disk.
func (s *Store) Reload() error {
	return s.loadIndex()
}

// EnsurePluginDir ensures the plugin directory exists.
func (s *Store) EnsurePluginDir(id string) (string, error) {
	dir := s.PluginPath(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create plugin directory: %w", err)
	}
	return dir, nil
}

// RemovePluginDir removes the plugin directory.
func (s *Store) RemovePluginDir(id string) error {
	dir := s.PluginPath(id)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove plugin directory: %w", err)
	}
	return nil
}

// GetPluginsNeedingUpdate returns plugins with auto-update enabled.
func (s *Store) GetPluginsNeedingUpdate() []*pluginsdk.InstalledPlugin {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*pluginsdk.InstalledPlugin
	for _, plugin := range s.index.Plugins {
		if plugin.AutoUpdate && plugin.Enabled {
			result = append(result, plugin)
		}
	}
	return result
}

// GetEnabledPlugins returns all enabled plugins.
func (s *Store) GetEnabledPlugins() []*pluginsdk.InstalledPlugin {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*pluginsdk.InstalledPlugin
	for _, plugin := range s.index.Plugins {
		if plugin.Enabled {
			result = append(result, plugin)
		}
	}
	return result
}

// sanitizeID sanitizes a plugin ID for filesystem use.
func sanitizeID(id string) string {
	// Replace / with --
	safe := filepath.Clean(id)
	safe = filepath.Base(safe)
	if safe == "." || safe == ".." || safe == "" {
		return "_invalid_"
	}
	return safe
}

// PluginDirExists checks if a plugin directory exists.
func (s *Store) PluginDirExists(id string) bool {
	dir := s.PluginPath(id)
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// WritePluginFile writes a file to the plugin directory.
func (s *Store) WritePluginFile(id, filename string, data []byte, perm os.FileMode) error {
	dir, err := s.EnsurePluginDir(id)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("write plugin file: %w", err)
	}
	return nil
}

// ReadPluginFile reads a file from the plugin directory.
func (s *Store) ReadPluginFile(id, filename string) ([]byte, error) {
	path := filepath.Join(s.PluginPath(id), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin file: %w", err)
	}
	return data, nil
}
