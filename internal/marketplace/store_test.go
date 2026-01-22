package marketplace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

func TestNewStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if store == nil {
		t.Fatal("expected non-nil store")
	}

	if store.BasePath() != tmpDir {
		t.Errorf("expected BasePath %s, got %s", tmpDir, store.BasePath())
	}
}

func TestStoreBasePath(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if store.BasePath() != tmpDir {
		t.Errorf("expected base path %s, got %s", tmpDir, store.BasePath())
	}
}

func TestStoreIndexPath(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	expected := filepath.Join(tmpDir, IndexFilename)
	if store.IndexPath() != expected {
		t.Errorf("expected index path %s, got %s", expected, store.IndexPath())
	}
}

func TestStorePluginPath(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	path := store.PluginPath("my-plugin")
	expected := filepath.Join(tmpDir, "my-plugin")
	if path != expected {
		t.Errorf("expected plugin path %s, got %s", expected, path)
	}
}

func TestStoreAddGet(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:          "test-plugin",
		Version:     "1.0.0",
		Path:        "/path/to/plugin",
		InstalledAt: time.Now(),
		Enabled:     true,
	}

	err = store.Add(plugin)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	retrieved, ok := store.Get("test-plugin")
	if !ok {
		t.Fatal("expected to find plugin")
	}

	if retrieved.ID != plugin.ID {
		t.Errorf("expected ID %s, got %s", plugin.ID, retrieved.ID)
	}
	if retrieved.Version != plugin.Version {
		t.Errorf("expected Version %s, got %s", plugin.Version, retrieved.Version)
	}
}

func TestStoreAddNil(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.Add(nil)
	if err == nil {
		t.Error("expected error for nil plugin")
	}
}

func TestStoreAddEmptyID(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID: "",
	}

	err = store.Add(plugin)
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestStoreIsInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if store.IsInstalled("nonexistent") {
		t.Error("expected IsInstalled to return false for nonexistent plugin")
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:      "test-plugin",
		Version: "1.0.0",
	}
	store.Add(plugin)

	if !store.IsInstalled("test-plugin") {
		t.Error("expected IsInstalled to return true for installed plugin")
	}
}

func TestStoreList(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Empty store
	list := store.List()
	if len(list) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(list))
	}

	// Add plugins
	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-1", Version: "1.0.0"})
	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-2", Version: "2.0.0"})

	list = store.List()
	if len(list) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(list))
	}
}

func TestStoreUpdate(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:      "test-plugin",
		Version: "1.0.0",
	}
	store.Add(plugin)

	plugin.Version = "2.0.0"
	err = store.Update(plugin)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	retrieved, _ := store.Get("test-plugin")
	if retrieved.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", retrieved.Version)
	}
}

func TestStoreUpdateNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:      "nonexistent",
		Version: "1.0.0",
	}

	err = store.Update(plugin)
	if err == nil {
		t.Error("expected error for updating nonexistent plugin")
	}
}

func TestStoreUpdateNil(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.Update(nil)
	if err == nil {
		t.Error("expected error for nil plugin")
	}
}

func TestStoreRemove(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:      "test-plugin",
		Version: "1.0.0",
	}
	store.Add(plugin)

	err = store.Remove("test-plugin")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if store.IsInstalled("test-plugin") {
		t.Error("expected plugin to be removed")
	}
}

func TestStoreRemoveNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.Remove("nonexistent")
	if err == nil {
		t.Error("expected error for removing nonexistent plugin")
	}
}

func TestStoreSetEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:      "test-plugin",
		Version: "1.0.0",
		Enabled: true,
	}
	store.Add(plugin)

	err = store.SetEnabled("test-plugin", false)
	if err != nil {
		t.Fatalf("SetEnabled() error = %v", err)
	}

	retrieved, _ := store.Get("test-plugin")
	if retrieved.Enabled {
		t.Error("expected plugin to be disabled")
	}
}

func TestStoreSetEnabledNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.SetEnabled("nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestStoreSetAutoUpdate(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:         "test-plugin",
		Version:    "1.0.0",
		AutoUpdate: false,
	}
	store.Add(plugin)

	err = store.SetAutoUpdate("test-plugin", true)
	if err != nil {
		t.Fatalf("SetAutoUpdate() error = %v", err)
	}

	retrieved, _ := store.Get("test-plugin")
	if !retrieved.AutoUpdate {
		t.Error("expected AutoUpdate to be true")
	}
}

func TestStoreSetAutoUpdateNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.SetAutoUpdate("nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestStoreSetConfig(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:      "test-plugin",
		Version: "1.0.0",
	}
	store.Add(plugin)

	config := map[string]any{"key": "value"}
	err = store.SetConfig("test-plugin", config)
	if err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}

	retrieved, _ := store.Get("test-plugin")
	if retrieved.Config["key"] != "value" {
		t.Error("expected config to be set")
	}
}

func TestStoreSetConfigNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.SetConfig("nonexistent", map[string]any{})
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestStoreGetSetRegistries(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	registries := []string{"https://registry1.dev", "https://registry2.dev"}
	err = store.SetRegistries(registries)
	if err != nil {
		t.Fatalf("SetRegistries() error = %v", err)
	}

	retrieved := store.GetRegistries()
	if len(retrieved) != 2 {
		t.Errorf("expected 2 registries, got %d", len(retrieved))
	}
}

func TestStoreReload(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	plugin := &pluginsdk.InstalledPlugin{
		ID:      "test-plugin",
		Version: "1.0.0",
	}
	store.Add(plugin)

	// Reload should preserve data
	err = store.Reload()
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	if !store.IsInstalled("test-plugin") {
		t.Error("expected plugin to persist after reload")
	}
}

func TestStoreEnsurePluginDir(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	dir, err := store.EnsurePluginDir("test-plugin")
	if err != nil {
		t.Fatalf("EnsurePluginDir() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestStoreRemovePluginDir(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Create plugin directory
	dir, _ := store.EnsurePluginDir("test-plugin")

	// Remove it
	err = store.RemovePluginDir("test-plugin")
	if err != nil {
		t.Fatalf("RemovePluginDir() error = %v", err)
	}

	_, err = os.Stat(dir)
	if !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func TestStorePluginDirExists(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if store.PluginDirExists("nonexistent") {
		t.Error("expected PluginDirExists to return false")
	}

	store.EnsurePluginDir("test-plugin")

	if !store.PluginDirExists("test-plugin") {
		t.Error("expected PluginDirExists to return true")
	}
}

func TestStoreWriteReadPluginFile(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	data := []byte("plugin file content")
	err = store.WritePluginFile("test-plugin", "test.txt", data, 0644)
	if err != nil {
		t.Fatalf("WritePluginFile() error = %v", err)
	}

	retrieved, err := store.ReadPluginFile("test-plugin", "test.txt")
	if err != nil {
		t.Fatalf("ReadPluginFile() error = %v", err)
	}

	if string(retrieved) != string(data) {
		t.Errorf("expected %s, got %s", string(data), string(retrieved))
	}
}

func TestStoreReadPluginFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.ReadPluginFile("nonexistent", "file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestStoreGetPluginsNeedingUpdate(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Add plugins
	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-1", Enabled: true, AutoUpdate: true})
	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-2", Enabled: true, AutoUpdate: false})
	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-3", Enabled: false, AutoUpdate: true})

	plugins := store.GetPluginsNeedingUpdate()

	// Only plugin-1 should be returned (enabled and auto-update)
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin needing update, got %d", len(plugins))
	}
	if len(plugins) > 0 && plugins[0].ID != "plugin-1" {
		t.Errorf("expected plugin-1, got %s", plugins[0].ID)
	}
}

func TestStoreGetEnabledPlugins(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(WithBasePath(tmpDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-1", Enabled: true})
	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-2", Enabled: false})
	store.Add(&pluginsdk.InstalledPlugin{ID: "plugin-3", Enabled: true})

	plugins := store.GetEnabledPlugins()

	if len(plugins) != 2 {
		t.Errorf("expected 2 enabled plugins, got %d", len(plugins))
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"my-plugin", "my-plugin"},
		{"../dangerous", "dangerous"}, // filepath.Clean then filepath.Base extracts safe name
		{".", "_invalid_"},
		{"..", "_invalid_"},
		{"", "_invalid_"},
		{"path/to/plugin", "plugin"}, // Only base name
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeID(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
