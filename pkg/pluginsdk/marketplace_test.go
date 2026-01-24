package pluginsdk

import (
	"testing"
	"time"
)

func TestNewPluginIndex(t *testing.T) {
	idx := NewPluginIndex()

	if idx.Version != "1" {
		t.Errorf("Version = %q, want %q", idx.Version, "1")
	}
	if idx.Plugins == nil {
		t.Error("Plugins should not be nil")
	}
	if len(idx.Plugins) != 0 {
		t.Errorf("len(Plugins) = %d, want 0", len(idx.Plugins))
	}
	if idx.LastUpdated.IsZero() {
		t.Error("LastUpdated should not be zero")
	}
}

func TestMarketplaceManifestStruct(t *testing.T) {
	manifest := MarketplaceManifest{
		ID:          "org/test-plugin",
		Name:        "Test Plugin",
		Description: "A test plugin for testing",
		Version:     "1.2.3",
		Author:      "Test Author",
		Homepage:    "https://example.com",
		License:     "MIT",
		Keywords:    []string{"test", "plugin"},
		Categories:  []string{"tools"},
		Requires: &PluginRequirements{
			NexusVersion: ">=1.0.0",
			Go:           "1.21",
			OS:           []string{"linux", "darwin"},
			Arch:         []string{"amd64", "arm64"},
		},
		Artifacts: []PluginArtifact{
			{
				OS:       "linux",
				Arch:     "amd64",
				URL:      "https://example.com/plugin.so",
				Checksum: "abc123",
				Size:     1024,
				Format:   "so",
			},
		},
		PublishedAt: time.Now(),
		Deprecated:  false,
	}

	if manifest.ID != "org/test-plugin" {
		t.Errorf("ID = %q", manifest.ID)
	}
	if manifest.Version != "1.2.3" {
		t.Errorf("Version = %q", manifest.Version)
	}
	if manifest.License != "MIT" {
		t.Errorf("License = %q", manifest.License)
	}
	if len(manifest.Keywords) != 2 {
		t.Errorf("len(Keywords) = %d", len(manifest.Keywords))
	}
	if manifest.Requires.NexusVersion != ">=1.0.0" {
		t.Errorf("Requires.NexusVersion = %q", manifest.Requires.NexusVersion)
	}
	if len(manifest.Artifacts) != 1 {
		t.Errorf("len(Artifacts) = %d", len(manifest.Artifacts))
	}
}

func TestPluginRequirementsStruct(t *testing.T) {
	reqs := PluginRequirements{
		NexusVersion: ">=2.0.0",
		Go:           "1.22",
		OS:           []string{"linux"},
		Arch:         []string{"amd64"},
		Dependencies: []PluginDependency{
			{ID: "dep1", Version: ">=1.0.0", Optional: false},
			{ID: "dep2", Version: "^2.0.0", Optional: true},
		},
	}

	if reqs.NexusVersion != ">=2.0.0" {
		t.Errorf("NexusVersion = %q", reqs.NexusVersion)
	}
	if len(reqs.Dependencies) != 2 {
		t.Errorf("len(Dependencies) = %d", len(reqs.Dependencies))
	}
	if reqs.Dependencies[0].ID != "dep1" {
		t.Errorf("Dependencies[0].ID = %q", reqs.Dependencies[0].ID)
	}
	if reqs.Dependencies[1].Optional != true {
		t.Error("Dependencies[1].Optional should be true")
	}
}

func TestPluginArtifactStruct(t *testing.T) {
	artifact := PluginArtifact{
		OS:        "darwin",
		Arch:      "arm64",
		URL:       "https://example.com/plugin-darwin-arm64.tar.gz",
		Checksum:  "sha256:abc123",
		Signature: "sig123",
		Size:      2048,
		Format:    "tar.gz",
	}

	if artifact.OS != "darwin" {
		t.Errorf("OS = %q", artifact.OS)
	}
	if artifact.Arch != "arm64" {
		t.Errorf("Arch = %q", artifact.Arch)
	}
	if artifact.Format != "tar.gz" {
		t.Errorf("Format = %q", artifact.Format)
	}
	if artifact.Size != 2048 {
		t.Errorf("Size = %d", artifact.Size)
	}
}

func TestInstalledPluginStruct(t *testing.T) {
	now := time.Now()
	installed := InstalledPlugin{
		ID:           "org/my-plugin",
		Version:      "1.0.0",
		Path:         "/path/to/plugin",
		BinaryPath:   "/path/to/plugin/plugin.so",
		ManifestPath: "/path/to/plugin/nexus.plugin.json",
		Checksum:     "sha256:checksum",
		Verified:     true,
		InstalledAt:  now,
		UpdatedAt:    now,
		Source:       "https://registry.example.com",
		AutoUpdate:   true,
		Enabled:      true,
		Config:       map[string]any{"key": "value"},
	}

	if installed.ID != "org/my-plugin" {
		t.Errorf("ID = %q", installed.ID)
	}
	if !installed.Verified {
		t.Error("Verified should be true")
	}
	if !installed.AutoUpdate {
		t.Error("AutoUpdate should be true")
	}
	if !installed.Enabled {
		t.Error("Enabled should be true")
	}
	if installed.Config["key"] != "value" {
		t.Errorf("Config[key] = %v", installed.Config["key"])
	}
}

func TestPluginSearchResultStruct(t *testing.T) {
	result := PluginSearchResult{
		Plugin: &MarketplaceManifest{
			ID:   "org/plugin",
			Name: "Plugin",
		},
		Score:            0.95,
		Installed:        true,
		InstalledVersion: "1.0.0",
		UpdateAvailable:  true,
	}

	if result.Score != 0.95 {
		t.Errorf("Score = %f", result.Score)
	}
	if !result.Installed {
		t.Error("Installed should be true")
	}
	if result.InstalledVersion != "1.0.0" {
		t.Errorf("InstalledVersion = %q", result.InstalledVersion)
	}
	if !result.UpdateAvailable {
		t.Error("UpdateAvailable should be true")
	}
}

func TestPluginIndexStruct(t *testing.T) {
	idx := &PluginIndex{
		Version: "1",
		Plugins: map[string]*InstalledPlugin{
			"plugin1": {ID: "plugin1", Version: "1.0.0"},
			"plugin2": {ID: "plugin2", Version: "2.0.0"},
		},
		LastUpdated: time.Now(),
		Registries:  []string{"https://registry1.example.com", "https://registry2.example.com"},
	}

	if idx.Version != "1" {
		t.Errorf("Version = %q", idx.Version)
	}
	if len(idx.Plugins) != 2 {
		t.Errorf("len(Plugins) = %d", len(idx.Plugins))
	}
	if len(idx.Registries) != 2 {
		t.Errorf("len(Registries) = %d", len(idx.Registries))
	}
}

func TestRegistryIndexStruct(t *testing.T) {
	idx := RegistryIndex{
		Version:     "1",
		Name:        "Official Registry",
		Description: "The official plugin registry",
		Plugins: []*MarketplaceManifest{
			{ID: "plugin1"},
			{ID: "plugin2"},
		},
		PublicKey: "base64pubkey",
		UpdatedAt: time.Now(),
	}

	if idx.Name != "Official Registry" {
		t.Errorf("Name = %q", idx.Name)
	}
	if len(idx.Plugins) != 2 {
		t.Errorf("len(Plugins) = %d", len(idx.Plugins))
	}
	if idx.PublicKey != "base64pubkey" {
		t.Errorf("PublicKey = %q", idx.PublicKey)
	}
}

func TestInstallOptionsStruct(t *testing.T) {
	opts := InstallOptions{
		Version:    "1.0.0",
		Force:      true,
		SkipVerify: false,
		AutoUpdate: true,
		Config:     map[string]any{"token": "secret"},
	}

	if opts.Version != "1.0.0" {
		t.Errorf("Version = %q", opts.Version)
	}
	if !opts.Force {
		t.Error("Force should be true")
	}
	if opts.SkipVerify {
		t.Error("SkipVerify should be false")
	}
	if !opts.AutoUpdate {
		t.Error("AutoUpdate should be true")
	}
}

func TestUpdateOptionsStruct(t *testing.T) {
	opts := UpdateOptions{
		Version:    "2.0.0",
		Force:      false,
		SkipVerify: true,
	}

	if opts.Version != "2.0.0" {
		t.Errorf("Version = %q", opts.Version)
	}
	if opts.Force {
		t.Error("Force should be false")
	}
	if !opts.SkipVerify {
		t.Error("SkipVerify should be true")
	}
}

func TestPluginDependencyStruct(t *testing.T) {
	dep := PluginDependency{
		ID:       "required-plugin",
		Version:  ">=1.5.0",
		Optional: false,
	}

	if dep.ID != "required-plugin" {
		t.Errorf("ID = %q", dep.ID)
	}
	if dep.Version != ">=1.5.0" {
		t.Errorf("Version = %q", dep.Version)
	}
	if dep.Optional {
		t.Error("Optional should be false")
	}
}

func TestMarketplaceManifest_Deprecated(t *testing.T) {
	manifest := MarketplaceManifest{
		ID:                 "old/plugin",
		Deprecated:         true,
		DeprecationMessage: "Use new/plugin instead",
	}

	if !manifest.Deprecated {
		t.Error("Deprecated should be true")
	}
	if manifest.DeprecationMessage != "Use new/plugin instead" {
		t.Errorf("DeprecationMessage = %q", manifest.DeprecationMessage)
	}
}
