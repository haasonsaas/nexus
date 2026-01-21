package skills

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// SkillFilename is the expected filename for skill definitions.
	SkillFilename = "SKILL.md"

	// FrontmatterDelimiter marks the beginning and end of YAML frontmatter.
	FrontmatterDelimiter = "---"
)

// ParseSkillFile parses a SKILL.md file and returns a SkillEntry.
func ParseSkillFile(path string) (*SkillEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return ParseSkill(data, filepath.Dir(path))
}

// ParseSkill parses SKILL.md content and returns a SkillEntry.
func ParseSkill(data []byte, skillPath string) (*SkillEntry, error) {
	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("split frontmatter: %w", err)
	}

	var entry SkillEntry
	if err := yaml.Unmarshal(frontmatter, &entry); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	// Validate required fields
	if entry.Name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	if entry.Description == "" {
		return nil, fmt.Errorf("skill description is required")
	}

	entry.Content = strings.TrimSpace(string(body))
	entry.Path = skillPath

	return &entry, nil
}

// splitFrontmatter separates YAML frontmatter from markdown body.
// Returns (frontmatter, body, error).
func splitFrontmatter(data []byte) ([]byte, []byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	// Find opening delimiter
	if !scanner.Scan() {
		return nil, nil, fmt.Errorf("empty file")
	}
	firstLine := strings.TrimSpace(scanner.Text())
	if firstLine != FrontmatterDelimiter {
		return nil, nil, fmt.Errorf("missing opening frontmatter delimiter")
	}

	// Read frontmatter until closing delimiter
	var frontmatterLines []string
	foundClosing := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == FrontmatterDelimiter {
			foundClosing = true
			break
		}
		frontmatterLines = append(frontmatterLines, line)
	}

	if !foundClosing {
		return nil, nil, fmt.Errorf("missing closing frontmatter delimiter")
	}

	// Read remaining content as body
	var bodyLines []string
	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scanner error: %w", err)
	}

	frontmatter := []byte(strings.Join(frontmatterLines, "\n"))
	body := []byte(strings.Join(bodyLines, "\n"))

	return frontmatter, body, nil
}

// ValidateSkill checks if a skill entry is valid.
func ValidateSkill(entry *SkillEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Validate name format: lowercase, hyphens, no spaces
	for _, r := range entry.Name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("name must be lowercase alphanumeric with hyphens: got %q", entry.Name)
		}
	}

	if entry.Description == "" {
		return fmt.Errorf("description is required")
	}

	return nil
}

// ExpandBaseDir replaces {baseDir} placeholders in skill content.
func ExpandBaseDir(content string, baseDir string) string {
	return strings.ReplaceAll(content, "{baseDir}", baseDir)
}
