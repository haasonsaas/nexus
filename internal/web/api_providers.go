package web

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

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

	h.jsonResponse(w, apiProvidersResponse{Providers: providers})
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
		h.jsonError(w, "Channel registry not configured (gateway channels unavailable)", http.StatusServiceUnavailable)
		return
	}

	var req providerTestRequest
	status, err := decodeJSONRequest(w, r, &req)
	if err != nil {
		msg := "Invalid request body"
		if status == http.StatusRequestEntityTooLarge {
			msg = "Request entity too large"
		}
		h.jsonError(w, msg, status)
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

	sendErr := adapter.Send(ctx, &models.Message{
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
	if sendErr != nil {
		h.jsonResponse(w, providerTestResponse{
			Success: false,
			Message: message,
			Error:   sendErr.Error(),
		})
		return
	}

	h.jsonResponse(w, providerTestResponse{
		Success: true,
		Message: message,
	})
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
