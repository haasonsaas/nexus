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

// ErrPathTraversal indicates an attempted path traversal attack.
var ErrPathTraversal = fmt.Errorf("path traversal detected")

// ValidatePluginPath checks that a plugin path is safe and doesn't attempt
// path traversal. Returns the cleaned absolute path or an error.
func ValidatePluginPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("plugin path is empty")
	}

	// Clean the path to normalize it
	cleaned := filepath.Clean(path)

	// Check for path traversal attempts.
	// After cleaning, ".." should not appear as a path segment in a safe path.
	if containsPathTraversalSegment(cleaned) {
		return "", fmt.Errorf("%w: path contains '..' after cleaning: %s", ErrPathTraversal, path)
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Re-check the absolute path for any remaining traversal
	if containsPathTraversalSegment(absPath) {
		return "", fmt.Errorf("%w: absolute path contains '..': %s", ErrPathTraversal, absPath)
	}

	return absPath, nil
}

func containsPathTraversalSegment(path string) bool {
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

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
	// Validate path to prevent traversal attacks
	validatedPath, err := ValidatePluginPath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(validatedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat manifest path: %w", err)
	}
	if !info.IsDir() {
		manifest, err := pluginsdk.DecodeManifestFile(validatedPath)
		if err != nil {
			return nil, fmt.Errorf("load manifest %s: %w", validatedPath, err)
		}
		return &ManifestInfo{Manifest: manifest, Path: validatedPath}, nil
	}

	for _, name := range []string{pluginsdk.ManifestFilename, pluginsdk.LegacyManifestFilename} {
		manifestPath := filepath.Join(validatedPath, name)
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
		// Validate path to prevent traversal attacks
		validated, err := ValidatePluginPath(trimmed)
		if err != nil {
			// Skip invalid paths silently - they will fail later if used
			continue
		}
		if _, ok := seen[validated]; ok {
			continue
		}
		seen[validated] = struct{}{}
		normalized = append(normalized, validated)
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
