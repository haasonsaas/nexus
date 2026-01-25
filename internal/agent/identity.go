package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultIdentityFilename is the standard filename for agent identity configuration.
const DefaultIdentityFilename = "IDENTITY.md"

// Identity represents an agent's persona configuration loaded from IDENTITY.md.
type Identity struct {
	Name     string // Agent's display name
	Emoji    string // Representative emoji
	Theme    string // Visual theme
	Creature string // AI type: robot, familiar, ghost, etc.
	Vibe     string // Personality: sharp, warm, chaotic, calm
	Avatar   string // Path to avatar image or URL
}

// identityPlaceholders are placeholder values that should be ignored during parsing.
var identityPlaceholders = map[string]bool{
	"pick something you like": true,
	"ai? robot? familiar? ghost in the machine? something weirder?": true,
	"how do you come across? sharp? warm? chaotic? calm?":           true,
	"your signature - pick one that feels right":                    true,
	"workspace-relative path, http(s) url, or data uri":             true,
}

// ParseIdentityMarkdown parses an IDENTITY.md file and extracts identity configuration.
// Returns nil if no valid values are found.
func ParseIdentityMarkdown(content string) *Identity {
	id := &Identity{}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and headers
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for list item pattern: - **Key**: Value
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
			continue
		}

		// Remove leading bullet
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)

		// Find key-value separator
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])

		// Strip markdown bold formatting from key
		key = stripMarkdownBold(key)
		key = strings.ToLower(key)

		// Normalize the value
		value = normalizeValue(value)

		// Skip placeholder values
		if isPlaceholder(value) {
			continue
		}

		// Map to struct fields
		switch key {
		case "name":
			id.Name = value
		case "emoji":
			id.Emoji = value
		case "theme":
			id.Theme = value
		case "creature":
			id.Creature = value
		case "vibe":
			id.Vibe = value
		case "avatar":
			id.Avatar = value
		}
	}

	if !id.HasValues() {
		return nil
	}

	return id
}

// LoadIdentityFromFile loads identity from a file path.
func LoadIdentityFromFile(path string) (*Identity, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ParseIdentityMarkdown(string(content)), nil
}

// LoadIdentityFromWorkspace loads IDENTITY.md from a workspace directory.
func LoadIdentityFromWorkspace(workspace string) (*Identity, error) {
	path := filepath.Join(workspace, DefaultIdentityFilename)
	return LoadIdentityFromFile(path)
}

// HasValues returns true if identity has any non-empty values.
func (i *Identity) HasValues() bool {
	if i == nil {
		return false
	}
	return i.Name != "" ||
		i.Emoji != "" ||
		i.Theme != "" ||
		i.Creature != "" ||
		i.Vibe != "" ||
		i.Avatar != ""
}

// stripMarkdownBold removes ** bold markers from a string.
func stripMarkdownBold(s string) string {
	s = strings.TrimPrefix(s, "**")
	s = strings.TrimSuffix(s, "**")
	return s
}

// normalizeValue cleans up a value string.
func normalizeValue(s string) string {
	// Trim whitespace
	s = strings.TrimSpace(s)

	// Remove surrounding quotes if present
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}

	// Remove trailing comments (but not URL protocol slashes)
	// Only strip // comments if preceded by whitespace
	if idx := strings.Index(s, " //"); idx > 0 {
		s = strings.TrimSpace(s[:idx])
	}

	return s
}

// isPlaceholder checks if a value is a known placeholder.
func isPlaceholder(value string) bool {
	if value == "" {
		return true
	}

	lower := strings.ToLower(value)
	return identityPlaceholders[lower]
}
