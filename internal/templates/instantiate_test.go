package templates

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/multiagent"
	"github.com/haasonsaas/nexus/internal/tools/policy"
)

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected float64
		wantErr  bool
	}{
		{"int", int(42), 42.0, false},
		{"int8", int8(8), 8.0, false},
		{"int16", int16(16), 16.0, false},
		{"int32", int32(32), 32.0, false},
		{"int64", int64(64), 64.0, false},
		{"uint", uint(10), 10.0, false},
		{"uint8", uint8(8), 8.0, false},
		{"uint16", uint16(16), 16.0, false},
		{"uint32", uint32(32), 32.0, false},
		{"uint64", uint64(64), 64.0, false},
		{"float32", float32(3.14), 3.140000104904175, false}, // float32 precision
		{"float64", float64(3.14159), 3.14159, false},
		{"string", "not a number", 0, true},
		{"bool", true, 0, true},
		{"nil", nil, 0, true},
		{"slice", []int{1, 2}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := toFloat64(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %v (%T)", tt.input, tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("toFloat64(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestApplyOverrides(t *testing.T) {
	t.Run("applies model override", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{Model: "old-model"}
		overrides := &AgentTemplateSpec{Model: "new-model"}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if agent.Model != "new-model" {
			t.Errorf("Model = %q, want %q", agent.Model, "new-model")
		}
	})

	t.Run("applies provider override", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{Provider: "old-provider"}
		overrides := &AgentTemplateSpec{Provider: "new-provider"}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if agent.Provider != "new-provider" {
			t.Errorf("Provider = %q, want %q", agent.Provider, "new-provider")
		}
	})

	t.Run("applies tools override", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{Tools: []string{"tool1"}}
		overrides := &AgentTemplateSpec{Tools: []string{"tool2", "tool3"}}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if len(agent.Tools) != 2 {
			t.Errorf("Tools length = %d, want 2", len(agent.Tools))
		}
		if agent.Tools[0] != "tool2" {
			t.Errorf("Tools[0] = %q, want %q", agent.Tools[0], "tool2")
		}
	})

	t.Run("applies tool policy override", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{}
		newPolicy := &policy.Policy{}
		overrides := &AgentTemplateSpec{ToolPolicy: newPolicy}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if agent.ToolPolicy == nil {
			t.Fatal("ToolPolicy should not be nil")
		}
	})

	t.Run("applies handoff rules override", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{}
		overrides := &AgentTemplateSpec{
			HandoffRules: []multiagent.HandoffRule{
				{TargetAgentID: "target-1"},
			},
		}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if len(agent.HandoffRules) != 1 {
			t.Fatalf("HandoffRules length = %d, want 1", len(agent.HandoffRules))
		}
		if agent.HandoffRules[0].TargetAgentID != "target-1" {
			t.Errorf("HandoffRules[0].TargetAgentID = %q, want %q", agent.HandoffRules[0].TargetAgentID, "target-1")
		}
	})

	t.Run("applies max iterations override", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{MaxIterations: 5}
		overrides := &AgentTemplateSpec{MaxIterations: 10}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if agent.MaxIterations != 10 {
			t.Errorf("MaxIterations = %d, want 10", agent.MaxIterations)
		}
	})

	t.Run("applies metadata override", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{Metadata: map[string]any{"key1": "value1"}}
		overrides := &AgentTemplateSpec{Metadata: map[string]any{"key2": "value2"}}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if agent.Metadata["key2"] != "value2" {
			t.Errorf("Metadata[key2] = %v, want value2", agent.Metadata["key2"])
		}
	})

	t.Run("creates metadata if nil", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{Metadata: nil}
		overrides := &AgentTemplateSpec{Metadata: map[string]any{"key": "value"}}
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if agent.Metadata == nil {
			t.Fatal("Metadata should not be nil")
		}
		if agent.Metadata["key"] != "value" {
			t.Errorf("Metadata[key] = %v, want value", agent.Metadata["key"])
		}
	})

	t.Run("does not override with empty values", func(t *testing.T) {
		agent := &multiagent.AgentDefinition{
			Model:         "original-model",
			Provider:      "original-provider",
			Tools:         []string{"original-tool"},
			MaxIterations: 5,
		}
		overrides := &AgentTemplateSpec{} // All empty values
		var warnings []string

		applyOverrides(agent, overrides, &warnings)

		if agent.Model != "original-model" {
			t.Errorf("Model should not change, got %q", agent.Model)
		}
		if agent.Provider != "original-provider" {
			t.Errorf("Provider should not change, got %q", agent.Provider)
		}
		if len(agent.Tools) != 1 || agent.Tools[0] != "original-tool" {
			t.Errorf("Tools should not change, got %v", agent.Tools)
		}
		if agent.MaxIterations != 5 {
			t.Errorf("MaxIterations should not change, got %d", agent.MaxIterations)
		}
	})
}

func TestNewInstantiator(t *testing.T) {
	r := &Registry{
		templates: make(map[string]*AgentTemplate),
	}

	inst := NewInstantiator(r)
	if inst == nil {
		t.Fatal("expected non-nil instantiator")
	}
	if inst.registry != r {
		t.Error("registry should be set")
	}
	if inst.varsEngine == nil {
		t.Error("varsEngine should be initialized")
	}
}

func TestInstantiator_InstantiateTemplateNotFound(t *testing.T) {
	r := &Registry{
		templates: make(map[string]*AgentTemplate),
	}
	inst := NewInstantiator(r)

	_, err := inst.Instantiate(&InstantiationRequest{
		TemplateName: "nonexistent",
		AgentID:      "agent-1",
	})

	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}
