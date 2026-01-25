// Package security provides security audit capabilities for runtime configuration
// and filesystem permission validation.
package security

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
)

// AuditSeverity represents the severity level of a security finding.
type AuditSeverity string

const (
	SeverityInfo     AuditSeverity = "info"
	SeverityWarn     AuditSeverity = "warn"
	SeverityCritical AuditSeverity = "critical"
)

// Severity is an alias for AuditSeverity for backward compatibility.
type Severity = AuditSeverity

// Additional severity levels for backward compatibility.
const (
	SeverityHigh   AuditSeverity = "high"
	SeverityMedium AuditSeverity = "medium"
	SeverityLow    AuditSeverity = "low"
)

// AuditFinding represents a single security audit finding.
type AuditFinding struct {
	CheckID     string        `json:"check_id"`
	Severity    AuditSeverity `json:"severity"`
	Title       string        `json:"title"`
	Detail      string        `json:"detail"`
	Remediation string        `json:"remediation,omitempty"`
}

// Finding is an alias for AuditFinding for backward compatibility.
type Finding = AuditFinding

// AuditSummary contains counts of findings by severity.
type AuditSummary struct {
	Critical int `json:"critical"`
	Warn     int `json:"warn"`
	Info     int `json:"info"`
}

// AuditReport contains all findings from a security audit.
type AuditReport struct {
	Timestamp time.Time      `json:"timestamp"`
	Summary   AuditSummary   `json:"summary"`
	Findings  []AuditFinding `json:"findings"`
}

// AuditResult is an alias for AuditReport for backward compatibility.
type AuditResult = AuditReport

// HasCritical returns true if any findings are critical severity.
func (r *AuditReport) HasCritical() bool {
	return r.Summary.Critical > 0
}

// HasHighOrAbove returns true if any findings are high or critical severity.
func (r *AuditReport) HasHighOrAbove() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			return true
		}
	}
	return false
}

// CountBySeverity returns the number of findings for each severity level.
func (r *AuditReport) CountBySeverity() map[AuditSeverity]int {
	counts := make(map[AuditSeverity]int)
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	return counts
}

// AuditOptions configures which checks to run.
type AuditOptions struct {
	// StateDir is the directory where state files are stored.
	StateDir string

	// ConfigPath is the path to the configuration file.
	ConfigPath string

	// Config is the loaded configuration (optional, will load from ConfigPath if nil).
	Config *config.Config

	// IncludeFilesystem enables filesystem permission checks.
	IncludeFilesystem bool

	// IncludeGateway enables gateway/server configuration checks.
	IncludeGateway bool

	// IncludeConfig enables configuration content checks.
	IncludeConfig bool

	// CheckSymlinks enables symlink detection.
	CheckSymlinks bool

	// AllowGroupReadable allows group-readable permissions on sensitive files.
	AllowGroupReadable bool
}

// AuditConfig is an alias for AuditOptions for backward compatibility.
type AuditConfig = AuditOptions

// RunAudit performs a comprehensive security audit based on the provided options.
func RunAudit(opts AuditOptions) (*AuditReport, error) {
	report := &AuditReport{
		Timestamp: time.Now(),
		Findings:  make([]AuditFinding, 0),
	}

	// Filesystem checks
	if opts.IncludeFilesystem {
		fsFindings, err := auditFilesystem(opts)
		if err != nil {
			return nil, fmt.Errorf("filesystem audit failed: %w", err)
		}
		report.Findings = append(report.Findings, fsFindings...)
	}

	// Gateway checks
	if opts.IncludeGateway {
		cfg := opts.Config
		if cfg == nil && opts.ConfigPath != "" {
			var err error
			cfg, err = config.Load(opts.ConfigPath)
			if err != nil {
				// Config load errors are warnings, not fatal
				report.Findings = append(report.Findings, AuditFinding{
					CheckID:  "config.load_error",
					Severity: SeverityWarn,
					Title:    "Failed to load configuration",
					Detail:   fmt.Sprintf("Could not load config from %s: %v", opts.ConfigPath, err),
				})
			}
		}
		if cfg != nil {
			gatewayFindings := AuditGatewayConfig(cfg)
			report.Findings = append(report.Findings, gatewayFindings...)
		}
	}

	// Config content checks
	if opts.IncludeConfig {
		cfg := opts.Config
		if cfg == nil && opts.ConfigPath != "" {
			var err error
			cfg, err = config.Load(opts.ConfigPath)
			if err == nil {
				configFindings := auditConfigContent(cfg)
				report.Findings = append(report.Findings, configFindings...)
			}
		} else if cfg != nil {
			configFindings := auditConfigContent(cfg)
			report.Findings = append(report.Findings, configFindings...)
		}
	}

	// Compute summary
	report.Summary = computeSummary(report.Findings)

	return report, nil
}

// computeSummary calculates the summary counts from findings.
func computeSummary(findings []AuditFinding) AuditSummary {
	summary := AuditSummary{}
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical, SeverityHigh:
			summary.Critical++
		case SeverityWarn, SeverityMedium:
			summary.Warn++
		case SeverityInfo, SeverityLow:
			summary.Info++
		}
	}
	return summary
}

// Auditor performs security audits on the system.
type Auditor struct {
	config AuditOptions
}

// NewAuditor creates a new security auditor.
func NewAuditor(config AuditOptions) *Auditor {
	return &Auditor{config: config}
}

// Run performs a full security audit and returns the results.
func (a *Auditor) Run() (*AuditReport, error) {
	opts := a.config
	opts.IncludeFilesystem = true
	return RunAudit(opts)
}

// Permission bit constants for clarity.
const (
	worldReadable = 0004
	worldWritable = 0002
	groupReadable = 0040
	groupWritable = 0020
)

// Permission check helpers

func isWorldWritable(mode fs.FileMode) bool {
	return mode&worldWritable != 0
}

func isGroupWritable(mode fs.FileMode) bool {
	return mode&groupWritable != 0
}

func isWorldReadable(mode fs.FileMode) bool {
	return mode&worldReadable != 0
}

func isGroupReadable(mode fs.FileMode) bool {
	return mode&groupReadable != 0
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
func CheckPath(path string) ([]AuditFinding, error) {
	opts := AuditOptions{
		CheckSymlinks: true,
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return checkDirectory(path, "directory", opts)
	}
	return checkConfigFile(path, opts)
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
