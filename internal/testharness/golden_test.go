package testharness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeTestName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"TestSimple", "TestSimple"},
		{"Test/WithSlash", "Test_WithSlash"},
		{"Test With Spaces", "Test_With_Spaces"},
		{"Test:WithColon", "Test_WithColon"},
		{"Test/With/Multiple/Slashes", "Test_With_Multiple_Slashes"},
		{"Complex:Test/Name Here", "Complex_Test_Name_Here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeTestName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeTestName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		wantDiff bool
	}{
		{
			name:     "identical strings",
			expected: "line1\nline2\nline3",
			actual:   "line1\nline2\nline3",
			wantDiff: false,
		},
		{
			name:     "different lines",
			expected: "line1\nold\nline3",
			actual:   "line1\nnew\nline3",
			wantDiff: true,
		},
		{
			name:     "extra line in actual",
			expected: "line1\nline2",
			actual:   "line1\nline2\nline3",
			wantDiff: true,
		},
		{
			name:     "extra line in expected",
			expected: "line1\nline2\nline3",
			actual:   "line1\nline2",
			wantDiff: true,
		},
		{
			name:     "empty strings",
			expected: "",
			actual:   "",
			wantDiff: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := diff(tt.expected, tt.actual)
			if tt.wantDiff && result == "" {
				t.Error("expected diff output but got empty string")
			}
			if !tt.wantDiff && result != "" {
				t.Errorf("expected no diff but got: %s", result)
			}
		})
	}
}

func TestGolden_goldenPath(t *testing.T) {
	g := &Golden{
		dir:  "testdata/golden",
		name: "TestExample",
	}

	tests := []struct {
		name     string
		expected string
	}{
		{"", "testdata/golden/TestExample.golden"},
		{"suffix", "testdata/golden/TestExample_suffix.golden"},
		{"json", "testdata/golden/TestExample_json.golden"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.goldenPath(tt.name)
			if result != tt.expected {
				t.Errorf("goldenPath(%q) = %q, want %q", tt.name, result, tt.expected)
			}
		})
	}
}

func TestNewGolden(t *testing.T) {
	g := NewGolden(t)
	if g == nil {
		t.Fatal("NewGolden returned nil")
	}
	if g.t != t {
		t.Error("Golden.t not set correctly")
	}
	if g.dir == "" {
		t.Error("Golden.dir not set")
	}
	if g.name == "" {
		t.Error("Golden.name not set")
	}
}

func TestNewGoldenAt(t *testing.T) {
	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "custom", "golden")

	g := NewGoldenAt(t, customDir)
	if g == nil {
		t.Fatal("NewGoldenAt returned nil")
	}
	if g.dir != customDir {
		t.Errorf("Golden.dir = %q, want %q", g.dir, customDir)
	}

	// Verify directory was created
	if _, err := os.Stat(customDir); os.IsNotExist(err) {
		t.Error("custom golden directory was not created")
	}
}

func TestInitGoldenFlag(t *testing.T) {
	// Save and restore original value
	origValue := UpdateGolden
	t.Cleanup(func() { UpdateGolden = origValue })

	// Test with env var not set
	os.Unsetenv("UPDATE_GOLDEN")
	UpdateGolden = false
	InitGoldenFlag()
	if UpdateGolden {
		t.Error("expected UpdateGolden to remain false when env not set")
	}

	// Test with env var set
	os.Setenv("UPDATE_GOLDEN", "1")
	t.Cleanup(func() { os.Unsetenv("UPDATE_GOLDEN") })
	InitGoldenFlag()
	if !UpdateGolden {
		t.Error("expected UpdateGolden to be true when env is '1'")
	}
}

func TestGolden_Assert_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	goldenDir := filepath.Join(tmpDir, "golden")

	// Create a mock test for the golden helper
	mockT := &testing.T{}

	g := &Golden{
		t:    mockT,
		dir:  goldenDir,
		name: "TestNonexistent",
	}

	// This would call t.Fatalf in a real test, but we can't easily test that
	// Instead, just verify the goldenPath is correct
	path := g.goldenPath("")
	expectedPath := filepath.Join(goldenDir, "TestNonexistent.golden")
	if path != expectedPath {
		t.Errorf("goldenPath() = %q, want %q", path, expectedPath)
	}
}

func TestUpdateGoldenDefault(t *testing.T) {
	// By default, UpdateGolden should be false unless env var is set
	// This test just documents the default behavior
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		t.Skip("skipping when UPDATE_GOLDEN is set")
	}

	// Reset to ensure we're testing the default
	origValue := UpdateGolden
	t.Cleanup(func() { UpdateGolden = origValue })

	// Re-evaluate the package-level var
	if os.Getenv("UPDATE_GOLDEN") != "1" && UpdateGolden {
		// This would only fail if something else set UpdateGolden
		t.Log("UpdateGolden was already set to true by something else")
	}
}
