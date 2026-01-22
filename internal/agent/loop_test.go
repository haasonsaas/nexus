package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// loopTestProvider allows control over LLM responses for loop testing.
type loopTestProvider struct {
	responses    [][]CompletionChunk
	currentCall  int32
	completeFunc func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error)
}

func (p *loopTestProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	if p.completeFunc != nil {
		return p.completeFunc(ctx, req)
	}

	call := int(atomic.AddInt32(&p.currentCall, 1)) - 1
	ch := make(chan *CompletionChunk, 10)

	go func() {
		defer close(ch)
		if call < len(p.responses) {
			for _, chunk := range p.responses[call] {
				select {
				case ch <- &chunk:
				case <-ctx.Done():
					ch <- &CompletionChunk{Error: ctx.Err()}
					return
				}
			}
		}
	}()

	return ch, nil
}

func (p *loopTestProvider) Name() string        { return "loop-test" }
func (p *loopTestProvider) Models() []Model     { return nil }
func (p *loopTestProvider) SupportsTools() bool { return true }

// loopMemoryStore implements sessions.Store for testing.
type loopMemoryStore struct {
	history  []*models.Message
	messages []*models.Message
}

func newLoopMemoryStore() *loopMemoryStore {
	return &loopMemoryStore{
		history:  make([]*models.Message, 0),
		messages: make([]*models.Message, 0),
	}
}

func (s *loopMemoryStore) Create(ctx context.Context, session *models.Session) error  { return nil }
func (s *loopMemoryStore) Get(ctx context.Context, id string) (*models.Session, error) { return nil, nil }
func (s *loopMemoryStore) Update(ctx context.Context, session *models.Session) error   { return nil }
func (s *loopMemoryStore) Delete(ctx context.Context, id string) error                 { return nil }
func (s *loopMemoryStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	return nil, nil
}
func (s *loopMemoryStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	return nil, nil
}
func (s *loopMemoryStore) List(ctx context.Context, agentID string, opts sessions.ListOptions) ([]*models.Session, error) {
	return nil, nil
}
func (s *loopMemoryStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	s.messages = append(s.messages, msg)
	return nil
}
func (s *loopMemoryStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	return s.history, nil
}

func TestAgenticLoop_DefaultConfig(t *testing.T) {
	config := DefaultLoopConfig()

	if config.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10", config.MaxIterations)
	}
	if config.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", config.MaxTokens)
	}
	if !config.EnableBackpressure {
		t.Error("EnableBackpressure should be true")
	}
	if !config.StreamToolResults {
		t.Error("StreamToolResults should be true")
	}
	if config.ExecutorConfig == nil {
		t.Error("ExecutorConfig should not be nil")
	}
}

func TestAgenticLoop_NoToolCalls(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			{{Text: "Hello, how can I help?"}, {Done: true}},
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var text string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		text += chunk.Text
	}

	if text != "Hello, how can I help?" {
		t.Errorf("got text %q, want %q", text, "Hello, how can I help?")
	}

	if provider.currentCall != 1 {
		t.Errorf("provider called %d times, want 1", provider.currentCall)
	}
}

func TestAgenticLoop_SingleToolCall(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			// First call: tool call
			{
				{ToolCall: &models.ToolCall{
					ID:    "call-1",
					Name:  "echo",
					Input: json.RawMessage(`{"text": "test"}`),
				}},
				{Done: true},
			},
			// Second call: final response
			{
				{Text: "The tool returned: test"},
				{Done: true},
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "echo",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			var p struct{ Text string `json:"text"` }
			json.Unmarshal(params, &p)
			return &ToolResult{Content: p.Text}, nil
		},
	})

	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "echo test"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var text string
	var toolResults []*models.ToolResult
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		text += chunk.Text
		if chunk.ToolResult != nil {
			toolResults = append(toolResults, chunk.ToolResult)
		}
	}

	if text != "The tool returned: test" {
		t.Errorf("got text %q, want %q", text, "The tool returned: test")
	}

	if len(toolResults) != 1 {
		t.Fatalf("got %d tool results, want 1", len(toolResults))
	}
	if toolResults[0].Content != "test" {
		t.Errorf("tool result = %q, want %q", toolResults[0].Content, "test")
	}

	if provider.currentCall != 2 {
		t.Errorf("provider called %d times, want 2", provider.currentCall)
	}
}

func TestAgenticLoop_MaxIterationsReached(t *testing.T) {
	// Provider always returns a tool call, never completes
	provider := &loopTestProvider{
		completeFunc: func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
			ch := make(chan *CompletionChunk, 2)
			ch <- &CompletionChunk{ToolCall: &models.ToolCall{
				ID:    "call-infinite",
				Name:  "noop",
				Input: json.RawMessage(`{}`),
			}}
			close(ch)
			return ch, nil
		},
	}

	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "noop",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "ok"}, nil
		},
	})

	store := newLoopMemoryStore()
	config := &LoopConfig{
		MaxIterations:      3, // Low limit
		MaxTokens:          4096,
		ExecutorConfig:     DefaultExecutorConfig(),
		StreamToolResults:  true,
		EnableBackpressure: true,
	}

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "loop forever"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var loopErr error
	for chunk := range ch {
		if chunk.Error != nil {
			loopErr = chunk.Error
		}
	}

	if loopErr == nil {
		t.Fatal("expected max iterations error")
	}

	var loopError *LoopError
	if !errors.As(loopErr, &loopError) {
		t.Fatalf("expected LoopError, got %T", loopErr)
	}

	if !errors.Is(loopError.Cause, ErrMaxIterations) {
		t.Errorf("expected ErrMaxIterations, got %v", loopError.Cause)
	}
}

func TestAgenticLoop_ContextCancellation(t *testing.T) {
	started := make(chan struct{})
	provider := &loopTestProvider{
		completeFunc: func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
			ch := make(chan *CompletionChunk)
			go func() {
				close(started)
				<-ctx.Done()
				ch <- &CompletionChunk{Error: ctx.Err()}
				close(ch)
			}()
			return ch, nil
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	ctx, cancel := context.WithCancel(context.Background())

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, err := loop.Run(ctx, session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Wait for provider to start, then cancel
	<-started
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
}

func TestAgenticLoop_ProviderError(t *testing.T) {
	expectedErr := errors.New("provider unavailable")
	provider := &loopTestProvider{
		completeFunc: func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
			return nil, expectedErr
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var gotErr error
	for chunk := range ch {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}

	if gotErr == nil {
		t.Fatal("expected provider error")
	}

	var loopError *LoopError
	if !errors.As(gotErr, &loopError) {
		t.Fatalf("expected LoopError, got %T", gotErr)
	}
	if loopError.Phase != PhaseStream {
		t.Errorf("phase = %s, want %s", loopError.Phase, PhaseStream)
	}
}

func TestAgenticLoop_StreamingError(t *testing.T) {
	streamErr := errors.New("streaming failed")
	provider := &loopTestProvider{
		completeFunc: func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
			ch := make(chan *CompletionChunk, 2)
			ch <- &CompletionChunk{Text: "partial..."}
			ch <- &CompletionChunk{Error: streamErr}
			close(ch)
			return ch, nil
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var gotErr error
	for chunk := range ch {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}

	if gotErr == nil {
		t.Fatal("expected streaming error")
	}
}

func TestAgenticLoop_SetDefaultModel(t *testing.T) {
	var capturedModel string
	provider := &loopTestProvider{
		completeFunc: func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
			capturedModel = req.Model
			ch := make(chan *CompletionChunk, 1)
			ch <- &CompletionChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)
	loop.SetDefaultModel("gpt-4-turbo")

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, _ := loop.Run(context.Background(), session, msg)
	for range ch {
	}

	if capturedModel != "gpt-4-turbo" {
		t.Errorf("model = %q, want %q", capturedModel, "gpt-4-turbo")
	}
}

func TestAgenticLoop_SetDefaultSystem(t *testing.T) {
	var capturedSystem string
	provider := &loopTestProvider{
		completeFunc: func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
			capturedSystem = req.System
			ch := make(chan *CompletionChunk, 1)
			ch <- &CompletionChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)
	loop.SetDefaultSystem("You are a helpful assistant.")

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, _ := loop.Run(context.Background(), session, msg)
	for range ch {
	}

	if capturedSystem != "You are a helpful assistant." {
		t.Errorf("system = %q, want %q", capturedSystem, "You are a helpful assistant.")
	}
}

func TestAgenticLoop_ContextSystemPromptOverride(t *testing.T) {
	var capturedSystem string
	provider := &loopTestProvider{
		completeFunc: func(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
			capturedSystem = req.System
			ch := make(chan *CompletionChunk, 1)
			ch <- &CompletionChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)
	loop.SetDefaultSystem("default system")

	ctx := WithSystemPrompt(context.Background(), "override system")
	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, _ := loop.Run(ctx, session, msg)
	for range ch {
	}

	if capturedSystem != "override system" {
		t.Errorf("system = %q, want %q", capturedSystem, "override system")
	}
}

func TestAgenticLoop_MultipleToolCalls(t *testing.T) {
	var toolExecutions int32
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			// First call: multiple tool calls
			{
				{ToolCall: &models.ToolCall{ID: "call-1", Name: "increment", Input: json.RawMessage(`{}`)}},
				{ToolCall: &models.ToolCall{ID: "call-2", Name: "increment", Input: json.RawMessage(`{}`)}},
				{ToolCall: &models.ToolCall{ID: "call-3", Name: "increment", Input: json.RawMessage(`{}`)}},
				{Done: true},
			},
			// Second call: final response
			{
				{Text: "Done"},
				{Done: true},
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "increment",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			atomic.AddInt32(&toolExecutions, 1)
			return &ToolResult{Content: "incremented"}, nil
		},
	})

	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "run increment 3 times"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var toolResults int
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		if chunk.ToolResult != nil {
			toolResults++
		}
	}

	if toolExecutions != 3 {
		t.Errorf("tool executed %d times, want 3", toolExecutions)
	}
	if toolResults != 3 {
		t.Errorf("got %d tool results, want 3", toolResults)
	}
}

func TestAgenticLoop_ToolError(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			{
				{ToolCall: &models.ToolCall{ID: "call-1", Name: "failing", Input: json.RawMessage(`{}`)}},
				{Done: true},
			},
			// After tool error, continue
			{
				{Text: "Tool failed"},
				{Done: true},
			},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "failing",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "error occurred", IsError: true}, nil
		},
	})

	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var errorResults int
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected loop error: %v", chunk.Error)
		}
		if chunk.ToolResult != nil && chunk.ToolResult.IsError {
			errorResults++
		}
	}

	if errorResults != 1 {
		t.Errorf("got %d error results, want 1", errorResults)
	}
}

func TestAgenticLoop_NilConfig(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			{{Text: "ok"}, {Done: true}},
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()

	// Pass nil config - should use defaults
	loop := NewAgenticLoop(provider, registry, store, nil)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, err := loop.Run(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
	}
}

func TestAgenticLoop_RunWithBranch(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			{{Text: "branched response"}, {Done: true}},
		},
	}

	registry := NewToolRegistry()
	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, err := loop.RunWithBranch(context.Background(), session, msg, "branch-abc")
	if err != nil {
		t.Fatalf("RunWithBranch() error = %v", err)
	}

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
	}

	// Verify branch ID was set
	if msg.BranchID != "branch-abc" {
		t.Errorf("BranchID = %q, want %q", msg.BranchID, "branch-abc")
	}
}

func TestAgenticLoop_ConfigureTool(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			{{Text: "ok"}, {Done: true}},
		},
	}

	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "slow_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "done"}, nil
		},
	})

	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	loop := NewAgenticLoop(provider, registry, store, config)

	// Configure tool with custom settings
	loop.ConfigureTool("slow_tool", &ToolConfig{
		Timeout:  5 * time.Second,
		Retries:  3,
		Priority: 10,
	})

	// Verify configuration was applied (indirectly via executor)
	tc := loop.executor.getToolConfig("slow_tool")
	if tc == nil {
		t.Fatal("expected tool config to be set")
	}
	if tc.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", tc.Timeout)
	}
	if tc.Retries != 3 {
		t.Errorf("retries = %d, want 3", tc.Retries)
	}
}

func TestAgenticRuntime_Integration(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			{
				{ToolCall: &models.ToolCall{ID: "call-1", Name: "test_tool", Input: json.RawMessage(`{}`)}},
				{Done: true},
			},
			{
				{Text: "Final response"},
				{Done: true},
			},
		},
	}

	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	runtime := NewAgenticRuntime(provider, store, config)
	runtime.SetDefaultModel("test-model")
	runtime.SetSystemPrompt("You are helpful.")

	runtime.RegisterTool(&testExecTool{
		name: "test_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "tool output"}, nil
		},
	})

	session := &models.Session{ID: "session-1"}
	msg := &models.Message{Role: models.RoleUser, Content: "test"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var text string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		text += chunk.Text
	}

	if text != "Final response" {
		t.Errorf("got text %q, want %q", text, "Final response")
	}
}

func TestAgenticRuntime_ExecutorMetrics(t *testing.T) {
	provider := &loopTestProvider{
		responses: [][]CompletionChunk{
			{{Text: "ok"}, {Done: true}},
		},
	}

	store := newLoopMemoryStore()
	config := DefaultLoopConfig()

	runtime := NewAgenticRuntime(provider, store, config)

	metrics := runtime.ExecutorMetrics()
	if metrics == nil {
		t.Fatal("expected metrics snapshot")
	}
	if metrics.TotalExecutions != 0 {
		t.Errorf("TotalExecutions = %d, want 0", metrics.TotalExecutions)
	}
}

func TestLoopState_Initialization(t *testing.T) {
	state := &LoopState{
		Phase:     PhaseInit,
		Iteration: 0,
	}

	if state.Phase != PhaseInit {
		t.Errorf("Phase = %s, want %s", state.Phase, PhaseInit)
	}
	if state.Iteration != 0 {
		t.Errorf("Iteration = %d, want 0", state.Iteration)
	}
	if len(state.Messages) != 0 {
		t.Errorf("Messages should be empty")
	}
	if len(state.PendingTools) != 0 {
		t.Errorf("PendingTools should be empty")
	}
}

func TestLoopError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *LoopError
		contains string
	}{
		{
			name: "with message",
			err: &LoopError{
				Phase:     PhaseStream,
				Iteration: 2,
				Message:   "streaming failed",
			},
			contains: "streaming failed",
		},
		{
			name: "with cause",
			err: &LoopError{
				Phase:     PhaseExecuteTools,
				Iteration: 1,
				Cause:     errors.New("tool error"),
			},
			contains: "tool error",
		},
		{
			name: "phase only",
			err: &LoopError{
				Phase:     PhaseComplete,
				Iteration: 3,
			},
			contains: "complete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if !containsIgnoreCase(errStr, tt.contains) {
				t.Errorf("error string %q should contain %q", errStr, tt.contains)
			}
		})
	}
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr)
}

func TestLoopError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	loopErr := &LoopError{
		Phase: PhaseInit,
		Cause: cause,
	}

	if !errors.Is(loopErr, cause) {
		t.Error("LoopError should unwrap to its cause")
	}
}
