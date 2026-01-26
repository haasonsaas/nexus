package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/haasonsaas/nexus/internal/commands"
	"github.com/haasonsaas/nexus/internal/web"
)

func (s *Server) startHTTPServer(ctx context.Context) error {
	if s == nil || s.config == nil || s.config.Server.HTTPPort == 0 {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.HTTPPort)
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", s.handleHealthz)
	if s.webhookHooks != nil {
		basePath := s.webhookHooks.Config().BasePath
		if basePath == "" {
			basePath = "/hooks"
		}
		mux.Handle(basePath, s.webhookHooks)
		mux.Handle(basePath+"/", s.webhookHooks)
	}

	if s.config.Channels.HomeAssistant.Enabled {
		var haHandler http.Handler = http.HandlerFunc(s.handleHomeAssistantConversation)
		haHandler = web.AuthMiddleware(s.authService, s.logger)(haHandler)
		mux.Handle("/api/v1/ha/conversation", haHandler)
	}

	mux.Handle("/ws", s.newWSControlPlane())

	webHandler, err := web.NewHandler(&web.Config{
		BasePath:            "/ui",
		AuthService:         s.authService,
		SessionStore:        s.sessions,
		ArtifactRepo:        s.artifactRepo,
		ChannelRegistry:     s.channels,
		CronScheduler:       s.cronScheduler,
		SkillsManager:       s.skillsManager,
		EdgeManager:         s.edgeManager,
		ToolSummaryProvider: s.toolManager,
		GatewayConfig:       s.config,
		EventStore:          s.eventStore,
		UsageCache:          s.integration.UsageCache(),
		ConfigManager:       s,
		ConfigPath:          s.configPath,
		DefaultAgentID:      s.config.Session.DefaultAgentID,
		Logger:              s.logger,
		ServerStartTime:     s.startTime,
	})
	if err != nil {
		return fmt.Errorf("web handler: %w", err)
	}
	mux.Handle("/", webHandler.Mount())

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("http listen: %w", err)
	}

	s.httpServer = server
	s.httpListener = listener

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if s.logger != nil {
				s.logger.Error("http server error", "error", err)
			}
		}
	}()

	if s.logger != nil {
		s.logger.Info("starting http server", "addr", addr)
	}

	return nil
}

func (s *Server) stopHTTPServer(ctx context.Context) {
	if s == nil || s.httpServer == nil {
		return
	}
	shutdownCtx := ctx
	var cancel context.CancelFunc
	if shutdownCtx == nil {
		shutdownCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil && s.logger != nil {
		s.logger.Warn("http server shutdown error", "error", err)
	}
	s.httpServer = nil
	s.httpListener = nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Use integration health checker if available
	if s.integration != nil {
		// Quick health check without probing
		probeChannels := r.URL.Query().Get("probe") == "true"
		summary, err := s.integration.CheckHealth(r.Context(), &commands.HealthCheckOptions{
			ProbeChannels: &probeChannels,
		})
		if err != nil {
			payload := map[string]any{
				"status": "error",
				"error":  err.Error(),
			}
			data, marshalErr := json.Marshal(payload)
			if marshalErr != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			if _, writeErr := w.Write(data); writeErr != nil && s.logger != nil {
				s.logger.Debug("healthz write failed", "error", writeErr)
			}
			return
		}

		// Build response
		status := "ok"
		statusCode := http.StatusOK
		if !summary.OK {
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		}

		response := map[string]any{
			"status":      status,
			"ts":          summary.Ts,
			"duration_ms": summary.DurationMs,
		}
		if s.nodeID != "" {
			response["node_id"] = s.nodeID
		}

		// Include activity stats
		activityStats := s.integration.GetActivityStats()
		response["activity"] = map[string]any{
			"channels":        activityStats.TotalChannels,
			"recent_inbound":  activityStats.RecentInbound,
			"recent_outbound": activityStats.RecentOutbound,
		}

		// Include migration status
		current, latest, pending, err := s.integration.GetMigrationStatus()
		if err == nil {
			response["migrations"] = map[string]any{
				"current": current,
				"latest":  latest,
				"pending": pending,
			}
		}

		data, err := json.Marshal(response)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("healthz marshal failed", "error", err)
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(statusCode)
		if _, err := w.Write(data); err != nil && s.logger != nil {
			s.logger.Debug("healthz write failed", "error", err)
		}
		return
	}

	// Fallback to simple health check
	w.WriteHeader(http.StatusOK)
	response := map[string]any{"status": "ok"}
	if s.nodeID != "" {
		response["node_id"] = s.nodeID
	}
	data, err := json.Marshal(response)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("healthz marshal failed", "error", err)
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(data); err != nil && s.logger != nil {
		s.logger.Debug("healthz write failed", "error", err)
	}
}
