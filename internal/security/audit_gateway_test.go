package security

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestAuditGatewayConfig_Nil(t *testing.T) {
	findings := AuditGatewayConfig(nil)
	if len(findings) != 0 {
		t.Errorf("Expected 0 findings for nil config, got %d", len(findings))
	}
}

func TestAuditServerBind_PublicNoAuth(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "0.0.0.0",
		},
		Auth: config.AuthConfig{
			// No API keys or JWT secret
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "server.bind_no_auth" {
			found = true
			if f.Severity != SeverityCritical {
				t.Errorf("Expected critical severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find server.bind_no_auth finding")
	}
}

func TestAuditServerBind_PublicWithAuth(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "0.0.0.0",
		},
		Auth: config.AuthConfig{
			APIKeys: []config.APIKeyConfig{
				{Key: "a-secure-api-key-that-is-long-enough"},
			},
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "server.bind_public" {
			found = true
			if f.Severity != SeverityWarn {
				t.Errorf("Expected warn severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find server.bind_public finding")
	}
}

func TestAuditGatewayAuth_WeakAPIKey(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			APIKeys: []config.APIKeyConfig{
				{Key: "short"}, // Less than 24 chars
			},
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "auth.weak_api_key" {
			found = true
			if f.Severity != SeverityWarn {
				t.Errorf("Expected warn severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find auth.weak_api_key finding")
	}
}

func TestAuditGatewayAuth_WeakJWTSecret(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret: "tooshort", // Less than 32 chars
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "auth.weak_jwt_secret" {
			found = true
			if f.Severity != SeverityWarn {
				t.Errorf("Expected warn severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find auth.weak_jwt_secret finding")
	}
}

func TestAuditChannelSecurity_MatrixOpenAccess(t *testing.T) {
	cfg := &config.Config{
		Channels: config.ChannelsConfig{
			Matrix: config.MatrixConfig{
				Enabled: true,
				// No allowed users or rooms
			},
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "channel.matrix.open_access" {
			found = true
			if f.Severity != SeverityCritical {
				t.Errorf("Expected critical severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find channel.matrix.open_access finding")
	}
}

func TestAuditChannelSecurity_MatrixWithAllowlist(t *testing.T) {
	cfg := &config.Config{
		Channels: config.ChannelsConfig{
			Matrix: config.MatrixConfig{
				Enabled:      true,
				AllowedUsers: []string{"@user:example.com"},
			},
		},
	}

	findings := AuditGatewayConfig(cfg)

	for _, f := range findings {
		if f.CheckID == "channel.matrix.open_access" {
			t.Error("Should not find channel.matrix.open_access when allowlist is set")
		}
	}
}

func TestAuditToolPolicies_WildcardAllowlist(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Execution: config.ToolExecutionConfig{
				Approval: config.ApprovalConfig{
					Allowlist: []string{"*"},
				},
			},
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "tools.allowlist.wildcard" {
			found = true
			if f.Severity != SeverityCritical {
				t.Errorf("Expected critical severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find tools.allowlist.wildcard finding")
	}
}

func TestAuditToolPolicies_DefaultAllowed(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Execution: config.ToolExecutionConfig{
				Approval: config.ApprovalConfig{
					DefaultDecision: "allowed",
				},
			},
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "tools.default_allowed" {
			found = true
			if f.Severity != SeverityWarn {
				t.Errorf("Expected warn severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find tools.default_allowed finding")
	}
}

func TestAuditMarketplace_SkipVerify(t *testing.T) {
	cfg := &config.Config{
		Marketplace: config.MarketplaceConfig{
			Enabled:    true,
			SkipVerify: true,
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "marketplace.skip_verify" {
			found = true
			if f.Severity != SeverityCritical {
				t.Errorf("Expected critical severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find marketplace.skip_verify finding")
	}
}

func TestAuditMarketplace_NoTrustedKeys(t *testing.T) {
	cfg := &config.Config{
		Marketplace: config.MarketplaceConfig{
			Enabled:    true,
			SkipVerify: false,
			// No trusted keys
		},
	}

	findings := AuditGatewayConfig(cfg)

	found := false
	for _, f := range findings {
		if f.CheckID == "marketplace.no_trusted_keys" {
			found = true
			if f.Severity != SeverityWarn {
				t.Errorf("Expected warn severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find marketplace.no_trusted_keys finding")
	}
}
