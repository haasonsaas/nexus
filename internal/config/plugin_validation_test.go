package config

import "testing"

func TestPluginValidationIssues_NilConfig(t *testing.T) {
	// Register a validator that should not be called with nil config.
	RegisterPluginValidator(func(cfg *Config) []string {
		t.Fatal("validator should not be called with nil config")
		return nil
	})
	defer RegisterPluginValidator(nil)

	issues := pluginValidationIssues(nil)
	if issues != nil {
		t.Fatalf("expected nil issues for nil config, got %v", issues)
	}
}

func TestPluginValidationIssues_NoValidator(t *testing.T) {
	RegisterPluginValidator(nil)

	issues := pluginValidationIssues(&Config{})
	if issues != nil {
		t.Fatalf("expected nil issues when no validator registered, got %v", issues)
	}
}

func TestPluginValidationIssues_ValidatorReturnsIssues(t *testing.T) {
	RegisterPluginValidator(func(cfg *Config) []string {
		return []string{"issue1", "issue2"}
	})
	defer RegisterPluginValidator(nil)

	issues := pluginValidationIssues(&Config{})
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[0] != "issue1" || issues[1] != "issue2" {
		t.Fatalf("unexpected issues: %v", issues)
	}
}

func TestPluginValidationIssues_ValidatorReturnsNil(t *testing.T) {
	RegisterPluginValidator(func(cfg *Config) []string {
		return nil
	})
	defer RegisterPluginValidator(nil)

	issues := pluginValidationIssues(&Config{})
	if issues != nil {
		t.Fatalf("expected nil issues, got %v", issues)
	}
}
