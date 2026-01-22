package web

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// SystemStatus holds system health information.
type SystemStatus struct {
	Uptime         time.Duration   `json:"uptime"`
	UptimeString   string          `json:"uptime_string"`
	GoVersion      string          `json:"go_version"`
	NumGoroutines  int             `json:"num_goroutines"`
	MemAllocMB     float64         `json:"mem_alloc_mb"`
	MemSysMB       float64         `json:"mem_sys_mb"`
	NumCPU         int             `json:"num_cpu"`
	SessionCount   int             `json:"session_count"`
	DatabaseStatus string          `json:"database_status"`
	Channels       []ChannelStatus `json:"channels"`
}

// ChannelStatus holds channel health information.
type ChannelStatus struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
}

// APISessionListResponse is the JSON response for session list.
type APISessionListResponse struct {
	Sessions []*SessionSummary `json:"sessions"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	HasMore  bool              `json:"has_more"`
}

// SessionSummary is a compact session representation.
type SessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Channel   string    `json:"channel"`
	ChannelID string    `json:"channel_id"`
	AgentID   string    `json:"agent_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// APIMessagesResponse is the JSON response for messages.
type APIMessagesResponse struct {
	Messages []*models.Message `json:"messages"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	HasMore  bool              `json:"has_more"`
}

// apiSessionList handles GET /api/sessions.
func (h *Handler) apiSessionList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Parse parameters
	channelFilter := r.URL.Query().Get("channel")
	agentFilter := r.URL.Query().Get("agent")
	if agentFilter == "" {
		agentFilter = h.config.DefaultAgentID
	}

	page := parseIntParam(r, "page", 1)
	pageSize := parseIntParam(r, "size", 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	opts := sessions.ListOptions{
		Limit:  pageSize + 1,
		Offset: offset,
	}
	if channelFilter != "" {
		opts.Channel = models.ChannelType(channelFilter)
	}

	var sessionList []*models.Session
	if h.config.SessionStore != nil {
		var err error
		sessionList, err = h.config.SessionStore.List(ctx, agentFilter, opts)
		if err != nil {
			h.jsonError(w, "Failed to list sessions", http.StatusInternalServerError)
			return
		}
	}

	hasMore := len(sessionList) > pageSize
	if hasMore {
		sessionList = sessionList[:pageSize]
	}

	// Check if this is an htmx request for partial content
	if r.Header.Get("HX-Request") == "true" {
		// Render partial HTML
		data := SessionListData{
			Sessions: sessionList,
			Page:     page,
			PageSize: pageSize,
			HasMore:  hasMore,
		}
		h.renderPartial(w, "sessions/rows.html", data)
		return
	}

	// JSON response
	summaries := make([]*SessionSummary, len(sessionList))
	for i, s := range sessionList {
		summaries[i] = &SessionSummary{
			ID:        s.ID,
			Title:     s.Title,
			Channel:   string(s.Channel),
			ChannelID: s.ChannelID,
			AgentID:   s.AgentID,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
		}
	}

	h.jsonResponse(w, APISessionListResponse{
		Sessions: summaries,
		Total:    len(summaries),
		Page:     page,
		PageSize: pageSize,
		HasMore:  hasMore,
	})
}

// apiSessionMessages handles GET /api/sessions/{id}/messages.
func (h *Handler) apiSessionMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Extract session ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		h.jsonError(w, "Session ID required", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	// Pagination
	page := parseIntParam(r, "page", 1)
	pageSize := parseIntParam(r, "size", 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	var messages []*models.Message
	hasMore := false

	if h.config.SessionStore != nil {
		// Fetch messages
		allMessages, err := h.config.SessionStore.GetHistory(ctx, sessionID, pageSize*page+1)
		if err != nil {
			h.jsonError(w, "Failed to get messages", http.StatusInternalServerError)
			return
		}

		// Calculate pagination
		start := (page - 1) * pageSize
		if start >= len(allMessages) {
			messages = []*models.Message{}
		} else {
			end := start + pageSize
			if end > len(allMessages) {
				end = len(allMessages)
			} else if end < len(allMessages) {
				hasMore = true
			}
			messages = allMessages[start:end]
		}
	}

	// Check if this is an htmx request for partial content
	if r.Header.Get("HX-Request") == "true" {
		data := struct {
			Messages []*models.Message
			Page     int
			PageSize int
			HasMore  bool
		}{
			Messages: messages,
			Page:     page,
			PageSize: pageSize,
			HasMore:  hasMore,
		}
		h.renderPartial(w, "sessions/messages.html", data)
		return
	}

	h.jsonResponse(w, APIMessagesResponse{
		Messages: messages,
		Total:    len(messages),
		Page:     page,
		PageSize: pageSize,
		HasMore:  hasMore,
	})
}

// apiStatus handles GET /api/status.
func (h *Handler) apiStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	status := h.getSystemStatus(ctx)

	// Check if this is an htmx request
	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "status/metrics.html", status)
		return
	}

	h.jsonResponse(w, status)
}

// getSystemStatus gathers system health information.
func (h *Handler) getSystemStatus(ctx context.Context) *SystemStatus {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Duration(0)
	if !h.config.ServerStartTime.IsZero() {
		uptime = time.Since(h.config.ServerStartTime)
	}

	status := &SystemStatus{
		Uptime:        uptime,
		UptimeString:  formatDuration(uptime),
		GoVersion:     runtime.Version(),
		NumGoroutines: runtime.NumGoroutine(),
		MemAllocMB:    float64(m.Alloc) / 1024 / 1024,
		MemSysMB:      float64(m.Sys) / 1024 / 1024,
		NumCPU:        runtime.NumCPU(),
		Channels:      []ChannelStatus{},
	}

	// Check database status
	if h.config.SessionStore != nil {
		// Try a simple operation to verify connectivity
		_, err := h.config.SessionStore.List(ctx, h.config.DefaultAgentID, sessions.ListOptions{Limit: 1})
		if err != nil {
			status.DatabaseStatus = "error"
		} else {
			status.DatabaseStatus = "connected"
		}
	} else {
		status.DatabaseStatus = "not configured"
	}

	return status
}

// renderPartial renders a partial template for htmx requests.
func (h *Handler) renderPartial(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		h.config.Logger.Error("partial template render error", "error", err, "template", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// jsonResponse writes a JSON response.
func (h *Handler) jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.config.Logger.Error("json encode error", "error", err)
	}
}

// jsonError writes a JSON error response.
func (h *Handler) jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// userFromContext extracts the user from context if available.
func userFromContext(ctx context.Context) *models.User {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil
	}
	return user
}
