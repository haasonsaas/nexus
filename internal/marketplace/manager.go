package marketplace

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

// Manager provides the high-level marketplace API.
type Manager struct {
	store     *Store
	registry  *RegistryClient
	verifier  *Verifier
	installer *Installer
	logger    *slog.Logger
	mu        sync.RWMutex
}

// ManagerConfig configures the marketplace manager.
type ManagerConfig struct {
	// BasePath is the base path for the plugin store.
	BasePath string

	// Registries are the registry URLs.
	Registries []string

	// TrustedKeys are the trusted signing keys (name -> base64 public key).
	TrustedKeys map[string]string

	// Logger is the logger to use.
	Logger *slog.Logger
}

// NewManager creates a new marketplace manager.
func NewManager(cfg *ManagerConfig) (*Manager, error) {
	if cfg == nil {
		cfg = &ManagerConfig{}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default().With("component", "marketplace")
	}

	// Create store
	storeOpts := []StoreOption{WithStoreLogger(logger)}
	if cfg.BasePath != "" {
		storeOpts = append(storeOpts, WithBasePath(cfg.BasePath))
	}

	store, err := NewStore(storeOpts...)
	if err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}

	// Create registry client
	registryOpts := []RegistryClientOption{WithLogger(logger)}
	if len(cfg.Registries) > 0 {
		registryOpts = append(registryOpts, WithRegistries(cfg.Registries))
	} else if regs := store.GetRegistries(); len(regs) > 0 {
		registryOpts = append(registryOpts, WithRegistries(regs))
	}

	registry := NewRegistryClient(registryOpts...)

	// Create verifier
	verifierOpts := []VerifierOption{WithVerifierLogger(logger)}
	for name, key := range cfg.TrustedKeys {
		verifierOpts = append(verifierOpts, WithTrustedKeyBase64(name, key))
	}

	verifier := NewVerifier(verifierOpts...)

	// Create installer
	installer := NewInstaller(store, registry, verifier, WithInstallerLogger(logger))

	return &Manager{
		store:     store,
		registry:  registry,
		verifier:  verifier,
		installer: installer,
		logger:    logger,
	}, nil
}

// Search searches for plugins in the marketplace.
func (m *Manager) Search(ctx context.Context, query string, opts SearchOptions) ([]*pluginsdk.PluginSearchResult, error) {
	results, err := m.registry.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	// Augment with installation status
	for _, result := range results {
		if installed, ok := m.store.Get(result.Plugin.ID); ok {
			result.Installed = true
			result.InstalledVersion = installed.Version
			result.UpdateAvailable = installed.Version != result.Plugin.Version
		}
	}

	return results, nil
}

// GetPlugin gets a plugin manifest from the marketplace.
func (m *Manager) GetPlugin(ctx context.Context, id string) (*pluginsdk.MarketplaceManifest, error) {
	manifest, _, err := m.registry.GetPlugin(ctx, id)
	return manifest, err
}

// Install installs a plugin from the marketplace.
func (m *Manager) Install(ctx context.Context, id string, opts pluginsdk.InstallOptions) (*InstallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installer.Install(ctx, id, opts)
}

// Update updates an installed plugin.
func (m *Manager) Update(ctx context.Context, id string, opts pluginsdk.UpdateOptions) (*InstallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installer.Update(ctx, id, opts)
}

// UpdateAll updates all plugins with auto-update enabled.
func (m *Manager) UpdateAll(ctx context.Context) ([]*InstallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installer.UpdateAll(ctx)
}

// Uninstall removes an installed plugin.
func (m *Manager) Uninstall(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installer.Uninstall(ctx, id)
}

// Verify verifies an installed plugin's integrity.
func (m *Manager) Verify(ctx context.Context, id string) (*VerificationResult, error) {
	return m.installer.VerifyInstalled(ctx, id)
}

// List returns all installed plugins.
func (m *Manager) List() []*pluginsdk.InstalledPlugin {
	return m.store.List()
}

// Get returns an installed plugin by ID.
func (m *Manager) Get(id string) (*pluginsdk.InstalledPlugin, bool) {
	return m.store.Get(id)
}

// IsInstalled checks if a plugin is installed.
func (m *Manager) IsInstalled(id string) bool {
	return m.store.IsInstalled(id)
}

// Enable enables a plugin.
func (m *Manager) Enable(id string) error {
	return m.store.SetEnabled(id, true)
}

// Disable disables a plugin.
func (m *Manager) Disable(id string) error {
	return m.store.SetEnabled(id, false)
}

// SetAutoUpdate enables or disables auto-update for a plugin.
func (m *Manager) SetAutoUpdate(id string, autoUpdate bool) error {
	return m.store.SetAutoUpdate(id, autoUpdate)
}

// SetConfig sets the configuration for a plugin.
func (m *Manager) SetConfig(id string, config map[string]any) error {
	return m.store.SetConfig(id, config)
}

// CheckUpdates checks for available updates.
func (m *Manager) CheckUpdates(ctx context.Context) (map[string]string, error) {
	return m.installer.CheckUpdates(ctx)
}

// GetRegistries returns the configured registries.
func (m *Manager) GetRegistries() []string {
	return m.registry.Registries()
}

// AddRegistry adds a registry.
func (m *Manager) AddRegistry(url string) error {
	m.registry.AddRegistry(url)
	return m.store.SetRegistries(m.registry.Registries())
}

// ClearCache clears the registry cache.
func (m *Manager) ClearCache() {
	m.registry.ClearCache()
}

// GetEnabledPlugins returns all enabled plugins.
func (m *Manager) GetEnabledPlugins() []*pluginsdk.InstalledPlugin {
	return m.store.GetEnabledPlugins()
}

// GetStore returns the underlying store (for advanced use).
func (m *Manager) GetStore() *Store {
	return m.store
}

// GetRegistry returns the underlying registry client (for advanced use).
func (m *Manager) GetRegistry() *RegistryClient {
	return m.registry
}

// Info returns marketplace status information.
func (m *Manager) Info() *MarketplaceInfo {
	installed := m.store.List()
	enabled := 0
	autoUpdate := 0
	for _, p := range installed {
		if p.Enabled {
			enabled++
		}
		if p.AutoUpdate {
			autoUpdate++
		}
	}

	return &MarketplaceInfo{
		StorePath:       m.store.BasePath(),
		Registries:      m.registry.Registries(),
		InstalledCount:  len(installed),
		EnabledCount:    enabled,
		AutoUpdateCount: autoUpdate,
		HasTrustedKeys:  m.verifier.HasTrustedKeys(),
		TrustedKeyNames: m.verifier.TrustedKeyNames(),
		Platform:        fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// MarketplaceInfo contains marketplace status information.
type MarketplaceInfo struct {
	// StorePath is the path to the plugin store.
	StorePath string

	// Registries are the configured registry URLs.
	Registries []string

	// InstalledCount is the number of installed plugins.
	InstalledCount int

	// EnabledCount is the number of enabled plugins.
	EnabledCount int

	// AutoUpdateCount is the number of plugins with auto-update enabled.
	AutoUpdateCount int

	// HasTrustedKeys indicates if trusted keys are configured.
	HasTrustedKeys bool

	// TrustedKeyNames are the names of trusted keys.
	TrustedKeyNames []string

	// Platform is the current platform (os/arch).
	Platform string
}

// PluginInfo returns detailed information about a plugin.
func (m *Manager) PluginInfo(ctx context.Context, id string) (*PluginInfoResult, error) {
	result := &PluginInfoResult{
		ID: id,
	}

	// Check if installed
	if installed, ok := m.store.Get(id); ok {
		result.Installed = installed
	}

	// Try to get from registry
	manifest, source, err := m.registry.GetPlugin(ctx, id)
	if err == nil {
		result.Manifest = manifest
		result.Source = source

		// Check compatibility
		artifact := GetArtifactForPlatform(manifest)
		result.Compatible = artifact != nil

		// Check if update available
		if result.Installed != nil {
			result.UpdateAvailable = result.Installed.Version != manifest.Version
		}
	}

	return result, nil
}

// PluginInfoResult contains detailed plugin information.
type PluginInfoResult struct {
	// ID is the plugin ID.
	ID string

	// Installed is the installed plugin info, if installed.
	Installed *pluginsdk.InstalledPlugin

	// Manifest is the marketplace manifest, if available.
	Manifest *pluginsdk.MarketplaceManifest

	// Source is the registry URL where the plugin was found.
	Source string

	// Compatible indicates if the plugin is compatible with the current platform.
	Compatible bool

	// UpdateAvailable indicates if an update is available.
	UpdateAvailable bool
}

// Reload reloads the store index from disk.
func (m *Manager) Reload() error {
	return m.store.Reload()
}

// FormatPluginID formats a plugin ID for display.
func FormatPluginID(id string) string {
	// Remove org prefix if present
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		return id[idx+1:]
	}
	return id
}

// ValidatePluginID validates a plugin ID.
func ValidatePluginID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("plugin ID is required")
	}
	if strings.ContainsAny(id, "\\:*?\"<>|") {
		return fmt.Errorf("plugin ID contains invalid characters")
	}
	return nil
}
