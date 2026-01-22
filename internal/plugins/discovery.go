package plugins

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

type ManifestInfo struct {
	Manifest *pluginsdk.Manifest
	Path     string
}

type manifestCacheEntry struct {
	expires   time.Time
	manifests map[string]ManifestInfo
}

var manifestCache = struct {
	mu      sync.Mutex
	entries map[string]manifestCacheEntry
}{
	entries: make(map[string]manifestCacheEntry),
}

const defaultManifestCacheTTL = 2 * time.Second

// DiscoverManifests scans directories for plugin manifests.
func DiscoverManifests(paths []string) (map[string]ManifestInfo, error) {
	normalized := normalizeManifestPaths(paths)
	if cached, ok := cachedManifests(normalized); ok {
		return cached, nil
	}

	manifests := make(map[string]ManifestInfo)
	for _, root := range normalized {
		if strings.TrimSpace(root) == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat plugin path: %w", err)
		}
		if !info.IsDir() {
			entry, err := loadManifestFromPath(root)
			if err != nil {
				return nil, err
			}
			if entry != nil {
				if err := registerManifest(manifests, *entry); err != nil {
					return nil, err
				}
			}
			continue
		}

		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !isManifestFilename(d.Name()) {
				return nil
			}
			manifest, err := pluginsdk.DecodeManifestFile(path)
			if err != nil {
				return fmt.Errorf("load manifest %s: %w", path, err)
			}
			entry := ManifestInfo{Manifest: manifest, Path: path}
			return registerManifest(manifests, entry)
		}); err != nil {
			return nil, fmt.Errorf("walk plugin path: %w", err)
		}
	}
	storeManifestCache(normalized, manifests)
	return manifests, nil
}

// LoadManifestForPath loads a manifest from a file or directory path.
func LoadManifestForPath(path string) (ManifestInfo, error) {
	entry := ManifestInfo{}
	manifest, err := loadManifestFromPath(path)
	if err != nil {
		return entry, err
	}
	if manifest == nil {
		return entry, fmt.Errorf("manifest not found at %s", path)
	}
	return *manifest, nil
}

func loadManifestFromPath(path string) (*ManifestInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat manifest path: %w", err)
	}
	if !info.IsDir() {
		manifest, err := pluginsdk.DecodeManifestFile(path)
		if err != nil {
			return nil, fmt.Errorf("load manifest %s: %w", path, err)
		}
		return &ManifestInfo{Manifest: manifest, Path: path}, nil
	}

	for _, name := range []string{pluginsdk.ManifestFilename, pluginsdk.LegacyManifestFilename} {
		manifestPath := filepath.Join(path, name)
		if _, err := os.Stat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat manifest %s: %w", manifestPath, err)
		}
		manifest, err := pluginsdk.DecodeManifestFile(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("load manifest %s: %w", manifestPath, err)
		}
		return &ManifestInfo{Manifest: manifest, Path: manifestPath}, nil
	}

	return nil, nil
}

func isManifestFilename(name string) bool {
	return name == pluginsdk.ManifestFilename || name == pluginsdk.LegacyManifestFilename
}

func registerManifest(manifests map[string]ManifestInfo, entry ManifestInfo) error {
	if entry.Manifest == nil {
		return fmt.Errorf("manifest is nil")
	}
	if err := entry.Manifest.Validate(); err != nil {
		return err
	}
	id := entry.Manifest.ID
	if existing, ok := manifests[id]; ok {
		return fmt.Errorf("duplicate manifest id %q (%s, %s)", id, existing.Path, entry.Path)
	}
	manifests[id] = entry
	return nil
}

func normalizeManifestPaths(paths []string) []string {
	seen := make(map[string]struct{})
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	sort.Strings(normalized)
	return normalized
}

func cachedManifests(paths []string) (map[string]ManifestInfo, bool) {
	ttl := manifestCacheTTL()
	if ttl <= 0 || len(paths) == 0 || manifestCacheDisabled() {
		return nil, false
	}
	key := manifestCacheKey(paths)
	if key == "" {
		return nil, false
	}

	now := time.Now()
	manifestCache.mu.Lock()
	entry, ok := manifestCache.entries[key]
	if ok && now.Before(entry.expires) {
		manifests := cloneManifestMap(entry.manifests)
		manifestCache.mu.Unlock()
		return manifests, true
	}
	if ok {
		delete(manifestCache.entries, key)
	}
	manifestCache.mu.Unlock()
	return nil, false
}

func storeManifestCache(paths []string, manifests map[string]ManifestInfo) {
	ttl := manifestCacheTTL()
	if ttl <= 0 || len(paths) == 0 || manifestCacheDisabled() {
		return
	}
	key := manifestCacheKey(paths)
	if key == "" {
		return
	}

	manifestCache.mu.Lock()
	manifestCache.entries[key] = manifestCacheEntry{
		expires:   time.Now().Add(ttl),
		manifests: cloneManifestMap(manifests),
	}
	manifestCache.mu.Unlock()
}

func manifestCacheKey(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return strings.Join(paths, "\n")
}

func cloneManifestMap(src map[string]ManifestInfo) map[string]ManifestInfo {
	dst := make(map[string]ManifestInfo, len(src))
	for key, info := range src {
		dst[key] = info
	}
	return dst
}

func manifestCacheTTL() time.Duration {
	value := strings.TrimSpace(os.Getenv("NEXUS_PLUGIN_MANIFEST_CACHE_MS"))
	if value == "" {
		return defaultManifestCacheTTL
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultManifestCacheTTL
	}
	if parsed <= 0 {
		return 0
	}
	return time.Duration(parsed) * time.Millisecond
}

func manifestCacheDisabled() bool {
	return envBool(os.Getenv("NEXUS_DISABLE_PLUGIN_MANIFEST_CACHE"))
}

func envBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
