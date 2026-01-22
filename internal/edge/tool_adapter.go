package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/internal/tools/naming"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// ToolAdapter wraps an edge tool to implement the agent.Tool interface.
// This allows edge tools to be used seamlessly with the agent runtime.
type ToolAdapter struct {
	tool            *EdgeTool
	manager         *Manager
	identity        naming.ToolIdentity
	approvalManager *policy.ApprovalManager
}

// NewToolAdapter creates a tool adapter for an edge tool.
func NewToolAdapter(tool *EdgeTool, manager *Manager) *ToolAdapter {
	return &ToolAdapter{
		tool:     tool,
		manager:  manager,
		identity: naming.EdgeTool(tool.EdgeID, tool.Name),
	}
}

// NewToolAdapterWithApproval creates a tool adapter with approval gating.
func NewToolAdapterWithApproval(tool *EdgeTool, manager *Manager, approvalManager *policy.ApprovalManager) *ToolAdapter {
	return &ToolAdapter{
		tool:            tool,
		manager:         manager,
		identity:        naming.EdgeTool(tool.EdgeID, tool.Name),
		approvalManager: approvalManager,
	}
}

// Name returns the safe name for LLM function calling.
func (a *ToolAdapter) Name() string {
	return a.identity.SafeName
}

// Description returns the tool description.
func (a *ToolAdapter) Description() string {
	return a.tool.Description
}

// Schema returns the JSON Schema for tool parameters.
func (a *ToolAdapter) Schema() json.RawMessage {
	return json.RawMessage(a.tool.InputSchema)
}

// Execute runs the tool on the edge.
func (a *ToolAdapter) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	// Get run context for tracing
	runID := observability.GetRunID(ctx)
	toolCallID := observability.GetToolCallID(ctx)
	sessionID := ""
	if session := agent.SessionFromContext(ctx); session != nil {
		sessionID = session.ID
	}

	// Check approval if approval manager is configured
	approved := true
	if a.approvalManager != nil && a.tool.RequiresApproval {
		// Use medium risk level by default for edge tools that require approval
		riskLevel := pb.RiskLevel_RISK_LEVEL_MEDIUM

		err := a.approvalManager.CheckApproval(ctx, a.identity.SafeName, a.tool.EdgeID, string(params), sessionID, "", riskLevel)
		if err != nil {
			if errors.Is(err, policy.ErrApprovalRequired) {
				// Wait for approval decision
				if waitErr := a.waitForApproval(ctx, err); waitErr != nil {
					return &agent.ToolResult{
						Content: fmt.Sprintf("Tool execution denied: %v", waitErr),
						IsError: true,
					}, nil
				}
				approved = true
			} else {
				return &agent.ToolResult{
					Content: fmt.Sprintf("Approval check failed: %v", err),
					IsError: true,
				}, nil
			}
		}
	}

	// Determine timeout
	timeout := 60 * time.Second
	if a.tool.TimeoutSeconds > 0 {
		timeout = time.Duration(a.tool.TimeoutSeconds) * time.Second
	}

	// Pass correlation IDs through metadata for edge-side logging
	metadata := make(map[string]string)
	if toolCallID != "" {
		metadata["tool_call_id"] = toolCallID
	}

	result, err := a.manager.ExecuteTool(ctx, a.tool.EdgeID, a.tool.Name, string(params), ExecuteOptions{
		RunID:     runID,
		SessionID: sessionID,
		Timeout:   timeout,
		Approved:  approved,
		Metadata:  metadata,
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Edge tool execution failed: %v", err),
			IsError: true,
		}, nil
	}

	if result.IsError {
		return &agent.ToolResult{
			Content: result.Content,
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: result.Content,
		IsError: false,
	}, nil
}

// waitForApproval waits for an approval decision when required.
func (a *ToolAdapter) waitForApproval(ctx context.Context, approvalErr error) error {
	// Extract the request ID from the error (format: "approval required: request_id=xxx")
	errStr := approvalErr.Error()
	var requestID string
	if _, err := fmt.Sscanf(errStr, "approval required: request_id=%s", &requestID); err != nil {
		return fmt.Errorf("invalid approval error format: %w", approvalErr)
	}

	// Wait for the approval decision
	return a.approvalManager.WaitForApproval(ctx, requestID)
}

// Identity returns the tool's canonical identity.
func (a *ToolAdapter) Identity() naming.ToolIdentity {
	return a.identity
}

// RequiresApproval returns whether this tool needs approval.
func (a *ToolAdapter) RequiresApproval() bool {
	return a.tool.RequiresApproval
}

// ProducesArtifacts returns whether this tool produces artifacts.
func (a *ToolAdapter) ProducesArtifacts() bool {
	return a.tool.ProducesArtifacts
}

// EdgeID returns the ID of the edge providing this tool.
func (a *ToolAdapter) EdgeID() string {
	return a.tool.EdgeID
}

// ToolProvider provides edge tools to the agent runtime.
type ToolProvider struct {
	manager         *Manager
	approvalManager *policy.ApprovalManager
}

// NewToolProvider creates a tool provider.
func NewToolProvider(manager *Manager) *ToolProvider {
	return &ToolProvider{manager: manager}
}

// NewToolProviderWithApproval creates a tool provider with approval gating.
func NewToolProviderWithApproval(manager *Manager, approvalManager *policy.ApprovalManager) *ToolProvider {
	return &ToolProvider{manager: manager, approvalManager: approvalManager}
}

// GetTools returns all available edge tools as agent.Tool interfaces.
func (p *ToolProvider) GetTools() []agent.Tool {
	edgeTools := p.manager.GetTools()
	tools := make([]agent.Tool, len(edgeTools))
	for i, et := range edgeTools {
		if p.approvalManager != nil {
			tools[i] = NewToolAdapterWithApproval(et, p.manager, p.approvalManager)
		} else {
			tools[i] = NewToolAdapter(et, p.manager)
		}
	}
	return tools
}

// GetTool returns a specific edge tool.
func (p *ToolProvider) GetTool(edgeID, toolName string) (agent.Tool, bool) {
	tool, ok := p.manager.GetTool(edgeID, toolName)
	if !ok {
		return nil, false
	}
	if p.approvalManager != nil {
		return NewToolAdapterWithApproval(tool, p.manager, p.approvalManager), true
	}
	return NewToolAdapter(tool, p.manager), true
}

// GetToolByCanonical returns a tool by its canonical name.
func (p *ToolProvider) GetToolByCanonical(canonical string) (agent.Tool, bool) {
	identity, err := naming.Parse(canonical)
	if err != nil || identity.Source != naming.SourceEdge {
		return nil, false
	}
	return p.GetTool(identity.Namespace, identity.Name)
}
