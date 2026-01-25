package packs

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadFromDir reads a knowledge pack from a directory.
func LoadFromDir(dir string) (*Pack, error) {
	if dir == "" {
		return nil, fmt.Errorf("pack directory is required")
	}
	path, err := findPackFile(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pack: %w", err)
	}
	var pack Pack
	if err := yaml.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("parse pack: %w", err)
	}
	if pack.Name == "" {
		return nil, fmt.Errorf("pack name is required")
	}
	if len(pack.Documents) == 0 {
		return nil, fmt.Errorf("pack has no documents")
	}
	return &pack, nil
}

func findPackFile(dir string) (string, error) {
	candidates := []string{"pack.yaml", "pack.yml"}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("pack.yaml not found in %s", dir)
}
