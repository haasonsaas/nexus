package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"sort"
	"time"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/infra"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

var maxAPIRequestBodyBytes int64 = 10 * 1024 * 1024

// maxQueryParamLen limits the length of individual query parameters to prevent abuse.
const maxQueryParamLen = 512

// clampQueryParam returns the query parameter value truncated to maxQueryParamLen.
func clampQueryParam(r *http.Request, key string) string {
	v := r.URL.Query().Get(key)
	if len(v) > maxQueryParamLen {
		return v[:maxQueryParamLen]
	}
	return v
}

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, dst any) (int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIRequestBodyBytes)
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return http.StatusRequestEntityTooLarge, err
		}
		return http.StatusBadRequest, err
	}

	return 0, nil
}

// SystemStatus holds system health information.
type SystemStatus struct {
	Uptime         time.Duration       `json:"uptime"`
	UptimeString   string              `json:"uptime_string"`
	GoVersion      string              `json:"go_version"`
	NumGoroutines  int                 `json:"num_goroutines"`
	MemAllocMB     float64             `json:"mem_alloc_mb"`
	MemSysMB       float64             `json:"mem_sys_mb"`
	NumCPU         int                 `json:"num_cpu"`
	SessionCount   int                 `json:"session_count"`
	DatabaseStatus string              `json:"database_status"`
	Channels       []ChannelStatus     `json:"channels"`
	HealthChecks   *infra.HealthReport `json:"health_checks,omitempty"`
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

	if len(infra.DefaultHealthRegistry.Names()) > 0 {
		report := infra.CheckHealth(ctx)
		status.HealthChecks = &report
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
