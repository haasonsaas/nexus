package plugins

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

// ValidateConfig validates plugin configuration and manifests.
func ValidateConfig(cfg *config.Config) error {
	issues := ValidationIssues(cfg)
	if len(issues) > 0 {
		return &config.ConfigValidationError{Issues: issues}
	}
	return nil
}

func validateManifest(manifest *pluginsdk.Manifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest is nil")
	}
	return manifest.Validate()
}

// ValidationIssues returns plugin validation issues for config validation hooks.
func ValidationIssues(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}

	var issues []string
	if cfg.Plugins.Isolation.Enabled {
		issues = append(issues, pluginIsolationNotImplementedMessage)
	}
	if len(cfg.Plugins.Entries) == 0 {
		return issues
	}

	paths := append([]string{}, cfg.Plugins.Load.Paths...)
	for _, entry := range cfg.Plugins.Entries {
		if entry.Path != "" {
			paths = append(paths, entry.Path)
		}
	}

	manifestIndex, err := DiscoverManifests(paths)
	if err != nil {
		return []string{fmt.Sprintf("plugin manifest discovery failed: %v", err)}
	}

	for id, entry := range cfg.Plugins.Entries {
		var info ManifestInfo
		var ok bool

		if entry.Path != "" {
			info, err = LoadManifestForPath(entry.Path)
			if err != nil {
				issues = append(issues, fmt.Sprintf("plugins.entries.%s manifest error: %v", id, err))
				continue
			}
			if strings.TrimSpace(info.Manifest.ID) != "" && info.Manifest.ID != id {
				issues = append(issues, fmt.Sprintf("plugins.entries.%s manifest id mismatch: %q", id, info.Manifest.ID))
				continue
			}
		} else {
			info, ok = manifestIndex[id]
			if !ok {
				issues = append(issues, fmt.Sprintf("plugins.entries.%s missing manifest", id))
				continue
			}
		}

		if err := validateManifest(info.Manifest); err != nil {
			issues = append(issues, fmt.Sprintf("plugins.entries.%s invalid manifest: %v", id, err))
			continue
		}

		configValues := entry.Config
		if configValues == nil {
			configValues = map[string]any{}
		}
		if err := info.Manifest.ValidateConfig(configValues); err != nil {
			issues = append(issues, fmt.Sprintf("plugins.entries.%s config invalid: %v", id, err))
		}
	}

	return issues
}
