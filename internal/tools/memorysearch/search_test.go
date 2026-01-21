package memorysearch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMemorySearchToolExecute(t *testing.T) {
	dir := t.TempDir()
	memoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("Remember alpha bravo"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "2026-01-21.md"), []byte("alpha log entry"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewMemorySearchTool(&Config{
		Directory:     "memory",
		MemoryFile:    "MEMORY.md",
		WorkspacePath: dir,
		MaxResults:    5,
		MaxSnippetLen: 120,
	})

	params, _ := json.Marshal(map[string]any{"query": "alpha"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if result.Content == "" {
		t.Fatalf("expected content")
	}
}

func TestMemorySearchToolVectorMode(t *testing.T) {
	dir := t.TempDir()
	memoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "2026-01-21.md"), []byte("vector search content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewMemorySearchTool(&Config{
		Directory:     "memory",
		WorkspacePath: dir,
		Mode:          "vector",
	})

	params, _ := json.Marshal(map[string]any{"query": "vector"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if result.Content == "" {
		t.Fatalf("expected content")
	}
}
