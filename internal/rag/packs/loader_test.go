package packs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromDir(t *testing.T) {
	dir := t.TempDir()
	pack := `name: test-pack
version: "1.0"
documents:
  - name: Doc1
    path: docs/doc1.txt
`
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(pack), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}
	p, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir error: %v", err)
	}
	if p.Name != "test-pack" {
		t.Fatalf("expected pack name, got %q", p.Name)
	}
	if len(p.Documents) != 1 {
		t.Fatalf("expected documents, got %d", len(p.Documents))
	}
}
