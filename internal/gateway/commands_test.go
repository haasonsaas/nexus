package gateway

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

type countingProvider struct {
	mu        sync.Mutex
	calls     int
	lastModel string
}

func (p *countingProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	p.mu.Lock()
	p.calls++
	p.lastModel = req.Model
	p.mu.Unlock()

	ch := make(chan *agent.CompletionChunk, 1)
	ch <- &agent.CompletionChunk{Text: "ok"}
	close(ch)
	return ch, nil
}

func (p *countingProvider) Name() string { return "counting" }

func (p *countingProvider) Models() []agent.Model { return nil }

func (p *countingProvider) SupportsTools() bool { return false }

func (p *countingProvider) stats() (int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.lastModel
}

func TestHandleMessageCommandHelpBypassesRuntime(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	store := sessions.NewMemoryStore()
	provider := &countingProvider{}
	runtime := agent.NewRuntime(provider, store)
	server.sessions = store
	server.runtime = runtime

	adapter := &recordingAdapter{}
	registry := channels.NewRegistry()
	registry.Register(adapter)
	server.channels = registry

	msg := &models.Message{
		ID:        "cmd_help",
		Channel:   models.ChannelTelegram,
		ChannelID: "1",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "/help",
		Metadata: map[string]any{
			"chat_id": int64(42),
		},
	}

	server.handleMessage(context.Background(), msg)

	if calls, _ := provider.stats(); calls != 0 {
		t.Fatalf("expected runtime not to run for /help, got calls=%d", calls)
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.messages) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(adapter.messages))
	}
	if !strings.Contains(adapter.messages[0].Content, "Available Commands") {
		t.Fatalf("expected help output, got %q", adapter.messages[0].Content)
	}
}

func TestHandleMessageCommandModelSetsOverride(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Session: config.SessionConfig{
			DefaultAgentID: "agent-test",
		},
	}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	store := sessions.NewMemoryStore()
	provider := &countingProvider{}
	runtime := agent.NewRuntime(provider, store)
	runtime.SetDefaultModel("default-model")
	server.sessions = store
	server.runtime = runtime
	server.defaultModel = "default-model"

	adapter := &recordingAdapter{}
	registry := channels.NewRegistry()
	registry.Register(adapter)
	server.channels = registry

	cmdMsg := &models.Message{
		ID:        "cmd_model",
		Channel:   models.ChannelTelegram,
		ChannelID: "2",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "/model gpt-4",
		Metadata: map[string]any{
			"chat_id": int64(99),
		},
	}
	server.handleMessage(context.Background(), cmdMsg)

	if calls, _ := provider.stats(); calls != 0 {
		t.Fatalf("expected runtime not to run for /model, got calls=%d", calls)
	}

	sessionKey := sessions.SessionKey("agent-test", models.ChannelTelegram, "99")
	session, err := store.GetByKey(context.Background(), sessionKey)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}
	if session.Metadata == nil || session.Metadata["model"] != "gpt-4" {
		t.Fatalf("expected session model override to be set, got %#v", session.Metadata)
	}

	msg := &models.Message{
		ID:        "next_msg",
		Channel:   models.ChannelTelegram,
		ChannelID: "3",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "hello",
		Metadata: map[string]any{
			"chat_id": int64(99),
		},
	}
	server.handleMessage(context.Background(), msg)

	if calls, lastModel := provider.stats(); calls != 1 || lastModel != "gpt-4" {
		t.Fatalf("expected model override applied (calls=1, model=gpt-4), got calls=%d model=%q", calls, lastModel)
	}
}
