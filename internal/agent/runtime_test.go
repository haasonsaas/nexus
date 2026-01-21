package agent

import (
	"context"
	"errors"
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

type recordingProvider struct {
	lastModel  string
	lastSystem string
}

func (p *recordingProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	p.lastModel = req.Model
	p.lastSystem = req.System
	ch := make(chan *CompletionChunk, 1)
	ch <- &CompletionChunk{Text: "ok"}
	close(ch)
	return ch, nil
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) Models() []Model { return nil }

func (p *recordingProvider) SupportsTools() bool { return false }

type cancelProvider struct {
	started chan struct{}
}

func (p *cancelProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	ch := make(chan *CompletionChunk, 1)
	close(p.started)
	go func() {
		<-ctx.Done()
		ch <- &CompletionChunk{Error: ctx.Err()}
		close(ch)
	}()
	return ch, nil
}

func (p *cancelProvider) Name() string { return "cancel" }

func (p *cancelProvider) Models() []Model { return nil }

func (p *cancelProvider) SupportsTools() bool { return false }

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

func TestProcessUsesDefaultModel(t *testing.T) {
	provider := &recordingProvider{}
	runtime := NewRuntime(provider, stubStore{})
	runtime.SetDefaultModel("gpt-4o")
	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	for range ch {
	}

	if provider.lastModel != "gpt-4o" {
		t.Fatalf("expected default model gpt-4o, got %q", provider.lastModel)
	}
}

func TestProcessUsesDefaultSystemPrompt(t *testing.T) {
	provider := &recordingProvider{}
	runtime := NewRuntime(provider, stubStore{})
	runtime.SetSystemPrompt("system prompt")
	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	for range ch {
	}

	if provider.lastSystem != "system prompt" {
		t.Fatalf("expected system prompt to be applied, got %q", provider.lastSystem)
	}
}

func TestProcessUsesContextSystemPromptOverride(t *testing.T) {
	provider := &recordingProvider{}
	runtime := NewRuntime(provider, stubStore{})
	runtime.SetSystemPrompt("default prompt")
	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ctx := WithSystemPrompt(context.Background(), "override prompt")
	ch, err := runtime.Process(ctx, session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	for range ch {
	}

	if provider.lastSystem != "override prompt" {
		t.Fatalf("expected override prompt, got %q", provider.lastSystem)
	}
}

func TestProcessPropagatesContextCancel(t *testing.T) {
	provider := &cancelProvider{started: make(chan struct{})}
	runtime := NewRuntime(provider, stubStore{})
	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := runtime.Process(ctx, session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	<-provider.started
	cancel()

	var gotErr error
	for chunk := range ch {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}

	if gotErr == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", gotErr)
	}
}
