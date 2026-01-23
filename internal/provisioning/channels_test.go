package provisioning

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTokenHint(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{"empty token", "", ""},
		{"short token (1 char)", "a", ""},
		{"short token (3 chars)", "abc", ""},
		{"exact 4 chars", "abcd", "...abcd"},
		{"longer token", "1234567890", "...7890"},
		{"typical bot token", "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ", "...wxYZ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenHint(tt.token)
			if result != tt.expected {
				t.Errorf("tokenHint(%q) = %q, want %q", tt.token, result, tt.expected)
			}
		})
	}
}

func TestNewChannelProvisioner(t *testing.T) {
	t.Run("with nil logger", func(t *testing.T) {
		p := NewChannelProvisioner("/tmp/config.yaml", nil)
		if p == nil {
			t.Fatal("expected non-nil provisioner")
		}
		if p.logger == nil {
			t.Error("logger should default to slog.Default()")
		}
		if p.configPath != "/tmp/config.yaml" {
			t.Errorf("configPath = %q, want %q", p.configPath, "/tmp/config.yaml")
		}
	})
}

func TestSetYAMLValue(t *testing.T) {
	t.Run("set bool value", func(t *testing.T) {
		yamlContent := `
channels:
  telegram:
    enabled: false
`
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(yamlContent), &node); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		err := setYAMLValue(&node, []string{"channels", "telegram", "enabled"}, true)
		if err != nil {
			t.Fatalf("setYAMLValue error: %v", err)
		}

		// Marshal and check
		output, _ := yaml.Marshal(&node)
		if !contains(string(output), "enabled: true") {
			t.Errorf("expected enabled: true in output, got: %s", output)
		}
	})

	t.Run("set string value", func(t *testing.T) {
		yamlContent := `
config:
  name: old
`
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(yamlContent), &node); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		err := setYAMLValue(&node, []string{"config", "name"}, "new")
		if err != nil {
			t.Fatalf("setYAMLValue error: %v", err)
		}

		output, _ := yaml.Marshal(&node)
		if !contains(string(output), "name: new") {
			t.Errorf("expected name: new in output, got: %s", output)
		}
	})

	t.Run("create missing path", func(t *testing.T) {
		yamlContent := `
existing: value
`
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(yamlContent), &node); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		err := setYAMLValue(&node, []string{"new", "nested", "key"}, "value")
		if err != nil {
			t.Fatalf("setYAMLValue error: %v", err)
		}

		output, _ := yaml.Marshal(&node)
		if !contains(string(output), "new:") {
			t.Errorf("expected new: in output, got: %s", output)
		}
	})

	t.Run("empty document error", func(t *testing.T) {
		node := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{}}
		err := setYAMLValue(node, []string{"key"}, "value")
		if err == nil {
			t.Error("expected error for empty document")
		}
	})

	t.Run("unsupported value type", func(t *testing.T) {
		yamlContent := `key: value`
		var node yaml.Node
		yaml.Unmarshal([]byte(yamlContent), &node)

		err := setYAMLValue(&node, []string{}, 123) // int not supported
		if err == nil {
			t.Error("expected error for unsupported type")
		}
	})

	t.Run("expected mapping error", func(t *testing.T) {
		yamlContent := `key: scalar_value`
		var node yaml.Node
		yaml.Unmarshal([]byte(yamlContent), &node)

		// Try to set nested value on a scalar
		err := setYAMLValue(&node, []string{"key", "nested"}, "value")
		if err == nil {
			t.Error("expected error when trying to navigate into scalar")
		}
	})
}

func TestWriteFilePreserveMode(t *testing.T) {
	t.Run("preserves existing file mode", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.yaml")

		// Create file with specific mode
		if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
			t.Fatalf("write original file: %v", err)
		}

		// Write new content
		err := writeFilePreserveMode(path, []byte("updated"))
		if err != nil {
			t.Fatalf("writeFilePreserveMode error: %v", err)
		}

		// Check content
		content, _ := os.ReadFile(path)
		if string(content) != "updated" {
			t.Errorf("content = %q, want %q", string(content), "updated")
		}

		// Check mode preserved
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o640 {
			t.Errorf("mode = %o, want %o", info.Mode().Perm(), 0o640)
		}
	})

	t.Run("uses default mode for new file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "new.yaml")

		err := writeFilePreserveMode(path, []byte("content"))
		if err != nil {
			t.Fatalf("writeFilePreserveMode error: %v", err)
		}

		content, _ := os.ReadFile(path)
		if string(content) != "content" {
			t.Errorf("content = %q, want %q", string(content), "content")
		}

		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o644 {
			t.Errorf("mode = %o, want %o", info.Mode().Perm(), 0o644)
		}
	})

	t.Run("cleans up tmp file on success", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.yaml")

		writeFilePreserveMode(path, []byte("content"))

		// Tmp file should not exist
		tmpPath := path + ".tmp"
		if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
			t.Error("tmp file should not exist after successful write")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
