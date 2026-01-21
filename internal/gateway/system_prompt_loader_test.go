package gateway

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestReadPromptFileMissing(t *testing.T) {
	content, err := readPromptFile(filepath.Join(t.TempDir(), "missing.md"))
	if err != nil {
		t.Fatalf("readPromptFile() error = %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}
}

func TestReadPromptFileTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("\nhello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	content, err := readPromptFile(path)
	if err != nil {
		t.Fatalf("readPromptFile() error = %v", err)
	}
	if content != "hello" {
		t.Fatalf("expected trimmed content, got %q", content)
	}
}

func TestLoadToolNotesCombinesInlineAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("file notes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Notes:     "inline notes",
			NotesFile: path,
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	notes := server.loadToolNotes()
	if !strings.Contains(notes, "inline notes") || !strings.Contains(notes, "file notes") {
		t.Fatalf("expected merged notes, got %q", notes)
	}
}
