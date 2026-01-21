package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Server is the main Nexus gateway server.
type Server struct {
	config   *config.Config
	grpc     *grpc.Server
	channels *channels.Registry
	logger   *slog.Logger
}

// NewServer creates a new gateway server.
func NewServer(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	// Create gRPC server with interceptors
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingInterceptor(logger),
			// TODO: Add auth interceptor
		),
		grpc.ChainStreamInterceptor(
			streamLoggingInterceptor(logger),
			// TODO: Add stream auth interceptor
		),
	)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("nexus", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable reflection for development
	reflection.Register(grpcServer)

	return &Server{
		config:   cfg,
		grpc:     grpcServer,
		channels: channels.NewRegistry(),
		logger:   logger,
	}, nil
}

// Start begins serving requests.
func (s *Server) Start(ctx context.Context) error {
	// Start channel adapters
	if err := s.channels.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start channels: %w", err)
	}

	// Start message processing
	go s.processMessages(ctx)

	// Start gRPC server
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.logger.Info("starting gRPC server", "addr", addr)
	return s.grpc.Serve(lis)
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("stopping server")

	// Stop accepting new connections
	s.grpc.GracefulStop()

	// Stop channel adapters
	if err := s.channels.StopAll(ctx); err != nil {
		s.logger.Error("error stopping channels", "error", err)
	}

	return nil
}

// processMessages handles incoming messages from all channels.
func (s *Server) processMessages(ctx context.Context) {
	messages := s.channels.AggregateMessages(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-messages:
			go s.handleMessage(ctx, msg)
		}
	}
}

// handleMessage processes a single incoming message.
func (s *Server) handleMessage(ctx context.Context, msg *models.Message) {
	s.logger.Debug("received message",
		"channel", msg.Channel,
		"content_length", len(msg.Content),
	)

	// TODO: Implement full message handling:
	// 1. Route to session
	// 2. Run agent
	// 3. Send response
}

// loggingInterceptor logs unary RPC calls.
func loggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		logger.Debug("rpc call", "method", info.FullMethod)
		resp, err := handler(ctx, req)
		if err != nil {
			logger.Error("rpc error", "method", info.FullMethod, "error", err)
		}
		return resp, err
	}
}

// streamLoggingInterceptor logs streaming RPC calls.
func streamLoggingInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		logger.Debug("stream started", "method", info.FullMethod)
		err := handler(srv, ss)
		if err != nil {
			logger.Error("stream error", "method", info.FullMethod, "error", err)
		}
		logger.Debug("stream ended", "method", info.FullMethod)
		return err
	}
}
