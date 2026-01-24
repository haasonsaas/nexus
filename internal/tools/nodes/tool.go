package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/internal/observability"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Tool exposes a minimal node control surface over connected edges.
type Tool struct {
	manager  *edge.Manager
	tofuAuth *edge.TOFUAuthenticator
}

// NewTool creates a nodes tool.
func NewTool(manager *edge.Manager, tofuAuth *edge.TOFUAuthenticator) *Tool {
	return &Tool{manager: manager, tofuAuth: tofuAuth}
}

func (t *Tool) Name() string { return "nodes" }

func (t *Tool) Description() string {
	return "Inspect and control connected edge nodes (status/describe/pending/approve/reject/invoke)."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: status, describe, pending, approve, reject, invoke.",
			},
			"edge_id": map[string]interface{}{
				"type":        "string",
				"description": "Edge/node identifier.",
			},
			"tool": map[string]interface{}{
				"type":        "string",
				"description": "Tool name to invoke on an edge (invoke action).",
			},
			"params": map[string]interface{}{
				"type":        "object",
				"description": "Parameters to pass to the edge tool (invoke action).",
			},
			"timeout_ms": map[string]interface{}{
				"type":        "number",
				"description": "Override tool timeout in milliseconds (invoke action).",
			},
			"approved": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether the invocation has been approved (invoke action).",
			},
			"approved_by": map[string]interface{}{
				"type":        "string",
				"description": "Approver identifier (approve action).",
			},
		},
		"required": []string{"action"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *Tool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.manager == nil {
		return toolError("edge manager unavailable"), nil
	}
	var input struct {
		Action     string          `json:"action"`
		EdgeID     string          `json:"edge_id"`
		Tool       string          `json:"tool"`
		Params     json.RawMessage `json:"params"`
		TimeoutMS  int             `json:"timeout_ms"`
		Approved   bool            `json:"approved"`
		ApprovedBy string          `json:"approved_by"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return toolError("action is required"), nil
	}

	switch action {
	case "status", "list":
		edges := t.manager.ListEdges()
		items := make([]json.RawMessage, 0, len(edges))
		for _, edgeStatus := range edges {
			payload, err := marshalProto(edgeStatus)
			if err != nil {
				return toolError(fmt.Sprintf("encode edge: %v", err)), nil
			}
			items = append(items, payload)
		}
		return jsonResult(map[string]interface{}{
			"edges": items,
		}), nil
	case "describe":
		edgeID := strings.TrimSpace(input.EdgeID)
		if edgeID == "" {
			return toolError("edge_id is required"), nil
		}
		edgeStatus, ok := t.manager.GetEdge(edgeID)
		if !ok {
			return toolError("edge not found"), nil
		}
		payload, err := marshalProto(edgeStatus)
		if err != nil {
			return toolError(fmt.Sprintf("encode edge: %v", err)), nil
		}
		return jsonResult(map[string]interface{}{
			"edge": payload,
		}), nil
	case "pending":
		if t.tofuAuth == nil {
			return toolError("pending requests unsupported (edge auth mode is not tofu)"), nil
		}
		pending := t.tofuAuth.ListPending()
		items := make([]map[string]interface{}, 0, len(pending))
		for _, p := range pending {
			items = append(items, map[string]interface{}{
				"edge_id":      p.EdgeID,
				"name":         p.Name,
				"requested_at": p.RequestAt.Format(time.RFC3339),
			})
		}
		return jsonResult(map[string]interface{}{
			"pending": items,
		}), nil
	case "approve":
		if t.tofuAuth == nil {
			return toolError("approve unsupported (edge auth mode is not tofu)"), nil
		}
		edgeID := strings.TrimSpace(input.EdgeID)
		if edgeID == "" {
			return toolError("edge_id is required"), nil
		}
		approvedBy := strings.TrimSpace(input.ApprovedBy)
		if approvedBy == "" {
			approvedBy = "nodes tool"
		}
		if err := t.tofuAuth.Approve(edgeID, approvedBy); err != nil {
			return toolError(fmt.Sprintf("approve: %v", err)), nil
		}
		return jsonResult(map[string]interface{}{
			"status":  "approved",
			"edge_id": edgeID,
		}), nil
	case "reject":
		if t.tofuAuth == nil {
			return toolError("reject unsupported (edge auth mode is not tofu)"), nil
		}
		edgeID := strings.TrimSpace(input.EdgeID)
		if edgeID == "" {
			return toolError("edge_id is required"), nil
		}
		if err := t.tofuAuth.Reject(edgeID); err != nil {
			return toolError(fmt.Sprintf("reject: %v", err)), nil
		}
		return jsonResult(map[string]interface{}{
			"status":  "rejected",
			"edge_id": edgeID,
		}), nil
	case "invoke":
		edgeID := strings.TrimSpace(input.EdgeID)
		if edgeID == "" {
			return toolError("edge_id is required"), nil
		}
		toolName := strings.TrimSpace(input.Tool)
		if toolName == "" {
			return toolError("tool is required"), nil
		}
		runID := observability.GetRunID(ctx)
		toolCallID := observability.GetToolCallID(ctx)
		sessionID := ""
		if session := agent.SessionFromContext(ctx); session != nil {
			sessionID = session.ID
		}

		payload := input.Params
		if len(payload) == 0 {
			payload = []byte("{}")
		}
		opts := edge.ExecuteOptions{
			RunID:     runID,
			SessionID: sessionID,
			Approved:  input.Approved,
		}
		if input.TimeoutMS > 0 {
			opts.Timeout = time.Duration(input.TimeoutMS) * time.Millisecond
		}
		if toolCallID != "" {
			opts.Metadata = map[string]string{"tool_call_id": toolCallID}
		}

		result, err := t.manager.ExecuteTool(ctx, edgeID, toolName, string(payload), opts)
		if err != nil {
			return toolError(fmt.Sprintf("invoke: %v", err)), nil
		}

		artifacts := make([]agent.Artifact, 0, len(result.Artifacts))
		for _, art := range result.Artifacts {
			artifacts = append(artifacts, agent.Artifact{
				ID:       art.Id,
				Type:     art.Type,
				MimeType: art.MimeType,
				Filename: art.Filename,
				Data:     art.Data,
			})
		}
		return &agent.ToolResult{
			Content:   result.Content,
			IsError:   result.IsError,
			Artifacts: artifacts,
		}, nil
	default:
		return toolError("unsupported action"), nil
	}
}

func jsonResult(payload map[string]interface{}) *agent.ToolResult {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err))
	}
	return &agent.ToolResult{Content: string(encoded)}
}

func marshalProto(msg proto.Message) (json.RawMessage, error) {
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

func toolError(message string) *agent.ToolResult {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
