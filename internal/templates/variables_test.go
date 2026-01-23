package templates

import (
	"strings"
	"testing"
)

func TestNewVariableEngine(t *testing.T) {
	engine := NewVariableEngine()
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if engine.FuncMap == nil {
		t.Error("FuncMap should be initialized")
	}
	if engine.LeftDelim != "{{" {
		t.Errorf("LeftDelim = %q, want %q", engine.LeftDelim, "{{")
	}
	if engine.RightDelim != "}}" {
		t.Errorf("RightDelim = %q, want %q", engine.RightDelim, "}}")
	}
}

func TestVariableEngine_Process(t *testing.T) {
	engine := NewVariableEngine()

	tests := []struct {
		name     string
		template string
		vars     map[string]any
		want     string
		wantErr  bool
	}{
		{
			name:     "empty template",
			template: "",
			vars:     nil,
			want:     "",
		},
		{
			name:     "no variables",
			template: "Hello World",
			vars:     nil,
			want:     "Hello World",
		},
		{
			name:     "simple variable",
			template: "Hello {{.Name}}",
			vars:     map[string]any{"Name": "World"},
			want:     "Hello World",
		},
		{
			name:     "multiple variables",
			template: "{{.Greeting}} {{.Name}}!",
			vars:     map[string]any{"Greeting": "Hello", "Name": "User"},
			want:     "Hello User!",
		},
		{
			name:     "missing variable returns empty",
			template: "Hello {{.Missing}}",
			vars:     map[string]any{},
			want:     "Hello <no value>",
		},
		{
			name:     "upper function",
			template: `{{upper .Name}}`,
			vars:     map[string]any{"Name": "test"},
			want:     "TEST",
		},
		{
			name:     "lower function",
			template: `{{lower .Name}}`,
			vars:     map[string]any{"Name": "TEST"},
			want:     "test",
		},
		{
			name:     "trim function",
			template: `{{trim .Name}}`,
			vars:     map[string]any{"Name": "  hello  "},
			want:     "hello",
		},
		{
			name:     "replace function",
			template: `{{replace .Text "old" "new"}}`,
			vars:     map[string]any{"Text": "old value"},
			want:     "new value",
		},
		{
			name:     "default function with nil",
			template: `{{default "default" .Missing}}`,
			vars:     map[string]any{},
			want:     "default",
		},
		{
			name:     "default function with value",
			template: `{{default "default" .Value}}`,
			vars:     map[string]any{"Value": "actual"},
			want:     "actual",
		},
		{
			name:     "invalid template syntax",
			template: "{{.Name",
			vars:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.Process(tt.template, tt.vars)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.want {
				t.Errorf("result = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestVariableEngine_CustomDelimiters(t *testing.T) {
	engine := &VariableEngine{
		FuncMap:    defaultFuncMap(),
		LeftDelim:  "<<",
		RightDelim: ">>",
	}

	result, err := engine.Process("Hello <<.Name>>", map[string]any{"Name": "World"})
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	if result != "Hello World" {
		t.Errorf("result = %q, want %q", result, "Hello World")
	}

	// Original delimiters should not work
	result2, _ := engine.Process("Hello {{.Name}}", map[string]any{"Name": "World"})
	if result2 != "Hello {{.Name}}" {
		t.Errorf("original delimiters should not work, got %q", result2)
	}
}

func TestVariableEngine_ProcessWithMissingKeyError(t *testing.T) {
	engine := NewVariableEngine()

	t.Run("returns error for missing key", func(t *testing.T) {
		_, err := engine.ProcessWithMissingKeyError("Hello {{.Missing}}", map[string]any{})
		if err == nil {
			t.Error("expected error for missing key")
		}
	})

	t.Run("succeeds with all keys present", func(t *testing.T) {
		result, err := engine.ProcessWithMissingKeyError("Hello {{.Name}}", map[string]any{"Name": "World"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "Hello World" {
			t.Errorf("result = %q, want %q", result, "Hello World")
		}
	})

	t.Run("handles empty template", func(t *testing.T) {
		result, err := engine.ProcessWithMissingKeyError("", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "" {
			t.Errorf("result = %q, want empty", result)
		}
	})
}

func TestVariableEngine_ValidatePattern(t *testing.T) {
	engine := NewVariableEngine()

	tests := []struct {
		name    string
		pattern string
		value   string
		wantErr bool
	}{
		{
			name:    "valid email pattern",
			pattern: `^[a-z]+@[a-z]+\.[a-z]+$`,
			value:   "test@example.com",
			wantErr: false,
		},
		{
			name:    "invalid email pattern",
			pattern: `^[a-z]+@[a-z]+\.[a-z]+$`,
			value:   "not-an-email",
			wantErr: true,
		},
		{
			name:    "simple pattern match",
			pattern: `^hello`,
			value:   "hello world",
			wantErr: false,
		},
		{
			name:    "invalid regex pattern",
			pattern: `[invalid`,
			value:   "test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.ValidatePattern(tt.pattern, tt.value)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultValue(t *testing.T) {
	tests := []struct {
		name     string
		def      any
		value    any
		expected any
	}{
		{"nil value returns default", "default", nil, "default"},
		{"empty string returns default", "default", "", "default"},
		{"non-empty string returns value", "default", "actual", "actual"},
		{"non-string value returns value", "default", 123, 123},
		{"zero int still returns value", "default", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := defaultValue(tt.def, tt.value)
			if result != tt.expected {
				t.Errorf("result = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil returns empty", nil, ""},
		{"string passes through", "hello", "hello"},
		{"int converts", 42, "42"},
		{"float converts", 3.14, "3.14"},
		{"bool converts", true, "true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toString(tt.input)
			if result != tt.expected {
				t.Errorf("result = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{"int passes through", 42, 42},
		{"int8 converts", int8(10), 10},
		{"int16 converts", int16(100), 100},
		{"int32 converts", int32(1000), 1000},
		{"int64 converts", int64(10000), 10000},
		{"uint converts", uint(5), 5},
		{"uint8 converts", uint8(8), 8},
		{"uint16 converts", uint16(16), 16},
		{"uint32 converts", uint32(32), 32},
		{"uint64 converts", uint64(64), 64},
		{"float32 converts", float32(3.7), 3},
		{"float64 converts", float64(4.9), 4},
		{"string numeric converts", "123", 123},
		{"string non-numeric returns 0", "abc", 0},
		{"nil returns 0", nil, 0},
		{"other type returns 0", struct{}{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toInt(tt.input)
			if result != tt.expected {
				t.Errorf("result = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"non-empty string", "hello", true},
		{"empty string", "", false},
		{"string false", "false", false},
		{"string 0", "0", false},
		{"string no", "no", false},
		{"int non-zero", 1, true},
		{"int zero", 0, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toBool(tt.input)
			if result != tt.expected {
				t.Errorf("result = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIndent(t *testing.T) {
	tests := []struct {
		name     string
		spaces   int
		input    string
		expected string
	}{
		{"single line", 2, "hello", "  hello"},
		{"multiple lines", 4, "line1\nline2", "    line1\n    line2"},
		{"empty line preserved", 2, "line1\n\nline2", "  line1\n\n  line2"},
		{"zero spaces", 0, "hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indent(tt.spaces, tt.input)
			if result != tt.expected {
				t.Errorf("result = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDefaultFuncMap(t *testing.T) {
	funcMap := defaultFuncMap()

	// Verify key functions exist
	functions := []string{
		"upper", "lower", "title", "trim", "trimPrefix", "trimSuffix",
		"replace", "contains", "hasPrefix", "hasSuffix", "split", "join",
		"default", "coalesce", "ternary", "toString", "toInt", "toBool",
		"first", "last", "len", "indent", "nindent", "quote", "squote",
		"codeBlock", "bullet", "numbered",
	}

	for _, fn := range functions {
		if _, ok := funcMap[fn]; !ok {
			t.Errorf("missing function: %s", fn)
		}
	}

	// Test specific functions through template execution
	engine := &VariableEngine{FuncMap: funcMap, LeftDelim: "{{", RightDelim: "}}"}

	t.Run("coalesce returns first non-empty", func(t *testing.T) {
		result, _ := engine.Process(`{{coalesce "" nil "value"}}`, nil)
		if result != "value" {
			t.Errorf("coalesce result = %q, want %q", result, "value")
		}
	})

	t.Run("ternary works", func(t *testing.T) {
		result1, _ := engine.Process(`{{ternary true "yes" "no"}}`, nil)
		if result1 != "yes" {
			t.Errorf("ternary(true) = %q, want %q", result1, "yes")
		}
		result2, _ := engine.Process(`{{ternary false "yes" "no"}}`, nil)
		if result2 != "no" {
			t.Errorf("ternary(false) = %q, want %q", result2, "no")
		}
	})

	t.Run("quote function", func(t *testing.T) {
		result, _ := engine.Process(`{{quote "hello"}}`, nil)
		if result != `"hello"` {
			t.Errorf("quote result = %q, want %q", result, `"hello"`)
		}
	})

	t.Run("squote function", func(t *testing.T) {
		result, _ := engine.Process(`{{squote "hello"}}`, nil)
		if result != `'hello'` {
			t.Errorf("squote result = %q, want %q", result, `'hello'`)
		}
	})

	t.Run("codeBlock function", func(t *testing.T) {
		result, _ := engine.Process(`{{codeBlock "go" "fmt.Println()"}}`, nil)
		if !strings.Contains(result, "```go") || !strings.Contains(result, "fmt.Println()") {
			t.Errorf("codeBlock result = %q", result)
		}
	})
}

func TestNewVariableContext(t *testing.T) {
	ctx := NewVariableContext()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.Variables == nil {
		t.Error("Variables should be initialized")
	}
	if ctx.Env == nil {
		t.Error("Env should be initialized")
	}
	if ctx.Data == nil {
		t.Error("Data should be initialized")
	}
}

func TestVariableContext_WithMethods(t *testing.T) {
	ctx := NewVariableContext().
		WithVariable("key1", "value1").
		WithVariables(map[string]any{"key2": "value2", "key3": "value3"}).
		WithAgent("agent-1", "Test Agent").
		WithTemplate("test-template", "1.0.0")

	if ctx.Variables["key1"] != "value1" {
		t.Error("WithVariable failed")
	}
	if ctx.Variables["key2"] != "value2" || ctx.Variables["key3"] != "value3" {
		t.Error("WithVariables failed")
	}
	if ctx.Agent.ID != "agent-1" || ctx.Agent.Name != "Test Agent" {
		t.Error("WithAgent failed")
	}
	if ctx.Template.Name != "test-template" || ctx.Template.Version != "1.0.0" {
		t.Error("WithTemplate failed")
	}
}

func TestVariableContext_ToMap(t *testing.T) {
	ctx := NewVariableContext().
		WithVariable("custom", "value").
		WithAgent("agent-1", "Agent Name").
		WithTemplate("tmpl", "2.0")

	ctx.Env["FOO"] = "bar"
	ctx.Data["extra"] = "data"

	m := ctx.ToMap()

	if m["custom"] != "value" {
		t.Error("custom variable missing")
	}
	if m["agent_id"] != "agent-1" {
		t.Error("agent_id missing")
	}
	if m["agent_name"] != "Agent Name" {
		t.Error("agent_name missing")
	}
	if m["template_name"] != "tmpl" {
		t.Error("template_name missing")
	}
	if m["template_version"] != "2.0" {
		t.Error("template_version missing")
	}
	if env, ok := m["env"].(map[string]string); !ok || env["FOO"] != "bar" {
		t.Error("env missing or incorrect")
	}
	if m["extra"] != "data" {
		t.Error("extra data missing")
	}
}
