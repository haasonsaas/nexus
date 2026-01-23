package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestLoaderConfigFromConfig(t *testing.T) {
	t.Run("nil config uses defaults", func(t *testing.T) {
		cfg := LoaderConfigFromConfig(nil)
		if cfg.SoulFile != "SOUL.md" {
			t.Errorf("SoulFile = %q, want %q", cfg.SoulFile, "SOUL.md")
		}
		if cfg.UserFile != "USER.md" {
			t.Errorf("UserFile = %q, want %q", cfg.UserFile, "USER.md")
		}
	})

	t.Run("overrides from config", func(t *testing.T) {
		appCfg := &config.Config{
			Workspace: config.WorkspaceConfig{
				Path:         "/custom/path",
				SoulFile:     "custom_soul.md",
				IdentityFile: "custom_id.md",
			},
		}
		cfg := LoaderConfigFromConfig(appCfg)
		if cfg.Root != "/custom/path" {
			t.Errorf("Root = %q, want %q", cfg.Root, "/custom/path")
		}
		if cfg.SoulFile != "custom_soul.md" {
			t.Errorf("SoulFile = %q, want %q", cfg.SoulFile, "custom_soul.md")
		}
		if cfg.IdentityFile != "custom_id.md" {
			t.Errorf("IdentityFile = %q, want %q", cfg.IdentityFile, "custom_id.md")
		}
		// Unchanged defaults
		if cfg.UserFile != "USER.md" {
			t.Errorf("UserFile = %q, want %q", cfg.UserFile, "USER.md")
		}
	})
}

func TestLoadWorkspace(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	soulContent := "# SOUL.md\n\nBe helpful and concise."
	userContent := "# USER.md\n\n- Name: Alice\n- Preferred address: Ali\n- Timezone: PST"
	identityContent := "# IDENTITY.md\n\n- Name: Nexus\n- Creature: Phoenix\n- Vibe: Helpful\n- Emoji: 🔥"

	os.WriteFile(filepath.Join(tmpDir, "SOUL.md"), []byte(soulContent), 0644)
	os.WriteFile(filepath.Join(tmpDir, "USER.md"), []byte(userContent), 0644)
	os.WriteFile(filepath.Join(tmpDir, "IDENTITY.md"), []byte(identityContent), 0644)

	ctx, err := LoadWorkspace(LoaderConfig{Root: tmpDir})
	if err != nil {
		t.Fatalf("LoadWorkspace error: %v", err)
	}

	if ctx.SoulContent != soulContent {
		t.Errorf("SoulContent = %q, want %q", ctx.SoulContent, soulContent)
	}

	if ctx.Identity == nil {
		t.Fatal("Identity is nil")
	}
	if ctx.Identity.Name != "Nexus" {
		t.Errorf("Identity.Name = %q, want %q", ctx.Identity.Name, "Nexus")
	}
	if ctx.Identity.Creature != "Phoenix" {
		t.Errorf("Identity.Creature = %q, want %q", ctx.Identity.Creature, "Phoenix")
	}
	if ctx.Identity.Emoji != "🔥" {
		t.Errorf("Identity.Emoji = %q, want %q", ctx.Identity.Emoji, "🔥")
	}

	if ctx.User == nil {
		t.Fatal("User is nil")
	}
	if ctx.User.Name != "Alice" {
		t.Errorf("User.Name = %q, want %q", ctx.User.Name, "Alice")
	}
	if ctx.User.PreferredAddress != "Ali" {
		t.Errorf("User.PreferredAddress = %q, want %q", ctx.User.PreferredAddress, "Ali")
	}
	if ctx.User.Timezone != "PST" {
		t.Errorf("User.Timezone = %q, want %q", ctx.User.Timezone, "PST")
	}
}

func TestLoadWorkspace_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// No files created - should not error
	ctx, err := LoadWorkspace(LoaderConfig{Root: tmpDir})
	if err != nil {
		t.Fatalf("LoadWorkspace error: %v", err)
	}

	if ctx.SoulContent != "" {
		t.Errorf("SoulContent should be empty for missing file")
	}
	if ctx.Identity != nil {
		t.Errorf("Identity should be nil for missing file")
	}
}

func TestParseIdentity(t *testing.T) {
	content := `# IDENTITY.md - Agent Identity

- Name: TestBot
- Creature: Dragon
- Vibe: Chill
- Emoji: 🐉
`
	id := parseIdentity(content)

	if id.Name != "TestBot" {
		t.Errorf("Name = %q, want %q", id.Name, "TestBot")
	}
	if id.Creature != "Dragon" {
		t.Errorf("Creature = %q, want %q", id.Creature, "Dragon")
	}
	if id.Vibe != "Chill" {
		t.Errorf("Vibe = %q, want %q", id.Vibe, "Chill")
	}
	if id.Emoji != "🐉" {
		t.Errorf("Emoji = %q, want %q", id.Emoji, "🐉")
	}
}

func TestParseUserProfile(t *testing.T) {
	content := `# USER.md - User Profile

- Name: Bob Smith
- Preferred address: Bob
- Pronouns (optional): he/him
- Timezone (optional): EST
- Notes: Likes coffee
`
	user := parseUserProfile(content)

	if user.Name != "Bob Smith" {
		t.Errorf("Name = %q, want %q", user.Name, "Bob Smith")
	}
	if user.PreferredAddress != "Bob" {
		t.Errorf("PreferredAddress = %q, want %q", user.PreferredAddress, "Bob")
	}
	if user.Pronouns != "he/him" {
		t.Errorf("Pronouns = %q, want %q", user.Pronouns, "he/him")
	}
	if user.Timezone != "EST" {
		t.Errorf("Timezone = %q, want %q", user.Timezone, "EST")
	}
	if user.Notes != "Likes coffee" {
		t.Errorf("Notes = %q, want %q", user.Notes, "Likes coffee")
	}
}

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		input       string
		expectedKey string
		expectedVal string
	}{
		{"- Name: Alice", "Name", "Alice"},
		{"Name: Bob", "Name", "Bob"},
		{"  - Key: Value  ", "Key", "Value"},
		{"No colon here", "", ""},
		{"Empty:", "Empty", ""},
		{": NoKey", "", "NoKey"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			key, val := parseKeyValue(tt.input)
			if key != tt.expectedKey {
				t.Errorf("key = %q, want %q", key, tt.expectedKey)
			}
			if val != tt.expectedVal {
				t.Errorf("val = %q, want %q", val, tt.expectedVal)
			}
		})
	}
}

func TestWorkspaceContext_SystemPromptContext(t *testing.T) {
	t.Run("with all data", func(t *testing.T) {
		ctx := &WorkspaceContext{
			SoulContent: "Be helpful.",
			Identity: &Identity{
				Name:     "Nexus",
				Creature: "Phoenix",
				Vibe:     "Calm",
				Emoji:    "🔥",
			},
			User: &UserProfile{
				Name:             "Alice",
				PreferredAddress: "Ali",
				Timezone:         "PST",
			},
		}

		prompt := ctx.SystemPromptContext()

		if !strings.Contains(prompt, "Be helpful") {
			t.Error("should contain soul content")
		}
		if !strings.Contains(prompt, "Your name is Nexus") {
			t.Error("should contain identity name")
		}
		if !strings.Contains(prompt, "You are a Phoenix") {
			t.Error("should contain creature")
		}
		if !strings.Contains(prompt, "talking to Alice") {
			t.Error("should contain user name")
		}
		if !strings.Contains(prompt, "address them as Ali") {
			t.Error("should contain preferred address")
		}
		if !strings.Contains(prompt, "timezone is PST") {
			t.Error("should contain timezone")
		}
	})

	t.Run("empty context", func(t *testing.T) {
		ctx := &WorkspaceContext{}
		prompt := ctx.SystemPromptContext()
		if prompt != "" {
			t.Errorf("expected empty prompt, got %q", prompt)
		}
	})

	t.Run("user without preferred address uses name", func(t *testing.T) {
		ctx := &WorkspaceContext{
			User: &UserProfile{Name: "Alice"},
		}
		prompt := ctx.SystemPromptContext()
		if !strings.Contains(prompt, "address them as Alice") {
			t.Errorf("should use name as address, got %q", prompt)
		}
	})
}

func TestLoadSoul(t *testing.T) {
	tmpDir := t.TempDir()
	content := "# SOUL.md\nBe awesome."
	os.WriteFile(filepath.Join(tmpDir, "SOUL.md"), []byte(content), 0644)

	soul, err := LoadSoul(tmpDir, "")
	if err != nil {
		t.Fatalf("LoadSoul error: %v", err)
	}
	if soul != content {
		t.Errorf("soul = %q, want %q", soul, content)
	}
}

func TestLoadIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	content := "- Name: Bot\n- Emoji: 🤖"
	os.WriteFile(filepath.Join(tmpDir, "IDENTITY.md"), []byte(content), 0644)

	id, err := LoadIdentity(tmpDir, "")
	if err != nil {
		t.Fatalf("LoadIdentity error: %v", err)
	}
	if id.Name != "Bot" {
		t.Errorf("Name = %q, want %q", id.Name, "Bot")
	}
	if id.Emoji != "🤖" {
		t.Errorf("Emoji = %q, want %q", id.Emoji, "🤖")
	}
}

func TestLoadUser(t *testing.T) {
	tmpDir := t.TempDir()
	content := "- Name: User\n- Timezone: UTC"
	os.WriteFile(filepath.Join(tmpDir, "USER.md"), []byte(content), 0644)

	user, err := LoadUser(tmpDir, "")
	if err != nil {
		t.Fatalf("LoadUser error: %v", err)
	}
	if user.Name != "User" {
		t.Errorf("Name = %q, want %q", user.Name, "User")
	}
	if user.Timezone != "UTC" {
		t.Errorf("Timezone = %q, want %q", user.Timezone, "UTC")
	}
}

func TestLoadMemory(t *testing.T) {
	tmpDir := t.TempDir()
	content := "# Memory\n\nRemember this."
	os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte(content), 0644)

	mem, err := LoadMemory(tmpDir, "")
	if err != nil {
		t.Fatalf("LoadMemory error: %v", err)
	}
	if mem != content {
		t.Errorf("memory = %q, want %q", mem, content)
	}
}
