package security

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultStateDir returns the default nexus state directory.
func DefaultStateDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".nexus")
}

// DefaultConfigPath returns the default nexus configuration file path.
func DefaultConfigPath() string {
	return filepath.Join(DefaultStateDir(), "nexus.yaml")
}

// FixAction represents an action taken to fix a security issue.
type FixAction struct {
	// Type is the kind of fix (chmod, config, etc.)
	Type string `json:"type"`

	// Path is the file or directory affected.
	Path string `json:"path"`

	// Description describes what was done.
	Description string `json:"description"`

	// Success indicates if the fix was applied.
	Success bool `json:"success"`

	// Skipped indicates why the fix was skipped (if applicable).
	Skipped string `json:"skipped,omitempty"`

	// Error contains any error message.
	Error string `json:"error,omitempty"`
}

// FixResult contains the results of a security fix operation.
type FixResult struct {
	// Actions is the list of all fix actions attempted.
	Actions []FixAction `json:"actions"`

	// FixedCount is the number of successful fixes.
	FixedCount int `json:"fixed_count"`

	// SkippedCount is the number of skipped fixes.
	SkippedCount int `json:"skipped_count"`

	// ErrorCount is the number of failed fixes.
	ErrorCount int `json:"error_count"`
}

// FixOptions configures the security fix operation.
type FixOptions struct {
	// StateDir is the directory containing nexus state files.
	StateDir string

	// ConfigPath is the path to the configuration file.
	ConfigPath string

	// DryRun if true, only reports what would be done without making changes.
	DryRun bool
}

// Fix attempts to automatically fix common security issues.
// It returns a result indicating what was fixed or what errors occurred.
func Fix(opts FixOptions) *FixResult {
	result := &FixResult{
		Actions: make([]FixAction, 0),
	}

	// Fix state directory permissions
	if opts.StateDir != "" {
		result.Actions = append(result.Actions, fixDirectoryPermissions(opts.StateDir, 0700, opts.DryRun))
	}

	// Fix config file permissions
	if opts.ConfigPath != "" {
		result.Actions = append(result.Actions, fixFilePermissions(opts.ConfigPath, 0600, opts.DryRun))
	}

	// Fix common sensitive files within state directory
	if opts.StateDir != "" {
		sensitiveFiles := []string{
			"config.yaml",
			"config.yml",
			"secrets.yaml",
			"credentials.json",
			"auth.json",
		}

		for _, name := range sensitiveFiles {
			path := filepath.Join(opts.StateDir, name)
			if _, err := os.Stat(path); err == nil {
				result.Actions = append(result.Actions, fixFilePermissions(path, 0600, opts.DryRun))
			}
		}

		// Fix sensitive subdirectories
		sensitiveDirs := []string{
			"credentials",
			"oauth",
			"tokens",
			"keys",
			"sessions",
		}

		for _, name := range sensitiveDirs {
			path := filepath.Join(opts.StateDir, name)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				result.Actions = append(result.Actions, fixDirectoryPermissions(path, 0700, opts.DryRun))
				// Also fix files within
				entries, _ := os.ReadDir(path)
				for _, entry := range entries {
					if !entry.IsDir() {
						filePath := filepath.Join(path, entry.Name())
						result.Actions = append(result.Actions, fixFilePermissions(filePath, 0600, opts.DryRun))
					}
				}
			}
		}
	}

	// Count results
	for _, action := range result.Actions {
		if action.Success {
			result.FixedCount++
		} else if action.Skipped != "" {
			result.SkippedCount++
		} else if action.Error != "" {
			result.ErrorCount++
		}
	}

	return result
}

// fixFilePermissions attempts to set secure permissions on a file.
func fixFilePermissions(path string, mode os.FileMode, dryRun bool) FixAction {
	action := FixAction{
		Type:        "chmod",
		Path:        path,
		Description: fmt.Sprintf("Set file permissions to %o", mode),
	}

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			action.Skipped = "file does not exist"
			return action
		}
		action.Error = fmt.Sprintf("failed to stat: %v", err)
		return action
	}

	// Skip symlinks
	if info.Mode()&fs.ModeSymlink != 0 {
		action.Skipped = "symlink (not modified for safety)"
		return action
	}

	// Skip if not a regular file
	if !info.Mode().IsRegular() {
		action.Skipped = "not a regular file"
		return action
	}

	// Check current permissions
	currentMode := info.Mode().Perm()
	if currentMode == mode {
		action.Skipped = "already has correct permissions"
		return action
	}

	// Don't actually change in dry run
	if dryRun {
		action.Description = fmt.Sprintf("Would change from %o to %o", currentMode, mode)
		action.Success = true
		return action
	}

	// Apply the fix
	if err := os.Chmod(path, mode); err != nil {
		action.Error = fmt.Sprintf("chmod failed: %v", err)
		return action
	}

	action.Description = fmt.Sprintf("Changed from %o to %o", currentMode, mode)
	action.Success = true
	return action
}

// fixDirectoryPermissions attempts to set secure permissions on a directory.
func fixDirectoryPermissions(path string, mode os.FileMode, dryRun bool) FixAction {
	action := FixAction{
		Type:        "chmod",
		Path:        path,
		Description: fmt.Sprintf("Set directory permissions to %o", mode),
	}

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			action.Skipped = "directory does not exist"
			return action
		}
		action.Error = fmt.Sprintf("failed to stat: %v", err)
		return action
	}

	// Skip symlinks
	if info.Mode()&fs.ModeSymlink != 0 {
		action.Skipped = "symlink (not modified for safety)"
		return action
	}

	// Skip if not a directory
	if !info.IsDir() {
		action.Skipped = "not a directory"
		return action
	}

	// Check current permissions
	currentMode := info.Mode().Perm()
	if currentMode == mode {
		action.Skipped = "already has correct permissions"
		return action
	}

	// Don't actually change in dry run
	if dryRun {
		action.Description = fmt.Sprintf("Would change from %o to %o", currentMode, mode)
		action.Success = true
		return action
	}

	// Apply the fix
	if err := os.Chmod(path, mode); err != nil {
		action.Error = fmt.Sprintf("chmod failed: %v", err)
		return action
	}

	action.Description = fmt.Sprintf("Changed from %o to %o", currentMode, mode)
	action.Success = true
	return action
}

// RunDefaultFix runs security fixes with default options.
func RunDefaultFix() *FixResult {
	return Fix(FixOptions{
		StateDir:   DefaultStateDir(),
		ConfigPath: DefaultConfigPath(),
		DryRun:     false,
	})
}

// RunDefaultFixDryRun runs security fixes in dry-run mode with default options.
func RunDefaultFixDryRun() *FixResult {
	return Fix(FixOptions{
		StateDir:   DefaultStateDir(),
		ConfigPath: DefaultConfigPath(),
		DryRun:     true,
	})
}

// DefaultStateDir returns the default nexus state directory.
func DefaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".nexus"
	}
	return filepath.Join(home, ".nexus")
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	return "nexus.yaml"
}
