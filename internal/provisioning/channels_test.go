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

func TestChannelInfoStruct(t *testing.T) {
	info := ChannelInfo{
		Type:      "telegram",
		Enabled:   true,
		HasToken:  true,
		TokenHint: "...wxYZ",
	}

	if info.Type != "telegram" {
		t.Errorf("Type = %q, want %q", info.Type, "telegram")
	}
	if !info.Enabled {
		t.Error("Enabled should be true")
	}
	if !info.HasToken {
		t.Error("HasToken should be true")
	}
	if info.TokenHint != "...wxYZ" {
		t.Errorf("TokenHint = %q, want %q", info.TokenHint, "...wxYZ")
	}
}

// minimalConfig returns a minimal valid config with the required fields
func minimalConfig(channelSection string) string {
	return `version: 1
llm:
  default_provider: anthropic
  providers:
    anthropic:
      default_model: claude-3-haiku-20240307
` + channelSection
}

func TestListChannels(t *testing.T) {
	t.Run("with telegram configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  telegram:
    enabled: true
    bot_token: "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ"
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		channels, err := p.ListChannels(t.Context())
		if err != nil {
			t.Fatalf("ListChannels error: %v", err)
		}

		if len(channels) != 1 {
			t.Fatalf("expected 1 channel, got %d", len(channels))
		}
		if channels[0].Type != "telegram" {
			t.Errorf("Type = %q, want %q", channels[0].Type, "telegram")
		}
		if !channels[0].Enabled {
			t.Error("Expected telegram to be enabled")
		}
		if !channels[0].HasToken {
			t.Error("Expected telegram to have token")
		}
		if channels[0].TokenHint != "...wxYZ" {
			t.Errorf("TokenHint = %q, want %q", channels[0].TokenHint, "...wxYZ")
		}
	})

	t.Run("with multiple channels", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  telegram:
    enabled: true
    bot_token: "telegram-token-1234"
  discord:
    enabled: false
    bot_token: "discord-token-5678"
  slack:
    enabled: true
    bot_token: "xoxb-slack-token"
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		channels, err := p.ListChannels(t.Context())
		if err != nil {
			t.Fatalf("ListChannels error: %v", err)
		}

		if len(channels) != 3 {
			t.Fatalf("expected 3 channels, got %d", len(channels))
		}
	})

	t.Run("with no channels configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels: {}
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		channels, err := p.ListChannels(t.Context())
		if err != nil {
			t.Fatalf("ListChannels error: %v", err)
		}

		if len(channels) != 0 {
			t.Errorf("expected 0 channels, got %d", len(channels))
		}
	})

	t.Run("config file not found", func(t *testing.T) {
		p := NewChannelProvisioner("/nonexistent/config.yaml", nil)
		_, err := p.ListChannels(t.Context())
		if err == nil {
			t.Error("expected error for missing config file")
		}
	})
}

func TestValidateChannel(t *testing.T) {
	t.Run("valid telegram config", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  telegram:
    bot_token: "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ"
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "telegram")
		if err != nil {
			t.Errorf("ValidateChannel error: %v", err)
		}
	})

	t.Run("telegram missing token", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  telegram:
    enabled: true
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "telegram")
		if err == nil {
			t.Error("expected error for missing telegram token")
		}
	})

	t.Run("telegram invalid token format", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  telegram:
    bot_token: "invalid-no-colon"
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "telegram")
		if err == nil {
			t.Error("expected error for invalid telegram token format")
		}
	})

	t.Run("valid discord config", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  discord:
    bot_token: "discord-bot-token"
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "discord")
		if err != nil {
			t.Errorf("ValidateChannel error: %v", err)
		}
	})

	t.Run("discord missing token", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  discord:
    enabled: true
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "discord")
		if err == nil {
			t.Error("expected error for missing discord token")
		}
	})

	t.Run("valid slack config", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  slack:
    bot_token: "xoxb-slack-bot-token"
    app_token: "xapp-slack-app-token"
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "slack")
		if err != nil {
			t.Errorf("ValidateChannel error: %v", err)
		}
	})

	t.Run("slack missing app_token", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels:
  slack:
    bot_token: "xoxb-slack-bot-token"
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "slack")
		if err == nil {
			t.Error("expected error for missing slack app_token")
		}
	})

	t.Run("unsupported channel type", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels: {}
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ValidateChannel(t.Context(), "unsupported")
		if err == nil {
			t.Error("expected error for unsupported channel type")
		}
	})
}

func TestEnableDisableChannel(t *testing.T) {
	t.Run("enable channel", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  telegram:
    enabled: false
    bot_token: "test-token:abc"
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.EnableChannel(t.Context(), "telegram")
		if err != nil {
			t.Fatalf("EnableChannel error: %v", err)
		}

		// Verify the change
		content, _ := os.ReadFile(cfgPath)
		if !contains(string(content), "enabled: true") {
			t.Errorf("expected enabled: true in config, got: %s", content)
		}
	})

	t.Run("disable channel", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  discord:
    enabled: true
    bot_token: "discord-token"
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.DisableChannel(t.Context(), "discord")
		if err != nil {
			t.Fatalf("DisableChannel error: %v", err)
		}

		// Verify the change
		content, _ := os.ReadFile(cfgPath)
		if !contains(string(content), "enabled: false") {
			t.Errorf("expected enabled: false in config, got: %s", content)
		}
	})

	t.Run("enable nonexistent config file", func(t *testing.T) {
		p := NewChannelProvisioner("/nonexistent/config.yaml", nil)
		err := p.EnableChannel(t.Context(), "telegram")
		if err == nil {
			t.Error("expected error for missing config file")
		}
	})
}

func TestApplyProvisioning(t *testing.T) {
	t.Run("provision telegram", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  telegram:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "telegram", map[string]string{
			"bot_token": "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ",
		})
		if err != nil {
			t.Fatalf("ApplyProvisioning error: %v", err)
		}

		content, _ := os.ReadFile(cfgPath)
		if !contains(string(content), "bot_token: 123456789:ABCdefGHIjklMNOpqrSTUvwxYZ") {
			t.Errorf("expected bot_token in config, got: %s", content)
		}
		if !contains(string(content), "enabled: true") {
			t.Errorf("expected enabled: true in config, got: %s", content)
		}
	})

	t.Run("provision telegram missing token", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  telegram:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "telegram", map[string]string{})
		if err == nil {
			t.Error("expected error for missing bot_token")
		}
	})

	t.Run("provision discord", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  discord:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "discord", map[string]string{
			"bot_token":      "discord-bot-token",
			"application_id": "123456789",
		})
		if err != nil {
			t.Fatalf("ApplyProvisioning error: %v", err)
		}

		content, _ := os.ReadFile(cfgPath)
		if !contains(string(content), "bot_token: discord-bot-token") {
			t.Errorf("expected bot_token in config, got: %s", content)
		}
	})

	t.Run("provision discord missing fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  discord:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "discord", map[string]string{
			"bot_token": "discord-bot-token",
		})
		if err == nil {
			t.Error("expected error for missing application_id")
		}
	})

	t.Run("provision slack", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  slack:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "slack", map[string]string{
			"bot_token":      "xoxb-slack-bot-token",
			"app_token":      "xapp-slack-app-token",
			"signing_secret": "slack-signing-secret",
		})
		if err != nil {
			t.Fatalf("ApplyProvisioning error: %v", err)
		}

		content, _ := os.ReadFile(cfgPath)
		if !contains(string(content), "bot_token: xoxb-slack-bot-token") {
			t.Errorf("expected bot_token in config, got: %s", content)
		}
		if !contains(string(content), "app_token: xapp-slack-app-token") {
			t.Errorf("expected app_token in config, got: %s", content)
		}
	})

	t.Run("provision slack missing required fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  slack:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "slack", map[string]string{
			"bot_token": "xoxb-slack-bot-token",
		})
		if err == nil {
			t.Error("expected error for missing app_token")
		}
	})

	t.Run("provision signal", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  signal:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "signal", map[string]string{
			"phone_number": "+1234567890",
		})
		if err != nil {
			t.Fatalf("ApplyProvisioning error: %v", err)
		}

		content, _ := os.ReadFile(cfgPath)
		if !contains(string(content), "account: \"+1234567890\"") && !contains(string(content), "account: +1234567890") {
			t.Errorf("expected account in config, got: %s", content)
		}
	})

	t.Run("provision whatsapp", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `version: 1
channels:
  whatsapp:
    enabled: false
`
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "whatsapp", map[string]string{})
		if err != nil {
			t.Fatalf("ApplyProvisioning error: %v", err)
		}

		content, _ := os.ReadFile(cfgPath)
		if !contains(string(content), "enabled: true") {
			t.Errorf("expected enabled: true in config, got: %s", content)
		}
	})

	t.Run("provision unsupported channel", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		configContent := minimalConfig(`channels: {}
`)
		if err := os.WriteFile(cfgPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.ApplyProvisioning(t.Context(), "unsupported", map[string]string{})
		if err == nil {
			t.Error("expected error for unsupported channel type")
		}
	})

	t.Run("nil provisioner", func(t *testing.T) {
		var p *ChannelProvisioner
		err := p.ApplyProvisioning(t.Context(), "telegram", map[string]string{
			"bot_token": "test",
		})
		if err == nil {
			t.Error("expected error for nil provisioner")
		}
	})

	t.Run("empty config path", func(t *testing.T) {
		p := NewChannelProvisioner("", nil)
		err := p.ApplyProvisioning(t.Context(), "telegram", map[string]string{
			"bot_token": "test",
		})
		if err == nil {
			t.Error("expected error for empty config path")
		}
	})
}

func TestUpdateChannelEnabled(t *testing.T) {
	t.Run("invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		// Write invalid YAML
		if err := os.WriteFile(cfgPath, []byte("invalid: yaml: content:"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		p := NewChannelProvisioner(cfgPath, nil)
		err := p.EnableChannel(t.Context(), "telegram")
		if err == nil {
			t.Error("expected error for invalid YAML")
		}
	})
}
