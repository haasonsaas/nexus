package templates

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strings"
	"text/template"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// VariableEngine handles variable substitution in templates.
type VariableEngine struct {
	// FuncMap contains custom template functions.
	FuncMap template.FuncMap

	// Delimiters for template parsing (default: {{ }}).
	LeftDelim  string
	RightDelim string
}

// NewVariableEngine creates a new variable engine with default settings.
func NewVariableEngine() *VariableEngine {
	return &VariableEngine{
		FuncMap:    defaultFuncMap(),
		LeftDelim:  "{{",
		RightDelim: "}}",
	}
}

// Process applies variable substitution to a template string.
func (e *VariableEngine) Process(tmplStr string, vars map[string]any) (string, error) {
	if tmplStr == "" {
		return "", nil
	}

	// Create a new template with our settings
	t := template.New("template")
	t.Funcs(e.FuncMap)

	if e.LeftDelim != "" && e.RightDelim != "" {
		t.Delims(e.LeftDelim, e.RightDelim)
	}

	// Parse the template
	parsed, err := t.Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	// Execute the template
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// ProcessWithMissingKeyError processes a template, returning an error for missing keys.
func (e *VariableEngine) ProcessWithMissingKeyError(tmplStr string, vars map[string]any) (string, error) {
	if tmplStr == "" {
		return "", nil
	}

	t := template.New("template")
	t.Funcs(e.FuncMap)
	t.Option("missingkey=error")

	if e.LeftDelim != "" && e.RightDelim != "" {
		t.Delims(e.LeftDelim, e.RightDelim)
	}

	parsed, err := t.Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := parsed.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// ValidatePattern validates a string against a regex pattern.
func (e *VariableEngine) ValidatePattern(pattern, value string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	if !re.MatchString(value) {
		return fmt.Errorf("value does not match pattern %q", pattern)
	}
	return nil
}

// ExtractVariables extracts variable names from a template string.
func (e *VariableEngine) ExtractVariables(tmplStr string) []string {
	return ExtractVariablesFromContent(tmplStr)
}

// defaultFuncMap returns the default template function map.
func defaultFuncMap() template.FuncMap {
	titleCase := cases.Title(language.Und)
	return template.FuncMap{
		// String functions
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"title":      titleCase.String,
		"trim":       strings.TrimSpace,
		"trimPrefix": strings.TrimPrefix,
		"trimSuffix": strings.TrimSuffix,
		"replace":    strings.ReplaceAll,
		"contains":   strings.Contains,
		"hasPrefix":  strings.HasPrefix,
		"hasSuffix":  strings.HasSuffix,
		"split":      strings.Split,
		"join":       strings.Join,
		"repeat":     strings.Repeat,

		// Conditional functions
		"default": defaultValue,
		"coalesce": func(values ...any) any {
			for _, v := range values {
				if v != nil && v != "" {
					return v
				}
			}
			return nil
		},
		"ternary": func(condition bool, trueVal, falseVal any) any {
			if condition {
				return trueVal
			}
			return falseVal
		},

		// Type conversion
		"toString": toString,
		"toInt":    toInt,
		"toBool":   toBool,

		// List functions
		"first": func(list []any) any {
			if len(list) > 0 {
				return list[0]
			}
			return nil
		},
		"last": func(list []any) any {
			if len(list) > 0 {
				return list[len(list)-1]
			}
			return nil
		},
		"len": func(v any) int {
			switch val := v.(type) {
			case string:
				return len(val)
			case []any:
				return len(val)
			case []string:
				return len(val)
			case map[string]any:
				return len(val)
			default:
				return 0
			}
		},

		// Formatting
		"indent": indent,
		"nindent": func(spaces int, s string) string {
			return "\n" + indent(spaces, s)
		},
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
		"squote": func(s string) string {
			return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
		},

		// Date/time (using string format)
		"now": func() string {
			return time.Now().UTC().Format(time.RFC3339)
		},

		// Markdown helpers
		"codeBlock": func(lang, code string) string {
			return fmt.Sprintf("```%s\n%s\n```", lang, code)
		},
		"bullet": func(items []string) string {
			var lines []string
			for _, item := range items {
				lines = append(lines, "- "+item)
			}
			return strings.Join(lines, "\n")
		},
		"numbered": func(items []string) string {
			var lines []string
			for i, item := range items {
				lines = append(lines, fmt.Sprintf("%d. %s", i+1, item))
			}
			return strings.Join(lines, "\n")
		},
	}
}

// defaultValue returns the default value if the input is empty.
func defaultValue(def, value any) any {
	if value == nil {
		return def
	}
	if str, ok := value.(string); ok && str == "" {
		return def
	}
	return value
}

// toString converts a value to string.
func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// toInt converts a value to int.
func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int8:
		return int(val)
	case int16:
		return int(val)
	case int32:
		return int(val)
	case int64:
		if val > int64(math.MaxInt) {
			return math.MaxInt
		}
		if val < int64(math.MinInt) {
			return math.MinInt
		}
		// #nosec G115 -- bounded by checks above
		return int(val)
	case uint:
		if val > uint(math.MaxInt) {
			return math.MaxInt
		}
		// #nosec G115 -- bounded by checks above
		return int(val)
	case uint8:
		return int(val)
	case uint16:
		return int(val)
	case uint32:
		return int(val)
	case uint64:
		if val > uint64(math.MaxInt) {
			return math.MaxInt
		}
		// #nosec G115 -- bounded by checks above
		return int(val)
	case float32:
		return int(val)
	case float64:
		return int(val)
	case string:
		var i int
		if _, err := fmt.Sscanf(val, "%d", &i); err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}

// toBool converts a value to bool.
func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "false" && val != "0" && val != "no"
	case int, int8, int16, int32, int64:
		return val != 0
	case uint, uint8, uint16, uint32, uint64:
		return val != 0
	case float32, float64:
		return val != 0
	default:
		return v != nil
	}
}

// indent adds indentation to each line of a string.
func indent(spaces int, s string) string {
	pad := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// VariableContext provides a structured context for template variables.
type VariableContext struct {
	// User-provided variables
	Variables map[string]any

	// Agent information
	Agent struct {
		ID   string
		Name string
	}

	// Template information
	Template struct {
		Name    string
		Version string
	}

	// Environment information
	Env map[string]string

	// Custom data
	Data map[string]any
}

// ToMap converts a VariableContext to a flat map for template execution.
func (vc *VariableContext) ToMap() map[string]any {
	result := make(map[string]any)

	// Copy variables
	for k, v := range vc.Variables {
		result[k] = v
	}

	// Add agent info
	result["agent_id"] = vc.Agent.ID
	result["agent_name"] = vc.Agent.Name

	// Add template info
	result["template_name"] = vc.Template.Name
	result["template_version"] = vc.Template.Version

	// Add env
	if vc.Env != nil {
		result["env"] = vc.Env
	}

	// Add custom data
	for k, v := range vc.Data {
		result[k] = v
	}

	return result
}

// NewVariableContext creates a new variable context.
func NewVariableContext() *VariableContext {
	return &VariableContext{
		Variables: make(map[string]any),
		Env:       make(map[string]string),
		Data:      make(map[string]any),
	}
}

// WithVariable adds a variable to the context.
func (vc *VariableContext) WithVariable(name string, value any) *VariableContext {
	vc.Variables[name] = value
	return vc
}

// WithVariables adds multiple variables to the context.
func (vc *VariableContext) WithVariables(vars map[string]any) *VariableContext {
	for k, v := range vars {
		vc.Variables[k] = v
	}
	return vc
}

// WithAgent sets the agent information.
func (vc *VariableContext) WithAgent(id, name string) *VariableContext {
	vc.Agent.ID = id
	vc.Agent.Name = name
	return vc
}

// WithTemplate sets the template information.
func (vc *VariableContext) WithTemplate(name, version string) *VariableContext {
	vc.Template.Name = name
	vc.Template.Version = version
	return vc
}
