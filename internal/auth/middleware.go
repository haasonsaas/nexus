package auth

import (
	"context"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryInterceptor enforces JWT/API key auth for unary calls.
func UnaryInterceptor(service *Service, logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if service == nil || !service.Enabled() {
			return handler(ctx, req)
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		if token := extractBearer(md); token != "" {
			user, err := service.ValidateJWT(token)
			if err != nil {
				if logger != nil {
					logger.Warn("jwt validation failed", "error", err)
				}
				return nil, status.Error(codes.Unauthenticated, "invalid token")
			}
			ctx = WithUser(ctx, user)
			return handler(ctx, req)
		}

		if apiKey := extractAPIKey(md); apiKey != "" {
			user, err := service.ValidateAPIKey(apiKey)
			if err != nil {
				if logger != nil {
					logger.Warn("api key validation failed", "error", err)
				}
				return nil, status.Error(codes.Unauthenticated, "invalid api key")
			}
			ctx = WithUser(ctx, user)
			return handler(ctx, req)
		}

		return nil, status.Error(codes.Unauthenticated, "missing credentials")
	}
}

// StreamInterceptor enforces JWT/API key auth for streaming calls.
func StreamInterceptor(service *Service, logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if service == nil || !service.Enabled() {
			return handler(srv, stream)
		}
		md, ok := metadata.FromIncomingContext(stream.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		if token := extractBearer(md); token != "" {
			user, err := service.ValidateJWT(token)
			if err != nil {
				if logger != nil {
					logger.Warn("jwt validation failed", "error", err)
				}
				return status.Error(codes.Unauthenticated, "invalid token")
			}
			return handler(srv, &wrappedStream{ServerStream: stream, ctx: WithUser(stream.Context(), user)})
		}

		if apiKey := extractAPIKey(md); apiKey != "" {
			user, err := service.ValidateAPIKey(apiKey)
			if err != nil {
				if logger != nil {
					logger.Warn("api key validation failed", "error", err)
				}
				return status.Error(codes.Unauthenticated, "invalid api key")
			}
			return handler(srv, &wrappedStream{ServerStream: stream, ctx: WithUser(stream.Context(), user)})
		}

		return status.Error(codes.Unauthenticated, "missing credentials")
	}
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}

func extractBearer(md metadata.MD) string {
	for _, value := range md.Get("authorization") {
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "bearer ") {
			return strings.TrimSpace(value[len("bearer "):])
		}
	}
	return ""
}

func extractAPIKey(md metadata.MD) string {
	for _, key := range []string{"x-api-key", "api-key"} {
		for _, value := range md.Get(key) {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
