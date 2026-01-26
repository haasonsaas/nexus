package web

import "net/http"

func (h *Handler) apiAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}
	agentID := r.URL.Query().Get("agent")
	if agentID == "" {
		agentID = r.URL.Query().Get("agent_id")
	}
	if agentID == "" {
		agentID = h.config.DefaultAgentID
	}

	overview, err := h.computeAnalyticsOverview(ctx, agentID, period)
	if err != nil {
		h.config.Logger.Error("failed to compute analytics overview", "error", err)
		if r.Header.Get("HX-Request") == "true" {
			http.Error(w, "Failed to compute analytics", http.StatusInternalServerError)
			return
		}
		h.jsonError(w, "Failed to compute analytics", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "analytics/overview.html", overview)
		return
	}

	h.jsonResponse(w, overview)
}
