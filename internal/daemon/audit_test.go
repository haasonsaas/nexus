package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsNodeRuntime(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"node binary", "/usr/bin/node", true},
		{"node.exe on Windows", "C:\\Program Files\\nodejs\\node.exe", true},
		{"bun binary", "/usr/bin/bun", false},
		{"nexus binary", "/usr/bin/nexus", false},
		{"empty path", "", false},
		{"node in path", "/home/user/.nvm/versions/node/v18/bin/node", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeRuntime(tt.path)
			if result != tt.expected {
				t.Errorf("isNodeRuntime(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsBunRuntime(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"bun binary", "/usr/bin/bun", true},
		{"bun.exe on Windows", "C:\\Users\\user\\.bun\\bin\\bun.exe", true},
		{"node binary", "/usr/bin/node", false},
		{"nexus binary", "/usr/bin/nexus", false},
		{"empty path", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBunRuntime(tt.path)
			if result != tt.expected {
				t.Errorf("isBunRuntime(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsVersionManagedPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"nvm path", "/home/user/.nvm/versions/node/v18/bin/node", true},
		{"fnm path", "/home/user/.fnm/node-versions/v18/installation/bin/node", true},
		{"volta path", "/home/user/.volta/bin/node", true},
		{"asdf path", "/home/user/.asdf/installs/nodejs/18.0.0/bin/node", true},
		{"n path", "/home/user/.n/n/versions/node/18/bin/node", true},
		{"nodenv path", "/home/user/.nodenv/versions/18.0.0/bin/node", true},
		{"pnpm path", "/home/user/.local/share/pnpm/node", true},
		{"system node", "/usr/bin/node", false},
		{"homebrew node", "/opt/homebrew/bin/node", false},
		{"local bin", "/usr/local/bin/node", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVersionManagedPath(tt.path)
			if result != tt.expected {
				t.Errorf("isVersionManagedPath(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestGetMinimalServicePathParts(t *testing.T) {
	env := map[string]string{
		"HOME": "/home/testuser",
	}

	parts := getMinimalServicePathParts(env)

	if len(parts) == 0 {
		t.Error("Expected non-empty path parts")
	}

	// Check that /usr/bin is included
	found := false
	for _, p := range parts {
		if p == "/usr/bin" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected /usr/bin in minimal path parts")
	}
}

func TestNeedsRuntimeMigration(t *testing.T) {
	tests := []struct {
		name     string
		issues   []ServiceConfigIssue
		expected bool
	}{
		{
			name:     "no issues",
			issues:   []ServiceConfigIssue{},
			expected: false,
		},
		{
			name: "bun runtime issue",
			issues: []ServiceConfigIssue{
				{Code: AuditCodeGatewayRuntimeBun},
			},
			expected: true,
		},
		{
			name: "version manager issue",
			issues: []ServiceConfigIssue{
				{Code: AuditCodeGatewayRuntimeVersionManager},
			},
			expected: true,
		},
		{
			name: "node missing issue",
			issues: []ServiceConfigIssue{
				{Code: AuditCodeGatewayRuntimeNodeMissing},
			},
			expected: true,
		},
		{
			name: "unrelated issue",
			issues: []ServiceConfigIssue{
				{Code: AuditCodeSystemdRestartSec},
			},
			expected: false,
		},
		{
			name: "mixed issues with runtime",
			issues: []ServiceConfigIssue{
				{Code: AuditCodeSystemdRestartSec},
				{Code: AuditCodeGatewayRuntimeBun},
				{Code: AuditCodeLaunchdKeepAlive},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NeedsRuntimeMigration(tt.issues)
			if result != tt.expected {
				t.Errorf("NeedsRuntimeMigration() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAuditGatewayCommand(t *testing.T) {
	tests := []struct {
		name          string
		params        AuditParams
		expectedCodes []string
	}{
		{
			name: "nil command",
			params: AuditParams{
				Command: nil,
			},
			expectedCodes: []string{AuditCodeGatewayCommandMissing},
		},
		{
			name: "empty program arguments",
			params: AuditParams{
				Command: &ServiceCommand{
					ProgramArguments: []string{},
				},
			},
			expectedCodes: []string{AuditCodeGatewayCommandMissing},
		},
		{
			name: "missing gateway subcommand",
			params: AuditParams{
				Command: &ServiceCommand{
					ProgramArguments: []string{"/usr/bin/nexus", "help"},
				},
			},
			expectedCodes: []string{AuditCodeGatewayCommandMissing},
		},
		{
			name: "has gateway subcommand",
			params: AuditParams{
				Command: &ServiceCommand{
					ProgramArguments: []string{"/usr/bin/nexus", "gateway"},
				},
			},
			expectedCodes: []string{},
		},
		{
			name: "has serve subcommand",
			params: AuditParams{
				Command: &ServiceCommand{
					ProgramArguments: []string{"/usr/bin/nexus", "serve", "--config", "nexus.yaml"},
				},
			},
			expectedCodes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := auditGatewayCommand(tt.params)

			if len(issues) != len(tt.expectedCodes) {
				t.Errorf("Expected %d issues, got %d", len(tt.expectedCodes), len(issues))
				return
			}

			for i, code := range tt.expectedCodes {
				if issues[i].Code != code {
					t.Errorf("Expected issue code %q, got %q", code, issues[i].Code)
				}
			}
		})
	}
}

func TestAuditGatewayServicePath(t *testing.T) {
	tests := []struct {
		name          string
		params        AuditParams
		checkContains []string
	}{
		{
			name: "no PATH set",
			params: AuditParams{
				Platform: "linux",
				Command: &ServiceCommand{
					Environment: map[string]string{},
				},
				Env: map[string]string{},
			},
			checkContains: []string{AuditCodeGatewayPathMissing},
		},
		{
			name: "PATH with version manager",
			params: AuditParams{
				Platform: "linux",
				Command: &ServiceCommand{
					Environment: map[string]string{
						"PATH": "/home/user/.nvm/versions/node/v18/bin:/usr/bin",
					},
				},
			},
			checkContains: []string{AuditCodeGatewayPathNonMinimal},
		},
		{
			name: "good PATH",
			params: AuditParams{
				Platform: "linux",
				Command: &ServiceCommand{
					Environment: map[string]string{
						"PATH": "/usr/local/bin:/usr/bin:/bin",
					},
				},
			},
			checkContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := auditGatewayServicePath(tt.params)

			for _, expectedCode := range tt.checkContains {
				found := false
				for _, issue := range issues {
					if issue.Code == expectedCode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find issue code %q", expectedCode)
				}
			}
		})
	}
}

func TestAuditGatewayRuntime(t *testing.T) {
	tests := []struct {
		name          string
		params        AuditParams
		expectedCodes []string
	}{
		{
			name: "bun runtime",
			params: AuditParams{
				Command: &ServiceCommand{
					ProgramArguments: []string{"/usr/bin/bun", "nexus.js"},
				},
			},
			expectedCodes: []string{AuditCodeGatewayRuntimeBun},
		},
		{
			name: "version managed node",
			params: AuditParams{
				Command: &ServiceCommand{
					ProgramArguments: []string{"/home/user/.nvm/versions/node/v18/bin/node", "nexus.js"},
				},
			},
			expectedCodes: []string{AuditCodeGatewayRuntimeVersionManager},
		},
		{
			name: "nil command",
			params: AuditParams{
				Command: nil,
			},
			expectedCodes: []string{},
		},
		{
			name: "nexus binary (not node or bun)",
			params: AuditParams{
				Command: &ServiceCommand{
					ProgramArguments: []string{"/usr/bin/nexus", "serve"},
				},
			},
			expectedCodes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := auditGatewayRuntime(tt.params)

			if len(issues) != len(tt.expectedCodes) {
				t.Errorf("Expected %d issues, got %d: %+v", len(tt.expectedCodes), len(issues), issues)
				return
			}

			for i, code := range tt.expectedCodes {
				if issues[i].Code != code {
					t.Errorf("Expected issue code %q, got %q", code, issues[i].Code)
				}
			}
		})
	}
}

func TestAuditSystemdUnit(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectedCodes []string
	}{
		{
			name: "good unit file",
			content: `[Unit]
Description=Nexus Gateway
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/nexus serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`,
			expectedCodes: []string{},
		},
		{
			name: "missing After network-online",
			content: `[Unit]
Description=Nexus Gateway
After=network.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/nexus serve
RestartSec=5

[Install]
WantedBy=default.target
`,
			expectedCodes: []string{AuditCodeSystemdAfterNetwork},
		},
		{
			name: "missing Wants network-online",
			content: `[Unit]
Description=Nexus Gateway
After=network-online.target

[Service]
ExecStart=/usr/bin/nexus serve
RestartSec=5

[Install]
WantedBy=default.target
`,
			expectedCodes: []string{AuditCodeSystemdWantsNetwork},
		},
		{
			name: "RestartSec too low",
			content: `[Unit]
Description=Nexus Gateway
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/nexus serve
RestartSec=1

[Install]
WantedBy=default.target
`,
			expectedCodes: []string{AuditCodeSystemdRestartSec},
		},
		{
			name: "RestartSec too high",
			content: `[Unit]
Description=Nexus Gateway
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/nexus serve
RestartSec=30

[Install]
WantedBy=default.target
`,
			expectedCodes: []string{AuditCodeSystemdRestartSec},
		},
		{
			name: "missing RestartSec",
			content: `[Unit]
Description=Nexus Gateway
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/nexus serve

[Install]
WantedBy=default.target
`,
			expectedCodes: []string{AuditCodeSystemdRestartSec},
		},
		{
			name: "all issues",
			content: `[Unit]
Description=Nexus Gateway

[Service]
ExecStart=/usr/bin/nexus serve

[Install]
WantedBy=default.target
`,
			expectedCodes: []string{
				AuditCodeSystemdAfterNetwork,
				AuditCodeSystemdWantsNetwork,
				AuditCodeSystemdRestartSec,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary unit file
			tmpDir := t.TempDir()
			unitPath := filepath.Join(tmpDir, "nexus.service")
			if err := os.WriteFile(unitPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write unit file: %v", err)
			}

			issues, err := auditSystemdUnit(unitPath)
			if err != nil {
				t.Fatalf("auditSystemdUnit() error: %v", err)
			}

			if len(issues) != len(tt.expectedCodes) {
				t.Errorf("Expected %d issues, got %d: %+v", len(tt.expectedCodes), len(issues), issues)
				return
			}

			for _, expectedCode := range tt.expectedCodes {
				found := false
				for _, issue := range issues {
					if issue.Code == expectedCode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find issue code %q in %+v", expectedCode, issues)
				}
			}
		})
	}
}

func TestAuditLaunchdPlist(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectedCodes []string
	}{
		{
			name: "good plist",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/nexus</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`,
			expectedCodes: []string{},
		},
		{
			name: "RunAtLoad false",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
    <key>RunAtLoad</key>
    <false/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`,
			expectedCodes: []string{AuditCodeLaunchdRunAtLoad},
		},
		{
			name: "missing RunAtLoad",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`,
			expectedCodes: []string{AuditCodeLaunchdRunAtLoad},
		},
		{
			name: "KeepAlive false",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
</dict>
</plist>
`,
			expectedCodes: []string{AuditCodeLaunchdKeepAlive},
		},
		{
			name: "missing KeepAlive",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>
`,
			expectedCodes: []string{AuditCodeLaunchdKeepAlive},
		},
		{
			name: "KeepAlive as dict (valid)",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
</dict>
</plist>
`,
			expectedCodes: []string{},
		},
		{
			name: "all issues",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
</dict>
</plist>
`,
			expectedCodes: []string{AuditCodeLaunchdRunAtLoad, AuditCodeLaunchdKeepAlive},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary plist file
			tmpDir := t.TempDir()
			plistPath := filepath.Join(tmpDir, "com.haasonsaas.nexus.plist")
			if err := os.WriteFile(plistPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write plist file: %v", err)
			}

			issues, err := auditLaunchdPlist(plistPath)
			if err != nil {
				t.Fatalf("auditLaunchdPlist() error: %v", err)
			}

			if len(issues) != len(tt.expectedCodes) {
				t.Errorf("Expected %d issues, got %d: %+v", len(tt.expectedCodes), len(issues), issues)
				return
			}

			for _, expectedCode := range tt.expectedCodes {
				found := false
				for _, issue := range issues {
					if issue.Code == expectedCode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find issue code %q in %+v", expectedCode, issues)
				}
			}
		})
	}
}

func TestAuditGatewayServiceConfig(t *testing.T) {
	tests := []struct {
		name          string
		params        AuditParams
		expectOK      bool
		minIssueCount int
	}{
		{
			name: "minimal config with essential paths",
			params: AuditParams{
				Platform: "linux",
				Env: map[string]string{
					"HOME": "/nonexistent", // Use nonexistent home to avoid local filesystem deps
				},
				Command: &ServiceCommand{
					ProgramArguments: []string{"/usr/bin/nexus", "serve"},
					Environment: map[string]string{
						"PATH": "/usr/local/bin:/usr/bin:/bin",
					},
				},
			},
			expectOK:      true,
			minIssueCount: 0,
		},
		{
			name: "missing command",
			params: AuditParams{
				Platform: "linux",
				Command:  nil,
			},
			expectOK:      false,
			minIssueCount: 1,
		},
		{
			name: "bun runtime with path issues",
			params: AuditParams{
				Platform: "darwin",
				Command: &ServiceCommand{
					ProgramArguments: []string{"/usr/local/bin/bun", "serve"},
					Environment:      map[string]string{},
				},
			},
			expectOK:      false,
			minIssueCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit, err := AuditGatewayServiceConfig(tt.params)
			if err != nil {
				t.Fatalf("AuditGatewayServiceConfig() error: %v", err)
			}

			if audit.OK != tt.expectOK {
				t.Errorf("Expected OK=%v, got %v (issues: %+v)", tt.expectOK, audit.OK, audit.Issues)
			}

			if len(audit.Issues) < tt.minIssueCount {
				t.Errorf("Expected at least %d issues, got %d", tt.minIssueCount, len(audit.Issues))
			}
		})
	}
}

func TestAuditGatewayServiceConfigWithServiceFile(t *testing.T) {
	// Test Linux with systemd unit file
	t.Run("linux with unit file", func(t *testing.T) {
		tmpDir := t.TempDir()
		unitPath := filepath.Join(tmpDir, "nexus.service")
		unitContent := `[Unit]
Description=Nexus Gateway

[Service]
ExecStart=/usr/bin/nexus serve

[Install]
WantedBy=default.target
`
		if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
			t.Fatalf("Failed to write unit file: %v", err)
		}

		params := AuditParams{
			Platform: "linux",
			Command: &ServiceCommand{
				ProgramArguments: []string{"/usr/bin/nexus", "serve"},
				SourcePath:       unitPath,
				Environment: map[string]string{
					"PATH": "/usr/local/bin:/usr/bin:/bin",
				},
			},
		}

		audit, err := AuditGatewayServiceConfig(params)
		if err != nil {
			t.Fatalf("AuditGatewayServiceConfig() error: %v", err)
		}

		// Should have systemd-related issues
		hasSystemdIssue := false
		for _, issue := range audit.Issues {
			if issue.Code == AuditCodeSystemdAfterNetwork ||
				issue.Code == AuditCodeSystemdWantsNetwork ||
				issue.Code == AuditCodeSystemdRestartSec {
				hasSystemdIssue = true
				break
			}
		}
		if !hasSystemdIssue {
			t.Error("Expected systemd-related issues")
		}
	})

	// Test macOS with launchd plist
	t.Run("darwin with plist file", func(t *testing.T) {
		tmpDir := t.TempDir()
		plistPath := filepath.Join(tmpDir, "com.haasonsaas.nexus.plist")
		plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.haasonsaas.nexus</string>
</dict>
</plist>
`
		if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
			t.Fatalf("Failed to write plist file: %v", err)
		}

		params := AuditParams{
			Platform: "darwin",
			Command: &ServiceCommand{
				ProgramArguments: []string{"/usr/local/bin/nexus", "serve"},
				SourcePath:       plistPath,
				Environment: map[string]string{
					"PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
				},
			},
		}

		audit, err := AuditGatewayServiceConfig(params)
		if err != nil {
			t.Fatalf("AuditGatewayServiceConfig() error: %v", err)
		}

		// Should have launchd-related issues
		hasLaunchdIssue := false
		for _, issue := range audit.Issues {
			if issue.Code == AuditCodeLaunchdRunAtLoad ||
				issue.Code == AuditCodeLaunchdKeepAlive {
				hasLaunchdIssue = true
				break
			}
		}
		if !hasLaunchdIssue {
			t.Error("Expected launchd-related issues")
		}
	})
}

func TestServiceConfigIssueLevels(t *testing.T) {
	// Test that issues have proper levels set
	params := AuditParams{
		Platform: "linux",
		Command: &ServiceCommand{
			ProgramArguments: []string{"/home/user/.nvm/versions/node/v18/bin/node", "nexus.js"},
			Environment: map[string]string{
				"PATH": "/home/user/.nvm/versions/node/v18/bin:/usr/bin",
			},
		},
	}

	audit, err := AuditGatewayServiceConfig(params)
	if err != nil {
		t.Fatalf("AuditGatewayServiceConfig() error: %v", err)
	}

	for _, issue := range audit.Issues {
		if issue.Level != LevelRecommended && issue.Level != LevelAggressive {
			t.Errorf("Issue %q has invalid level: %q", issue.Code, issue.Level)
		}

		// Version manager path issues should be aggressive
		if issue.Code == AuditCodeGatewayPathNonMinimal && issue.Level != LevelAggressive {
			t.Errorf("Expected %q to have level %q, got %q",
				AuditCodeGatewayPathNonMinimal, LevelAggressive, issue.Level)
		}
	}
}
