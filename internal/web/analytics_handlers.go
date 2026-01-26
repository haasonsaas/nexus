package web

import "net/http"

// AnalyticsData holds data for the analytics dashboard page.
type AnalyticsData struct {
	PageData
	Overview    *AnalyticsOverview
	Period      string
	AgentFilter string
}

func (h *Handler) handleAnalytics(w http.ResponseWriter, r *http.Request) {
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
	}

	data := AnalyticsData{
		PageData: PageData{
			Title:       "Analytics",
			CurrentPath: "/analytics",
			User:        userFromContext(ctx),
		},
		Overview:    overview,
		Period:      period,
		AgentFilter: agentID,
	}

	h.render(w, "layout.html", data)
}
