package memorysearch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryGetTool_ReadsSnippet(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	memFile := filepath.Join(root, "MEMORY.md")
	if err := os.WriteFile(memFile, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := &Config{
		Directory:     "memory",
		MemoryFile:    "MEMORY.md",
		WorkspacePath: root,
	}
	tool := NewMemoryGetTool(cfg)
	params, _ := json.Marshal(map[string]interface{}{
		"path":  "MEMORY.md",
		"from":  2,
		"lines": 1,
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Content, "line2") {
		t.Fatalf("expected line2, got %s", result.Content)
	}
}

func TestMemoryGetTool_RejectsOutsidePath(t *testing.T) {
	root := t.TempDir()
	cfg := &Config{
		Directory:     "memory",
		MemoryFile:    "MEMORY.md",
		WorkspacePath: root,
	}
	tool := NewMemoryGetTool(cfg)
	params, _ := json.Marshal(map[string]interface{}{
		"path": "../secrets.txt",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for outside path")
	}
}
