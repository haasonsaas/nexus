package service

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateSystemdUnit(t *testing.T) {
	content := GenerateSystemdUnit("/usr/local/bin/nexus", "/etc/nexus.yaml")
	if !containsAll(content, []string{"ExecStart=/usr/local/bin/nexus serve --config /etc/nexus.yaml", "Restart=on-failure"}) {
		t.Fatalf("expected systemd unit content, got %q", content)
	}
}

func TestGenerateLaunchdPlist(t *testing.T) {
	content := GenerateLaunchdPlist("/usr/local/bin/nexus", "/etc/nexus.yaml")
	if !containsAll(content, []string{"ProgramArguments", "/usr/local/bin/nexus", "--config", "/etc/nexus.yaml"}) {
		t.Fatalf("expected launchd plist content, got %q", content)
	}
}

func TestRestartUserServiceCommands(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "darwin":
	default:
		t.Skip("restart not supported on this platform")
	}

	origRunner := commandRunner
	t.Cleanup(func() { commandRunner = origRunner })

	var calls []string
	commandRunner = func(ctx context.Context, name string, args ...string) error {
		calls = append(calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
		return nil
	}

	steps, err := RestartUserService(context.Background())
	if err != nil {
		t.Fatalf("RestartUserService() error = %v", err)
	}
	if len(steps) == 0 {
		t.Fatalf("expected restart steps")
	}
	if len(calls) != len(steps) {
		t.Fatalf("expected %d command calls, got %d", len(steps), len(calls))
	}
	if runtime.GOOS == "linux" {
		expected := []string{"systemctl --user daemon-reload", "systemctl --user restart nexus"}
		if !containsAll(strings.Join(calls, " "), expected) {
			t.Fatalf("expected systemctl calls, got %v", calls)
		}
	}
	if runtime.GOOS == "darwin" {
		if !strings.Contains(strings.Join(calls, " "), "launchctl unload") {
			t.Fatalf("expected launchctl unload, got %v", calls)
		}
		if !strings.Contains(strings.Join(calls, " "), "launchctl load -w") {
			t.Fatalf("expected launchctl load, got %v", calls)
		}
	}
}

func containsAll(content string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(content, needle) {
			return false
		}
	}
	return true
}

func TestNormalizeConfigPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "nexus.yaml"},
		{"  ", "nexus.yaml"},
		{"custom.yaml", "custom.yaml"},
		{"/etc/nexus/config.yaml", "/etc/nexus/config.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeConfigPath(tt.input); got != tt.expected {
				t.Errorf("normalizeConfigPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestInstallResult(t *testing.T) {
	result := InstallResult{
		Path:         "/path/to/service",
		Instructions: []string{"step 1", "step 2"},
	}

	if result.Path != "/path/to/service" {
		t.Errorf("Path = %q, want %q", result.Path, "/path/to/service")
	}
	if len(result.Instructions) != 2 {
		t.Errorf("Instructions length = %d, want 2", len(result.Instructions))
	}
}

func TestUserHomeDir(t *testing.T) {
	home := userHomeDir()
	// Should return something valid (either actual home or ".")
	if home == "" {
		t.Error("userHomeDir() returned empty string")
	}
}

func TestInstallUserService_UnsupportedOS(t *testing.T) {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		t.Skip("skipping unsupported OS test on supported platform")
	}

	_, err := InstallUserService("config.yaml", false)
	if err == nil {
		t.Error("expected error for unsupported OS")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %v, want 'not supported' message", err)
	}
}

func TestRestartUserService_UnsupportedOS(t *testing.T) {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		t.Skip("skipping unsupported OS test on supported platform")
	}

	_, err := RestartUserService(context.Background())
	if err == nil {
		t.Error("expected error for unsupported OS")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %v, want 'not supported' message", err)
	}
}

func TestConstants(t *testing.T) {
	if SystemdUnitName != "nexus.service" {
		t.Errorf("SystemdUnitName = %q, want %q", SystemdUnitName, "nexus.service")
	}
	if LaunchdLabel != "com.haasonsaas.nexus" {
		t.Errorf("LaunchdLabel = %q, want %q", LaunchdLabel, "com.haasonsaas.nexus")
	}
}

func TestGenerateSystemdUnit_Content(t *testing.T) {
	content := GenerateSystemdUnit("/custom/path/nexus", "/custom/config.yaml")

	// Verify all required sections
	if !strings.Contains(content, "[Unit]") {
		t.Error("missing [Unit] section")
	}
	if !strings.Contains(content, "[Service]") {
		t.Error("missing [Service] section")
	}
	if !strings.Contains(content, "[Install]") {
		t.Error("missing [Install] section")
	}
	if !strings.Contains(content, "Description=Nexus Gateway") {
		t.Error("missing Description")
	}
	if !strings.Contains(content, "After=network.target") {
		t.Error("missing After=network.target")
	}
	if !strings.Contains(content, "ExecStart=/custom/path/nexus serve --config /custom/config.yaml") {
		t.Error("missing ExecStart with correct paths")
	}
	if !strings.Contains(content, "RestartSec=3") {
		t.Error("missing RestartSec=3")
	}
	if !strings.Contains(content, "WantedBy=default.target") {
		t.Error("missing WantedBy")
	}
}

func TestGenerateLaunchdPlist_Content(t *testing.T) {
	content := GenerateLaunchdPlist("/custom/path/nexus", "/custom/config.yaml")

	// Verify XML structure
	if !strings.Contains(content, `<?xml version="1.0"`) {
		t.Error("missing XML declaration")
	}
	if !strings.Contains(content, "<!DOCTYPE plist") {
		t.Error("missing DOCTYPE")
	}
	if !strings.Contains(content, `<plist version="1.0">`) {
		t.Error("missing plist version")
	}
	if !strings.Contains(content, "<key>Label</key>") {
		t.Error("missing Label key")
	}
	if !strings.Contains(content, "<string>com.haasonsaas.nexus</string>") {
		t.Error("missing correct Label value")
	}
	if !strings.Contains(content, "<key>RunAtLoad</key>") {
		t.Error("missing RunAtLoad")
	}
	if !strings.Contains(content, "<true/>") {
		t.Error("missing true values")
	}
	if !strings.Contains(content, "<key>KeepAlive</key>") {
		t.Error("missing KeepAlive")
	}
	if !strings.Contains(content, "<string>/custom/path/nexus</string>") {
		t.Error("missing exec path in arguments")
	}
	if !strings.Contains(content, "<string>/custom/config.yaml</string>") {
		t.Error("missing config path in arguments")
	}
	if !strings.Contains(content, "<string>serve</string>") {
		t.Error("missing serve command")
	}
}

func TestRestartUserService_CommandError(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("restart not supported on this platform")
	}

	origRunner := commandRunner
	t.Cleanup(func() { commandRunner = origRunner })

	// Make the first command fail
	commandRunner = func(ctx context.Context, name string, args ...string) error {
		return context.DeadlineExceeded
	}

	_, err := RestartUserService(context.Background())
	if err == nil {
		t.Fatal("expected error when command fails")
	}
}

func TestContainsAll(t *testing.T) {
	tests := []struct {
		content  string
		needles  []string
		expected bool
	}{
		{"hello world", []string{"hello", "world"}, true},
		{"hello world", []string{"hello", "foo"}, false},
		{"", []string{}, true},
		{"hello", []string{}, true},
		{"", []string{"hello"}, false},
	}

	for _, tt := range tests {
		result := containsAll(tt.content, tt.needles)
		if result != tt.expected {
			t.Errorf("containsAll(%q, %v) = %v, want %v", tt.content, tt.needles, result, tt.expected)
		}
	}
}

func TestInstallUserService_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	// Create temp directory to act as XDG_CONFIG_HOME
	tmpDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Cleanup(func() {
		if originalXDG == "" {
			os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			os.Setenv("XDG_CONFIG_HOME", originalXDG)
		}
	})

	result, err := InstallUserService("test-config.yaml", true)
	if err != nil {
		t.Fatalf("InstallUserService() error = %v", err)
	}

	// Verify file was created
	if result.Path == "" {
		t.Error("expected Path to be set")
	}
	if !strings.Contains(result.Path, "systemd") {
		t.Errorf("Path %q should contain 'systemd'", result.Path)
	}

	// Verify instructions
	if len(result.Instructions) == 0 {
		t.Error("expected Instructions to be set")
	}

	// Verify file exists and has correct content
	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("failed to read service file: %v", err)
	}
	if !strings.Contains(string(content), "ExecStart=") {
		t.Error("service file should contain ExecStart")
	}
	if !strings.Contains(string(content), "test-config.yaml") {
		t.Error("service file should contain config path")
	}
}

func TestInstallUserService_NoOverwrite(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	tmpDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Cleanup(func() {
		if originalXDG == "" {
			os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			os.Setenv("XDG_CONFIG_HOME", originalXDG)
		}
	})

	// First install
	result1, err := InstallUserService("first-config.yaml", false)
	if err != nil {
		t.Fatalf("first InstallUserService() error = %v", err)
	}

	// Second install without overwrite - should return same path without modification
	result2, err := InstallUserService("second-config.yaml", false)
	if err != nil {
		t.Fatalf("second InstallUserService() error = %v", err)
	}

	if result1.Path != result2.Path {
		t.Errorf("paths should match: %q != %q", result1.Path, result2.Path)
	}

	// Verify file still has first config
	content, err := os.ReadFile(result2.Path)
	if err != nil {
		t.Fatalf("failed to read service file: %v", err)
	}
	if !strings.Contains(string(content), "first-config.yaml") {
		t.Error("file should still contain first config path")
	}
}

func TestInstallUserService_Overwrite(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-specific test")
	}

	tmpDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Cleanup(func() {
		if originalXDG == "" {
			os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			os.Setenv("XDG_CONFIG_HOME", originalXDG)
		}
	})

	// First install
	_, err := InstallUserService("first-config.yaml", false)
	if err != nil {
		t.Fatalf("first InstallUserService() error = %v", err)
	}

	// Second install with overwrite
	result2, err := InstallUserService("second-config.yaml", true)
	if err != nil {
		t.Fatalf("second InstallUserService() error = %v", err)
	}

	// Verify file has second config
	content, err := os.ReadFile(result2.Path)
	if err != nil {
		t.Fatalf("failed to read service file: %v", err)
	}
	if !strings.Contains(string(content), "second-config.yaml") {
		t.Error("file should contain second config path after overwrite")
	}
}
