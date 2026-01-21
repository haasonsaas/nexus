package service

import (
	"context"
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
