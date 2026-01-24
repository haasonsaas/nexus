package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	json5 "github.com/yosuke-furukawa/json5/encoding/json5"
	"gopkg.in/yaml.v3"
)

const includeKey = "$include"

// LoadRaw reads a configuration file into a merged raw map, resolving $include directives.
func LoadRaw(path string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("config path is required")
	}
	seen := map[string]bool{}
	return loadRawRecursive(path, seen)
}

// loadRawRecursive loads a config file, resolving $include directives with cycle detection.
func loadRawRecursive(path string, seen map[string]bool) (map[string]any, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if seen[absPath] {
		return nil, fmt.Errorf("config include cycle detected at %s", absPath)
	}
	seen[absPath] = true
	defer delete(seen, absPath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	expanded := os.ExpandEnv(string(data))
	raw, err := parseRawBytes([]byte(expanded), absPath)
	if err != nil {
		return nil, err
	}

	includes, err := extractIncludes(raw)
	if err != nil {
		return nil, err
	}

	merged := map[string]any{}
	if len(includes) > 0 {
		baseDir := filepath.Dir(absPath)
		for _, inc := range includes {
			if strings.TrimSpace(inc) == "" {
				continue
			}
			incPath := inc
			if !filepath.IsAbs(incPath) {
				incPath = filepath.Join(baseDir, incPath)
			}
			incRaw, err := loadRawRecursive(incPath, seen)
			if err != nil {
				return nil, err
			}
			merged = mergeMaps(merged, incRaw)
		}
	}

	merged = mergeMaps(merged, raw)
	return merged, nil
}

func parseRawBytes(data []byte, pathHint string) (map[string]any, error) {
	format := strings.ToLower(filepath.Ext(pathHint))
	if format == ".json" || format == ".json5" {
		var raw map[string]any
		if err := json5.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if raw == nil {
			raw = map[string]any{}
		}
		return raw, nil
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("failed to parse config: expected single document")
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, nil
}

func extractIncludes(raw map[string]any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	var includeVal any
	if val, ok := raw[includeKey]; ok {
		includeVal = val
		delete(raw, includeKey)
	} else if val, ok := raw["include"]; ok {
		includeVal = val
		delete(raw, "include")
	}
	if includeVal == nil {
		return nil, nil
	}

	switch typed := includeVal.(type) {
	case string:
		return []string{typed}, nil
	case []string:
		return typed, nil
	case []any:
		paths := make([]string, 0, len(typed))
		for _, entry := range typed {
			value, ok := entry.(string)
			if !ok {
				return nil, fmt.Errorf("include entries must be strings")
			}
			paths = append(paths, value)
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("include must be a string or list of strings")
	}
}

func mergeMaps(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range src {
		if valueMap, ok := value.(map[string]any); ok {
			if existing, ok := dst[key].(map[string]any); ok {
				dst[key] = mergeMaps(existing, valueMap)
				continue
			}
		}
		dst[key] = value
	}
	return dst
}

func decodeRawConfig(raw map[string]any) (*Config, error) {
	payload, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize config: %w", err)
	}
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("failed to parse config: expected single document")
	}
	return &cfg, nil
}
