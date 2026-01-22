package tape

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestTape_Basic(t *testing.T) {
	tape := NewTape()

	if tape.Version != "1.0" {
		t.Errorf("Version = %q, want %q", tape.Version, "1.0")
	}

	if tape.TotalTurns() != 0 {
		t.Errorf("TotalTurns = %d, want 0", tape.TotalTurns())
	}
}

func TestTape_AddTurn(t *testing.T) {
	tape := NewTape()

	tape.AddTurn(Turn{
		Text:       "Hello, world!",
		StopReason: "end_turn",
		Duration:   time.Second,
	})

	if tape.TotalTurns() != 1 {
		t.Errorf("TotalTurns = %d, want 1", tape.TotalTurns())
	}

	turn, ok := tape.GetTurn(0)
	if !ok {
		t.Fatal("should get turn 0")
	}
	if turn.Text != "Hello, world!" {
		t.Errorf("Text = %q, want %q", turn.Text, "Hello, world!")
	}
	if turn.Index != 0 {
		t.Errorf("Index = %d, want 0", turn.Index)
	}
}

func TestTape_AddToolRun(t *testing.T) {
	tape := NewTape()

	tape.AddToolRun(ToolRun{
		TurnIndex: 0,
		Call: models.ToolCall{
			ID:    "call-1",
			Name:  "test_tool",
			Input: json.RawMessage(`{"key": "value"}`),
		},
		Result:   &agent.ToolResult{Content: "result"},
		Duration: 100 * time.Millisecond,
	})

	if tape.TotalToolRuns() != 1 {
		t.Errorf("TotalToolRuns = %d, want 1", tape.TotalToolRuns())
	}

	runs := tape.GetToolRuns(0)
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0].Call.Name != "test_tool" {
		t.Errorf("Name = %q, want %q", runs[0].Call.Name, "test_tool")
	}
}

func TestTape_MarshalUnmarshal(t *testing.T) {
	tape := NewTape()
	tape.Model = "claude-3-5-sonnet"
	tape.SystemPrompt = "You are helpful."

	tape.AddTurn(Turn{
		Text:       "Test response",
		StopReason: "end_turn",
	})

	tape.AddToolRun(ToolRun{
		TurnIndex: 0,
		Call: models.ToolCall{
			ID:   "call-1",
			Name: "search",
		},
		Result: &agent.ToolResult{Content: "found it"},
	})

	data, err := tape.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	restored, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored.Model != tape.Model {
		t.Errorf("Model = %q, want %q", restored.Model, tape.Model)
	}
	if restored.TotalTurns() != tape.TotalTurns() {
		t.Errorf("TotalTurns = %d, want %d", restored.TotalTurns(), tape.TotalTurns())
	}
	if restored.TotalToolRuns() != tape.TotalToolRuns() {
		t.Errorf("TotalToolRuns = %d, want %d", restored.TotalToolRuns(), tape.TotalToolRuns())
	}
}

func TestTape_Summary(t *testing.T) {
	tape := NewTape()
	tape.Model = "gpt-4o"

	tape.AddTurn(Turn{
		Text: "Response 1",
		Chunks: []agent.CompletionChunk{
			{Text: "Res"},
			{Text: "ponse 1"},
		},
	})
	tape.AddTurn(Turn{
		Text: "Response 2",
		Chunks: []agent.CompletionChunk{
			{Text: "Response 2"},
		},
	})

	summary := tape.Summary()

	if summary.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", summary.TurnCount)
	}
	if summary.TotalChunks != 3 {
		t.Errorf("TotalChunks = %d, want 3", summary.TotalChunks)
	}
	if summary.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", summary.Model, "gpt-4o")
	}
}

// mockProvider implements LLMProvider for testing
type mockProvider struct {
	responses [][]agent.CompletionChunk
	callCount int
}

func (m *mockProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	ch := make(chan *agent.CompletionChunk, 10)

	go func() {
		defer close(ch)
		if m.callCount < len(m.responses) {
			for _, chunk := range m.responses[m.callCount] {
				ch <- &chunk
			}
		}
		m.callCount++
	}()

	return ch, nil
}

func (m *mockProvider) Name() string              { return "mock" }
func (m *mockProvider) Models() []agent.Model     { return nil }
func (m *mockProvider) SupportsTools() bool       { return true }

func TestRecorder_RecordsResponses(t *testing.T) {
	provider := &mockProvider{
		responses: [][]agent.CompletionChunk{
			{{Text: "Hello "}, {Text: "world!"}},
		},
	}

	recorder := NewRecorder(provider)
	ch, err := recorder.Complete(context.Background(), &agent.CompletionRequest{
		Model: "test-model",
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	// Consume the channel
	var text string
	for chunk := range ch {
		text += chunk.Text
	}

	if text != "Hello world!" {
		t.Errorf("text = %q, want %q", text, "Hello world!")
	}

	tape := recorder.Tape()
	if tape.TotalTurns() != 1 {
		t.Errorf("TotalTurns = %d, want 1", tape.TotalTurns())
	}

	turn, _ := tape.GetTurn(0)
	if turn.Text != "Hello world!" {
		t.Errorf("recorded text = %q, want %q", turn.Text, "Hello world!")
	}
}

func TestReplayer_ReplaysResponses(t *testing.T) {
	// Create a tape with recorded responses
	tape := NewTape()
	tape.AddTurn(Turn{
		Chunks: []agent.CompletionChunk{
			{Text: "Replayed "},
			{Text: "response"},
		},
		Text: "Replayed response",
	})

	replayer := NewReplayer(tape)
	ch, err := replayer.Complete(context.Background(), &agent.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	var text string
	for chunk := range ch {
		text += chunk.Text
	}

	if text != "Replayed response" {
		t.Errorf("text = %q, want %q", text, "Replayed response")
	}
}

func TestReplayer_TapeExhausted(t *testing.T) {
	tape := NewTape()
	tape.AddTurn(Turn{Text: "Only one"})

	replayer := NewReplayer(tape)

	// First call succeeds
	_, err := replayer.Complete(context.Background(), &agent.CompletionRequest{})
	if err != nil {
		t.Fatalf("First Complete failed: %v", err)
	}

	// Second call should fail
	_, err = replayer.Complete(context.Background(), &agent.CompletionRequest{})
	if err != ErrTapeExhausted {
		t.Errorf("err = %v, want ErrTapeExhausted", err)
	}
}

func TestReplayer_StrictMode(t *testing.T) {
	tape := NewTape()
	tape.AddTurn(Turn{
		Request: &agent.CompletionRequest{
			Model: "expected-model",
		},
		Text: "response",
	})

	replayer := NewReplayer(tape).WithMode(ReplayStrict)

	// Call with different model
	ch, _ := replayer.Complete(context.Background(), &agent.CompletionRequest{
		Model: "different-model",
	})

	// Drain the channel
	for range ch {
	}

	mismatches := replayer.Mismatches()
	if len(mismatches) == 0 {
		t.Error("expected mismatches in strict mode")
	}

	found := false
	for _, m := range mismatches {
		if m.Field == "model" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected model mismatch")
	}
}

func TestReplayTool(t *testing.T) {
	tape := NewTape()
	tape.AddTurn(Turn{Text: "response"})
	tape.AddToolRun(ToolRun{
		TurnIndex: 0,
		Call: models.ToolCall{
			Name:  "search",
			Input: json.RawMessage(`{"query": "test"}`),
		},
		Result: &agent.ToolResult{Content: "found it"},
	})

	replayer := NewReplayer(tape)
	// Simulate having processed turn 0
	replayer.Complete(context.Background(), &agent.CompletionRequest{})
	for range make(chan struct{}) {
		break // Immediately break, just to advance the replayer
	}

	tool := replayer.NewReplayTool("search", json.RawMessage(`{}`))
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Content != "found it" {
		t.Errorf("Content = %q, want %q", result.Content, "found it")
	}
}

func TestReplayToolRegistry(t *testing.T) {
	tape := NewTape()
	tape.AddToolRun(ToolRun{TurnIndex: 0, Call: models.ToolCall{Name: "tool_a"}})
	tape.AddToolRun(ToolRun{TurnIndex: 0, Call: models.ToolCall{Name: "tool_b"}})
	tape.AddToolRun(ToolRun{TurnIndex: 1, Call: models.ToolCall{Name: "tool_a"}})

	replayer := NewReplayer(tape)
	registry := NewReplayToolRegistry(replayer)

	if len(registry.All()) != 2 {
		t.Errorf("got %d tools, want 2 unique", len(registry.All()))
	}

	if _, ok := registry.Get("tool_a"); !ok {
		t.Error("should have tool_a")
	}
	if _, ok := registry.Get("tool_b"); !ok {
		t.Error("should have tool_b")
	}
}
