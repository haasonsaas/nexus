package daemon

import (
	"strings"
	"testing"
)

func TestResolveWindowsTaskName(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name:     "default name",
			env:      map[string]string{},
			expected: DefaultWindowsTaskName,
		},
		{
			name: "override with env var",
			env: map[string]string{
				EnvNexusWindowsTask: "Custom Task",
			},
			expected: "Custom Task",
		},
		{
			name: "profile-specific name",
			env: map[string]string{
				EnvNexusProfile: "prod",
			},
			expected: "Nexus Gateway (prod)",
		},
		{
			name: "env var takes precedence over profile",
			env: map[string]string{
				EnvNexusProfile:     "prod",
				EnvNexusWindowsTask: "Override Task",
			},
			expected: "Override Task",
		},
		{
			name: "whitespace trimmed",
			env: map[string]string{
				EnvNexusWindowsTask: "  Trimmed Task  ",
			},
			expected: "Trimmed Task",
		},
		{
			name: "default profile ignored",
			env: map[string]string{
				EnvNexusProfile: "default",
			},
			expected: DefaultWindowsTaskName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveWindowsTaskName(tt.env)
			if result != tt.expected {
				t.Errorf("resolveWindowsTaskName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestResolveTaskScriptPath(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantSuffix  string
		wantContain string
	}{
		{
			name: "default path",
			env: map[string]string{
				"HOME": "C:\\Users\\test",
			},
			wantSuffix: "gateway.cmd",
		},
		{
			name: "custom script name",
			env: map[string]string{
				"HOME":                    "C:\\Users\\test",
				"NEXUS_TASK_SCRIPT_NAME": "custom.cmd",
			},
			wantSuffix: "custom.cmd",
		},
		{
			name: "override path",
			env: map[string]string{
				"NEXUS_TASK_SCRIPT": "C:\\custom\\path\\script.cmd",
			},
			wantContain: "C:\\custom\\path\\script.cmd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveTaskScriptPath(tt.env)
			if tt.wantSuffix != "" && !strings.HasSuffix(result, tt.wantSuffix) {
				t.Errorf("resolveTaskScriptPath() = %q, want suffix %q", result, tt.wantSuffix)
			}
			if tt.wantContain != "" && result != tt.wantContain {
				t.Errorf("resolveTaskScriptPath() = %q, want %q", result, tt.wantContain)
			}
		})
	}
}

func TestResolveTaskUser(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name:     "empty env",
			env:      map[string]string{},
			expected: "",
		},
		{
			name: "USERNAME only",
			env: map[string]string{
				"USERNAME": "testuser",
			},
			expected: "testuser",
		},
		{
			name: "USERNAME with domain",
			env: map[string]string{
				"USERNAME":   "testuser",
				"USERDOMAIN": "MYDOMAIN",
			},
			expected: "MYDOMAIN\\testuser",
		},
		{
			name: "already qualified username",
			env: map[string]string{
				"USERNAME":   "DOMAIN\\user",
				"USERDOMAIN": "OTHER",
			},
			expected: "DOMAIN\\user",
		},
		{
			name: "USER fallback",
			env: map[string]string{
				"USER": "unixuser",
			},
			expected: "unixuser",
		},
		{
			name: "LOGNAME fallback",
			env: map[string]string{
				"LOGNAME": "loguser",
			},
			expected: "loguser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveTaskUser(tt.env)
			if result != tt.expected {
				t.Errorf("resolveTaskUser() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestQuoteCmdArg(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"path\\to\\file", "path\\to\\file"},
		{"path with spaces", `"path with spaces"`},
		{`path"with"quotes`, `"path\"with\"quotes"`},
		{"path\twith\ttabs", `"path	with	tabs"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := quoteCmdArg(tt.input)
			if result != tt.expected {
				t.Errorf("quoteCmdArg(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildTaskScript(t *testing.T) {
	tests := []struct {
		name         string
		opts         struct {
			Description      string
			ProgramArguments []string
			WorkingDirectory string
			Environment      map[string]string
		}
		wantContains []string
	}{
		{
			name: "basic script",
			opts: struct {
				Description      string
				ProgramArguments []string
				WorkingDirectory string
				Environment      map[string]string
			}{
				ProgramArguments: []string{"C:\\Program Files\\nexus\\nexus.exe", "serve"},
			},
			wantContains: []string{
				"@echo off",
				`"C:\Program Files\nexus\nexus.exe" serve`,
			},
		},
		{
			name: "with description",
			opts: struct {
				Description      string
				ProgramArguments []string
				WorkingDirectory string
				Environment      map[string]string
			}{
				Description:      "Nexus Gateway Service",
				ProgramArguments: []string{"nexus.exe", "serve"},
			},
			wantContains: []string{
				"rem Nexus Gateway Service",
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
				ProgramArguments: []string{"nexus.exe", "serve"},
				WorkingDirectory: "C:\\Program Files\\nexus",
			},
			wantContains: []string{
				`cd /d "C:\Program Files\nexus"`,
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
				ProgramArguments: []string{"nexus.exe", "serve"},
				Environment: map[string]string{
					"FOO": "bar",
				},
			},
			wantContains: []string{
				"set FOO=bar",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildTaskScript(tt.opts)
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("BuildTaskScript() missing %q in:\n%s", want, result)
				}
			}
			// Check line endings are CRLF
			if !strings.Contains(result, "\r\n") {
				t.Error("BuildTaskScript() should use CRLF line endings")
			}
		})
	}
}

func TestParseSchtasksQuery(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected SchtasksQueryInfo
	}{
		{
			name: "running status",
			output: `Status: Running
Last Run Time: 1/24/2026 10:00:00 AM
Last Run Result: 0
`,
			expected: SchtasksQueryInfo{
				Status:        "Running",
				LastRunTime:   "1/24/2026 10:00:00 AM",
				LastRunResult: "0",
			},
		},
		{
			name: "ready status",
			output: `Status: Ready
Last Run Time: N/A
Last Run Result: 267011
`,
			expected: SchtasksQueryInfo{
				Status:        "Ready",
				LastRunTime:   "N/A",
				LastRunResult: "267011",
			},
		},
		{
			name:     "empty output",
			output:   "",
			expected: SchtasksQueryInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSchtasksQuery(tt.output)
			if result.Status != tt.expected.Status {
				t.Errorf("Status = %q, want %q", result.Status, tt.expected.Status)
			}
			if result.LastRunTime != tt.expected.LastRunTime {
				t.Errorf("LastRunTime = %q, want %q", result.LastRunTime, tt.expected.LastRunTime)
			}
			if result.LastRunResult != tt.expected.LastRunResult {
				t.Errorf("LastRunResult = %q, want %q", result.LastRunResult, tt.expected.LastRunResult)
			}
		})
	}
}

func TestIsTaskNotRunning(t *testing.T) {
	tests := []struct {
		output   string
		expected bool
	}{
		{"The task is not running", true},
		{"Task not running", true},
		{"NOT RUNNING", true},
		{"Error: Something else happened", false},
		{"Running successfully", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			result := isTaskNotRunning(tt.output)
			if result != tt.expected {
				t.Errorf("isTaskNotRunning(%q) = %v, want %v", tt.output, result, tt.expected)
			}
		})
	}
}

func TestParseWindowsCommandLine(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "nexus.exe serve",
			expected: []string{"nexus.exe", "serve"},
		},
		{
			input:    `"C:\Program Files\nexus\nexus.exe" serve`,
			expected: []string{"C:\\Program Files\\nexus\\nexus.exe", "serve"},
		},
		{
			input:    `"C:\path" "arg with spaces"`,
			expected: []string{"C:\\path", "arg with spaces"},
		},
		{
			input:    `nexus.exe --config "C:\config\nexus.yaml"`,
			expected: []string{"nexus.exe", "--config", "C:\\config\\nexus.yaml"},
		},
		{
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseWindowsCommandLine(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("parseWindowsCommandLine(%q) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i, want := range tt.expected {
				if result[i] != want {
					t.Errorf("parseWindowsCommandLine(%q)[%d] = %q, want %q", tt.input, i, result[i], want)
				}
			}
		})
	}
}

func TestSchtasksManagerInterface(t *testing.T) {
	// Verify SchtasksManager implements ServiceManager
	var _ ServiceManager = (*SchtasksManager)(nil)

	manager := &SchtasksManager{}
	if manager.Label() != "Scheduled Task" {
		t.Errorf("Label() = %q, want %q", manager.Label(), "Scheduled Task")
	}
}
