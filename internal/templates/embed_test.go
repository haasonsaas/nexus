package templates

import (
	"context"
	"testing"
)

func TestBuiltinFS(t *testing.T) {
	fs := BuiltinFS()
	if fs == nil {
		t.Fatal("BuiltinFS() returned nil")
	}
}

func TestNewBuiltinSource(t *testing.T) {
	source := NewBuiltinSource()
	if source == nil {
		t.Fatal("NewBuiltinSource() returned nil")
	}
	if source.sourceType != SourceBuiltin {
		t.Errorf("sourceType = %v, want %v", source.sourceType, SourceBuiltin)
	}
	if source.priority != PriorityBuiltin {
		t.Errorf("priority = %v, want %v", source.priority, PriorityBuiltin)
	}
}

func TestAddBuiltinSource(t *testing.T) {
	registry, err := NewRegistry(nil, "")
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	initialSources := len(registry.sources)
	AddBuiltinSource(registry)

	if len(registry.sources) != initialSources+1 {
		t.Errorf("AddBuiltinSource() did not add source, have %d want %d", len(registry.sources), initialSources+1)
	}
}

func TestBuiltinTemplateNames(t *testing.T) {
	names := BuiltinTemplateNames()
	if len(names) == 0 {
		t.Error("BuiltinTemplateNames() returned empty slice")
	}

	// Verify expected builtin templates
	expected := map[string]bool{
		"customer-support":   true,
		"code-review":        true,
		"research-assistant": true,
	}
	for _, name := range names {
		if !expected[name] {
			t.Errorf("unexpected builtin template: %q", name)
		}
	}
}

func TestIsBuiltinTemplate(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"customer-support", true},
		{"code-review", true},
		{"research-assistant", true},
		{"custom-template", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsBuiltinTemplate(tt.name)
			if result != tt.expected {
				t.Errorf("IsBuiltinTemplate(%q) = %v, want %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestLoadBuiltinTemplate(t *testing.T) {
	ctx := context.Background()

	t.Run("existing template", func(t *testing.T) {
		// Try to load each builtin template
		for _, name := range BuiltinTemplateNames() {
			tmpl, err := LoadBuiltinTemplate(ctx, name)
			if err != nil {
				t.Errorf("LoadBuiltinTemplate(%q) error = %v", name, err)
				continue
			}
			if tmpl == nil {
				t.Errorf("LoadBuiltinTemplate(%q) returned nil", name)
				continue
			}
			if tmpl.Name != name {
				t.Errorf("LoadBuiltinTemplate(%q).Name = %q", name, tmpl.Name)
			}
		}
	})

	t.Run("nonexistent template", func(t *testing.T) {
		tmpl, err := LoadBuiltinTemplate(ctx, "nonexistent")
		if err != nil {
			t.Errorf("LoadBuiltinTemplate() error = %v", err)
		}
		if tmpl != nil {
			t.Error("expected nil for nonexistent template")
		}
	})
}

func TestNewRegistryWithBuiltins(t *testing.T) {
	registry, err := NewRegistryWithBuiltins(nil, "")
	if err != nil {
		t.Fatalf("NewRegistryWithBuiltins() error = %v", err)
	}
	if registry == nil {
		t.Fatal("NewRegistryWithBuiltins() returned nil")
	}

	// Verify builtin source was added
	foundBuiltin := false
	for _, source := range registry.sources {
		if source.Type() == SourceBuiltin {
			foundBuiltin = true
			break
		}
	}
	if !foundBuiltin {
		t.Error("builtin source not found in registry")
	}
}
