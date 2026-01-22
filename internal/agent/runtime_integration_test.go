package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	agentctx "github.com/haasonsaas/nexus/internal/agent/context"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// =============================================================================
// Mock Provider for Multi-Turn Conversations
// =============================================================================

// multiTurnProvider simulates an LLM that can make tool calls and respond.
type multiTurnProvider struct {
	mu        sync.Mutex
	responses []multiTurnResponse
	callCount int
}

type multiTurnResponse struct {
	text      string
	toolCalls []models.ToolCall
	err       error
}

func (p *multiTurnProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	ch := make(chan *CompletionChunk, 10)

	p.mu.Lock()
	idx := p.callCount
	p.callCount++
	p.mu.Unlock()

	go func() {
		defer close(ch)

		if idx >= len(p.responses) {
			// Default: just return "done" with no tool calls
			ch <- &CompletionChunk{Text: "done"}
			ch <- &CompletionChunk{Done: true}
			return
		}

		resp := p.responses[idx]

		if resp.err != nil {
			ch <- &CompletionChunk{Error: resp.err}
			return
		}

		if resp.text != "" {
			ch <- &CompletionChunk{Text: resp.text}
		}

		for i := range resp.toolCalls {
			tc := resp.toolCalls[i]
			ch <- &CompletionChunk{ToolCall: &tc}
		}

		ch <- &CompletionChunk{Done: true}
	}()

	return ch, nil
}

func (p *multiTurnProvider) Name() string         { return "multi-turn" }
func (p *multiTurnProvider) Models() []Model      { return nil }
func (p *multiTurnProvider) SupportsTools() bool  { return true }

// =============================================================================
// In-Memory Session Store for Integration Tests
// =============================================================================

type memoryStore struct {
	mu       sync.Mutex
	sessions map[string]*models.Session
	messages map[string][]*models.Message
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		sessions: make(map[string]*models.Session),
		messages: make(map[string][]*models.Message),
	}
}

func (s *memoryStore) Create(ctx context.Context, session *models.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *memoryStore) Get(ctx context.Context, id string) (*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id], nil
}

func (s *memoryStore) Update(ctx context.Context, session *models.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *memoryStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	delete(s.messages, id)
	return nil
}

func (s *memoryStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	return nil, nil
}

func (s *memoryStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	return nil, nil
}

func (s *memoryStore) List(ctx context.Context, agentID string, opts sessions.ListOptions) ([]*models.Session, error) {
	return nil, nil
}

func (s *memoryStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], msg)
	return nil
}

func (s *memoryStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.messages[sessionID]
	if len(msgs) > limit {
		return msgs[len(msgs)-limit:], nil
	}
	return msgs, nil
}

func (s *memoryStore) getMessages(sessionID string) []*models.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[sessionID]
}

// =============================================================================
// Integration Test Tool
// =============================================================================

type integrationTool struct {
	name        string
	execFunc    func(ctx context.Context, params json.RawMessage) (*ToolResult, error)
	execCount   int
	mu          sync.Mutex
}

func (t *integrationTool) Name() string             { return t.name }
func (t *integrationTool) Description() string      { return "integration test tool" }
func (t *integrationTool) Schema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }

func (t *integrationTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	t.mu.Lock()
	t.execCount++
	t.mu.Unlock()
	if t.execFunc != nil {
		return t.execFunc(ctx, params)
	}
	return &ToolResult{Content: "ok"}, nil
}

func (t *integrationTool) getExecCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.execCount
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestProcess_SingleTurn_NoTools(t *testing.T) {
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			{text: "Hello! How can I help you?"},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "Hi"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var text string
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		text += chunk.Text
	}

	if text != "Hello! How can I help you?" {
		t.Errorf("text = %q, want %q", text, "Hello! How can I help you?")
	}

	// Verify messages were persisted
	msgs := store.getMessages("test-session")
	if len(msgs) != 2 { // user + assistant
		t.Errorf("got %d messages, want 2", len(msgs))
	}
}

func TestProcess_SingleToolCall(t *testing.T) {
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			// First call: LLM requests a tool call
			{
				text: "Let me search for that.",
				toolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "search", Input: json.RawMessage(`{"query":"golang"}`)},
				},
			},
			// Second call: LLM provides final response after tool result
			{text: "I found Go is a programming language."},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)

	searchTool := &integrationTool{
		name: "search",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "Go is a programming language created at Google."}, nil
		},
	}
	runtime.RegisterTool(searchTool)

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "What is Go?"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var text string
	var toolResults []*models.ToolResult
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		text += chunk.Text
		if chunk.ToolResult != nil {
			toolResults = append(toolResults, chunk.ToolResult)
		}
	}

	// Tool should have executed
	if searchTool.getExecCount() != 1 {
		t.Errorf("tool exec count = %d, want 1", searchTool.getExecCount())
	}

	// Should have received tool result
	if len(toolResults) != 1 {
		t.Errorf("got %d tool results, want 1", len(toolResults))
	}

	// Final text should include both parts
	if text != "Let me search for that.I found Go is a programming language." {
		t.Errorf("text = %q", text)
	}

	// Verify persistence: user + assistant(with tool call) + tool + assistant
	msgs := store.getMessages("test-session")
	if len(msgs) != 4 {
		t.Errorf("got %d messages, want 4", len(msgs))
	}
}

func TestProcess_MultipleToolCalls_SingleIteration(t *testing.T) {
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			{
				text: "Searching multiple sources...",
				toolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "search_web", Input: json.RawMessage(`{}`)},
					{ID: "tc-2", Name: "search_docs", Input: json.RawMessage(`{}`)},
				},
			},
			{text: "Combined results..."},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)

	webTool := &integrationTool{name: "search_web"}
	docsTool := &integrationTool{name: "search_docs"}
	runtime.RegisterTool(webTool)
	runtime.RegisterTool(docsTool)

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "Search everything"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var toolResults int
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		if chunk.ToolResult != nil {
			toolResults++
		}
	}

	// Both tools should execute
	if webTool.getExecCount() != 1 {
		t.Errorf("web tool exec count = %d, want 1", webTool.getExecCount())
	}
	if docsTool.getExecCount() != 1 {
		t.Errorf("docs tool exec count = %d, want 1", docsTool.getExecCount())
	}

	// Should get 2 tool results
	if toolResults != 2 {
		t.Errorf("got %d tool results, want 2", toolResults)
	}
}

func TestProcess_MultipleIterations(t *testing.T) {
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			// Iteration 1: First tool call
			{
				text: "First step...",
				toolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "step1", Input: json.RawMessage(`{}`)},
				},
			},
			// Iteration 2: Second tool call based on first result
			{
				text: "Second step...",
				toolCalls: []models.ToolCall{
					{ID: "tc-2", Name: "step2", Input: json.RawMessage(`{}`)},
				},
			},
			// Iteration 3: Final response
			{text: "All done!"},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)

	step1Tool := &integrationTool{name: "step1"}
	step2Tool := &integrationTool{name: "step2"}
	runtime.RegisterTool(step1Tool)
	runtime.RegisterTool(step2Tool)

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "Do the thing"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var toolResults int
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		if chunk.ToolResult != nil {
			toolResults++
		}
	}

	// Both tools should execute (one per iteration)
	if step1Tool.getExecCount() != 1 {
		t.Errorf("step1 exec count = %d, want 1", step1Tool.getExecCount())
	}
	if step2Tool.getExecCount() != 1 {
		t.Errorf("step2 exec count = %d, want 1", step2Tool.getExecCount())
	}

	// Verify persistence: user + (assistant+tool) * 2 + assistant
	msgs := store.getMessages("test-session")
	// user(1) + assistant(2) + tool(3) + assistant(4) + tool(5) + assistant(6)
	if len(msgs) != 6 {
		t.Errorf("got %d messages, want 6", len(msgs))
	}
}

func TestProcess_MaxIterations(t *testing.T) {
	// Provider always returns tool calls (infinite loop)
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			{toolCalls: []models.ToolCall{{ID: "tc-1", Name: "loop", Input: json.RawMessage(`{}`)}}},
			{toolCalls: []models.ToolCall{{ID: "tc-2", Name: "loop", Input: json.RawMessage(`{}`)}}},
			{toolCalls: []models.ToolCall{{ID: "tc-3", Name: "loop", Input: json.RawMessage(`{}`)}}},
			{toolCalls: []models.ToolCall{{ID: "tc-4", Name: "loop", Input: json.RawMessage(`{}`)}}},
			{toolCalls: []models.ToolCall{{ID: "tc-5", Name: "loop", Input: json.RawMessage(`{}`)}}},
			{toolCalls: []models.ToolCall{{ID: "tc-6", Name: "loop", Input: json.RawMessage(`{}`)}}},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)
	runtime.SetMaxIterations(3)

	loopTool := &integrationTool{name: "loop"}
	runtime.RegisterTool(loopTool)

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "Loop forever"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var gotError error
	for chunk := range chunks {
		if chunk.Error != nil {
			gotError = chunk.Error
		}
	}

	// Should hit max iterations error
	if gotError == nil {
		t.Fatal("expected max iterations error")
	}

	// Should have executed exactly 3 times (maxIters)
	if loopTool.getExecCount() != 3 {
		t.Errorf("tool exec count = %d, want 3", loopTool.getExecCount())
	}
}

func TestProcess_ToolError_ContinuesLoop(t *testing.T) {
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			{
				toolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "failing", Input: json.RawMessage(`{}`)},
				},
			},
			{text: "The tool failed, but I can still respond."},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)

	failingTool := &integrationTool{
		name: "failing",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "tool execution failed", IsError: true}, nil
		},
	}
	runtime.RegisterTool(failingTool)

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "Try the tool"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var text string
	var toolResults []*models.ToolResult
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}
		text += chunk.Text
		if chunk.ToolResult != nil {
			toolResults = append(toolResults, chunk.ToolResult)
		}
	}

	// Tool result should indicate error
	if len(toolResults) != 1 || !toolResults[0].IsError {
		t.Error("expected tool result with IsError=true")
	}

	// Loop should continue and produce final response
	if text != "The tool failed, but I can still respond." {
		t.Errorf("text = %q", text)
	}
}

func TestProcess_ConcurrentToolExecution(t *testing.T) {
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			{
				toolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "slow1", Input: json.RawMessage(`{}`)},
					{ID: "tc-2", Name: "slow2", Input: json.RawMessage(`{}`)},
					{ID: "tc-3", Name: "slow3", Input: json.RawMessage(`{}`)},
				},
			},
			{text: "All done"},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)
	runtime.SetToolExecConfig(ToolExecConfig{
		Concurrency:    3, // All can run in parallel
		PerToolTimeout: 5 * time.Second,
	})

	var execStart sync.Map

	makeTool := func(name string) *integrationTool {
		return &integrationTool{
			name: name,
			execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
				execStart.Store(name, time.Now())
				time.Sleep(50 * time.Millisecond)
				return &ToolResult{Content: name + " done"}, nil
			},
		}
	}

	runtime.RegisterTool(makeTool("slow1"))
	runtime.RegisterTool(makeTool("slow2"))
	runtime.RegisterTool(makeTool("slow3"))

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "Run all tools"}

	start := time.Now()
	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
	}
	elapsed := time.Since(start)

	// If running concurrently, should complete in ~50ms not ~150ms
	if elapsed > 150*time.Millisecond {
		t.Errorf("elapsed %v, tools may not have run concurrently", elapsed)
	}
}

func TestProcess_ContextPacking(t *testing.T) {
	var lastMsgCount int
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			{text: "Response"},
		},
	}

	// Wrap to capture message count
	wrappedProvider := &messageCountingProvider{
		inner:    provider,
		msgCount: &lastMsgCount,
	}

	store := newMemoryStore()
	// Pre-populate history with many messages
	for i := 0; i < 100; i++ {
		store.AppendMessage(context.Background(), "test-session", &models.Message{
			ID:        fmt.Sprintf("msg-%d", i),
			SessionID: "test-session",
			Role:      models.RoleUser,
			Content:   fmt.Sprintf("Message %d content", i),
			CreatedAt: time.Now(),
		})
	}

	runtime := NewRuntime(wrappedProvider, store)
	runtime.SetPackOptions(&agentctx.PackOptions{
		MaxMessages:        10, // Only allow 10 messages
		MaxChars:           50000,
		MaxToolResultChars: 6000,
		IncludeSummary:     true,
	})

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "New message"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
	}

	// Should have packed to <= 10 messages despite 100+ in history
	if lastMsgCount > 10 {
		t.Errorf("message count = %d, expected <= 10 after packing", lastMsgCount)
	}
}

// messageCountingProvider wraps a provider to count messages
type messageCountingProvider struct {
	inner    LLMProvider
	msgCount *int
}

func (p *messageCountingProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	*p.msgCount = len(req.Messages)
	return p.inner.Complete(ctx, req)
}

func (p *messageCountingProvider) Name() string        { return p.inner.Name() }
func (p *messageCountingProvider) Models() []Model     { return p.inner.Models() }
func (p *messageCountingProvider) SupportsTools() bool { return p.inner.SupportsTools() }

func TestProcess_LifecycleEvents(t *testing.T) {
	provider := &multiTurnProvider{
		responses: []multiTurnResponse{
			{
				toolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "test_tool", Input: json.RawMessage(`{}`)},
				},
			},
			{text: "Done"},
		},
	}
	store := newMemoryStore()
	runtime := NewRuntime(provider, store)

	testTool := &integrationTool{name: "test_tool"}
	runtime.RegisterTool(testTool)

	session := &models.Session{ID: "test-session", Channel: models.ChannelTelegram}
	msg := &models.Message{Role: models.RoleUser, Content: "Test"}

	chunks, err := runtime.Process(context.Background(), session, msg)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	var events []*models.RuntimeEvent
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
		if chunk.Event != nil {
			events = append(events, chunk.Event)
		}
	}

	// Should have lifecycle events
	eventTypes := make(map[models.RuntimeEventType]int)
	for _, e := range events {
		eventTypes[e.Type]++
	}

	// Check for expected event types
	expectedTypes := []models.RuntimeEventType{
		models.EventIterationStart,
		models.EventThinkingStart,
		models.EventThinkingEnd,
		models.EventToolStarted,
		models.EventToolCompleted,
		models.EventIterationEnd,
	}

	for _, et := range expectedTypes {
		if eventTypes[et] == 0 {
			t.Errorf("missing event type: %s", et)
		}
	}
}
