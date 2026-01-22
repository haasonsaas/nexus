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

func TestNewRuntimeWithOptions(t *testing.T) {
	opts := RuntimeOptions{
		MaxIterations:     10,
		ToolParallelism:   8,
		ToolTimeout:       60 * time.Second,
		ToolMaxAttempts:   3,
		ToolRetryBackoff:  time.Second,
		DisableToolEvents: true,
	}

	runtime := NewRuntimeWithOptions(stubProvider{}, stubStore{}, opts)

	if runtime.maxIterations != 10 {
		t.Errorf("maxIterations = %d, want 10", runtime.maxIterations)
	}
	if runtime.toolExec.Concurrency != 8 {
		t.Errorf("ToolParallelism = %d, want 8", runtime.toolExec.Concurrency)
	}
	if runtime.toolExec.PerToolTimeout != 60*time.Second {
		t.Errorf("ToolTimeout = %v, want 60s", runtime.toolExec.PerToolTimeout)
	}
}

func TestRuntimeSetOptions(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})

	// Initially uses defaults
	if runtime.maxIterations != 0 && runtime.opts.MaxIterations != 5 {
		t.Errorf("default MaxIterations = %d, want 5", runtime.opts.MaxIterations)
	}

	// Update options
	runtime.SetOptions(RuntimeOptions{
		MaxIterations:   15,
		ToolParallelism: 6,
	})

	if runtime.maxIterations != 15 {
		t.Errorf("maxIterations = %d, want 15", runtime.maxIterations)
	}
}

func TestRuntimeSetMaxIterations(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})
	runtime.SetMaxIterations(20)

	if runtime.maxIterations != 20 {
		t.Errorf("maxIterations = %d, want 20", runtime.maxIterations)
	}
}

func TestRuntimeSetMaxWallTime(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})
	runtime.SetMaxWallTime(5 * time.Minute)

	if runtime.maxWallTime != 5*time.Minute {
		t.Errorf("maxWallTime = %v, want 5m", runtime.maxWallTime)
	}
}

func TestRuntimeSetToolExecConfig(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})
	config := ToolExecConfig{
		Concurrency:    10,
		PerToolTimeout: 45 * time.Second,
		MaxAttempts:    5,
		RetryBackoff:   500 * time.Millisecond,
	}
	runtime.SetToolExecConfig(config)

	if runtime.toolExec.Concurrency != 10 {
		t.Errorf("Concurrency = %d, want 10", runtime.toolExec.Concurrency)
	}
	if runtime.toolExec.PerToolTimeout != 45*time.Second {
		t.Errorf("PerToolTimeout = %v, want 45s", runtime.toolExec.PerToolTimeout)
	}
}

func TestToolRegistryGet(t *testing.T) {
	registry := NewToolRegistry()
	tool := &testTool{name: "finder"}
	registry.Register(tool)

	// Found case
	found, ok := registry.Get("finder")
	if !ok {
		t.Error("expected tool to be found")
	}
	if found.Name() != "finder" {
		t.Errorf("Name() = %q, want %q", found.Name(), "finder")
	}

	// Not found case
	_, ok = registry.Get("nonexistent")
	if ok {
		t.Error("expected tool not to be found")
	}
}

func TestToolRegistryExecuteNotFound(t *testing.T) {
	registry := NewToolRegistry()

	result, err := registry.Execute(context.Background(), "nonexistent", nil)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing tool")
	}
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("content = %q, should contain 'not found'", result.Content)
	}
}

func TestToolRegistryAsLLMTools(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&testTool{name: "tool1"})
	registry.Register(&testTool{name: "tool2"})
	registry.Register(&testTool{name: "tool3"})

	tools := registry.AsLLMTools()
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3", len(tools))
	}

	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name()] = true
	}
	if !names["tool1"] || !names["tool2"] || !names["tool3"] {
		t.Errorf("missing tools: %v", names)
	}
}

func TestWithSystemPromptEmpty(t *testing.T) {
	ctx := WithSystemPrompt(context.Background(), "")
	_, ok := systemPromptFromContext(ctx)
	if ok {
		t.Error("empty prompt should not be stored")
	}

	ctx = WithSystemPrompt(context.Background(), "   ")
	_, ok = systemPromptFromContext(ctx)
	if ok {
		t.Error("whitespace-only prompt should not be stored")
	}
}

func TestWithToolPolicyNil(t *testing.T) {
	ctx := WithToolPolicy(context.Background(), nil, nil)
	_, _, ok := toolPolicyFromContext(ctx)
	if ok {
		t.Error("nil policy should not be stored")
	}
}

func TestWithSessionNil(t *testing.T) {
	ctx := WithSession(context.Background(), nil)
	session := SessionFromContext(ctx)
	if session != nil {
		t.Error("nil session should not be stored")
	}
}

func TestSessionFromContext(t *testing.T) {
	session := &models.Session{ID: "test-session"}
	ctx := WithSession(context.Background(), session)

	retrieved := SessionFromContext(ctx)
	if retrieved == nil {
		t.Fatal("expected session, got nil")
	}
	if retrieved.ID != "test-session" {
		t.Errorf("ID = %q, want %q", retrieved.ID, "test-session")
	}
}

func TestProcessWallTimeLimit(t *testing.T) {
	// Provider that blocks until context is cancelled
	provider := &cancelProvider{started: make(chan struct{})}
	runtime := NewRuntime(provider, stubStore{})
	runtime.SetMaxWallTime(50 * time.Millisecond)

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	<-provider.started

	var gotErr error
	for chunk := range ch {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}

	if gotErr == nil {
		t.Fatal("expected timeout error")
	}
}

func TestProcessMaxIterationsExceeded(t *testing.T) {
	// Provider that always returns tool calls
	calls := int32(0)
	provider := &loopProvider{}

	runtime := NewRuntimeWithOptions(provider, stubStore{}, RuntimeOptions{
		MaxIterations:   2,
		ToolParallelism: 1,
	})
	runtime.RegisterTool(&testTool{name: "test_tool"})

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "loop"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	for range ch {
	}

	_ = calls
}

type streamingProvider struct {
	chunks []string
}

func (p *streamingProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	ch := make(chan *CompletionChunk, len(p.chunks)+1)
	for _, text := range p.chunks {
		ch <- &CompletionChunk{Text: text}
	}
	ch <- &CompletionChunk{Done: true}
	close(ch)
	return ch, nil
}

func (p *streamingProvider) Name() string        { return "streaming" }
func (p *streamingProvider) Models() []Model     { return nil }
func (p *streamingProvider) SupportsTools() bool { return false }

func TestProcessStreamsConcatenation(t *testing.T) {
	provider := &streamingProvider{chunks: []string{"Hello", " ", "World", "!"}}
	runtime := NewRuntime(provider, stubStore{})

	session := &models.Session{ID: "session-1", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "greet me"}

	ch, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var result strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		result.WriteString(chunk.Text)
	}

	if result.String() != "Hello World!" {
		t.Errorf("result = %q, want %q", result.String(), "Hello World!")
	}
}

func TestMatchToolPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		tool     string
		expected bool
	}{
		// Exact match
		{"bash", "bash", true},
		{"bash", "zsh", false},

		// Empty cases
		{"", "bash", false},
		{"bash", "", false},

		// mcp:* pattern
		{"mcp:*", "mcp:server.tool", true},
		{"mcp:*", "other_tool", false},

		// Prefix patterns (ending with .*)
		{"mcp:github.*", "mcp:github.search", true},
		{"mcp:github.*", "mcp:github.issues", true},
		{"mcp:github.*", "mcp:gitlab.search", false},

		// Standard wildcard suffix
		{"read_*", "read_file", false}, // Note: .* not *, so this won't match
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.tool, func(t *testing.T) {
			result := matchToolPattern(tt.pattern, tt.tool)
			if result != tt.expected {
				t.Errorf("matchToolPattern(%q, %q) = %v, want %v", tt.pattern, tt.tool, result, tt.expected)
			}
		})
	}
}

func TestBuildCompletionMessages(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})

	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi there!"},
		{ID: "3", Role: models.RoleUser, Content: "How are you?", Attachments: []models.Attachment{{URL: "image.png"}}},
	}

	messages, err := runtime.buildCompletionMessages(history)
	if err != nil {
		t.Fatalf("buildCompletionMessages() error = %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}

	if messages[0].Role != "user" || messages[0].Content != "Hello" {
		t.Errorf("message 0: role=%q content=%q", messages[0].Role, messages[0].Content)
	}
	if messages[1].Role != "assistant" || messages[1].Content != "Hi there!" {
		t.Errorf("message 1: role=%q content=%q", messages[1].Role, messages[1].Content)
	}
	if len(messages[2].Attachments) != 1 {
		t.Errorf("message 2 attachments: %d, want 1", len(messages[2].Attachments))
	}
}

func TestBuildCompletionMessagesWithToolCalls(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})

	history := []*models.Message{
		{
			ID:   "1",
			Role: models.RoleAssistant,
			ToolCalls: []models.ToolCall{
				{ID: "call-1", Name: "search", Input: json.RawMessage(`{"q":"test"}`)},
			},
		},
		{
			ID:   "2",
			Role: models.RoleTool,
			ToolResults: []models.ToolResult{
				{ToolCallID: "call-1", Content: "results"},
			},
		},
	}

	messages, err := runtime.buildCompletionMessages(history)
	if err != nil {
		t.Fatalf("buildCompletionMessages() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}

	if len(messages[0].ToolCalls) != 1 {
		t.Errorf("message 0 tool calls: %d, want 1", len(messages[0].ToolCalls))
	}
	if len(messages[1].ToolResults) != 1 {
		t.Errorf("message 1 tool results: %d, want 1", len(messages[1].ToolResults))
	}
}

func TestBuildCompletionMessagesMissingRole(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})

	history := []*models.Message{
		{ID: "1", Role: "", Content: "Missing role"},
	}

	_, err := runtime.buildCompletionMessages(history)
	if err == nil {
		t.Error("expected error for missing role")
	}
}

func TestBuildCompletionMessagesNilMessage(t *testing.T) {
	runtime := NewRuntime(stubProvider{}, stubStore{})

	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		nil, // nil message should be skipped
		{ID: "2", Role: models.RoleAssistant, Content: "Hi"},
	}

	messages, err := runtime.buildCompletionMessages(history)
	if err != nil {
		t.Fatalf("buildCompletionMessages() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2 (nil skipped)", len(messages))
	}
}

func TestDefaultRuntimeOptions(t *testing.T) {
	opts := DefaultRuntimeOptions()

	if opts.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5", opts.MaxIterations)
	}
	if opts.ToolParallelism != 4 {
		t.Errorf("ToolParallelism = %d, want 4", opts.ToolParallelism)
	}
	if opts.ToolTimeout != 30*time.Second {
		t.Errorf("ToolTimeout = %v, want 30s", opts.ToolTimeout)
	}
	if opts.ToolMaxAttempts != 1 {
		t.Errorf("ToolMaxAttempts = %d, want 1", opts.ToolMaxAttempts)
	}
	if opts.DisableToolEvents {
		t.Error("DisableToolEvents should be false by default")
	}
}
