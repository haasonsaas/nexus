package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubAdapter struct {
	messages chan *models.Message
}

func (a *stubAdapter) Start(ctx context.Context) error { return nil }

func (a *stubAdapter) Stop(ctx context.Context) error {
	close(a.messages)
	return nil
}

func (a *stubAdapter) Send(ctx context.Context, msg *models.Message) error { return nil }

func (a *stubAdapter) Messages() <-chan *models.Message { return a.messages }

func (a *stubAdapter) Type() models.ChannelType { return models.ChannelTelegram }

func (a *stubAdapter) Status() channels.Status { return channels.Status{Connected: true} }

func (a *stubAdapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	return channels.HealthStatus{Healthy: true}
}

func (a *stubAdapter) Metrics() channels.MetricsSnapshot { return channels.MetricsSnapshot{} }

func TestStopWaitsForMessageHandlers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	adapter := &stubAdapter{messages: make(chan *models.Message, 1)}
	registry := channels.NewRegistry()
	registry.Register(adapter)
	server.channels = registry

	started := make(chan struct{})
	block := make(chan struct{})
	server.handleMessageHook = func(ctx context.Context, msg *models.Message) {
		close(started)
		<-block
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.startProcessing(ctx)

	adapter.messages <- &models.Message{
		Channel: models.ChannelTelegram,
		Role:    models.RoleUser,
		Content: "hello",
	}

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("handler did not start")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	done := make(chan error, 1)
	go func() {
		done <- server.Stop(stopCtx)
	}()

	select {
	case err := <-done:
		t.Fatalf("Stop returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
		// expected: Stop should block until handler completes
	}

	close(block)

	if err := <-done; err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
