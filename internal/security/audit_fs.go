package security

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// auditFilesystem performs filesystem permission and symlink checks.
func auditFilesystem(opts AuditOptions) ([]AuditFinding, error) {
	var findings []AuditFinding

	// Check state directory
	if opts.StateDir != "" {
		dirFindings, err := checkDirectory(opts.StateDir, "state directory", opts)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		findings = append(findings, dirFindings...)
	}

	// Check config file
	if opts.ConfigPath != "" {
		fileFindings, err := checkConfigFile(opts.ConfigPath, opts)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		findings = append(findings, fileFindings...)
	}

	return findings, nil
}

// checkDirectory audits permissions on a directory.
func checkDirectory(path, description string, opts AuditOptions) ([]AuditFinding, error) {
	var findings []AuditFinding

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Directory doesn't exist, nothing to check
		}
		return nil, err
	}

	// Check if it's a symlink
	if opts.CheckSymlinks && info.Mode()&os.ModeSymlink != 0 {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.symlink_state_dir",
			Severity:    SeverityWarn,
			Title:       fmt.Sprintf("%s is a symlink", description),
			Detail:      fmt.Sprintf("The %s at %s is a symbolic link. Symlinks can cross trust boundaries and be exploited for symlink attacks.", description, path),
			Remediation: "Use a real directory instead of a symlink for sensitive data storage.",
		})
	}

	// Check permissions
	mode := info.Mode().Perm()

	// World-writable is critical
	if isWorldWritable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.state_dir_world_writable",
			Severity:    SeverityCritical,
			Title:       fmt.Sprintf("%s is world-writable", description),
			Detail:      fmt.Sprintf("The %s at %s has permissions %o, allowing any user to write to it.", description, path, mode),
			Remediation: fmt.Sprintf("Run: chmod o-w %s", path),
		})
	}

	// Group-writable is a warning
	if isGroupWritable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.state_dir_group_writable",
			Severity:    SeverityWarn,
			Title:       fmt.Sprintf("%s is group-writable", description),
			Detail:      fmt.Sprintf("The %s at %s has permissions %o, allowing group members to write to it.", description, path, mode),
			Remediation: fmt.Sprintf("Run: chmod g-w %s", path),
		})
	}

	// World-readable is a warning
	if isWorldReadable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.state_dir_world_readable",
			Severity:    SeverityWarn,
			Title:       fmt.Sprintf("%s is world-readable", description),
			Detail:      fmt.Sprintf("The %s at %s has permissions %o, allowing any user to read its contents.", description, path, mode),
			Remediation: fmt.Sprintf("Run: chmod o-r %s", path),
		})
	}

	// Group-readable is info level (unless not allowed)
	if !opts.AllowGroupReadable && isGroupReadable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.state_dir_group_readable",
			Severity:    SeverityInfo,
			Title:       fmt.Sprintf("%s is group-readable", description),
			Detail:      fmt.Sprintf("The %s at %s has permissions %o, allowing group members to read its contents.", description, path, mode),
			Remediation: fmt.Sprintf("Run: chmod 700 %s", path),
		})
	}

	// Check files within the directory
	if info.IsDir() {
		err = filepath.WalkDir(path, func(filePath string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip files we can't access
			}
			if filePath == path {
				return nil // Skip the root directory itself
			}

			fileInfo, err := d.Info()
			if err != nil {
				return nil
			}

			// Check for symlinks in directory
			if opts.CheckSymlinks && fileInfo.Mode()&os.ModeSymlink != 0 {
				findings = append(findings, AuditFinding{
					CheckID:     "fs.symlink_in_state",
					Severity:    SeverityInfo,
					Title:       "Symlink found in state directory",
					Detail:      fmt.Sprintf("The path %s is a symbolic link. Symlinks can cross trust boundaries.", filePath),
					Remediation: "Review whether this symlink is necessary and trusted.",
				})
			}

			fileMode := fileInfo.Mode().Perm()

			// Check for sensitive files with bad permissions
			if isSensitiveFile(filePath) {
				if isWorldReadable(fileMode) {
					findings = append(findings, AuditFinding{
						CheckID:     "fs.sensitive_file_world_readable",
						Severity:    SeverityCritical,
						Title:       "Sensitive file is world-readable",
						Detail:      fmt.Sprintf("The file %s has permissions %o, exposing sensitive data to all users.", filePath, fileMode),
						Remediation: fmt.Sprintf("Run: chmod 600 %s", filePath),
					})
				}

				if isWorldWritable(fileMode) {
					findings = append(findings, AuditFinding{
						CheckID:     "fs.sensitive_file_world_writable",
						Severity:    SeverityCritical,
						Title:       "Sensitive file is world-writable",
						Detail:      fmt.Sprintf("The file %s has permissions %o, allowing any user to modify sensitive data.", filePath, fileMode),
						Remediation: fmt.Sprintf("Run: chmod 600 %s", filePath),
					})
				}

				if !opts.AllowGroupReadable && isGroupReadable(fileMode) {
					findings = append(findings, AuditFinding{
						CheckID:     "fs.sensitive_file_group_readable",
						Severity:    SeverityWarn,
						Title:       "Sensitive file is group-readable",
						Detail:      fmt.Sprintf("The file %s has permissions %o, allowing group access.", filePath, fileMode),
						Remediation: fmt.Sprintf("Run: chmod 600 %s", filePath),
					})
				}
			}

			return nil
		})
		if err != nil {
			return findings, err
		}
	}

	return findings, nil
}

// checkConfigFile audits permissions on the config file.
func checkConfigFile(path string, opts AuditOptions) ([]AuditFinding, error) {
	var findings []AuditFinding

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Check if it's a symlink
	if opts.CheckSymlinks && info.Mode()&os.ModeSymlink != 0 {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.config_symlink",
			Severity:    SeverityWarn,
			Title:       "Config file is a symlink",
			Detail:      fmt.Sprintf("The configuration file at %s is a symbolic link. This could be exploited to read or modify unintended files.", path),
			Remediation: "Use a real file instead of a symlink for the configuration.",
		})
	}

	mode := info.Mode().Perm()

	// World-writable config is critical
	if isWorldWritable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.config_world_writable",
			Severity:    SeverityCritical,
			Title:       "Config file is world-writable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o, allowing any user to modify it. This could lead to arbitrary code execution.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	// Group-writable config is a warning
	if isGroupWritable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.config_group_writable",
			Severity:    SeverityWarn,
			Title:       "Config file is group-writable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o, allowing group members to modify it.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	// World-readable config is critical (may contain secrets)
	if isWorldReadable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.config_world_readable",
			Severity:    SeverityCritical,
			Title:       "Config file is world-readable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o. Config files often contain API keys, tokens, and other secrets.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	// Group-readable config is a warning
	if !opts.AllowGroupReadable && isGroupReadable(mode) {
		findings = append(findings, AuditFinding{
			CheckID:     "fs.config_group_readable",
			Severity:    SeverityWarn,
			Title:       "Config file is group-readable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	return findings, nil
}

// Backward compatibility check IDs mapping
const (
	// Legacy check IDs - keep for backward compatibility
	FS001 = "FS-001" // symlink state dir
	FS002 = "FS-002" // world-writable dir
	FS003 = "FS-003" // group-writable dir
	FS004 = "FS-004" // world-readable dir
	FS005 = "FS-005" // sensitive file world-readable
	FS006 = "FS-006" // sensitive file group-readable
	FS010 = "FS-010" // config symlink
	FS011 = "FS-011" // config world-writable
	FS012 = "FS-012" // config group-writable
	FS013 = "FS-013" // config world-readable
	FS014 = "FS-014" // config group-readable
)
