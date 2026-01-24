package files

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverRejectsEscape(t *testing.T) {
	root := t.TempDir()
	resolver := Resolver{Root: root}
	_, err := resolver.Resolve("../outside.txt")
	if err == nil {
		t.Fatal("expected escape to be rejected")
	}
}

func TestReadWriteEdit(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Workspace: root, MaxReadBytes: 10}

	writeTool := NewWriteTool(cfg)
	readTool := NewReadTool(cfg)
	editTool := NewEditTool(cfg)

	writeParams, _ := json.Marshal(map[string]interface{}{
		"path":    "notes.txt",
		"content": "hello world",
	})
	if _, err := writeTool.Execute(context.Background(), writeParams); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	readParams, _ := json.Marshal(map[string]interface{}{
		"path": "notes.txt",
	})
	result, err := readTool.Execute(context.Background(), readParams)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("expected content, got %s", result.Content)
	}

	editParams, _ := json.Marshal(map[string]interface{}{
		"path": "notes.txt",
		"edits": []map[string]interface{}{
			{
				"old_text": "world",
				"new_text": "nexus",
			},
		},
	})
	if _, err := editTool.Execute(context.Background(), editParams); err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello nexus" {
		t.Fatalf("unexpected content: %s", string(data))
	}
}

func TestApplyPatch(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Workspace: root}
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool := NewApplyPatchTool(cfg)
	patch := strings.Join([]string{
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -1,3 +1,3 @@",
		" a",
		"-b",
		"+bb",
		" c",
		"",
	}, "\n")

	params, _ := json.Marshal(map[string]interface{}{"patch": patch})
	if _, err := tool.Execute(context.Background(), params); err != nil {
		t.Fatalf("apply patch failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "a\nbb\nc\n" {
		t.Fatalf("unexpected content: %s", string(data))
	}
}
