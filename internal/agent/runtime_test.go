package agent

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubProvider struct{}

func (stubProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	ch := make(chan *CompletionChunk)
	close(ch)
	return ch, nil
}

func (stubProvider) Name() string { return "stub" }

func (stubProvider) Models() []Model { return nil }

func (stubProvider) SupportsTools() bool { return false }

type stubStore struct{}

func (stubStore) Create(ctx context.Context, session *models.Session) error { return nil }

func (stubStore) Get(ctx context.Context, id string) (*models.Session, error) { return nil, nil }

func (stubStore) Update(ctx context.Context, session *models.Session) error { return nil }

func (stubStore) Delete(ctx context.Context, id string) error { return nil }

func (stubStore) GetByKey(ctx context.Context, key string) (*models.Session, error) { return nil, nil }

func (stubStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	return nil, nil
}

func (stubStore) List(ctx context.Context, agentID string, opts sessions.ListOptions) ([]*models.Session, error) {
	return nil, nil
}

func (stubStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	return nil
}

func (stubStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	return nil, nil
}

func TestProcessReturnsBufferedChannel(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})
	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if cap(ch) != processBufferSize {
		t.Fatalf("expected buffered channel size %d, got %d", processBufferSize, cap(ch))
	}

	for range ch {
	}
}
