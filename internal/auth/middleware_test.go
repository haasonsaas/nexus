package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestUnaryInterceptorAllowsWhenDisabled(t *testing.T) {
	service := NewService(Config{})
	interceptor := UnaryInterceptor(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handlerCalled := false

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}
}

func TestUnaryInterceptorRejectsMissingCredentials(t *testing.T) {
	service := NewService(Config{JWTSecret: "secret"})
	interceptor := UnaryInterceptor(service, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	})

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
}

func TestUnaryInterceptorAcceptsValidToken(t *testing.T) {
	service := NewService(Config{JWTSecret: "secret", TokenExpiry: time.Hour})
	token, err := service.GenerateJWT(&models.User{ID: "user-1"})
	if err != nil {
		t.Fatalf("GenerateJWT() error = %v", err)
	}

	md := metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	interceptor := UnaryInterceptor(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handlerCalled := false

	_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}
}

func TestUnaryInterceptorAcceptsAPIKey(t *testing.T) {
	service := NewService(Config{APIKeys: []APIKeyConfig{{Key: "k1", UserID: "user-1"}}})
	md := metadata.New(map[string]string{
		"x-api-key": "k1",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	interceptor := UnaryInterceptor(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handlerCalled := false

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}
}

func TestStreamInterceptorAcceptsValidToken(t *testing.T) {
	service := NewService(Config{JWTSecret: "secret", TokenExpiry: time.Hour})
	token, err := service.GenerateJWT(&models.User{ID: "user-1"})
	if err != nil {
		t.Fatalf("GenerateJWT() error = %v", err)
	}
	md := metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	interceptor := StreamInterceptor(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handlerCalled := false

	err = interceptor(nil, &stubServerStream{ctx: ctx}, &grpc.StreamServerInfo{}, func(srv any, stream grpc.ServerStream) error {
		handlerCalled = true
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}
}

type stubServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *stubServerStream) Context() context.Context {
	return s.ctx
}
