// Package web provides the HTTP dashboard UI for Nexus.
package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/artifacts"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/controlplane"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/haasonsaas/nexus/pkg/models"
)

//go:embed templates/*.html templates/**/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Config holds web UI configuration.
type Config struct {
	// BasePath is the URL prefix for the UI (default: /ui)
	BasePath string
	// AuthService for validating requests (optional)
	AuthService *auth.Service
	// SessionStore for accessing session data
	SessionStore sessions.Store
	// ArtifactRepo for accessing stored artifacts (optional)
	ArtifactRepo artifacts.Repository
	// ChannelRegistry for provider status and QR login
	ChannelRegistry *channels.Registry
	// CronScheduler for listing cron jobs
	CronScheduler *cron.Scheduler
	// SkillsManager for listing and refreshing skills
	SkillsManager *skills.Manager
	// EdgeManager for listing connected nodes/tools
	EdgeManager *edge.Manager
	// EventStore for usage and observability data
	EventStore observability.EventStore
	// ToolSummaryProvider supplies core + MCP tool metadata (optional)
	ToolSummaryProvider ToolSummaryProvider
	// GatewayConfig is the active runtime configuration (for summary views)
	GatewayConfig *config.Config
	// ConfigManager exposes config control plane operations (optional)
	ConfigManager controlplane.ConfigManager
	// ConfigPath is the path to the loaded config file (optional)
	ConfigPath string
	// DefaultAgentID is the agent ID used for listing sessions
	DefaultAgentID string
	// Logger for request logging
	Logger *slog.Logger
	// ServerStartTime for uptime calculation
	ServerStartTime time.Time
}

// ToolSummaryProvider exposes tool metadata for UI display.
type ToolSummaryProvider interface {
	ToolSummaries() []models.ToolSummary
}

// Handler is the main web UI HTTP handler.
type Handler struct {
	config    *Config
	templates *template.Template
	mux       *http.ServeMux

	qrMu      sync.RWMutex
	qrCodes   map[models.ChannelType]string
	qrUpdated map[models.ChannelType]time.Time
}

// NewHandler creates a new web UI handler.
func NewHandler(cfg *Config) (*Handler, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "/ui"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DefaultAgentID == "" {
		cfg.DefaultAgentID = "main"
	}

	// Parse templates with custom functions
	funcMap := template.FuncMap{
		"formatTime":     formatTime,
		"formatDuration": formatDuration,
		"truncate":       truncate,
		"channelIcon":    channelIcon,
		"roleClass":      roleClass,
		"hasPrefix":      strings.HasPrefix,
		"lower":          strings.ToLower,
		"upper":          strings.ToUpper,
		"prettyJSON":     prettyJSON,
		"add":            func(a, b int) int { return a + b },
		"sub":            func(a, b int) int { return a - b },
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html", "templates/**/*.html")
	if err != nil {
		return nil, err
	}

	h := &Handler{
		config:    cfg,
		templates: tmpl,
		mux:       http.NewServeMux(),
		qrCodes:   make(map[models.ChannelType]string),
		qrUpdated: make(map[models.ChannelType]time.Time),
	}

	h.setupRoutes()
	return h, nil
}

// setupRoutes configures all HTTP routes.
func (h *Handler) setupRoutes() {
	// Static files
	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		h.mux.Handle("/static/", http.NotFoundHandler())
	} else {
		h.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))
	}

	// Page routes
	h.mux.HandleFunc("/", h.handleIndex)
	h.mux.HandleFunc("/sessions", h.handleSessionList)
	h.mux.HandleFunc("/sessions/", h.handleSessionDetail)
	h.mux.HandleFunc("/analytics", h.handleAnalytics)
	h.mux.HandleFunc("/status", h.handleStatusDashboard)
	h.mux.HandleFunc("/providers", h.handleProviders)
	h.mux.HandleFunc("/cron", h.handleCron)
	h.mux.HandleFunc("/skills", h.handleSkills)
	h.mux.HandleFunc("/tools", h.handleTools)
	h.mux.HandleFunc("/nodes", h.handleNodes)
	h.mux.HandleFunc("/config", h.handleConfig)
	h.mux.HandleFunc("/webchat", h.handleWebChat)

	// API routes for htmx
	h.mux.HandleFunc("/api/sessions", h.apiSessionList)
	h.mux.HandleFunc("/api/sessions/", h.apiSession)
	h.mux.HandleFunc("/api/status", h.apiStatus)
	h.mux.HandleFunc("/api/providers", h.apiProviders)
	h.mux.HandleFunc("/api/providers/", h.apiProvider)
	h.mux.HandleFunc("/api/cron", h.apiCron)
	h.mux.HandleFunc("/api/skills", h.apiSkills)
	h.mux.HandleFunc("/api/skills/refresh", h.apiSkillsRefresh)
	h.mux.HandleFunc("/api/tools", h.apiTools)
	h.mux.HandleFunc("/api/usage/costs", h.apiUsageCosts)
	h.mux.HandleFunc("/api/nodes", h.apiNodes)
	h.mux.HandleFunc("/api/nodes/", h.apiNode)
	h.mux.HandleFunc("/api/config", h.apiConfig)
	h.mux.HandleFunc("/api/config/schema", h.apiConfigSchema)
	h.mux.HandleFunc("/api/artifacts", h.apiArtifacts)
	h.mux.HandleFunc("/api/artifacts/", h.apiArtifact)

	// Versioned API routes (JSON + optional htmx partials)
	h.mux.HandleFunc("/api/v1/analytics/overview", h.apiAnalyticsOverview)
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip base path prefix
	path := r.URL.Path
	if h.config.BasePath != "" && h.config.BasePath != "/" {
		path = strings.TrimPrefix(path, h.config.BasePath)
		if path == "" {
			path = "/"
		}
	}
	r.URL.Path = path

	h.mux.ServeHTTP(w, r)
}

// Mount returns the handler with middleware applied.
func (h *Handler) Mount() http.Handler {
	var handler http.Handler = h

	// Apply auth middleware if configured
	if h.config.AuthService != nil && h.config.AuthService.Enabled() {
		handler = AuthMiddleware(h.config.AuthService, h.config.Logger)(handler)
	}

	// Apply logging middleware
	handler = LoggingMiddleware(h.config.Logger)(handler)

	return handler
}

// Template helper functions

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	hours := int(d.Hours())
	if hours < 24 {
		return strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
	}
	days := hours / 24
	remainingHours := hours % 24
	if days == 1 && remainingHours == 0 {
		return "1 day"
	}
	if days == 1 {
		return "1 day " + (time.Duration(remainingHours) * time.Hour).String()
	}
	if remainingHours == 0 {
		if days == 1 {
			return "1 day"
		}
		return strings.Replace(strings.TrimSuffix((time.Duration(days)*24*time.Hour).String(), "0m0s"), "h", " days", 1)
	}
	return d.Round(time.Hour).String()
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func channelIcon(ch models.ChannelType) string {
	switch ch {
	case models.ChannelTelegram:
		return "telegram"
	case models.ChannelSlack:
		return "slack"
	case models.ChannelDiscord:
		return "discord"
	case models.ChannelWhatsApp:
		return "whatsapp"
	case models.ChannelAPI:
		return "api"
	default:
		return "chat"
	}
}

func roleClass(role models.Role) string {
	switch role {
	case models.RoleUser:
		return "message-user"
	case models.RoleAssistant:
		return "message-assistant"
	case models.RoleSystem:
		return "message-system"
	case models.RoleTool:
		return "message-tool"
	default:
		return ""
	}
}
