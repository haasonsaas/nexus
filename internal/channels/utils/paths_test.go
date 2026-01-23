package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home directory: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "expands tilde prefix",
			path:     "~/foo/bar",
			expected: filepath.Join(home, "foo/bar"),
		},
		{
			name:     "preserves absolute path",
			path:     "/absolute/path",
			expected: "/absolute/path",
		},
		{
			name:     "preserves relative path",
			path:     "relative/path",
			expected: "relative/path",
		},
		{
			name:     "handles tilde only",
			path:     "~/",
			expected: home,
		},
		{
			name:     "does not expand tilde in middle",
			path:     "/foo/~/bar",
			expected: "/foo/~/bar",
		},
		{
			name:     "empty string",
			path:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.path)
			if got != tt.expected {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestExpandPathWithDefault(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home directory: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		defaultPath string
		expected    string
	}{
		{
			name:        "uses provided path",
			path:        "~/custom",
			defaultPath: "~/default",
			expected:    filepath.Join(home, "custom"),
		},
		{
			name:        "uses default when empty",
			path:        "",
			defaultPath: "~/default",
			expected:    filepath.Join(home, "default"),
		},
		{
			name:        "uses default when whitespace only",
			path:        "   ",
			defaultPath: "~/default",
			expected:    filepath.Join(home, "default"),
		},
		{
			name:        "expands default path too",
			path:        "",
			defaultPath: "~/config/nexus",
			expected:    filepath.Join(home, "config/nexus"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPathWithDefault(tt.path, tt.defaultPath)
			if got != tt.expected {
				t.Errorf("ExpandPathWithDefault(%q, %q) = %q, want %q", tt.path, tt.defaultPath, got, tt.expected)
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "creates new directory",
			path:    filepath.Join(tmpDir, "new", "nested", "dir"),
			wantErr: false,
		},
		{
			name:    "succeeds on existing directory",
			path:    tmpDir,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureDir(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureDir(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				info, err := os.Stat(tt.path)
				if err != nil {
					t.Errorf("directory not created: %v", err)
					return
				}
				if !info.IsDir() {
					t.Errorf("path is not a directory")
				}
			}
		})
	}
}

func TestEnsureParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "parent", "child", "file.txt")

	err := EnsureParentDir(filePath)
	if err != nil {
		t.Errorf("EnsureParentDir(%q) error = %v", filePath, err)
		return
	}

	parentDir := filepath.Dir(filePath)
	info, err := os.Stat(parentDir)
	if err != nil {
		t.Errorf("parent directory not created: %v", err)
		return
	}
	if !info.IsDir() {
		t.Errorf("parent path is not a directory")
	}
}
