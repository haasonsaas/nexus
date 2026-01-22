package tape

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
)

// Recorder wraps a provider and tools to record all interactions.
type Recorder struct {
	provider agent.LLMProvider
	tape     *Tape
	mu       sync.Mutex
	turnIdx  int
}

// NewRecorder creates a new recorder wrapping the given provider.
func NewRecorder(provider agent.LLMProvider) *Recorder {
	tape := NewTape()
	tape.Metadata["provider"] = provider.Name()

	return &Recorder{
		provider: provider,
		tape:     tape,
		turnIdx:  0,
	}
}

// WithModel sets the model in the tape metadata.
func (r *Recorder) WithModel(model string) *Recorder {
	r.tape.Model = model
	return r
}

// WithSystemPrompt sets the system prompt in the tape.
func (r *Recorder) WithSystemPrompt(system string) *Recorder {
	r.tape.SystemPrompt = system
	return r
}

// Complete implements LLMProvider, recording the interaction.
func (r *Recorder) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	r.mu.Lock()
	turnIndex := r.turnIdx
	r.turnIdx++
	r.mu.Unlock()

	start := time.Now()

	// Call the underlying provider
	upstream, err := r.provider.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	// Create buffered channel for recording
	out := make(chan *agent.CompletionChunk, 10)

	go func() {
		defer close(out)

		turn := Turn{
			Index:   turnIndex,
			Request: req,
			Chunks:  []agent.CompletionChunk{},
		}

		var textBuilder string
		var toolCalls []interface{}

		for chunk := range upstream {
			// Record the chunk
			turn.Chunks = append(turn.Chunks, *chunk)

			// Track text
			if chunk.Text != "" {
				textBuilder += chunk.Text
			}

			// Track tool calls
			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, chunk.ToolCall)
			}

			// Forward to consumer
			out <- chunk
		}

		turn.Text = textBuilder
		turn.Duration = time.Since(start)

		// Determine stop reason
		if len(toolCalls) > 0 {
			turn.StopReason = "tool_use"
		} else {
			turn.StopReason = "end_turn"
		}

		// Record the turn
		r.mu.Lock()
		r.tape.AddTurn(turn)
		r.mu.Unlock()
	}()

	return out, nil
}

// Name implements LLMProvider.
func (r *Recorder) Name() string {
	return "recorder:" + r.provider.Name()
}

// Models implements LLMProvider.
func (r *Recorder) Models() []agent.Model {
	return r.provider.Models()
}

// SupportsTools implements LLMProvider.
func (r *Recorder) SupportsTools() bool {
	return r.provider.SupportsTools()
}

// RecordToolRun records a tool execution.
func (r *Recorder) RecordToolRun(turnIndex int, call interface{}, result *agent.ToolResult, err error, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Convert call to models.ToolCall if needed
	callData, _ := json.Marshal(call)
	var toolCall struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	json.Unmarshal(callData, &toolCall)

	run := ToolRun{
		TurnIndex: turnIndex,
		Call: struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}{
			ID:    toolCall.ID,
			Name:  toolCall.Name,
			Input: toolCall.Input,
		},
		Result:   result,
		Duration: duration,
	}

	if err != nil {
		run.Error = err.Error()
	}

	r.tape.AddToolRun(run)
}

// Tape returns the recorded tape.
func (r *Recorder) Tape() *Tape {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tape.Clone()
}

// Reset clears the recording and starts fresh.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tape = NewTape()
	r.tape.Metadata["provider"] = r.provider.Name()
	r.turnIdx = 0
}

// RecordingTool wraps a tool to record executions.
type RecordingTool struct {
	tool      agent.Tool
	recorder  *Recorder
	turnIndex int
}

// WrapTool creates a recording wrapper for a tool.
func (r *Recorder) WrapTool(tool agent.Tool, turnIndex int) *RecordingTool {
	return &RecordingTool{
		tool:      tool,
		recorder:  r,
		turnIndex: turnIndex,
	}
}

// Name implements Tool.
func (t *RecordingTool) Name() string {
	return t.tool.Name()
}

// Description implements Tool.
func (t *RecordingTool) Description() string {
	return t.tool.Description()
}

// Schema implements Tool.
func (t *RecordingTool) Schema() json.RawMessage {
	return t.tool.Schema()
}

// Execute implements Tool, recording the execution.
func (t *RecordingTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	start := time.Now()

	result, err := t.tool.Execute(ctx, params)

	call := struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}{
		Name:  t.tool.Name(),
		Input: params,
	}

	t.recorder.RecordToolRun(t.turnIndex, call, result, err, time.Since(start))

	return result, err
}
