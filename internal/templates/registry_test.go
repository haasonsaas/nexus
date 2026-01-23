package templates

import (
	"testing"
)

func TestSortTemplates(t *testing.T) {
	templates := []*AgentTemplate{
		{Name: "zebra"},
		{Name: "alpha"},
		{Name: "middle"},
		{Name: "beta"},
	}

	sortTemplates(templates)

	expected := []string{"alpha", "beta", "middle", "zebra"}
	for i, tmpl := range templates {
		if tmpl.Name != expected[i] {
			t.Errorf("templates[%d].Name = %q, want %q", i, tmpl.Name, expected[i])
		}
	}
}

func TestSortTemplates_Empty(t *testing.T) {
	var templates []*AgentTemplate
	sortTemplates(templates) // Should not panic
	if len(templates) != 0 {
		t.Errorf("expected empty slice")
	}
}

func TestSortTemplates_Single(t *testing.T) {
	templates := []*AgentTemplate{{Name: "single"}}
	sortTemplates(templates)
	if templates[0].Name != "single" {
		t.Errorf("Name = %q, want %q", templates[0].Name, "single")
	}
}

func TestNormalizeSearchQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello", "hello"},
		{"  hello  ", "hello"},
		{"UPPER", "upper"},
		{"MiXeD", "mixed"},
		{"", ""},
		{"  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeSearchQuery(tt.input); got != tt.expected {
				t.Errorf("normalizeSearchQuery(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMatchesQuery(t *testing.T) {
	tmpl := &AgentTemplate{
		Name:        "code-assistant",
		Description: "A helpful coding assistant for Python",
		Tags:        []string{"code", "python", "programming"},
	}

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{"matches name", "code", true},
		{"matches name case insensitive", "CODE", true},
		{"matches description", "coding", true},
		{"matches description word", "python", true},
		{"matches tag", "programming", true},
		{"no match", "javascript", false},
		{"empty query matches", "", true},
		{"partial match name", "assist", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := normalizeSearchQuery(tt.query)
			if got := matchesQuery(tmpl, normalized); got != tt.expected {
				t.Errorf("matchesQuery() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMatchesQuery_EmptyTemplate(t *testing.T) {
	tmpl := &AgentTemplate{
		Name:        "",
		Description: "",
		Tags:        nil,
	}

	if matchesQuery(tmpl, "anything") {
		t.Error("expected no match for empty template")
	}
}

func TestNormalizeWatchPath_Empty(t *testing.T) {
	path, ok := normalizeWatchPath("")
	if ok {
		t.Error("expected ok=false for empty path")
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

func TestNormalizeWatchPath_ValidDir(t *testing.T) {
	// Use the current directory which should exist
	path, ok := normalizeWatchPath(".")
	if !ok {
		t.Error("expected ok=true for current directory")
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestNormalizeWatchPath_NonExistent(t *testing.T) {
	path, ok := normalizeWatchPath("/nonexistent/path/12345")
	if ok {
		t.Error("expected ok=false for non-existent path")
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

func TestRegistry_Struct(t *testing.T) {
	// Test that Registry struct has expected fields
	r := &Registry{
		templates: make(map[string]*AgentTemplate),
	}

	if r.templates == nil {
		t.Error("templates should not be nil")
	}
}
