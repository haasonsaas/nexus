package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `
server:
  host: 0.0.0.0
  extra: true
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	if _, err := Load(path); err == nil {
		t.Fatalf("expected error for unknown field")
	}
}

func TestLoadValidatesSessionScope(t *testing.T) {
	path := writeConfig(t, `
session:
  slack_scope: nope
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "slack_scope") {
		t.Fatalf("expected slack_scope error, got %v", err)
	}
}

func TestLoadValidatesDefaultProvider(t *testing.T) {
	path := writeConfig(t, `
llm:
  default_provider: openai
  providers:
    anthropic: {}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "default_provider") {
		t.Fatalf("expected default_provider error, got %v", err)
	}
}

func TestLoadValidatesHeartbeatFile(t *testing.T) {
	path := writeConfig(t, `
session:
  heartbeat:
    enabled: true
    file: "   "
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "heartbeat") {
		t.Fatalf("expected heartbeat error, got %v", err)
	}
}

func TestLoadValidConfig(t *testing.T) {
	path := writeConfig(t, `
session:
  slack_scope: thread
  discord_scope: channel
  heartbeat:
    enabled: false
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	if _, err := Load(path); err != nil {
		t.Fatalf("expected config to load, got %v", err)
	}
}

func TestLoadValidatesMemoryScope(t *testing.T) {
	path := writeConfig(t, `
session:
  memory:
    scope: nope
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "memory.scope") {
		t.Fatalf("expected memory.scope error, got %v", err)
	}
}

func TestLoadValidatesMemoryFlushThreshold(t *testing.T) {
	path := writeConfig(t, `
session:
  memory_flush:
    enabled: true
    threshold: -1
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "memory_flush.threshold") {
		t.Fatalf("expected memory_flush.threshold error, got %v", err)
	}
}

func TestLoadValidatesMemorySearchMaxResults(t *testing.T) {
	path := writeConfig(t, `
tools:
  memory_search:
    enabled: true
    max_results: -5
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "memory_search.max_results") {
		t.Fatalf("expected memory_search.max_results error, got %v", err)
	}
}

func TestLoadValidatesWorkspaceMaxChars(t *testing.T) {
	path := writeConfig(t, `
workspace:
  enabled: true
  max_chars: -5
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "workspace.max_chars") {
		t.Fatalf("expected workspace.max_chars error, got %v", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "nexus.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
