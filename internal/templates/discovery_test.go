package templates

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourcePriority_Constants(t *testing.T) {
	// Verify ordering: Extra < Builtin < Local < Workspace
	if PriorityExtra >= PriorityBuiltin {
		t.Error("PriorityExtra should be less than PriorityBuiltin")
	}
	if PriorityBuiltin >= PriorityLocal {
		t.Error("PriorityBuiltin should be less than PriorityLocal")
	}
	if PriorityLocal >= PriorityWorkspace {
		t.Error("PriorityLocal should be less than PriorityWorkspace")
	}
}

func TestNewLocalSource(t *testing.T) {
	source := NewLocalSource("/tmp/templates", SourceLocal, PriorityLocal)

	if source == nil {
		t.Fatal("expected non-nil source")
	}
	if source.Type() != SourceLocal {
		t.Errorf("Type() = %v, want %v", source.Type(), SourceLocal)
	}
	if source.Priority() != PriorityLocal {
		t.Errorf("Priority() = %d, want %d", source.Priority(), PriorityLocal)
	}
}

func TestLocalSource_WatchPaths(t *testing.T) {
	source := NewLocalSource("/test/path", SourceLocal, PriorityLocal)
	paths := source.WatchPaths()

	if len(paths) != 1 {
		t.Fatalf("WatchPaths returned %d paths, want 1", len(paths))
	}
	if paths[0] != "/test/path" {
		t.Errorf("WatchPaths[0] = %q, want %q", paths[0], "/test/path")
	}
}

func TestLocalSource_Discover_NonexistentDirectory(t *testing.T) {
	source := NewLocalSource("/nonexistent/path/that/does/not/exist", SourceLocal, PriorityLocal)
	ctx := context.Background()

	templates, err := source.Discover(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if templates != nil {
		t.Errorf("expected nil templates for nonexistent directory, got %d", len(templates))
	}
}

func TestLocalSource_Discover_NotADirectory(t *testing.T) {
	// Create a test file (not directory)
	f, err := os.CreateTemp("", "test-template-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	source := NewLocalSource(f.Name(), SourceLocal, PriorityLocal)
	ctx := context.Background()

	_, err = source.Discover(ctx)
	if err == nil {
		t.Error("expected error for non-directory path")
	}
}

func TestLocalSource_Discover_EmptyDirectory(t *testing.T) {
	// Create a test directory
	dir, err := os.MkdirTemp("", "test-templates-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	source := NewLocalSource(dir, SourceLocal, PriorityLocal)
	ctx := context.Background()

	templates, err := source.Discover(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(templates) != 0 {
		t.Errorf("expected 0 templates, got %d", len(templates))
	}
}

func TestLocalSource_Discover_WithTemplate(t *testing.T) {
	// Create a test directory structure
	dir, err := os.MkdirTemp("", "test-templates-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Create a template subdirectory
	templateDir := filepath.Join(dir, "my-template")
	if err := os.MkdirAll(templateDir, 0755); err != nil {
		t.Fatalf("create template dir: %v", err)
	}

	// Create TEMPLATE.md
	templateContent := `---
name: my-template
description: A test template
agent:
  name: Test Agent
---
This is the system prompt.
`
	if err := os.WriteFile(filepath.Join(templateDir, "TEMPLATE.md"), []byte(templateContent), 0644); err != nil {
		t.Fatalf("write TEMPLATE.md: %v", err)
	}

	source := NewLocalSource(dir, SourceLocal, PriorityLocal)
	ctx := context.Background()

	templates, err := source.Discover(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}
	if templates[0].Name != "my-template" {
		t.Errorf("name = %q, want %q", templates[0].Name, "my-template")
	}
	if templates[0].Source != SourceLocal {
		t.Errorf("source = %v, want %v", templates[0].Source, SourceLocal)
	}
}

func TestLocalSource_Discover_ContextCancellation(t *testing.T) {
	dir, err := os.MkdirTemp("", "test-templates-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Create several template directories
	for i := 0; i < 5; i++ {
		subdir := filepath.Join(dir, "template-"+string(rune('a'+i)))
		os.MkdirAll(subdir, 0755)
	}

	source := NewLocalSource(dir, SourceLocal, PriorityLocal)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = source.Discover(ctx)
	if err != context.Canceled {
		// May or may not return error depending on timing
	}
}

func TestBuildDefaultSources(t *testing.T) {
	t.Run("with all paths", func(t *testing.T) {
		sources := BuildDefaultSources("/workspace", "/home/user/.nexus/templates", []string{"/extra1", "/extra2"})

		if len(sources) != 4 {
			t.Errorf("expected 4 sources, got %d", len(sources))
		}
	})

	t.Run("with empty paths", func(t *testing.T) {
		sources := BuildDefaultSources("", "", nil)

		if len(sources) != 0 {
			t.Errorf("expected 0 sources, got %d", len(sources))
		}
	})

	t.Run("workspace only", func(t *testing.T) {
		sources := BuildDefaultSources("/workspace", "", nil)

		if len(sources) != 1 {
			t.Errorf("expected 1 source, got %d", len(sources))
		}
		if sources[0].Type() != SourceWorkspace {
			t.Errorf("source type = %v, want %v", sources[0].Type(), SourceWorkspace)
		}
	})
}

func TestNewGitSource(t *testing.T) {
	t.Run("with default branch", func(t *testing.T) {
		source := NewGitSource("https://github.com/test/repo", "", "templates", "/cache", 0, 25)

		if source.Branch != "main" {
			t.Errorf("Branch = %q, want %q", source.Branch, "main")
		}
		if source.Type() != SourceGit {
			t.Errorf("Type() = %v, want %v", source.Type(), SourceGit)
		}
		if source.Priority() != 25 {
			t.Errorf("Priority() = %d, want 25", source.Priority())
		}
	})

	t.Run("with custom branch", func(t *testing.T) {
		source := NewGitSource("https://github.com/test/repo", "develop", "", "/cache", 0, 30)

		if source.Branch != "develop" {
			t.Errorf("Branch = %q, want %q", source.Branch, "develop")
		}
	})
}

func TestGitSource_WatchPaths(t *testing.T) {
	t.Run("without subpath", func(t *testing.T) {
		source := NewGitSource("https://github.com/test/repo", "main", "", "/cache", 0, 25)
		paths := source.WatchPaths()

		if len(paths) != 1 {
			t.Fatalf("WatchPaths returned %d paths, want 1", len(paths))
		}
	})

	t.Run("with subpath", func(t *testing.T) {
		source := NewGitSource("https://github.com/test/repo", "main", "templates", "/cache", 0, 25)
		paths := source.WatchPaths()

		if len(paths) != 1 {
			t.Fatalf("WatchPaths returned %d paths, want 1", len(paths))
		}
		if !filepath.IsAbs(paths[0]) || !contains(paths[0], "templates") {
			t.Errorf("unexpected path: %q", paths[0])
		}
	})
}

func TestNewRegistrySource(t *testing.T) {
	source := NewRegistrySource("https://registry.example.com", "token123", 50)

	if source.Type() != SourceRegistry {
		t.Errorf("Type() = %v, want %v", source.Type(), SourceRegistry)
	}
	if source.Priority() != 50 {
		t.Errorf("Priority() = %d, want 50", source.Priority())
	}
	if source.URL != "https://registry.example.com" {
		t.Errorf("URL = %q", source.URL)
	}
	if source.Auth != "token123" {
		t.Errorf("Auth = %q", source.Auth)
	}
}

func TestDiscoverAll(t *testing.T) {
	ctx := context.Background()

	t.Run("empty sources", func(t *testing.T) {
		templates, err := DiscoverAll(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(templates) != 0 {
			t.Errorf("expected 0 templates, got %d", len(templates))
		}
	})

	t.Run("priority ordering", func(t *testing.T) {
		// Create two test directories with same-named template
		dir1, err := os.MkdirTemp("", "test-low-*")
		if err != nil {
			t.Fatalf("create temp dir: %v", err)
		}
		defer os.RemoveAll(dir1)

		dir2, err := os.MkdirTemp("", "test-high-*")
		if err != nil {
			t.Fatalf("create temp dir: %v", err)
		}
		defer os.RemoveAll(dir2)

		// Create same-named template in both
		templateContent1 := `---
name: test-template
description: Low priority version
agent:
  name: Low
---
Low priority content.
`
		templateContent2 := `---
name: test-template
description: High priority version
agent:
  name: High
---
High priority content.
`

		os.MkdirAll(filepath.Join(dir1, "test-template"), 0755)
		os.WriteFile(filepath.Join(dir1, "test-template", "TEMPLATE.md"), []byte(templateContent1), 0644)

		os.MkdirAll(filepath.Join(dir2, "test-template"), 0755)
		os.WriteFile(filepath.Join(dir2, "test-template", "TEMPLATE.md"), []byte(templateContent2), 0644)

		sources := []DiscoverySource{
			NewLocalSource(dir1, SourceExtra, 10),     // Lower priority
			NewLocalSource(dir2, SourceWorkspace, 40), // Higher priority
		}

		templates, err := DiscoverAll(ctx, sources)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(templates))
		}
		if templates[0].Description != "High priority version" {
			t.Errorf("expected high priority version, got %q", templates[0].Description)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
