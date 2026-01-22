package tape

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ErrTapeExhausted indicates the tape has no more turns to replay.
var ErrTapeExhausted = errors.New("tape exhausted: no more turns to replay")

// ErrTapeMismatch indicates a mismatch between expected and actual requests.
var ErrTapeMismatch = errors.New("tape mismatch: request differs from recorded")

// ErrToolNotInTape indicates a tool call is not found in the tape.
var ErrToolNotInTape = errors.New("tool call not found in tape")

// ReplayMode controls how strictly the replayer matches requests.
type ReplayMode int

const (
	// ReplayStrict requires exact request matching
	ReplayStrict ReplayMode = iota

	// ReplayLoose ignores request differences and just returns recorded responses
	ReplayLoose
)

// Replayer replays a recorded tape for testing.
type Replayer struct {
	tape        *Tape
	mode        ReplayMode
	turnIdx     int
	toolRunIdx  map[int]int // turnIndex -> next tool run index for that turn
	mu          sync.Mutex
	mismatches  []Mismatch
}

// Mismatch records a difference between expected and actual values.
type Mismatch struct {
	TurnIndex int    `json:"turn_index"`
	Field     string `json:"field"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
}

// NewReplayer creates a replayer from a tape.
func NewReplayer(tape *Tape) *Replayer {
	return &Replayer{
		tape:       tape.Clone(), // Clone to avoid mutation
		mode:       ReplayLoose,
		turnIdx:    0,
		toolRunIdx: make(map[int]int),
	}
}

// WithMode sets the replay mode.
func (r *Replayer) WithMode(mode ReplayMode) *Replayer {
	r.mode = mode
	return r
}

// Complete implements LLMProvider, returning recorded responses.
func (r *Replayer) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	r.mu.Lock()

	if r.turnIdx >= len(r.tape.Turns) {
		r.mu.Unlock()
		return nil, ErrTapeExhausted
	}

	turn := r.tape.Turns[r.turnIdx]
	currentTurn := r.turnIdx
	r.turnIdx++
	r.mu.Unlock()

	// Check for mismatches in strict mode
	if r.mode == ReplayStrict {
		r.checkMismatches(currentTurn, req, turn.Request)
	}

	// Create channel and replay chunks
	out := make(chan *agent.CompletionChunk, len(turn.Chunks)+1)

	go func() {
		defer close(out)

		for _, chunk := range turn.Chunks {
			select {
			case <-ctx.Done():
				out <- &agent.CompletionChunk{Error: ctx.Err()}
				return
			case out <- &chunk:
			}
		}
	}()

	return out, nil
}

// checkMismatches compares requests and records any differences.
func (r *Replayer) checkMismatches(turnIndex int, actual, expected *agent.CompletionRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if actual.Model != expected.Model && expected.Model != "" {
		r.mismatches = append(r.mismatches, Mismatch{
			TurnIndex: turnIndex,
			Field:     "model",
			Expected:  expected.Model,
			Actual:    actual.Model,
		})
	}

	if len(actual.Messages) != len(expected.Messages) {
		r.mismatches = append(r.mismatches, Mismatch{
			TurnIndex: turnIndex,
			Field:     "message_count",
			Expected:  fmt.Sprintf("%d", len(expected.Messages)),
			Actual:    fmt.Sprintf("%d", len(actual.Messages)),
		})
	}
}

// Name implements LLMProvider.
func (r *Replayer) Name() string {
	return "replayer"
}

// Models implements LLMProvider.
func (r *Replayer) Models() []agent.Model {
	return []agent.Model{
		{ID: "replay", Name: "Tape Replay", ContextSize: 200000},
	}
}

// SupportsTools implements LLMProvider.
func (r *Replayer) SupportsTools() bool {
	return true
}

// Mismatches returns any recorded mismatches from strict mode.
func (r *Replayer) Mismatches() []Mismatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Mismatch{}, r.mismatches...)
}

// Reset resets the replayer to the beginning.
func (r *Replayer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turnIdx = 0
	r.toolRunIdx = make(map[int]int)
	r.mismatches = nil
}

// CurrentTurn returns the current turn index.
func (r *Replayer) CurrentTurn() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.turnIdx
}

// ReplayTool wraps a tool to return recorded results.
type ReplayTool struct {
	replayer  *Replayer
	name      string
	schema    json.RawMessage
}

// NewReplayTool creates a tool that returns recorded results.
func (r *Replayer) NewReplayTool(name string, schema json.RawMessage) *ReplayTool {
	return &ReplayTool{
		replayer: r,
		name:     name,
		schema:   schema,
	}
}

// Name implements Tool.
func (t *ReplayTool) Name() string {
	return t.name
}

// Description implements Tool.
func (t *ReplayTool) Description() string {
	return "Replay tool for testing"
}

// Schema implements Tool.
func (t *ReplayTool) Schema() json.RawMessage {
	return t.schema
}

// Execute implements Tool, returning recorded results.
func (t *ReplayTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	t.replayer.mu.Lock()
	defer t.replayer.mu.Unlock()

	// Find the current turn (the one we just processed)
	turnIndex := t.replayer.turnIdx - 1
	if turnIndex < 0 {
		turnIndex = 0
	}

	// Get the next tool run for this turn
	runs := t.replayer.tape.GetToolRuns(turnIndex)
	runIdx := t.replayer.toolRunIdx[turnIndex]

	if runIdx >= len(runs) {
		return nil, fmt.Errorf("%w: %s at turn %d", ErrToolNotInTape, t.name, turnIndex)
	}

	run := runs[runIdx]
	t.replayer.toolRunIdx[turnIndex] = runIdx + 1

	// Check tool name matches
	if run.Call.Name != t.name {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrTapeMismatch, run.Call.Name, t.name)
	}

	// Return recorded result
	if run.Error != "" {
		return nil, errors.New(run.Error)
	}

	return run.Result, nil
}

// ReplayToolRegistry creates tools from a tape for replay.
type ReplayToolRegistry struct {
	replayer *Replayer
	tools    map[string]*ReplayTool
}

// NewReplayToolRegistry creates a registry of replay tools from a tape.
func NewReplayToolRegistry(replayer *Replayer) *ReplayToolRegistry {
	registry := &ReplayToolRegistry{
		replayer: replayer,
		tools:    make(map[string]*ReplayTool),
	}

	// Discover tools from tape
	seen := make(map[string]bool)
	for _, run := range replayer.tape.ToolRuns {
		if !seen[run.Call.Name] {
			seen[run.Call.Name] = true
			registry.tools[run.Call.Name] = replayer.NewReplayTool(
				run.Call.Name,
				json.RawMessage(`{"type": "object"}`),
			)
		}
	}

	return registry
}

// Get returns a replay tool by name.
func (r *ReplayToolRegistry) Get(name string) (*ReplayTool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// All returns all replay tools.
func (r *ReplayToolRegistry) All() []*ReplayTool {
	tools := make([]*ReplayTool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// ToAgentTools converts replay tools to agent.Tool interface.
func (r *ReplayToolRegistry) ToAgentTools() []agent.Tool {
	tools := make([]agent.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// ToolCall is a helper to build tool calls for testing.
func ToolCall(id, name string, input any) models.ToolCall {
	data, _ := json.Marshal(input)
	return models.ToolCall{
		ID:    id,
		Name:  name,
		Input: data,
	}
}
