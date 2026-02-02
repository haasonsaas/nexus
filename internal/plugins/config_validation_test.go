package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestValidateConfigMissingManifest(t *testing.T) {
	cfg := writeConfigFile(t, `
plugins:
  entries:
    voice-call:
      enabled: true
      config: {}
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	parsed, err := config.Load(cfg)
	if err == nil {
		t.Fatalf("expected load error")
	}
	if !strings.Contains(err.Error(), "plugins.entries.voice-call missing manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = parsed
}

func TestValidateConfigSchema(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
  "id": "voice-call",
  "configSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["token"],
    "properties": {
      "token": { "type": "string" }
    }
  }
}`)

	cfg := writeConfigFile(t, `
plugins:
  load:
    paths:
      - `+dir+`
  entries:
    voice-call:
      enabled: true
      config: {}
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	parsed, err := config.Load(cfg)
	if err == nil {
		t.Fatalf("expected load error")
	}
	if !strings.Contains(err.Error(), "plugins.entries.voice-call config invalid") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = parsed
}

func TestValidateConfigAcceptsPluginConfig(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
  "id": "voice-call",
  "configSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["token"],
    "properties": {
      "token": { "type": "string" }
    }
  }
}`)

	cfg := writeConfigFile(t, `
plugins:
  load:
    paths:
      - `+dir+`
  entries:
    voice-call:
      enabled: true
      config:
        token: abc
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	parsed, err := config.Load(cfg)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := ValidateConfig(parsed); err != nil {
		t.Fatalf("expected validation to pass, got %v", err)
	}
}

func TestValidateConfigAllowsPluginIsolationEnabled(t *testing.T) {
	cfg := writeConfigFile(t, `
plugins:
  isolation:
    enabled: true
    backend: daytona
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	parsed, err := config.Load(cfg)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := ValidateConfig(parsed); err != nil {
		t.Fatalf("expected validation to pass, got %v", err)
	}
}

func TestValidateConfigRejectsIsolationMissingBackend(t *testing.T) {
	cfg := writeConfigFile(t, `
plugins:
  isolation:
    enabled: true
llm:
  default_provider: anthropic
  providers:
    anthropic: {}
`)

	parsed, err := config.Load(cfg)
	if err != nil {
		if !strings.Contains(err.Error(), "plugins.isolation.backend") {
			t.Fatalf("expected isolation backend error, got %v", err)
		}
		return
	}
	if err := ValidateConfig(parsed); err == nil {
		t.Fatalf("expected validation to fail when isolation backend is missing")
	}
}

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "nexus.yaml")
	trimmed := strings.TrimSpace(contents)
	if !strings.HasPrefix(trimmed, "version:") {
		trimmed = fmt.Sprintf("version: %d\n%s", config.CurrentVersion, trimmed)
	}
	if err := os.WriteFile(path, []byte(trimmed), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func writeManifest(t *testing.T, dir string, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "nexus.plugin.json")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
