// Package tape provides recording and replay capabilities for agent conversations.
// This enables testing the agentic loop without making real LLM API calls.
package tape

import (
	"encoding/json"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Tape records a complete conversation with an agent.
type Tape struct {
	// Version of the tape format
	Version string `json:"version"`

	// CreatedAt is when the tape was recorded
	CreatedAt time.Time `json:"created_at"`

	// Model is the LLM model used
	Model string `json:"model,omitempty"`

	// SystemPrompt used for the conversation
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Turns contains each LLM request/response turn
	Turns []Turn `json:"turns"`

	// ToolRuns contains each tool execution
	ToolRuns []ToolRun `json:"tool_runs"`

	// Metadata holds arbitrary metadata
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Turn represents a single LLM turn (request + response).
type Turn struct {
	// Index is the 0-based turn number
	Index int `json:"index"`

	// Request is the completion request sent to the LLM
	Request *agent.CompletionRequest `json:"request"`

	// Chunks is the streamed response chunks
	Chunks []agent.CompletionChunk `json:"chunks"`

	// ToolCalls returned by the LLM in this turn
	ToolCalls []models.ToolCall `json:"tool_calls,omitempty"`

	// Text is the accumulated text response
	Text string `json:"text,omitempty"`

	// StopReason indicates why the turn ended
	StopReason string `json:"stop_reason,omitempty"`

	// Duration is how long the turn took
	Duration time.Duration `json:"duration"`
}

// ToolRun represents a single tool execution.
type ToolRun struct {
	// TurnIndex is the turn when this tool was called
	TurnIndex int `json:"turn_index"`

	// Call is the tool call from the LLM
	Call models.ToolCall `json:"call"`

	// Result is the tool execution result
	Result *agent.ToolResult `json:"result"`

	// Error is any error that occurred (as string for serialization)
	Error string `json:"error,omitempty"`

	// Duration is how long the tool took
	Duration time.Duration `json:"duration"`
}

// NewTape creates a new empty tape.
func NewTape() *Tape {
	return &Tape{
		Version:   "1.0",
		CreatedAt: time.Now(),
		Turns:     []Turn{},
		ToolRuns:  []ToolRun{},
		Metadata:  make(map[string]any),
	}
}

// AddTurn adds a turn to the tape.
func (t *Tape) AddTurn(turn Turn) {
	turn.Index = len(t.Turns)
	t.Turns = append(t.Turns, turn)
}

// AddToolRun adds a tool run to the tape.
func (t *Tape) AddToolRun(run ToolRun) {
	t.ToolRuns = append(t.ToolRuns, run)
}

// GetTurn returns the turn at the given index.
func (t *Tape) GetTurn(index int) (*Turn, bool) {
	if index < 0 || index >= len(t.Turns) {
		return nil, false
	}
	return &t.Turns[index], true
}

// GetToolRuns returns all tool runs for a given turn.
func (t *Tape) GetToolRuns(turnIndex int) []ToolRun {
	var runs []ToolRun
	for _, run := range t.ToolRuns {
		if run.TurnIndex == turnIndex {
			runs = append(runs, run)
		}
	}
	return runs
}

// TotalTurns returns the number of turns in the tape.
func (t *Tape) TotalTurns() int {
	return len(t.Turns)
}

// TotalToolRuns returns the number of tool runs in the tape.
func (t *Tape) TotalToolRuns() int {
	return len(t.ToolRuns)
}

// Marshal serializes the tape to JSON.
func (t *Tape) Marshal() ([]byte, error) {
	return json.MarshalIndent(t, "", "  ")
}

// Unmarshal deserializes a tape from JSON.
func Unmarshal(data []byte) (*Tape, error) {
	var tape Tape
	if err := json.Unmarshal(data, &tape); err != nil {
		return nil, err
	}
	return &tape, nil
}

// Clone creates a deep copy of the tape.
func (t *Tape) Clone() *Tape {
	data, err := t.Marshal()
	if err == nil {
		if clone, err := Unmarshal(data); err == nil {
			return clone
		}
	}
	clone := *t
	if t.Metadata != nil {
		clone.Metadata = make(map[string]any, len(t.Metadata))
		for k, v := range t.Metadata {
			clone.Metadata[k] = v
		}
	}
	clone.Turns = append([]Turn(nil), t.Turns...)
	clone.ToolRuns = append([]ToolRun(nil), t.ToolRuns...)
	return &clone
}

// Summary returns a brief summary of the tape contents.
func (t *Tape) Summary() TapeSummary {
	var totalChunks int
	var totalText int
	for _, turn := range t.Turns {
		totalChunks += len(turn.Chunks)
		totalText += len(turn.Text)
	}

	return TapeSummary{
		Version:      t.Version,
		CreatedAt:    t.CreatedAt,
		Model:        t.Model,
		TurnCount:    len(t.Turns),
		ToolRunCount: len(t.ToolRuns),
		TotalChunks:  totalChunks,
		TotalTextLen: totalText,
	}
}

// TapeSummary is a brief overview of a tape.
type TapeSummary struct {
	Version      string    `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	Model        string    `json:"model,omitempty"`
	TurnCount    int       `json:"turn_count"`
	ToolRunCount int       `json:"tool_run_count"`
	TotalChunks  int       `json:"total_chunks"`
	TotalTextLen int       `json:"total_text_len"`
}
