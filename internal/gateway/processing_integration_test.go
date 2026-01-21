package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

type inboundAdapter struct {
	inbound chan *models.Message
	sent    chan *models.Message
}

func (a *inboundAdapter) Start(ctx context.Context) error { return nil }

func (a *inboundAdapter) Stop(ctx context.Context) error { return nil }

func (a *inboundAdapter) Send(ctx context.Context, msg *models.Message) error {
	a.sent <- msg
	return nil
}

func (a *inboundAdapter) Messages() <-chan *models.Message { return a.inbound }

func (a *inboundAdapter) Type() models.ChannelType { return models.ChannelTelegram }

func (a *inboundAdapter) Status() channels.Status { return channels.Status{Connected: true} }

func (a *inboundAdapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	return channels.HealthStatus{Healthy: true}
}

func (a *inboundAdapter) Metrics() channels.MetricsSnapshot { return channels.MetricsSnapshot{} }

type streamingProvider struct{}

func (streamingProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	ch := make(chan *agent.CompletionChunk, 2)
	ch <- &agent.CompletionChunk{Text: "hello"}
	ch <- &agent.CompletionChunk{Text: " world"}
	close(ch)
	return ch, nil
}

func (streamingProvider) Name() string { return "streaming" }

func (streamingProvider) Models() []agent.Model { return nil }

func (streamingProvider) SupportsTools() bool { return false }

func TestProcessingLoopHandlesInbound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	store := &recordingStore{}
	runtime := agent.NewRuntime(streamingProvider{}, store)
	server.sessions = store
	server.runtime = runtime

	adapter := &inboundAdapter{
		inbound: make(chan *models.Message, 1),
		sent:    make(chan *models.Message, 1),
	}
	registry := channels.NewRegistry()
	registry.Register(adapter)
	server.channels = registry

	ctx, cancel := context.WithCancel(context.Background())
	server.startProcessing(ctx)
	defer cancel()

	adapter.inbound <- &models.Message{
		ID:        "tg_2",
		Channel:   models.ChannelTelegram,
		ChannelID: "2",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "ping",
		Metadata: map[string]any{
			"chat_id": int64(55),
		},
		CreatedAt: time.Now(),
	}

	select {
	case sent := <-adapter.sent:
		if sent.Content != "hello world" {
			t.Fatalf("expected concatenated response, got %q", sent.Content)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for outbound send")
	}

	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := server.waitForProcessing(waitCtx); err != nil {
		t.Fatalf("waitForProcessing() error = %v", err)
	}
}
