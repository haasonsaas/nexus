package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Supervisor implements the supervisor pattern for multi-agent orchestration.
// A supervisor agent acts as a central coordinator that:
//   - Analyzes incoming requests
//   - Delegates tasks to specialist agents
//   - Manages the overall conversation flow
//   - Synthesizes results from multiple agents
//
// This pattern is useful when you want centralized control and decision-making,
// as opposed to peer-to-peer handoffs where agents decide independently.
type Supervisor struct {
	orchestrator *Orchestrator

	// supervisorID is the ID of the supervisor agent.
	supervisorID string

	// delegationPrompt is appended to the supervisor's system prompt.
	delegationPrompt string

	// maxDelegations limits how many delegations per conversation turn.
	maxDelegations int

	// allowParallel enables parallel delegation to multiple agents.
	allowParallel bool
}

// NewSupervisor creates a new supervisor for the given agent ID.
func NewSupervisor(orchestrator *Orchestrator, supervisorID string) *Supervisor {
	return &Supervisor{
		orchestrator:   orchestrator,
		supervisorID:   supervisorID,
		maxDelegations: 5,
		allowParallel:  false,
	}
}

// SetDelegationPrompt sets a custom delegation prompt for the supervisor.
func (s *Supervisor) SetDelegationPrompt(prompt string) {
	s.delegationPrompt = prompt
}

// SetMaxDelegations sets the maximum delegations per turn.
func (s *Supervisor) SetMaxDelegations(max int) {
	s.maxDelegations = max
}

// SetAllowParallel enables or disables parallel delegation.
func (s *Supervisor) SetAllowParallel(allow bool) {
	s.allowParallel = allow
}

// SelectAgent determines which agent should handle a message.
// In supervisor mode, this typically returns the supervisor unless
// a delegation is already in progress.
func (s *Supervisor) SelectAgent(ctx context.Context, session *models.Session, msg *models.Message, meta *SessionMetadata) (string, error) {
	// If there's an active handoff stack, continue with the current delegated agent
	if len(meta.ActiveHandoffStack) > 0 && meta.CurrentAgentID != s.supervisorID {
		return meta.CurrentAgentID, nil
	}

	// Otherwise, the supervisor handles the request
	return s.supervisorID, nil
}

// BuildSupervisorPrompt creates the system prompt addition for supervisor behavior.
func (s *Supervisor) BuildSupervisorPrompt() string {
	agents := s.orchestrator.ListAgents()

	var sb strings.Builder
	sb.WriteString("\n\n## Supervisor Role\n\n")
	sb.WriteString("You are a supervisor agent coordinating a team of specialists. ")
	sb.WriteString("Analyze user requests and delegate to the appropriate specialist when needed.\n\n")

	sb.WriteString("### Available Specialists\n\n")
	for _, agent := range agents {
		if agent.ID == s.supervisorID {
			continue // Don't list self
		}
		if !agent.CanReceiveHandoffs {
			continue
		}
		sb.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n", agent.Name, agent.ID, agent.Description))
		if len(agent.Tools) > 0 {
			sb.WriteString(fmt.Sprintf("  - Tools: %s\n", strings.Join(agent.Tools, ", ")))
		}
	}

	sb.WriteString("\n### Delegation Guidelines\n\n")
	sb.WriteString("1. Analyze the user's request to understand what expertise is needed\n")
	sb.WriteString("2. If you can handle it directly, do so\n")
	sb.WriteString("3. If specialist expertise is needed, use the `delegate` tool\n")
	sb.WriteString("4. After delegation, synthesize the specialist's response for the user\n")
	sb.WriteString("5. You can delegate to multiple specialists if needed\n")

	if s.delegationPrompt != "" {
		sb.WriteString("\n### Additional Instructions\n\n")
		sb.WriteString(s.delegationPrompt)
	}

	return sb.String()
}

// DelegateTool is a specialized handoff tool for supervisor delegation.
type DelegateTool struct {
	supervisor *Supervisor
}

// NewDelegateTool creates a new delegation tool for the supervisor.
func NewDelegateTool(supervisor *Supervisor) *DelegateTool {
	return &DelegateTool{
		supervisor: supervisor,
	}
}

// Name returns the tool name.
func (d *DelegateTool) Name() string {
	return "delegate"
}

// Description returns the tool description.
func (d *DelegateTool) Description() string {
	agents := d.supervisor.orchestrator.ListAgents()
	var agentList strings.Builder
	for _, a := range agents {
		if a.ID == d.supervisor.supervisorID {
			continue
		}
		if a.CanReceiveHandoffs {
			agentList.WriteString(fmt.Sprintf("\n- %s (%s): %s", a.Name, a.ID, a.Description))
		}
	}

	return fmt.Sprintf(`Delegate a task to a specialist agent.

Use this when the user's request requires expertise from a specialist.
The specialist will complete the task and return results to you.

Available specialists:%s

Provide clear instructions about what you need the specialist to do.`, agentList.String())
}

// Schema returns the JSON schema for delegation input.
func (d *DelegateTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"specialist": map[string]any{
				"type":        "string",
				"description": "The ID of the specialist to delegate to",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Clear description of what the specialist should do",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Relevant context from the conversation",
			},
			"expected_output": map[string]any{
				"type":        "string",
				"description": "What kind of response you expect from the specialist",
			},
		},
		"required": []string{"specialist", "task"},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

// DelegateInput is the input for the delegate tool.
type DelegateInput struct {
	Specialist     string `json:"specialist"`
	Task           string `json:"task"`
	Context        string `json:"context,omitempty"`
	ExpectedOutput string `json:"expected_output,omitempty"`
}

// Execute processes a delegation request.
func (d *DelegateTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input DelegateInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid delegation parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Validate specialist exists
	specialist, ok := d.supervisor.orchestrator.GetAgent(input.Specialist)
	if !ok {
		// Try finding by name
		for _, agent := range d.supervisor.orchestrator.ListAgents() {
			if strings.EqualFold(agent.Name, input.Specialist) || strings.EqualFold(agent.ID, input.Specialist) {
				specialist = agent
				ok = true
				break
			}
		}
	}

	if !ok {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Specialist not found: %s", input.Specialist),
			IsError: true,
		}, nil
	}

	if !specialist.CanReceiveHandoffs {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Specialist %s cannot receive delegations", specialist.Name),
			IsError: true,
		}, nil
	}

	// Build delegation response (similar to handoff but framed as delegation)
	resultData, err := json.Marshal(map[string]any{
		"handoff_request": &HandoffRequest{
			FromAgentID:    d.supervisor.supervisorID,
			ToAgentID:      specialist.ID,
			Reason:         input.Task,
			ReturnExpected: true, // Delegations always return
			Context: &SharedContext{
				Task:    input.Task,
				Summary: input.Context,
				Metadata: map[string]any{
					"expected_output": input.ExpectedOutput,
					"is_delegation":   true,
				},
			},
		},
		"target_agent":  specialist.ID,
		"target_name":   specialist.Name,
		"status":        "delegated",
		"is_delegation": true,
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to create delegation: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: string(resultData),
		IsError: false,
	}, nil
}

// ReportTool allows specialists to report back to the supervisor.
type ReportTool struct {
	supervisor *Supervisor
}

// NewReportTool creates a new report tool.
func NewReportTool(supervisor *Supervisor) *ReportTool {
	return &ReportTool{
		supervisor: supervisor,
	}
}

// Name returns the tool name.
func (r *ReportTool) Name() string {
	return "report"
}

// Description returns the tool description.
func (r *ReportTool) Description() string {
	return `Report your findings back to the supervisor.

Use this when you have completed your delegated task and want to
return your results to the supervisor for synthesis.

Provide a clear summary of what you found and any relevant details.`
}

// Schema returns the JSON schema for the report input.
func (r *ReportTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "Summary of your findings",
			},
			"details": map[string]any{
				"type":        "string",
				"description": "Detailed results or data",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"complete", "partial", "failed"},
				"description": "Status of the task",
			},
			"follow_up": map[string]any{
				"type":        "string",
				"description": "Suggested follow-up actions if any",
			},
		},
		"required": []string{"summary", "status"},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

// ReportInput is the input for the report tool.
type ReportInput struct {
	Summary  string `json:"summary"`
	Details  string `json:"details,omitempty"`
	Status   string `json:"status"`
	FollowUp string `json:"follow_up,omitempty"`
}

// Execute processes a report back to the supervisor.
func (r *ReportTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input ReportInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid report parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Get current agent
	currentAgentID, _ := CurrentAgentFromContext(ctx)

	// Build return response
	resultData, err := json.Marshal(map[string]any{
		"return_to": r.supervisor.supervisorID,
		"summary":   input.Summary,
		"details":   input.Details,
		"status":    input.Status,
		"follow_up": input.FollowUp,
		"from":      currentAgentID,
		"is_return": true,
		"is_report": true,
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to create report: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: string(resultData),
		IsError: false,
	}, nil
}

// SetupSupervisorAgent configures an agent as a supervisor.
// This adds the necessary tools and system prompt modifications.
func (s *Supervisor) SetupSupervisorAgent() error {
	runtime, ok := s.orchestrator.GetRuntime(s.supervisorID)
	if !ok {
		return fmt.Errorf("supervisor agent not found: %s", s.supervisorID)
	}

	// Register supervisor-specific tools
	runtime.RegisterTool(NewDelegateTool(s))
	runtime.RegisterTool(NewListAgentsTool(s.orchestrator))

	// Register report tool with all specialist agents
	reportTool := NewReportTool(s)
	for id, agentRuntime := range s.orchestrator.runtimes {
		if id != s.supervisorID {
			agentRuntime.RegisterTool(reportTool)
		}
	}

	return nil
}

// SupervisorConfig holds configuration for supervisor behavior.
type SupervisorConfig struct {
	// SupervisorID is the agent that acts as supervisor.
	SupervisorID string `json:"supervisor_id" yaml:"supervisor_id"`

	// DelegationPrompt is additional instructions for the supervisor.
	DelegationPrompt string `json:"delegation_prompt,omitempty" yaml:"delegation_prompt"`

	// MaxDelegations limits delegations per turn.
	MaxDelegations int `json:"max_delegations,omitempty" yaml:"max_delegations"`

	// AllowParallel enables parallel delegation.
	AllowParallel bool `json:"allow_parallel,omitempty" yaml:"allow_parallel"`

	// AutoSynthesize automatically synthesizes specialist responses.
	AutoSynthesize bool `json:"auto_synthesize,omitempty" yaml:"auto_synthesize"`

	// SynthesisPrompt guides how to synthesize multiple responses.
	SynthesisPrompt string `json:"synthesis_prompt,omitempty" yaml:"synthesis_prompt"`
}

// ApplyConfig applies configuration to the supervisor.
func (s *Supervisor) ApplyConfig(config *SupervisorConfig) {
	if config.DelegationPrompt != "" {
		s.delegationPrompt = config.DelegationPrompt
	}
	if config.MaxDelegations > 0 {
		s.maxDelegations = config.MaxDelegations
	}
	s.allowParallel = config.AllowParallel
}

// DefaultSupervisorSystemPrompt returns a default system prompt for supervisors.
func DefaultSupervisorSystemPrompt() string {
	return `You are a supervisor agent coordinating a team of specialist agents.

Your responsibilities:
1. Understand user requests and determine what expertise is needed
2. Delegate tasks to appropriate specialists using the delegate tool
3. Synthesize responses from specialists into coherent answers
4. Handle tasks directly when no specialist is needed
5. Manage multi-step workflows that require multiple specialists

Guidelines:
- Always analyze the request before delegating
- Provide clear, specific instructions when delegating
- Combine and summarize specialist responses for the user
- If a task fails, consider trying a different specialist or approach
- Be transparent about what specialists are working on

You have access to the delegate tool to assign tasks and the list_agents tool
to see available specialists.`
}
