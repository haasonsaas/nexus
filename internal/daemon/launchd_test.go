package daemon

import (
	"strings"
	"testing"
)

func TestResolveLaunchdLabel(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name:     "default label",
			env:      map[string]string{},
			expected: DefaultLaunchdLabel,
		},
		{
			name: "override with env var",
			env: map[string]string{
				EnvNexusLaunchdLabel: "com.custom.label",
			},
			expected: "com.custom.label",
		},
		{
			name: "profile-specific label",
			env: map[string]string{
				EnvNexusProfile: "prod",
			},
			expected: "com.haasonsaas.nexus.prod",
		},
		{
			name: "env var takes precedence over profile",
			env: map[string]string{
				EnvNexusProfile:      "prod",
				EnvNexusLaunchdLabel: "com.override.label",
			},
			expected: "com.override.label",
		},
		{
			name: "whitespace trimmed",
			env: map[string]string{
				EnvNexusLaunchdLabel: "  com.trimmed.label  ",
			},
			expected: "com.trimmed.label",
		},
		{
			name: "default profile ignored",
			env: map[string]string{
				EnvNexusProfile: "default",
			},
			expected: DefaultLaunchdLabel,
		},
		{
			name: "Default profile ignored (case insensitive)",
			env: map[string]string{
				EnvNexusProfile: "Default",
			},
			expected: DefaultLaunchdLabel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveLaunchdLabel(tt.env)
			if result != tt.expected {
				t.Errorf("resolveLaunchdLabel() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestResolveLaunchdPlistPath(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantSuffix  string
		wantContain string
	}{
		{
			name: "default path",
			env: map[string]string{
				"HOME": "/Users/test",
			},
			wantSuffix:  ".plist",
			wantContain: "Library/LaunchAgents",
		},
		{
			name: "with profile",
			env: map[string]string{
				"HOME":          "/Users/test",
				EnvNexusProfile: "dev",
			},
			wantContain: "com.haasonsaas.nexus.dev.plist",
		},
		{
			name:        "no home uses dot",
			env:         map[string]string{},
			wantContain: "Library/LaunchAgents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveLaunchdPlistPath(tt.env)
			if !strings.HasSuffix(result, tt.wantSuffix) && tt.wantSuffix != "" {
				t.Errorf("resolveLaunchdPlistPath() = %q, want suffix %q", result, tt.wantSuffix)
			}
			if !strings.Contains(result, tt.wantContain) {
				t.Errorf("resolveLaunchdPlistPath() = %q, want contain %q", result, tt.wantContain)
			}
		})
	}
}

func TestBuildLaunchAgentPlist(t *testing.T) {
	tests := []struct {
		name string
		opts struct {
			Label            string
			Comment          string
			ProgramArguments []string
			WorkingDirectory string
			StdoutPath       string
			StderrPath       string
			Environment      map[string]string
		}
		wantContains []string
	}{
		{
			name: "basic plist",
			opts: struct {
				Label            string
				Comment          string
				ProgramArguments []string
				WorkingDirectory string
				StdoutPath       string
				StderrPath       string
				Environment      map[string]string
			}{
				Label:            "com.test.label",
				ProgramArguments: []string{"/usr/bin/test", "--arg"},
				StdoutPath:       "/tmp/stdout.log",
				StderrPath:       "/tmp/stderr.log",
			},
			wantContains: []string{
				"<key>Label</key>",
				"<string>com.test.label</string>",
				"<key>RunAtLoad</key>",
				"<true/>",
				"<key>KeepAlive</key>",
				"<key>ProgramArguments</key>",
				"<string>/usr/bin/test</string>",
				"<string>--arg</string>",
				"<key>StandardOutPath</key>",
				"<key>StandardErrorPath</key>",
			},
		},
		{
			name: "with comment",
			opts: struct {
				Label            string
				Comment          string
				ProgramArguments []string
				WorkingDirectory string
				StdoutPath       string
				StderrPath       string
				Environment      map[string]string
			}{
				Label:            "com.test.label",
				Comment:          "Test Service v1.0",
				ProgramArguments: []string{"/usr/bin/test"},
				StdoutPath:       "/tmp/stdout.log",
				StderrPath:       "/tmp/stderr.log",
			},
			wantContains: []string{
				"<key>Comment</key>",
				"<string>Test Service v1.0</string>",
			},
		},
		{
			name: "with working directory",
			opts: struct {
				Label            string
				Comment          string
				ProgramArguments []string
				WorkingDirectory string
				StdoutPath       string
				StderrPath       string
				Environment      map[string]string
			}{
				Label:            "com.test.label",
				ProgramArguments: []string{"/usr/bin/test"},
				WorkingDirectory: "/var/lib/test",
				StdoutPath:       "/tmp/stdout.log",
				StderrPath:       "/tmp/stderr.log",
			},
			wantContains: []string{
				"<key>WorkingDirectory</key>",
				"<string>/var/lib/test</string>",
			},
		},
		{
			name: "with environment variables",
			opts: struct {
				Label            string
				Comment          string
				ProgramArguments []string
				WorkingDirectory string
				StdoutPath       string
				StderrPath       string
				Environment      map[string]string
			}{
				Label:            "com.test.label",
				ProgramArguments: []string{"/usr/bin/test"},
				StdoutPath:       "/tmp/stdout.log",
				StderrPath:       "/tmp/stderr.log",
				Environment: map[string]string{
					"FOO": "bar",
					"BAZ": "qux",
				},
			},
			wantContains: []string{
				"<key>EnvironmentVariables</key>",
				"<key>FOO</key>",
				"<string>bar</string>",
			},
		},
		{
			name: "escapes special characters",
			opts: struct {
				Label            string
				Comment          string
				ProgramArguments []string
				WorkingDirectory string
				StdoutPath       string
				StderrPath       string
				Environment      map[string]string
			}{
				Label:            "com.test.label",
				ProgramArguments: []string{"/usr/bin/test", "--arg=<value>"},
				StdoutPath:       "/tmp/stdout.log",
				StderrPath:       "/tmp/stderr.log",
			},
			wantContains: []string{
				"&lt;value&gt;",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildLaunchAgentPlist(tt.opts)
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("BuildLaunchAgentPlist() missing %q in:\n%s", want, result)
				}
			}
		})
	}
}

func TestPlistEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"<tag>", "&lt;tag&gt;"},
		{"a & b", "a &amp; b"},
		{"\"quoted\"", "&quot;quoted&quot;"},
		{"it's", "it&apos;s"},
		{"<>&\"'", "&lt;&gt;&amp;&quot;&apos;"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := plistEscape(tt.input)
			if result != tt.expected {
				t.Errorf("plistEscape(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseLaunchctlPrint(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected LaunchctlPrintInfo
	}{
		{
			name: "running state",
			output: `state = running
pid = 4242
last exit status = 0
`,
			expected: LaunchctlPrintInfo{
				State:          "running",
				PID:            4242,
				LastExitStatus: 0,
			},
		},
		{
			name: "stopped with exit info",
			output: `state = not running
last exit status = 1
last exit reason = exited
`,
			expected: LaunchctlPrintInfo{
				State:          "not running",
				LastExitStatus: 1,
				LastExitReason: "exited",
			},
		},
		{
			name: "mixed case",
			output: `State = Running
PID = 1234
`,
			expected: LaunchctlPrintInfo{
				State: "Running",
				PID:   1234,
			},
		},
		{
			name:     "empty output",
			output:   "",
			expected: LaunchctlPrintInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLaunchctlPrint(tt.output)
			if result.State != tt.expected.State {
				t.Errorf("State = %q, want %q", result.State, tt.expected.State)
			}
			if result.PID != tt.expected.PID {
				t.Errorf("PID = %d, want %d", result.PID, tt.expected.PID)
			}
			if result.LastExitStatus != tt.expected.LastExitStatus {
				t.Errorf("LastExitStatus = %d, want %d", result.LastExitStatus, tt.expected.LastExitStatus)
			}
			if result.LastExitReason != tt.expected.LastExitReason {
				t.Errorf("LastExitReason = %q, want %q", result.LastExitReason, tt.expected.LastExitReason)
			}
		})
	}
}

func TestIsLaunchctlNotLoaded(t *testing.T) {
	tests := []struct {
		output   string
		expected bool
	}{
		{"No such process", true},
		{"Could not find service", true},
		{"Service not found", true},
		{"no such process", true},
		{"Error: Something else happened", false},
		{"Running successfully", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			result := isLaunchctlNotLoaded(tt.output)
			if result != tt.expected {
				t.Errorf("isLaunchctlNotLoaded(%q) = %v, want %v", tt.output, result, tt.expected)
			}
		})
	}
}

func TestResolveLogPaths(t *testing.T) {
	tests := []struct {
		name           string
		env            map[string]string
		wantLogDir     string
		wantStdoutName string
		wantStderrName string
	}{
		{
			name: "default paths",
			env: map[string]string{
				"HOME": "/Users/test",
			},
			wantLogDir:     "/Users/test/.nexus/logs",
			wantStdoutName: "gateway.log",
			wantStderrName: "gateway.err.log",
		},
		{
			name: "custom log prefix",
			env: map[string]string{
				"HOME":            "/Users/test",
				EnvNexusLogPrefix: "custom",
			},
			wantLogDir:     "/Users/test/.nexus/logs",
			wantStdoutName: "custom.log",
			wantStderrName: "custom.err.log",
		},
		{
			name: "custom state dir",
			env: map[string]string{
				"HOME":           "/Users/test",
				EnvNexusStateDir: "/custom/state",
			},
			wantLogDir: "/custom/state/logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logDir, stdoutPath, stderrPath := resolveLogPaths(tt.env)
			if logDir != tt.wantLogDir {
				t.Errorf("logDir = %q, want %q", logDir, tt.wantLogDir)
			}
			if tt.wantStdoutName != "" && !strings.HasSuffix(stdoutPath, tt.wantStdoutName) {
				t.Errorf("stdoutPath = %q, want suffix %q", stdoutPath, tt.wantStdoutName)
			}
			if tt.wantStderrName != "" && !strings.HasSuffix(stderrPath, tt.wantStderrName) {
				t.Errorf("stderrPath = %q, want suffix %q", stderrPath, tt.wantStderrName)
			}
		})
	}
}

func TestLaunchdManagerInterface(t *testing.T) {
	// Verify LaunchdManager implements ServiceManager
	var _ ServiceManager = (*LaunchdManager)(nil)

	manager := &LaunchdManager{}
	if manager.Label() != "LaunchAgent" {
		t.Errorf("Label() = %q, want %q", manager.Label(), "LaunchAgent")
	}
}
