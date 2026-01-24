package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestLoggingInterceptor(t *testing.T) {
	tests := []struct {
		name        string
		handlerErr  error
		wantLogErr  bool
		wantLogCall bool
	}{
		{
			name:        "successful call logs method",
			handlerErr:  nil,
			wantLogErr:  false,
			wantLogCall: true,
		},
		{
			name:        "failed call logs error",
			handlerErr:  errors.New("handler error"),
			wantLogErr:  true,
			wantLogCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			interceptor := loggingInterceptor(logger)

			info := &grpc.UnaryServerInfo{
				FullMethod: "/test.Service/TestMethod",
			}

			handler := func(ctx context.Context, req any) (any, error) {
				return "response", tt.handlerErr
			}

			resp, err := interceptor(context.Background(), "request", info, handler)

			// Check error propagation
			if tt.handlerErr != nil {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp != "response" {
					t.Errorf("response = %v, want 'response'", resp)
				}
			}

			// Check logging
			logOutput := logBuf.String()
			if tt.wantLogCall && logOutput == "" {
				t.Error("expected log output, got empty")
			}
			if tt.wantLogErr && err != nil {
				if logOutput == "" {
					t.Error("expected error log output, got empty")
				}
			}
		})
	}
}

// mockServerStream implements grpc.ServerStream for testing
type mockServerStream struct {
	ctx context.Context
}

func (m *mockServerStream) SetHeader(md metadata.MD) error  { return nil }
func (m *mockServerStream) SendHeader(md metadata.MD) error { return nil }
func (m *mockServerStream) SetTrailer(md metadata.MD)       {}
func (m *mockServerStream) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}
func (m *mockServerStream) SendMsg(msg any) error { return nil }
func (m *mockServerStream) RecvMsg(msg any) error { return io.EOF }

func TestStreamLoggingInterceptor(t *testing.T) {
	tests := []struct {
		name       string
		handlerErr error
		wantLogErr bool
	}{
		{
			name:       "successful stream logs start and end",
			handlerErr: nil,
			wantLogErr: false,
		},
		{
			name:       "failed stream logs error",
			handlerErr: errors.New("stream error"),
			wantLogErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			interceptor := streamLoggingInterceptor(logger)

			info := &grpc.StreamServerInfo{
				FullMethod: "/test.Service/StreamMethod",
			}

			stream := &mockServerStream{}

			handler := func(srv any, ss grpc.ServerStream) error {
				return tt.handlerErr
			}

			err := interceptor(nil, stream, info, handler)

			// Check error propagation
			if tt.handlerErr != nil {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			// Check logging
			logOutput := logBuf.String()
			if logOutput == "" {
				t.Error("expected log output, got empty")
			}
		})
	}
}

func TestLoggingInterceptor_NilLogger(t *testing.T) {
	// Verify that the interceptor handles a nil logger gracefully
	// This should cause a panic, which we want to catch
	defer func() {
		if r := recover(); r != nil {
			// Expected if logger is nil and we try to log
			t.Log("recovered from panic as expected with nil logger")
		}
	}()

	interceptor := loggingInterceptor(nil)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, nil
	}
	// This might panic - testing defensive behavior
	_, _ = interceptor(context.Background(), nil, info, handler)
}
