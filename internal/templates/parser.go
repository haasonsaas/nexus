package templates

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
	// TemplateFilename is the expected filename for template definitions.
	TemplateFilename = "TEMPLATE.md"

	// FrontmatterDelimiter marks the beginning and end of YAML frontmatter.
	FrontmatterDelimiter = "---"
)

// ParseTemplateFile parses a TEMPLATE.md file and returns an AgentTemplate.
func ParseTemplateFile(path string) (*AgentTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return ParseTemplate(data, filepath.Dir(path))
}

// ParseTemplate parses TEMPLATE.md content and returns an AgentTemplate.
func ParseTemplate(data []byte, templatePath string) (*AgentTemplate, error) {
	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("split frontmatter: %w", err)
	}

	var tmpl AgentTemplate
	if err := yaml.Unmarshal(frontmatter, &tmpl); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	// Validate required fields
	if tmpl.Name == "" {
		return nil, fmt.Errorf("template name is required")
	}
	if tmpl.Description == "" {
		return nil, fmt.Errorf("template description is required")
	}

	tmpl.Content = strings.TrimSpace(string(body))
	tmpl.Path = templatePath

	return &tmpl, nil
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

// ValidateTemplate checks if a template is valid.
func ValidateTemplate(tmpl *AgentTemplate) error {
	if tmpl.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Validate name format: lowercase, hyphens, no spaces
	for _, r := range tmpl.Name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("name must be lowercase alphanumeric with hyphens: got %q", tmpl.Name)
		}
	}

	if tmpl.Description == "" {
		return fmt.Errorf("description is required")
	}

	// Validate variables
	varNames := make(map[string]struct{})
	for i, v := range tmpl.Variables {
		if v.Name == "" {
			return fmt.Errorf("variable %d: name is required", i)
		}
		if _, exists := varNames[v.Name]; exists {
			return fmt.Errorf("duplicate variable name: %s", v.Name)
		}
		varNames[v.Name] = struct{}{}

		if err := validateVariable(&v); err != nil {
			return fmt.Errorf("variable %q: %w", v.Name, err)
		}
	}

	return nil
}

// validateVariable checks if a template variable is valid.
func validateVariable(v *TemplateVariable) error {
	// Validate variable name format
	for _, r := range v.Name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return fmt.Errorf("name must be alphanumeric with underscores: got %q", v.Name)
		}
	}

	// Validate type
	switch v.Type {
	case VariableTypeString, VariableTypeNumber, VariableTypeBoolean, VariableTypeArray, VariableTypeObject:
		// Valid types
	case "":
		// Default to string if not specified
	default:
		return fmt.Errorf("invalid type: %s", v.Type)
	}

	// Validate default value matches type
	if v.Default != nil && v.Type != "" {
		if err := validateValueType(v.Default, v.Type); err != nil {
			return fmt.Errorf("default value: %w", err)
		}
	}

	// Validate options match type
	for i, opt := range v.Options {
		if v.Type != "" {
			if err := validateValueType(opt, v.Type); err != nil {
				return fmt.Errorf("option %d: %w", i, err)
			}
		}
	}

	return nil
}

// validateValueType checks if a value matches the expected type.
func validateValueType(value any, expectedType VariableType) error {
	switch expectedType {
	case VariableTypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case VariableTypeNumber:
		switch value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			// Valid numeric types
		default:
			return fmt.Errorf("expected number, got %T", value)
		}
	case VariableTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case VariableTypeArray:
		switch value.(type) {
		case []any, []string, []int, []float64:
			// Valid array types
		default:
			return fmt.Errorf("expected array, got %T", value)
		}
	case VariableTypeObject:
		switch value.(type) {
		case map[string]any, map[string]string:
			// Valid object types
		default:
			return fmt.Errorf("expected object, got %T", value)
		}
	}
	return nil
}

// ParseTemplateFromReader parses a template from an io.Reader.
func ParseTemplateFromReader(r *bufio.Reader, templatePath string) (*AgentTemplate, error) {
	data, err := readAll(r)
	if err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}
	return ParseTemplate(data, templatePath)
}

// readAll reads all data from a bufio.Reader.
func readAll(r *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExtractVariablesFromContent extracts variable names used in the content.
// Looks for {{.varname}} patterns.
func ExtractVariablesFromContent(content string) []string {
	var variables []string
	seen := make(map[string]struct{})

	// Simple extraction - looks for {{.name}} or {{ .name }} patterns
	i := 0
	for i < len(content) {
		// Find {{
		start := strings.Index(content[i:], "{{")
		if start == -1 {
			break
		}
		start += i

		// Find }}
		end := strings.Index(content[start:], "}}")
		if end == -1 {
			break
		}
		end += start

		// Extract the expression
		expr := strings.TrimSpace(content[start+2 : end])

		// Check if it's a simple variable reference (.name)
		if strings.HasPrefix(expr, ".") && !strings.Contains(expr, " ") {
			varName := strings.TrimPrefix(expr, ".")
			// Handle nested paths like .agent.name - just take the first part
			if idx := strings.Index(varName, "."); idx != -1 {
				varName = varName[:idx]
			}
			if varName != "" {
				if _, exists := seen[varName]; !exists {
					seen[varName] = struct{}{}
					variables = append(variables, varName)
				}
			}
		}

		i = end + 2
	}

	return variables
}
