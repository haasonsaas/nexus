package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resolver resolves and validates workspace-relative paths.
type Resolver struct {
	Root string
}

// Resolve returns an absolute, cleaned path within the workspace root.
func (r Resolver) Resolve(path string) (string, error) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return "", fmt.Errorf("path is required")
	}
	root := strings.TrimSpace(r.Root)
	if root == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	var target string
	if filepath.IsAbs(clean) {
		target = filepath.Clean(clean)
	} else {
		target = filepath.Join(rootAbs, clean)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return targetAbs, nil
}
