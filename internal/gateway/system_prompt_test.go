package gateway

import (
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestBuildSystemPrompt(t *testing.T) {
	cfg := &config.Config{
		Identity: config.IdentityConfig{
			Name:     "Clawd",
			Creature: "owl",
			Vibe:     "curious",
			Emoji:    "owl",
		},
		User: config.UserConfig{
			PreferredAddress: "Haas",
			Pronouns:         "he/him",
			Timezone:         "America/Denver",
			Notes:            "likes concise answers",
		},
	}

	prompt := buildSystemPrompt(cfg, SystemPromptOptions{})
	if !strings.Contains(prompt, "Clawd") || !strings.Contains(prompt, "owl") {
		t.Fatalf("expected identity in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Haas") || !strings.Contains(prompt, "he/him") {
		t.Fatalf("expected user info in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "concise") {
		t.Fatalf("expected guidance in prompt, got %q", prompt)
	}
}

func TestBuildSystemPromptAddsBootstrapGuidance(t *testing.T) {
	cfg := &config.Config{}

	prompt := buildSystemPrompt(cfg, SystemPromptOptions{})
	if !strings.Contains(prompt, "ask the user") {
		t.Fatalf("expected bootstrap guidance, got %q", prompt)
	}
}

func TestBuildSystemPromptIncludesToolNotes(t *testing.T) {
	cfg := &config.Config{}

	prompt := buildSystemPrompt(cfg, SystemPromptOptions{
		ToolNotes: "imsg: confirm before sending\nsag: ask for voice",
	})
	if !strings.Contains(prompt, "Tool notes:") {
		t.Fatalf("expected tool notes header, got %q", prompt)
	}
	if !strings.Contains(prompt, "confirm before sending") {
		t.Fatalf("expected tool notes content, got %q", prompt)
	}
}

func TestBuildSystemPromptIncludesMemoryLines(t *testing.T) {
	cfg := &config.Config{}

	prompt := buildSystemPrompt(cfg, SystemPromptOptions{
		MemoryLines: []string{"- [12:00:00] user (slack/session-1): hello"},
	})
	if !strings.Contains(prompt, "Recent memory:") {
		t.Fatalf("expected memory header, got %q", prompt)
	}
	if !strings.Contains(prompt, "session-1") {
		t.Fatalf("expected memory line, got %q", prompt)
	}
}

func TestBuildSystemPromptIncludesHeartbeat(t *testing.T) {
	cfg := &config.Config{}

	prompt := buildSystemPrompt(cfg, SystemPromptOptions{
		Heartbeat: "- Check alerts\n- Review backlog",
	})
	if !strings.Contains(prompt, "Heartbeat checklist") {
		t.Fatalf("expected heartbeat header, got %q", prompt)
	}
	if !strings.Contains(prompt, "Review backlog") {
		t.Fatalf("expected heartbeat content, got %q", prompt)
	}
}
