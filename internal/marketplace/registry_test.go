package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

func TestNewRegistryClient(t *testing.T) {
	client := NewRegistryClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	registries := client.Registries()
	if len(registries) != 1 {
		t.Errorf("expected 1 default registry, got %d", len(registries))
	}
	if registries[0] != DefaultRegistryURL {
		t.Errorf("expected default registry %s, got %s", DefaultRegistryURL, registries[0])
	}
}

func TestRegistryClientWithOptions(t *testing.T) {
	customRegistries := []string{"https://custom.registry.dev"}
	customClient := &http.Client{Timeout: 10 * time.Second}

	client := NewRegistryClient(
		WithRegistries(customRegistries),
		WithHTTPClient(customClient),
		WithCacheTTL(5*time.Minute),
	)

	registries := client.Registries()
	if len(registries) != 1 || registries[0] != "https://custom.registry.dev" {
		t.Errorf("expected custom registry, got %v", registries)
	}
}

func TestAddRegistry(t *testing.T) {
	client := NewRegistryClient()

	client.AddRegistry("https://new.registry.dev")

	registries := client.Registries()
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

func TestAddRegistryDuplicate(t *testing.T) {
	client := NewRegistryClient(
		WithRegistries([]string{"https://registry.dev"}),
	)

	initialCount := len(client.Registries())
	client.AddRegistry("https://registry.dev") // Add duplicate

	if len(client.Registries()) != initialCount {
		t.Error("duplicate registry should not be added")
	}
}

func TestFetchIndex(t *testing.T) {
	index := &pluginsdk.RegistryIndex{
		Version: "1.0.0",
		Plugins: []*pluginsdk.MarketplaceManifest{
			{ID: "test-plugin", Name: "Test Plugin", Version: "1.0.0"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	result, err := client.FetchIndex(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("FetchIndex() error = %v", err)
	}

	if result.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", result.Version)
	}
	if len(result.Plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(result.Plugins))
	}
}

func TestFetchIndexCaching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		index := &pluginsdk.RegistryIndex{Version: "1.0.0"}
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(
		WithRegistries([]string{server.URL}),
		WithCacheTTL(1*time.Hour), // Long TTL
	)

	// First fetch
	_, err := client.FetchIndex(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("first FetchIndex() error = %v", err)
	}

	// Second fetch should use cache
	_, err = client.FetchIndex(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("second FetchIndex() error = %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestFetchIndexError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	_, err := client.FetchIndex(context.Background(), server.URL)
	if err == nil {
		t.Error("expected error for failed fetch")
	}
}

func TestFetchIndexInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	_, err := client.FetchIndex(context.Background(), server.URL)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGetPlugin(t *testing.T) {
	index := &pluginsdk.RegistryIndex{
		Plugins: []*pluginsdk.MarketplaceManifest{
			{ID: "my-plugin", Name: "My Plugin", Version: "1.0.0"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	plugin, source, err := client.GetPlugin(context.Background(), "my-plugin")
	if err != nil {
		t.Fatalf("GetPlugin() error = %v", err)
	}

	if plugin.ID != "my-plugin" {
		t.Errorf("expected plugin ID 'my-plugin', got %s", plugin.ID)
	}
	if source != server.URL {
		t.Errorf("expected source %s, got %s", server.URL, source)
	}
}

func TestGetPluginNotFound(t *testing.T) {
	index := &pluginsdk.RegistryIndex{
		Plugins: []*pluginsdk.MarketplaceManifest{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	_, _, err := client.GetPlugin(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestSearch(t *testing.T) {
	index := &pluginsdk.RegistryIndex{
		Plugins: []*pluginsdk.MarketplaceManifest{
			{ID: "github-plugin", Name: "GitHub Integration", Description: "GitHub tools", Keywords: []string{"git", "vcs"}},
			{ID: "slack-plugin", Name: "Slack Bot", Description: "Slack integration"},
			{ID: "another-github", Name: "Another GitHub", Keywords: []string{"github"}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	results, err := client.Search(context.Background(), "github", SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results for 'github', got %d", len(results))
	}
}

func TestSearchWithCategory(t *testing.T) {
	index := &pluginsdk.RegistryIndex{
		Plugins: []*pluginsdk.MarketplaceManifest{
			{ID: "vcs-plugin", Name: "VCS Plugin", Categories: []string{"development"}},
			{ID: "chat-plugin", Name: "Chat Plugin", Categories: []string{"communication"}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	results, err := client.Search(context.Background(), "", SearchOptions{Category: "development"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result for category 'development', got %d", len(results))
	}
}

func TestSearchWithLimit(t *testing.T) {
	index := &pluginsdk.RegistryIndex{
		Plugins: []*pluginsdk.MarketplaceManifest{
			{ID: "plugin-1", Name: "Plugin 1"},
			{ID: "plugin-2", Name: "Plugin 2"},
			{ID: "plugin-3", Name: "Plugin 3"},
			{ID: "plugin-4", Name: "Plugin 4"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(WithRegistries([]string{server.URL}))

	results, err := client.Search(context.Background(), "", SearchOptions{Limit: 2})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results (limit), got %d", len(results))
	}
}

func TestDefaultSearchOptions(t *testing.T) {
	opts := DefaultSearchOptions()

	if opts.OS == "" {
		t.Error("expected OS to be set")
	}
	if opts.Arch == "" {
		t.Error("expected Arch to be set")
	}
	if opts.Limit != 50 {
		t.Errorf("expected Limit 50, got %d", opts.Limit)
	}
}

func TestIsCompatible(t *testing.T) {
	tests := []struct {
		name       string
		plugin     *pluginsdk.MarketplaceManifest
		os         string
		arch       string
		compatible bool
	}{
		{
			name: "no artifacts",
			plugin: &pluginsdk.MarketplaceManifest{
				Artifacts: nil,
			},
			os:         "linux",
			arch:       "amd64",
			compatible: true, // Source-only plugin
		},
		{
			name: "exact match",
			plugin: &pluginsdk.MarketplaceManifest{
				Artifacts: []pluginsdk.PluginArtifact{
					{OS: "linux", Arch: "amd64"},
				},
			},
			os:         "linux",
			arch:       "amd64",
			compatible: true,
		},
		{
			name: "no match",
			plugin: &pluginsdk.MarketplaceManifest{
				Artifacts: []pluginsdk.PluginArtifact{
					{OS: "linux", Arch: "amd64"},
				},
			},
			os:         "darwin",
			arch:       "arm64",
			compatible: false,
		},
		{
			name: "any os",
			plugin: &pluginsdk.MarketplaceManifest{
				Artifacts: []pluginsdk.PluginArtifact{
					{OS: "any", Arch: "amd64"},
				},
			},
			os:         "darwin",
			arch:       "amd64",
			compatible: true,
		},
		{
			name: "any arch",
			plugin: &pluginsdk.MarketplaceManifest{
				Artifacts: []pluginsdk.PluginArtifact{
					{OS: "linux", Arch: "any"},
				},
			},
			os:         "linux",
			arch:       "arm64",
			compatible: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCompatible(tt.plugin, tt.os, tt.arch)
			if result != tt.compatible {
				t.Errorf("isCompatible() = %v, want %v", result, tt.compatible)
			}
		})
	}
}

func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name     string
		plugin   *pluginsdk.MarketplaceManifest
		query    string
		minScore float64
	}{
		{
			name:     "empty query",
			plugin:   &pluginsdk.MarketplaceManifest{ID: "test"},
			query:    "",
			minScore: 1.0,
		},
		{
			name:     "exact ID match",
			plugin:   &pluginsdk.MarketplaceManifest{ID: "github"},
			query:    "github",
			minScore: 0.7, // ID match + exact match bonus
		},
		{
			name:     "partial ID match",
			plugin:   &pluginsdk.MarketplaceManifest{ID: "github-plugin"},
			query:    "github",
			minScore: 0.4, // ID match
		},
		{
			name:     "name match",
			plugin:   &pluginsdk.MarketplaceManifest{ID: "gh", Name: "GitHub Integration"},
			query:    "github",
			minScore: 0.3, // Name match
		},
		{
			name:     "no match",
			plugin:   &pluginsdk.MarketplaceManifest{ID: "slack", Name: "Slack Bot"},
			query:    "github",
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateScore(tt.plugin, tt.query)
			if score < tt.minScore {
				t.Errorf("calculateScore() = %v, want >= %v", score, tt.minScore)
			}
		})
	}
}

func TestGetArtifactForOS(t *testing.T) {
	manifest := &pluginsdk.MarketplaceManifest{
		Artifacts: []pluginsdk.PluginArtifact{
			{OS: "linux", Arch: "amd64", URL: "https://example.com/linux-amd64.so"},
			{OS: "darwin", Arch: "arm64", URL: "https://example.com/darwin-arm64.so"},
		},
	}

	tests := []struct {
		name      string
		os        string
		arch      string
		expectURL string
		expectNil bool
	}{
		{
			name:      "linux amd64",
			os:        "linux",
			arch:      "amd64",
			expectURL: "https://example.com/linux-amd64.so",
		},
		{
			name:      "darwin arm64",
			os:        "darwin",
			arch:      "arm64",
			expectURL: "https://example.com/darwin-arm64.so",
		},
		{
			name:      "no match",
			os:        "windows",
			arch:      "amd64",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact := GetArtifactForOS(manifest, tt.os, tt.arch)
			if tt.expectNil {
				if artifact != nil {
					t.Error("expected nil artifact")
				}
			} else {
				if artifact == nil {
					t.Fatal("expected non-nil artifact")
				}
				if artifact.URL != tt.expectURL {
					t.Errorf("expected URL %s, got %s", tt.expectURL, artifact.URL)
				}
			}
		})
	}
}

func TestGetArtifactForOSNilManifest(t *testing.T) {
	artifact := GetArtifactForOS(nil, "linux", "amd64")
	if artifact != nil {
		t.Error("expected nil artifact for nil manifest")
	}
}

func TestClearCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		index := &pluginsdk.RegistryIndex{Version: "1.0.0"}
		json.NewEncoder(w).Encode(index)
	}))
	defer server.Close()

	client := NewRegistryClient(
		WithRegistries([]string{server.URL}),
		WithCacheTTL(1*time.Hour),
	)

	// First fetch
	_, _ = client.FetchIndex(context.Background(), server.URL)

	// Clear cache
	client.ClearCache()

	// Second fetch should hit server
	_, _ = client.FetchIndex(context.Background(), server.URL)

	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls after cache clear, got %d", callCount)
	}
}

func TestDownloadArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("plugin binary content"))
	}))
	defer server.Close()

	client := NewRegistryClient()

	artifact := &pluginsdk.PluginArtifact{
		URL: server.URL + "/plugin.so",
	}

	data, err := client.DownloadArtifact(context.Background(), artifact)
	if err != nil {
		t.Fatalf("DownloadArtifact() error = %v", err)
	}

	if string(data) != "plugin binary content" {
		t.Errorf("expected 'plugin binary content', got %s", string(data))
	}
}

func TestDownloadArtifactNil(t *testing.T) {
	client := NewRegistryClient()

	_, err := client.DownloadArtifact(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil artifact")
	}
}

func TestDownloadArtifactEmptyURL(t *testing.T) {
	client := NewRegistryClient()

	artifact := &pluginsdk.PluginArtifact{URL: ""}

	_, err := client.DownloadArtifact(context.Background(), artifact)
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestDownloadArtifactError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewRegistryClient()

	artifact := &pluginsdk.PluginArtifact{
		URL: server.URL + "/notfound.so",
	}

	_, err := client.DownloadArtifact(context.Background(), artifact)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}
