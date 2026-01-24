package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillFile(t *testing.T) {
	t.Run("valid skill file", func(t *testing.T) {
		dir := t.TempDir()
		skillFile := filepath.Join(dir, SkillFilename)
		content := `---
name: test-skill
description: A test skill for testing
homepage: https://example.com
---

# Test Skill

This is the skill content.
`
		if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			t.Fatalf("ParseSkillFile error: %v", err)
		}

		if skill.Name != "test-skill" {
			t.Errorf("Name = %q, want %q", skill.Name, "test-skill")
		}
		if skill.Description != "A test skill for testing" {
			t.Errorf("Description = %q, want %q", skill.Description, "A test skill for testing")
		}
		if skill.Homepage != "https://example.com" {
			t.Errorf("Homepage = %q, want %q", skill.Homepage, "https://example.com")
		}
		if skill.Path != dir {
			t.Errorf("Path = %q, want %q", skill.Path, dir)
		}
		if !strings.Contains(skill.Content, "Test Skill") {
			t.Errorf("Content should contain 'Test Skill', got %q", skill.Content)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := ParseSkillFile("/nonexistent/path/SKILL.md")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
		if !strings.Contains(err.Error(), "read file") {
			t.Errorf("error should mention read file: %v", err)
		}
	})

	t.Run("skill with metadata", func(t *testing.T) {
		dir := t.TempDir()
		skillFile := filepath.Join(dir, SkillFilename)
		content := `---
name: advanced-skill
description: An advanced skill
metadata:
  emoji: "🚀"
  always: true
  os:
    - darwin
    - linux
  execution: edge
  requires:
    bins:
      - git
      - curl
    env:
      - API_KEY
---

# Advanced Skill
`
		if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			t.Fatalf("ParseSkillFile error: %v", err)
		}

		if skill.Metadata == nil {
			t.Fatal("Metadata should not be nil")
		}
		if skill.Metadata.Emoji != "🚀" {
			t.Errorf("Metadata.Emoji = %q", skill.Metadata.Emoji)
		}
		if !skill.Metadata.Always {
			t.Error("Metadata.Always should be true")
		}
		if len(skill.Metadata.OS) != 2 {
			t.Errorf("Metadata.OS length = %d, want 2", len(skill.Metadata.OS))
		}
		if skill.Metadata.Execution != ExecEdge {
			t.Errorf("Metadata.Execution = %q, want %q", skill.Metadata.Execution, ExecEdge)
		}
		if skill.Metadata.Requires == nil {
			t.Fatal("Metadata.Requires should not be nil")
		}
		if len(skill.Metadata.Requires.Bins) != 2 {
			t.Errorf("Metadata.Requires.Bins length = %d, want 2", len(skill.Metadata.Requires.Bins))
		}
	})
}

func TestParseSkill(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		skillPath   string
		wantName    string
		wantDesc    string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid minimal skill",
			data: `---
name: minimal
description: A minimal skill
---

Content here.
`,
			skillPath: "/skills/minimal",
			wantName:  "minimal",
			wantDesc:  "A minimal skill",
			wantErr:   false,
		},
		{
			name: "missing name",
			data: `---
description: A skill without a name
---

Content.
`,
			skillPath:   "/skills/test",
			wantErr:     true,
			errContains: "name is required",
		},
		{
			name: "missing description",
			data: `---
name: no-desc
---

Content.
`,
			skillPath:   "/skills/test",
			wantErr:     true,
			errContains: "description is required",
		},
		{
			name:        "empty data",
			data:        "",
			skillPath:   "/skills/test",
			wantErr:     true,
			errContains: "empty file",
		},
		{
			name:        "missing frontmatter",
			data:        "# Just markdown content",
			skillPath:   "/skills/test",
			wantErr:     true,
			errContains: "missing opening frontmatter delimiter",
		},
		{
			name: "unclosed frontmatter",
			data: `---
name: test
description: test
`,
			skillPath:   "/skills/test",
			wantErr:     true,
			errContains: "missing closing frontmatter delimiter",
		},
		{
			name: "invalid yaml",
			data: `---
name: [invalid yaml
description: test
---

Content.
`,
			skillPath:   "/skills/test",
			wantErr:     true,
			errContains: "parse frontmatter",
		},
		{
			name: "skill with all fields",
			data: `---
name: full-skill
description: A full skill
homepage: https://example.com
metadata:
  emoji: "⚡"
  primaryEnv: MY_API_KEY
  skillKey: custom-key
  toolGroups:
    - browser
    - filesystem
---

# Full Skill

This is comprehensive content.
`,
			skillPath: "/skills/full",
			wantName:  "full-skill",
			wantDesc:  "A full skill",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skill, err := ParseSkill([]byte(tt.data), tt.skillPath)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if skill.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", skill.Name, tt.wantName)
			}
			if skill.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", skill.Description, tt.wantDesc)
			}
			if skill.Path != tt.skillPath {
				t.Errorf("Path = %q, want %q", skill.Path, tt.skillPath)
			}
		})
	}
}

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		data            string
		wantFrontmatter string
		wantBody        string
		wantErr         bool
		errContains     string
	}{
		{
			name: "standard frontmatter",
			data: `---
name: test
description: test
---

# Body content
More content here.
`,
			wantFrontmatter: "name: test\ndescription: test",
			wantBody:        "\n# Body content\nMore content here.",
			wantErr:         false,
		},
		{
			name:        "empty input",
			data:        "",
			wantErr:     true,
			errContains: "empty file",
		},
		{
			name:        "no frontmatter",
			data:        "# Just markdown",
			wantErr:     true,
			errContains: "missing opening frontmatter delimiter",
		},
		{
			name:        "only opening delimiter",
			data:        "---\nsome content",
			wantErr:     true,
			errContains: "missing closing frontmatter delimiter",
		},
		{
			name: "empty frontmatter",
			data: `---
---

Body only.
`,
			wantFrontmatter: "",
			wantBody:        "\nBody only.",
			wantErr:         false,
		},
		{
			name: "frontmatter with spaces around delimiter",
			data: `   ---
name: test
   ---
Body.
`,
			wantFrontmatter: "name: test",
			wantBody:        "Body.",
			wantErr:         false,
		},
		{
			name: "multiline body",
			data: `---
key: value
---

Line 1
Line 2
Line 3
`,
			wantFrontmatter: "key: value",
			wantBody:        "\nLine 1\nLine 2\nLine 3",
			wantErr:         false,
		},
		{
			name: "body with triple dashes",
			data: `---
name: test
---

Content with --- in it
More content.
`,
			wantFrontmatter: "name: test",
			wantBody:        "\nContent with --- in it\nMore content.",
			wantErr:         false,
		},
		{
			name: "empty body",
			data: `---
name: test
---
`,
			wantFrontmatter: "name: test",
			wantBody:        "",
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter, body, err := splitFrontmatter([]byte(tt.data))

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(frontmatter) != tt.wantFrontmatter {
				t.Errorf("frontmatter = %q, want %q", string(frontmatter), tt.wantFrontmatter)
			}
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", string(body), tt.wantBody)
			}
		})
	}
}

func TestValidateSkill(t *testing.T) {
	tests := []struct {
		name        string
		skill       *SkillEntry
		wantErr     bool
		errContains string
	}{
		{
			name: "valid skill",
			skill: &SkillEntry{
				Name:        "valid-skill",
				Description: "A valid skill",
			},
			wantErr: false,
		},
		{
			name: "valid skill with numbers",
			skill: &SkillEntry{
				Name:        "skill-v2-beta3",
				Description: "A skill with numbers",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			skill: &SkillEntry{
				Name:        "",
				Description: "Has description",
			},
			wantErr:     true,
			errContains: "name is required",
		},
		{
			name: "uppercase in name",
			skill: &SkillEntry{
				Name:        "InvalidName",
				Description: "Has description",
			},
			wantErr:     true,
			errContains: "must be lowercase",
		},
		{
			name: "spaces in name",
			skill: &SkillEntry{
				Name:        "invalid name",
				Description: "Has description",
			},
			wantErr:     true,
			errContains: "must be lowercase",
		},
		{
			name: "underscores in name",
			skill: &SkillEntry{
				Name:        "invalid_name",
				Description: "Has description",
			},
			wantErr:     true,
			errContains: "must be lowercase",
		},
		{
			name: "special characters in name",
			skill: &SkillEntry{
				Name:        "invalid@name",
				Description: "Has description",
			},
			wantErr:     true,
			errContains: "must be lowercase",
		},
		{
			name: "empty description",
			skill: &SkillEntry{
				Name:        "valid-name",
				Description: "",
			},
			wantErr:     true,
			errContains: "description is required",
		},
		{
			name: "single character name",
			skill: &SkillEntry{
				Name:        "a",
				Description: "Single char name",
			},
			wantErr: false,
		},
		{
			name: "name with only hyphens and numbers",
			skill: &SkillEntry{
				Name:        "123-456",
				Description: "Numeric name",
			},
			wantErr: false,
		},
		{
			name: "name starting with hyphen",
			skill: &SkillEntry{
				Name:        "-starts-with-hyphen",
				Description: "Hyphen start",
			},
			wantErr: false, // Current implementation allows this
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSkill(tt.skill)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestExpandBaseDir(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		baseDir  string
		expected string
	}{
		{
			name:     "single placeholder",
			content:  "Read file at {baseDir}/config.yaml",
			baseDir:  "/home/user/project",
			expected: "Read file at /home/user/project/config.yaml",
		},
		{
			name:     "multiple placeholders",
			content:  "{baseDir}/src and {baseDir}/test",
			baseDir:  "/project",
			expected: "/project/src and /project/test",
		},
		{
			name:     "no placeholders",
			content:  "No placeholders here",
			baseDir:  "/some/path",
			expected: "No placeholders here",
		},
		{
			name:     "empty content",
			content:  "",
			baseDir:  "/path",
			expected: "",
		},
		{
			name:     "empty baseDir",
			content:  "{baseDir}/file",
			baseDir:  "",
			expected: "/file",
		},
		{
			name:     "placeholder in middle of word",
			content:  "prefix{baseDir}suffix",
			baseDir:  "/dir",
			expected: "prefix/dirsuffix",
		},
		{
			name:     "multiline content",
			content:  "Line 1: {baseDir}/a\nLine 2: {baseDir}/b",
			baseDir:  "/root",
			expected: "Line 1: /root/a\nLine 2: /root/b",
		},
		{
			name:     "case sensitive",
			content:  "{BASEDIR} vs {baseDir}",
			baseDir:  "/path",
			expected: "{BASEDIR} vs /path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandBaseDir(tt.content, tt.baseDir)
			if result != tt.expected {
				t.Errorf("ExpandBaseDir() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseSkillFile_EdgeCases(t *testing.T) {
	t.Run("skill with install specs", func(t *testing.T) {
		dir := t.TempDir()
		skillFile := filepath.Join(dir, SkillFilename)
		content := `---
name: installable-skill
description: A skill with install instructions
metadata:
  install:
    - id: brew
      kind: brew
      formula: mytool
      bins:
        - mytool
      label: Install with Homebrew
      os:
        - darwin
    - id: apt
      kind: apt
      package: mytool
      bins:
        - mytool
      os:
        - linux
---

# Installable Skill
`
		if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			t.Fatalf("ParseSkillFile error: %v", err)
		}

		if skill.Metadata == nil {
			t.Fatal("Metadata should not be nil")
		}
		if len(skill.Metadata.Install) != 2 {
			t.Errorf("Install specs length = %d, want 2", len(skill.Metadata.Install))
		}

		brewSpec := skill.Metadata.Install[0]
		if brewSpec.ID != "brew" {
			t.Errorf("First install ID = %q, want 'brew'", brewSpec.ID)
		}
		if brewSpec.Formula != "mytool" {
			t.Errorf("Formula = %q, want 'mytool'", brewSpec.Formula)
		}
	})

	t.Run("skill with tool groups", func(t *testing.T) {
		dir := t.TempDir()
		skillFile := filepath.Join(dir, SkillFilename)
		content := `---
name: browser-skill
description: A skill requiring browser tools
metadata:
  toolGroups:
    - browser
    - filesystem
    - network
---

# Browser Skill
`
		if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			t.Fatalf("ParseSkillFile error: %v", err)
		}

		if skill.Metadata == nil {
			t.Fatal("Metadata should not be nil")
		}
		groups := skill.RequiredToolGroups()
		if len(groups) != 3 {
			t.Errorf("ToolGroups length = %d, want 3", len(groups))
		}
	})

	t.Run("skill with requires anyBins", func(t *testing.T) {
		dir := t.TempDir()
		skillFile := filepath.Join(dir, SkillFilename)
		content := `---
name: editor-skill
description: A skill requiring an editor
metadata:
  requires:
    anyBins:
      - vim
      - nvim
      - nano
---

# Editor Skill
`
		if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			t.Fatalf("ParseSkillFile error: %v", err)
		}

		if skill.Metadata == nil || skill.Metadata.Requires == nil {
			t.Fatal("Metadata.Requires should not be nil")
		}
		if len(skill.Metadata.Requires.AnyBins) != 3 {
			t.Errorf("AnyBins length = %d, want 3", len(skill.Metadata.Requires.AnyBins))
		}
	})

	t.Run("content trimming", func(t *testing.T) {
		dir := t.TempDir()
		skillFile := filepath.Join(dir, SkillFilename)
		content := `---
name: trim-test
description: Test content trimming
---


   Content with whitespace


`
		if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			t.Fatalf("ParseSkillFile error: %v", err)
		}

		if strings.HasPrefix(skill.Content, "\n") || strings.HasSuffix(skill.Content, "\n") {
			t.Errorf("Content should be trimmed, got %q", skill.Content)
		}
		if !strings.Contains(skill.Content, "Content with whitespace") {
			t.Errorf("Content should contain inner text, got %q", skill.Content)
		}
	})
}

func TestConstants(t *testing.T) {
	if SkillFilename != "SKILL.md" {
		t.Errorf("SkillFilename = %q, want %q", SkillFilename, "SKILL.md")
	}
	if FrontmatterDelimiter != "---" {
		t.Errorf("FrontmatterDelimiter = %q, want %q", FrontmatterDelimiter, "---")
	}
}
