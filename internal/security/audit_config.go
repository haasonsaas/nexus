package security

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// auditConfigContent checks configuration content for security issues.
// This includes secrets detection, insecure defaults, and policy checks.
func auditConfigContent(cfg *config.Config) []AuditFinding {
	var findings []AuditFinding

	if cfg == nil {
		return findings
	}

	// Check for potential secrets in plaintext
	findings = append(findings, auditSecretsInConfig(cfg)...)

	// Check for open channel policies
	findings = append(findings, auditChannelPolicies(cfg)...)

	// Check for insecure edge configuration
	findings = append(findings, auditEdgeConfig(cfg)...)

	return findings
}

// auditSecretsInConfig checks for potential secrets that look like they might
// be hardcoded rather than coming from environment variables.
func auditSecretsInConfig(cfg *config.Config) []AuditFinding {
	var findings []AuditFinding

	// Patterns that suggest a secret is hardcoded (not from env var)
	hardcodedPatterns := []*regexp.Regexp{
		regexp.MustCompile(`^sk-[a-zA-Z0-9]{20,}`),             // OpenAI API key
		regexp.MustCompile(`^xoxb-[0-9]+-[0-9]+-[a-zA-Z0-9]+`), // Slack bot token
		regexp.MustCompile(`^xapp-[0-9]+-[a-zA-Z0-9]+`),        // Slack app token
		regexp.MustCompile(`^[0-9]+:[a-zA-Z0-9_-]{35}`),        // Telegram bot token
		regexp.MustCompile(`^ghp_[a-zA-Z0-9]{36}`),             // GitHub personal access token
		regexp.MustCompile(`^gho_[a-zA-Z0-9]{36}`),             // GitHub OAuth token
		regexp.MustCompile(`^github_pat_[a-zA-Z0-9_]+`),        // GitHub fine-grained PAT
		regexp.MustCompile(`^AKIA[0-9A-Z]{16}`),                // AWS access key
		regexp.MustCompile(`^AIza[0-9A-Za-z_-]{35}`),           // Google API key
	}

	// Check LLM provider API keys
	for providerName, provider := range cfg.LLM.Providers {
		if provider.APIKey != "" {
			for _, pattern := range hardcodedPatterns {
				if pattern.MatchString(provider.APIKey) {
					findings = append(findings, AuditFinding{
						CheckID:     fmt.Sprintf("config.hardcoded_api_key.%s", providerName),
						Severity:    SeverityWarn,
						Title:       fmt.Sprintf("Potential hardcoded API key in %s provider", providerName),
						Detail:      fmt.Sprintf("The API key for llm.providers.%s appears to be hardcoded. Consider using environment variables.", providerName),
						Remediation: "Use environment variables like OPENAI_API_KEY instead of hardcoding secrets in config files.",
					})
					break
				}
			}
		}
	}

	// Check channel tokens
	if cfg.Channels.Telegram.BotToken != "" {
		for _, pattern := range hardcodedPatterns {
			if pattern.MatchString(cfg.Channels.Telegram.BotToken) {
				findings = append(findings, AuditFinding{
					CheckID:     "config.hardcoded_telegram_token",
					Severity:    SeverityWarn,
					Title:       "Potential hardcoded Telegram bot token",
					Detail:      "The Telegram bot token appears to be hardcoded in the config file.",
					Remediation: "Use environment variables for sensitive tokens.",
				})
				break
			}
		}
	}

	if cfg.Channels.Slack.BotToken != "" {
		for _, pattern := range hardcodedPatterns {
			if pattern.MatchString(cfg.Channels.Slack.BotToken) {
				findings = append(findings, AuditFinding{
					CheckID:     "config.hardcoded_slack_bot_token",
					Severity:    SeverityWarn,
					Title:       "Potential hardcoded Slack bot token",
					Detail:      "The Slack bot token appears to be hardcoded in the config file.",
					Remediation: "Use environment variables for sensitive tokens.",
				})
				break
			}
		}
	}

	if cfg.Channels.Slack.AppToken != "" {
		for _, pattern := range hardcodedPatterns {
			if pattern.MatchString(cfg.Channels.Slack.AppToken) {
				findings = append(findings, AuditFinding{
					CheckID:     "config.hardcoded_slack_app_token",
					Severity:    SeverityWarn,
					Title:       "Potential hardcoded Slack app token",
					Detail:      "The Slack app token appears to be hardcoded in the config file.",
					Remediation: "Use environment variables for sensitive tokens.",
				})
				break
			}
		}
	}

	// Check database URL for embedded passwords
	if cfg.Database.URL != "" {
		if containsEmbeddedPassword(cfg.Database.URL) {
			findings = append(findings, AuditFinding{
				CheckID:     "config.database_password_in_url",
				Severity:    SeverityWarn,
				Title:       "Database URL may contain embedded password",
				Detail:      "The database.url appears to contain an embedded password. Consider using environment variables.",
				Remediation: "Use DATABASE_URL environment variable or separate password configuration.",
			})
		}
	}

	// Check OAuth client secrets
	if cfg.Auth.OAuth.Google.ClientSecret != "" && len(cfg.Auth.OAuth.Google.ClientSecret) > 10 {
		findings = append(findings, AuditFinding{
			CheckID:     "config.oauth_google_secret",
			Severity:    SeverityInfo,
			Title:       "Google OAuth client secret in config",
			Detail:      "Google OAuth client secret is configured. Ensure this is loaded from environment variables in production.",
			Remediation: "Use environment variables for OAuth secrets.",
		})
	}

	if cfg.Auth.OAuth.GitHub.ClientSecret != "" && len(cfg.Auth.OAuth.GitHub.ClientSecret) > 10 {
		findings = append(findings, AuditFinding{
			CheckID:     "config.oauth_github_secret",
			Severity:    SeverityInfo,
			Title:       "GitHub OAuth client secret in config",
			Detail:      "GitHub OAuth client secret is configured. Ensure this is loaded from environment variables in production.",
			Remediation: "Use environment variables for OAuth secrets.",
		})
	}

	return findings
}

// containsEmbeddedPassword checks if a URL contains a password component.
func containsEmbeddedPassword(url string) bool {
	// Check for password in URL format: scheme://user:password@host
	// This is a simple heuristic
	if strings.Contains(url, "://") {
		parts := strings.SplitN(url, "://", 2)
		if len(parts) == 2 {
			authPart := strings.SplitN(parts[1], "@", 2)
			if len(authPart) == 2 {
				// Check if there's a colon in the auth part (user:pass)
				if strings.Contains(authPart[0], ":") {
					userPass := strings.SplitN(authPart[0], ":", 2)
					// If password part is non-empty and doesn't look like an env var reference
					if len(userPass) == 2 && userPass[1] != "" && !strings.HasPrefix(userPass[1], "${") {
						return true
					}
				}
			}
		}
	}
	return false
}

// auditChannelPolicies checks for overly permissive channel policies.
func auditChannelPolicies(cfg *config.Config) []AuditFinding {
	var findings []AuditFinding

	type channelPolicy struct {
		name    string
		enabled bool
		dm      config.ChannelPolicyConfig
		group   config.ChannelPolicyConfig
	}

	channels := []channelPolicy{
		{"telegram", cfg.Channels.Telegram.Enabled, cfg.Channels.Telegram.DM, cfg.Channels.Telegram.Group},
		{"discord", cfg.Channels.Discord.Enabled, cfg.Channels.Discord.DM, cfg.Channels.Discord.Group},
		{"slack", cfg.Channels.Slack.Enabled, cfg.Channels.Slack.DM, cfg.Channels.Slack.Group},
		{"whatsapp", cfg.Channels.WhatsApp.Enabled, cfg.Channels.WhatsApp.DM, cfg.Channels.WhatsApp.Group},
		{"signal", cfg.Channels.Signal.Enabled, cfg.Channels.Signal.DM, cfg.Channels.Signal.Group},
		{"imessage", cfg.Channels.IMessage.Enabled, cfg.Channels.IMessage.DM, cfg.Channels.IMessage.Group},
		{"matrix", cfg.Channels.Matrix.Enabled, cfg.Channels.Matrix.DM, cfg.Channels.Matrix.Group},
		{"teams", cfg.Channels.Teams.Enabled, cfg.Channels.Teams.DM, cfg.Channels.Teams.Group},
	}

	for _, ch := range channels {
		if !ch.enabled {
			continue
		}

		// Check DM policy
		dmPolicy := strings.ToLower(strings.TrimSpace(ch.dm.Policy))
		if dmPolicy == "" || dmPolicy == "open" {
			findings = append(findings, AuditFinding{
				CheckID:  fmt.Sprintf("config.channel.%s.dm_open", ch.name),
				Severity: SeverityInfo,
				Title:    fmt.Sprintf("%s DM policy is open", strings.Title(ch.name)),
				Detail:   fmt.Sprintf("channels.%s.dm.policy is 'open', allowing anyone to DM the bot.", ch.name),
			})
		}

		// Check group policy
		groupPolicy := strings.ToLower(strings.TrimSpace(ch.group.Policy))
		if groupPolicy == "" || groupPolicy == "open" {
			findings = append(findings, AuditFinding{
				CheckID:  fmt.Sprintf("config.channel.%s.group_open", ch.name),
				Severity: SeverityInfo,
				Title:    fmt.Sprintf("%s group policy is open", strings.Title(ch.name)),
				Detail:   fmt.Sprintf("channels.%s.group.policy is 'open', allowing messages from any group.", ch.name),
			})
		}
	}

	return findings
}

// auditEdgeConfig checks for insecure edge daemon configuration.
func auditEdgeConfig(cfg *config.Config) []AuditFinding {
	var findings []AuditFinding

	if !cfg.Edge.Enabled {
		return findings
	}

	// Check for dev auth mode
	authMode := strings.ToLower(strings.TrimSpace(cfg.Edge.AuthMode))
	if authMode == "dev" {
		findings = append(findings, AuditFinding{
			CheckID:     "config.edge_dev_mode",
			Severity:    SeverityCritical,
			Title:       "Edge daemon using dev auth mode",
			Detail:      "edge.auth_mode is set to 'dev', which accepts all connections without authentication.",
			Remediation: "Use 'token' or 'tofu' auth mode in production.",
		})
	}

	// Check for token auth without tokens configured
	if authMode == "token" && len(cfg.Edge.Tokens) == 0 {
		findings = append(findings, AuditFinding{
			CheckID:     "config.edge_no_tokens",
			Severity:    SeverityWarn,
			Title:       "Edge daemon using token auth without tokens",
			Detail:      "edge.auth_mode is 'token' but no edge.tokens are configured.",
			Remediation: "Configure edge.tokens with pre-shared authentication tokens.",
		})
	}

	return findings
}
