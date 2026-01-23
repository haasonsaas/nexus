package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConstants(t *testing.T) {
	t.Run("DefaultConfigName", func(t *testing.T) {
		if DefaultConfigName != "nexus.yaml" {
			t.Errorf("DefaultConfigName = %q, want %q", DefaultConfigName, "nexus.yaml")
		}
	})

	t.Run("ProfileExt", func(t *testing.T) {
		if ProfileExt != ".yaml" {
			t.Errorf("ProfileExt = %q, want %q", ProfileExt, ".yaml")
		}
	})
}

func TestConfigDir(t *testing.T) {
	dir := ConfigDir()
	if dir == "" {
		t.Error("ConfigDir returned empty string")
	}
	if !strings.Contains(dir, ".nexus") {
		t.Errorf("ConfigDir = %q, should contain .nexus", dir)
	}
	if !strings.Contains(dir, "profiles") {
		t.Errorf("ConfigDir = %q, should contain profiles", dir)
	}
}

func TestActiveProfileFile(t *testing.T) {
	path := ActiveProfileFile()
	if path == "" {
		t.Error("ActiveProfileFile returned empty string")
	}
	if !strings.Contains(path, ".nexus") {
		t.Errorf("ActiveProfileFile = %q, should contain .nexus", path)
	}
	if !strings.HasSuffix(path, "active_profile") {
		t.Errorf("ActiveProfileFile = %q, should end with active_profile", path)
	}
}

func TestProfileConfigPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty returns default",
			input:    "",
			expected: DefaultConfigName,
		},
		{
			name:     "whitespace only returns default",
			input:    "   ",
			expected: DefaultConfigName,
		},
		{
			name:  "valid name returns profile path",
			input: "test",
		},
		{
			name:  "name with spaces is trimmed",
			input: "  myprofile  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProfileConfigPath(tt.input)

			if tt.expected == DefaultConfigName {
				if result != DefaultConfigName {
					t.Errorf("ProfileConfigPath(%q) = %q, want %q", tt.input, result, DefaultConfigName)
				}
			} else {
				if result == DefaultConfigName {
					t.Errorf("ProfileConfigPath(%q) = %q, should not be default", tt.input, result)
				}
				if !strings.HasSuffix(result, ProfileExt) {
					t.Errorf("ProfileConfigPath(%q) = %q, should end with %s", tt.input, result, ProfileExt)
				}
				trimmedName := strings.TrimSpace(tt.input)
				if !strings.Contains(result, trimmedName+ProfileExt) {
					t.Errorf("ProfileConfigPath(%q) = %q, should contain %s", tt.input, result, trimmedName+ProfileExt)
				}
			}
		})
	}
}

func TestDefaultConfigPath(t *testing.T) {
	// DefaultConfigPath depends on ReadActiveProfile
	// Without an active profile, it should return DefaultConfigName
	path := DefaultConfigPath()
	if path == "" {
		t.Error("DefaultConfigPath returned empty string")
	}
	// Either it's the default config name or a profile path
	if path != DefaultConfigName && !strings.HasSuffix(path, ProfileExt) {
		t.Errorf("DefaultConfigPath = %q, unexpected format", path)
	}
}

func TestReadActiveProfile_NotExist(t *testing.T) {
	// Create a temp directory structure
	tmpDir := t.TempDir()

	// Override the home directory temporarily
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	name, err := ReadActiveProfile()
	if err != nil {
		t.Fatalf("ReadActiveProfile error: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty name when file doesn't exist, got %q", name)
	}
}

func TestWriteActiveProfile(t *testing.T) {
	t.Run("empty name does nothing", func(t *testing.T) {
		err := WriteActiveProfile("")
		if err != nil {
			t.Errorf("WriteActiveProfile(\"\") error: %v", err)
		}
	})

	t.Run("whitespace only does nothing", func(t *testing.T) {
		err := WriteActiveProfile("   ")
		if err != nil {
			t.Errorf("WriteActiveProfile(\"   \") error: %v", err)
		}
	})

	t.Run("writes profile name", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		err := WriteActiveProfile("testprofile")
		if err != nil {
			t.Fatalf("WriteActiveProfile error: %v", err)
		}

		// Verify the file was created
		path := ActiveProfileFile()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		if strings.TrimSpace(string(data)) != "testprofile" {
			t.Errorf("file content = %q, want %q", string(data), "testprofile")
		}
	})
}

func TestReadActiveProfile(t *testing.T) {
	t.Run("reads existing profile", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		// Write a profile first
		if err := WriteActiveProfile("myprofile"); err != nil {
			t.Fatalf("WriteActiveProfile error: %v", err)
		}

		// Read it back
		name, err := ReadActiveProfile()
		if err != nil {
			t.Fatalf("ReadActiveProfile error: %v", err)
		}
		if name != "myprofile" {
			t.Errorf("ReadActiveProfile = %q, want %q", name, "myprofile")
		}
	})

	t.Run("handles file with extra whitespace", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		// Create directory and write file with whitespace
		dir := filepath.Join(tmpDir, ".nexus")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}
		path := filepath.Join(dir, "active_profile")
		if err := os.WriteFile(path, []byte("  spacedprofile  \n"), 0o644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}

		name, err := ReadActiveProfile()
		if err != nil {
			t.Fatalf("ReadActiveProfile error: %v", err)
		}
		if name != "spacedprofile" {
			t.Errorf("ReadActiveProfile = %q, want %q", name, "spacedprofile")
		}
	})
}

func TestListProfiles(t *testing.T) {
	t.Run("returns nil when directory doesn't exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		profiles, err := ListProfiles()
		if err != nil {
			t.Fatalf("ListProfiles error: %v", err)
		}
		if profiles != nil {
			t.Errorf("expected nil, got %v", profiles)
		}
	})

	t.Run("returns empty when directory is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		// Create empty profiles directory
		dir := ConfigDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}

		profiles, err := ListProfiles()
		if err != nil {
			t.Fatalf("ListProfiles error: %v", err)
		}
		if len(profiles) != 0 {
			t.Errorf("expected 0 profiles, got %d", len(profiles))
		}
	})

	t.Run("returns profiles sorted", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		// Create profiles directory with test files
		dir := ConfigDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}

		// Create profile files
		for _, name := range []string{"charlie", "alpha", "beta"} {
			path := filepath.Join(dir, name+ProfileExt)
			if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
				t.Fatalf("WriteFile error: %v", err)
			}
		}

		profiles, err := ListProfiles()
		if err != nil {
			t.Fatalf("ListProfiles error: %v", err)
		}
		if len(profiles) != 3 {
			t.Fatalf("expected 3 profiles, got %d", len(profiles))
		}

		// Should be sorted
		expected := []string{"alpha", "beta", "charlie"}
		for i, name := range expected {
			if profiles[i] != name {
				t.Errorf("profiles[%d] = %q, want %q", i, profiles[i], name)
			}
		}
	})

	t.Run("ignores non-yaml files", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		dir := ConfigDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}

		// Create various files
		files := []string{"valid.yaml", "invalid.txt", "another.json", "profile.yaml"}
		for _, f := range files {
			path := filepath.Join(dir, f)
			if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
				t.Fatalf("WriteFile error: %v", err)
			}
		}

		profiles, err := ListProfiles()
		if err != nil {
			t.Fatalf("ListProfiles error: %v", err)
		}
		if len(profiles) != 2 {
			t.Fatalf("expected 2 yaml profiles, got %d: %v", len(profiles), profiles)
		}
	})

	t.Run("ignores directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		dir := ConfigDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}

		// Create a file and a directory with .yaml extension (unusual but test it)
		if err := os.WriteFile(filepath.Join(dir, "real.yaml"), []byte("test"), 0o644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "fakedir.yaml"), 0o755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}

		profiles, err := ListProfiles()
		if err != nil {
			t.Fatalf("ListProfiles error: %v", err)
		}
		if len(profiles) != 1 {
			t.Fatalf("expected 1 profile (file only), got %d: %v", len(profiles), profiles)
		}
		if profiles[0] != "real" {
			t.Errorf("profiles[0] = %q, want %q", profiles[0], "real")
		}
	})
}

func TestDefaultConfigPath_WithActiveProfile(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Set an active profile
	if err := WriteActiveProfile("custom"); err != nil {
		t.Fatalf("WriteActiveProfile error: %v", err)
	}

	path := DefaultConfigPath()
	if path == DefaultConfigName {
		t.Error("DefaultConfigPath should not return default when profile is set")
	}
	if !strings.Contains(path, "custom") {
		t.Errorf("DefaultConfigPath = %q, should contain 'custom'", path)
	}
}
