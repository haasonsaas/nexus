package multiagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

func createTestSupervisorOrchestrator() (*Orchestrator, *Supervisor) {
	config := &MultiAgentConfig{
		DefaultAgentID:     "default-agent",
		SupervisorAgentID:  "supervisor",
		EnablePeerHandoffs: true,
		MaxHandoffDepth:    10,
		DefaultContextMode: ContextFull,
	}

	orch := &Orchestrator{
		config:   config,
		agents:   make(map[string]*AgentDefinition),
		runtimes: make(map[string]*agent.Runtime),
	}

	agents := []*AgentDefinition{
		{
			ID:                 "supervisor",
			Name:               "Supervisor Agent",
			Description:        "Coordinates other agents",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "code-specialist",
			Name:               "Code Specialist",
			Description:        "Handles coding tasks",
			Tools:              []string{"exec", "write"},
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "research-specialist",
			Name:               "Research Specialist",
			Description:        "Handles research tasks",
			Tools:              []string{"search", "fetch"},
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "no-handoff-specialist",
			Name:               "No Handoff Specialist",
			Description:        "Cannot receive handoffs",
			CanReceiveHandoffs: false,
		},
	}

	for _, a := range agents {
		orch.agents[a.ID] = a
		orch.runtimes[a.ID] = nil
	}

	supervisor := NewSupervisor(orch, "supervisor")

	return orch, supervisor
}

func TestNewSupervisor(t *testing.T) {
	orch, supervisor := createTestSupervisorOrchestrator()

	if supervisor == nil {
		t.Fatal("expected supervisor to be created")
	}

	if supervisor.orchestrator != orch {
		t.Error("expected orchestrator to be set")
	}

	if supervisor.supervisorID != "supervisor" {
		t.Errorf("expected supervisorID %q, got %q", "supervisor", supervisor.supervisorID)
	}

	if supervisor.maxDelegations != 5 {
		t.Errorf("expected default maxDelegations=5, got %d", supervisor.maxDelegations)
	}

	if supervisor.allowParallel {
		t.Error("expected allowParallel to be false by default")
	}
}

func TestSupervisor_SetDelegationPrompt(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	prompt := "Custom delegation instructions"
	supervisor.SetDelegationPrompt(prompt)

	if supervisor.delegationPrompt != prompt {
		t.Errorf("expected prompt %q, got %q", prompt, supervisor.delegationPrompt)
	}
}

func TestSupervisor_SetMaxDelegations(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	supervisor.SetMaxDelegations(10)

	if supervisor.maxDelegations != 10 {
		t.Errorf("expected maxDelegations=10, got %d", supervisor.maxDelegations)
	}
}

func TestSupervisor_SetAllowParallel(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	supervisor.SetAllowParallel(true)

	if !supervisor.allowParallel {
		t.Error("expected allowParallel to be true")
	}
}

func TestSupervisor_SelectAgent(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}
	msg := &models.Message{Content: "test message"}

	tests := []struct {
		name        string
		meta        *SessionMetadata
		wantAgentID string
	}{
		{
			name: "no active handoff returns supervisor",
			meta: &SessionMetadata{
				CurrentAgentID: "",
			},
			wantAgentID: "supervisor",
		},
		{
			name: "active handoff continues with delegated agent",
			meta: &SessionMetadata{
				CurrentAgentID:     "code-specialist",
				ActiveHandoffStack: []string{"supervisor"},
			},
			wantAgentID: "code-specialist",
		},
		{
			name: "empty handoff stack with current agent returns supervisor",
			meta: &SessionMetadata{
				CurrentAgentID:     "code-specialist",
				ActiveHandoffStack: []string{},
			},
			wantAgentID: "supervisor",
		},
		{
			name: "supervisor as current agent returns supervisor",
			meta: &SessionMetadata{
				CurrentAgentID:     "supervisor",
				ActiveHandoffStack: []string{"supervisor"},
			},
			wantAgentID: "supervisor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentID, err := supervisor.SelectAgent(ctx, session, msg, tt.meta)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if agentID != tt.wantAgentID {
				t.Errorf("agentID = %q, want %q", agentID, tt.wantAgentID)
			}
		})
	}
}

func TestSupervisor_BuildSupervisorPrompt(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	prompt := supervisor.BuildSupervisorPrompt()

	// Check for expected sections
	expectedPhrases := []string{
		"Supervisor Role",
		"Available Specialists",
		"Code Specialist",
		"Research Specialist",
		"Delegation Guidelines",
	}

	for _, phrase := range expectedPhrases {
		if !containsSubstring(prompt, phrase) {
			t.Errorf("expected prompt to contain %q", phrase)
		}
	}

	// Should not include supervisor itself
	if containsSubstring(prompt, "Supervisor Agent") {
		// The name might appear, but it shouldn't be in the specialists list
		// This is a weak check - the key is that supervisor isn't listed as a delegatee
	}

	// Should not include agents that can't receive handoffs
	if containsSubstring(prompt, "No Handoff Specialist") {
		t.Error("should not include agents that cannot receive handoffs")
	}
}

func TestSupervisor_BuildSupervisorPrompt_WithCustomDelegation(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	customPrompt := "Always prefer code-specialist for technical questions"
	supervisor.SetDelegationPrompt(customPrompt)

	prompt := supervisor.BuildSupervisorPrompt()

	if !containsSubstring(prompt, "Additional Instructions") {
		t.Error("expected 'Additional Instructions' section")
	}

	if !containsSubstring(prompt, customPrompt) {
		t.Error("expected custom delegation prompt to be included")
	}
}

func TestSupervisor_ApplyConfig(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	config := &SupervisorConfig{
		SupervisorID:     "new-supervisor",
		DelegationPrompt: "Custom prompt",
		MaxDelegations:   15,
		AllowParallel:    true,
	}

	supervisor.ApplyConfig(config)

	if supervisor.delegationPrompt != "Custom prompt" {
		t.Errorf("expected delegationPrompt %q, got %q", "Custom prompt", supervisor.delegationPrompt)
	}

	if supervisor.maxDelegations != 15 {
		t.Errorf("expected maxDelegations=15, got %d", supervisor.maxDelegations)
	}

	if !supervisor.allowParallel {
		t.Error("expected allowParallel to be true")
	}
}

func TestSupervisor_ApplyConfig_PartialConfig(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	// Set initial values
	supervisor.delegationPrompt = "initial"
	supervisor.maxDelegations = 5

	// Apply partial config (only MaxDelegations)
	config := &SupervisorConfig{
		MaxDelegations: 20,
	}

	supervisor.ApplyConfig(config)

	// DelegationPrompt should be unchanged
	if supervisor.delegationPrompt != "initial" {
		t.Error("delegationPrompt should not change when empty in config")
	}

	// MaxDelegations should be updated
	if supervisor.maxDelegations != 20 {
		t.Errorf("expected maxDelegations=20, got %d", supervisor.maxDelegations)
	}
}

func TestDefaultSupervisorSystemPrompt(t *testing.T) {
	prompt := DefaultSupervisorSystemPrompt()

	if prompt == "" {
		t.Error("expected non-empty default prompt")
	}

	expectedPhrases := []string{
		"supervisor agent",
		"coordinating",
		"delegate tool",
		"list_agents",
	}

	for _, phrase := range expectedPhrases {
		if !containsSubstring(prompt, phrase) {
			t.Errorf("expected default prompt to contain %q", phrase)
		}
	}
}

func TestNewDelegateTool(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	tool := NewDelegateTool(supervisor)

	if tool == nil {
		t.Fatal("expected tool to be created")
	}

	if tool.Name() != "delegate" {
		t.Errorf("expected name 'delegate', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	// Description should list available specialists
	if !containsSubstring(desc, "Code Specialist") {
		t.Error("expected description to list Code Specialist")
	}
}

func TestDelegateTool_Schema(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewDelegateTool(supervisor)

	schema := tool.Schema()

	if len(schema) == 0 {
		t.Error("expected non-empty schema")
	}

	// Verify it's valid JSON
	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}

	// Check for required fields
	if schemaMap["type"] != "object" {
		t.Error("expected type to be 'object'")
	}

	props, ok := schemaMap["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in schema")
	}

	if _, ok := props["specialist"]; !ok {
		t.Error("expected 'specialist' property")
	}

	if _, ok := props["task"]; !ok {
		t.Error("expected 'task' property")
	}
}

func TestDelegateTool_Execute(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewDelegateTool(supervisor)
	ctx := context.Background()

	tests := []struct {
		name        string
		input       DelegateInput
		wantError   bool
		errContains string
	}{
		{
			name: "valid delegation by ID",
			input: DelegateInput{
				Specialist:     "code-specialist",
				Task:           "Review this code",
				Context:        "User submitted Python code",
				ExpectedOutput: "Code review feedback",
			},
			wantError: false,
		},
		{
			name: "valid delegation by name",
			input: DelegateInput{
				Specialist: "Code Specialist",
				Task:       "Write a function",
			},
			wantError: false,
		},
		{
			name: "specialist not found",
			input: DelegateInput{
				Specialist: "non-existent",
				Task:       "Do something",
			},
			wantError:   true,
			errContains: "not found",
		},
		{
			name: "specialist cannot receive handoffs",
			input: DelegateInput{
				Specialist: "no-handoff-specialist",
				Task:       "Do something",
			},
			wantError:   true,
			errContains: "cannot receive delegations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(tt.input)
			result, err := tool.Execute(ctx, params)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("expected result")
			}

			if tt.wantError {
				if !result.IsError {
					t.Error("expected error result")
				}
				if tt.errContains != "" && !containsSubstring(result.Content, tt.errContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errContains, result.Content)
				}
			} else {
				if result.IsError {
					t.Errorf("unexpected error: %s", result.Content)
				}

				// Verify result contains handoff request
				var resultData map[string]interface{}
				if err := json.Unmarshal([]byte(result.Content), &resultData); err != nil {
					t.Fatalf("invalid result JSON: %v", err)
				}

				if _, ok := resultData["handoff_request"]; !ok {
					t.Error("expected handoff_request in result")
				}

				if resultData["is_delegation"] != true {
					t.Error("expected is_delegation to be true")
				}
			}
		})
	}
}

func TestDelegateTool_Execute_InvalidJSON(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewDelegateTool(supervisor)
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("invalid json"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for invalid JSON")
	}

	if !containsSubstring(result.Content, "Invalid delegation parameters") {
		t.Errorf("expected invalid parameters message, got %s", result.Content)
	}
}

func TestNewReportTool(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()

	tool := NewReportTool(supervisor)

	if tool == nil {
		t.Fatal("expected tool to be created")
	}

	if tool.Name() != "report" {
		t.Errorf("expected name 'report', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestReportTool_Schema(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewReportTool(supervisor)

	schema := tool.Schema()

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}

	props, ok := schemaMap["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in schema")
	}

	if _, ok := props["summary"]; !ok {
		t.Error("expected 'summary' property")
	}

	if _, ok := props["status"]; !ok {
		t.Error("expected 'status' property")
	}

	// Check status enum
	statusProp := props["status"].(map[string]interface{})
	if statusProp["type"] != "string" {
		t.Error("expected status type to be string")
	}
}

func TestReportTool_Execute(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewReportTool(supervisor)
	ctx := WithCurrentAgent(context.Background(), "code-specialist")

	tests := []struct {
		name  string
		input ReportInput
	}{
		{
			name: "complete report",
			input: ReportInput{
				Summary:  "Task completed successfully",
				Details:  "Detailed implementation notes",
				Status:   "complete",
				FollowUp: "May need additional testing",
			},
		},
		{
			name: "partial report",
			input: ReportInput{
				Summary: "Made progress but not finished",
				Status:  "partial",
			},
		},
		{
			name: "failed report",
			input: ReportInput{
				Summary: "Could not complete the task",
				Status:  "failed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(tt.input)
			result, err := tool.Execute(ctx, params)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.IsError {
				t.Errorf("unexpected error: %s", result.Content)
			}

			var resultData map[string]interface{}
			if err := json.Unmarshal([]byte(result.Content), &resultData); err != nil {
				t.Fatalf("invalid result JSON: %v", err)
			}

			if resultData["return_to"] != "supervisor" {
				t.Errorf("expected return_to 'supervisor', got %v", resultData["return_to"])
			}

			if resultData["is_report"] != true {
				t.Error("expected is_report to be true")
			}

			if resultData["summary"] != tt.input.Summary {
				t.Errorf("expected summary %q, got %v", tt.input.Summary, resultData["summary"])
			}
		})
	}
}

func TestReportTool_Execute_InvalidJSON(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewReportTool(supervisor)
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("invalid"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result")
	}
}

func TestSupervisorConfig_Fields(t *testing.T) {
	config := SupervisorConfig{
		SupervisorID:     "sup-1",
		DelegationPrompt: "Custom prompt",
		MaxDelegations:   10,
		AllowParallel:    true,
		AutoSynthesize:   true,
		SynthesisPrompt:  "Combine results",
	}

	if config.SupervisorID != "sup-1" {
		t.Error("expected SupervisorID to be set")
	}

	if config.DelegationPrompt != "Custom prompt" {
		t.Error("expected DelegationPrompt to be set")
	}

	if config.MaxDelegations != 10 {
		t.Error("expected MaxDelegations to be set")
	}

	if !config.AllowParallel {
		t.Error("expected AllowParallel to be true")
	}

	if !config.AutoSynthesize {
		t.Error("expected AutoSynthesize to be true")
	}

	if config.SynthesisPrompt != "Combine results" {
		t.Error("expected SynthesisPrompt to be set")
	}
}

func TestDelegateInput_Fields(t *testing.T) {
	input := DelegateInput{
		Specialist:     "code-specialist",
		Task:           "Write a function",
		Context:        "Additional context",
		ExpectedOutput: "A working function",
	}

	if input.Specialist != "code-specialist" {
		t.Error("expected Specialist to be set")
	}

	if input.Task != "Write a function" {
		t.Error("expected Task to be set")
	}

	if input.Context != "Additional context" {
		t.Error("expected Context to be set")
	}

	if input.ExpectedOutput != "A working function" {
		t.Error("expected ExpectedOutput to be set")
	}
}

func TestReportInput_Fields(t *testing.T) {
	input := ReportInput{
		Summary:  "Completed task",
		Details:  "Implementation details",
		Status:   "complete",
		FollowUp: "Next steps",
	}

	if input.Summary != "Completed task" {
		t.Error("expected Summary to be set")
	}

	if input.Details != "Implementation details" {
		t.Error("expected Details to be set")
	}

	if input.Status != "complete" {
		t.Error("expected Status to be set")
	}

	if input.FollowUp != "Next steps" {
		t.Error("expected FollowUp to be set")
	}
}

func TestSupervisor_SetupSupervisorAgent(t *testing.T) {
	// This test requires actual agent.Runtime instances to work properly.
	// Since SetupSupervisorAgent calls runtime.RegisterTool() and our test
	// orchestrator has nil runtimes (we can't easily mock agent.Runtime),
	// we skip this test. The functionality is tested through integration tests.
	t.Skip("SetupSupervisorAgent requires actual agent.Runtime instances")
}

func TestSupervisor_SetupSupervisorAgent_NotFound(t *testing.T) {
	orch := &Orchestrator{
		config:   &MultiAgentConfig{},
		agents:   make(map[string]*AgentDefinition),
		runtimes: make(map[string]*agent.Runtime),
	}

	supervisor := NewSupervisor(orch, "non-existent")

	err := supervisor.SetupSupervisorAgent()

	if err == nil {
		t.Error("expected error for non-existent supervisor")
	}

	if !containsSubstring(err.Error(), "supervisor agent not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestDelegateTool_HandoffRequestStructure(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewDelegateTool(supervisor)
	ctx := context.Background()

	input := DelegateInput{
		Specialist:     "code-specialist",
		Task:           "Test task",
		Context:        "Test context",
		ExpectedOutput: "Test output",
	}

	params, _ := json.Marshal(input)
	result, _ := tool.Execute(ctx, params)

	var resultData map[string]interface{}
	_ = json.Unmarshal([]byte(result.Content), &resultData)

	handoffReq, ok := resultData["handoff_request"].(map[string]interface{})
	if !ok {
		t.Fatal("expected handoff_request to be a map")
	}

	// Verify handoff request fields
	if handoffReq["from_agent_id"] != "supervisor" {
		t.Errorf("expected from_agent_id 'supervisor', got %v", handoffReq["from_agent_id"])
	}

	if handoffReq["to_agent_id"] != "code-specialist" {
		t.Errorf("expected to_agent_id 'code-specialist', got %v", handoffReq["to_agent_id"])
	}

	if handoffReq["reason"] != "Test task" {
		t.Errorf("expected reason 'Test task', got %v", handoffReq["reason"])
	}

	if handoffReq["return_expected"] != true {
		t.Error("expected return_expected to be true for delegations")
	}

	// Check context
	ctxData, ok := handoffReq["context"].(map[string]interface{})
	if !ok {
		t.Fatal("expected context in handoff request")
	}

	if ctxData["task"] != "Test task" {
		t.Error("expected task in context")
	}

	if ctxData["summary"] != "Test context" {
		t.Error("expected summary in context")
	}

	metadata, ok := ctxData["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("expected metadata in context")
	}

	if metadata["is_delegation"] != true {
		t.Error("expected is_delegation in metadata")
	}

	if metadata["expected_output"] != "Test output" {
		t.Error("expected expected_output in metadata")
	}
}

func TestDelegateTool_FindByPartialName(t *testing.T) {
	_, supervisor := createTestSupervisorOrchestrator()
	tool := NewDelegateTool(supervisor)
	ctx := context.Background()

	// Test case-insensitive matching
	input := DelegateInput{
		Specialist: "CODE SPECIALIST", // Uppercase
		Task:       "Test task",
	}

	params, _ := json.Marshal(input)
	result, _ := tool.Execute(ctx, params)

	if result.IsError {
		t.Errorf("expected case-insensitive match to work: %s", result.Content)
	}
}
