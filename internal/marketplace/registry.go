// Package marketplace provides plugin marketplace functionality for Nexus.
package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

// DefaultRegistryURL is the default Nexus plugin registry.
const DefaultRegistryURL = "https://plugins.nexus.dev"

// RegistryClient provides access to plugin registries.
type RegistryClient struct {
	registries []string
	httpClient *http.Client
	cache      *registryCache
	logger     *slog.Logger
	mu         sync.RWMutex
}

// registryCache caches registry indexes.
type registryCache struct {
	mu      sync.RWMutex
	indexes map[string]*cachedIndex
	ttl     time.Duration
}

type cachedIndex struct {
	index     *pluginsdk.RegistryIndex
	fetchedAt time.Time
}

// RegistryClientOption configures a RegistryClient.
type RegistryClientOption func(*RegistryClient)

// WithRegistries sets the registry URLs.
func WithRegistries(urls []string) RegistryClientOption {
	return func(c *RegistryClient) {
		c.registries = urls
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) RegistryClientOption {
	return func(c *RegistryClient) {
		c.httpClient = client
	}
}

// WithCacheTTL sets the cache TTL.
func WithCacheTTL(ttl time.Duration) RegistryClientOption {
	return func(c *RegistryClient) {
		c.cache.ttl = ttl
	}
}

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) RegistryClientOption {
	return func(c *RegistryClient) {
		c.logger = logger
	}
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(opts ...RegistryClientOption) *RegistryClient {
	c := &RegistryClient{
		registries: []string{DefaultRegistryURL},
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: &registryCache{
			indexes: make(map[string]*cachedIndex),
			ttl:     15 * time.Minute,
		},
		logger: slog.Default().With("component", "marketplace.registry"),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Registries returns the configured registry URLs.
func (c *RegistryClient) Registries() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]string, len(c.registries))
	copy(result, c.registries)
	return result
}

// AddRegistry adds a registry URL.
func (c *RegistryClient) AddRegistry(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.registries {
		if r == url {
			return
		}
	}
	c.registries = append(c.registries, url)
}

// FetchIndex fetches the index from a single registry.
func (c *RegistryClient) FetchIndex(ctx context.Context, registryURL string) (*pluginsdk.RegistryIndex, error) {
	// Check cache
	c.cache.mu.RLock()
	cached, ok := c.cache.indexes[registryURL]
	c.cache.mu.RUnlock()

	if ok && time.Since(cached.fetchedAt) < c.cache.ttl {
		c.logger.Debug("using cached registry index", "registry", registryURL)
		return cached.index, nil
	}

	// Fetch fresh index
	indexURL, err := url.JoinPath(registryURL, "index.json")
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nexus-marketplace/1.0")

	c.logger.Debug("fetching registry index", "url", indexURL)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch registry index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if readErr != nil {
			return nil, fmt.Errorf("registry returned %d and failed to read body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var index pluginsdk.RegistryIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("decode registry index: %w", err)
	}

	// Update cache
	c.cache.mu.Lock()
	c.cache.indexes[registryURL] = &cachedIndex{
		index:     &index,
		fetchedAt: time.Now(),
	}
	c.cache.mu.Unlock()

	c.logger.Info("fetched registry index",
		"registry", registryURL,
		"plugins", len(index.Plugins))

	return &index, nil
}

// FetchAllIndexes fetches indexes from all configured registries.
func (c *RegistryClient) FetchAllIndexes(ctx context.Context) (map[string]*pluginsdk.RegistryIndex, error) {
	registries := c.Registries()
	result := make(map[string]*pluginsdk.RegistryIndex)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errors := make([]error, 0)

	for _, reg := range registries {
		wg.Add(1)
		go func(regURL string) {
			defer wg.Done()
			index, err := c.FetchIndex(ctx, regURL)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				c.logger.Warn("failed to fetch registry",
					"registry", regURL,
					"error", err)
				errors = append(errors, fmt.Errorf("%s: %w", regURL, err))
				return
			}
			result[regURL] = index
		}(reg)
	}

	wg.Wait()

	if len(result) == 0 && len(errors) > 0 {
		return nil, fmt.Errorf("failed to fetch any registries: %v", errors)
	}

	return result, nil
}

// GetPlugin fetches a specific plugin manifest from registries.
func (c *RegistryClient) GetPlugin(ctx context.Context, id string) (*pluginsdk.MarketplaceManifest, string, error) {
	indexes, err := c.FetchAllIndexes(ctx)
	if err != nil {
		return nil, "", err
	}

	for regURL, index := range indexes {
		for _, plugin := range index.Plugins {
			if plugin.ID == id {
				return plugin, regURL, nil
			}
		}
	}

	return nil, "", fmt.Errorf("plugin not found: %s", id)
}

// Search searches for plugins across all registries.
func (c *RegistryClient) Search(ctx context.Context, query string, opts SearchOptions) ([]*pluginsdk.PluginSearchResult, error) {
	indexes, err := c.FetchAllIndexes(ctx)
	if err != nil {
		return nil, err
	}

	var results []*pluginsdk.PluginSearchResult
	seen := make(map[string]bool)
	queryLower := strings.ToLower(query)

	for _, index := range indexes {
		for _, plugin := range index.Plugins {
			if seen[plugin.ID] {
				continue
			}
			seen[plugin.ID] = true

			// Filter by category
			if opts.Category != "" {
				found := false
				for _, cat := range plugin.Categories {
					if strings.EqualFold(cat, opts.Category) {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			// Filter by platform
			if opts.OS != "" || opts.Arch != "" {
				compatible := isCompatible(plugin, opts.OS, opts.Arch)
				if !compatible {
					continue
				}
			}

			// Calculate search score
			score := calculateScore(plugin, queryLower)
			if score == 0 && query != "" {
				continue
			}

			results = append(results, &pluginsdk.PluginSearchResult{
				Plugin: plugin,
				Score:  score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Apply limit
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// SearchOptions configures plugin search.
type SearchOptions struct {
	// Category filters by category.
	Category string

	// OS filters by operating system.
	OS string

	// Arch filters by architecture.
	Arch string

	// Limit limits the number of results.
	Limit int

	// IncludeDeprecated includes superseded plugins.
	IncludeDeprecated bool
}

// DefaultSearchOptions returns search options for the current platform.
func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		OS:    runtime.GOOS,
		Arch:  runtime.GOARCH,
		Limit: 50,
	}
}

// isCompatible checks if a plugin is compatible with the given OS/Arch.
func isCompatible(plugin *pluginsdk.MarketplaceManifest, os, arch string) bool {
	if len(plugin.Artifacts) == 0 {
		return true // No artifacts means source-only
	}

	for _, artifact := range plugin.Artifacts {
		osMatch := os == "" || artifact.OS == os || artifact.OS == "any"
		archMatch := arch == "" || artifact.Arch == arch || artifact.Arch == "any"
		if osMatch && archMatch {
			return true
		}
	}

	return false
}

// calculateScore calculates the search relevance score for a plugin.
func calculateScore(plugin *pluginsdk.MarketplaceManifest, query string) float64 {
	if query == "" {
		return 1.0
	}

	score := 0.0

	// ID match (highest weight)
	if strings.Contains(strings.ToLower(plugin.ID), query) {
		score += 0.4
		if strings.ToLower(plugin.ID) == query {
			score += 0.3
		}
	}

	// Name match
	if strings.Contains(strings.ToLower(plugin.Name), query) {
		score += 0.3
	}

	// Description match
	if strings.Contains(strings.ToLower(plugin.Description), query) {
		score += 0.1
	}

	// Keyword match
	for _, kw := range plugin.Keywords {
		if strings.Contains(strings.ToLower(kw), query) {
			score += 0.1
			break
		}
	}

	// Category match
	for _, cat := range plugin.Categories {
		if strings.Contains(strings.ToLower(cat), query) {
			score += 0.1
			break
		}
	}

	return score
}

// DownloadArtifact downloads a plugin artifact.
func (c *RegistryClient) DownloadArtifact(ctx context.Context, artifact *pluginsdk.PluginArtifact) ([]byte, error) {
	if artifact == nil || artifact.URL == "" {
		return nil, fmt.Errorf("invalid artifact")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "nexus-marketplace/1.0")

	c.logger.Debug("downloading artifact", "url", artifact.URL)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Limit download size (100MB max)
	const maxSize = 100 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}

	c.logger.Info("downloaded artifact",
		"url", artifact.URL,
		"size", len(data))

	return data, nil
}

// GetArtifactForPlatform returns the artifact for the current platform.
func GetArtifactForPlatform(manifest *pluginsdk.MarketplaceManifest) *pluginsdk.PluginArtifact {
	return GetArtifactForOS(manifest, runtime.GOOS, runtime.GOARCH)
}

// GetArtifactForOS returns the artifact for the specified OS/Arch.
func GetArtifactForOS(manifest *pluginsdk.MarketplaceManifest, os, arch string) *pluginsdk.PluginArtifact {
	if manifest == nil {
		return nil
	}

	for i := range manifest.Artifacts {
		artifact := &manifest.Artifacts[i]
		osMatch := artifact.OS == os || artifact.OS == "any"
		archMatch := artifact.Arch == arch || artifact.Arch == "any"
		if osMatch && archMatch {
			return artifact
		}
	}

	return nil
}

// ClearCache clears the registry cache.
func (c *RegistryClient) ClearCache() {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	c.cache.indexes = make(map[string]*cachedIndex)
}
