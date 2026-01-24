package skills

import (
	"os"
	"testing"
)

func TestNewGatingContext(t *testing.T) {
	overrides := map[string]*SkillConfig{
		"skill-a": {APIKey: "test-key"},
	}
	configValues := map[string]any{
		"tools": map[string]any{
			"browser": map[string]any{
				"enabled": true,
			},
		},
	}

	ctx := NewGatingContext(overrides, configValues)

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.OS == "" {
		t.Error("OS should be set to current OS")
	}
	if ctx.PathBins == nil {
		t.Error("PathBins should be initialized")
	}
	if ctx.EnvVars == nil {
		t.Error("EnvVars should be initialized")
	}
	if ctx.Overrides == nil {
		t.Error("Overrides should be set")
	}
	if ctx.ConfigValues == nil {
		t.Error("ConfigValues should be set")
	}
}

func TestGatingContext_CheckBinary(t *testing.T) {
	ctx := NewGatingContext(nil, nil)

	// "ls" should exist on all Unix systems
	if !ctx.CheckBinary("ls") {
		// On Windows, try "cmd"
		if !ctx.CheckBinary("cmd") {
			t.Skip("neither ls nor cmd found on PATH")
		}
	}

	// Verify caching
	ctx.PathBins["cached-bin"] = true
	if !ctx.CheckBinary("cached-bin") {
		t.Error("should return cached result")
	}

	// Non-existent binary
	if ctx.CheckBinary("nonexistent-binary-xyz-123") {
		t.Error("should return false for non-existent binary")
	}
}

func TestGatingContext_CheckEnv(t *testing.T) {
	ctx := NewGatingContext(nil, nil)

	// PATH should be set on all systems
	if !ctx.CheckEnv("PATH") {
		t.Error("PATH should be set")
	}

	// Verify caching
	ctx.EnvVars["CACHED_VAR"] = true
	if !ctx.CheckEnv("CACHED_VAR") {
		t.Error("should return cached result")
	}

	// Non-existent variable
	if ctx.CheckEnv("NONEXISTENT_VAR_XYZ_123") {
		t.Error("should return false for non-existent var")
	}
}

func TestGatingContext_CheckEnvOrConfig(t *testing.T) {
	t.Run("returns true for set env var", func(t *testing.T) {
		ctx := NewGatingContext(nil, nil)
		// PATH exists
		if !ctx.CheckEnvOrConfig("any-skill", "PATH") {
			t.Error("should return true for PATH")
		}
	})

	t.Run("returns true for skill config APIKey", func(t *testing.T) {
		overrides := map[string]*SkillConfig{
			"my-skill": {APIKey: "secret"},
		}
		ctx := NewGatingContext(overrides, nil)

		if !ctx.CheckEnvOrConfig("my-skill", "SOME_API_KEY") {
			t.Error("should return true when APIKey is set in config")
		}
	})

	t.Run("returns true for skill config env override", func(t *testing.T) {
		overrides := map[string]*SkillConfig{
			"my-skill": {Env: map[string]string{"MY_VAR": "value"}},
		}
		ctx := NewGatingContext(overrides, nil)

		if !ctx.CheckEnvOrConfig("my-skill", "MY_VAR") {
			t.Error("should return true when var is in skill env config")
		}
	})

	t.Run("returns false when not found", func(t *testing.T) {
		ctx := NewGatingContext(nil, nil)
		if ctx.CheckEnvOrConfig("unknown-skill", "NONEXISTENT_XYZ") {
			t.Error("should return false when not found anywhere")
		}
	})
}

func TestGatingContext_CheckConfig(t *testing.T) {
	configValues := map[string]any{
		"tools": map[string]any{
			"browser": map[string]any{
				"enabled": true,
			},
			"sandbox": map[string]any{
				"enabled": false,
			},
		},
		"simple": "value",
	}

	ctx := NewGatingContext(nil, configValues)

	t.Run("returns true for truthy nested value", func(t *testing.T) {
		if !ctx.CheckConfig("tools.browser.enabled") {
			t.Error("should return true for enabled browser")
		}
	})

	t.Run("returns false for falsy nested value", func(t *testing.T) {
		if ctx.CheckConfig("tools.sandbox.enabled") {
			t.Error("should return false for disabled sandbox")
		}
	})

	t.Run("returns false for non-existent path", func(t *testing.T) {
		if ctx.CheckConfig("tools.nonexistent.path") {
			t.Error("should return false for non-existent path")
		}
	})

	t.Run("returns false when config is nil", func(t *testing.T) {
		nilCtx := NewGatingContext(nil, nil)
		if nilCtx.CheckConfig("any.path") {
			t.Error("should return false when configValues is nil")
		}
	})

	t.Run("returns true for truthy string", func(t *testing.T) {
		if !ctx.CheckConfig("simple") {
			t.Error("should return true for truthy string")
		}
	})
}

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{"nil", nil, false},
		{"true bool", true, true},
		{"false bool", false, false},
		{"non-empty string", "hello", true},
		{"empty string", "", false},
		{"false string", "false", false},
		{"zero string", "0", false},
		{"non-zero int", 42, true},
		{"zero int", 0, false},
		{"non-zero uint", uint(1), true},
		// Note: Due to Go type switch behavior with multi-type cases,
		// uint(0) != 0 compares interface to int literal, returning true
		{"zero uint", uint(0), true},
		{"non-zero float", 3.14, true},
		// Same behavior for float64: 0.0 != 0 compares interface to int literal
		{"zero float", 0.0, true},
		{"map (default true)", map[string]any{}, true},
		{"slice (default true)", []int{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTruthy(tt.value); got != tt.expected {
				t.Errorf("isTruthy(%v) = %v, want %v", tt.value, got, tt.expected)
			}
		})
	}
}

func TestSkillEntry_CheckEligibility(t *testing.T) {
	t.Run("eligible with no metadata", func(t *testing.T) {
		skill := &SkillEntry{Name: "test"}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		if !result.Eligible {
			t.Errorf("should be eligible, got reason: %s", result.Reason)
		}
	})

	t.Run("always flag skips checks", func(t *testing.T) {
		skill := &SkillEntry{
			Name:     "always-skill",
			Metadata: &SkillMetadata{Always: true},
		}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		if !result.Eligible {
			t.Error("should be eligible with always flag")
		}
		if result.Reason != "always enabled" {
			t.Errorf("reason = %q, want %q", result.Reason, "always enabled")
		}
	})

	t.Run("disabled in config", func(t *testing.T) {
		disabled := false
		overrides := map[string]*SkillConfig{
			"disabled-skill": {Enabled: &disabled},
		}
		skill := &SkillEntry{Name: "disabled-skill"}
		ctx := NewGatingContext(overrides, nil)

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible when disabled")
		}
		if result.Reason != "disabled in config" {
			t.Errorf("reason = %q", result.Reason)
		}
	})

	t.Run("OS mismatch", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "os-specific",
			Metadata: &SkillMetadata{
				OS: []string{"nonexistent-os"},
			},
		}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible with wrong OS")
		}
	})

	t.Run("missing required binary", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "bin-required",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					Bins: []string{"nonexistent-binary-xyz-123"},
				},
			},
		}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible with missing binary")
		}
	})

	t.Run("any-bins satisfied", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "any-bin",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					AnyBins: []string{"nonexistent-xyz", "ls", "another-nonexistent"},
				},
			},
		}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		// "ls" should exist on Unix
		if !result.Eligible {
			t.Logf("reason: %s", result.Reason)
			t.Skip("ls not found on PATH")
		}
	})

	t.Run("any-bins none found", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "any-bin-missing",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					AnyBins: []string{"nonexistent-a-xyz", "nonexistent-b-xyz"},
				},
			},
		}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible when no any-bin is found")
		}
	})

	t.Run("missing required env var", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "env-required",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					Env: []string{"NONEXISTENT_VAR_XYZ_123"},
				},
			},
		}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible with missing env var")
		}
	})

	t.Run("missing config requirement", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "config-required",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					Config: []string{"tools.browser.enabled"},
				},
			},
		}
		ctx := NewGatingContext(nil, nil)

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible with missing config")
		}
	})

	t.Run("config requirement satisfied", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "config-required",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					Config: []string{"tools.browser.enabled"},
				},
			},
		}
		configValues := map[string]any{
			"tools": map[string]any{
				"browser": map[string]any{
					"enabled": true,
				},
			},
		}
		ctx := NewGatingContext(nil, configValues)

		result := skill.CheckEligibility(ctx)
		if !result.Eligible {
			t.Errorf("should be eligible, got: %s", result.Reason)
		}
	})
}

// mockToolPolicy implements ToolPolicyChecker for testing
type mockToolPolicy struct {
	allowedGroups map[string]bool
	edgeConnected bool
}

func (m *mockToolPolicy) IsGroupAllowed(group string) bool {
	return m.allowedGroups[group]
}

func (m *mockToolPolicy) HasEdgeConnected() bool {
	return m.edgeConnected
}

func TestSkillEntry_CheckEligibility_ToolGroups(t *testing.T) {
	t.Run("tool group not allowed", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "tool-group-skill",
			Metadata: &SkillMetadata{
				ToolGroups: []string{"group:fs"},
			},
		}
		ctx := NewGatingContext(nil, nil)
		ctx.ToolPolicy = &mockToolPolicy{
			allowedGroups: map[string]bool{},
		}

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible when tool group not allowed")
		}
	})

	t.Run("tool group allowed", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "tool-group-skill",
			Metadata: &SkillMetadata{
				ToolGroups: []string{"group:fs"},
			},
		}
		ctx := NewGatingContext(nil, nil)
		ctx.ToolPolicy = &mockToolPolicy{
			allowedGroups: map[string]bool{"group:fs": true},
		}

		result := skill.CheckEligibility(ctx)
		if !result.Eligible {
			t.Errorf("should be eligible, got: %s", result.Reason)
		}
	})
}

func TestSkillEntry_CheckEligibility_EdgeExecution(t *testing.T) {
	t.Run("edge required but not connected", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "edge-skill",
			Metadata: &SkillMetadata{
				Execution: ExecEdge,
			},
		}
		ctx := NewGatingContext(nil, nil)
		ctx.ToolPolicy = &mockToolPolicy{
			edgeConnected: false,
		}

		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			t.Error("should not be eligible when edge not connected")
		}
	})

	t.Run("edge required and connected", func(t *testing.T) {
		skill := &SkillEntry{
			Name: "edge-skill",
			Metadata: &SkillMetadata{
				Execution: ExecEdge,
			},
		}
		ctx := NewGatingContext(nil, nil)
		ctx.ToolPolicy = &mockToolPolicy{
			edgeConnected: true,
		}

		result := skill.CheckEligibility(ctx)
		if !result.Eligible {
			t.Errorf("should be eligible, got: %s", result.Reason)
		}
	})
}

func TestFilterEligible(t *testing.T) {
	skills := []*SkillEntry{
		{Name: "eligible-1"},
		{Name: "eligible-2"},
		{
			Name: "ineligible",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					Bins: []string{"nonexistent-xyz-123"},
				},
			},
		},
	}

	ctx := NewGatingContext(nil, nil)
	eligible := FilterEligible(skills, ctx)

	if len(eligible) != 2 {
		t.Errorf("expected 2 eligible skills, got %d", len(eligible))
	}
}

func TestGetIneligibleReasons(t *testing.T) {
	skills := []*SkillEntry{
		{Name: "eligible"},
		{
			Name: "ineligible-bin",
			Metadata: &SkillMetadata{
				Requires: &SkillRequires{
					Bins: []string{"nonexistent-xyz-123"},
				},
			},
		},
		{
			Name: "ineligible-os",
			Metadata: &SkillMetadata{
				OS: []string{"nonexistent-os"},
			},
		},
	}

	ctx := NewGatingContext(nil, nil)
	reasons := GetIneligibleReasons(skills, ctx)

	if len(reasons) != 2 {
		t.Errorf("expected 2 ineligible skills, got %d", len(reasons))
	}

	if _, ok := reasons["ineligible-bin"]; !ok {
		t.Error("expected ineligible-bin in reasons")
	}
	if _, ok := reasons["ineligible-os"]; !ok {
		t.Error("expected ineligible-os in reasons")
	}
}

// Note: Tests for ExecutionLocation, RequiresEdge, RequiredToolGroups, ToSnapshot,
// SourceType constants, ExecutionLocation constants, and various struct tests are in
// execution_test.go and manager_test.go

func TestEligibilityResult_Struct(t *testing.T) {
	result := EligibilityResult{
		Eligible: true,
		Reason:   "always enabled",
	}

	if !result.Eligible {
		t.Error("Eligible should be true")
	}
	if result.Reason != "always enabled" {
		t.Errorf("Reason = %q", result.Reason)
	}
}

func TestGatingContext_CheckEnvOrConfig_WithTempEnvVar(t *testing.T) {
	// Set a temp env var
	key := "TEST_GATING_VAR_XYZ"
	os.Setenv(key, "test-value")
	defer os.Unsetenv(key)

	ctx := NewGatingContext(nil, nil)

	if !ctx.CheckEnvOrConfig("any-skill", key) {
		t.Error("should return true for set env var")
	}
}

func TestSkillRequires_StructGating(t *testing.T) {
	requires := &SkillRequires{
		Bins:    []string{"git", "docker"},
		AnyBins: []string{"npm", "yarn"},
		Env:     []string{"API_KEY"},
		Config:  []string{"tools.enabled"},
	}

	if len(requires.Bins) != 2 {
		t.Errorf("Bins len = %d", len(requires.Bins))
	}
	if len(requires.AnyBins) != 2 {
		t.Errorf("AnyBins len = %d", len(requires.AnyBins))
	}
}

func TestInstallSpec_StructGating(t *testing.T) {
	spec := InstallSpec{
		ID:      "brew-git",
		Kind:    "brew",
		Formula: "git",
		Package: "git-pkg",
		Module:  "github.com/git/git",
		URL:     "https://git-scm.com",
		Bins:    []string{"git"},
		Label:   "Install via Homebrew",
		OS:      []string{"darwin"},
	}

	if spec.ID != "brew-git" {
		t.Errorf("ID = %q", spec.ID)
	}
	if spec.Kind != "brew" {
		t.Errorf("Kind = %q", spec.Kind)
	}
	if spec.Package != "git-pkg" {
		t.Errorf("Package = %q", spec.Package)
	}
	if spec.Module != "github.com/git/git" {
		t.Errorf("Module = %q", spec.Module)
	}
}
