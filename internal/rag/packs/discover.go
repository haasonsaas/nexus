package packs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoveredPack captures a pack found under a root directory.
type DiscoveredPack struct {
	Pack         *Pack
	Path         string
	Root         string
	RelativePath string
}

// Discover scans a root directory for knowledge packs.
func Discover(root string) ([]DiscoveredPack, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("pack root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve pack root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat pack root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("pack root is not a directory: %s", absRoot)
	}

	var packs []DiscoveredPack
	var errs []error

	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			errs = append(errs, fmt.Errorf("scan %s: %w", path, walkErr))
			return nil
		}
		if entry == nil || !entry.IsDir() {
			return nil
		}
		if path != absRoot && shouldSkipDir(entry.Name()) {
			return fs.SkipDir
		}
		if !hasPackFile(path) {
			return nil
		}
		pack, err := LoadFromDir(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("load pack %s: %w", path, err))
			return fs.SkipDir
		}
		relPath, err := filepath.Rel(absRoot, path)
		if err != nil {
			relPath = ""
		}
		if relPath == "." {
			relPath = filepath.Base(path)
		}
		packs = append(packs, DiscoveredPack{
			Pack:         pack,
			Path:         path,
			Root:         absRoot,
			RelativePath: relPath,
		})
		return fs.SkipDir
	})
	if err != nil {
		errs = append(errs, err)
	}

	sort.Slice(packs, func(i, j int) bool {
		return strings.ToLower(packs[i].Pack.Name) < strings.ToLower(packs[j].Pack.Name)
	})
	return packs, errors.Join(errs...)
}

// FilterPacks filters packs by a search query.
func FilterPacks(packs []DiscoveredPack, query string) []DiscoveredPack {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(terms) == 0 {
		return packs
	}
	filtered := make([]DiscoveredPack, 0, len(packs))
	for _, pack := range packs {
		if packMatchesTerms(pack, terms) {
			filtered = append(filtered, pack)
		}
	}
	return filtered
}

func packMatchesTerms(pack DiscoveredPack, terms []string) bool {
	if pack.Pack == nil {
		return false
	}
	haystack := buildPackSearchText(pack)
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

func buildPackSearchText(pack DiscoveredPack) string {
	var builder strings.Builder
	builder.WriteString(pack.Pack.Name)
	builder.WriteString(" ")
	builder.WriteString(pack.Pack.Version)
	builder.WriteString(" ")
	builder.WriteString(pack.Pack.Description)
	builder.WriteString(" ")
	builder.WriteString(pack.Path)
	builder.WriteString(" ")
	builder.WriteString(pack.RelativePath)
	for _, doc := range pack.Pack.Documents {
		builder.WriteString(" ")
		builder.WriteString(doc.Name)
		builder.WriteString(" ")
		builder.WriteString(doc.Path)
		builder.WriteString(" ")
		builder.WriteString(doc.ContentType)
		builder.WriteString(" ")
		builder.WriteString(doc.Source)
		for _, tag := range doc.Tags {
			builder.WriteString(" ")
			builder.WriteString(tag)
		}
	}
	return strings.ToLower(builder.String())
}

func hasPackFile(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "pack.yaml")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "pack.yml")); err == nil {
		return true
	}
	return false
}

func shouldSkipDir(name string) bool {
	if name == "" {
		return false
	}
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor", ".idea", ".vscode", "__pycache__":
		return true
	}
	return strings.HasPrefix(name, ".")
}
