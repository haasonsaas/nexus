package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestRepairWorkspaceDisabled(t *testing.T) {
	cfg := &config.Config{Workspace: config.WorkspaceConfig{Enabled: false}}
	result, err := RepairWorkspace(cfg)
	if err != nil {
		t.Fatalf("RepairWorkspace() error = %v", err)
	}
	if len(result.Created) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("expected no changes when disabled")
	}
}

func TestRepairWorkspaceCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Enabled:      true,
			Path:         dir,
			AgentsFile:   "AGENTS.md",
			SoulFile:     "SOUL.md",
			UserFile:     "USER.md",
			IdentityFile: "IDENTITY.md",
			ToolsFile:    "TOOLS.md",
			MemoryFile:   "MEMORY.md",
		},
	}

	result, err := RepairWorkspace(cfg)
	if err != nil {
		t.Fatalf("RepairWorkspace() error = %v", err)
	}
	if len(result.Created) == 0 {
		t.Fatalf("expected files to be created")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("expected AGENTS.md to exist: %v", err)
	}
}

func TestRepairHeartbeatCreatesFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{Enabled: true, Path: dir},
		Session: config.SessionConfig{
			Heartbeat: config.HeartbeatConfig{Enabled: true, File: "HEARTBEAT.md"},
		},
	}

	path, created, err := RepairHeartbeat(cfg, "")
	if err != nil {
		t.Fatalf("RepairHeartbeat() error = %v", err)
	}
	if !created {
		t.Fatalf("expected heartbeat file to be created")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected heartbeat file at %s: %v", path, err)
	}
}
