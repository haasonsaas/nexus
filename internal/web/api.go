package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"gopkg.in/yaml.v3"

	"github.com/haasonsaas/nexus/internal/artifacts"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/edge"
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
	// Connection status details (optional)
	Connected bool   `json:"connected,omitempty"`
	Error     string `json:"error,omitempty"`
	LastPing  int64  `json:"last_ping,omitempty"`
	// Health check details (optional)
	Healthy         bool   `json:"healthy,omitempty"`
	HealthMessage   string `json:"health_message,omitempty"`
	HealthLatencyMs int64  `json:"health_latency_ms,omitempty"`
	HealthDegraded  bool   `json:"health_degraded,omitempty"`
}

// ProviderStatus is a detailed provider health snapshot.
type ProviderStatus struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Connected      bool   `json:"connected"`
	Error          string `json:"error,omitempty"`
	LastPing       int64  `json:"last_ping,omitempty"`
	Healthy        bool   `json:"healthy,omitempty"`
	HealthMessage  string `json:"health_message,omitempty"`
	HealthLatency  int64  `json:"health_latency_ms,omitempty"`
	HealthDegraded bool   `json:"health_degraded,omitempty"`
	QRAvailable    bool   `json:"qr_available,omitempty"`
	QRUpdatedAt    string `json:"qr_updated_at,omitempty"`
}

type providerTestRequest struct {
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
}

type providerTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CronJobSummary is a safe representation of a cron job for UI/API.
type CronJobSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Enabled   bool      `json:"enabled"`
	Schedule  string    `json:"schedule"`
	NextRun   time.Time `json:"next_run"`
	LastRun   time.Time `json:"last_run"`
	LastError string    `json:"last_error,omitempty"`
}

// SkillSummary is a UI-friendly skill snapshot.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Path        string `json:"path"`
	Emoji       string `json:"emoji,omitempty"`
	Execution   string `json:"execution,omitempty"`
	Eligible    bool   `json:"eligible"`
	Reason      string `json:"reason,omitempty"`
}

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

// APIArtifactSummary is a compact artifact representation.
type APIArtifactSummary struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	MimeType   string `json:"mime_type"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	Reference  string `json:"reference"`
	TTLSeconds int32  `json:"ttl_seconds"`
	Redacted   bool   `json:"redacted"`
}

type APIArtifactListResponse struct {
	Artifacts []*APIArtifactSummary `json:"artifacts"`
	Total     int                   `json:"total"`
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

// apiSessionPatch handles PATCH/POST /api/sessions/{id}.
func (h *Handler) apiSessionPatch(w http.ResponseWriter, r *http.Request, sessionID string) {
	if h.config.SessionStore == nil {
		h.jsonError(w, "Session store not configured", http.StatusServiceUnavailable)
		return
	}

	var req apiSessionPatchRequest
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.jsonError(w, "Invalid JSON body", http.StatusBadRequest)
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

// apiProviders handles GET /api/providers.
func (h *Handler) apiProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	providers := h.listProviders(r.Context())
	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "providers/list.html", providers)
		return
	}

	h.jsonResponse(w, map[string]any{"providers": providers})
}

// apiProvider handles provider-specific actions (e.g., QR).
func (h *Handler) apiProvider(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/providers/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		h.jsonError(w, "Provider required", http.StatusBadRequest)
		return
	}
	provider := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		for _, p := range h.listProviders(r.Context()) {
			if strings.EqualFold(p.Name, provider) {
				h.jsonResponse(w, p)
				return
			}
		}
		h.jsonError(w, "Provider not found", http.StatusNotFound)
		return
	}

	switch parts[1] {
	case "qr":
		h.apiProviderQR(w, r, provider)
	case "test":
		h.apiProviderTest(w, r, provider)
	default:
		h.jsonError(w, "Not found", http.StatusNotFound)
	}
}

// apiProviderQR renders the latest QR code for a provider if available.
func (h *Handler) apiProviderQR(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ch := models.ChannelType(strings.ToLower(provider))
	code, ok := h.getQRCode(r.Context(), ch)
	if !ok || code == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if strings.EqualFold(r.URL.Query().Get("format"), "text") {
		h.jsonResponse(w, map[string]string{"code": code})
		return
	}

	size := parseIntParam(r, "size", 256)
	if size < 128 {
		size = 128
	}
	if size > 512 {
		size = 512
	}
	png, err := qrcode.Encode(code, qrcode.Medium, size)
	if err != nil {
		h.jsonError(w, "Failed to render QR code", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png) //nolint:errcheck
}

func (h *Handler) apiProviderTest(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.config == nil || h.config.ChannelRegistry == nil {
		h.jsonError(w, "Channel registry not configured", http.StatusServiceUnavailable)
		return
	}

	var req providerTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	channelID := strings.TrimSpace(req.ChannelID)
	if channelID == "" {
		h.jsonError(w, "channel_id is required", http.StatusBadRequest)
		return
	}

	channelType := models.ChannelType(strings.ToLower(provider))
	adapter, ok := h.config.ChannelRegistry.GetOutbound(channelType)
	if !ok {
		h.jsonError(w, "Provider not available", http.StatusNotFound)
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "Nexus test message"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := adapter.Send(ctx, &models.Message{
		Channel:   channelType,
		ChannelID: channelID,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   message,
		Metadata: map[string]any{
			"channel_test": true,
		},
		CreatedAt: time.Now(),
	})
	if err != nil {
		h.jsonResponse(w, providerTestResponse{
			Success: false,
			Message: message,
			Error:   err.Error(),
		})
		return
	}

	h.jsonResponse(w, providerTestResponse{
		Success: true,
		Message: message,
	})
}

// apiCron handles GET /api/cron.
func (h *Handler) apiCron(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobs := h.listCronJobs()
	h.jsonResponse(w, map[string]any{
		"enabled": h.config != nil && h.config.GatewayConfig != nil && h.config.GatewayConfig.Cron.Enabled,
		"jobs":    jobs,
	})
}

// apiSkills handles GET /api/skills.
func (h *Handler) apiSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.jsonResponse(w, map[string]any{"skills": h.listSkills(r.Context())})
}

// apiSkillsRefresh triggers skill discovery.
func (h *Handler) apiSkillsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.SkillsManager == nil {
		h.jsonError(w, "Skills not configured", http.StatusServiceUnavailable)
		return
	}
	go func() {
		_ = h.config.SkillsManager.Discover(context.Background()) //nolint:errcheck
	}()
	h.jsonResponse(w, map[string]string{"status": "refreshing"})
}

// apiNodes handles GET /api/nodes.
func (h *Handler) apiNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.jsonResponse(w, map[string]any{"nodes": h.listNodes()})
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

// apiConfig handles GET/PATCH /api/config.
func (h *Handler) apiConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		configYAML, configPath := h.configSnapshot()
		if strings.EqualFold(r.URL.Query().Get("format"), "yaml") {
			w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(configYAML)) //nolint:errcheck
			return
		}
		h.jsonResponse(w, map[string]string{
			"path":   configPath,
			"config": configYAML,
		})
	case http.MethodPatch, http.MethodPost:
		h.apiConfigPatch(w, r)
	default:
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// apiConfigSchema handles GET /api/config/schema.
func (h *Handler) apiConfigSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var schema []byte
	var err error
	if h != nil && h.config != nil && h.config.ConfigManager != nil {
		schema, err = h.config.ConfigManager.ConfigSchema(r.Context())
	} else {
		schema, err = config.JSONSchema()
	}
	if err != nil {
		h.jsonError(w, "Failed to build config schema", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(schema) //nolint:errcheck
}

// apiArtifacts handles GET /api/artifacts.
func (h *Handler) apiArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.ArtifactRepo == nil {
		h.jsonError(w, "Artifacts not configured", http.StatusServiceUnavailable)
		return
	}

	filter := artifacts.Filter{
		SessionID: r.URL.Query().Get("session_id"),
		EdgeID:    r.URL.Query().Get("edge_id"),
		Type:      r.URL.Query().Get("type"),
		Limit:     parseIntParam(r, "limit", 50),
	}

	results, err := h.config.ArtifactRepo.ListArtifacts(r.Context(), filter)
	if err != nil {
		h.jsonError(w, "Failed to list artifacts", http.StatusInternalServerError)
		return
	}

	items := make([]*APIArtifactSummary, 0, len(results))
	for _, art := range results {
		if art == nil {
			continue
		}
		items = append(items, &APIArtifactSummary{
			ID:         art.Id,
			Type:       art.Type,
			MimeType:   art.MimeType,
			Filename:   art.Filename,
			Size:       art.Size,
			Reference:  art.Reference,
			TTLSeconds: art.TtlSeconds,
			Redacted:   strings.HasPrefix(art.Reference, "redacted://"),
		})
	}

	h.jsonResponse(w, APIArtifactListResponse{
		Artifacts: items,
		Total:     len(items),
	})
}

// apiArtifact handles GET /api/artifacts/{id}.
func (h *Handler) apiArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.ArtifactRepo == nil {
		h.jsonError(w, "Artifacts not configured", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/artifacts/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		h.jsonError(w, "Artifact ID required", http.StatusBadRequest)
		return
	}
	artifactID := parts[0]

	artifact, reader, err := h.config.ArtifactRepo.GetArtifact(r.Context(), artifactID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "expired") {
			h.jsonError(w, "Artifact not found", http.StatusNotFound)
		} else {
			h.config.Logger.Error("failed to get artifact", "id", artifactID, "error", err)
			h.jsonError(w, "Failed to retrieve artifact", http.StatusInternalServerError)
		}
		return
	}
	defer reader.Close()

	raw := strings.EqualFold(r.URL.Query().Get("raw"), "1") || strings.EqualFold(r.URL.Query().Get("raw"), "true")
	download := strings.EqualFold(r.URL.Query().Get("download"), "1") || strings.EqualFold(r.URL.Query().Get("download"), "true")

	if raw {
		if strings.HasPrefix(artifact.Reference, "redacted://") {
			http.Error(w, "Artifact redacted", http.StatusGone)
			return
		}
		contentType := artifact.MimeType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		if download && artifact.Filename != "" {
			safeName := sanitizeAttachmentFilename(artifact.Filename)
			if safeName != "" {
				w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
					"filename": safeName,
				}))
			}
		}
		if _, err := io.Copy(w, reader); err != nil {
			h.config.Logger.Error("artifact download failed", "error", err)
		}
		return
	}

	h.jsonResponse(w, APIArtifactSummary{
		ID:         artifact.Id,
		Type:       artifact.Type,
		MimeType:   artifact.MimeType,
		Filename:   artifact.Filename,
		Size:       artifact.Size,
		Reference:  artifact.Reference,
		TTLSeconds: artifact.TtlSeconds,
		Redacted:   strings.HasPrefix(artifact.Reference, "redacted://"),
	})
}

func (h *Handler) apiNodeTools(w http.ResponseWriter, r *http.Request, nodeID string, rest []string) {
	if h.config.EdgeManager == nil {
		h.jsonError(w, "Edge manager not configured", http.StatusServiceUnavailable)
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
		h.jsonResponse(w, map[string]any{"tools": summaries})
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
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			h.jsonError(w, "Invalid JSON body", http.StatusBadRequest)
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
	h.jsonResponse(w, map[string]any{
		"content":       result.Content,
		"is_error":      result.IsError,
		"duration_ms":   result.DurationMs,
		"error_details": result.ErrorDetails,
		"artifacts":     result.Artifacts,
	})
}

func (h *Handler) apiConfigPatch(w http.ResponseWriter, r *http.Request) {
	if h.config == nil || strings.TrimSpace(h.config.ConfigPath) == "" {
		h.jsonError(w, "Config path not available", http.StatusServiceUnavailable)
		return
	}
	applyRequested := strings.EqualFold(r.URL.Query().Get("apply"), "true") || strings.EqualFold(r.URL.Query().Get("apply"), "1")
	baseHash := strings.TrimSpace(r.URL.Query().Get("base_hash"))
	rawContent := ""

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			h.jsonError(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
		if apply, ok := payload["apply"].(bool); ok && apply {
			applyRequested = true
		}
		if hash, ok := payload["base_hash"].(string); ok && strings.TrimSpace(hash) != "" {
			baseHash = strings.TrimSpace(hash)
		}
		if rawPayload, ok := payload["raw"].(string); ok && strings.TrimSpace(rawPayload) != "" {
			rawContent = rawPayload
		}

		if rawContent == "" {
			raw, err := doctor.LoadRawConfig(h.config.ConfigPath)
			if err != nil {
				h.jsonError(w, "Failed to read config", http.StatusInternalServerError)
				return
			}
			if path, ok := payload["path"].(string); ok && strings.TrimSpace(path) != "" {
				setPathValue(raw, path, payload["value"])
			} else {
				delete(payload, "path")
				delete(payload, "value")
				delete(payload, "apply")
				delete(payload, "base_hash")
				delete(payload, "raw")
				mergeMaps(raw, payload)
			}
			if err := doctor.WriteRawConfig(h.config.ConfigPath, raw); err != nil {
				h.jsonError(w, "Failed to write config", http.StatusInternalServerError)
				return
			}
		} else if err := writeRawConfigFile(h.config.ConfigPath, rawContent); err != nil {
			h.jsonError(w, "Failed to write config", http.StatusInternalServerError)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			h.jsonError(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		if strings.EqualFold(r.FormValue("apply"), "true") || strings.EqualFold(r.FormValue("apply"), "1") {
			applyRequested = true
		}
		if hash := strings.TrimSpace(r.FormValue("base_hash")); hash != "" {
			baseHash = hash
		}
		path := strings.TrimSpace(r.FormValue("path"))
		value := strings.TrimSpace(r.FormValue("value"))
		if path == "" {
			h.jsonError(w, "path is required", http.StatusBadRequest)
			return
		}
		raw, err := doctor.LoadRawConfig(h.config.ConfigPath)
		if err != nil {
			h.jsonError(w, "Failed to read config", http.StatusInternalServerError)
			return
		}
		var decoded any
		if value != "" {
			if err := json.Unmarshal([]byte(value), &decoded); err == nil {
				setPathValue(raw, path, decoded)
			} else {
				setPathValue(raw, path, value)
			}
		} else {
			setPathValue(raw, path, value)
		}
		if err := doctor.WriteRawConfig(h.config.ConfigPath, raw); err != nil {
			h.jsonError(w, "Failed to write config", http.StatusInternalServerError)
			return
		}
	}

	var applyResult any
	if applyRequested {
		if h.config.ConfigManager == nil {
			h.jsonError(w, "Config apply not available", http.StatusServiceUnavailable)
			return
		}
		if rawContent == "" {
			if data, err := os.ReadFile(h.config.ConfigPath); err == nil {
				rawContent = string(data)
			}
		}
		result, err := h.config.ConfigManager.ApplyConfig(r.Context(), rawContent, baseHash)
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		applyResult = result
	}

	configYAML, configPath := h.configSnapshot()
	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "config/raw.html", map[string]string{
			"ConfigYAML": configYAML,
			"ConfigPath": configPath,
		})
		return
	}
	response := map[string]any{
		"path":   configPath,
		"config": configYAML,
	}
	if applyResult != nil {
		response["apply"] = applyResult
	}
	h.jsonResponse(w, response)
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

	// Channel status
	if h.config != nil && h.config.ChannelRegistry != nil {
		adapters := h.config.ChannelRegistry.All()
		sort.Slice(adapters, func(i, j int) bool {
			return string(adapters[i].Type()) < string(adapters[j].Type())
		})
		for _, adapter := range adapters {
			channelType := adapter.Type()
			entry := ChannelStatus{
				Name:    string(channelType),
				Type:    string(channelType),
				Enabled: channelEnabled(h.config.GatewayConfig, channelType),
			}
			if healthAdapter, ok := adapter.(channels.HealthAdapter); ok {
				chStatus := healthAdapter.Status()
				entry.Connected = chStatus.Connected
				entry.Error = chStatus.Error
				entry.LastPing = chStatus.LastPing
				switch {
				case chStatus.Connected:
					entry.Status = "connected"
				case chStatus.Error != "":
					entry.Status = "error"
				default:
					entry.Status = "disconnected"
				}
				healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				health := healthAdapter.HealthCheck(healthCtx)
				cancel()
				entry.Healthy = health.Healthy
				entry.HealthMessage = health.Message
				entry.HealthLatencyMs = health.Latency.Milliseconds()
				entry.HealthDegraded = health.Degraded
			}
			status.Channels = append(status.Channels, entry)
		}
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

func (h *Handler) listProviders(ctx context.Context) []*ProviderStatus {
	if h == nil || h.config == nil || h.config.ChannelRegistry == nil {
		return nil
	}
	adapters := h.config.ChannelRegistry.All()
	sort.Slice(adapters, func(i, j int) bool {
		return string(adapters[i].Type()) < string(adapters[j].Type())
	})

	results := make([]*ProviderStatus, 0, len(adapters))
	for _, adapter := range adapters {
		channelType := adapter.Type()
		entry := &ProviderStatus{
			Name:    string(channelType),
			Enabled: channelEnabled(h.config.GatewayConfig, channelType),
		}
		if healthAdapter, ok := adapter.(channels.HealthAdapter); ok {
			st := healthAdapter.Status()
			entry.Connected = st.Connected
			entry.Error = st.Error
			entry.LastPing = st.LastPing
			healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			health := healthAdapter.HealthCheck(healthCtx)
			cancel()
			entry.Healthy = health.Healthy
			entry.HealthMessage = health.Message
			entry.HealthLatency = health.Latency.Milliseconds()
			entry.HealthDegraded = health.Degraded
		}
		if _, ok := adapter.(channels.QRAdapter); ok {
			entry.QRAvailable = h.hasQRCode(channelType)
			if entry.QRAvailable {
				entry.QRUpdatedAt = h.qrUpdatedAt(channelType)
			}
		}
		results = append(results, entry)
	}

	return results
}

func (h *Handler) listCronJobs() []*CronJobSummary {
	if h == nil || h.config == nil || h.config.CronScheduler == nil {
		return nil
	}
	jobs := h.config.CronScheduler.Jobs()
	out := make([]*CronJobSummary, 0, len(jobs))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		out = append(out, &CronJobSummary{
			ID:        job.ID,
			Name:      job.Name,
			Type:      string(job.Type),
			Enabled:   job.Enabled,
			Schedule:  formatSchedule(job.Schedule),
			NextRun:   job.NextRun,
			LastRun:   job.LastRun,
			LastError: job.LastError,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (h *Handler) listSkills(ctx context.Context) []*SkillSummary {
	if h == nil || h.config == nil || h.config.SkillsManager == nil {
		return nil
	}
	entries := h.config.SkillsManager.ListAll()
	out := make([]*SkillSummary, 0, len(entries))
	for _, skill := range entries {
		if skill == nil {
			continue
		}
		eligible := false
		reason := ""
		if _, ok := h.config.SkillsManager.GetEligible(skill.Name); ok {
			eligible = true
		} else if result, err := h.config.SkillsManager.CheckEligibility(skill.Name); err == nil {
			reason = result.Reason
		}
		emoji := ""
		execution := ""
		if skill.Metadata != nil {
			emoji = skill.Metadata.Emoji
			if skill.Metadata.Execution != "" {
				execution = string(skill.Metadata.Execution)
			}
		}
		out = append(out, &SkillSummary{
			Name:        skill.Name,
			Description: skill.Description,
			Source:      string(skill.Source),
			Path:        skill.Path,
			Emoji:       emoji,
			Execution:   execution,
			Eligible:    eligible,
			Reason:      reason,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
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

func (h *Handler) configSnapshot() (string, string) {
	configPath := ""
	if h != nil && h.config != nil {
		configPath = h.config.ConfigPath
	}

	var raw map[string]any
	if configPath != "" {
		if loaded, err := doctor.LoadRawConfig(configPath); err == nil {
			raw = loaded
		}
	}
	if raw == nil && h != nil && h.config != nil && h.config.GatewayConfig != nil {
		raw = configToMap(h.config.GatewayConfig)
	}
	if raw == nil {
		return "", configPath
	}

	redacted := redactConfigMap(raw)
	payload, err := yaml.Marshal(redacted)
	if err != nil {
		return "", configPath
	}
	return string(payload), configPath
}

func writeRawConfigFile(path string, raw string) error {
	data := []byte(raw)
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

func (h *Handler) getQRCode(ctx context.Context, channelType models.ChannelType) (string, bool) {
	if code := h.cachedQRCode(channelType); code != "" {
		return code, true
	}
	if h == nil || h.config == nil || h.config.ChannelRegistry == nil {
		return "", false
	}
	adapter, ok := h.config.ChannelRegistry.Get(channelType)
	if !ok {
		return "", false
	}
	qrAdapter, ok := adapter.(channels.QRAdapter)
	if !ok {
		return "", false
	}

	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	select {
	case <-ctx.Done():
		return "", false
	case <-timeout.C:
		return "", false
	case code, ok := <-qrAdapter.QRChannel():
		if !ok || code == "" {
			return "", false
		}
		h.cacheQRCode(channelType, code)
		return code, true
	}
}

func (h *Handler) cacheQRCode(channelType models.ChannelType, code string) {
	h.qrMu.Lock()
	h.qrCodes[channelType] = code
	h.qrUpdated[channelType] = time.Now()
	h.qrMu.Unlock()
}

func (h *Handler) hasQRCode(channelType models.ChannelType) bool {
	h.qrMu.RLock()
	defer h.qrMu.RUnlock()
	code := h.qrCodes[channelType]
	return strings.TrimSpace(code) != ""
}

func (h *Handler) cachedQRCode(channelType models.ChannelType) string {
	h.qrMu.RLock()
	defer h.qrMu.RUnlock()
	return h.qrCodes[channelType]
}

func (h *Handler) qrUpdatedAt(channelType models.ChannelType) string {
	h.qrMu.RLock()
	defer h.qrMu.RUnlock()
	if ts, ok := h.qrUpdated[channelType]; ok && !ts.IsZero() {
		return ts.Format(time.RFC3339)
	}
	return ""
}

func channelEnabled(cfg *config.Config, channel models.ChannelType) bool {
	if cfg == nil {
		return true
	}
	switch channel {
	case models.ChannelTelegram:
		return cfg.Channels.Telegram.Enabled
	case models.ChannelDiscord:
		return cfg.Channels.Discord.Enabled
	case models.ChannelSlack:
		return cfg.Channels.Slack.Enabled
	case models.ChannelWhatsApp:
		return cfg.Channels.WhatsApp.Enabled
	case models.ChannelSignal:
		return cfg.Channels.Signal.Enabled
	case models.ChannelIMessage:
		return cfg.Channels.IMessage.Enabled
	case models.ChannelMatrix:
		return cfg.Channels.Matrix.Enabled
	case models.ChannelTeams:
		return cfg.Channels.Teams.Enabled
	case models.ChannelEmail:
		return cfg.Channels.Email.Enabled
	default:
		return true
	}
}

func formatSchedule(schedule cron.Schedule) string {
	switch schedule.Kind {
	case "cron":
		return fmt.Sprintf("cron: %s", schedule.CronExpr)
	case "every":
		if schedule.Timezone != "" {
			return fmt.Sprintf("every %s (%s)", schedule.Every, schedule.Timezone)
		}
		return fmt.Sprintf("every %s", schedule.Every)
	case "at":
		if schedule.Timezone != "" {
			return fmt.Sprintf("at %s (%s)", schedule.At.Format(time.RFC3339), schedule.Timezone)
		}
		return fmt.Sprintf("at %s", schedule.At.Format(time.RFC3339))
	default:
		return schedule.Kind
	}
}

func configToMap(cfg *config.Config) map[string]any {
	if cfg == nil {
		return nil
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
}

func redactConfigMap(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		if isSensitiveKey(key) {
			out[key] = "***"
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			out[key] = redactConfigMap(typed)
		case []any:
			out[key] = redactConfigSlice(typed)
		default:
			out[key] = value
		}
	}
	return out
}

func redactConfigSlice(values []any) []any {
	out := make([]any, len(values))
	for i, value := range values {
		switch typed := value.(type) {
		case map[string]any:
			out[i] = redactConfigMap(typed)
		case []any:
			out[i] = redactConfigSlice(typed)
		default:
			out[i] = value
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range []string{
		"token",
		"secret",
		"api_key",
		"apikey",
		"password",
		"jwt",
		"signing",
		"client_secret",
		"private",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func mergeMaps(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if existing, ok := dst[key]; ok {
			existingMap, okExisting := existing.(map[string]any)
			valueMap, okValue := value.(map[string]any)
			if okExisting && okValue {
				mergeMaps(existingMap, valueMap)
				dst[key] = existingMap
				continue
			}
		}
		dst[key] = value
	}
}

func setPathValue(raw map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := raw
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
}

func sanitizeAttachmentFilename(name string) string {
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\\", "")
	return strings.TrimSpace(name)
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
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		h.config.Logger.Error("json encode error", "error", err)
	}
}

// userFromContext extracts the user from context if available.
func userFromContext(ctx context.Context) *models.User {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil
	}
	return user
}
