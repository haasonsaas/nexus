package gateway

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

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

type blockingProvider struct {
	started chan struct{}
	done    chan struct{}
}

func (p *blockingProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if p.started != nil {
		select {
		case <-p.started:
		default:
			close(p.started)
		}
	}
	ch := make(chan *agent.CompletionChunk)
	go func() {
		<-ctx.Done()
		if p.done != nil {
			close(p.done)
		}
		close(ch)
	}()
	return ch, nil
}

func (p *blockingProvider) Name() string { return "blocking" }

func (p *blockingProvider) Models() []agent.Model { return nil }

func (p *blockingProvider) SupportsTools() bool { return false }

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

func TestHandleMessageCommandStopCancelsRun(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	store := sessions.NewMemoryStore()
	provider := &blockingProvider{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
	runtime := agent.NewRuntime(provider, store)
	server.sessions = store
	server.runtime = runtime

	adapter := &recordingAdapter{}
	registry := channels.NewRegistry()
	registry.Register(adapter)
	server.channels = registry

	msg := &models.Message{
		ID:        "long_run",
		Channel:   models.ChannelTelegram,
		ChannelID: "1",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "long task",
		Metadata: map[string]any{
			"chat_id": int64(77),
		},
	}

	go server.handleMessage(context.Background(), msg)

	select {
	case <-provider.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for provider to start")
	}

	stopMsg := &models.Message{
		ID:        "stop_cmd",
		Channel:   models.ChannelTelegram,
		ChannelID: "2",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "/stop",
		Metadata: map[string]any{
			"chat_id": int64(77),
		},
	}
	server.handleMessage(context.Background(), stopMsg)

	select {
	case <-provider.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected /stop to cancel active run")
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	found := false
	for _, sent := range adapter.messages {
		if strings.Contains(sent.Content, "Stopping") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stop acknowledgement, got %#v", adapter.messages)
	}
}

func TestHandleMessageCommandNewSetsModelForNewSession(t *testing.T) {
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
	server.sessions = store
	server.runtime = runtime

	adapter := &recordingAdapter{}
	registry := channels.NewRegistry()
	registry.Register(adapter)
	server.channels = registry

	cmdMsg := &models.Message{
		ID:        "cmd_new",
		Channel:   models.ChannelTelegram,
		ChannelID: "1",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "/new gpt-4",
		Metadata: map[string]any{
			"chat_id": int64(55),
		},
	}
	server.handleMessage(context.Background(), cmdMsg)

	if calls, _ := provider.stats(); calls != 0 {
		t.Fatalf("expected runtime not to run for /new, got calls=%d", calls)
	}

	sessionKey := sessions.SessionKey("agent-test", models.ChannelTelegram, "55")
	session, err := store.GetByKey(context.Background(), sessionKey)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}
	if session.Metadata == nil || session.Metadata["model"] != "gpt-4" {
		t.Fatalf("expected new session model override to be set, got %#v", session.Metadata)
	}
}

func TestHandleMessageCommandBlockedByAllowlist(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Commands: config.CommandsConfig{
			Enabled:   boolPtr(true),
			AllowFrom: map[string][]string{"telegram": {"999"}},
		},
	}
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
			"user_id": int64(123),
		},
	}

	server.handleMessage(context.Background(), msg)

	if calls, _ := provider.stats(); calls != 0 {
		t.Fatalf("expected runtime not to run for blocked /help, got calls=%d", calls)
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.messages) != 0 {
		t.Fatalf("expected no reply for blocked command, got %d", len(adapter.messages))
	}
}

func TestHandleMessageInlineCommandStripsAndRuns(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Commands: config.CommandsConfig{
			Enabled:         boolPtr(true),
			InlineAllowFrom: map[string][]string{"telegram": {"123"}},
		},
	}
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
		ID:        "inline_status",
		Channel:   models.ChannelTelegram,
		ChannelID: "1",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "hello /status there",
		Metadata: map[string]any{
			"chat_id": int64(77),
			"user_id": "123",
		},
	}

	server.handleMessage(context.Background(), msg)

	if calls, _ := provider.stats(); calls != 1 {
		t.Fatalf("expected runtime to run once, got calls=%d", calls)
	}

	sessionKey := sessions.SessionKey("main", models.ChannelTelegram, "77")
	session, err := store.GetByKey(context.Background(), sessionKey)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}
	history, err := store.GetHistory(context.Background(), session.ID, 10)
	if err != nil {
		t.Fatalf("failed to load history: %v", err)
	}
	var inbound *models.Message
	for _, entry := range history {
		if entry.Role == models.RoleUser {
			inbound = entry
			break
		}
	}
	if inbound == nil {
		t.Fatal("expected inbound message in history")
	}
	if strings.Contains(inbound.Content, "/status") {
		t.Fatalf("expected inline command stripped, got %q", inbound.Content)
	}
	if strings.TrimSpace(inbound.Content) != "hello there" {
		t.Fatalf("expected stripped content 'hello there', got %q", inbound.Content)
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.messages) != 2 {
		t.Fatalf("expected 2 outbound messages (status + reply), got %d", len(adapter.messages))
	}
	foundStatus := false
	for _, sent := range adapter.messages {
		if strings.Contains(sent.Content, "Session active") {
			foundStatus = true
			break
		}
	}
	if !foundStatus {
		t.Fatalf("expected inline /status reply, got %#v", adapter.messages)
	}
}

func TestHandleMessageInlineCommandIgnoredWithoutAllowlist(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Commands: config.CommandsConfig{
			Enabled: boolPtr(true),
		},
	}
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
		ID:        "inline_blocked",
		Channel:   models.ChannelTelegram,
		ChannelID: "1",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "hello /status there",
		Metadata: map[string]any{
			"chat_id": int64(88),
			"user_id": int64(321),
		},
	}

	server.handleMessage(context.Background(), msg)

	if calls, _ := provider.stats(); calls != 1 {
		t.Fatalf("expected runtime to run once, got calls=%d", calls)
	}

	sessionKey := sessions.SessionKey("main", models.ChannelTelegram, "88")
	session, err := store.GetByKey(context.Background(), sessionKey)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}
	history, err := store.GetHistory(context.Background(), session.ID, 10)
	if err != nil {
		t.Fatalf("failed to load history: %v", err)
	}
	var inbound *models.Message
	for _, entry := range history {
		if entry.Role == models.RoleUser {
			inbound = entry
			break
		}
	}
	if inbound == nil {
		t.Fatal("expected inbound message in history")
	}
	if !strings.Contains(inbound.Content, "/status") {
		t.Fatalf("expected inline command preserved, got %q", inbound.Content)
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.messages) != 1 {
		t.Fatalf("expected 1 outbound message (model reply), got %d", len(adapter.messages))
	}
}

func boolPtr(value bool) *bool {
	return &value
}
