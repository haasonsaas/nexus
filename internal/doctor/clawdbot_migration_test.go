package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClawdbotWorkspaceFiles(t *testing.T) {
	expectedFiles := []string{"AGENTS.md", "SOUL.md", "USER.md", "IDENTITY.md", "MEMORY.md"}

	if len(ClawdbotWorkspaceFiles) != len(expectedFiles) {
		t.Errorf("ClawdbotWorkspaceFiles has %d files, want %d", len(ClawdbotWorkspaceFiles), len(expectedFiles))
	}

	for _, expected := range expectedFiles {
		found := false
		for _, actual := range ClawdbotWorkspaceFiles {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ClawdbotWorkspaceFiles missing %q", expected)
		}
	}
}

func TestValidateClawdbotWorkspace(t *testing.T) {
	t.Run("valid with SOUL.md", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "SOUL.md"), []byte("content"), 0o644)

		valid, missing := ValidateClawdbotWorkspace(tmpDir)
		if !valid {
			t.Error("expected valid workspace with SOUL.md")
		}
		if len(missing) != 4 { // Should be missing 4 other files
			t.Errorf("missing = %v, want 4 missing files", missing)
		}
	})

	t.Run("valid with IDENTITY.md", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "IDENTITY.md"), []byte("content"), 0o644)

		valid, _ := ValidateClawdbotWorkspace(tmpDir)
		if !valid {
			t.Error("expected valid workspace with IDENTITY.md")
		}
	})

	t.Run("invalid without SOUL.md or IDENTITY.md", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("content"), 0o644)
		os.WriteFile(filepath.Join(tmpDir, "USER.md"), []byte("content"), 0o644)

		valid, missing := ValidateClawdbotWorkspace(tmpDir)
		if valid {
			t.Error("expected invalid workspace without SOUL.md or IDENTITY.md")
		}
		// Should have SOUL.md and IDENTITY.md in missing
		hasSoul := false
		hasIdentity := false
		for _, m := range missing {
			if m == "SOUL.md" {
				hasSoul = true
			}
			if m == "IDENTITY.md" {
				hasIdentity = true
			}
		}
		if !hasSoul || !hasIdentity {
			t.Errorf("missing = %v, expected to include SOUL.md and IDENTITY.md", missing)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		valid, missing := ValidateClawdbotWorkspace(tmpDir)
		if valid {
			t.Error("expected invalid for empty directory")
		}
		if len(missing) != len(ClawdbotWorkspaceFiles) {
			t.Errorf("missing = %d, want %d", len(missing), len(ClawdbotWorkspaceFiles))
		}
	})
}

func TestMigrateClawdbotWorkspace(t *testing.T) {
	t.Run("migrates files successfully", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		// Create source files
		os.WriteFile(filepath.Join(sourceDir, "SOUL.md"), []byte("soul content"), 0o644)
		os.WriteFile(filepath.Join(sourceDir, "USER.md"), []byte("user content"), 0o644)

		result, err := MigrateClawdbotWorkspace(sourceDir, targetDir, false)
		if err != nil {
			t.Fatalf("MigrateClawdbotWorkspace() error = %v", err)
		}

		// Verify copied files
		if len(result.CopiedFiles) != 2 {
			t.Errorf("CopiedFiles = %v, want 2 files", result.CopiedFiles)
		}

		// Verify content was copied
		content, err := os.ReadFile(filepath.Join(targetDir, "SOUL.md"))
		if err != nil || string(content) != "soul content" {
			t.Error("SOUL.md content not copied correctly")
		}

		// Verify new files were created
		if len(result.CreatedFiles) == 0 {
			t.Error("expected new files to be created (TOOLS.md, HEARTBEAT.md)")
		}
	})

	t.Run("skips existing files without overwrite", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		// Create source file
		os.WriteFile(filepath.Join(sourceDir, "SOUL.md"), []byte("new content"), 0o644)
		// Create existing target file
		os.WriteFile(filepath.Join(targetDir, "SOUL.md"), []byte("existing content"), 0o644)

		result, err := MigrateClawdbotWorkspace(sourceDir, targetDir, false)
		if err != nil {
			t.Fatalf("MigrateClawdbotWorkspace() error = %v", err)
		}

		// Verify file was skipped
		found := false
		for _, s := range result.SkippedFiles {
			if strings.Contains(s, "SOUL.md") && strings.Contains(s, "already exists") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SkippedFiles = %v, expected SOUL.md to be skipped", result.SkippedFiles)
		}

		// Verify content was not overwritten
		content, _ := os.ReadFile(filepath.Join(targetDir, "SOUL.md"))
		if string(content) != "existing content" {
			t.Error("existing file was overwritten")
		}
	})

	t.Run("overwrites files with overwrite flag", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		// Create source file
		os.WriteFile(filepath.Join(sourceDir, "SOUL.md"), []byte("new content"), 0o644)
		// Create existing target file
		os.WriteFile(filepath.Join(targetDir, "SOUL.md"), []byte("existing content"), 0o644)

		result, err := MigrateClawdbotWorkspace(sourceDir, targetDir, true)
		if err != nil {
			t.Fatalf("MigrateClawdbotWorkspace() error = %v", err)
		}

		// Verify file was copied (not skipped)
		found := false
		for _, c := range result.CopiedFiles {
			if c == "SOUL.md" {
				found = true
				break
			}
		}
		if !found {
			t.Error("SOUL.md should have been copied with overwrite=true")
		}

		// Verify content was overwritten
		content, _ := os.ReadFile(filepath.Join(targetDir, "SOUL.md"))
		if string(content) != "new content" {
			t.Error("existing file was not overwritten")
		}
	})

	t.Run("returns error for nonexistent source", func(t *testing.T) {
		_, err := MigrateClawdbotWorkspace("/nonexistent/source", t.TempDir(), false)
		if err == nil {
			t.Error("expected error for nonexistent source")
		}
	})

	t.Run("returns error for file source (not directory)", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "file")
		os.WriteFile(filePath, []byte("content"), 0o644)

		_, err := MigrateClawdbotWorkspace(filePath, t.TempDir(), false)
		if err == nil {
			t.Error("expected error for file source")
		}
		if !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("error = %q, expected to mention 'not a directory'", err.Error())
		}
	})

	t.Run("creates target directory if needed", func(t *testing.T) {
		sourceDir := t.TempDir()
		os.WriteFile(filepath.Join(sourceDir, "SOUL.md"), []byte("content"), 0o644)

		targetDir := filepath.Join(t.TempDir(), "nested", "target")

		_, err := MigrateClawdbotWorkspace(sourceDir, targetDir, false)
		if err != nil {
			t.Fatalf("MigrateClawdbotWorkspace() error = %v", err)
		}

		// Verify target directory was created
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			t.Error("target directory was not created")
		}
	})
}

func TestMigrateClawdbotConfig(t *testing.T) {
	t.Run("sets version to 1", func(t *testing.T) {
		sourceConfig := map[string]any{}

		nexusConfig, result, err := MigrateClawdbotConfig(sourceConfig)
		if err != nil {
			t.Fatalf("MigrateClawdbotConfig() error = %v", err)
		}

		if nexusConfig["version"] != 1 {
			t.Errorf("version = %v, want 1", nexusConfig["version"])
		}

		found := false
		for _, m := range result.ConfigMigrations {
			if strings.Contains(m, "version") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected version migration to be logged")
		}
	})

	t.Run("copies identity section", func(t *testing.T) {
		sourceConfig := map[string]any{
			"identity": map[string]any{
				"name": "TestAgent",
			},
		}

		nexusConfig, result, err := MigrateClawdbotConfig(sourceConfig)
		if err != nil {
			t.Fatalf("MigrateClawdbotConfig() error = %v", err)
		}

		if nexusConfig["identity"] == nil {
			t.Error("identity section not copied")
		}

		found := false
		for _, m := range result.ConfigMigrations {
			if strings.Contains(m, "identity") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected identity migration to be logged")
		}
	})

	t.Run("moves plugins to tools", func(t *testing.T) {
		sourceConfig := map[string]any{
			"plugins": map[string]any{
				"sandbox":   map[string]any{"enabled": true},
				"browser":   map[string]any{"enabled": true},
				"websearch": map[string]any{"enabled": true},
				"other":     map[string]any{"enabled": true}, // Should not be migrated
			},
		}

		nexusConfig, result, err := MigrateClawdbotConfig(sourceConfig)
		if err != nil {
			t.Fatalf("MigrateClawdbotConfig() error = %v", err)
		}

		tools, ok := nexusConfig["tools"].(map[string]any)
		if !ok {
			t.Fatal("tools section not created")
		}

		if tools["sandbox"] == nil {
			t.Error("sandbox not migrated to tools")
		}
		if tools["browser"] == nil {
			t.Error("browser not migrated to tools")
		}
		if tools["websearch"] == nil {
			t.Error("websearch not migrated to tools")
		}
		if tools["other"] != nil {
			t.Error("other plugin should not be migrated to tools")
		}

		// Check migrations logged
		sandboxMigrated := false
		for _, m := range result.ConfigMigrations {
			if strings.Contains(m, "plugins.sandbox -> tools.sandbox") {
				sandboxMigrated = true
				break
			}
		}
		if !sandboxMigrated {
			t.Error("sandbox migration not logged")
		}
	})

	t.Run("moves memory to session.memory", func(t *testing.T) {
		sourceConfig := map[string]any{
			"memory": map[string]any{
				"enabled": true,
			},
		}

		nexusConfig, result, err := MigrateClawdbotConfig(sourceConfig)
		if err != nil {
			t.Fatalf("MigrateClawdbotConfig() error = %v", err)
		}

		session, ok := nexusConfig["session"].(map[string]any)
		if !ok {
			t.Fatal("session section not created")
		}
		if session["memory"] == nil {
			t.Error("memory not moved to session.memory")
		}

		found := false
		for _, m := range result.ConfigMigrations {
			if strings.Contains(m, "memory -> session.memory") {
				found = true
				break
			}
		}
		if !found {
			t.Error("memory migration not logged")
		}
	})

	t.Run("warns about multi-agent config", func(t *testing.T) {
		sourceConfig := map[string]any{
			"agents": []any{
				map[string]any{"name": "agent1"},
				map[string]any{"name": "agent2"},
			},
		}

		_, result, err := MigrateClawdbotConfig(sourceConfig)
		if err != nil {
			t.Fatalf("MigrateClawdbotConfig() error = %v", err)
		}

		found := false
		for _, w := range result.ConfigWarnings {
			if strings.Contains(w, "agents") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected warning about multiple agents")
		}
	})

	t.Run("warns about observability", func(t *testing.T) {
		sourceConfig := map[string]any{
			"observability": map[string]any{
				"enabled": true,
			},
		}

		_, result, err := MigrateClawdbotConfig(sourceConfig)
		if err != nil {
			t.Fatalf("MigrateClawdbotConfig() error = %v", err)
		}

		found := false
		for _, w := range result.ConfigWarnings {
			if strings.Contains(w, "observability") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected warning about observability")
		}
	})
}

func TestFormatMigrationResult(t *testing.T) {
	result := &ClawdbotMigrationResult{
		SourceWorkspace:  "/source/path",
		TargetWorkspace:  "/target/path",
		CopiedFiles:      []string{"SOUL.md", "USER.md"},
		SkippedFiles:     []string{"AGENTS.md (not found)"},
		CreatedFiles:     []string{"TOOLS.md"},
		ConfigMigrations: []string{"set version to 1"},
		ConfigWarnings:   []string{"multi-agent warning"},
	}

	output := FormatMigrationResult(result)

	// Check sections are present
	if !strings.Contains(output, "Clawdbot Workspace Migration") {
		t.Error("missing title")
	}
	if !strings.Contains(output, "/source/path") {
		t.Error("missing source path")
	}
	if !strings.Contains(output, "/target/path") {
		t.Error("missing target path")
	}
	if !strings.Contains(output, "Copied files:") {
		t.Error("missing copied files section")
	}
	if !strings.Contains(output, "SOUL.md") {
		t.Error("missing SOUL.md in copied files")
	}
	if !strings.Contains(output, "Skipped files:") {
		t.Error("missing skipped files section")
	}
	if !strings.Contains(output, "Created files") {
		t.Error("missing created files section")
	}
	if !strings.Contains(output, "TOOLS.md") {
		t.Error("missing TOOLS.md in created files")
	}
	if !strings.Contains(output, "Config migrations:") {
		t.Error("missing config migrations section")
	}
	if !strings.Contains(output, "Warnings:") {
		t.Error("missing warnings section")
	}
}

func TestGetDefaultToolsContent(t *testing.T) {
	content := getDefaultToolsContent()
	if content == "" {
		t.Error("getDefaultToolsContent returned empty string")
	}
}

func TestGetDefaultHeartbeatContent(t *testing.T) {
	content := getDefaultHeartbeatContent()
	if content == "" {
		t.Error("getDefaultHeartbeatContent returned empty string")
	}
}

func TestClawdbotMigrationResult_Struct(t *testing.T) {
	result := ClawdbotMigrationResult{
		CopiedFiles:      []string{"file1"},
		SkippedFiles:     []string{"file2"},
		CreatedFiles:     []string{"file3"},
		ConfigMigrations: []string{"migration1"},
		ConfigWarnings:   []string{"warning1"},
		SourceWorkspace:  "/source",
		TargetWorkspace:  "/target",
	}

	if len(result.CopiedFiles) != 1 {
		t.Errorf("CopiedFiles length = %d, want 1", len(result.CopiedFiles))
	}
	if result.SourceWorkspace != "/source" {
		t.Errorf("SourceWorkspace = %q, want %q", result.SourceWorkspace, "/source")
	}
}
