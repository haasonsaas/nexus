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

		// Channel policy audits
		auditChannelPolicies(&audit, cfg)

		// Elevated mode audits
		auditElevatedConfig(&audit, cfg)

		// Sandbox mode audits
		auditSandboxConfig(&audit, cfg)
	}

	return audit
}

// auditChannelPolicies checks for insecure channel configurations.
func auditChannelPolicies(audit *SecurityAudit, cfg *config.Config) {
	// Check if Telegram has open DMs without allowlist
	if cfg.Channels.Telegram.Enabled {
		if !hasChannelAllowlist(cfg, "telegram") {
			audit.Findings = append(audit.Findings, SecurityFinding{
				Severity: SeverityWarning,
				Message:  "Telegram channel enabled without sender allowlist (anyone can message the bot)",
			})
		}
	}

	// Check Discord
	if cfg.Channels.Discord.Enabled {
		if !hasChannelAllowlist(cfg, "discord") {
			audit.Findings = append(audit.Findings, SecurityFinding{
				Severity: SeverityWarning,
				Message:  "Discord channel enabled without sender allowlist",
			})
		}
	}

	// Check WhatsApp
	if cfg.Channels.WhatsApp.Enabled {
		if !hasChannelAllowlist(cfg, "whatsapp") {
			audit.Findings = append(audit.Findings, SecurityFinding{
				Severity: SeverityWarning,
				Message:  "WhatsApp channel enabled without sender allowlist",
			})
		}
	}

	// Check Slack
	if cfg.Channels.Slack.Enabled {
		if !hasChannelAllowlist(cfg, "slack") {
			audit.Findings = append(audit.Findings, SecurityFinding{
				Severity: SeverityInfo,
				Message:  "Slack channel enabled - ensure workspace access is properly configured",
			})
		}
	}
}

// auditElevatedConfig checks elevated execution configuration.
func auditElevatedConfig(audit *SecurityAudit, cfg *config.Config) {
	if cfg.Tools.Elevated.Enabled != nil && *cfg.Tools.Elevated.Enabled {
		if len(cfg.Tools.Elevated.AllowFrom) == 0 {
			audit.Findings = append(audit.Findings, SecurityFinding{
				Severity: SeverityCritical,
				Message:  "Elevated mode enabled without allow_from restrictions (any user can enable elevated tools)",
			})
		}

		if len(cfg.Tools.Elevated.Tools) == 0 {
			audit.Findings = append(audit.Findings, SecurityFinding{
				Severity: SeverityInfo,
				Message:  "Elevated mode enabled with default tools (execute_code)",
			})
		}
	}
}

// auditSandboxConfig checks sandbox configuration.
func auditSandboxConfig(audit *SecurityAudit, cfg *config.Config) {
	if !cfg.Tools.Sandbox.Enabled {
		// Check if code execution tools are enabled without sandbox
		audit.Findings = append(audit.Findings, SecurityFinding{
			Severity: SeverityInfo,
			Message:  "Sandbox disabled - code execution runs on host (ensure tools.policy restricts execute_code)",
		})
	}

	if cfg.Tools.Sandbox.NetworkEnabled {
		audit.Findings = append(audit.Findings, SecurityFinding{
			Severity: SeverityWarning,
			Message:  "Sandbox network access enabled - sandboxed code can make outbound connections",
		})
	}
}

// hasChannelAllowlist checks if a channel has an allowlist configured via elevated.allow_from.
func hasChannelAllowlist(cfg *config.Config, channel string) bool {
	// Check global allowlist in elevated config
	if len(cfg.Tools.Elevated.AllowFrom) > 0 {
		for channelKey := range cfg.Tools.Elevated.AllowFrom {
			if strings.EqualFold(channelKey, channel) {
				return true
			}
		}
	}

	// Slack uses workspace-level access, so consider it allowlisted by default
	if strings.EqualFold(channel, "slack") {
		return true
	}

	return false
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
