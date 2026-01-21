package gateway

import (
	"context"
	"io"
	"log/slog"
	"strconv"
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

type recordingAdapter struct {
	messages []*models.Message
	mu       sync.Mutex
}

func (a *recordingAdapter) Start(ctx context.Context) error { return nil }

func (a *recordingAdapter) Stop(ctx context.Context) error { return nil }

func (a *recordingAdapter) Send(ctx context.Context, msg *models.Message) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, msg)
	return nil
}

func (a *recordingAdapter) Messages() <-chan *models.Message { return nil }

func (a *recordingAdapter) Type() models.ChannelType { return models.ChannelTelegram }

func (a *recordingAdapter) Status() channels.Status { return channels.Status{Connected: true} }

func (a *recordingAdapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	return channels.HealthStatus{Healthy: true}
}

func (a *recordingAdapter) Metrics() channels.MetricsSnapshot { return channels.MetricsSnapshot{} }

type recordingStore struct {
	messages []*models.Message
	session  *models.Session
	lastKey  string
}

func (s *recordingStore) Create(ctx context.Context, session *models.Session) error { return nil }

func (s *recordingStore) Get(ctx context.Context, id string) (*models.Session, error) {
	return nil, nil
}

func (s *recordingStore) Update(ctx context.Context, session *models.Session) error { return nil }

func (s *recordingStore) Delete(ctx context.Context, id string) error { return nil }

func (s *recordingStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	return nil, nil
}

func (s *recordingStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	s.lastKey = key
	if s.session == nil {
		s.session = &models.Session{
			ID:        "session-1",
			AgentID:   agentID,
			Channel:   channel,
			ChannelID: channelID,
			Key:       key,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}
	return s.session, nil
}

func (s *recordingStore) List(ctx context.Context, agentID string, opts sessions.ListOptions) ([]*models.Session, error) {
	return nil, nil
}

func (s *recordingStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	copied := *msg
	s.messages = append(s.messages, &copied)
	return nil
}

func (s *recordingStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	return nil, nil
}

type fixedProvider struct{}

func (fixedProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	ch := make(chan *agent.CompletionChunk, 1)
	ch <- &agent.CompletionChunk{Text: "pong"}
	close(ch)
	return ch, nil
}

func (fixedProvider) Name() string { return "fixed" }

func (fixedProvider) Models() []agent.Model { return nil }

func (fixedProvider) SupportsTools() bool { return false }

func TestHandleMessagePersistsAndResponds(t *testing.T) {
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

	store := &recordingStore{}
	runtime := agent.NewRuntime(fixedProvider{}, store)
	server.sessions = store
	server.runtime = runtime

	adapter := &recordingAdapter{}
	registry := channels.NewRegistry()
	registry.Register(adapter)
	server.channels = registry

	msg := &models.Message{
		ID:        "tg_1",
		Channel:   models.ChannelTelegram,
		ChannelID: "1",
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "ping",
		Metadata: map[string]any{
			"chat_id": int64(123),
		},
		CreatedAt: time.Now(),
	}

	server.handleMessage(context.Background(), msg)

	if store.lastKey == "" {
		t.Fatal("expected session key to be set")
	}
	if !strings.HasPrefix(store.lastKey, "agent-test:") {
		t.Fatalf("expected session key to use agent-test, got %q", store.lastKey)
	}

	if len(store.messages) != 2 {
		t.Fatalf("expected 2 messages persisted, got %d", len(store.messages))
	}

	inbound := store.messages[0]
	if inbound.SessionID != "session-1" {
		t.Fatalf("expected inbound session id to be set, got %q", inbound.SessionID)
	}

	outbound := store.messages[1]
	if outbound.Direction != models.DirectionOutbound {
		t.Fatalf("expected outbound direction, got %v", outbound.Direction)
	}
	if outbound.Content != "pong" {
		t.Fatalf("expected outbound content pong, got %q", outbound.Content)
	}

	if len(adapter.messages) != 1 {
		t.Fatalf("expected 1 outbound message sent, got %d", len(adapter.messages))
	}

	sent := adapter.messages[0]
	chatID, ok := sent.Metadata["chat_id"]
	if !ok {
		t.Fatalf("expected chat_id metadata on outbound message")
	}
	if id, ok := chatID.(int64); ok && id != 123 {
		t.Fatalf("expected chat_id 123, got %d", id)
	}
	if id, ok := chatID.(int); ok && id != 123 {
		t.Fatalf("expected chat_id 123, got %d", id)
	}
	if id, ok := chatID.(string); ok {
		if id != strconv.FormatInt(123, 10) {
			t.Fatalf("expected chat_id 123, got %s", id)
		}
	}
}
