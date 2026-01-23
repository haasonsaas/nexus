// Package testharness provides compatibility test utilities including
// golden file snapshot testing and behavioral test fixtures.
package testharness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// UpdateGolden is set via -update flag or UPDATE_GOLDEN=1 env var to update golden files.
var UpdateGolden = os.Getenv("UPDATE_GOLDEN") == "1"

// Golden provides snapshot testing functionality that compares actual output
// against stored golden files. When UpdateGolden is true, it updates the files instead.
type Golden struct {
	t    *testing.T
	dir  string
	name string
}

// NewGolden creates a new golden file helper for the test.
// Golden files are stored in testdata/<test_name>.golden files.
func NewGolden(t *testing.T) *Golden {
	t.Helper()
	testDataDir := filepath.Join("testdata", "golden")
	if err := os.MkdirAll(testDataDir, 0o755); err != nil {
		t.Fatalf("failed to create golden dir: %v", err)
	}
	return &Golden{
		t:    t,
		dir:  testDataDir,
		name: sanitizeTestName(t.Name()),
	}
}

// NewGoldenAt creates a golden file helper at a specific directory.
func NewGoldenAt(t *testing.T, dir string) *Golden {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create golden dir: %v", err)
	}
	return &Golden{
		t:    t,
		dir:  dir,
		name: sanitizeTestName(t.Name()),
	}
}

// Assert compares actual against the golden file.
// If UpdateGolden is set, it updates the golden file instead of comparing.
func (g *Golden) Assert(actual string) {
	g.t.Helper()
	g.assertNamed("", actual)
}

// AssertNamed compares actual against a named golden file.
// Useful when a single test has multiple golden assertions.
func (g *Golden) AssertNamed(name, actual string) {
	g.t.Helper()
	g.assertNamed(name, actual)
}

// AssertJSON compares JSON output, pretty-printing for readability.
func (g *Golden) AssertJSON(actual any) {
	g.t.Helper()
	g.assertJSONNamed("", actual)
}

// AssertJSONNamed compares named JSON output.
func (g *Golden) AssertJSONNamed(name string, actual any) {
	g.t.Helper()
	g.assertJSONNamed(name, actual)
}

func (g *Golden) assertNamed(name, actual string) {
	g.t.Helper()

	filename := g.goldenPath(name)

	if UpdateGolden {
		if err := os.WriteFile(filename, []byte(actual), 0o644); err != nil {
			g.t.Fatalf("failed to update golden file %s: %v", filename, err)
		}
		g.t.Logf("updated golden file: %s", filename)
		return
	}

	expected, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			g.t.Fatalf("golden file %s does not exist. Run with -update to create it.\n\nActual output:\n%s", filename, actual)
		}
		g.t.Fatalf("failed to read golden file %s: %v", filename, err)
	}

	if string(expected) != actual {
		g.t.Errorf("golden file mismatch %s\n\nExpected:\n%s\n\nActual:\n%s\n\nDiff:\n%s",
			filename, string(expected), actual, diff(string(expected), actual))
	}
}

func (g *Golden) assertJSONNamed(name string, actual any) {
	g.t.Helper()

	pretty, err := json.MarshalIndent(actual, "", "  ")
	if err != nil {
		g.t.Fatalf("failed to marshal JSON: %v", err)
	}

	g.assertNamed(name+".json", string(pretty))
}

func (g *Golden) goldenPath(name string) string {
	if name == "" {
		return filepath.Join(g.dir, g.name+".golden")
	}
	return filepath.Join(g.dir, g.name+"_"+name+".golden")
}

func sanitizeTestName(name string) string {
	// Replace special characters with underscores
	replacer := strings.NewReplacer("/", "_", " ", "_", ":", "_")
	return replacer.Replace(name)
}

// diff returns a simple line-based diff of two strings.
func diff(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")

	var result strings.Builder

	maxLen := len(expectedLines)
	if len(actualLines) > maxLen {
		maxLen = len(actualLines)
	}

	for i := 0; i < maxLen; i++ {
		var exp, act string
		if i < len(expectedLines) {
			exp = expectedLines[i]
		}
		if i < len(actualLines) {
			act = actualLines[i]
		}

		if exp != act {
			result.WriteString("- ")
			result.WriteString(exp)
			result.WriteString("\n+ ")
			result.WriteString(act)
			result.WriteString("\n")
		}
	}

	return result.String()
}

// InitGoldenFlag initializes the update flag from the test flags.
// Call this in TestMain if using custom test flags.
func InitGoldenFlag() {
	// In production, this would parse -update flag
	// For now, check environment variable
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		UpdateGolden = true
	}
}
