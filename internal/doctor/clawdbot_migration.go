package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/workspace"
)

// ClawdbotMigrationResult records what was migrated from a Clawdbot workspace.
type ClawdbotMigrationResult struct {
	// Workspace file copy results
	CopiedFiles  []string
	SkippedFiles []string
	CreatedFiles []string

	// Config migration results
	ConfigMigrations []string
	ConfigWarnings   []string

	// Paths
	SourceWorkspace string
	TargetWorkspace string
}

// ClawdbotWorkspaceFiles are the workspace files to migrate.
var ClawdbotWorkspaceFiles = []string{
	"AGENTS.md",
	"SOUL.md",
	"USER.md",
	"IDENTITY.md",
	"MEMORY.md",
}

// MigrateClawdbotWorkspace copies workspace files from a Clawdbot workspace to Nexus.
func MigrateClawdbotWorkspace(sourcePath, targetPath string, overwrite bool) (*ClawdbotMigrationResult, error) {
	result := &ClawdbotMigrationResult{
		SourceWorkspace: sourcePath,
		TargetWorkspace: targetPath,
	}

	// Verify source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("source workspace not found: %w", err)
	}
	if !sourceInfo.IsDir() {
		return nil, fmt.Errorf("source workspace is not a directory: %s", sourcePath)
	}

	// Create target directory if needed
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return nil, fmt.Errorf("create target workspace: %w", err)
	}

	// Copy workspace files
	for _, filename := range ClawdbotWorkspaceFiles {
		srcFile := filepath.Join(sourcePath, filename)
		dstFile := filepath.Join(targetPath, filename)

		// Check if source exists
		if _, err := os.Stat(srcFile); os.IsNotExist(err) {
			result.SkippedFiles = append(result.SkippedFiles, fmt.Sprintf("%s (not found in source)", filename))
			continue
		}

		// Check if target exists (skip if not overwriting)
		if _, err := os.Stat(dstFile); err == nil && !overwrite {
			result.SkippedFiles = append(result.SkippedFiles, fmt.Sprintf("%s (already exists)", filename))
			continue
		}

		// Copy the file
		if err := copyFile(srcFile, dstFile); err != nil {
			return nil, fmt.Errorf("copy %s: %w", filename, err)
		}
		result.CopiedFiles = append(result.CopiedFiles, filename)
	}

	// Create TOOLS.md if it doesn't exist (new in Nexus)
	toolsFile := filepath.Join(targetPath, "TOOLS.md")
	if _, err := os.Stat(toolsFile); os.IsNotExist(err) {
		if err := os.WriteFile(toolsFile, []byte(getDefaultToolsContent()), 0o644); err != nil {
			return nil, fmt.Errorf("create TOOLS.md: %w", err)
		}
		result.CreatedFiles = append(result.CreatedFiles, "TOOLS.md")
	}

	// Create HEARTBEAT.md if it doesn't exist (new in Nexus)
	heartbeatFile := filepath.Join(targetPath, "HEARTBEAT.md")
	if _, err := os.Stat(heartbeatFile); os.IsNotExist(err) {
		if err := os.WriteFile(heartbeatFile, []byte(getDefaultHeartbeatContent()), 0o644); err != nil {
			return nil, fmt.Errorf("create HEARTBEAT.md: %w", err)
		}
		result.CreatedFiles = append(result.CreatedFiles, "HEARTBEAT.md")
	}

	return result, nil
}

// ValidateClawdbotWorkspace checks if a directory looks like a Clawdbot workspace.
func ValidateClawdbotWorkspace(path string) (bool, []string) {
	var found []string
	var missing []string

	for _, filename := range ClawdbotWorkspaceFiles {
		filePath := filepath.Join(path, filename)
		if _, err := os.Stat(filePath); err == nil {
			found = append(found, filename)
		} else {
			missing = append(missing, filename)
		}
	}

	// Consider valid if at least SOUL.md or IDENTITY.md exists
	isValid := false
	for _, f := range found {
		if f == "SOUL.md" || f == "IDENTITY.md" {
			isValid = true
			break
		}
	}

	return isValid, missing
}

// MigrateClawdbotConfig transforms a Clawdbot config to Nexus format.
func MigrateClawdbotConfig(sourceConfig map[string]any) (map[string]any, *ClawdbotMigrationResult, error) {
	result := &ClawdbotMigrationResult{}
	nexusConfig := make(map[string]any)

	// Set version
	nexusConfig["version"] = 1
	result.ConfigMigrations = append(result.ConfigMigrations, "set version to 1")

	// Copy identity section if present
	if identity, ok := getStringMap(sourceConfig, "identity"); ok {
		nexusConfig["identity"] = identity
		result.ConfigMigrations = append(result.ConfigMigrations, "copied identity section")
	}

	// Copy user section if present
	if user, ok := getStringMap(sourceConfig, "user"); ok {
		nexusConfig["user"] = user
		result.ConfigMigrations = append(result.ConfigMigrations, "copied user section")
	}

	// Copy workspace section
	if workspace, ok := getStringMap(sourceConfig, "workspace"); ok {
		nexusConfig["workspace"] = workspace
		result.ConfigMigrations = append(result.ConfigMigrations, "copied workspace section")
	}

	// Copy LLM provider settings
	if llm, ok := getStringMap(sourceConfig, "llm"); ok {
		nexusConfig["llm"] = llm
		result.ConfigMigrations = append(result.ConfigMigrations, "copied llm section")
	}

	// Copy channels section
	if channels, ok := getStringMap(sourceConfig, "channels"); ok {
		nexusConfig["channels"] = channels
		result.ConfigMigrations = append(result.ConfigMigrations, "copied channels section")
	}

	// Migrate plugins -> tools
	if plugins, ok := getStringMap(sourceConfig, "plugins"); ok {
		tools := make(map[string]any)
		for key, val := range plugins {
			if key == "sandbox" || key == "browser" || key == "websearch" {
				tools[key] = val
				result.ConfigMigrations = append(result.ConfigMigrations, fmt.Sprintf("moved plugins.%s -> tools.%s", key, key))
			}
		}
		if len(tools) > 0 {
			nexusConfig["tools"] = tools
		}
	}

	// Copy session config
	if session, ok := getStringMap(sourceConfig, "session"); ok {
		nexusConfig["session"] = session
		result.ConfigMigrations = append(result.ConfigMigrations, "copied session section")
	}

	// Copy memory config if present (may need transformation)
	if memory, ok := getStringMap(sourceConfig, "memory"); ok {
		// In Nexus, memory is under session.memory
		if nexusSession, ok := getStringMap(nexusConfig, "session"); ok {
			nexusSession["memory"] = memory
		} else {
			nexusConfig["session"] = map[string]any{"memory": memory}
		}
		result.ConfigMigrations = append(result.ConfigMigrations, "moved memory -> session.memory")
	}

	// Handle agents array (Clawdbot multi-agent) - warn if present
	if agents, ok := sourceConfig["agents"]; ok {
		if agentList, ok := agents.([]any); ok && len(agentList) > 1 {
			result.ConfigWarnings = append(result.ConfigWarnings,
				fmt.Sprintf("found %d agents - Nexus uses single-agent config; only first agent settings migrated", len(agentList)))
		}
	}

	// Copy observability if present (warn - not fully supported)
	if _, ok := sourceConfig["observability"]; ok {
		result.ConfigWarnings = append(result.ConfigWarnings,
			"observability section not migrated (use Nexus's built-in observability)")
	}

	return nexusConfig, result, nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Get source file info for permissions
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// getDefaultToolsContent returns the default TOOLS.md content from bootstrap.
func getDefaultToolsContent() string {
	files := workspace.DefaultBootstrapFiles()
	for _, file := range files {
		if file.Name == "TOOLS.md" {
			return file.Content
		}
	}
	return "# Tool Notes\n\nAdd tool notes here.\n"
}

// getDefaultHeartbeatContent returns the default HEARTBEAT.md content from bootstrap.
func getDefaultHeartbeatContent() string {
	files := workspace.DefaultBootstrapFiles()
	for _, file := range files {
		if file.Name == "HEARTBEAT.md" {
			return file.Content
		}
	}
	return "# Heartbeat Checklist\n\n- Report only new/changed items.\n"
}

// FormatMigrationResult formats the migration result for display.
func FormatMigrationResult(result *ClawdbotMigrationResult) string {
	var sb strings.Builder

	sb.WriteString("Clawdbot Workspace Migration\n")
	sb.WriteString("============================\n\n")

	sb.WriteString(fmt.Sprintf("Source: %s\n", result.SourceWorkspace))
	sb.WriteString(fmt.Sprintf("Target: %s\n\n", result.TargetWorkspace))

	if len(result.CopiedFiles) > 0 {
		sb.WriteString("Copied files:\n")
		for _, f := range result.CopiedFiles {
			sb.WriteString(fmt.Sprintf("  ✓ %s\n", f))
		}
		sb.WriteString("\n")
	}

	if len(result.CreatedFiles) > 0 {
		sb.WriteString("Created files (new in Nexus):\n")
		for _, f := range result.CreatedFiles {
			sb.WriteString(fmt.Sprintf("  + %s\n", f))
		}
		sb.WriteString("\n")
	}

	if len(result.SkippedFiles) > 0 {
		sb.WriteString("Skipped files:\n")
		for _, f := range result.SkippedFiles {
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
		sb.WriteString("\n")
	}

	if len(result.ConfigMigrations) > 0 {
		sb.WriteString("Config migrations:\n")
		for _, m := range result.ConfigMigrations {
			sb.WriteString(fmt.Sprintf("  → %s\n", m))
		}
		sb.WriteString("\n")
	}

	if len(result.ConfigWarnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, w := range result.ConfigWarnings {
			sb.WriteString(fmt.Sprintf("  ⚠ %s\n", w))
		}
	}

	return sb.String()
}
