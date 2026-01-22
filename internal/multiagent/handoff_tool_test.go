package multiagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

func createHandoffToolTestOrchestrator() *Orchestrator {
	config := &MultiAgentConfig{
		DefaultAgentID:     "default-agent",
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
			ID:                 "default-agent",
			Name:               "Default Agent",
			Description:        "Default handler",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "code-agent",
			Name:               "Code Agent",
			Description:        "Handles coding tasks",
			Tools:              []string{"exec", "write"},
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "research-agent",
			Name:               "Research Agent",
			Description:        "Handles research",
			Tools:              []string{"search", "fetch"},
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "no-handoff-agent",
			Name:               "No Handoff Agent",
			Description:        "Cannot receive handoffs",
			CanReceiveHandoffs: false,
		},
	}

	for _, a := range agents {
		orch.agents[a.ID] = a
	}

	return orch
}

func TestNewHandoffTool(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)

	if tool == nil {
		t.Fatal("expected tool to be created")
	}

	if tool.orchestrator != orch {
		t.Error("expected orchestrator to be set")
	}
}

func TestHandoffTool_Name(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)

	if tool.Name() != "handoff" {
		t.Errorf("expected name 'handoff', got %q", tool.Name())
	}
}

func TestHandoffTool_Description(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)

	desc := tool.Description()

	if desc == "" {
		t.Error("expected non-empty description")
	}

	// Should list agents that can receive handoffs
	expectedAgents := []string{"Code Agent", "Research Agent", "Default Agent"}
	for _, agent := range expectedAgents {
		if !containsSubstring(desc, agent) {
			t.Errorf("expected description to contain %q", agent)
		}
	}

	// Should not list agents that cannot receive handoffs
	if containsSubstring(desc, "No Handoff Agent") {
		t.Error("should not include agents that cannot receive handoffs")
	}
}

func TestHandoffTool_Schema(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)

	schema := tool.Schema()

	if len(schema) == 0 {
		t.Error("expected non-empty schema")
	}

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}

	if schemaMap["type"] != "object" {
		t.Error("expected type to be 'object'")
	}

	props, ok := schemaMap["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in schema")
	}

	// Check required properties
	requiredProps := []string{"target_agent", "reason"}
	for _, prop := range requiredProps {
		if _, ok := props[prop]; !ok {
			t.Errorf("expected '%s' property in schema", prop)
		}
	}

	// Check optional properties
	optionalProps := []string{"context", "return_expected"}
	for _, prop := range optionalProps {
		if _, ok := props[prop]; !ok {
			t.Errorf("expected '%s' property in schema", prop)
		}
	}

	// Check target_agent has enum
	targetProp, ok := props["target_agent"].(map[string]interface{})
	if !ok {
		t.Fatal("expected target_agent property")
	}

	if _, ok := targetProp["enum"]; !ok {
		t.Error("expected enum in target_agent property")
	}
}

func TestHandoffTool_Execute(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)
	ctx := WithCurrentAgent(context.Background(), "default-agent")

	tests := []struct {
		name        string
		input       HandoffToolInput
		wantError   bool
		errContains string
	}{
		{
			name: "valid handoff by ID",
			input: HandoffToolInput{
				TargetAgent:    "code-agent",
				Reason:         "Need code review",
				Context:        "User submitted Python code",
				ReturnExpected: true,
			},
			wantError: false,
		},
		{
			name: "valid handoff by name",
			input: HandoffToolInput{
				TargetAgent: "Code Agent",
				Reason:      "Need coding help",
			},
			wantError: false,
		},
		{
			name: "target not found",
			input: HandoffToolInput{
				TargetAgent: "non-existent",
				Reason:      "Test",
			},
			wantError:   true,
			errContains: "Target agent not found",
		},
		{
			name: "target cannot receive handoffs",
			input: HandoffToolInput{
				TargetAgent: "no-handoff-agent",
				Reason:      "Test",
			},
			wantError:   true,
			errContains: "cannot receive handoffs",
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

				// Verify result structure
				var resultData map[string]interface{}
				if err := json.Unmarshal([]byte(result.Content), &resultData); err != nil {
					t.Fatalf("invalid result JSON: %v", err)
				}

				if _, ok := resultData["handoff_request"]; !ok {
					t.Error("expected handoff_request in result")
				}

				if resultData["status"] != "initiated" {
					t.Errorf("expected status 'initiated', got %v", resultData["status"])
				}
			}
		})
	}
}

func TestHandoffTool_Execute_SelfHandoff(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)
	ctx := WithCurrentAgent(context.Background(), "code-agent")

	input := HandoffToolInput{
		TargetAgent: "code-agent",
		Reason:      "Self handoff",
	}

	params, _ := json.Marshal(input)
	result, _ := tool.Execute(ctx, params)

	if !result.IsError {
		t.Error("expected error for self-handoff")
	}

	if !containsSubstring(result.Content, "Cannot hand off to yourself") {
		t.Errorf("expected self-handoff error message, got %s", result.Content)
	}
}

func TestHandoffTool_Execute_InvalidJSON(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("invalid json"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for invalid JSON")
	}

	if !containsSubstring(result.Content, "Invalid handoff parameters") {
		t.Errorf("expected invalid parameters message, got %s", result.Content)
	}
}

func TestHandoffTool_Execute_NoCurrentAgent(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)
	ctx := context.Background() // No current agent

	input := HandoffToolInput{
		TargetAgent: "code-agent",
		Reason:      "Test handoff",
	}

	params, _ := json.Marshal(input)
	result, _ := tool.Execute(ctx, params)

	if result.IsError {
		t.Errorf("expected success even without current agent: %s", result.Content)
	}

	// FromAgentID should be "unknown"
	var resultData map[string]interface{}
	_ = json.Unmarshal([]byte(result.Content), &resultData)

	handoffReq := resultData["handoff_request"].(map[string]interface{})
	if handoffReq["from_agent_id"] != "unknown" {
		t.Errorf("expected from_agent_id 'unknown', got %v", handoffReq["from_agent_id"])
	}
}

func TestHandoffTool_FindTargetAgent(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)

	tests := []struct {
		name       string
		identifier string
		wantID     string
		wantFound  bool
	}{
		{
			name:       "exact ID match",
			identifier: "code-agent",
			wantID:     "code-agent",
			wantFound:  true,
		},
		{
			name:       "name match case insensitive",
			identifier: "CODE AGENT",
			wantID:     "code-agent",
			wantFound:  true,
		},
		{
			name:       "partial name match",
			identifier: "Code",
			wantID:     "code-agent",
			wantFound:  true,
		},
		{
			name:       "ID case insensitive",
			identifier: "CODE-AGENT",
			wantID:     "code-agent",
			wantFound:  true,
		},
		{
			name:       "with whitespace",
			identifier: "  code-agent  ",
			wantID:     "code-agent",
			wantFound:  true,
		},
		{
			name:       "not found",
			identifier: "non-existent",
			wantFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, found := tool.findTargetAgent(tt.identifier)

			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}

			if tt.wantFound && agent.ID != tt.wantID {
				t.Errorf("agent.ID = %s, want %s", agent.ID, tt.wantID)
			}
		})
	}
}

func TestHandoffTool_GetAvailableAgentNames(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)

	names := tool.getAvailableAgentNames()

	if names == "" {
		t.Error("expected non-empty agent names")
	}

	// Should include agents that can receive handoffs
	expectedAgents := []string{"Code Agent", "Research Agent", "Default Agent"}
	for _, agent := range expectedAgents {
		if !containsSubstring(names, agent) {
			t.Errorf("expected names to contain %q", agent)
		}
	}

	// Should not include agents that cannot receive handoffs
	if containsSubstring(names, "No Handoff Agent") {
		t.Error("should not include agents that cannot receive handoffs")
	}
}

func TestHandoffTool_ParseResult(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)

	tests := []struct {
		name    string
		result  *models.ToolResult
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil result",
			result:  nil,
			wantErr: true,
			errMsg:  "empty tool result",
		},
		{
			name: "empty content",
			result: &models.ToolResult{
				Content: "",
			},
			wantErr: true,
			errMsg:  "empty tool result",
		},
		{
			name: "invalid JSON",
			result: &models.ToolResult{
				Content: "not json",
			},
			wantErr: true,
			errMsg:  "failed to parse",
		},
		{
			name: "missing handoff_request",
			result: &models.ToolResult{
				Content: `{"status": "ok"}`,
			},
			wantErr: true,
			errMsg:  "no handoff request",
		},
		{
			name: "valid result",
			result: &models.ToolResult{
				Content: `{"handoff_request": {"from_agent_id": "agent-1", "to_agent_id": "agent-2", "reason": "test"}, "status": "initiated"}`,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := tool.ParseResult(tt.result)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				} else if !containsSubstring(err.Error(), tt.errMsg) {
					t.Errorf("expected error to contain %q, got %v", tt.errMsg, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if req == nil {
				t.Error("expected request")
			}
		})
	}
}

func TestIsHandoffTool(t *testing.T) {
	tests := []struct {
		name     string
		toolCall *models.ToolCall
		want     bool
	}{
		{
			name:     "nil tool call",
			toolCall: nil,
			want:     false,
		},
		{
			name: "handoff tool",
			toolCall: &models.ToolCall{
				Name: "handoff",
			},
			want: true,
		},
		{
			name: "other tool",
			toolCall: &models.ToolCall{
				Name: "exec",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsHandoffTool(tt.toolCall)
			if got != tt.want {
				t.Errorf("IsHandoffTool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewReturnTool(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewReturnTool(orch)

	if tool == nil {
		t.Fatal("expected tool to be created")
	}

	if tool.Name() != "return_control" {
		t.Errorf("expected name 'return_control', got %q", tool.Name())
	}
}

func TestReturnTool_Description(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewReturnTool(orch)

	desc := tool.Description()

	if desc == "" {
		t.Error("expected non-empty description")
	}

	expectedPhrases := []string{
		"Return control",
		"handed off",
		"completed",
	}

	for _, phrase := range expectedPhrases {
		if !containsSubstring(desc, phrase) {
			t.Errorf("expected description to contain %q", phrase)
		}
	}
}

func TestReturnTool_Schema(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewReturnTool(orch)

	schema := tool.Schema()

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}

	props, ok := schemaMap["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in schema")
	}

	requiredProps := []string{"summary"}
	for _, prop := range requiredProps {
		if _, ok := props[prop]; !ok {
			t.Errorf("expected '%s' property in schema", prop)
		}
	}

	optionalProps := []string{"result", "success"}
	for _, prop := range optionalProps {
		if _, ok := props[prop]; !ok {
			t.Errorf("expected '%s' property in schema", prop)
		}
	}
}

func TestReturnTool_Execute(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewReturnTool(orch)

	tests := []struct {
		name        string
		ctx         context.Context
		input       ReturnToolInput
		wantError   bool
		errContains string
	}{
		{
			name: "valid return",
			ctx:  WithHandoffStack(WithCurrentAgent(context.Background(), "code-agent"), []string{"default-agent"}),
			input: ReturnToolInput{
				Summary: "Task completed",
				Result:  "Here are the results",
				Success: true,
			},
			wantError: false,
		},
		{
			name: "no handoff stack",
			ctx:  WithCurrentAgent(context.Background(), "code-agent"),
			input: ReturnToolInput{
				Summary: "Task completed",
				Success: true,
			},
			wantError:   true,
			errContains: "No previous agent to return to",
		},
		{
			name: "empty handoff stack",
			ctx:  WithHandoffStack(WithCurrentAgent(context.Background(), "code-agent"), []string{}),
			input: ReturnToolInput{
				Summary: "Task completed",
				Success: true,
			},
			wantError:   true,
			errContains: "No previous agent to return to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(tt.input)
			result, err := tool.Execute(tt.ctx, params)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantError {
				if !result.IsError {
					t.Error("expected error result")
				}
				if tt.errContains != "" && !containsSubstring(result.Content, tt.errContains) {
					t.Errorf("expected error to contain %q, got %s", tt.errContains, result.Content)
				}
				return
			}

			if result.IsError {
				t.Errorf("unexpected error: %s", result.Content)
			}

			// Verify result structure
			var resultData map[string]interface{}
			if err := json.Unmarshal([]byte(result.Content), &resultData); err != nil {
				t.Fatalf("invalid result JSON: %v", err)
			}

			if resultData["is_return"] != true {
				t.Error("expected is_return to be true")
			}

			if resultData["return_to"] != "default-agent" {
				t.Errorf("expected return_to 'default-agent', got %v", resultData["return_to"])
			}
		})
	}
}

func TestReturnTool_Execute_InvalidJSON(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewReturnTool(orch)
	ctx := WithHandoffStack(context.Background(), []string{"agent-1"})

	result, _ := tool.Execute(ctx, []byte("invalid"))

	if !result.IsError {
		t.Error("expected error result")
	}

	if !containsSubstring(result.Content, "Invalid return parameters") {
		t.Errorf("expected invalid parameters message, got %s", result.Content)
	}
}

func TestReturnTool_DefaultSuccess(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewReturnTool(orch)
	ctx := WithHandoffStack(WithCurrentAgent(context.Background(), "code-agent"), []string{"default-agent"})

	// Input without explicit success field (should default to true)
	input := map[string]interface{}{
		"summary": "Done",
	}

	params, _ := json.Marshal(input)
	result, _ := tool.Execute(ctx, params)

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}

	var resultData map[string]interface{}
	_ = json.Unmarshal([]byte(result.Content), &resultData)

	if resultData["success"] != true {
		t.Error("expected success to default to true")
	}
}

func TestCurrentAgentFromContextString(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		wantID string
	}{
		{
			name:   "with agent",
			ctx:    WithCurrentAgent(context.Background(), "test-agent"),
			wantID: "test-agent",
		},
		{
			name:   "without agent",
			ctx:    context.Background(),
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CurrentAgentFromContextString(tt.ctx)
			if got != tt.wantID {
				t.Errorf("CurrentAgentFromContextString() = %q, want %q", got, tt.wantID)
			}
		})
	}
}

func TestNewListAgentsTool(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewListAgentsTool(orch)

	if tool == nil {
		t.Fatal("expected tool to be created")
	}

	if tool.Name() != "list_agents" {
		t.Errorf("expected name 'list_agents', got %q", tool.Name())
	}
}

func TestListAgentsTool_Description(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewListAgentsTool(orch)

	desc := tool.Description()

	if desc == "" {
		t.Error("expected non-empty description")
	}

	expectedPhrases := []string{
		"List",
		"available agents",
		"capabilities",
	}

	for _, phrase := range expectedPhrases {
		if !containsSubstring(desc, phrase) {
			t.Errorf("expected description to contain %q", phrase)
		}
	}
}

func TestListAgentsTool_Schema(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewListAgentsTool(orch)

	schema := tool.Schema()

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}

	if schemaMap["type"] != "object" {
		t.Error("expected type to be 'object'")
	}

	// Should have empty properties (no required input)
	props := schemaMap["properties"].(map[string]interface{})
	if len(props) != 0 {
		t.Errorf("expected empty properties, got %d", len(props))
	}
}

func TestListAgentsTool_Execute(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewListAgentsTool(orch)
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("{}"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}

	if result.Content == "" {
		t.Error("expected non-empty content")
	}

	// Verify content includes agent information
	expectedContent := []string{
		"Available Agents",
		"Code Agent",
		"Research Agent",
		"Default Agent",
		"No Handoff Agent",
		"ID",
		"Description",
		"Can receive handoffs",
	}

	for _, expected := range expectedContent {
		if !containsSubstring(result.Content, expected) {
			t.Errorf("expected content to contain %q", expected)
		}
	}

	// Verify tool lists are included for agents with tools
	if !containsSubstring(result.Content, "exec") || !containsSubstring(result.Content, "write") {
		t.Error("expected tools to be listed for code-agent")
	}
}

func TestHandoffToolInput_Fields(t *testing.T) {
	input := HandoffToolInput{
		TargetAgent:    "test-agent",
		Reason:         "Test reason",
		Context:        "Additional context",
		ReturnExpected: true,
	}

	if input.TargetAgent != "test-agent" {
		t.Error("expected TargetAgent to be set")
	}

	if input.Reason != "Test reason" {
		t.Error("expected Reason to be set")
	}

	if input.Context != "Additional context" {
		t.Error("expected Context to be set")
	}

	if !input.ReturnExpected {
		t.Error("expected ReturnExpected to be true")
	}
}

func TestReturnToolInput_Fields(t *testing.T) {
	input := ReturnToolInput{
		Summary: "Task summary",
		Result:  "Task result",
		Success: true,
	}

	if input.Summary != "Task summary" {
		t.Error("expected Summary to be set")
	}

	if input.Result != "Task result" {
		t.Error("expected Result to be set")
	}

	if !input.Success {
		t.Error("expected Success to be true")
	}
}

func TestHandoffTool_HandoffRequestFields(t *testing.T) {
	orch := createHandoffToolTestOrchestrator()
	tool := NewHandoffTool(orch)
	ctx := WithCurrentAgent(context.Background(), "default-agent")

	input := HandoffToolInput{
		TargetAgent:    "code-agent",
		Reason:         "Test handoff",
		Context:        "Context info",
		ReturnExpected: true,
	}

	params, _ := json.Marshal(input)
	result, _ := tool.Execute(ctx, params)

	var resultData map[string]interface{}
	_ = json.Unmarshal([]byte(result.Content), &resultData)

	handoffReq := resultData["handoff_request"].(map[string]interface{})

	// Verify all fields
	if handoffReq["from_agent_id"] != "default-agent" {
		t.Error("expected from_agent_id to be set")
	}

	if handoffReq["to_agent_id"] != "code-agent" {
		t.Error("expected to_agent_id to be set")
	}

	if handoffReq["reason"] != "Test handoff" {
		t.Error("expected reason to be set")
	}

	if handoffReq["return_expected"] != true {
		t.Error("expected return_expected to be true")
	}

	// Check context
	ctx2 := handoffReq["context"].(map[string]interface{})
	if ctx2["summary"] != "Context info" {
		t.Error("expected context summary to be set")
	}

	if ctx2["task"] != "Test handoff" {
		t.Error("expected context task to be set")
	}
}
