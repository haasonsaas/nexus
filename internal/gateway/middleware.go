// Package gateway provides the main Nexus gateway server.
//
// middleware.go contains gRPC interceptors for logging and authentication.
package gateway

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
)

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
