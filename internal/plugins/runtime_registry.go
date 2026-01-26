package plugins

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
	"github.com/spf13/cobra"
)

var defaultRuntimeRegistry = NewRuntimeRegistry()

// DefaultRuntimeRegistry returns the process-wide runtime plugin registry.
func DefaultRuntimeRegistry() *RuntimeRegistry {
	return defaultRuntimeRegistry
}

// RegisterRuntimePlugin registers a runtime plugin into the default registry.
func RegisterRuntimePlugin(plugin pluginsdk.RuntimePlugin) error {
	return defaultRuntimeRegistry.Register(plugin)
}

type runtimeEntry struct {
	id           string
	plugin       pluginsdk.RuntimePlugin
	manifest     *pluginsdk.Manifest
	loader       func() (pluginsdk.RuntimePlugin, error)
	loadOnce     sync.Once
	loadErr      error
	loaded       pluginsdk.RuntimePlugin
	channelsOnce sync.Once
	channelsErr  error
	toolsOnce    sync.Once
	toolsErr     error
	cliOnce      sync.Once
	cliErr       error
	servicesOnce sync.Once
	servicesErr  error
	hooksOnce    sync.Once
	hooksErr     error
}

// RuntimeRegistry manages runtime plugin loading and registration.
type RuntimeRegistry struct {
	mu      sync.Mutex
	plugins map[string]*runtimeEntry
}

// NewRuntimeRegistry creates a runtime plugin registry.
func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{
		plugins: make(map[string]*runtimeEntry),
	}
}

// Register adds an in-process runtime plugin.
func (r *RuntimeRegistry) Register(plugin pluginsdk.RuntimePlugin) error {
	if plugin == nil {
		return fmt.Errorf("runtime plugin is nil")
	}
	manifest := plugin.Manifest()
	if manifest == nil {
		return fmt.Errorf("runtime plugin manifest is nil")
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	id := manifest.ID
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.plugins[id]
	if !ok {
		entry = &runtimeEntry{id: id}
		r.plugins[id] = entry
	}
	if entry.id == "" {
		entry.id = id
	}
	entry.plugin = plugin
	entry.manifest = manifest
	entry.loaded = plugin
	return nil
}

// LoadChannels registers channel adapters from enabled runtime plugins.
func (r *RuntimeRegistry) LoadChannels(cfg *config.Config, registry *channels.Registry) error {
	if cfg == nil || registry == nil {
		return nil
	}
	for id, entry := range cfg.Plugins.Entries {
		if !entry.Enabled {
			continue
		}
		pluginEntry := r.ensureEntry(id, entry.Path)
		plugin, err := pluginEntry.load(entry.Path)
		if err != nil {
			return err
		}
		pluginEntry.channelsOnce.Do(func() {
			var allowedChannels []string
			if manifest := plugin.Manifest(); manifest != nil {
				allowedChannels = manifest.Channels
			}
			api := &runtimeChannelRegistry{
				registry: registry,
				pluginID: id,
				allowed:  allowSet(allowedChannels),
			}
			pluginEntry.channelsErr = plugin.RegisterChannels(api, normalizeConfig(entry.Config))
		})
		if pluginEntry.channelsErr != nil {
			return pluginEntry.channelsErr
		}
	}
	return nil
}

// LoadTools registers tools from enabled runtime plugins.
func (r *RuntimeRegistry) LoadTools(cfg *config.Config, runtime *agent.Runtime) error {
	if cfg == nil || runtime == nil {
		return nil
	}
	for id, entry := range cfg.Plugins.Entries {
		if !entry.Enabled {
			continue
		}
		pluginEntry := r.ensureEntry(id, entry.Path)
		plugin, err := pluginEntry.load(entry.Path)
		if err != nil {
			return err
		}
		pluginEntry.toolsOnce.Do(func() {
			var allowedTools []string
			if manifest := plugin.Manifest(); manifest != nil {
				allowedTools = manifest.Tools
			}
			api := &runtimeToolRegistry{
				runtime:  runtime,
				pluginID: id,
				allowed:  allowSet(allowedTools),
			}
			pluginEntry.toolsErr = plugin.RegisterTools(api, normalizeConfig(entry.Config))
		})
		if pluginEntry.toolsErr != nil {
			return pluginEntry.toolsErr
		}
	}
	return nil
}

// LoadCLI registers CLI commands from enabled runtime plugins.
func (r *RuntimeRegistry) LoadCLI(cfg *config.Config, rootCmd *cobra.Command, logger *slog.Logger) error {
	if cfg == nil || rootCmd == nil {
		return nil
	}
	for id, entry := range cfg.Plugins.Entries {
		if !entry.Enabled {
			continue
		}
		pluginEntry := r.ensureEntry(id, entry.Path)
		plugin, err := pluginEntry.load(entry.Path)
		if err != nil {
			return err
		}

		// Check if plugin supports CLI registration
		extPlugin, ok := plugin.(pluginsdk.ExtendedPlugin)
		if !ok {
			continue
		}

		pluginEntry.cliOnce.Do(func() {
			api := &runtimeCLIRegistry{rootCmd: rootCmd, pluginID: id}
			pluginEntry.cliErr = extPlugin.RegisterCLI(api, normalizeConfig(entry.Config))
		})
		if pluginEntry.cliErr != nil {
			if logger != nil {
				logger.Warn("plugin CLI registration failed", "plugin_id", id, "error", pluginEntry.cliErr)
			}
			// Don't fail the entire load for CLI registration errors
		}
	}
	return nil
}

// LoadServices registers services from enabled runtime plugins.
func (r *RuntimeRegistry) LoadServices(cfg *config.Config, manager *ServiceManager, logger *slog.Logger) error {
	if cfg == nil || manager == nil {
		return nil
	}
	for id, entry := range cfg.Plugins.Entries {
		if !entry.Enabled {
			continue
		}
		pluginEntry := r.ensureEntry(id, entry.Path)
		plugin, err := pluginEntry.load(entry.Path)
		if err != nil {
			return err
		}

		// Check if plugin supports service registration
		extPlugin, ok := plugin.(pluginsdk.ExtendedPlugin)
		if !ok {
			continue
		}

		pluginEntry.servicesOnce.Do(func() {
			api := &runtimeServiceRegistry{manager: manager, pluginID: id}
			pluginEntry.servicesErr = extPlugin.RegisterServices(api, normalizeConfig(entry.Config))
		})
		if pluginEntry.servicesErr != nil {
			if logger != nil {
				logger.Warn("plugin service registration failed", "plugin_id", id, "error", pluginEntry.servicesErr)
			}
		}
	}
	return nil
}

// LoadHooks registers hooks from enabled runtime plugins.
func (r *RuntimeRegistry) LoadHooks(cfg *config.Config, registry *hooks.Registry, logger *slog.Logger) error {
	if cfg == nil || registry == nil {
		return nil
	}
	for id, entry := range cfg.Plugins.Entries {
		if !entry.Enabled {
			continue
		}
		pluginEntry := r.ensureEntry(id, entry.Path)
		plugin, err := pluginEntry.load(entry.Path)
		if err != nil {
			return err
		}

		// Check if plugin supports hook registration
		extPlugin, ok := plugin.(pluginsdk.ExtendedPlugin)
		if !ok {
			continue
		}

		pluginEntry.hooksOnce.Do(func() {
			api := &runtimeHookRegistry{registry: registry, pluginID: id}
			pluginEntry.hooksErr = extPlugin.RegisterHooks(api, normalizeConfig(entry.Config))
		})
		if pluginEntry.hooksErr != nil {
			if logger != nil {
				logger.Warn("plugin hook registration failed", "plugin_id", id, "error", pluginEntry.hooksErr)
			}
		}
	}
	return nil
}

// LoadFullPlugins loads plugins implementing the FullPlugin interface with unified API.
func (r *RuntimeRegistry) LoadFullPlugins(cfg *config.Config, api *PluginAPIBuilder) error {
	if cfg == nil || api == nil {
		return nil
	}
	for id, entry := range cfg.Plugins.Entries {
		if !entry.Enabled {
			continue
		}
		pluginEntry := r.ensureEntry(id, entry.Path)
		plugin, err := pluginEntry.load(entry.Path)
		if err != nil {
			return err
		}

		// Check if plugin implements FullPlugin interface
		fullPlugin, ok := plugin.(pluginsdk.FullPlugin)
		if !ok {
			continue
		}

		pluginAPI := api.Build(id, normalizeConfig(entry.Config))
		if err := fullPlugin.Register(pluginAPI); err != nil {
			if api.Logger != nil {
				api.Logger.Warn("full plugin registration failed", "plugin_id", id, "error", err)
			}
		}
	}
	return nil
}

// PluginAPIBuilder helps construct PluginAPI instances for plugins.
type PluginAPIBuilder struct {
	Channels       *channels.Registry
	Tools          *agent.Runtime
	RootCmd        *cobra.Command
	ServiceManager *ServiceManager
	HookRegistry   *hooks.Registry
	Logger         *slog.Logger
	WorkspaceDir   string
}

// Build creates a PluginAPI for a specific plugin.
func (b *PluginAPIBuilder) Build(pluginID string, cfg map[string]any) *pluginsdk.PluginAPI {
	pluginLogger := b.Logger
	if pluginLogger == nil {
		pluginLogger = slog.Default()
	}
	pluginLogger = pluginLogger.With("plugin_id", pluginID)

	return &pluginsdk.PluginAPI{
		Channels: &runtimeChannelRegistry{registry: b.Channels},
		Tools:    &runtimeToolRegistry{runtime: b.Tools},
		CLI:      &runtimeCLIRegistry{rootCmd: b.RootCmd, pluginID: pluginID},
		Services: &runtimeServiceRegistry{manager: b.ServiceManager, pluginID: pluginID},
		Hooks:    &runtimeHookRegistry{registry: b.HookRegistry, pluginID: pluginID},
		Config:   cfg,
		Logger:   &pluginLoggerAdapter{logger: pluginLogger},
		ResolvePath: func(path string) string {
			if filepath.IsAbs(path) {
				return path
			}
			return filepath.Join(b.WorkspaceDir, path)
		},
	}
}

func (r *RuntimeRegistry) ensureEntry(id, path string) *runtimeEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.plugins[id]
	if !ok {
		entry = &runtimeEntry{}
		r.plugins[id] = entry
	}
	if entry.loader == nil && entry.plugin == nil && strings.TrimSpace(path) != "" {
		entry.loader = func() (pluginsdk.RuntimePlugin, error) {
			return loadRuntimePlugin(resolvePluginBinary(path, id))
		}
	}
	return entry
}

func (e *runtimeEntry) load(path string) (pluginsdk.RuntimePlugin, error) {
	e.loadOnce.Do(func() {
		if e.plugin != nil {
			e.loaded = e.plugin
			return
		}
		if e.loader == nil {
			e.loadErr = fmt.Errorf("runtime plugin %q not registered", path)
			return
		}
		e.loaded, e.loadErr = e.loader()
	})
	if e.loadErr != nil {
		return nil, e.loadErr
	}
	if e.loaded == nil {
		return nil, fmt.Errorf("runtime plugin not loaded")
	}
	if e.id != "" {
		if manifest := e.loaded.Manifest(); manifest != nil {
			if strings.TrimSpace(manifest.ID) != "" && manifest.ID != e.id {
				return nil, fmt.Errorf("runtime plugin id mismatch: expected %q got %q", e.id, manifest.ID)
			}
		}
	}
	return e.loaded, nil
}

func resolvePluginBinary(path string, id string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	// Validate path to prevent traversal attacks
	validatedPath, err := ValidatePluginPath(path)
	if err != nil {
		// Return empty on invalid path - will fail later with clear error
		return ""
	}
	path = validatedPath

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".so" {
		return path
	}
	if ext == ".json" {
		path = filepath.Dir(path)
	}
	if strings.HasSuffix(path, string(filepath.Separator)) {
		path = strings.TrimSuffix(path, string(filepath.Separator))
	}
	if path == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(path, "plugin.so"),
		filepath.Join(path, id+".so"),
	}
	if ext != "" && ext != ".so" && ext != ".json" {
		candidates = append([]string{path}, candidates...)
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

func normalizeConfig(cfg map[string]any) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	return cfg
}

func allowSet(values []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, v := range values {
		key := strings.TrimSpace(v)
		if key == "" {
			continue
		}
		out[key] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
