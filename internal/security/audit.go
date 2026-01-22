// Package security provides security auditing and hardening features.
package security

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Severity indicates the severity of a security finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// Finding represents a single security audit finding.
type Finding struct {
	CheckID     string   `json:"check_id"`
	Severity    Severity `json:"severity"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Remediation string   `json:"remediation,omitempty"`
}

// Summary provides counts of findings by severity.
type Summary struct {
	Critical int `json:"critical"`
	Warn     int `json:"warn"`
	Info     int `json:"info"`
}

// Report contains the complete security audit results.
type Report struct {
	Timestamp int64      `json:"ts"`
	Summary   Summary    `json:"summary"`
	Findings  []Finding  `json:"findings"`
	Deep      *DeepAudit `json:"deep,omitempty"`
}

// DeepAudit contains results from deep connectivity tests.
type DeepAudit struct {
	Gateway *GatewayProbe `json:"gateway,omitempty"`
}

// GatewayProbe contains gateway connectivity test results.
type GatewayProbe struct {
	Attempted bool   `json:"attempted"`
	URL       string `json:"url,omitempty"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

// AuditOptions configures the security audit.
type AuditOptions struct {
	// ConfigPath is the path to the configuration file.
	ConfigPath string
	// StateDir is the path to the state directory.
	StateDir string
	// IncludeFilesystem enables filesystem permission checks.
	IncludeFilesystem bool
	// IncludeGateway enables gateway security checks.
	IncludeGateway bool
	// IncludeChannels enables channel-specific security checks.
	IncludeChannels bool
	// Deep enables deep connectivity probes.
	Deep bool
	// DeepTimeout is the timeout for deep probes.
	DeepTimeout time.Duration
}

// DefaultAuditOptions returns sensible defaults for security auditing.
func DefaultAuditOptions() AuditOptions {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	return AuditOptions{
		ConfigPath:        filepath.Join(homeDir, ".nexus", "nexus.yaml"),
		StateDir:          filepath.Join(homeDir, ".nexus"),
		IncludeFilesystem: true,
		IncludeGateway:    true,
		IncludeChannels:   true,
		Deep:              false,
		DeepTimeout:       10 * time.Second,
	}
}

// Auditor performs security audits.
type Auditor struct {
	opts AuditOptions
}

// NewAuditor creates a new security auditor with the given options.
func NewAuditor(opts AuditOptions) *Auditor {
	return &Auditor{opts: opts}
}

// Audit performs a security audit and returns a report.
func (a *Auditor) Audit(ctx context.Context) (*Report, error) {
	var findings []Finding

	if a.opts.IncludeFilesystem {
		fsFindings := a.auditFilesystem()
		findings = append(findings, fsFindings...)
	}

	if a.opts.IncludeGateway {
		gwFindings := a.auditGatewayConfig()
		findings = append(findings, gwFindings...)
	}

	report := &Report{
		Timestamp: time.Now().Unix(),
		Summary:   countBySeverity(findings),
		Findings:  findings,
	}

	return report, nil
}

// auditFilesystem checks filesystem permissions for security issues.
func (a *Auditor) auditFilesystem() []Finding {
	var findings []Finding

	// Check state directory
	if info, err := os.Lstat(a.opts.StateDir); err == nil {
		findings = append(findings, a.checkDirPermissions(a.opts.StateDir, info, "state_dir")...)
	}

	// Check config file
	if info, err := os.Lstat(a.opts.ConfigPath); err == nil {
		findings = append(findings, a.checkFilePermissions(a.opts.ConfigPath, info, "config")...)
	}

	// Check credentials directory
	credsDir := filepath.Join(a.opts.StateDir, "credentials")
	if info, err := os.Lstat(credsDir); err == nil {
		findings = append(findings, a.checkDirPermissions(credsDir, info, "credentials_dir")...)
	}

	// Check for sensitive files with loose permissions
	sensitivePatterns := []string{"*.key", "*.pem", "*.token", "credentials.json"}
	for _, pattern := range sensitivePatterns {
		matches, err := filepath.Glob(filepath.Join(a.opts.StateDir, pattern))
		if err != nil {
			continue // Invalid pattern, skip
		}
		for _, match := range matches {
			if info, err := os.Lstat(match); err == nil {
				findings = append(findings, a.checkFilePermissions(match, info, "sensitive_file")...)
			}
		}
	}

	return findings
}

// titleCase converts a prefix like "state_dir" to "State Dir".
func titleCase(s string) string {
	caser := cases.Title(language.English)
	return caser.String(strings.ReplaceAll(s, "_", " "))
}

// checkDirPermissions checks directory permissions for security issues.
func (a *Auditor) checkDirPermissions(path string, info fs.FileInfo, prefix string) []Finding {
	var findings []Finding
	mode := info.Mode()

	// Check if symlink
	if mode&os.ModeSymlink != 0 {
		findings = append(findings, Finding{
			CheckID:  fmt.Sprintf("fs.%s.symlink", prefix),
			Severity: SeverityWarn,
			Title:    fmt.Sprintf("%s is a symlink", titleCase(prefix)),
			Detail:   fmt.Sprintf("%s is a symlink; treat this as an extra trust boundary.", path),
		})
	}

	perm := mode.Perm()

	// World-writable
	if perm&0002 != 0 {
		findings = append(findings, Finding{
			CheckID:     fmt.Sprintf("fs.%s.perms_world_writable", prefix),
			Severity:    SeverityCritical,
			Title:       fmt.Sprintf("%s is world-writable", titleCase(prefix)),
			Detail:      fmt.Sprintf("%s mode=%04o; other users can write to your data.", path, perm),
			Remediation: fmt.Sprintf("chmod 700 %s", path),
		})
	} else if perm&0020 != 0 {
		// Group-writable
		findings = append(findings, Finding{
			CheckID:     fmt.Sprintf("fs.%s.perms_group_writable", prefix),
			Severity:    SeverityWarn,
			Title:       fmt.Sprintf("%s is group-writable", titleCase(prefix)),
			Detail:      fmt.Sprintf("%s mode=%04o; group users can write to your data.", path, perm),
			Remediation: fmt.Sprintf("chmod 700 %s", path),
		})
	} else if perm&0044 != 0 {
		// Readable by others
		findings = append(findings, Finding{
			CheckID:     fmt.Sprintf("fs.%s.perms_readable", prefix),
			Severity:    SeverityWarn,
			Title:       fmt.Sprintf("%s is readable by others", titleCase(prefix)),
			Detail:      fmt.Sprintf("%s mode=%04o; consider restricting to 700.", path, perm),
			Remediation: fmt.Sprintf("chmod 700 %s", path),
		})
	}

	return findings
}

// checkFilePermissions checks file permissions for security issues.
func (a *Auditor) checkFilePermissions(path string, info fs.FileInfo, prefix string) []Finding {
	var findings []Finding
	mode := info.Mode()

	// Check if symlink
	if mode&os.ModeSymlink != 0 {
		findings = append(findings, Finding{
			CheckID:  fmt.Sprintf("fs.%s.symlink", prefix),
			Severity: SeverityWarn,
			Title:    fmt.Sprintf("%s is a symlink", titleCase(prefix)),
			Detail:   fmt.Sprintf("%s is a symlink; make sure you trust its target.", path),
		})
	}

	perm := mode.Perm()

	// World or group writable
	if perm&0022 != 0 {
		findings = append(findings, Finding{
			CheckID:     fmt.Sprintf("fs.%s.perms_writable", prefix),
			Severity:    SeverityCritical,
			Title:       fmt.Sprintf("%s is writable by others", titleCase(prefix)),
			Detail:      fmt.Sprintf("%s mode=%04o; another user could modify sensitive configuration.", path, perm),
			Remediation: fmt.Sprintf("chmod 600 %s", path),
		})
	} else if perm&0004 != 0 {
		// World-readable
		findings = append(findings, Finding{
			CheckID:     fmt.Sprintf("fs.%s.perms_world_readable", prefix),
			Severity:    SeverityCritical,
			Title:       fmt.Sprintf("%s is world-readable", titleCase(prefix)),
			Detail:      fmt.Sprintf("%s mode=%04o; file can contain tokens and secrets.", path, perm),
			Remediation: fmt.Sprintf("chmod 600 %s", path),
		})
	} else if perm&0040 != 0 {
		// Group-readable
		findings = append(findings, Finding{
			CheckID:     fmt.Sprintf("fs.%s.perms_group_readable", prefix),
			Severity:    SeverityWarn,
			Title:       fmt.Sprintf("%s is group-readable", titleCase(prefix)),
			Detail:      fmt.Sprintf("%s mode=%04o; file can contain tokens and secrets.", path, perm),
			Remediation: fmt.Sprintf("chmod 600 %s", path),
		})
	}

	return findings
}

// auditGatewayConfig checks gateway configuration for security issues.
func (a *Auditor) auditGatewayConfig() []Finding {
	var findings []Finding

	// Try to load the config file
	cfg, err := config.Load(a.opts.ConfigPath)
	if err != nil {
		// If config doesn't exist or can't be parsed, skip gateway checks
		return findings
	}

	// Run gateway configuration audit
	findings = append(findings, AuditGatewayConfig(cfg)...)

	return findings
}

// countBySeverity counts findings by severity level.
func countBySeverity(findings []Finding) Summary {
	var summary Summary
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			summary.Critical++
		case SeverityWarn:
			summary.Warn++
		case SeverityInfo:
			summary.Info++
		}
	}
	return summary
}

// FormatReport formats a security audit report for display.
func FormatReport(report *Report) string {
	var sb strings.Builder

	sb.WriteString("Security Audit Report\n")
	sb.WriteString("=====================\n")
	sb.WriteString(fmt.Sprintf("Time: %s\n\n", time.Unix(report.Timestamp, 0).Format(time.RFC3339)))

	sb.WriteString("Summary:\n")
	sb.WriteString(fmt.Sprintf("  Critical: %d\n", report.Summary.Critical))
	sb.WriteString(fmt.Sprintf("  Warnings: %d\n", report.Summary.Warn))
	sb.WriteString(fmt.Sprintf("  Info:     %d\n\n", report.Summary.Info))

	if len(report.Findings) == 0 {
		sb.WriteString("No issues found.\n")
		return sb.String()
	}

	sb.WriteString("Findings:\n")
	for i, f := range report.Findings {
		severity := strings.ToUpper(string(f.Severity))
		sb.WriteString(fmt.Sprintf("\n%d. [%s] %s\n", i+1, severity, f.Title))
		sb.WriteString(fmt.Sprintf("   Check: %s\n", f.CheckID))
		sb.WriteString(fmt.Sprintf("   Detail: %s\n", f.Detail))
		if f.Remediation != "" {
			sb.WriteString(fmt.Sprintf("   Fix: %s\n", f.Remediation))
		}
	}

	return sb.String()
}
