package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureWorkspaceFilesCreatesMissing(t *testing.T) {
	root := t.TempDir()
	files := []BootstrapFile{{Name: "AGENTS.md", Content: "hello"}}

	result, err := EnsureWorkspaceFiles(root, files, false)
	if err != nil {
		t.Fatalf("EnsureWorkspaceFiles() error = %v", err)
	}
	if len(result.Created) != 1 {
		t.Fatalf("expected 1 created file, got %d", len(result.Created))
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped files, got %d", len(result.Skipped))
	}

	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "hello" {
		t.Fatalf("expected content to be written, got %q", string(data))
	}
}

func TestEnsureWorkspaceFilesSkipsExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "SOUL.md")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	files := []BootstrapFile{{Name: "SOUL.md", Content: "new"}}
	result, err := EnsureWorkspaceFiles(root, files, false)
	if err != nil {
		t.Fatalf("EnsureWorkspaceFiles() error = %v", err)
	}
	if len(result.Created) != 0 {
		t.Fatalf("expected 0 created files, got %d", len(result.Created))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(result.Skipped))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "existing" {
		t.Fatalf("expected existing content to be preserved, got %q", string(data))
	}
}

func TestEnsureWorkspaceFilesOverwrites(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "USER.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	files := []BootstrapFile{{Name: "USER.md", Content: "new"}}
	result, err := EnsureWorkspaceFiles(root, files, true)
	if err != nil {
		t.Fatalf("EnsureWorkspaceFiles() error = %v", err)
	}
	if len(result.Created) != 1 {
		t.Fatalf("expected 1 created file, got %d", len(result.Created))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(data)) != "new" {
		t.Fatalf("expected overwritten content, got %q", string(data))
	}
}
