package computeruse

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/internal/observability"
)

// Config controls how the computer use tool selects its target edge.
type Config struct {
	EdgeID          string
	DisplayWidthPx  int
	DisplayHeightPx int
	DisplayNumber   int
}

// Tool exposes Claude computer use to the agent runtime by proxying to an edge.
type Tool struct {
	manager *edge.Manager
	config  Config
}

// NewTool creates a computer use tool backed by the edge manager.
func NewTool(manager *edge.Manager, cfg Config) *Tool {
	return &Tool{manager: manager, config: cfg}
}

func (t *Tool) Name() string { return "computer" }

func (t *Tool) Description() string {
	return "Control a connected computer via mouse/keyboard/screenshot actions."
}

func (t *Tool) Schema() json.RawMessage {
	return json.RawMessage(SchemaJSON)
}

func (t *Tool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.manager == nil {
		return &agent.ToolResult{Content: "edge manager unavailable", IsError: true}, nil
	}

	edgeID := strings.TrimSpace(t.config.EdgeID)
	if len(params) > 0 {
		var input struct {
			EdgeID string `json:"edge_id"`
		}
		if err := json.Unmarshal(params, &input); err == nil && strings.TrimSpace(input.EdgeID) != "" {
			edgeID = strings.TrimSpace(input.EdgeID)
		}
	}

	resolvedEdge, err := t.resolveEdge(edgeID)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	payload := string(params)
	if strings.TrimSpace(payload) == "" {
		payload = "{}"
	}

	runID := observability.GetRunID(ctx)
	toolCallID := observability.GetToolCallID(ctx)
	sessionID := ""
	if session := agent.SessionFromContext(ctx); session != nil {
		sessionID = session.ID
	}

	metadata := make(map[string]string)
	if toolCallID != "" {
		metadata["tool_call_id"] = toolCallID
	}

	result, err := t.manager.ExecuteTool(ctx, resolvedEdge, "nodes.computer_use", payload, edge.ExecuteOptions{
		RunID:     runID,
		SessionID: sessionID,
		Metadata:  metadata,
	})
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("computer_use failed: %v", err), IsError: true}, nil
	}

	artifacts := make([]agent.Artifact, 0, len(result.Artifacts))
	for _, art := range result.Artifacts {
		artifacts = append(artifacts, agent.Artifact{
			ID:       art.Id,
			Type:     art.Type,
			MimeType: art.MimeType,
			Filename: art.Filename,
			Data:     art.Data,
			URL:      art.Reference,
		})
	}

	return &agent.ToolResult{
		Content:   result.Content,
		IsError:   result.IsError,
		Artifacts: artifacts,
	}, nil
}

// ComputerUseConfig implements agent.ComputerUseConfigProvider.
func (t *Tool) ComputerUseConfig() *agent.ComputerUseConfig {
	if t.manager == nil {
		return nil
	}

	if t.config.DisplayWidthPx > 0 && t.config.DisplayHeightPx > 0 {
		return &agent.ComputerUseConfig{
			DisplayWidthPx:  t.config.DisplayWidthPx,
			DisplayHeightPx: t.config.DisplayHeightPx,
			DisplayNumber:   t.config.DisplayNumber,
		}
	}

	edgeID := strings.TrimSpace(t.config.EdgeID)
	if edgeID == "" {
		edgeID = t.autoSelectEdge()
	}
	if edgeID == "" {
		return nil
	}

	status, ok := t.manager.GetEdge(edgeID)
	if !ok || status == nil || status.Metadata == nil {
		return nil
	}

	width := parseInt(status.Metadata["display_width_px"])
	height := parseInt(status.Metadata["display_height_px"])
	if width == 0 || height == 0 {
		return nil
	}
	displayNumber := parseInt(status.Metadata["display_number"])

	return &agent.ComputerUseConfig{
		DisplayWidthPx:  width,
		DisplayHeightPx: height,
		DisplayNumber:   displayNumber,
	}
}

func (t *Tool) resolveEdge(edgeID string) (string, error) {
	if strings.TrimSpace(edgeID) != "" {
		if t.edgeHasComputerUse(edgeID) {
			return edgeID, nil
		}
		return "", fmt.Errorf("edge %q not connected or lacks nodes.computer_use", edgeID)
	}

	candidates := t.edgesWithComputerUse()
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no connected edges with nodes.computer_use")
	}
	return "", fmt.Errorf("multiple edges available for computer_use: %s", strings.Join(candidates, ", "))
}

func (t *Tool) autoSelectEdge() string {
	if strings.TrimSpace(t.config.EdgeID) != "" {
		return t.config.EdgeID
	}
	candidates := t.edgesWithComputerUse()
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

func (t *Tool) edgesWithComputerUse() []string {
	edges := t.manager.ListEdges()
	out := make([]string, 0, len(edges))
	for _, status := range edges {
		if status == nil {
			continue
		}
		for _, tool := range status.Tools {
			if tool == "nodes.computer_use" {
				out = append(out, status.EdgeId)
				break
			}
		}
	}
	return out
}

func (t *Tool) edgeHasComputerUse(edgeID string) bool {
	status, ok := t.manager.GetEdge(edgeID)
	if !ok || status == nil {
		return false
	}
	for _, tool := range status.Tools {
		if tool == "nodes.computer_use" {
			return true
		}
	}
	return false
}

func parseInt(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}
