package web

import (
	"net/http"
	"strings"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// PageData holds common data for page templates.
type PageData struct {
	Title       string
	CurrentPath string
	User        *models.User
	Error       string
	Flash       string
}

// SessionListData holds data for the session list page.
type SessionListData struct {
	PageData
	Sessions      []*models.Session
	ChannelFilter string
	AgentFilter   string
	Channels      []string
	TotalCount    int
	Page          int
	PageSize      int
	HasMore       bool
}

// SessionDetailData holds data for the session detail page.
type SessionDetailData struct {
	PageData
	Session  *models.Session
	Messages []*models.Message
	Page     int
	PageSize int
	HasMore  bool
}

// StatusData holds data for the status dashboard.
type StatusData struct {
	PageData
	Status *SystemStatus
}

// handleIndex redirects to the sessions list.
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, h.config.BasePath+"/sessions", http.StatusFound)
}

// handleSessionList renders the session list page.
func (h *Handler) handleSessionList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse filters
	channelFilter := r.URL.Query().Get("channel")
	agentFilter := r.URL.Query().Get("agent")
	if agentFilter == "" {
		agentFilter = h.config.DefaultAgentID
	}

	// Pagination
	page := parseIntParam(r, "page", 1)
	pageSize := parseIntParam(r, "size", 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	// Build list options
	opts := sessions.ListOptions{
		Limit:  pageSize + 1, // Fetch one extra to check if there are more
		Offset: offset,
	}
	if channelFilter != "" {
		opts.Channel = models.ChannelType(channelFilter)
	}

	// Fetch sessions
	var sessionList []*models.Session
	var err error
	if h.config.SessionStore != nil {
		sessionList, err = h.config.SessionStore.List(ctx, agentFilter, opts)
		if err != nil {
			h.config.Logger.Error("failed to list sessions", "error", err)
		}
	}

	// Check if there are more results
	hasMore := len(sessionList) > pageSize
	if hasMore {
		sessionList = sessionList[:pageSize]
	}

	data := SessionListData{
		PageData: PageData{
			Title:       "Sessions",
			CurrentPath: "/sessions",
			User:        userFromContext(ctx),
		},
		Sessions:      sessionList,
		ChannelFilter: channelFilter,
		AgentFilter:   agentFilter,
		Channels:      []string{"telegram", "slack", "discord", "api", "whatsapp"},
		TotalCount:    len(sessionList),
		Page:          page,
		PageSize:      pageSize,
		HasMore:       hasMore,
	}

	h.render(w, "layout.html", data)
}

// handleSessionDetail renders the session detail page.
func (h *Handler) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract session ID from path
	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
	if sessionID == "" {
		http.Redirect(w, r, h.config.BasePath+"/sessions", http.StatusFound)
		return
	}

	// Fetch session
	var session *models.Session
	var messages []*models.Message
	var err error

	if h.config.SessionStore != nil {
		session, err = h.config.SessionStore.Get(ctx, sessionID)
		if err != nil {
			h.config.Logger.Error("failed to get session", "error", err, "id", sessionID)
			h.renderError(w, "Session not found", http.StatusNotFound)
			return
		}

		// Pagination for messages
		page := parseIntParam(r, "page", 1)
		pageSize := parseIntParam(r, "size", 50)
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 200 {
			pageSize = 50
		}

		// Fetch message history
		// Note: The store returns messages in chronological order
		// For pagination, we fetch all and slice (could be optimized with offset in store)
		allMessages, err := h.config.SessionStore.GetHistory(ctx, sessionID, pageSize*page+1)
		if err != nil {
			h.config.Logger.Error("failed to get messages", "error", err, "session_id", sessionID)
		} else {
			// Calculate pagination
			start := 0
			end := len(allMessages)
			hasMore := false

			if len(allMessages) > pageSize*page {
				hasMore = true
				end = pageSize * page
			}
			if page > 1 {
				start = (page - 1) * pageSize
			}
			if start < len(allMessages) {
				messages = allMessages[start:min(end, len(allMessages))]
			}

			data := SessionDetailData{
				PageData: PageData{
					Title:       "Session: " + truncateTitle(session),
					CurrentPath: "/sessions/" + sessionID,
					User:        userFromContext(ctx),
				},
				Session:  session,
				Messages: messages,
				Page:     page,
				PageSize: pageSize,
				HasMore:  hasMore,
			}

			h.render(w, "layout.html", data)
			return
		}
	}

	data := SessionDetailData{
		PageData: PageData{
			Title:       "Session",
			CurrentPath: "/sessions/" + sessionID,
			User:        userFromContext(ctx),
			Error:       "Session not found or error loading data",
		},
	}
	h.render(w, "layout.html", data)
}

// handleStatusDashboard renders the system status page.
func (h *Handler) handleStatusDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	status := h.getSystemStatus(ctx)

	data := StatusData{
		PageData: PageData{
			Title:       "System Status",
			CurrentPath: "/status",
			User:        userFromContext(ctx),
		},
		Status: status,
	}

	h.render(w, "layout.html", data)
}

// render executes a template and writes the result.
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		h.config.Logger.Error("template render error", "error", err, "template", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// renderError renders an error page.
func (h *Handler) renderError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	data := PageData{
		Title: "Error",
		Error: message,
	}
	if err := h.templates.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, message, code)
	}
}

// Helper functions

func parseIntParam(r *http.Request, name string, defaultVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	var result int
	if _, err := parseIntSafe(val, &result); err != nil {
		return defaultVal
	}
	return result
}

func parseIntSafe(s string, result *int) (bool, error) {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false, nil
		}
	}
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	*result = n
	return true, nil
}

func truncateTitle(s *models.Session) string {
	if s == nil {
		return ""
	}
	if s.Title != "" {
		return truncate(s.Title, 30)
	}
	return truncate(s.ID, 12)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
