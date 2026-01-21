package doctor

import "testing"

func TestApplyConfigMigrationsMovesTools(t *testing.T) {
	raw := map[string]any{
		"plugins": map[string]any{
			"sandbox": map[string]any{"enabled": true},
			"browser": map[string]any{"enabled": true},
		},
	}

	report := ApplyConfigMigrations(raw)
	if len(report.Applied) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(report.Applied))
	}

	plugins := raw["plugins"].(map[string]any)
	if _, ok := plugins["sandbox"]; ok {
		t.Fatalf("expected plugins.sandbox to be removed")
	}
	if _, ok := plugins["browser"]; ok {
		t.Fatalf("expected plugins.browser to be removed")
	}

	tools := raw["tools"].(map[string]any)
	if _, ok := tools["sandbox"]; !ok {
		t.Fatalf("expected tools.sandbox to be set")
	}
	if _, ok := tools["browser"]; !ok {
		t.Fatalf("expected tools.browser to be set")
	}
}

func TestApplyConfigMigrationsRespectsExistingTools(t *testing.T) {
	raw := map[string]any{
		"plugins": map[string]any{
			"sandbox": map[string]any{"enabled": true},
		},
		"tools": map[string]any{
			"sandbox": map[string]any{"enabled": false},
		},
	}

	report := ApplyConfigMigrations(raw)
	if len(report.Applied) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(report.Applied))
	}

	plugins := raw["plugins"].(map[string]any)
	if _, ok := plugins["sandbox"]; ok {
		t.Fatalf("expected plugins.sandbox to be removed")
	}

	tools := raw["tools"].(map[string]any)
	if toolSandbox, ok := tools["sandbox"].(map[string]any); !ok || toolSandbox["enabled"].(bool) != false {
		t.Fatalf("expected tools.sandbox to remain unchanged")
	}
}

func TestApplyConfigMigrationsRemovesObservability(t *testing.T) {
	raw := map[string]any{
		"observability": map[string]any{"metrics": map[string]any{"enabled": true}},
	}

	report := ApplyConfigMigrations(raw)
	if len(report.Applied) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(report.Applied))
	}
	if _, ok := raw["observability"]; ok {
		t.Fatalf("expected observability to be removed")
	}
}
