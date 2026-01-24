// Package security provides security audit capabilities for runtime configuration
// and filesystem permission validation.
package security

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Severity represents the severity level of a security finding.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityWarn     Severity = "warn"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Finding represents a single security audit finding.
type Finding struct {
	CheckID     string   `json:"check_id"`
	Severity    Severity `json:"severity"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Remediation string   `json:"remediation,omitempty"`
}

// AuditResult contains all findings from a security audit.
type AuditResult struct {
	Findings []Finding `json:"findings"`
}

// HasCritical returns true if any findings are critical severity.
func (r *AuditResult) HasCritical() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// HasHighOrAbove returns true if any findings are high or critical severity.
func (r *AuditResult) HasHighOrAbove() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			return true
		}
	}
	return false
}

// CountBySeverity returns the number of findings for each severity level.
func (r *AuditResult) CountBySeverity() map[Severity]int {
	counts := make(map[Severity]int)
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	return counts
}

// AuditConfig holds configuration for the security audit.
type AuditConfig struct {
	// StateDir is the directory where state files are stored.
	StateDir string
	// ConfigFile is the path to the configuration file.
	ConfigFile string
	// CheckSymlinks enables symlink detection.
	CheckSymlinks bool
	// AllowGroupReadable allows group-readable permissions on sensitive files.
	AllowGroupReadable bool
}

// Auditor performs security audits on the system.
type Auditor struct {
	config AuditConfig
}

// NewAuditor creates a new security auditor.
func NewAuditor(config AuditConfig) *Auditor {
	return &Auditor{config: config}
}

// Run performs a full security audit and returns the results.
func (a *Auditor) Run() (*AuditResult, error) {
	result := &AuditResult{}

	// Collect filesystem findings
	fsFindings, err := a.collectFilesystemFindings()
	if err != nil {
		return nil, fmt.Errorf("filesystem audit failed: %w", err)
	}
	result.Findings = append(result.Findings, fsFindings...)

	return result, nil
}

// collectFilesystemFindings checks filesystem permissions.
func (a *Auditor) collectFilesystemFindings() ([]Finding, error) {
	var findings []Finding

	// Check state directory
	if a.config.StateDir != "" {
		dirFindings, err := a.checkDirectory(a.config.StateDir, "state directory")
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		findings = append(findings, dirFindings...)
	}

	// Check config file
	if a.config.ConfigFile != "" {
		fileFindings, err := a.checkConfigFile(a.config.ConfigFile)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		findings = append(findings, fileFindings...)
	}

	return findings, nil
}

// checkDirectory audits permissions on a directory.
func (a *Auditor) checkDirectory(path, description string) ([]Finding, error) {
	var findings []Finding

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Directory doesn't exist, nothing to check
		}
		return nil, err
	}

	// Check if it's a symlink
	if a.config.CheckSymlinks && info.Mode()&os.ModeSymlink != 0 {
		findings = append(findings, Finding{
			CheckID:     "FS-001",
			Severity:    SeverityMedium,
			Title:       fmt.Sprintf("%s is a symlink", description),
			Detail:      fmt.Sprintf("The %s at %s is a symbolic link, which could be exploited for symlink attacks.", description, path),
			Remediation: "Use a real directory instead of a symlink for sensitive data storage.",
		})
	}

	// Check permissions
	mode := info.Mode().Perm()

	if isWorldWritable(mode) {
		findings = append(findings, Finding{
			CheckID:     "FS-002",
			Severity:    SeverityCritical,
			Title:       fmt.Sprintf("%s is world-writable", description),
			Detail:      fmt.Sprintf("The %s at %s has permissions %o, allowing any user to write to it.", description, path, mode),
			Remediation: fmt.Sprintf("Run: chmod o-w %s", path),
		})
	}

	if isGroupWritable(mode) {
		findings = append(findings, Finding{
			CheckID:     "FS-003",
			Severity:    SeverityHigh,
			Title:       fmt.Sprintf("%s is group-writable", description),
			Detail:      fmt.Sprintf("The %s at %s has permissions %o, allowing group members to write to it.", description, path, mode),
			Remediation: fmt.Sprintf("Run: chmod g-w %s", path),
		})
	}

	if isWorldReadable(mode) {
		findings = append(findings, Finding{
			CheckID:     "FS-004",
			Severity:    SeverityMedium,
			Title:       fmt.Sprintf("%s is world-readable", description),
			Detail:      fmt.Sprintf("The %s at %s has permissions %o, allowing any user to read its contents.", description, path, mode),
			Remediation: fmt.Sprintf("Run: chmod o-r %s", path),
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

			fileMode := fileInfo.Mode().Perm()

			// Check for sensitive files with bad permissions
			if isSensitiveFile(filePath) {
				if isWorldReadable(fileMode) {
					findings = append(findings, Finding{
						CheckID:     "FS-005",
						Severity:    SeverityHigh,
						Title:       "Sensitive file is world-readable",
						Detail:      fmt.Sprintf("The file %s has permissions %o, exposing sensitive data.", filePath, fileMode),
						Remediation: fmt.Sprintf("Run: chmod 600 %s", filePath),
					})
				}

				if !a.config.AllowGroupReadable && isGroupReadable(fileMode) {
					findings = append(findings, Finding{
						CheckID:     "FS-006",
						Severity:    SeverityMedium,
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
func (a *Auditor) checkConfigFile(path string) ([]Finding, error) {
	var findings []Finding

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Check if it's a symlink
	if a.config.CheckSymlinks && info.Mode()&os.ModeSymlink != 0 {
		findings = append(findings, Finding{
			CheckID:     "FS-010",
			Severity:    SeverityMedium,
			Title:       "Config file is a symlink",
			Detail:      fmt.Sprintf("The configuration file at %s is a symbolic link.", path),
			Remediation: "Use a real file instead of a symlink for the configuration.",
		})
	}

	mode := info.Mode().Perm()

	if isWorldWritable(mode) {
		findings = append(findings, Finding{
			CheckID:     "FS-011",
			Severity:    SeverityCritical,
			Title:       "Config file is world-writable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o, allowing any user to modify it.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	if isGroupWritable(mode) {
		findings = append(findings, Finding{
			CheckID:     "FS-012",
			Severity:    SeverityHigh,
			Title:       "Config file is group-writable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o, allowing group members to modify it.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	if isWorldReadable(mode) {
		findings = append(findings, Finding{
			CheckID:     "FS-013",
			Severity:    SeverityHigh,
			Title:       "Config file is world-readable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o and may contain secrets.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	if !a.config.AllowGroupReadable && isGroupReadable(mode) {
		findings = append(findings, Finding{
			CheckID:     "FS-014",
			Severity:    SeverityMedium,
			Title:       "Config file is group-readable",
			Detail:      fmt.Sprintf("The configuration file at %s has permissions %o.", path, mode),
			Remediation: fmt.Sprintf("Run: chmod 600 %s", path),
		})
	}

	return findings, nil
}

// Permission check helpers

func isWorldWritable(mode fs.FileMode) bool {
	return mode&0002 != 0
}

func isGroupWritable(mode fs.FileMode) bool {
	return mode&0020 != 0
}

func isWorldReadable(mode fs.FileMode) bool {
	return mode&0004 != 0
}

func isGroupReadable(mode fs.FileMode) bool {
	return mode&0040 != 0
}

// isSensitiveFile checks if a file path indicates sensitive content.
func isSensitiveFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))

	sensitivePatterns := []string{
		"key",
		"secret",
		"token",
		"credential",
		"password",
		"private",
		".pem",
		".key",
		".p12",
		".pfx",
		"id_rsa",
		"id_ed25519",
		"id_ecdsa",
		"id_dsa",
	}

	for _, pattern := range sensitivePatterns {
		if strings.Contains(base, pattern) {
			return true
		}
	}

	// Check for environment files
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}

	return false
}

// CheckPath performs a quick permission check on a single path.
// Returns findings without running a full audit.
func CheckPath(path string) ([]Finding, error) {
	auditor := NewAuditor(AuditConfig{
		CheckSymlinks: true,
	})

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return auditor.checkDirectory(path, "directory")
	}
	return auditor.checkConfigFile(path)
}

// ValidatePermissions checks if a path has secure permissions.
// Returns an error if permissions are insecure.
func ValidatePermissions(path string, maxMode fs.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	mode := info.Mode().Perm()
	if mode&^maxMode != 0 {
		return fmt.Errorf("insecure permissions %o on %s (maximum allowed: %o)", mode, path, maxMode)
	}

	return nil
}

// SecureFileMode is the recommended permission mode for sensitive files.
const SecureFileMode fs.FileMode = 0600

// SecureDirMode is the recommended permission mode for sensitive directories.
const SecureDirMode fs.FileMode = 0700
