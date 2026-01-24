package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

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
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
}
