package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptorAllowsWhenNoSecret(t *testing.T) {
	interceptor := authUnaryInterceptor("", slog.New(slog.NewTextHandler(io.Discard, nil)))
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

func TestAuthInterceptorRejectsMissingToken(t *testing.T) {
	interceptor := authUnaryInterceptor("secret", slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	})

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
}

func TestAuthInterceptorAcceptsValidToken(t *testing.T) {
	token, err := signJWT("secret")
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	md := metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	interceptor := authUnaryInterceptor("secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
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

func TestStreamAuthInterceptorAcceptsValidToken(t *testing.T) {
	token, err := signJWT("secret")
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	md := metadata.New(map[string]string{
		"authorization": "Bearer " + token,
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	interceptor := authStreamInterceptor("secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
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

func signJWT(secret string) (string, error) {
	claims := jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
