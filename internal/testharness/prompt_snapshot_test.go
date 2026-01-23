package testharness_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/gateway"
	"github.com/haasonsaas/nexus/internal/testharness"
	"github.com/haasonsaas/nexus/pkg/models"
)

// TestPromptComposition_Minimal tests prompt generation with minimal config.
func TestPromptComposition_Minimal(t *testing.T) {
	cfg := &config.Config{}

	prompt, err := gateway.BuildSystemPrompt(cfg, "session-1", &models.Message{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/prompts")
	g.Assert(prompt)
}

// TestPromptComposition_FullIdentity tests prompt with complete identity and user.
func TestPromptComposition_FullIdentity(t *testing.T) {
	cfg := &config.Config{
		Identity: config.IdentityConfig{
			Name:     "Clawd",
			Creature: "owl",
			Vibe:     "curious and helpful",
			Emoji:    "🦉",
		},
		User: config.UserConfig{
			Name:             "Alex",
			PreferredAddress: "Alex",
			Pronouns:         "they/them",
			Timezone:         "America/Denver",
			Notes:            "prefers concise answers",
		},
	}

	prompt, err := gateway.BuildSystemPrompt(cfg, "session-1", &models.Message{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/prompts")
	g.Assert(prompt)
}

// TestPromptComposition_WithWorkspace tests prompt with SOUL, IDENTITY, MEMORY files.
func TestPromptComposition_WithWorkspace(t *testing.T) {
	dir := t.TempDir()

	// Create workspace files
	files := map[string]string{
		"SOUL.md": `# Persona
You are Clawd, a wise and thoughtful assistant.

## Boundaries
- Always be helpful and honest
- Never make things up
- Ask clarifying questions when needed`,
		"IDENTITY.md": `# Identity
Name: Clawd
Role: Assistant
Style: Conversational`,
		"MEMORY.md": `# Long-term Memory
- User prefers brief answers
- Project deadline is next Friday
- User timezone is America/Denver`,
		"AGENTS.md": `# Workspace Instructions
Follow the team's coding standards.
Use TypeScript for all new code.`,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Enabled:      true,
			Path:         dir,
			MaxChars:     2000,
			AgentsFile:   "AGENTS.md",
			SoulFile:     "SOUL.md",
			IdentityFile: "IDENTITY.md",
			MemoryFile:   "MEMORY.md",
		},
	}

	prompt, err := gateway.BuildSystemPrompt(cfg, "session-1", &models.Message{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/prompts")
	g.Assert(prompt)
}

// TestPromptComposition_WithToolNotes tests prompt with tool notes.
func TestPromptComposition_WithToolNotes(t *testing.T) {
	dir := t.TempDir()
	toolNotesPath := filepath.Join(dir, "tool-notes.md")
	toolNotes := `# Tool Notes
- imsg: Always confirm before sending iMessages
- slack: Use threads for long conversations
- exec: Avoid running commands that modify system state without approval`
	if err := os.WriteFile(toolNotesPath, []byte(toolNotes), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			NotesFile: toolNotesPath,
		},
	}

	prompt, err := gateway.BuildSystemPrompt(cfg, "session-1", &models.Message{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/prompts")
	g.Assert(prompt)
}

// TestPromptComposition_WithHeartbeat tests prompt with heartbeat enabled.
func TestPromptComposition_WithHeartbeat(t *testing.T) {
	dir := t.TempDir()
	heartbeatPath := filepath.Join(dir, "heartbeat.md")
	heartbeat := `- Check for new Slack messages
- Review open PRs
- Check build status`
	if err := os.WriteFile(heartbeatPath, []byte(heartbeat), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Session: config.SessionConfig{
			Heartbeat: config.HeartbeatConfig{
				Enabled: true,
				Mode:    "always",
				File:    heartbeatPath,
			},
		},
	}

	prompt, err := gateway.BuildSystemPrompt(cfg, "session-1", &models.Message{
		Content: "heartbeat",
		Metadata: map[string]any{
			"heartbeat": true,
		},
	})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/prompts")
	g.Assert(prompt)
}

// TestPromptComposition_Combined tests a fully configured prompt.
func TestPromptComposition_Combined(t *testing.T) {
	dir := t.TempDir()

	// Create all workspace files
	files := map[string]string{
		"SOUL.md":      "Be helpful and concise.",
		"MEMORY.md":    "User preference: dark mode enabled.",
		"tool-notes.md": "exec: require confirmation for destructive commands",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	cfg := &config.Config{
		Identity: config.IdentityConfig{
			Name: "Clawd",
			Vibe: "helpful",
		},
		User: config.UserConfig{
			PreferredAddress: "Developer",
			Timezone:         "UTC",
		},
		Workspace: config.WorkspaceConfig{
			Enabled:    true,
			Path:       dir,
			MaxChars:   1000,
			SoulFile:   "SOUL.md",
			MemoryFile: "MEMORY.md",
		},
		Tools: config.ToolsConfig{
			NotesFile: filepath.Join(dir, "tool-notes.md"),
		},
	}

	prompt, err := gateway.BuildSystemPrompt(cfg, "session-1", &models.Message{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/prompts")
	g.Assert(prompt)
}
