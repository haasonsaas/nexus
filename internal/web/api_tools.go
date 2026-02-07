package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/haasonsaas/nexus/internal/tools/naming"
	"github.com/haasonsaas/nexus/pkg/models"
)

// apiTools handles GET /api/tools.
func (h *Handler) apiTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tools := h.listTools(r.Context())
	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "tools/list.html", tools)
		return
	}
	h.jsonResponse(w, apiToolsResponse{Tools: tools})
}

func (h *Handler) listTools(_ context.Context) []models.ToolSummary {
	if h == nil || h.config == nil {
		return nil
	}

	results := make([]models.ToolSummary, 0)
	if h.config.ToolSummaryProvider != nil {
		results = append(results, h.config.ToolSummaryProvider.ToolSummaries()...)
	}

	if h.config.EdgeManager != nil {
		for _, tool := range h.config.EdgeManager.GetTools() {
			if tool == nil {
				continue
			}
			identity := naming.EdgeTool(tool.EdgeID, tool.Name)
			entry := models.ToolSummary{
				Name:        identity.SafeName,
				Description: tool.Description,
				Source:      "edge",
				Namespace:   tool.EdgeID,
				Canonical:   identity.CanonicalName,
			}
			if raw := strings.TrimSpace(tool.InputSchema); raw != "" && json.Valid([]byte(raw)) {
				entry.Schema = json.RawMessage(raw)
			}
			results = append(results, entry)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Source != results[j].Source {
			return results[i].Source < results[j].Source
		}
		if results[i].Namespace != results[j].Namespace {
			return results[i].Namespace < results[j].Namespace
		}
		return results[i].Name < results[j].Name
	})

	return results
}
