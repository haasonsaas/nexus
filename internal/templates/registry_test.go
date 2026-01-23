package templates

import (
	"context"
	"io"
	"log/slog"
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

func TestRegistry_GetNotFound(t *testing.T) {
	r := &Registry{
		templates: make(map[string]*AgentTemplate),
	}

	tmpl, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent template")
	}
	if tmpl != nil {
		t.Error("expected nil template")
	}
}

func TestRegistry_GetFound(t *testing.T) {
	r := &Registry{
		templates: map[string]*AgentTemplate{
			"test-template": {Name: "test-template", Description: "Test"},
		},
	}

	tmpl, ok := r.Get("test-template")
	if !ok {
		t.Error("expected ok=true for existing template")
	}
	if tmpl == nil {
		t.Fatal("expected non-nil template")
	}
	if tmpl.Name != "test-template" {
		t.Errorf("Name = %q, want %q", tmpl.Name, "test-template")
	}
}

func TestRegistry_List(t *testing.T) {
	r := &Registry{
		templates: map[string]*AgentTemplate{
			"zebra": {Name: "zebra"},
			"alpha": {Name: "alpha"},
			"beta":  {Name: "beta"},
		},
	}

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(list))
	}

	// Should be sorted alphabetically
	expected := []string{"alpha", "beta", "zebra"}
	for i, tmpl := range list {
		if tmpl.Name != expected[i] {
			t.Errorf("list[%d].Name = %q, want %q", i, tmpl.Name, expected[i])
		}
	}
}

func TestRegistry_ListEmpty(t *testing.T) {
	r := &Registry{
		templates: make(map[string]*AgentTemplate),
	}

	list := r.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d items", len(list))
	}
}

func TestRegistry_ListSnapshots(t *testing.T) {
	r := &Registry{
		templates: map[string]*AgentTemplate{
			"template1": {Name: "template1", Version: "1.0", Description: "First"},
			"template2": {Name: "template2", Version: "2.0", Description: "Second"},
		},
	}

	snapshots := r.ListSnapshots()
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}

	// Check that snapshots are correctly created
	for _, snap := range snapshots {
		if snap.Name == "" {
			t.Error("snapshot Name should not be empty")
		}
	}
}

func TestRegistry_ListByTag(t *testing.T) {
	r := &Registry{
		templates: map[string]*AgentTemplate{
			"python-helper": {Name: "python-helper", Tags: []string{"python", "coding"}},
			"go-helper":     {Name: "go-helper", Tags: []string{"go", "coding"}},
			"generic":       {Name: "generic", Tags: []string{"general"}},
		},
	}

	t.Run("finds matching tag", func(t *testing.T) {
		results := r.ListByTag("coding")
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("returns empty for no match", func(t *testing.T) {
		results := r.ListByTag("javascript")
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("finds single match", func(t *testing.T) {
		results := r.ListByTag("python")
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})
}

func TestRegistry_Search(t *testing.T) {
	r := &Registry{
		templates: map[string]*AgentTemplate{
			"code-assistant": {Name: "code-assistant", Description: "Helps with coding tasks"},
			"writer":         {Name: "writer", Description: "Helps with writing documents"},
			"analyst":        {Name: "analyst", Description: "Data analysis helper"},
		},
	}

	t.Run("finds by name", func(t *testing.T) {
		results := r.Search("code")
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("finds by description", func(t *testing.T) {
		results := r.Search("writing")
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("empty query returns all", func(t *testing.T) {
		results := r.Search("")
		if len(results) != 3 {
			t.Errorf("expected 3 results, got %d", len(results))
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		results := r.Search("xyz123")
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})
}

func TestRegistry_RegisterUnregister(t *testing.T) {
	r := &Registry{
		templates: make(map[string]*AgentTemplate),
		logger:    nopLogger(),
	}

	t.Run("register valid template", func(t *testing.T) {
		tmpl := &AgentTemplate{Name: "new-template", Description: "Test template"}
		err := r.Register(tmpl)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Verify it was added
		got, ok := r.Get("new-template")
		if !ok {
			t.Error("template should exist after register")
		}
		if got.Name != "new-template" {
			t.Errorf("Name = %q, want %q", got.Name, "new-template")
		}
	})

	t.Run("unregister existing template", func(t *testing.T) {
		ok := r.Unregister("new-template")
		if !ok {
			t.Error("expected true for existing template")
		}

		// Verify it was removed
		_, exists := r.Get("new-template")
		if exists {
			t.Error("template should not exist after unregister")
		}
	})

	t.Run("unregister nonexistent template", func(t *testing.T) {
		ok := r.Unregister("nonexistent")
		if ok {
			t.Error("expected false for nonexistent template")
		}
	})
}

func TestRegistry_AddSource(t *testing.T) {
	r := &Registry{
		sources: []DiscoverySource{},
	}

	mockSource := &mockDiscoverySource{}
	r.AddSource(mockSource)

	if len(r.sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(r.sources))
	}
}

// mockDiscoverySource is a minimal mock for testing
type mockDiscoverySource struct{}

func (m *mockDiscoverySource) Discover(ctx context.Context) ([]*AgentTemplate, error) {
	return nil, nil
}

func (m *mockDiscoverySource) Priority() int {
	return 0
}

func (m *mockDiscoverySource) Type() SourceType {
	return SourceLocal
}

// nopLogger returns a logger that discards all output
func nopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRegistry_ComputeWatchPaths(t *testing.T) {
	r := &Registry{
		sources:   []DiscoverySource{},
		templates: make(map[string]*AgentTemplate),
	}

	// With empty sources and templates, should return empty
	paths := r.computeWatchPaths()
	if paths == nil {
		t.Error("expected non-nil slice")
	}
}
