package profile

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultConfigName = "nexus.yaml"
	ProfileExt        = ".yaml"
)

// ConfigDir returns the base directory for profile configs.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "."
	}
	return filepath.Join(home, ".nexus", "profiles")
}

// ActiveProfileFile returns the path to the active profile marker.
func ActiveProfileFile() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "."
	}
	return filepath.Join(home, ".nexus", "active_profile")
}

// ProfileConfigPath returns the config path for a profile name.
func ProfileConfigPath(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultConfigName
	}
	return filepath.Join(ConfigDir(), name+ProfileExt)
}

// DefaultConfigPath returns the active profile config path if set, otherwise nexus.yaml.
func DefaultConfigPath() string {
	name, err := ReadActiveProfile()
	if err != nil || strings.TrimSpace(name) == "" {
		return DefaultConfigName
	}
	return ProfileConfigPath(name)
}

// ReadActiveProfile loads the active profile name.
func ReadActiveProfile() (string, error) {
	data, err := os.ReadFile(ActiveProfileFile())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteActiveProfile sets the active profile name.
func WriteActiveProfile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	path := ActiveProfileFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(name+"\n"), 0o644)
}

// ListProfiles returns available profile names.
func ListProfiles() ([]string, error) {
	dir := ConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ProfileExt) {
			continue
		}
		name = strings.TrimSuffix(name, ProfileExt)
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
