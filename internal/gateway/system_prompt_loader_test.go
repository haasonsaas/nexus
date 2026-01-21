package gateway

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
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

func TestLoadToolNotesUsesWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TOOLS.md")
	if err := os.WriteFile(path, []byte("workspace tools"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Enabled:      true,
			Path:         dir,
			MaxChars:     100,
			ToolsFile:    "TOOLS.md",
			AgentsFile:   "AGENTS.md",
			SoulFile:     "SOUL.md",
			UserFile:     "USER.md",
			IdentityFile: "IDENTITY.md",
			MemoryFile:   "MEMORY.md",
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	notes := server.loadToolNotes()
	if !strings.Contains(notes, "workspace tools") {
		t.Fatalf("expected workspace tool notes, got %q", notes)
	}
}

func TestLoadHeartbeatOnDemand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heartbeat.md")
	if err := os.WriteFile(path, []byte("check status"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Session: config.SessionConfig{
			Heartbeat: config.HeartbeatConfig{
				Enabled: true,
				File:    path,
				Mode:    "on_demand",
			},
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	msg := &models.Message{Content: "hello"}
	if heartbeat := server.loadHeartbeat(msg); heartbeat != "" {
		t.Fatalf("expected heartbeat to be empty, got %q", heartbeat)
	}

	msg = &models.Message{Content: "heartbeat"}
	if heartbeat := server.loadHeartbeat(msg); !strings.Contains(heartbeat, "check status") {
		t.Fatalf("expected heartbeat content, got %q", heartbeat)
	}
}

func TestReadPromptFileLimited(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.md")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	content, err := readPromptFileLimited(path, 5)
	if err != nil {
		t.Fatalf("readPromptFileLimited() error = %v", err)
	}
	if !strings.HasPrefix(content, "01234") {
		t.Fatalf("expected truncated content, got %q", content)
	}
	if !strings.Contains(content, "truncated") {
		t.Fatalf("expected truncation marker, got %q", content)
	}
}

func TestLoadWorkspaceSectionsFromConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Do the thing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("Be kind"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("Remember this"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Enabled:      true,
			Path:         dir,
			MaxChars:     100,
			AgentsFile:   "AGENTS.md",
			SoulFile:     "SOUL.md",
			UserFile:     "USER.md",
			IdentityFile: "IDENTITY.md",
			MemoryFile:   "MEMORY.md",
		},
	}

	sections, err := loadWorkspaceSectionsFromConfig(cfg)
	if err != nil {
		t.Fatalf("loadWorkspaceSectionsFromConfig() error = %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	if sections[0].Label != "Workspace instructions" {
		t.Fatalf("expected workspace instructions first, got %q", sections[0].Label)
	}
	if sections[1].Label != "Persona and boundaries" {
		t.Fatalf("expected persona second, got %q", sections[1].Label)
	}
	if sections[2].Label != "Workspace memory" {
		t.Fatalf("expected workspace memory third, got %q", sections[2].Label)
	}
}
