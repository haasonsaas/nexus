package edge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/tools/naming"
)

// ToolAdapter wraps an edge tool to implement the agent.Tool interface.
// This allows edge tools to be used seamlessly with the agent runtime.
type ToolAdapter struct {
	tool    *EdgeTool
	manager *Manager
	identity naming.ToolIdentity
}

// NewToolAdapter creates a tool adapter for an edge tool.
func NewToolAdapter(tool *EdgeTool, manager *Manager) *ToolAdapter {
	return &ToolAdapter{
		tool:     tool,
		manager:  manager,
		identity: naming.EdgeTool(tool.EdgeID, tool.Name),
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
	runID := ""
	sessionID := ""
	if session := agent.SessionFromContext(ctx); session != nil {
		sessionID = session.ID
	}

	// Determine timeout
	timeout := 60 * time.Second
	if a.tool.TimeoutSeconds > 0 {
		timeout = time.Duration(a.tool.TimeoutSeconds) * time.Second
	}

	result, err := a.manager.ExecuteTool(ctx, a.tool.EdgeID, a.tool.Name, string(params), ExecuteOptions{
		RunID:     runID,
		SessionID: sessionID,
		Timeout:   timeout,
		Approved:  true, // TODO: integrate with approval system
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
	manager *Manager
}

// NewToolProvider creates a tool provider.
func NewToolProvider(manager *Manager) *ToolProvider {
	return &ToolProvider{manager: manager}
}

// GetTools returns all available edge tools as agent.Tool interfaces.
func (p *ToolProvider) GetTools() []agent.Tool {
	edgeTools := p.manager.GetTools()
	tools := make([]agent.Tool, len(edgeTools))
	for i, et := range edgeTools {
		tools[i] = NewToolAdapter(et, p.manager)
	}
	return tools
}

// GetTool returns a specific edge tool.
func (p *ToolProvider) GetTool(edgeID, toolName string) (agent.Tool, bool) {
	tool, ok := p.manager.GetTool(edgeID, toolName)
	if !ok {
		return nil, false
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
