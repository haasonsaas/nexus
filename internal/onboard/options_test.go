package onboard

import "testing"

func TestBuildConfigDefaults(t *testing.T) {
	cfg := BuildConfig(Options{Provider: "openai", ProviderKey: "key"})
	llm := cfg["llm"].(map[string]any)
	if llm["default_provider"].(string) != "openai" {
		t.Fatalf("expected default_provider openai")
	}
	providers := llm["providers"].(map[string]any)
	entry := providers["openai"].(map[string]any)
	if entry["api_key"].(string) != "key" {
		t.Fatalf("expected api key")
	}
}

func TestApplyAuthConfigSetsProvider(t *testing.T) {
	raw := map[string]any{}
	ApplyAuthConfig(raw, "anthropic", "secret", true)
	llm := raw["llm"].(map[string]any)
	if llm["default_provider"].(string) != "anthropic" {
		t.Fatalf("expected default provider")
	}
	providers := llm["providers"].(map[string]any)
	entry := providers["anthropic"].(map[string]any)
	if entry["api_key"].(string) != "secret" {
		t.Fatalf("expected api key")
	}
}
