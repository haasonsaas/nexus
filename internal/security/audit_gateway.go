package security

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// AuditGatewayConfig checks gateway configuration for security issues.
func AuditGatewayConfig(cfg *config.Config) []Finding {
	var findings []Finding

	if cfg == nil {
		return findings
	}

	// Check server bind address
	findings = append(findings, auditServerBind(cfg)...)

	// Check authentication
	findings = append(findings, auditGatewayAuth(cfg)...)

	// Check channel security
	findings = append(findings, auditChannelSecurity(cfg)...)

	// Check tool policies
	findings = append(findings, auditToolPolicies(cfg)...)

	// Check marketplace security
	findings = append(findings, auditMarketplace(cfg)...)

	return findings
}

func auditServerBind(cfg *config.Config) []Finding {
	var findings []Finding

	host := cfg.Server.Host
	if host == "" {
		host = "localhost"
	}

	// Check if binding to all interfaces without auth
	if host == "0.0.0.0" || host == "::" || host == "" {
		hasAuth := len(cfg.Auth.APIKeys) > 0 || cfg.Auth.JWTSecret != ""
		if !hasAuth {
			findings = append(findings, Finding{
				CheckID:     "server.bind_no_auth",
				Severity:    SeverityCritical,
				Title:       "Server binds to all interfaces without auth",
				Detail:      fmt.Sprintf("server.host=%q but no API keys or JWT secret configured.", host),
				Remediation: "Add auth.api_keys or auth.jwt_secret, or bind to localhost only.",
			})
		} else {
			findings = append(findings, Finding{
				CheckID:  "server.bind_public",
				Severity: SeverityWarn,
				Title:    "Server binds to all interfaces",
				Detail:   fmt.Sprintf("server.host=%q exposes the server beyond localhost.", host),
			})
		}
	}

	return findings
}

func auditGatewayAuth(cfg *config.Config) []Finding {
	var findings []Finding

	// Check API key strength
	for i, key := range cfg.Auth.APIKeys {
		if len(key.Key) < 24 {
			findings = append(findings, Finding{
				CheckID:     "auth.weak_api_key",
				Severity:    SeverityWarn,
				Title:       fmt.Sprintf("API key #%d is short", i+1),
				Detail:      fmt.Sprintf("API key has only %d characters; use at least 24 for security.", len(key.Key)),
				Remediation: "Generate a longer random API key (32+ characters recommended).",
			})
		}
	}

	// Check JWT secret strength
	if cfg.Auth.JWTSecret != "" && len(cfg.Auth.JWTSecret) < 32 {
		findings = append(findings, Finding{
			CheckID:     "auth.weak_jwt_secret",
			Severity:    SeverityWarn,
			Title:       "JWT secret is short",
			Detail:      fmt.Sprintf("JWT secret has only %d characters; use at least 32 for security.", len(cfg.Auth.JWTSecret)),
			Remediation: "Generate a longer random JWT secret (64+ characters recommended).",
		})
	}

	// Check for empty auth when channels are enabled
	hasChannels := cfg.Channels.Telegram.Enabled || cfg.Channels.Discord.Enabled ||
		cfg.Channels.Slack.Enabled || cfg.Channels.WhatsApp.Enabled ||
		cfg.Channels.Signal.Enabled || cfg.Channels.Matrix.Enabled
	hasAuth := len(cfg.Auth.APIKeys) > 0 || cfg.Auth.JWTSecret != ""
	if hasChannels && !hasAuth {
		findings = append(findings, Finding{
			CheckID:  "auth.channels_no_auth",
			Severity: SeverityInfo,
			Title:    "Channels enabled without gateway auth",
			Detail:   "Messaging channels are enabled but gateway has no auth configured.",
		})
	}

	return findings
}

func auditChannelSecurity(cfg *config.Config) []Finding {
	var findings []Finding

	// Check Matrix (has access controls)
	if cfg.Channels.Matrix.Enabled {
		if len(cfg.Channels.Matrix.AllowedUsers) == 0 && len(cfg.Channels.Matrix.AllowedRooms) == 0 {
			findings = append(findings, Finding{
				CheckID:     "channel.matrix.open_access",
				Severity:    SeverityCritical,
				Title:       "Matrix has no access restrictions",
				Detail:      "Matrix is enabled but no allowed_users or allowed_rooms are set.",
				Remediation: "Add channels.matrix.allowed_users or channels.matrix.allowed_rooms to restrict access.",
			})
		}
	}

	// Note: Discord, Telegram, Slack, WhatsApp, Signal don't have allowlist configs
	// Flag this as a potential security concern for public-facing bots

	if cfg.Channels.Discord.Enabled {
		findings = append(findings, Finding{
			CheckID:  "channel.discord.no_allowlist_support",
			Severity: SeverityInfo,
			Title:    "Discord lacks user/channel allowlists",
			Detail:   "Discord adapter doesn't support allowed_users or allowed_channels config.",
		})
	}

	if cfg.Channels.Telegram.Enabled {
		findings = append(findings, Finding{
			CheckID:  "channel.telegram.no_allowlist_support",
			Severity: SeverityInfo,
			Title:    "Telegram lacks user/chat allowlists",
			Detail:   "Telegram adapter doesn't support allowed_users or allowed_chats config.",
		})
	}

	if cfg.Channels.Slack.Enabled {
		findings = append(findings, Finding{
			CheckID:  "channel.slack.no_allowlist_support",
			Severity: SeverityInfo,
			Title:    "Slack lacks user/channel allowlists",
			Detail:   "Slack adapter doesn't support allowed_users or allowed_channels config.",
		})
	}

	return findings
}

func auditToolPolicies(cfg *config.Config) []Finding {
	var findings []Finding

	execution := cfg.Tools.Execution
	approval := execution.Approval

	// Check for wildcard tool approvals
	for _, pattern := range execution.RequireApproval {
		if pattern == "*" {
			findings = append(findings, Finding{
				CheckID:  "tools.approval.wildcard",
				Severity: SeverityInfo,
				Title:    "All tools require approval",
				Detail:   "tools.execution.require_approval contains '*' - all tools need user confirmation.",
			})
			break
		}
	}

	// Check for overly permissive allowlists
	if len(approval.Allowlist) > 50 {
		findings = append(findings, Finding{
			CheckID:     "tools.allowlist.large",
			Severity:    SeverityWarn,
			Title:       "Tool allowlist is very large",
			Detail:      fmt.Sprintf("tools.execution.approval.allowlist has %d entries; consider using denylist instead.", len(approval.Allowlist)),
			Remediation: "Use tools.execution.approval.denylist to block specific dangerous tools instead.",
		})
	}

	// Check for wildcard allowlist (allows everything)
	for _, pattern := range approval.Allowlist {
		if pattern == "*" {
			findings = append(findings, Finding{
				CheckID:     "tools.allowlist.wildcard",
				Severity:    SeverityCritical,
				Title:       "Tool allowlist allows everything",
				Detail:      "tools.execution.approval.allowlist contains '*' - all tools are auto-approved.",
				Remediation: "Remove '*' from allowlist and explicitly list allowed tools.",
			})
			break
		}
	}

	// Check for dangerous tools in allowlist without explicit approval
	dangerousPatterns := []string{"bash", "exec", "shell", "run_command", "execute_code"}
	for _, dangerous := range dangerousPatterns {
		for _, allowed := range approval.Allowlist {
			if strings.Contains(strings.ToLower(allowed), dangerous) {
				// Check if also in require_approval
				requiresApproval := false
				for _, req := range execution.RequireApproval {
					if req == allowed || req == "*" {
						requiresApproval = true
						break
					}
				}
				if !requiresApproval {
					findings = append(findings, Finding{
						CheckID:     fmt.Sprintf("tools.dangerous.%s", dangerous),
						Severity:    SeverityWarn,
						Title:       fmt.Sprintf("Dangerous tool pattern '%s' in allowlist", allowed),
						Detail:      fmt.Sprintf("Tool '%s' can execute arbitrary code but doesn't require approval.", allowed),
						Remediation: fmt.Sprintf("Add '%s' to tools.execution.require_approval.", allowed),
					})
				}
			}
		}
	}

	// Check default decision
	if approval.DefaultDecision == "allowed" {
		findings = append(findings, Finding{
			CheckID:     "tools.default_allowed",
			Severity:    SeverityWarn,
			Title:       "Default tool decision is 'allowed'",
			Detail:      "Unrecognized tools are auto-approved by default.",
			Remediation: "Set tools.execution.approval.default_decision to 'pending' or 'denied'.",
		})
	}

	return findings
}

func auditMarketplace(cfg *config.Config) []Finding {
	var findings []Finding

	if !cfg.Marketplace.Enabled {
		return findings
	}

	// Check for skip verify
	if cfg.Marketplace.SkipVerify {
		findings = append(findings, Finding{
			CheckID:     "marketplace.skip_verify",
			Severity:    SeverityCritical,
			Title:       "Marketplace signature verification disabled",
			Detail:      "marketplace.skip_verify=true allows installation of unsigned plugins.",
			Remediation: "Set marketplace.skip_verify=false and configure trusted_keys.",
		})
	}

	// Check for empty trusted keys with verification enabled
	if !cfg.Marketplace.SkipVerify && len(cfg.Marketplace.TrustedKeys) == 0 {
		findings = append(findings, Finding{
			CheckID:     "marketplace.no_trusted_keys",
			Severity:    SeverityWarn,
			Title:       "No marketplace trusted keys configured",
			Detail:      "Marketplace verification is enabled but no trusted_keys are set.",
			Remediation: "Add public keys to marketplace.trusted_keys for plugin verification.",
		})
	}

	return findings
}
