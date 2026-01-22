package agent

import (
	"context"
	"fmt"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// LoopConfig configures the agentic loop behavior.
type LoopConfig struct {
	// MaxIterations limits the number of tool use iterations
	// Default: 10
	MaxIterations int

	// MaxTokens is the default max tokens for LLM responses
	// Default: 4096
	MaxTokens int

	// ExecutorConfig configures the parallel tool executor
	ExecutorConfig *ExecutorConfig

	// EnableBackpressure enables backpressure handling for slow tools
	// Default: true
	EnableBackpressure bool

	// StreamToolResults streams tool results as they complete
	// Default: true
	StreamToolResults bool

	// BranchStore provides branch-aware storage operations
	// If nil, standard session history is used
	BranchStore sessions.BranchStore
}

// DefaultLoopConfig returns the default loop configuration.
func DefaultLoopConfig() *LoopConfig {
	return &LoopConfig{
		MaxIterations:      10,
		MaxTokens:          4096,
		ExecutorConfig:     DefaultExecutorConfig(),
		EnableBackpressure: true,
		StreamToolResults:  true,
	}
}

// AgenticLoop implements a multi-turn agentic conversation loop.
//
// The loop operates as a state machine:
//
//	┌──────────────────────────────────────────────────────────────┐
//	│                                                              │
//	│   ┌─────────┐     ┌──────────┐     ┌───────────────────┐   │
//	│   │  Init   │────▶│  Stream  │────▶│  Execute Tools    │   │
//	│   └─────────┘     └──────────┘     └───────────────────┘   │
//	│                          │                    │             │
//	│                          │                    │             │
//	│                          ▼                    │             │
//	│                   ┌──────────┐                │             │
//	│                   │ Complete │◀───────────────┘             │
//	│                   └──────────┘     (no tools or max iter)   │
//	│                                                              │
//	│                   ┌──────────┐                               │
//	│                   │ Continue │◀───────────────┐              │
//	│                   └──────────┘     (has tool results)       │
//	│                          │                                   │
//	│                          └───────────▶ Stream                │
//	│                                                              │
//	└──────────────────────────────────────────────────────────────┘
type AgenticLoop struct {
	provider LLMProvider
	executor *Executor
	sessions sessions.Store
	config   *LoopConfig

	defaultModel  string
	defaultSystem string
}

// NewAgenticLoop creates a new agentic loop.
func NewAgenticLoop(provider LLMProvider, registry *ToolRegistry, sessions sessions.Store, config *LoopConfig) *AgenticLoop {
	if config == nil {
		config = DefaultLoopConfig()
	}

	executor := NewExecutor(registry, config.ExecutorConfig)

	return &AgenticLoop{
		provider: provider,
		executor: executor,
		sessions: sessions,
		config:   config,
	}
}

// SetDefaultModel sets the default model for completions.
func (l *AgenticLoop) SetDefaultModel(model string) {
	l.defaultModel = model
}

// SetDefaultSystem sets the default system prompt.
func (l *AgenticLoop) SetDefaultSystem(system string) {
	l.defaultSystem = system
}

// ConfigureTool sets per-tool configuration.
func (l *AgenticLoop) ConfigureTool(name string, config *ToolConfig) {
	l.executor.ConfigureTool(name, config)
}

// LoopState tracks the current state of an agentic loop execution.
type LoopState struct {
	Phase           LoopPhase
	Iteration       int
	Messages        []CompletionMessage
	PendingTools    []models.ToolCall
	ToolResults     []models.ToolResult
	AccumulatedText string
	LastError       error
	BranchID        string // Current branch for branch-aware loops
}

// Run executes the agentic loop and streams results.
func (l *AgenticLoop) Run(ctx context.Context, session *models.Session, msg *models.Message) (<-chan *ResponseChunk, error) {
	chunks := make(chan *ResponseChunk, processBufferSize)

	go func() {
		defer close(chunks)

		state := &LoopState{
			Phase:     PhaseInit,
			Iteration: 0,
		}

		// Initialize: Load history and build initial messages
		if err := l.initializeState(ctx, session, msg, state); err != nil {
			chunks <- &ResponseChunk{Error: &LoopError{
				Phase:     PhaseInit,
				Iteration: 0,
				Cause:     err,
			}}
			return
		}

		// Main loop
		for state.Iteration < l.config.MaxIterations {
			select {
			case <-ctx.Done():
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     state.Phase,
					Iteration: state.Iteration,
					Cause:     ctx.Err(),
				}}
				return
			default:
			}

			// Stream phase: Call LLM and collect response
			state.Phase = PhaseStream
			toolCalls, err := l.streamPhase(ctx, state, chunks)
			if err != nil {
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     PhaseStream,
					Iteration: state.Iteration,
					Cause:     err,
				}}
				return
			}

			// If no tool calls, we're done
			if len(toolCalls) == 0 {
				state.Phase = PhaseComplete
				return
			}

			// Execute tools phase
			state.Phase = PhaseExecuteTools
			state.PendingTools = toolCalls

			toolResults, err := l.executeToolsPhase(ctx, state, chunks)
			if err != nil {
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     PhaseExecuteTools,
					Iteration: state.Iteration,
					Cause:     err,
				}}
				return
			}

			// Continue phase: Add tool results to messages
			state.Phase = PhaseContinue
			l.continuePhase(state, toolCalls, toolResults)

			state.Iteration++
		}

		// Max iterations reached
		chunks <- &ResponseChunk{Error: &LoopError{
			Phase:     state.Phase,
			Iteration: state.Iteration,
			Cause:     ErrMaxIterations,
			Message:   fmt.Sprintf("reached max iterations: %d", l.config.MaxIterations),
		}}
	}()

	return chunks, nil
}

// initializeState loads conversation history and sets up initial state.
func (l *AgenticLoop) initializeState(ctx context.Context, session *models.Session, msg *models.Message, state *LoopState) error {
	var history []*models.Message
	var err error

	// Use branch-aware history if branch store is configured and message has a branch
	if l.config.BranchStore != nil && msg.BranchID != "" {
		state.BranchID = msg.BranchID
		history, err = l.config.BranchStore.GetBranchHistory(ctx, msg.BranchID, 50)
		if err != nil {
			return fmt.Errorf("failed to get branch history: %w", err)
		}
	} else if l.config.BranchStore != nil {
		// Try to get primary branch for session
		branch, branchErr := l.config.BranchStore.GetPrimaryBranch(ctx, session.ID)
		if branchErr == nil {
			state.BranchID = branch.ID
			history, err = l.config.BranchStore.GetBranchHistory(ctx, branch.ID, 50)
			if err != nil {
				return fmt.Errorf("failed to get branch history: %w", err)
			}
		} else {
			// Fall back to standard session history
			history, err = l.sessions.GetHistory(ctx, session.ID, 50)
			if err != nil {
				return fmt.Errorf("failed to get history: %w", err)
			}
		}
	} else {
		// Standard session history
		history, err = l.sessions.GetHistory(ctx, session.ID, 50)
		if err != nil {
			return fmt.Errorf("failed to get history: %w", err)
		}
	}

	// Build messages from history
	state.Messages = make([]CompletionMessage, 0, len(history)+1)
	for _, m := range history {
		state.Messages = append(state.Messages, CompletionMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	// Add the new message
	state.Messages = append(state.Messages, CompletionMessage{
		Role:        string(msg.Role),
		Content:     msg.Content,
		Attachments: msg.Attachments,
	})

	return nil
}

// RunWithBranch executes the agentic loop on a specific branch.
func (l *AgenticLoop) RunWithBranch(ctx context.Context, session *models.Session, msg *models.Message, branchID string) (<-chan *ResponseChunk, error) {
	// Set branch ID on message for initializeState
	msg.BranchID = branchID
	return l.Run(ctx, session, msg)
}

// streamPhase streams from the LLM and collects any tool calls.
func (l *AgenticLoop) streamPhase(ctx context.Context, state *LoopState, chunks chan<- *ResponseChunk) ([]models.ToolCall, error) {
	// Build completion request
	req := &CompletionRequest{
		Model:     l.defaultModel,
		System:    l.defaultSystem,
		Messages:  state.Messages,
		Tools:     l.executor.registry.AsLLMTools(),
		MaxTokens: l.config.MaxTokens,
	}

	// Apply context overrides
	if system, ok := systemPromptFromContext(ctx); ok {
		req.System = system
	}

	// Call LLM
	completion, err := l.provider.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	// Collect response
	var toolCalls []models.ToolCall
	var textBuilder string

	for chunk := range completion {
		if chunk.Error != nil {
			return nil, chunk.Error
		}

		if chunk.Text != "" {
			textBuilder += chunk.Text
			chunks <- &ResponseChunk{Text: chunk.Text}
		}

		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
	}

	// Store accumulated text for message history
	state.AccumulatedText = textBuilder

	return toolCalls, nil
}

// executeToolsPhase executes pending tool calls in parallel.
func (l *AgenticLoop) executeToolsPhase(ctx context.Context, state *LoopState, chunks chan<- *ResponseChunk) ([]models.ToolResult, error) {
	if len(state.PendingTools) == 0 {
		return nil, nil
	}

	// Execute all tools in parallel
	results := l.executor.ExecuteAll(ctx, state.PendingTools)

	// Convert to tool results and stream each result
	toolResults := make([]models.ToolResult, len(results))

	for i, r := range results {
		if r.Error != nil {
			toolResults[i] = models.ToolResult{
				ToolCallID: r.ToolCallID,
				Content:    r.Error.Error(),
				IsError:    true,
			}
		} else if r.Result != nil {
			toolResults[i] = models.ToolResult{
				ToolCallID: r.ToolCallID,
				Content:    r.Result.Content,
				IsError:    r.Result.IsError,
			}
		}

		// Stream tool result if configured
		if l.config.StreamToolResults {
			chunks <- &ResponseChunk{ToolResult: &toolResults[i]}
		}
	}

	return toolResults, nil
}

// continuePhase adds the assistant message with tool calls and tool results to history.
func (l *AgenticLoop) continuePhase(state *LoopState, toolCalls []models.ToolCall, toolResults []models.ToolResult) {
	// Add assistant message with tool calls
	state.Messages = append(state.Messages, CompletionMessage{
		Role:      "assistant",
		Content:   state.AccumulatedText,
		ToolCalls: toolCalls,
	})

	// Add tool results message
	state.Messages = append(state.Messages, CompletionMessage{
		Role:        "tool",
		ToolResults: toolResults,
	})

	// Clear accumulated state
	state.AccumulatedText = ""
	state.PendingTools = nil
	state.ToolResults = nil
}

// AgenticRuntime wraps the AgenticLoop with the Runtime interface for compatibility.
type AgenticRuntime struct {
	loop *AgenticLoop
}

// NewAgenticRuntime creates a new agentic runtime.
func NewAgenticRuntime(provider LLMProvider, sessions sessions.Store, config *LoopConfig) *AgenticRuntime {
	registry := NewToolRegistry()
	loop := NewAgenticLoop(provider, registry, sessions, config)

	return &AgenticRuntime{
		loop: loop,
	}
}

// SetDefaultModel configures the fallback model.
func (r *AgenticRuntime) SetDefaultModel(model string) {
	r.loop.SetDefaultModel(model)
}

// SetSystemPrompt configures the fallback system prompt.
func (r *AgenticRuntime) SetSystemPrompt(system string) {
	r.loop.SetDefaultSystem(system)
}

// RegisterTool adds a tool to the runtime.
func (r *AgenticRuntime) RegisterTool(tool Tool) {
	r.loop.executor.registry.Register(tool)
}

// ConfigureTool sets per-tool configuration.
func (r *AgenticRuntime) ConfigureTool(name string, config *ToolConfig) {
	r.loop.ConfigureTool(name, config)
}

// Process handles an incoming message using the agentic loop.
func (r *AgenticRuntime) Process(ctx context.Context, session *models.Session, msg *models.Message) (<-chan *ResponseChunk, error) {
	return r.loop.Run(ctx, session, msg)
}

// ExecutorMetrics returns metrics from the tool executor.
func (r *AgenticRuntime) ExecutorMetrics() *ExecutorMetricsSnapshot {
	return r.loop.executor.Metrics()
}
