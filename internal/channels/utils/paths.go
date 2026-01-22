// Package utils provides shared utilities for channel adapters.
package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands ~ to the user's home directory.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ExpandPathWithDefault expands ~ and returns defaultPath if path is empty.
func ExpandPathWithDefault(path, defaultPath string) string {
	if strings.TrimSpace(path) == "" {
		return ExpandPath(defaultPath)
	}
	return ExpandPath(path)
}

// EnsureDir creates a directory and all parent directories if they don't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// EnsureParentDir creates the parent directory of a file path.
func EnsureParentDir(filePath string) error {
	return EnsureDir(filepath.Dir(filePath))
}
