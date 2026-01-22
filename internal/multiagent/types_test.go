package multiagent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgentDefinition_ToJSON(t *testing.T) {
	agent := &AgentDefinition{
		ID:                 "test-agent",
		Name:               "Test Agent",
		Description:        "A test agent",
		SystemPrompt:       "You are a test agent",
		Model:              "claude-3-opus",
		Provider:           "anthropic",
		Tools:              []string{"exec", "read"},
		CanReceiveHandoffs: true,
		MaxIterations:      10,
		Metadata: map[string]any{
			"key": "value",
		},
	}

	data, err := agent.ToJSON()
	if err != nil {
		t.Fatalf("failed to convert to JSON: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["id"] != "test-agent" {
		t.Error("expected id to be set")
	}

	if parsed["name"] != "Test Agent" {
		t.Error("expected name to be set")
	}
}

func TestAgentDefinition_Clone(t *testing.T) {
	original := &AgentDefinition{
		ID:          "original",
		Name:        "Original Agent",
		Description: "Original description",
		Tools:       []string{"tool1", "tool2"},
		HandoffRules: []HandoffRule{
			{TargetAgentID: "target"},
		},
		Metadata: map[string]any{
			"key": "value",
		},
	}

	clone := original.Clone()

	// Verify basic fields
	if clone.ID != original.ID {
		t.Error("expected ID to be cloned")
	}

	if clone.Name != original.Name {
		t.Error("expected Name to be cloned")
	}

	// Verify slice independence
	clone.Tools[0] = "modified"
	if original.Tools[0] == "modified" {
		t.Error("modifying clone should not affect original tools")
	}

	// Verify handoff rules independence
	clone.HandoffRules[0].TargetAgentID = "modified"
	if original.HandoffRules[0].TargetAgentID == "modified" {
		t.Error("modifying clone should not affect original handoff rules")
	}

	// Verify metadata independence
	clone.Metadata["key"] = "modified"
	if original.Metadata["key"] == "modified" {
		t.Error("modifying clone should not affect original metadata")
	}
}

func TestAgentDefinition_Clone_Nil(t *testing.T) {
	var agent *AgentDefinition = nil
	clone := agent.Clone()

	if clone != nil {
		t.Error("expected nil clone from nil agent")
	}
}

func TestAgentDefinition_Clone_EmptyFields(t *testing.T) {
	original := &AgentDefinition{
		ID:   "simple",
		Name: "Simple Agent",
		// All slices and maps are nil
	}

	clone := original.Clone()

	if clone.Tools != nil {
		t.Error("expected nil Tools to remain nil")
	}

	if clone.HandoffRules != nil {
		t.Error("expected nil HandoffRules to remain nil")
	}

	if clone.Metadata != nil {
		t.Error("expected nil Metadata to remain nil")
	}
}

func TestAgentDefinition_HasTool(t *testing.T) {
	agent := &AgentDefinition{
		ID:    "test",
		Tools: []string{"exec", "read", "write"},
	}

	tests := []struct {
		toolName string
		want     bool
	}{
		{"exec", true},
		{"read", true},
		{"write", true},
		{"search", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := agent.HasTool(tt.toolName)
			if got != tt.want {
				t.Errorf("HasTool(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestAgentDefinition_HasTool_EmptyTools(t *testing.T) {
	agent := &AgentDefinition{
		ID:    "test",
		Tools: nil,
	}

	if agent.HasTool("any") {
		t.Error("expected HasTool to return false for nil Tools")
	}
}

func TestAgentDefinition_GetHandoffTarget(t *testing.T) {
	agent := &AgentDefinition{
		ID: "test",
		HandoffRules: []HandoffRule{
			{
				TargetAgentID: "keyword-agent",
				Triggers: []RoutingTrigger{
					{Type: TriggerKeyword, Value: "help"},
				},
			},
			{
				TargetAgentID: "pattern-agent",
				Triggers: []RoutingTrigger{
					{Type: TriggerPattern, Value: "error"},
				},
			},
			{
				TargetAgentID: "multi-agent",
				Triggers: []RoutingTrigger{
					{Type: TriggerKeyword, Values: []string{"code", "debug"}},
				},
			},
		},
	}

	tests := []struct {
		name      string
		trigger   TriggerType
		value     string
		wantID    string
		wantFound bool
	}{
		{
			name:      "find by value",
			trigger:   TriggerKeyword,
			value:     "help",
			wantID:    "keyword-agent",
			wantFound: true,
		},
		{
			name:      "find by values list",
			trigger:   TriggerKeyword,
			value:     "code",
			wantID:    "multi-agent",
			wantFound: true,
		},
		{
			name:      "trigger type with matching value",
			trigger:   TriggerPattern,
			value:     "error",
			wantID:    "pattern-agent",
			wantFound: true,
		},
		{
			name:      "no matching trigger type",
			trigger:   TriggerIntent,
			value:     "test",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := agent.GetHandoffTarget(tt.trigger, tt.value)

			if tt.wantFound {
				if rule == nil {
					t.Error("expected rule to be found")
					return
				}
				if rule.TargetAgentID != tt.wantID {
					t.Errorf("expected target %s, got %s", tt.wantID, rule.TargetAgentID)
				}
			} else {
				if rule != nil {
					t.Errorf("expected nil rule, got %+v", rule)
				}
			}
		})
	}
}

func TestContainsValue(t *testing.T) {
	tests := []struct {
		slice []string
		value string
		want  bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
		{nil, "a", false},
		{[]string{"test"}, "test", true},
	}

	for _, tt := range tests {
		got := containsValue(tt.slice, tt.value)
		if got != tt.want {
			t.Errorf("containsValue(%v, %q) = %v, want %v", tt.slice, tt.value, got, tt.want)
		}
	}
}

func TestHandoffRule_Fields(t *testing.T) {
	rule := HandoffRule{
		TargetAgentID: "target",
		Triggers: []RoutingTrigger{
			{Type: TriggerKeyword, Value: "help"},
		},
		Priority:       10,
		ContextMode:    ContextSummary,
		SummaryPrompt:  "Summarize the conversation",
		ReturnToSender: true,
		Message:        "Please handle this",
	}

	if rule.TargetAgentID != "target" {
		t.Error("expected TargetAgentID to be set")
	}
	if len(rule.Triggers) != 1 {
		t.Error("expected Triggers to be set")
	}
	if rule.Priority != 10 {
		t.Error("expected Priority to be set")
	}
	if rule.ContextMode != ContextSummary {
		t.Error("expected ContextMode to be set")
	}
	if rule.SummaryPrompt != "Summarize the conversation" {
		t.Error("expected SummaryPrompt to be set")
	}
	if !rule.ReturnToSender {
		t.Error("expected ReturnToSender to be true")
	}
	if rule.Message != "Please handle this" {
		t.Error("expected Message to be set")
	}
}

func TestRoutingTrigger_Fields(t *testing.T) {
	trigger := RoutingTrigger{
		Type:      TriggerKeyword,
		Value:     "help",
		Values:    []string{"assist", "support"},
		Threshold: 0.8,
		Metadata: map[string]any{
			"key": "value",
		},
	}

	if trigger.Type != TriggerKeyword {
		t.Error("expected Type to be set")
	}
	if trigger.Value != "help" {
		t.Error("expected Value to be set")
	}
	if len(trigger.Values) != 2 {
		t.Error("expected Values to be set")
	}
	if trigger.Threshold != 0.8 {
		t.Error("expected Threshold to be set")
	}
	if trigger.Metadata["key"] != "value" {
		t.Error("expected Metadata to be set")
	}
}

func TestHandoffRequest_Fields(t *testing.T) {
	now := time.Now()
	request := HandoffRequest{
		FromAgentID:    "agent-1",
		ToAgentID:      "agent-2",
		Reason:         "Need specialist",
		Context:        &SharedContext{Task: "Review code"},
		ReturnExpected: true,
		Priority:       5,
		Timestamp:      now,
	}

	if request.FromAgentID != "agent-1" {
		t.Error("expected FromAgentID to be set")
	}
	if request.ToAgentID != "agent-2" {
		t.Error("expected ToAgentID to be set")
	}
	if request.Reason != "Need specialist" {
		t.Error("expected Reason to be set")
	}
	if request.Context == nil {
		t.Error("expected Context to be set")
	}
	if !request.ReturnExpected {
		t.Error("expected ReturnExpected to be true")
	}
	if request.Priority != 5 {
		t.Error("expected Priority to be set")
	}
	if request.Timestamp != now {
		t.Error("expected Timestamp to be set")
	}
}

func TestHandoffResult_Fields(t *testing.T) {
	result := HandoffResult{
		Success:      true,
		FromAgentID:  "agent-1",
		ToAgentID:    "agent-2",
		Response:     "Task completed",
		ShouldReturn: true,
		Error:        "",
		Duration:     5 * time.Second,
	}

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.FromAgentID != "agent-1" {
		t.Error("expected FromAgentID to be set")
	}
	if result.ToAgentID != "agent-2" {
		t.Error("expected ToAgentID to be set")
	}
	if result.Response != "Task completed" {
		t.Error("expected Response to be set")
	}
	if !result.ShouldReturn {
		t.Error("expected ShouldReturn to be true")
	}
	if result.Duration != 5*time.Second {
		t.Error("expected Duration to be set")
	}
}

func TestAgentState_Fields(t *testing.T) {
	now := time.Now()
	state := AgentState{
		AgentID:       "agent-1",
		Status:        StatusActive,
		Iteration:     3,
		StartedAt:     now,
		HandoffStack:  []string{"agent-0"},
		SharedContext: &SharedContext{Task: "Task"},
		Metadata: map[string]any{
			"key": "value",
		},
	}

	if state.AgentID != "agent-1" {
		t.Error("expected AgentID to be set")
	}
	if state.Status != StatusActive {
		t.Error("expected Status to be set")
	}
	if state.Iteration != 3 {
		t.Error("expected Iteration to be set")
	}
	if state.StartedAt != now {
		t.Error("expected StartedAt to be set")
	}
	if len(state.HandoffStack) != 1 {
		t.Error("expected HandoffStack to be set")
	}
	if state.SharedContext == nil {
		t.Error("expected SharedContext to be set")
	}
}

func TestAgentStatus_Values(t *testing.T) {
	statuses := []struct {
		status   AgentStatus
		expected string
	}{
		{StatusActive, "active"},
		{StatusWaiting, "waiting"},
		{StatusHandedOff, "handed_off"},
		{StatusComplete, "complete"},
		{StatusError, "error"},
	}

	for _, s := range statuses {
		if string(s.status) != s.expected {
			t.Errorf("status %s != expected %s", s.status, s.expected)
		}
	}
}

func TestMultiAgentConfig_Fields(t *testing.T) {
	config := MultiAgentConfig{
		DefaultAgentID:     "default",
		SupervisorAgentID:  "supervisor",
		Agents:             []AgentDefinition{{ID: "agent-1"}},
		GlobalHandoffRules: []HandoffRule{{TargetAgentID: "fallback"}},
		DefaultContextMode: ContextFull,
		MaxHandoffDepth:    10,
		HandoffTimeout:     5 * time.Minute,
		EnablePeerHandoffs: true,
		Metadata: map[string]any{
			"key": "value",
		},
	}

	if config.DefaultAgentID != "default" {
		t.Error("expected DefaultAgentID to be set")
	}
	if config.SupervisorAgentID != "supervisor" {
		t.Error("expected SupervisorAgentID to be set")
	}
	if len(config.Agents) != 1 {
		t.Error("expected Agents to be set")
	}
	if len(config.GlobalHandoffRules) != 1 {
		t.Error("expected GlobalHandoffRules to be set")
	}
	if config.DefaultContextMode != ContextFull {
		t.Error("expected DefaultContextMode to be set")
	}
	if config.MaxHandoffDepth != 10 {
		t.Error("expected MaxHandoffDepth to be set")
	}
	if config.HandoffTimeout != 5*time.Minute {
		t.Error("expected HandoffTimeout to be set")
	}
	if !config.EnablePeerHandoffs {
		t.Error("expected EnablePeerHandoffs to be true")
	}
}

func TestSessionMetadata_Fields(t *testing.T) {
	now := time.Now()
	meta := SessionMetadata{
		CurrentAgentID: "agent-1",
		AgentHistory: []AgentHistoryEntry{
			{AgentID: "agent-0"},
		},
		HandoffCount:       3,
		LastHandoffAt:      &now,
		ActiveHandoffStack: []string{"agent-0"},
	}

	if meta.CurrentAgentID != "agent-1" {
		t.Error("expected CurrentAgentID to be set")
	}
	if len(meta.AgentHistory) != 1 {
		t.Error("expected AgentHistory to be set")
	}
	if meta.HandoffCount != 3 {
		t.Error("expected HandoffCount to be set")
	}
	if meta.LastHandoffAt == nil {
		t.Error("expected LastHandoffAt to be set")
	}
	if len(meta.ActiveHandoffStack) != 1 {
		t.Error("expected ActiveHandoffStack to be set")
	}
}

func TestAgentHistoryEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := AgentHistoryEntry{
		AgentID:       "agent-1",
		StartedAt:     now,
		EndedAt:       &now,
		HandoffTo:     "agent-2",
		HandoffReason: "Specialist needed",
	}

	if entry.AgentID != "agent-1" {
		t.Error("expected AgentID to be set")
	}
	if entry.StartedAt != now {
		t.Error("expected StartedAt to be set")
	}
	if entry.EndedAt == nil {
		t.Error("expected EndedAt to be set")
	}
	if entry.HandoffTo != "agent-2" {
		t.Error("expected HandoffTo to be set")
	}
	if entry.HandoffReason != "Specialist needed" {
		t.Error("expected HandoffReason to be set")
	}
}

func TestHandoffToolInput_AllFields(t *testing.T) {
	input := HandoffToolInput{
		TargetAgent:    "target",
		Reason:         "Need help",
		Context:        "Additional context",
		ReturnExpected: true,
	}

	if input.TargetAgent != "target" {
		t.Error("expected TargetAgent to be set")
	}
	if input.Reason != "Need help" {
		t.Error("expected Reason to be set")
	}
	if input.Context != "Additional context" {
		t.Error("expected Context to be set")
	}
	if !input.ReturnExpected {
		t.Error("expected ReturnExpected to be true")
	}
}

func TestAgentManifest_Fields(t *testing.T) {
	manifest := AgentManifest{
		Agents: []AgentDefinition{
			{ID: "agent-1"},
		},
		GlobalConfig: &MultiAgentConfig{
			DefaultAgentID: "agent-1",
		},
		Source: "AGENTS.md",
	}

	if len(manifest.Agents) != 1 {
		t.Error("expected Agents to be set")
	}
	if manifest.GlobalConfig == nil {
		t.Error("expected GlobalConfig to be set")
	}
	if manifest.Source != "AGENTS.md" {
		t.Error("expected Source to be set")
	}
}

func TestTriggerType_AllTypes(t *testing.T) {
	types := []TriggerType{
		TriggerKeyword,
		TriggerPattern,
		TriggerIntent,
		TriggerToolUse,
		TriggerExplicit,
		TriggerFallback,
		TriggerAlways,
		TriggerTaskComplete,
		TriggerError,
	}

	// Verify all types are unique
	seen := make(map[TriggerType]bool)
	for _, typ := range types {
		if seen[typ] {
			t.Errorf("duplicate trigger type: %s", typ)
		}
		seen[typ] = true
	}

	// Verify expected count
	if len(types) != 9 {
		t.Errorf("expected 9 trigger types, got %d", len(types))
	}
}

func TestContextSharingMode_AllModes(t *testing.T) {
	modes := []ContextSharingMode{
		ContextFull,
		ContextSummary,
		ContextFiltered,
		ContextNone,
		ContextLastN,
	}

	// Verify all modes are unique
	seen := make(map[ContextSharingMode]bool)
	for _, mode := range modes {
		if seen[mode] {
			t.Errorf("duplicate context mode: %s", mode)
		}
		seen[mode] = true
	}

	// Verify expected count
	if len(modes) != 5 {
		t.Errorf("expected 5 context modes, got %d", len(modes))
	}
}

func TestAgentDefinition_JSON_Roundtrip(t *testing.T) {
	original := &AgentDefinition{
		ID:                 "test-agent",
		Name:               "Test Agent",
		Description:        "A test agent",
		SystemPrompt:       "You are helpful",
		Model:              "claude-3",
		Provider:           "anthropic",
		Tools:              []string{"exec", "read"},
		CanReceiveHandoffs: true,
		MaxIterations:      10,
	}

	// Serialize to JSON
	data, err := original.ToJSON()
	if err != nil {
		t.Fatalf("failed to serialize: %v", err)
	}

	// Deserialize back
	var restored AgentDefinition
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to deserialize: %v", err)
	}

	// Verify fields
	if restored.ID != original.ID {
		t.Error("ID mismatch")
	}
	if restored.Name != original.Name {
		t.Error("Name mismatch")
	}
	if restored.Model != original.Model {
		t.Error("Model mismatch")
	}
	if len(restored.Tools) != len(original.Tools) {
		t.Error("Tools length mismatch")
	}
	if restored.CanReceiveHandoffs != original.CanReceiveHandoffs {
		t.Error("CanReceiveHandoffs mismatch")
	}
}
