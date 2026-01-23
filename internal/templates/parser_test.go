package templates

import (
	"strings"
	"testing"
)

func TestParseTemplate(t *testing.T) {
	t.Run("valid template", func(t *testing.T) {
		data := []byte(`---
name: test-template
description: A test template
version: "1.0.0"
tags:
  - test
  - example
variables:
  - name: company_name
    type: string
    required: true
agent:
  model: gpt-4
---
# System Prompt

You are a helpful assistant for {{.company_name}}.
`)

		tmpl, err := ParseTemplate(data, "/path/to/template")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tmpl.Name != "test-template" {
			t.Errorf("Name = %q, want %q", tmpl.Name, "test-template")
		}
		if tmpl.Version != "1.0.0" {
			t.Errorf("Version = %q, want %q", tmpl.Version, "1.0.0")
		}
		if tmpl.Description != "A test template" {
			t.Errorf("Description = %q, want %q", tmpl.Description, "A test template")
		}
		if len(tmpl.Tags) != 2 {
			t.Errorf("Tags length = %d, want 2", len(tmpl.Tags))
		}
		if len(tmpl.Variables) != 1 {
			t.Errorf("Variables length = %d, want 1", len(tmpl.Variables))
		}
		if tmpl.Agent.Model != "gpt-4" {
			t.Errorf("Agent.Model = %q, want %q", tmpl.Agent.Model, "gpt-4")
		}
		if !strings.Contains(tmpl.Content, "helpful assistant") {
			t.Errorf("Content should contain 'helpful assistant', got %q", tmpl.Content)
		}
		if tmpl.Path != "/path/to/template" {
			t.Errorf("Path = %q, want %q", tmpl.Path, "/path/to/template")
		}
	})

	t.Run("missing name", func(t *testing.T) {
		data := []byte(`---
description: Test description
---
Content`)
		_, err := ParseTemplate(data, "")
		if err == nil {
			t.Error("expected error for missing name")
		}
		if !strings.Contains(err.Error(), "name is required") {
			t.Errorf("error should mention 'name is required', got %v", err)
		}
	})

	t.Run("missing description", func(t *testing.T) {
		data := []byte(`---
name: test-template
---
Content`)
		_, err := ParseTemplate(data, "")
		if err == nil {
			t.Error("expected error for missing description")
		}
		if !strings.Contains(err.Error(), "description is required") {
			t.Errorf("error should mention 'description is required', got %v", err)
		}
	})
}

func TestSplitFrontmatter(t *testing.T) {
	t.Run("valid frontmatter", func(t *testing.T) {
		data := []byte(`---
key: value
another: test
---
Body content here
More body content`)

		frontmatter, body, err := splitFrontmatter(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(string(frontmatter), "key: value") {
			t.Errorf("frontmatter should contain 'key: value', got %q", frontmatter)
		}
		if !strings.Contains(string(body), "Body content here") {
			t.Errorf("body should contain 'Body content here', got %q", body)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		data := []byte("")
		_, _, err := splitFrontmatter(data)
		if err == nil {
			t.Error("expected error for empty file")
		}
		if !strings.Contains(err.Error(), "empty file") {
			t.Errorf("error should mention 'empty file', got %v", err)
		}
	})

	t.Run("missing opening delimiter", func(t *testing.T) {
		data := []byte(`name: value
---
body`)
		_, _, err := splitFrontmatter(data)
		if err == nil {
			t.Error("expected error for missing opening delimiter")
		}
		if !strings.Contains(err.Error(), "missing opening frontmatter delimiter") {
			t.Errorf("error should mention opening delimiter, got %v", err)
		}
	})

	t.Run("missing closing delimiter", func(t *testing.T) {
		data := []byte(`---
key: value
body without closing`)
		_, _, err := splitFrontmatter(data)
		if err == nil {
			t.Error("expected error for missing closing delimiter")
		}
		if !strings.Contains(err.Error(), "missing closing frontmatter delimiter") {
			t.Errorf("error should mention closing delimiter, got %v", err)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		data := []byte(`---
key: value
---`)
		frontmatter, body, err := splitFrontmatter(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(frontmatter) != "key: value" {
			t.Errorf("frontmatter = %q, want %q", frontmatter, "key: value")
		}
		if string(body) != "" {
			t.Errorf("body should be empty, got %q", body)
		}
	})
}

func TestValidateTemplate(t *testing.T) {
	t.Run("valid template", func(t *testing.T) {
		tmpl := &AgentTemplate{
			Name:        "valid-template",
			Description: "A valid description",
			Variables: []TemplateVariable{
				{Name: "var1", Type: VariableTypeString},
			},
		}
		if err := ValidateTemplate(tmpl); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		tmpl := &AgentTemplate{Description: "Test"}
		err := ValidateTemplate(tmpl)
		if err == nil || !strings.Contains(err.Error(), "name is required") {
			t.Errorf("expected 'name is required' error, got %v", err)
		}
	})

	t.Run("invalid name format - uppercase", func(t *testing.T) {
		tmpl := &AgentTemplate{Name: "Invalid-Name", Description: "Test"}
		err := ValidateTemplate(tmpl)
		if err == nil || !strings.Contains(err.Error(), "lowercase") {
			t.Errorf("expected lowercase error, got %v", err)
		}
	})

	t.Run("invalid name format - spaces", func(t *testing.T) {
		tmpl := &AgentTemplate{Name: "invalid name", Description: "Test"}
		err := ValidateTemplate(tmpl)
		if err == nil || !strings.Contains(err.Error(), "lowercase") {
			t.Errorf("expected lowercase error, got %v", err)
		}
	})

	t.Run("valid name with hyphens and numbers", func(t *testing.T) {
		tmpl := &AgentTemplate{Name: "valid-name-123", Description: "Test"}
		if err := ValidateTemplate(tmpl); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing description", func(t *testing.T) {
		tmpl := &AgentTemplate{Name: "test-name"}
		err := ValidateTemplate(tmpl)
		if err == nil || !strings.Contains(err.Error(), "description is required") {
			t.Errorf("expected 'description is required' error, got %v", err)
		}
	})

	t.Run("duplicate variable names", func(t *testing.T) {
		tmpl := &AgentTemplate{
			Name:        "test",
			Description: "Test",
			Variables: []TemplateVariable{
				{Name: "var1", Type: VariableTypeString},
				{Name: "var1", Type: VariableTypeString},
			},
		}
		err := ValidateTemplate(tmpl)
		if err == nil || !strings.Contains(err.Error(), "duplicate variable name") {
			t.Errorf("expected duplicate variable error, got %v", err)
		}
	})

	t.Run("variable without name", func(t *testing.T) {
		tmpl := &AgentTemplate{
			Name:        "test",
			Description: "Test",
			Variables: []TemplateVariable{
				{Name: "", Type: VariableTypeString},
			},
		}
		err := ValidateTemplate(tmpl)
		if err == nil || !strings.Contains(err.Error(), "name is required") {
			t.Errorf("expected variable name required error, got %v", err)
		}
	})
}

func TestValidateVariable(t *testing.T) {
	t.Run("valid string variable", func(t *testing.T) {
		v := &TemplateVariable{Name: "test_var", Type: VariableTypeString}
		if err := validateVariable(v); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid variable name - special chars", func(t *testing.T) {
		v := &TemplateVariable{Name: "test-var", Type: VariableTypeString}
		err := validateVariable(v)
		if err == nil || !strings.Contains(err.Error(), "alphanumeric") {
			t.Errorf("expected alphanumeric error, got %v", err)
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		v := &TemplateVariable{Name: "test_var", Type: VariableType("invalid")}
		err := validateVariable(v)
		if err == nil || !strings.Contains(err.Error(), "invalid type") {
			t.Errorf("expected invalid type error, got %v", err)
		}
	})

	t.Run("empty type defaults to string", func(t *testing.T) {
		v := &TemplateVariable{Name: "test_var"}
		if err := validateVariable(v); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid default value", func(t *testing.T) {
		v := &TemplateVariable{Name: "test_var", Type: VariableTypeString, Default: "default"}
		if err := validateVariable(v); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid default value type", func(t *testing.T) {
		v := &TemplateVariable{Name: "test_var", Type: VariableTypeString, Default: 123}
		err := validateVariable(v)
		if err == nil || !strings.Contains(err.Error(), "expected string") {
			t.Errorf("expected type mismatch error, got %v", err)
		}
	})
}

func TestValidateValueType(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		varType VariableType
		wantErr bool
	}{
		// String tests
		{"string valid", "hello", VariableTypeString, false},
		{"string invalid", 123, VariableTypeString, true},

		// Number tests
		{"number int valid", 42, VariableTypeNumber, false},
		{"number float64 valid", 3.14, VariableTypeNumber, false},
		{"number int8 valid", int8(10), VariableTypeNumber, false},
		{"number int16 valid", int16(100), VariableTypeNumber, false},
		{"number int32 valid", int32(1000), VariableTypeNumber, false},
		{"number int64 valid", int64(10000), VariableTypeNumber, false},
		{"number uint valid", uint(5), VariableTypeNumber, false},
		{"number uint8 valid", uint8(8), VariableTypeNumber, false},
		{"number uint16 valid", uint16(16), VariableTypeNumber, false},
		{"number uint32 valid", uint32(32), VariableTypeNumber, false},
		{"number uint64 valid", uint64(64), VariableTypeNumber, false},
		{"number float32 valid", float32(3.14), VariableTypeNumber, false},
		{"number invalid", "not a number", VariableTypeNumber, true},

		// Boolean tests
		{"boolean valid true", true, VariableTypeBoolean, false},
		{"boolean valid false", false, VariableTypeBoolean, false},
		{"boolean invalid", "true", VariableTypeBoolean, true},

		// Array tests
		{"array []any valid", []any{1, 2, 3}, VariableTypeArray, false},
		{"array []string valid", []string{"a", "b"}, VariableTypeArray, false},
		{"array []int valid", []int{1, 2, 3}, VariableTypeArray, false},
		{"array []float64 valid", []float64{1.1, 2.2}, VariableTypeArray, false},
		{"array invalid", "not an array", VariableTypeArray, true},

		// Object tests
		{"object map[string]any valid", map[string]any{"k": "v"}, VariableTypeObject, false},
		{"object map[string]string valid", map[string]string{"k": "v"}, VariableTypeObject, false},
		{"object invalid", "not an object", VariableTypeObject, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateValueType(tt.value, tt.varType)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestExtractVariablesFromContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "simple variable",
			content:  "Hello {{.name}}!",
			expected: []string{"name"},
		},
		{
			name:     "multiple variables",
			content:  "{{.greeting}} {{.name}}! Welcome to {{.company}}.",
			expected: []string{"greeting", "name", "company"},
		},
		{
			name:     "duplicate variables",
			content:  "{{.name}} and {{.name}} again",
			expected: []string{"name"},
		},
		{
			name:     "nested path extracts first part",
			content:  "{{.agent.name}} and {{.config.timeout}}",
			expected: []string{"agent", "config"},
		},
		{
			name:     "with spaces in delimiters",
			content:  "{{ .name }} and {{  .other  }}",
			expected: []string{"name", "other"},
		},
		{
			name:     "function call ignored",
			content:  "{{upper .name}}",
			expected: []string{},
		},
		{
			name:     "no variables",
			content:  "Just plain text",
			expected: []string{},
		},
		{
			name:     "empty content",
			content:  "",
			expected: []string{},
		},
		{
			name:     "unclosed delimiter",
			content:  "{{.name",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractVariablesFromContent(tt.content)
			if len(result) != len(tt.expected) {
				t.Fatalf("got %d variables, want %d: %v vs %v", len(result), len(tt.expected), result, tt.expected)
			}
			for i, v := range tt.expected {
				if i >= len(result) || result[i] != v {
					t.Errorf("variable %d: got %q, want %q", i, result[i], v)
				}
			}
		})
	}
}

func TestTemplateFilename_Constant(t *testing.T) {
	if TemplateFilename != "TEMPLATE.md" {
		t.Errorf("TemplateFilename = %q, want %q", TemplateFilename, "TEMPLATE.md")
	}
}

func TestFrontmatterDelimiter_Constant(t *testing.T) {
	if FrontmatterDelimiter != "---" {
		t.Errorf("FrontmatterDelimiter = %q, want %q", FrontmatterDelimiter, "---")
	}
}
