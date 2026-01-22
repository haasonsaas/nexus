package marketplace

import (
	"testing"
)

func TestFormatPluginID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple-plugin", "simple-plugin"},
		{"org/plugin", "plugin"},
		{"@scope/plugin", "plugin"},
		{"org/sub/plugin", "plugin"},
		{"plugin", "plugin"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := FormatPluginID(tt.input)
			if result != tt.expected {
				t.Errorf("FormatPluginID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidatePluginID(t *testing.T) {
	tests := []struct {
		input     string
		wantError bool
	}{
		{"valid-plugin", false},
		{"my_plugin", false},
		{"plugin123", false},
		{"org/plugin", false},
		{"", true},
		{"   ", true},
		{`plugin\bad`, true},
		{"plugin:bad", true},
		{"plugin*bad", true},
		{"plugin?bad", true},
		{`plugin"bad`, true},
		{"plugin<bad", true},
		{"plugin>bad", true},
		{"plugin|bad", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidatePluginID(tt.input)
			if tt.wantError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewManagerNilConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create manager with minimal config
	cfg := &ManagerConfig{
		BasePath: tmpDir,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestNewManagerWithConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{
		BasePath:   tmpDir,
		Registries: []string{"https://custom.registry.dev"},
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	registries := mgr.GetRegistries()
	if len(registries) != 1 || registries[0] != "https://custom.registry.dev" {
		t.Errorf("expected custom registry, got %v", registries)
	}
}

func TestManagerList(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	list := mgr.List()
	if list == nil {
		t.Error("expected non-nil list")
	}
}

func TestManagerGet(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	_, ok := mgr.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for nonexistent plugin")
	}
}

func TestManagerIsInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if mgr.IsInstalled("nonexistent") {
		t.Error("expected IsInstalled to return false for nonexistent plugin")
	}
}

func TestManagerEnableDisable(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Enable/disable on nonexistent should error
	err = mgr.Enable("nonexistent")
	if err == nil {
		t.Error("expected error for enabling nonexistent plugin")
	}

	err = mgr.Disable("nonexistent")
	if err == nil {
		t.Error("expected error for disabling nonexistent plugin")
	}
}

func TestManagerSetAutoUpdate(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = mgr.SetAutoUpdate("nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestManagerSetConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = mgr.SetConfig("nonexistent", map[string]any{"key": "value"})
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestManagerAddRegistry(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = mgr.AddRegistry("https://new.registry.dev")
	if err != nil {
		t.Fatalf("AddRegistry() error = %v", err)
	}

	registries := mgr.GetRegistries()
	found := false
	for _, r := range registries {
		if r == "https://new.registry.dev" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected new registry to be added")
	}
}

func TestManagerClearCache(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Should not panic
	mgr.ClearCache()
}

func TestManagerGetEnabledPlugins(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	plugins := mgr.GetEnabledPlugins()
	// Empty list may be nil or empty slice, both are valid
	if len(plugins) != 0 {
		t.Error("expected empty plugins list")
	}
}

func TestManagerGetStore(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	store := mgr.GetStore()
	if store == nil {
		t.Error("expected non-nil store")
	}
}

func TestManagerGetRegistry(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	registry := mgr.GetRegistry()
	if registry == nil {
		t.Error("expected non-nil registry")
	}
}

func TestManagerInfo(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	info := mgr.Info()
	if info == nil {
		t.Fatal("expected non-nil info")
	}

	if info.StorePath != tmpDir {
		t.Errorf("expected StorePath %s, got %s", tmpDir, info.StorePath)
	}

	if info.Platform == "" {
		t.Error("expected Platform to be set")
	}
}

func TestManagerReload(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ManagerConfig{BasePath: tmpDir}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = mgr.Reload()
	if err != nil {
		t.Errorf("Reload() error = %v", err)
	}
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://example.com/plugin.tar.gz", "tar.gz"},
		{"https://example.com/plugin.tgz", "tar.gz"},
		{"https://example.com/plugin.zip", "zip"},
		{"https://example.com/plugin.so", "so"},
		{"https://example.com/plugin", ""},
		{"https://example.com/PLUGIN.TAR.GZ", "tar.gz"}, // Case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := detectFormat(tt.url)
			if result != tt.expected {
				t.Errorf("detectFormat(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}
