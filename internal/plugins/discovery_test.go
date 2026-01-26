package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

func TestDiscoverManifestsCachesResults(t *testing.T) {
	t.Setenv("NEXUS_PLUGIN_MANIFEST_CACHE_MS", "60000")
	t.Setenv("NEXUS_DISABLE_PLUGIN_MANIFEST_CACHE", "")

	dir := t.TempDir()
	manifest := &pluginsdk.Manifest{
		ID:           "alpha",
		ConfigSchema: []byte(`{"type":"object"}`),
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestPath := filepath.Join(dir, pluginsdk.ManifestFilename)
	if err := os.WriteFile(manifestPath, payload, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	initial, err := DiscoverManifests([]string{dir})
	if err != nil {
		t.Fatalf("discover manifests: %v", err)
	}
	if _, ok := initial["alpha"]; !ok {
		t.Fatalf("expected manifest alpha")
	}

	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}

	cached, err := DiscoverManifests([]string{dir})
	if err != nil {
		t.Fatalf("discover manifests from cache: %v", err)
	}
	if _, ok := cached["alpha"]; !ok {
		t.Fatalf("expected cached manifest alpha")
	}
}

func TestValidatePluginPath_AllowsDotDotSubstring(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foo..bar")

	abs, err := ValidatePluginPath(path)
	if err != nil {
		t.Fatalf("ValidatePluginPath(%q) error = %v", path, err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("ValidatePluginPath(%q) = %q; expected absolute path", path, abs)
	}
	if abs != filepath.Clean(abs) {
		t.Fatalf("ValidatePluginPath(%q) = %q; expected cleaned path", path, abs)
	}
}

func TestValidatePluginPath_RejectsTraversalSegment(t *testing.T) {
	path := filepath.Join("..", "plugin.so")
	_, err := ValidatePluginPath(path)
	if err == nil {
		t.Fatalf("ValidatePluginPath(%q) expected error", path)
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("ValidatePluginPath(%q) error = %v; expected ErrPathTraversal", path, err)
	}
}
