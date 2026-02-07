package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

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

type apiSessionPatchRequest struct {
	Title    string         `json:"title"`
	Metadata map[string]any `json:"metadata"`
}

// apiSession routes session-scoped API calls.
func (h *Handler) apiSession(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if path == "" {
		h.jsonError(w, "Session ID required", http.StatusBadRequest)
		return
	}
	parts := strings.Split(path, "/")
	sessionID := parts[0]
	if sessionID == "" {
		h.jsonError(w, "Session ID required", http.StatusBadRequest)
		return
	}

	if len(parts) > 1 && parts[1] == "messages" {
		h.apiSessionMessages(w, r)
		return
	}

	switch r.Method {
	case http.MethodPatch, http.MethodPost:
		h.apiSessionPatch(w, r, sessionID)
	default:
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// apiSessionList handles GET /api/sessions.
func (h *Handler) apiSessionList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Parse parameters
	channelFilter := clampQueryParam(r, "channel")
	agentFilter := clampQueryParam(r, "agent")
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

// apiSessionPatch handles PATCH/POST /api/sessions/{id}.
func (h *Handler) apiSessionPatch(w http.ResponseWriter, r *http.Request, sessionID string) {
	if h.config.SessionStore == nil {
		h.jsonError(w, "Session store not configured (set database.url)", http.StatusServiceUnavailable)
		return
	}

	var req apiSessionPatchRequest
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		status, err := decodeJSONRequest(w, r, &req)
		if err != nil {
			msg := "Invalid JSON body"
			if status == http.StatusRequestEntityTooLarge {
				msg = "Request entity too large"
			}
			h.jsonError(w, msg, status)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			h.jsonError(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		req.Title = strings.TrimSpace(r.FormValue("title"))
		metadataRaw := strings.TrimSpace(r.FormValue("metadata"))
		if metadataRaw != "" {
			if err := json.Unmarshal([]byte(metadataRaw), &req.Metadata); err != nil {
				h.jsonError(w, "Invalid metadata JSON", http.StatusBadRequest)
				return
			}
		}
	}

	ctx := r.Context()
	session, err := h.config.SessionStore.Get(ctx, sessionID)
	if err != nil {
		h.jsonError(w, "Session not found", http.StatusNotFound)
		return
	}

	if req.Title != "" {
		session.Title = req.Title
	}
	if req.Metadata != nil {
		session.Metadata = req.Metadata
	}

	if err := h.config.SessionStore.Update(ctx, session); err != nil {
		h.jsonError(w, "Failed to update session", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "sessions/title.html", session)
		return
	}

	h.jsonResponse(w, &SessionSummary{
		ID:        session.ID,
		Title:     session.Title,
		Channel:   string(session.Channel),
		ChannelID: session.ChannelID,
		AgentID:   session.AgentID,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	})
}
