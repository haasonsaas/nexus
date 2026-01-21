package service

import (
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

func containsAll(content string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(content, needle) {
			return false
		}
	}
	return true
}
