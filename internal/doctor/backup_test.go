package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupConfig(t *testing.T) {
	t.Run("creates backup of existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		// Create a test config file
		content := "test: content\nkey: value\n"
		if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to create test config: %v", err)
		}

		backupPath, err := BackupConfig(configPath)
		if err != nil {
			t.Fatalf("BackupConfig() error = %v", err)
		}

		// Verify backup was created
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			t.Error("backup file was not created")
		}

		// Verify backup contains same content
		backupContent, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("failed to read backup: %v", err)
		}
		if string(backupContent) != content {
			t.Errorf("backup content = %q, want %q", string(backupContent), content)
		}

		// Verify backup path format
		if !strings.HasPrefix(backupPath, configPath+".bak-") {
			t.Errorf("backup path %q doesn't have expected prefix", backupPath)
		}
	})

	t.Run("returns error for empty path", func(t *testing.T) {
		_, err := BackupConfig("")
		if err == nil {
			t.Error("expected error for empty path")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("error = %q, expected to contain 'empty'", err.Error())
		}
	})

	t.Run("returns error for whitespace path", func(t *testing.T) {
		_, err := BackupConfig("   ")
		if err == nil {
			t.Error("expected error for whitespace path")
		}
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		_, err := BackupConfig("/nonexistent/path/config.yaml")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("preserves file permissions", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		// Create with specific permissions
		if err := os.WriteFile(configPath, []byte("test"), 0o600); err != nil {
			t.Fatalf("failed to create test config: %v", err)
		}

		backupPath, err := BackupConfig(configPath)
		if err != nil {
			t.Fatalf("BackupConfig() error = %v", err)
		}

		// Check permissions match
		origInfo, _ := os.Stat(configPath)
		backupInfo, _ := os.Stat(backupPath)

		if origInfo.Mode().Perm() != backupInfo.Mode().Perm() {
			t.Errorf("backup permissions = %v, want %v", backupInfo.Mode().Perm(), origInfo.Mode().Perm())
		}
	})
}
