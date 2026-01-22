package marketplace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStageInstallRollbackOnActivateFailure(t *testing.T) {
	baseDir := t.TempDir()
	liveDir := filepath.Join(baseDir, "plugin")
	tempDir := filepath.Join(baseDir, "temp")

	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatalf("create live dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(liveDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	var call int
	renamer := func(old, new string) error {
		call++
		if call == 2 {
			return errors.New("activate failed")
		}
		return os.Rename(old, new)
	}

	_, _, err := stageInstall(tempDir, liveDir, renamer)
	if err == nil {
		t.Fatal("expected error")
	}

	if _, err := os.Stat(filepath.Join(liveDir, "old.txt")); err != nil {
		t.Fatalf("expected old file restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(liveDir, "new.txt")); err == nil {
		t.Fatalf("expected new file not present")
	}
}

func TestStageInstallLeavesBackupUntilCommit(t *testing.T) {
	baseDir := t.TempDir()
	liveDir := filepath.Join(baseDir, "plugin")
	tempDir := filepath.Join(baseDir, "temp")

	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatalf("create live dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(liveDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	backupPath, hadExisting, err := stageInstall(tempDir, liveDir, os.Rename)
	if err != nil {
		t.Fatalf("stageInstall error: %v", err)
	}
	if !hadExisting {
		t.Fatalf("expected existing plugin")
	}
	if backupPath == "" {
		t.Fatal("expected backup path")
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected backup to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(liveDir, "new.txt")); err != nil {
		t.Fatalf("expected new file in live dir: %v", err)
	}

	if err := rollbackInstall(liveDir, backupPath, hadExisting); err != nil {
		t.Fatalf("rollback error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(liveDir, "old.txt")); err != nil {
		t.Fatalf("expected old file after rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(liveDir, "new.txt")); err == nil {
		t.Fatalf("expected new file removed after rollback")
	}
}

func TestStageInstallNoExisting(t *testing.T) {
	baseDir := t.TempDir()
	liveDir := filepath.Join(baseDir, "plugin")
	tempDir := filepath.Join(baseDir, "temp")

	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	backupPath, hadExisting, err := stageInstall(tempDir, liveDir, os.Rename)
	if err != nil {
		t.Fatalf("stageInstall error: %v", err)
	}
	if hadExisting {
		t.Fatalf("expected no existing plugin")
	}
	if backupPath != "" {
		t.Fatalf("expected no backup path")
	}
	if _, err := os.Stat(filepath.Join(liveDir, "new.txt")); err != nil {
		t.Fatalf("expected new file in live dir: %v", err)
	}
}
