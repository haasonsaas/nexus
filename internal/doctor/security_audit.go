package doctor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// SecuritySeverity represents the severity of a security finding.
type SecuritySeverity string

const (
	SeverityInfo     SecuritySeverity = "info"
	SeverityWarning  SecuritySeverity = "warning"
	SeverityCritical SecuritySeverity = "critical"
)

// SecurityFinding represents a security-related finding.
type SecurityFinding struct {
	Severity SecuritySeverity
	Message  string
}

// SecurityAudit aggregates security findings.
type SecurityAudit struct {
	Findings []SecurityFinding
}

// AuditSecurity inspects config and workspace for common security hazards.
func AuditSecurity(cfg *config.Config, configPath string) SecurityAudit {
	audit := SecurityAudit{}

	if configPath != "" {
		if info, err := os.Stat(configPath); err == nil {
			appendPermFindings(&audit, "config file", configPath, info.Mode())
		}
	}

	if cfg != nil {
		workspacePath := strings.TrimSpace(cfg.Workspace.Path)
		if workspacePath != "" {
			if !filepath.IsAbs(workspacePath) {
				workspacePath = filepath.Clean(workspacePath)
			}
			if info, err := os.Stat(workspacePath); err == nil {
				appendPermFindings(&audit, "workspace directory", workspacePath, info.Mode())
			}
		}

		if isPublicBind(cfg.Server.Host) && !authEnabled(cfg) {
			audit.Findings = append(audit.Findings, SecurityFinding{
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("server.host %q is publicly reachable without auth (set auth.jwt_secret or api_keys)", cfg.Server.Host),
			})
		}
	}

	return audit
}

func appendPermFindings(audit *SecurityAudit, label, path string, mode os.FileMode) {
	perm := mode.Perm()
	if perm&0o022 != 0 {
		audit.Findings = append(audit.Findings, SecurityFinding{
			Severity: SeverityCritical,
			Message:  fmt.Sprintf("%s %q is group/world writable (%#o)", label, path, perm),
		})
	}
	if perm&0o044 != 0 {
		audit.Findings = append(audit.Findings, SecurityFinding{
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s %q is group/world readable (%#o)", label, path, perm),
		})
	}
}

func isPublicBind(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return true
	}
	if strings.EqualFold(trimmed, "localhost") {
		return false
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		return !ip.IsLoopback()
	}
	return true
}

func authEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	if strings.TrimSpace(cfg.Auth.JWTSecret) != "" {
		return true
	}
	return len(cfg.Auth.APIKeys) > 0
}
