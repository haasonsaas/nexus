package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"gopkg.in/yaml.v3"
)

// MigrationReport records applied migrations.
type MigrationReport struct {
	Applied     []string
	FromVersion int
	ToVersion   int
}

// LoadRawConfig reads a YAML config file into a mutable map.
func LoadRawConfig(path string) (map[string]any, error) {
	return config.LoadRaw(path)
}

// WriteRawConfig writes a config map back to disk, preserving JSON/JSON5/YAML formats.
func WriteRawConfig(path string, raw map[string]any) error {
	var data []byte
	var err error
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" || ext == ".json5" {
		data, err = json.MarshalIndent(raw, "", "  ")
	} else {
		data, err = yaml.Marshal(raw)
	}
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

// ApplyConfigMigrations updates legacy config keys in-place.
func ApplyConfigMigrations(raw map[string]any) (MigrationReport, error) {
	report := MigrationReport{ToVersion: config.CurrentVersion}
	if raw == nil {
		return report, nil
	}

	version, err := parseConfigVersion(raw)
	if err != nil {
		return report, err
	}
	report.FromVersion = version
	if version < 0 {
		return report, fmt.Errorf("invalid config version %d", version)
	}
	if version > config.CurrentVersion {
		return report, &config.VersionError{Version: version, Current: config.CurrentVersion, Reason: "newer than this build"}
	}

	plugins, _ := getStringMap(raw, "plugins")
	tools, _ := getStringMap(raw, "tools")
	if plugins != nil {
		for _, key := range []string{"sandbox", "browser", "websearch"} {
			val, ok := plugins[key]
			if !ok {
				continue
			}
			delete(plugins, key)
			if tools == nil {
				tools = ensureStringMap(raw, "tools")
			}
			if tools != nil {
				if _, exists := tools[key]; exists {
					report.Applied = append(report.Applied, fmt.Sprintf("removed plugins.%s (tools.%s already set)", key, key))
					continue
				}
				tools[key] = val
				report.Applied = append(report.Applied, fmt.Sprintf("moved plugins.%s -> tools.%s", key, key))
			}
		}
	}

	if version < config.CurrentVersion {
		raw["version"] = config.CurrentVersion
		report.Applied = append(report.Applied, fmt.Sprintf("set version to %d", config.CurrentVersion))
	}

	return report, nil
}

func parseConfigVersion(raw map[string]any) (int, error) {
	if raw == nil {
		return 0, nil
	}
	value, ok := raw["version"]
	if !ok || value == nil {
		return 0, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case int32:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case float32:
		return int(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, nil
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, fmt.Errorf("invalid config version %q", typed)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("invalid config version type %T", value)
	}
}

func ensureStringMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return nil
	}
	current, ok := root[key]
	if !ok {
		m := map[string]any{}
		root[key] = m
		return m
	}

	switch value := current.(type) {
	case map[string]any:
		return value
	case map[any]any:
		converted := map[string]any{}
		for k, v := range value {
			converted[fmt.Sprint(k)] = v
		}
		root[key] = converted
		return converted
	default:
		m := map[string]any{}
		root[key] = m
		return m
	}
}

func getStringMap(root map[string]any, key string) (map[string]any, bool) {
	if root == nil {
		return nil, false
	}
	current, ok := root[key]
	if !ok {
		return nil, false
	}
	switch value := current.(type) {
	case map[string]any:
		return value, true
	case map[any]any:
		converted := map[string]any{}
		for k, v := range value {
			converted[fmt.Sprint(k)] = v
		}
		root[key] = converted
		return converted, true
	default:
		return nil, false
	}
}
