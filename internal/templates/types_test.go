package templates

import (
	"testing"
	"time"
)

func TestVariableType_Constants(t *testing.T) {
	tests := []struct {
		constant VariableType
		expected string
	}{
		{VariableTypeString, "string"},
		{VariableTypeNumber, "number"},
		{VariableTypeBoolean, "boolean"},
		{VariableTypeArray, "array"},
		{VariableTypeObject, "object"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestSourceType_Constants(t *testing.T) {
	tests := []struct {
		constant SourceType
		expected string
	}{
		{SourceBuiltin, "builtin"},
		{SourceLocal, "local"},
		{SourceWorkspace, "workspace"},
		{SourceExtra, "extra"},
		{SourceGit, "git"},
		{SourceRegistry, "registry"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestAgentTemplate_ConfigKey(t *testing.T) {
	tmpl := &AgentTemplate{Name: "my-template"}
	if key := tmpl.ConfigKey(); key != "my-template" {
		t.Errorf("ConfigKey() = %q, want %q", key, "my-template")
	}
}

func TestAgentTemplate_IsEnabled(t *testing.T) {
	tmpl := &AgentTemplate{Name: "test-template"}

	t.Run("enabled by default when no override", func(t *testing.T) {
		overrides := map[string]*TemplateConfig{}
		if !tmpl.IsEnabled(overrides) {
			t.Error("expected enabled by default")
		}
	})

	t.Run("enabled when override is nil", func(t *testing.T) {
		overrides := map[string]*TemplateConfig{
			"test-template": {Enabled: nil},
		}
		if !tmpl.IsEnabled(overrides) {
			t.Error("expected enabled when override.Enabled is nil")
		}
	})

	t.Run("enabled when override is true", func(t *testing.T) {
		enabled := true
		overrides := map[string]*TemplateConfig{
			"test-template": {Enabled: &enabled},
		}
		if !tmpl.IsEnabled(overrides) {
			t.Error("expected enabled when override is true")
		}
	})

	t.Run("disabled when override is false", func(t *testing.T) {
		disabled := false
		overrides := map[string]*TemplateConfig{
			"test-template": {Enabled: &disabled},
		}
		if tmpl.IsEnabled(overrides) {
			t.Error("expected disabled when override is false")
		}
	})
}

func TestAgentTemplate_ToSnapshot(t *testing.T) {
	tmpl := &AgentTemplate{
		Name:        "test-template",
		Version:     "1.0.0",
		Description: "Test description",
		Tags:        []string{"tag1", "tag2"},
		Source:      SourceLocal,
		Path:        "/path/to/template",
	}

	snapshot := tmpl.ToSnapshot()

	if snapshot.Name != tmpl.Name {
		t.Errorf("Name = %q, want %q", snapshot.Name, tmpl.Name)
	}
	if snapshot.Version != tmpl.Version {
		t.Errorf("Version = %q, want %q", snapshot.Version, tmpl.Version)
	}
	if snapshot.Description != tmpl.Description {
		t.Errorf("Description = %q, want %q", snapshot.Description, tmpl.Description)
	}
	if len(snapshot.Tags) != len(tmpl.Tags) {
		t.Errorf("Tags length = %d, want %d", len(snapshot.Tags), len(tmpl.Tags))
	}
	if snapshot.Source != tmpl.Source {
		t.Errorf("Source = %v, want %v", snapshot.Source, tmpl.Source)
	}
	if snapshot.Path != tmpl.Path {
		t.Errorf("Path = %q, want %q", snapshot.Path, tmpl.Path)
	}
}

func TestAgentTemplate_HasVariable(t *testing.T) {
	tmpl := &AgentTemplate{
		Variables: []TemplateVariable{
			{Name: "var1"},
			{Name: "var2"},
		},
	}

	t.Run("returns true for existing variable", func(t *testing.T) {
		if !tmpl.HasVariable("var1") {
			t.Error("expected HasVariable('var1') to be true")
		}
	})

	t.Run("returns false for non-existing variable", func(t *testing.T) {
		if tmpl.HasVariable("var3") {
			t.Error("expected HasVariable('var3') to be false")
		}
	})
}

func TestAgentTemplate_GetVariable(t *testing.T) {
	tmpl := &AgentTemplate{
		Variables: []TemplateVariable{
			{Name: "var1", Description: "First variable"},
			{Name: "var2", Description: "Second variable"},
		},
	}

	t.Run("returns variable when exists", func(t *testing.T) {
		v := tmpl.GetVariable("var1")
		if v == nil {
			t.Fatal("expected non-nil variable")
		}
		if v.Description != "First variable" {
			t.Errorf("Description = %q, want %q", v.Description, "First variable")
		}
	})

	t.Run("returns nil when not exists", func(t *testing.T) {
		v := tmpl.GetVariable("var3")
		if v != nil {
			t.Error("expected nil for non-existing variable")
		}
	})
}

func TestAgentTemplate_GetRequiredVariables(t *testing.T) {
	tmpl := &AgentTemplate{
		Variables: []TemplateVariable{
			{Name: "required_no_default", Required: true, Default: nil},
			{Name: "required_with_default", Required: true, Default: "default"},
			{Name: "optional", Required: false},
			{Name: "another_required", Required: true, Default: nil},
		},
	}

	required := tmpl.GetRequiredVariables()

	if len(required) != 2 {
		t.Fatalf("expected 2 required variables, got %d", len(required))
	}

	names := make(map[string]bool)
	for _, v := range required {
		names[v.Name] = true
	}

	if !names["required_no_default"] {
		t.Error("missing required_no_default")
	}
	if !names["another_required"] {
		t.Error("missing another_required")
	}
	if names["required_with_default"] {
		t.Error("required_with_default should not be included (has default)")
	}
}

func TestTemplateVariable_Struct(t *testing.T) {
	v := TemplateVariable{
		Name:        "test_var",
		Description: "A test variable",
		Type:        VariableTypeString,
		Default:     "default_value",
		Required:    true,
		Sensitive:   true,
		Options:     []any{"option1", "option2"},
	}

	if v.Name != "test_var" {
		t.Errorf("Name = %q, want %q", v.Name, "test_var")
	}
	if v.Type != VariableTypeString {
		t.Errorf("Type = %v, want %v", v.Type, VariableTypeString)
	}
	if !v.Required {
		t.Error("Required should be true")
	}
	if !v.Sensitive {
		t.Error("Sensitive should be true")
	}
}

func TestVariableValidation_Struct(t *testing.T) {
	minLen := 5
	maxLen := 100
	minVal := 0.0
	maxVal := 100.0
	minItems := 1
	maxItems := 10

	validation := VariableValidation{
		Pattern:   "^[a-z]+$",
		MinLength: &minLen,
		MaxLength: &maxLen,
		Min:       &minVal,
		Max:       &maxVal,
		MinItems:  &minItems,
		MaxItems:  &maxItems,
	}

	if validation.Pattern != "^[a-z]+$" {
		t.Errorf("Pattern = %q, want %q", validation.Pattern, "^[a-z]+$")
	}
	if *validation.MinLength != 5 {
		t.Errorf("MinLength = %d, want 5", *validation.MinLength)
	}
	if *validation.MaxLength != 100 {
		t.Errorf("MaxLength = %d, want 100", *validation.MaxLength)
	}
}

func TestMCPServerRef_Struct(t *testing.T) {
	ref := MCPServerRef{
		Name:     "test-server",
		Command:  "test-command",
		Args:     []string{"--arg1", "--arg2"},
		Env:      map[string]string{"KEY": "VALUE"},
		URL:      "http://localhost:8080",
		Required: true,
		Tools:    []string{"tool1", "tool2"},
	}

	if ref.Name != "test-server" {
		t.Errorf("Name = %q, want %q", ref.Name, "test-server")
	}
	if len(ref.Args) != 2 {
		t.Errorf("Args length = %d, want 2", len(ref.Args))
	}
	if !ref.Required {
		t.Error("Required should be true")
	}
}

func TestAgentTemplateSpec_Struct(t *testing.T) {
	temp := 0.7
	spec := AgentTemplateSpec{
		Model:              "gpt-4",
		Provider:           "openai",
		Tools:              []string{"tool1", "tool2"},
		CanReceiveHandoffs: true,
		MaxIterations:      10,
		Temperature:        &temp,
		MaxTokens:          4096,
		Metadata:           map[string]any{"key": "value"},
	}

	if spec.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", spec.Model, "gpt-4")
	}
	if spec.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10", spec.MaxIterations)
	}
	if *spec.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", *spec.Temperature)
	}
}

func TestAgentTemplate_Struct(t *testing.T) {
	now := time.Now()
	tmpl := AgentTemplate{
		Name:           "test-template",
		Version:        "1.0.0",
		Description:    "A test template",
		Author:         "Test Author",
		Homepage:       "https://example.com",
		Tags:           []string{"test", "example"},
		Content:        "# Test Content",
		Path:           "/path/to/template",
		Source:         SourceLocal,
		SourcePriority: 10,
		CreatedAt:      &now,
		UpdatedAt:      &now,
	}

	if tmpl.Name != "test-template" {
		t.Errorf("Name = %q, want %q", tmpl.Name, "test-template")
	}
	if tmpl.SourcePriority != 10 {
		t.Errorf("SourcePriority = %d, want 10", tmpl.SourcePriority)
	}
}

func TestTemplateSnapshot_Struct(t *testing.T) {
	snapshot := TemplateSnapshot{
		Name:        "snapshot-template",
		Version:     "2.0.0",
		Description: "Snapshot description",
		Tags:        []string{"snapshot"},
		Source:      SourceGit,
		Path:        "/git/path",
	}

	if snapshot.Name != "snapshot-template" {
		t.Errorf("Name = %q, want %q", snapshot.Name, "snapshot-template")
	}
	if snapshot.Source != SourceGit {
		t.Errorf("Source = %v, want %v", snapshot.Source, SourceGit)
	}
}

func TestSourceConfig_Struct(t *testing.T) {
	cfg := SourceConfig{
		Type:    SourceGit,
		Path:    "/local/path",
		URL:     "https://github.com/example/repo",
		Branch:  "main",
		SubPath: "templates",
		Refresh: 30 * time.Minute,
		Auth:    "token",
	}

	if cfg.Type != SourceGit {
		t.Errorf("Type = %v, want %v", cfg.Type, SourceGit)
	}
	if cfg.Branch != "main" {
		t.Errorf("Branch = %q, want %q", cfg.Branch, "main")
	}
	if cfg.Refresh != 30*time.Minute {
		t.Errorf("Refresh = %v, want %v", cfg.Refresh, 30*time.Minute)
	}
}

func TestLoadConfig_Struct(t *testing.T) {
	cfg := LoadConfig{
		ExtraDirs:       []string{"/extra1", "/extra2"},
		Watch:           true,
		WatchDebounceMs: 500,
	}

	if len(cfg.ExtraDirs) != 2 {
		t.Errorf("ExtraDirs length = %d, want 2", len(cfg.ExtraDirs))
	}
	if !cfg.Watch {
		t.Error("Watch should be true")
	}
	if cfg.WatchDebounceMs != 500 {
		t.Errorf("WatchDebounceMs = %d, want 500", cfg.WatchDebounceMs)
	}
}

func TestInstantiationRequest_Struct(t *testing.T) {
	req := InstantiationRequest{
		TemplateName: "my-template",
		AgentID:      "agent-123",
		AgentName:    "My Agent",
		Variables:    map[string]any{"key": "value"},
	}

	if req.TemplateName != "my-template" {
		t.Errorf("TemplateName = %q, want %q", req.TemplateName, "my-template")
	}
	if req.AgentID != "agent-123" {
		t.Errorf("AgentID = %q, want %q", req.AgentID, "agent-123")
	}
}

func TestInstantiationResult_Struct(t *testing.T) {
	result := InstantiationResult{
		UsedVariables: map[string]any{"var1": "value1"},
		Warnings:      []string{"warning1", "warning2"},
	}

	if len(result.UsedVariables) != 1 {
		t.Errorf("UsedVariables length = %d, want 1", len(result.UsedVariables))
	}
	if len(result.Warnings) != 2 {
		t.Errorf("Warnings length = %d, want 2", len(result.Warnings))
	}
}
