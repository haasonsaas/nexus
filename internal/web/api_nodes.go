package web

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/edge"
)

// NodeSummary is a UI-friendly edge node snapshot.
type NodeSummary struct {
	EdgeID        string            `json:"edge_id"`
	Name          string            `json:"name"`
	Status        string            `json:"status"`
	ConnectedAt   time.Time         `json:"connected_at"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	Tools         []string          `json:"tools"`
	ChannelTypes  []string          `json:"channel_types,omitempty"`
	Version       string            `json:"version,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// NodeToolSummary is a UI-friendly tool snapshot for a node.
type NodeToolSummary struct {
	EdgeID            string `json:"edge_id"`
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	InputSchema       string `json:"input_schema,omitempty"`
	RequiresApproval  bool   `json:"requires_approval,omitempty"`
	ProducesArtifacts bool   `json:"produces_artifacts,omitempty"`
	TimeoutSeconds    int    `json:"timeout_seconds,omitempty"`
}

type edgeExecuteOptions struct {
	timeoutSeconds int
	approved       bool
	sessionID      string
	runID          string
	metadata       map[string]string
}

func (o edgeExecuteOptions) toExecuteOptions() edge.ExecuteOptions {
	opts := edge.ExecuteOptions{
		RunID:     o.runID,
		SessionID: o.sessionID,
		Approved:  o.approved,
		Metadata:  o.metadata,
	}
	if o.timeoutSeconds > 0 {
		opts.Timeout = time.Duration(o.timeoutSeconds) * time.Second
	}
	return opts
}

// apiNodes handles GET /api/nodes.
func (h *Handler) apiNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.jsonResponse(w, apiNodesResponse{Nodes: h.listNodes()})
}

// apiNode handles node-specific API actions.
func (h *Handler) apiNode(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		h.jsonError(w, "Node ID required", http.StatusBadRequest)
		return
	}
	nodeID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		for _, node := range h.listNodes() {
			if node.EdgeID == nodeID {
				h.jsonResponse(w, node)
				return
			}
		}
		h.jsonError(w, "Node not found", http.StatusNotFound)
		return
	}

	if parts[1] == "tools" {
		h.apiNodeTools(w, r, nodeID, parts[2:])
		return
	}

	h.jsonError(w, "Not found", http.StatusNotFound)
}

func (h *Handler) apiNodeTools(w http.ResponseWriter, r *http.Request, nodeID string, rest []string) {
	if h.config.EdgeManager == nil {
		h.jsonError(w, "Edge manager not configured (set edge.enabled)", http.StatusServiceUnavailable)
		return
	}

	if len(rest) == 0 || rest[0] == "" {
		if r.Method != http.MethodGet {
			h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tools := h.config.EdgeManager.GetTools()
		summaries := make([]*NodeToolSummary, 0, len(tools))
		for _, tool := range tools {
			if tool == nil || tool.EdgeID != nodeID {
				continue
			}
			summaries = append(summaries, &NodeToolSummary{
				EdgeID:            tool.EdgeID,
				Name:              tool.Name,
				Description:       tool.Description,
				InputSchema:       tool.InputSchema,
				RequiresApproval:  tool.RequiresApproval,
				ProducesArtifacts: tool.ProducesArtifacts,
				TimeoutSeconds:    tool.TimeoutSeconds,
			})
		}
		h.jsonResponse(w, apiNodeToolsResponse{Tools: summaries})
		return
	}

	toolName := rest[0]
	if r.Method != http.MethodPost {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input string
	opts := edgeExecuteOptions{}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var payload struct {
			Input          string            `json:"input"`
			TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
			Approved       bool              `json:"approved,omitempty"`
			SessionID      string            `json:"session_id,omitempty"`
			RunID          string            `json:"run_id,omitempty"`
			Metadata       map[string]string `json:"metadata,omitempty"`
		}
		status, err := decodeJSONRequest(w, r, &payload)
		if err != nil {
			msg := "Invalid JSON body"
			if status == http.StatusRequestEntityTooLarge {
				msg = "Request entity too large"
			}
			h.jsonError(w, msg, status)
			return
		}
		input = payload.Input
		opts.timeoutSeconds = payload.TimeoutSeconds
		opts.approved = payload.Approved
		opts.sessionID = payload.SessionID
		opts.runID = payload.RunID
		opts.metadata = payload.Metadata
	} else {
		if err := r.ParseForm(); err != nil {
			h.jsonError(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		input = r.FormValue("input")
		opts.timeoutSeconds = parseIntParam(r, "timeout_seconds", 0)
		opts.approved = strings.EqualFold(r.FormValue("approved"), "true")
		opts.sessionID = r.FormValue("session_id")
		opts.runID = r.FormValue("run_id")
	}

	result, err := h.config.EdgeManager.ExecuteTool(r.Context(), nodeID, toolName, input, opts.toExecuteOptions())
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.jsonResponse(w, apiToolExecResponse{
		Content:      result.Content,
		IsError:      result.IsError,
		DurationMs:   result.DurationMs,
		ErrorDetails: result.ErrorDetails,
		Artifacts:    result.Artifacts,
	})
}

func (h *Handler) listNodes() []*NodeSummary {
	if h == nil || h.config == nil || h.config.EdgeManager == nil {
		return nil
	}
	edges := h.config.EdgeManager.ListEdges()
	out := make([]*NodeSummary, 0, len(edges))
	for _, edgeStatus := range edges {
		if edgeStatus == nil {
			continue
		}
		status := "unknown"
		if edgeStatus.ConnectionStatus != 0 {
			status = edgeStatus.ConnectionStatus.String()
		}
		connectedAt := time.Time{}
		if edgeStatus.ConnectedAt != nil {
			connectedAt = edgeStatus.ConnectedAt.AsTime()
		}
		lastHeartbeat := time.Time{}
		if edgeStatus.LastHeartbeat != nil {
			lastHeartbeat = edgeStatus.LastHeartbeat.AsTime()
		}
		out = append(out, &NodeSummary{
			EdgeID:        edgeStatus.EdgeId,
			Name:          edgeStatus.Name,
			Status:        status,
			ConnectedAt:   connectedAt,
			LastHeartbeat: lastHeartbeat,
			Tools:         edgeStatus.Tools,
			ChannelTypes:  edgeStatus.ChannelTypes,
			Version:       edgeStatus.Version,
			Metadata:      edgeStatus.Metadata,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].EdgeID < out[j].EdgeID
	})
	return out
}
