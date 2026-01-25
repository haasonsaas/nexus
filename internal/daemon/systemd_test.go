package daemon

import (
	"strings"
	"testing"
)

func TestResolveSystemdServiceName(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name:     "default name",
			env:      map[string]string{},
			expected: DefaultSystemdServiceName,
		},
		{
			name: "override with env var",
			env: map[string]string{
				EnvNexusSystemdUnit: "custom-unit",
			},
			expected: "custom-unit",
		},
		{
			name: "strips .service suffix",
			env: map[string]string{
				EnvNexusSystemdUnit: "custom-unit.service",
			},
			expected: "custom-unit",
		},
		{
			name: "profile-specific name",
			env: map[string]string{
				EnvNexusProfile: "prod",
			},
			expected: "nexus-gateway-prod",
		},
		{
			name: "env var takes precedence over profile",
			env: map[string]string{
				EnvNexusProfile:     "prod",
				EnvNexusSystemdUnit: "override-unit",
			},
			expected: "override-unit",
		},
		{
			name: "whitespace trimmed",
			env: map[string]string{
				EnvNexusSystemdUnit: "  trimmed-unit  ",
			},
			expected: "trimmed-unit",
		},
		{
			name: "default profile ignored",
			env: map[string]string{
				EnvNexusProfile: "default",
			},
			expected: DefaultSystemdServiceName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveSystemdServiceName(tt.env)
			if result != tt.expected {
				t.Errorf("resolveSystemdServiceName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestResolveSystemdUnitPath(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantSuffix  string
		wantContain string
	}{
		{
			name: "default path",
			env: map[string]string{
				"HOME": "/home/test",
			},
			wantSuffix:  ".service",
			wantContain: ".config/systemd/user",
		},
		{
			name: "with profile",
			env: map[string]string{
				"HOME":          "/home/test",
				EnvNexusProfile: "dev",
			},
			wantContain: "nexus-gateway-dev.service",
		},
		{
			name:        "no home uses dot",
			env:         map[string]string{},
			wantContain: ".config/systemd/user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveSystemdUnitPath(tt.env)
			if !strings.HasSuffix(result, tt.wantSuffix) && tt.wantSuffix != "" {
				t.Errorf("resolveSystemdUnitPath() = %q, want suffix %q", result, tt.wantSuffix)
			}
			if !strings.Contains(result, tt.wantContain) {
				t.Errorf("resolveSystemdUnitPath() = %q, want contain %q", result, tt.wantContain)
			}
		})
	}
}

func TestBuildSystemdUnit(t *testing.T) {
	tests := []struct {
		name string
		opts struct {
			Description      string
			ProgramArguments []string
			WorkingDirectory string
			Environment      map[string]string
		}
		wantContains []string
	}{
		{
			name: "basic unit",
			opts: struct {
				Description      string
				ProgramArguments []string
				WorkingDirectory string
				Environment      map[string]string
			}{
				ProgramArguments: []string{"/usr/bin/nexus", "serve"},
			},
			wantContains: []string{
				"[Unit]",
				"Description=Nexus Gateway",
				"After=network-online.target",
				"Wants=network-online.target",
				"[Service]",
				"ExecStart=/usr/bin/nexus serve",
				"Restart=always",
				"RestartSec=5",
				"KillMode=process",
				"[Install]",
				"WantedBy=default.target",
			},
		},
		{
			name: "custom description",
			opts: struct {
				Description      string
				ProgramArguments []string
				WorkingDirectory string
				Environment      map[string]string
			}{
				Description:      "Custom Service Description",
				ProgramArguments: []string{"/usr/bin/nexus", "serve"},
			},
			wantContains: []string{
				"Description=Custom Service Description",
			},
		},
		{
			name: "with working directory",
			opts: struct {
				Description      string
				ProgramArguments []string
				WorkingDirectory string
				Environment      map[string]string
			}{
				ProgramArguments: []string{"/usr/bin/nexus", "serve"},
				WorkingDirectory: "/var/lib/nexus",
			},
			wantContains: []string{
				"WorkingDirectory=/var/lib/nexus",
			},
		},
		{
			name: "with environment variables",
			opts: struct {
				Description      string
				ProgramArguments []string
				WorkingDirectory string
				Environment      map[string]string
			}{
				ProgramArguments: []string{"/usr/bin/nexus", "serve"},
				Environment: map[string]string{
					"FOO": "bar",
				},
			},
			wantContains: []string{
				"Environment=FOO=bar",
			},
		},
		{
			name: "quotes args with spaces",
			opts: struct {
				Description      string
				ProgramArguments []string
				WorkingDirectory string
				Environment      map[string]string
			}{
				ProgramArguments: []string{"/usr/bin/nexus", "serve", "--config", "/path with spaces/config.yaml"},
			},
			wantContains: []string{
				`"/path with spaces/config.yaml"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildSystemdUnit(tt.opts)
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("BuildSystemdUnit() missing %q in:\n%s", want, result)
				}
			}
		})
	}
}

func TestSystemdEscapeArg(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"/usr/bin/test", "/usr/bin/test"},
		{"path with spaces", `"path with spaces"`},
		{`path with "quotes"`, `"path with \"quotes\""`},
		{`path\with\backslash`, `"path\\with\\backslash"`},
		{"path\twith\ttabs", `"path	with	tabs"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := systemdEscapeArg(tt.input)
			if result != tt.expected {
				t.Errorf("systemdEscapeArg(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseSystemdShow(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected SystemdShowInfo
	}{
		{
			name: "active state",
			output: `ActiveState=active
SubState=running
MainPID=4242
ExecMainStatus=0
ExecMainCode=exited
`,
			expected: SystemdShowInfo{
				ActiveState:    "active",
				SubState:       "running",
				MainPID:        4242,
				ExecMainStatus: 0,
				ExecMainCode:   "exited",
			},
		},
		{
			name: "inactive state",
			output: `ActiveState=inactive
SubState=dead
MainPID=0
ExecMainStatus=2
ExecMainCode=exited
`,
			expected: SystemdShowInfo{
				ActiveState:    "inactive",
				SubState:       "dead",
				ExecMainStatus: 2,
				ExecMainCode:   "exited",
			},
		},
		{
			name:     "empty output",
			output:   "",
			expected: SystemdShowInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSystemdShow(tt.output)
			if result.ActiveState != tt.expected.ActiveState {
				t.Errorf("ActiveState = %q, want %q", result.ActiveState, tt.expected.ActiveState)
			}
			if result.SubState != tt.expected.SubState {
				t.Errorf("SubState = %q, want %q", result.SubState, tt.expected.SubState)
			}
			if result.MainPID != tt.expected.MainPID {
				t.Errorf("MainPID = %d, want %d", result.MainPID, tt.expected.MainPID)
			}
			if result.ExecMainStatus != tt.expected.ExecMainStatus {
				t.Errorf("ExecMainStatus = %d, want %d", result.ExecMainStatus, tt.expected.ExecMainStatus)
			}
			if result.ExecMainCode != tt.expected.ExecMainCode {
				t.Errorf("ExecMainCode = %q, want %q", result.ExecMainCode, tt.expected.ExecMainCode)
			}
		})
	}
}

func TestParseSystemdExecStart(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "/usr/bin/nexus serve",
			expected: []string{"/usr/bin/nexus", "serve"},
		},
		{
			input:    `"/path with spaces/nexus" serve`,
			expected: []string{"/path with spaces/nexus", "serve"},
		},
		{
			input:    `"/path" "arg with spaces"`,
			expected: []string{"/path", "arg with spaces"},
		},
		{
			input:    `/usr/bin/nexus --config "/etc/nexus/config.yaml"`,
			expected: []string{"/usr/bin/nexus", "--config", "/etc/nexus/config.yaml"},
		},
		{
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseSystemdExecStart(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("ParseSystemdExecStart(%q) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i, want := range tt.expected {
				if result[i] != want {
					t.Errorf("ParseSystemdExecStart(%q)[%d] = %q, want %q", tt.input, i, result[i], want)
				}
			}
		})
	}
}

func TestParseSystemdEnvAssignment(t *testing.T) {
	tests := []struct {
		input     string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{
			input:     "FOO=bar",
			wantKey:   "FOO",
			wantValue: "bar",
			wantOK:    true,
		},
		{
			input:     `"FOO=bar with spaces"`,
			wantKey:   "FOO",
			wantValue: "bar with spaces",
			wantOK:    true,
		},
		{
			input:     `"PATH=/usr/bin:/usr/local/bin"`,
			wantKey:   "PATH",
			wantValue: "/usr/bin:/usr/local/bin",
			wantOK:    true,
		},
		{
			input:  "",
			wantOK: false,
		},
		{
			input:  "NOEQUALS",
			wantOK: false,
		},
		{
			input:  "=value",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			key, value, ok := ParseSystemdEnvAssignment(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ParseSystemdEnvAssignment(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				return
			}
			if ok {
				if key != tt.wantKey {
					t.Errorf("ParseSystemdEnvAssignment(%q) key = %q, want %q", tt.input, key, tt.wantKey)
				}
				if value != tt.wantValue {
					t.Errorf("ParseSystemdEnvAssignment(%q) value = %q, want %q", tt.input, value, tt.wantValue)
				}
			}
		})
	}
}

func TestSystemdManagerInterface(t *testing.T) {
	// Verify SystemdManager implements ServiceManager
	var _ ServiceManager = (*SystemdManager)(nil)

	manager := &SystemdManager{}
	if manager.Label() != "systemd" {
		t.Errorf("Label() = %q, want %q", manager.Label(), "systemd")
	}
}
