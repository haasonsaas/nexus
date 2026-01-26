package packs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	writePack(t, filepath.Join(root, "alpha"), "Alpha Pack", "Alpha description", []string{"alpha"})
	writePack(t, filepath.Join(root, "category", "beta"), "Beta Pack", "Beta description", []string{"beta"})
	writePack(t, filepath.Join(root, ".git", "hidden"), "Hidden Pack", "Hidden", []string{"hidden"})

	packs, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if len(packs) != 2 {
		t.Fatalf("expected 2 packs, got %d", len(packs))
	}

	names := []string{packs[0].Pack.Name, packs[1].Pack.Name}
	sort.Strings(names)
	if names[0] != "Alpha Pack" || names[1] != "Beta Pack" {
		t.Fatalf("unexpected pack names: %v", names)
	}

	for _, pack := range packs {
		if pack.Path == "" {
			t.Fatalf("pack path missing for %s", pack.Pack.Name)
		}
		if pack.Root == "" {
			t.Fatalf("pack root missing for %s", pack.Pack.Name)
		}
		if pack.RelativePath == "" {
			t.Fatalf("pack relative path missing for %s", pack.Pack.Name)
		}
	}
}

func TestFilterPacks(t *testing.T) {
	packs := []DiscoveredPack{
		{Pack: &Pack{Name: "Sales", Description: "Q1 pipeline", Documents: []PackDocument{{Name: "Forecast", Tags: []string{"finance"}}}}},
		{Pack: &Pack{Name: "Support", Description: "Ticket playbooks", Documents: []PackDocument{{Name: "Escalation", Tags: []string{"ops"}}}}},
	}

	filtered := FilterPacks(packs, "q1 finance")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 pack, got %d", len(filtered))
	}
	if filtered[0].Pack.Name != "Sales" {
		t.Fatalf("expected Sales pack, got %s", filtered[0].Pack.Name)
	}
}

func writePack(t *testing.T, dir, name, description string, tags []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	content := fmt.Sprintf(
		"name: %s\nversion: \"1.0\"\ndescription: %s\ndocuments:\n  - name: Doc\n    path: doc.txt\n    tags:%s\n",
		name,
		description,
		formatTags(tags),
	)
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write pack.yaml: %v", err)
	}
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return " []"
	}
	return fmt.Sprintf(" [%s]", joinTags(tags))
}

func joinTags(tags []string) string {
	joined := ""
	for i, tag := range tags {
		if i > 0 {
			joined += ", "
		}
		joined += tag
	}
	return joined
}
