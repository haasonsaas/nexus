package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// HandoffTool is a tool that allows LLM agents to request handoffs to other agents.
// This enables peer-to-peer handoffs where agents can delegate tasks to specialists.
//
// Usage by LLM:
//
//	{
//	  "name": "handoff",
//	  "input": {
//	    "target_agent": "code-reviewer",
//	    "reason": "This task requires code review expertise",
//	    "context": "User wants feedback on their Python implementation",
//	    "return_expected": true
//	  }
//	}
type HandoffTool struct {
	orchestrator *Orchestrator
}

// NewHandoffTool creates a new handoff tool.
func NewHandoffTool(orchestrator *Orchestrator) *HandoffTool {
	return &HandoffTool{
		orchestrator: orchestrator,
	}
}

// Name returns the tool name.
func (h *HandoffTool) Name() string {
	return "handoff"
}

// Description returns a description of the tool for LLMs.
func (h *HandoffTool) Description() string {
	agents := h.orchestrator.ListAgents()
	var agentList strings.Builder
	for _, a := range agents {
		if a.CanReceiveHandoffs {
			agentList.WriteString(fmt.Sprintf("\n- %s (%s): %s", a.Name, a.ID, a.Description))
		}
	}

	return fmt.Sprintf(`Transfer control to another specialized agent when a task is outside your expertise or requires specific capabilities.

Use this tool when:
- A user's request requires expertise you don't have
- The task needs tools or capabilities another agent possesses
- You're asked to hand off to a specific agent
- The conversation would benefit from a specialist

Available agents:%s

Provide a clear reason for the handoff to help the receiving agent understand the context.`, agentList.String())
}

// Schema returns the JSON schema for the tool's input.
func (h *HandoffTool) Schema() json.RawMessage {
	// Build agent enum dynamically
	agents := h.orchestrator.ListAgents()
	agentIDs := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.CanReceiveHandoffs {
			agentIDs = append(agentIDs, a.ID)
		}
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_agent": map[string]any{
				"type":        "string",
				"description": "The ID or name of the agent to hand off to",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this handoff is needed - helps the receiving agent understand the context",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Additional context or summary for the receiving agent",
			},
			"return_expected": map[string]any{
				"type":        "boolean",
				"description": "Whether control should return to you after the target agent completes",
				"default":     false,
			},
		},
		"required": []string{"target_agent", "reason"},
	}

	data, _ := json.Marshal(schema)
	return data
}

// Execute processes a handoff request.
func (h *HandoffTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input HandoffToolInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid handoff parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Validate target agent
	targetAgent, ok := h.findTargetAgent(input.TargetAgent)
	if !ok {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Target agent not found: %s. Available agents: %s",
				input.TargetAgent, h.getAvailableAgentNames()),
			IsError: true,
		}, nil
	}

	if !targetAgent.CanReceiveHandoffs {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Agent '%s' cannot receive handoffs", targetAgent.Name),
			IsError: true,
		}, nil
	}

	// Get current agent from context
	currentAgentID, _ := CurrentAgentFromContext(ctx)
	if currentAgentID == "" {
		currentAgentID = "unknown"
	}

	// Prevent self-handoff
	if currentAgentID == targetAgent.ID {
		return &agent.ToolResult{
			Content: "Cannot hand off to yourself",
			IsError: true,
		}, nil
	}

	// Build the handoff request
	request := &HandoffRequest{
		FromAgentID:    currentAgentID,
		ToAgentID:      targetAgent.ID,
		Reason:         input.Reason,
		ReturnExpected: input.ReturnExpected,
		Timestamp:      time.Now(),
	}

	// Add context if provided
	if input.Context != "" {
		request.Context = &SharedContext{
			Summary: input.Context,
			Task:    input.Reason,
		}
	}

	// Serialize the handoff request for the orchestrator to process
	resultData, err := json.Marshal(map[string]any{
		"handoff_request": request,
		"target_agent":    targetAgent.ID,
		"target_name":     targetAgent.Name,
		"status":          "initiated",
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to serialize handoff request: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: string(resultData),
		IsError: false,
	}, nil
}

// findTargetAgent finds an agent by ID or name.
func (h *HandoffTool) findTargetAgent(identifier string) (*AgentDefinition, bool) {
	identifier = strings.TrimSpace(identifier)

	// Try exact ID match first
	if agent, ok := h.orchestrator.GetAgent(identifier); ok {
		return agent, true
	}

	// Try case-insensitive name/ID match
	lowerID := strings.ToLower(identifier)
	for _, agent := range h.orchestrator.ListAgents() {
		if strings.ToLower(agent.ID) == lowerID || strings.ToLower(agent.Name) == lowerID {
			return agent, true
		}
	}

	// Try partial name match
	for _, agent := range h.orchestrator.ListAgents() {
		if strings.Contains(strings.ToLower(agent.Name), lowerID) {
			return agent, true
		}
	}

	return nil, false
}

// getAvailableAgentNames returns a comma-separated list of available agent names.
func (h *HandoffTool) getAvailableAgentNames() string {
	agents := h.orchestrator.ListAgents()
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.CanReceiveHandoffs {
			names = append(names, a.Name+" ("+a.ID+")")
		}
	}
	return strings.Join(names, ", ")
}

// ParseResult parses a handoff result from tool output.
func (h *HandoffTool) ParseResult(result *models.ToolResult) (*HandoffRequest, error) {
	if result == nil || result.Content == "" {
		return nil, fmt.Errorf("empty tool result")
	}

	var data struct {
		HandoffRequest *HandoffRequest `json:"handoff_request"`
		Status         string          `json:"status"`
	}

	if err := json.Unmarshal([]byte(result.Content), &data); err != nil {
		return nil, fmt.Errorf("failed to parse handoff result: %w", err)
	}

	if data.HandoffRequest == nil {
		return nil, fmt.Errorf("no handoff request in result")
	}

	return data.HandoffRequest, nil
}

// IsHandoffTool checks if a tool call is for the handoff tool.
func IsHandoffTool(tc *models.ToolCall) bool {
	return tc != nil && tc.Name == "handoff"
}

// ReturnTool allows agents to return control to the previous agent in the handoff stack.
type ReturnTool struct {
	orchestrator *Orchestrator
}

// NewReturnTool creates a new return tool.
func NewReturnTool(orchestrator *Orchestrator) *ReturnTool {
	return &ReturnTool{
		orchestrator: orchestrator,
	}
}

// Name returns the tool name.
func (r *ReturnTool) Name() string {
	return "return_control"
}

// Description returns a description of the tool.
func (r *ReturnTool) Description() string {
	return `Return control to the agent that handed off to you.

Use this tool when:
- You have completed the task you were given
- You need to return results to the requesting agent
- The handoff specified that return was expected

Provide a summary of what you accomplished and any relevant results.`
}

// Schema returns the JSON schema for the tool's input.
func (r *ReturnTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "Summary of what was accomplished",
			},
			"result": map[string]any{
				"type":        "string",
				"description": "The result or output to return to the previous agent",
			},
			"success": map[string]any{
				"type":        "boolean",
				"description": "Whether the task was completed successfully",
				"default":     true,
			},
		},
		"required": []string{"summary"},
	}

	data, _ := json.Marshal(schema)
	return data
}

// ReturnToolInput is the input for the return tool.
type ReturnToolInput struct {
	Summary string `json:"summary"`
	Result  string `json:"result,omitempty"`
	Success bool   `json:"success"`
}

// Execute processes a return request.
func (r *ReturnTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input ReturnToolInput
	input.Success = true // Default
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid return parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Check if there's a handoff stack to return to
	stack := HandoffStackFromContext(ctx)
	if len(stack) == 0 {
		return &agent.ToolResult{
			Content: "No previous agent to return to - you are the root agent",
			IsError: true,
		}, nil
	}

	// Get the agent to return to
	previousAgentID := stack[len(stack)-1]

	// Build the return response
	resultData, err := json.Marshal(map[string]any{
		"return_to":   previousAgentID,
		"summary":     input.Summary,
		"result":      input.Result,
		"success":     input.Success,
		"status":      "returning",
		"is_return":   true,
		"return_from": CurrentAgentFromContextString(ctx),
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to serialize return request: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: string(resultData),
		IsError: false,
	}, nil
}

// CurrentAgentFromContextString is a helper that returns the agent ID or empty string.
func CurrentAgentFromContextString(ctx context.Context) string {
	id, _ := CurrentAgentFromContext(ctx)
	return id
}

// ListAgentsTool allows LLMs to discover available agents.
type ListAgentsTool struct {
	orchestrator *Orchestrator
}

// NewListAgentsTool creates a new list agents tool.
func NewListAgentsTool(orchestrator *Orchestrator) *ListAgentsTool {
	return &ListAgentsTool{
		orchestrator: orchestrator,
	}
}

// Name returns the tool name.
func (l *ListAgentsTool) Name() string {
	return "list_agents"
}

// Description returns a description of the tool.
func (l *ListAgentsTool) Description() string {
	return `List all available agents and their capabilities.

Use this tool to:
- Discover what agents are available
- Understand each agent's specialization
- Decide which agent to hand off to`
}

// Schema returns the JSON schema for the tool's input.
func (l *ListAgentsTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}

	data, _ := json.Marshal(schema)
	return data
}

// Execute returns a list of available agents.
func (l *ListAgentsTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	agents := l.orchestrator.ListAgents()

	var result strings.Builder
	result.WriteString("Available Agents:\n\n")

	for _, agent := range agents {
		result.WriteString(fmt.Sprintf("## %s\n", agent.Name))
		result.WriteString(fmt.Sprintf("- **ID**: %s\n", agent.ID))
		result.WriteString(fmt.Sprintf("- **Description**: %s\n", agent.Description))
		if len(agent.Tools) > 0 {
			result.WriteString(fmt.Sprintf("- **Tools**: %s\n", strings.Join(agent.Tools, ", ")))
		}
		result.WriteString(fmt.Sprintf("- **Can receive handoffs**: %v\n", agent.CanReceiveHandoffs))
		result.WriteString("\n")
	}

	return &agent.ToolResult{
		Content: result.String(),
		IsError: false,
	}, nil
}
