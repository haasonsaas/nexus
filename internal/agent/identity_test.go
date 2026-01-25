package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIdentityMarkdown_Basic(t *testing.T) {
	content := `# Agent Identity

- **Name**: Atlas
- **Emoji**: 🤖
- **Creature**: AI assistant
- **Vibe**: sharp and helpful
- **Theme**: dark
- **Avatar**: /assets/avatar.png
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "Atlas" {
		t.Errorf("expected Name 'Atlas', got '%s'", id.Name)
	}
	if id.Emoji != "🤖" {
		t.Errorf("expected Emoji '🤖', got '%s'", id.Emoji)
	}
	if id.Creature != "AI assistant" {
		t.Errorf("expected Creature 'AI assistant', got '%s'", id.Creature)
	}
	if id.Vibe != "sharp and helpful" {
		t.Errorf("expected Vibe 'sharp and helpful', got '%s'", id.Vibe)
	}
	if id.Theme != "dark" {
		t.Errorf("expected Theme 'dark', got '%s'", id.Theme)
	}
	if id.Avatar != "/assets/avatar.png" {
		t.Errorf("expected Avatar '/assets/avatar.png', got '%s'", id.Avatar)
	}
}

func TestParseIdentityMarkdown_WithPlaceholders(t *testing.T) {
	content := `# Agent Identity

- **Name**: Atlas
- **Emoji**: pick something you like
- **Creature**: AI? Robot? Familiar? Ghost in the machine? Something weirder?
- **Vibe**: How do you come across? Sharp? Warm? Chaotic? Calm?
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "Atlas" {
		t.Errorf("expected Name 'Atlas', got '%s'", id.Name)
	}
	// Placeholders should be ignored
	if id.Emoji != "" {
		t.Errorf("expected Emoji to be empty (placeholder), got '%s'", id.Emoji)
	}
	if id.Creature != "" {
		t.Errorf("expected Creature to be empty (placeholder), got '%s'", id.Creature)
	}
	if id.Vibe != "" {
		t.Errorf("expected Vibe to be empty (placeholder), got '%s'", id.Vibe)
	}
}

func TestParseIdentityMarkdown_Empty(t *testing.T) {
	content := `# Agent Identity

Just some text without any key-value pairs.
`

	id := ParseIdentityMarkdown(content)
	if id != nil {
		t.Errorf("expected nil identity for empty content, got %+v", id)
	}
}

func TestParseIdentityMarkdown_AllPlaceholders(t *testing.T) {
	content := `# Agent Identity

- **Name**:
- **Emoji**: pick something you like
- **Avatar**: workspace-relative path, http(s) url, or data uri
`

	id := ParseIdentityMarkdown(content)
	if id != nil {
		t.Errorf("expected nil identity when all values are placeholders, got %+v", id)
	}
}

func TestParseIdentityMarkdown_AsteriskBullets(t *testing.T) {
	content := `# Identity

* **Name**: Nexus
* **Theme**: cyberpunk
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "Nexus" {
		t.Errorf("expected Name 'Nexus', got '%s'", id.Name)
	}
	if id.Theme != "cyberpunk" {
		t.Errorf("expected Theme 'cyberpunk', got '%s'", id.Theme)
	}
}

func TestParseIdentityMarkdown_WithoutBold(t *testing.T) {
	content := `# Identity

- Name: Simple Bot
- Emoji: 🧠
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "Simple Bot" {
		t.Errorf("expected Name 'Simple Bot', got '%s'", id.Name)
	}
	if id.Emoji != "🧠" {
		t.Errorf("expected Emoji '🧠', got '%s'", id.Emoji)
	}
}

func TestParseIdentityMarkdown_URLAvatar(t *testing.T) {
	content := `# Identity

- **Name**: CloudBot
- **Avatar**: https://example.com/avatar.png
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Avatar != "https://example.com/avatar.png" {
		t.Errorf("expected Avatar URL, got '%s'", id.Avatar)
	}
}

func TestParseIdentityMarkdown_DataURIAvatar(t *testing.T) {
	content := `# Identity

- **Name**: DataBot
- **Avatar**: data:image/png;base64,iVBORw0KGgo=
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Avatar != "data:image/png;base64,iVBORw0KGgo=" {
		t.Errorf("expected data URI avatar, got '%s'", id.Avatar)
	}
}

func TestParseIdentityMarkdown_CaseInsensitiveKeys(t *testing.T) {
	content := `# Identity

- **NAME**: AllCaps
- **theme**: lowercase
- **ViBeS**: mixed case
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "AllCaps" {
		t.Errorf("expected Name 'AllCaps', got '%s'", id.Name)
	}
	if id.Theme != "lowercase" {
		t.Errorf("expected Theme 'lowercase', got '%s'", id.Theme)
	}
	// "vibes" doesn't match "vibe", so should be empty
	if id.Vibe != "" {
		t.Errorf("expected Vibe to be empty (key mismatch), got '%s'", id.Vibe)
	}
}

func TestIdentity_HasValues(t *testing.T) {
	tests := []struct {
		name     string
		identity *Identity
		expected bool
	}{
		{"nil identity", nil, false},
		{"empty identity", &Identity{}, false},
		{"name only", &Identity{Name: "Test"}, true},
		{"emoji only", &Identity{Emoji: "🤖"}, true},
		{"theme only", &Identity{Theme: "dark"}, true},
		{"creature only", &Identity{Creature: "robot"}, true},
		{"vibe only", &Identity{Vibe: "calm"}, true},
		{"avatar only", &Identity{Avatar: "/path"}, true},
		{"all fields", &Identity{
			Name:     "Test",
			Emoji:    "🤖",
			Theme:    "dark",
			Creature: "robot",
			Vibe:     "calm",
			Avatar:   "/path",
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.identity.HasValues()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestLoadIdentityFromFile(t *testing.T) {
	// Create a temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "IDENTITY.md")

	content := `# Test Identity

- **Name**: FileBot
- **Emoji**: 📁
`

	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	id, err := LoadIdentityFromFile(testFile)
	if err != nil {
		t.Fatalf("LoadIdentityFromFile failed: %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "FileBot" {
		t.Errorf("expected Name 'FileBot', got '%s'", id.Name)
	}
	if id.Emoji != "📁" {
		t.Errorf("expected Emoji '📁', got '%s'", id.Emoji)
	}
}

func TestLoadIdentityFromFile_NotFound(t *testing.T) {
	_, err := LoadIdentityFromFile("/nonexistent/path/IDENTITY.md")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadIdentityFromWorkspace(t *testing.T) {
	// Create a temp workspace
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, DefaultIdentityFilename)

	content := `# Workspace Identity

- **Name**: WorkspaceBot
- **Theme**: professional
`

	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	id, err := LoadIdentityFromWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("LoadIdentityFromWorkspace failed: %v", err)
	}
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "WorkspaceBot" {
		t.Errorf("expected Name 'WorkspaceBot', got '%s'", id.Name)
	}
	if id.Theme != "professional" {
		t.Errorf("expected Theme 'professional', got '%s'", id.Theme)
	}
}

func TestLoadIdentityFromWorkspace_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := LoadIdentityFromWorkspace(tmpDir)
	if err == nil {
		t.Error("expected error when IDENTITY.md doesn't exist")
	}
}

func TestParseIdentityMarkdown_QuotedValues(t *testing.T) {
	content := `# Identity

- **Name**: "Quoted Name"
- **Vibe**: 'single quoted'
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "Quoted Name" {
		t.Errorf("expected Name 'Quoted Name', got '%s'", id.Name)
	}
	if id.Vibe != "single quoted" {
		t.Errorf("expected Vibe 'single quoted', got '%s'", id.Vibe)
	}
}

func TestParseIdentityMarkdown_ExtraWhitespace(t *testing.T) {
	content := `# Identity

-   **Name**:    Spacey Bot
- **Theme**:	tabbed
`

	id := ParseIdentityMarkdown(content)
	if id == nil {
		t.Fatal("expected non-nil identity")
	}

	if id.Name != "Spacey Bot" {
		t.Errorf("expected Name 'Spacey Bot', got '%s'", id.Name)
	}
	if id.Theme != "tabbed" {
		t.Errorf("expected Theme 'tabbed', got '%s'", id.Theme)
	}
}

func TestStripMarkdownBold(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"**bold**", "bold"},
		{"not bold", "not bold"},
		{"**partial", "partial"},
		{"partial**", "partial"},
		{"", ""},
	}

	for _, tt := range tests {
		result := stripMarkdownBold(tt.input)
		if result != tt.expected {
			t.Errorf("stripMarkdownBold(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  trimmed  ", "trimmed"},
		{`"quoted"`, "quoted"},
		{`'single'`, "single"},
		{"with // comment", "with"},
		{"https://example.com", "https://example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		result := normalizeValue(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeValue(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsPlaceholder(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", true},
		{"pick something you like", true},
		{"PICK SOMETHING YOU LIKE", true},
		{"actual value", false},
		{"workspace-relative path, http(s) url, or data uri", true},
	}

	for _, tt := range tests {
		result := isPlaceholder(tt.input)
		if result != tt.expected {
			t.Errorf("isPlaceholder(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}
