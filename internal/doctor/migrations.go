package doctor

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MigrationReport records applied migrations.
type MigrationReport struct {
	Applied []string
}

// LoadRawConfig reads a YAML config file into a mutable map.
func LoadRawConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, nil
}

// WriteRawConfig writes a YAML config map back to disk.
func WriteRawConfig(path string, raw map[string]any) error {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ApplyConfigMigrations updates legacy config keys in-place.
func ApplyConfigMigrations(raw map[string]any) MigrationReport {
	report := MigrationReport{}
	if raw == nil {
		return report
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

	if _, ok := raw["observability"]; ok {
		delete(raw, "observability")
		report.Applied = append(report.Applied, "removed observability (unsupported)")
	}

	return report
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
