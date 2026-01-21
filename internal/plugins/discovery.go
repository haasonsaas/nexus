package plugins

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

type ManifestInfo struct {
	Manifest *pluginsdk.Manifest
	Path     string
}

// DiscoverManifests scans directories for plugin manifests.
func DiscoverManifests(paths []string) (map[string]ManifestInfo, error) {
	manifests := make(map[string]ManifestInfo)
	for _, root := range paths {
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
