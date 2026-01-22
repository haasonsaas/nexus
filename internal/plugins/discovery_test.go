package plugins

import (
	"encoding/json"
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
