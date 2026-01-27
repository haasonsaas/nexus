package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFix_NonexistentPaths(t *testing.T) {
	result := Fix(FixOptions{
		StateDir:   "/nonexistent/path/abc123",
		ConfigPath: "/nonexistent/path/config.yaml",
		DryRun:     false,
	})

	// Should have actions but all skipped (paths don't exist)
	if len(result.Actions) < 2 {
		t.Errorf("expected at least 2 actions, got %d", len(result.Actions))
	}

	// Count skipped
	skipped := 0
	for _, action := range result.Actions {
		if action.Skipped != "" {
			skipped++
		}
	}

	if skipped < 2 {
		t.Errorf("expected at least 2 skipped actions, got %d", skipped)
	}
}

func TestFix_FilePermissions(t *testing.T) {
	// Create a test directory
	tmpDir := t.TempDir()

	// Create a file with insecure permissions
	testFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	result := Fix(FixOptions{
		ConfigPath: testFile,
		DryRun:     false,
	})

	// Should have fixed the file
	foundFix := false
	for _, action := range result.Actions {
		if action.Path == testFile && action.Success {
			foundFix = true
			break
		}
	}

	if !foundFix {
		t.Error("expected to find successful fix for config file")
	}

	// Verify permissions were changed
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestFix_DirectoryPermissions(t *testing.T) {
	// Create a test directory with insecure permissions
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	result := Fix(FixOptions{
		StateDir: stateDir,
		DryRun:   false,
	})

	// Should have fixed the directory
	foundFix := false
	for _, action := range result.Actions {
		if action.Path == stateDir && action.Success {
			foundFix = true
			break
		}
	}

	if !foundFix {
		t.Error("expected to find successful fix for state directory")
	}

	// Verify permissions were changed
	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0700 {
		t.Errorf("expected permissions 0700, got %o", info.Mode().Perm())
	}
}

func TestFix_DryRun(t *testing.T) {
	// Create a test directory
	tmpDir := t.TempDir()

	// Create a file with insecure permissions
	testFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	result := Fix(FixOptions{
		ConfigPath: testFile,
		DryRun:     true,
	})

	// Should report it would fix
	if result.FixedCount != 1 {
		t.Errorf("expected 1 fix action in dry run, got %d", result.FixedCount)
	}

	// Verify permissions were NOT changed
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0644 {
		t.Errorf("dry run should not change permissions, expected 0644, got %o", info.Mode().Perm())
	}
}

func TestFix_AlreadySecure(t *testing.T) {
	// Create a test directory
	tmpDir := t.TempDir()

	// Create a file with already secure permissions
	testFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	result := Fix(FixOptions{
		ConfigPath: testFile,
		DryRun:     false,
	})

	// Should be skipped
	foundSkip := false
	for _, action := range result.Actions {
		if action.Path == testFile && action.Skipped == "already has correct permissions" {
			foundSkip = true
			break
		}
	}

	if !foundSkip {
		t.Error("expected to find skipped action for already secure file")
	}
}

func TestFix_SensitiveSubdirectories(t *testing.T) {
	// Create a test directory structure
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "state")

	// Create sensitive subdirectory with insecure permissions
	credDir := filepath.Join(stateDir, "credentials")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a file in the credentials directory
	credFile := filepath.Join(credDir, "auth.json")
	if err := os.WriteFile(credFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	result := Fix(FixOptions{
		StateDir: stateDir,
		DryRun:   false,
	})

	// Should have fixed both directory and file
	fixedDir := false
	fixedFile := false
	for _, action := range result.Actions {
		if action.Path == credDir && action.Success {
			fixedDir = true
		}
		if action.Path == credFile && action.Success {
			fixedFile = true
		}
	}

	if !fixedDir {
		t.Error("expected credentials directory to be fixed")
	}
	if !fixedFile {
		t.Error("expected credentials file to be fixed")
	}

	// Verify permissions
	dirInfo, _ := os.Stat(credDir)
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("expected directory permissions 0700, got %o", dirInfo.Mode().Perm())
	}

	fileInfo, _ := os.Stat(credFile)
	if fileInfo.Mode().Perm() != 0600 {
		t.Errorf("expected file permissions 0600, got %o", fileInfo.Mode().Perm())
	}
}

func TestFix_SkipsSymlinks(t *testing.T) {
	// Create a test directory
	tmpDir := t.TempDir()

	// Create a real file
	realFile := filepath.Join(tmpDir, "real.yaml")
	if err := os.WriteFile(realFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink to it
	symlink := filepath.Join(tmpDir, "config.yaml")
	if err := os.Symlink(realFile, symlink); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	result := Fix(FixOptions{
		ConfigPath: symlink,
		DryRun:     false,
	})

	// Should skip the symlink
	foundSkip := false
	for _, action := range result.Actions {
		if action.Path == symlink && action.Skipped == "symlink (not modified for safety)" {
			foundSkip = true
			break
		}
	}

	if !foundSkip {
		t.Error("expected symlink to be skipped")
	}
}

func TestFixResult_Counts(t *testing.T) {
	// Create a test directory with various scenarios
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	// File that needs fixing
	insecureFile := filepath.Join(stateDir, "config.yaml")
	if err := os.WriteFile(insecureFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// File that's already secure
	secureFile := filepath.Join(stateDir, "secrets.yaml")
	if err := os.WriteFile(secureFile, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	result := Fix(FixOptions{
		StateDir: stateDir,
		DryRun:   false,
	})

	// Should have some fixed, some skipped, no errors
	if result.FixedCount == 0 {
		t.Error("expected at least one fix")
	}
	if result.SkippedCount == 0 {
		t.Error("expected at least one skip")
	}
	if result.ErrorCount != 0 {
		t.Errorf("expected no errors, got %d", result.ErrorCount)
	}

	// Total should match actions
	total := result.FixedCount + result.SkippedCount + result.ErrorCount
	if total != len(result.Actions) {
		t.Errorf("counts don't match actions: %d + %d + %d != %d",
			result.FixedCount, result.SkippedCount, result.ErrorCount, len(result.Actions))
	}
}
