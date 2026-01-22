package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/policy"
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

type toolRecordingProvider struct {
	tools []string
}

func (p *toolRecordingProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	for _, tool := range req.Tools {
		p.tools = append(p.tools, tool.Name())
	}
	ch := make(chan *CompletionChunk, 1)
	ch <- &CompletionChunk{Text: "ok"}
	close(ch)
	return ch, nil
}

func (p *toolRecordingProvider) Name() string { return "tool-recording" }

func (p *toolRecordingProvider) Models() []Model { return nil }

func (p *toolRecordingProvider) SupportsTools() bool { return true }

type toolCallProvider struct {
	toolCall *models.ToolCall
}

func (p *toolCallProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	ch := make(chan *CompletionChunk, 1)
	ch <- &CompletionChunk{ToolCall: p.toolCall}
	close(ch)
	return ch, nil
}

func (p *toolCallProvider) Name() string { return "tool-call" }

func (p *toolCallProvider) Models() []Model { return nil }

func (p *toolCallProvider) SupportsTools() bool { return true }

type onceToolProvider struct {
	toolCall *models.ToolCall
	calls    int32
}

func (p *onceToolProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	call := atomic.AddInt32(&p.calls, 1)
	ch := make(chan *CompletionChunk, 1)
	if call == 1 {
		ch <- &CompletionChunk{ToolCall: p.toolCall}
	}
	close(ch)
	return ch, nil
}

func (p *onceToolProvider) Name() string { return "once-tool" }

func (p *onceToolProvider) Models() []Model { return nil }

func (p *onceToolProvider) SupportsTools() bool { return true }

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

type testTool struct {
	name        string
	executed    bool
	description string
}

func (t *testTool) Name() string { return t.name }

func (t *testTool) Description() string { return t.description }

func (t *testTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (t *testTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	t.executed = true
	return &ToolResult{Content: "ok"}, nil
}

type loopProvider struct {
	calls int32
}

func (p *loopProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	call := atomic.AddInt32(&p.calls, 1)
	ch := make(chan *CompletionChunk, 1)
	if call == 1 {
		ch <- &CompletionChunk{ToolCall: &models.ToolCall{
			ID:    "tool-1",
			Name:  "test_tool",
			Input: json.RawMessage(`{}`),
		}}
	} else {
		ch <- &CompletionChunk{Text: "done"}
	}
	close(ch)
	return ch, nil
}

func (p *loopProvider) Name() string { return "loop" }

func (p *loopProvider) Models() []Model { return nil }

func (p *loopProvider) SupportsTools() bool { return true }

type multiToolProvider struct {
	calls int32
}

func (p *multiToolProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	call := atomic.AddInt32(&p.calls, 1)
	ch := make(chan *CompletionChunk, 2)
	if call == 1 {
		ch <- &CompletionChunk{ToolCall: &models.ToolCall{
			ID:    "call-1",
			Name:  "block",
			Input: json.RawMessage(`{"id":"one"}`),
		}}
		ch <- &CompletionChunk{ToolCall: &models.ToolCall{
			ID:    "call-2",
			Name:  "block",
			Input: json.RawMessage(`{"id":"two"}`),
		}}
	}
	close(ch)
	return ch, nil
}

func (p *multiToolProvider) Name() string { return "multi-tool" }

func (p *multiToolProvider) Models() []Model { return nil }

func (p *multiToolProvider) SupportsTools() bool { return true }

type blockingTool struct {
	started chan string
	release chan struct{}
}

func (b *blockingTool) Name() string { return "block" }

func (b *blockingTool) Description() string { return "blocks until released" }

func (b *blockingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`)
}

func (b *blockingTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var payload struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(params, &payload)
	b.started <- payload.ID
	select {
	case <-b.release:
		return &ToolResult{Content: "ok"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type flakyTool struct {
	calls int32
}

func (f *flakyTool) Name() string { return "flaky" }

func (f *flakyTool) Description() string { return "fails once" }

func (f *flakyTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (f *flakyTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	call := atomic.AddInt32(&f.calls, 1)
	if call == 1 {
		return nil, errors.New("temporary failure")
	}
	return &ToolResult{Content: "ok"}, nil
}

type timeoutTool struct{}

func (t *timeoutTool) Name() string { return "timeout" }

func (t *timeoutTool) Description() string { return "waits for ctx" }

func (t *timeoutTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (t *timeoutTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type countingTool struct {
	name  string
	calls int32
}

func (c *countingTool) Name() string { return c.name }

func (c *countingTool) Description() string { return "counts calls" }

func (c *countingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (c *countingTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	atomic.AddInt32(&c.calls, 1)
	return &ToolResult{Content: "ok"}, nil
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

func TestProcessAppliesToolPolicyFilter(t *testing.T) {
	provider := &toolRecordingProvider{}
	runtime := NewRuntime(provider, stubStore{})

	allowedTool := &testTool{name: "allowed_tool"}
	mcpTool := &testTool{name: "mcp_github_search"}
	runtime.RegisterTool(allowedTool)
	runtime.RegisterTool(mcpTool)

	resolver := policy.NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})
	resolver.RegisterAlias("mcp_github_search", "mcp:github.search")
	toolPolicy := &policy.Policy{Allow: []string{"mcp:github.search"}}

	ctx := WithToolPolicy(context.Background(), resolver, toolPolicy)
	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := runtime.Process(ctx, session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	for range ch {
	}

	if len(provider.tools) != 1 || provider.tools[0] != "mcp_github_search" {
		t.Fatalf("expected only MCP tool to be passed, got %v", provider.tools)
	}
}

func TestProcessDeniesToolCallByPolicy(t *testing.T) {
	tool := &testTool{name: "danger_tool"}
	provider := &toolCallProvider{
		toolCall: &models.ToolCall{
			ID:    "call-1",
			Name:  "danger_tool",
			Input: []byte(`{}`),
		},
	}
	runtime := NewRuntime(provider, stubStore{})
	runtime.RegisterTool(tool)

	resolver := policy.NewResolver()
	toolPolicy := &policy.Policy{Allow: []string{"safe_tool"}}
	ctx := WithToolPolicy(context.Background(), resolver, toolPolicy)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := runtime.Process(ctx, session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var gotResult *models.ToolResult
	for chunk := range ch {
		if chunk.ToolResult != nil {
			gotResult = chunk.ToolResult
		}
	}

	if gotResult == nil || !gotResult.IsError {
		t.Fatalf("expected denied tool result error, got %+v", gotResult)
	}
	if tool.executed {
		t.Fatal("expected tool not to execute when denied")
	}
}

func TestProcessLoopsOnToolCalls(t *testing.T) {
	provider := &loopProvider{}
	runtime := NewRuntimeWithOptions(provider, stubStore{}, RuntimeOptions{
		MaxIterations:   2,
		ToolParallelism: 1,
	})
	tool := &testTool{name: "test_tool"}
	runtime.RegisterTool(tool)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}
	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var output strings.Builder
	for chunk := range ch {
		if chunk.Text != "" {
			output.WriteString(chunk.Text)
		}
	}

	if output.String() != "done" {
		t.Fatalf("expected output %q, got %q", "done", output.String())
	}
	if provider.calls != 2 {
		t.Fatalf("expected provider to be called twice, got %d", provider.calls)
	}
	if !tool.executed {
		t.Fatal("expected tool to execute")
	}
}

func TestProcessExecutesToolCallsInParallel(t *testing.T) {
	provider := &multiToolProvider{}

	started := make(chan string, 2)
	release := make(chan struct{})
	tool := &blockingTool{started: started, release: release}

	opts := RuntimeOptions{MaxIterations: 2, ToolParallelism: 2}
	runtime := NewRuntimeWithOptions(provider, stubStore{}, opts)
	runtime.RegisterTool(tool)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Wait until both tool calls have started before releasing.
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	seen := make(map[string]struct{})
	for len(seen) < 2 {
		select {
		case id := <-started:
			seen[id] = struct{}{}
		case <-timeout.C:
			t.Fatal("timed out waiting for parallel tool starts")
		}
	}
	close(release)

	for range ch {
	}
}

func TestProcessRetriesToolCalls(t *testing.T) {
	provider := &onceToolProvider{
		toolCall: &models.ToolCall{
			ID:    "call-1",
			Name:  "flaky",
			Input: json.RawMessage(`{}`),
		},
	}
	tool := &flakyTool{}
	runtime := NewRuntimeWithOptions(provider, stubStore{}, RuntimeOptions{
		MaxIterations:   2,
		ToolParallelism: 1,
		ToolMaxAttempts: 2,
	})
	runtime.RegisterTool(tool)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}
	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var gotResult *models.ToolResult
	for chunk := range ch {
		if chunk.ToolResult != nil {
			gotResult = chunk.ToolResult
		}
	}

	if gotResult == nil || gotResult.IsError {
		t.Fatalf("expected successful tool result, got %+v", gotResult)
	}
	if tool.calls != 2 {
		t.Fatalf("expected tool to be called twice, got %d", tool.calls)
	}
}

func TestProcessToolTimeout(t *testing.T) {
	provider := &onceToolProvider{
		toolCall: &models.ToolCall{
			ID:    "call-1",
			Name:  "timeout",
			Input: json.RawMessage(`{}`),
		},
	}
	tool := &timeoutTool{}
	runtime := NewRuntimeWithOptions(provider, stubStore{}, RuntimeOptions{
		MaxIterations:   1,
		ToolParallelism: 1,
		ToolTimeout:     10 * time.Millisecond,
	})
	runtime.RegisterTool(tool)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}
	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var gotResult *models.ToolResult
	for chunk := range ch {
		if chunk.ToolResult != nil {
			gotResult = chunk.ToolResult
		}
	}

	if gotResult == nil || !gotResult.IsError {
		t.Fatalf("expected timeout error result, got %+v", gotResult)
	}
}

func TestProcessRequiresApproval(t *testing.T) {
	provider := &onceToolProvider{
		toolCall: &models.ToolCall{
			ID:    "call-1",
			Name:  "danger_tool",
			Input: json.RawMessage(`{}`),
		},
	}
	tool := &testTool{name: "danger_tool"}
	runtime := NewRuntimeWithOptions(provider, stubStore{}, RuntimeOptions{
		MaxIterations:   2,
		ToolParallelism: 1,
		RequireApproval: []string{"danger_tool"},
	})
	runtime.RegisterTool(tool)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}
	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var gotResult *models.ToolResult
	var gotApprovalEvent *models.ToolEvent
	for chunk := range ch {
		if chunk.ToolResult != nil {
			gotResult = chunk.ToolResult
		}
		if chunk.ToolEvent != nil && chunk.ToolEvent.Stage == models.ToolEventApprovalRequired {
			gotApprovalEvent = chunk.ToolEvent
		}
	}

	if gotApprovalEvent == nil {
		t.Fatal("expected approval required tool event")
	}
	if gotResult == nil || !gotResult.IsError {
		t.Fatalf("expected approval error result, got %+v", gotResult)
	}
	if !strings.Contains(gotResult.Content, "approval required") {
		t.Fatalf("expected approval required message, got %q", gotResult.Content)
	}
	if tool.executed {
		t.Fatal("expected tool not to execute when approval is required")
	}
}

func TestProcessAsyncToolCreatesJob(t *testing.T) {
	provider := &onceToolProvider{
		toolCall: &models.ToolCall{
			ID:    "call-1",
			Name:  "async_tool",
			Input: json.RawMessage(`{}`),
		},
	}
	tool := &countingTool{name: "async_tool"}
	jobStore := jobs.NewMemoryStore()
	runtime := NewRuntimeWithOptions(provider, stubStore{}, RuntimeOptions{
		MaxIterations:   2,
		ToolParallelism: 1,
		AsyncTools:      []string{"async_tool"},
		JobStore:        jobStore,
	})
	runtime.RegisterTool(tool)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}
	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var gotResult *models.ToolResult
	for chunk := range ch {
		if chunk.ToolResult != nil {
			gotResult = chunk.ToolResult
		}
	}

	if gotResult == nil || gotResult.IsError {
		t.Fatalf("expected async tool result, got %+v", gotResult)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(gotResult.Content), &payload); err != nil {
		t.Fatalf("failed to parse job payload: %v", err)
	}
	jobID, _ := payload["job_id"].(string)
	if jobID == "" {
		t.Fatalf("expected job_id in payload, got %v", payload)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := jobStore.Get(context.Background(), jobID)
		if err != nil {
			t.Fatalf("job store get error: %v", err)
		}
		if job != nil && (job.Status == jobs.StatusSucceeded || job.Status == jobs.StatusFailed) {
			if job.Status != jobs.StatusSucceeded {
				t.Fatalf("expected job succeeded, got %s (%s)", job.Status, job.Error)
			}
			if job.Result == nil || job.Result.Content != "ok" {
				t.Fatalf("expected job result ok, got %+v", job.Result)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt32(&tool.calls) != 1 {
		t.Fatalf("expected async tool to execute once, got %d", tool.calls)
	}
}
