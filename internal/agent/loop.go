package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// LoopConfig configures the agentic loop behavior including iteration limits,
// token budgets, and tool execution settings.
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

func sanitizeLoopConfig(config *LoopConfig) *LoopConfig {
	if config == nil {
		return DefaultLoopConfig()
	}
	cfg := *config
	defaults := DefaultLoopConfig()
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaults.MaxIterations
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = defaults.MaxTokens
	}
	if cfg.ExecutorConfig == nil {
		cfg.ExecutorConfig = defaults.ExecutorConfig
	}
	return &cfg
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

// NewAgenticLoop creates a new agentic loop with the given provider, tool registry, and session store.
// If config is nil, DefaultLoopConfig is used.
func NewAgenticLoop(provider LLMProvider, registry *ToolRegistry, sessions sessions.Store, config *LoopConfig) *AgenticLoop {
	config = sanitizeLoopConfig(config)
	if registry == nil {
		registry = NewToolRegistry()
	}

	executor := NewExecutor(registry, config.ExecutorConfig)
	if !config.EnableBackpressure {
		executor.sem = nil
	}

	return &AgenticLoop{
		provider: provider,
		executor: executor,
		sessions: sessions,
		config:   config,
	}
}

// SetDefaultModel sets the default model used when requests do not specify one.
func (l *AgenticLoop) SetDefaultModel(model string) {
	l.defaultModel = model
}

// SetDefaultSystem sets the default system prompt used when requests do not specify one.
func (l *AgenticLoop) SetDefaultSystem(system string) {
	l.defaultSystem = system
}

// ConfigureTool sets per-tool configuration overrides for timeout, retry, and priority.
func (l *AgenticLoop) ConfigureTool(name string, config *ToolConfig) {
	l.executor.ConfigureTool(name, config)
}

// LoopState tracks the current state of an agentic loop execution including
// phase, iteration count, accumulated messages, and pending tool operations.
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

// Run executes the agentic loop and streams results through a channel.
// The channel is closed when the loop completes or an error occurs.
func (l *AgenticLoop) Run(ctx context.Context, session *models.Session, msg *models.Message) (<-chan *ResponseChunk, error) {
	if l.provider == nil {
		return nil, ErrNoProvider
	}
	if l.config == nil {
		return nil, errors.New("loop config is nil")
	}
	if session == nil {
		return nil, errors.New("session is nil")
	}
	if msg == nil {
		return nil, errors.New("message is nil")
	}
	if l.sessions == nil && (l.config == nil || l.config.BranchStore == nil) {
		return nil, errors.New("no session store configured")
	}

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

		if err := l.persistInboundMessage(ctx, session, msg, state.BranchID); err != nil {
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

			if err := l.persistAssistantMessage(ctx, session, state, toolCalls); err != nil {
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     PhaseStream,
					Iteration: state.Iteration,
					Cause:     err,
				}}
				return
			}

			// If no tool calls, we're done
			if len(toolCalls) == 0 {
				l.addAssistantMessage(state, toolCalls)
				state.AccumulatedText = ""
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

			if err := l.persistToolMessage(ctx, session, state.BranchID, toolResults); err != nil {
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
	if l.config.BranchStore != nil {
		if msg.BranchID != "" {
			state.BranchID = msg.BranchID
		} else {
			branch, branchErr := l.config.BranchStore.EnsurePrimaryBranch(ctx, session.ID)
			if branchErr != nil {
				return fmt.Errorf("failed to ensure primary branch: %w", branchErr)
			}
			state.BranchID = branch.ID
			msg.BranchID = branch.ID
		}
		history, err = l.config.BranchStore.GetBranchHistory(ctx, state.BranchID, 50)
		if err != nil {
			return fmt.Errorf("failed to get branch history: %w", err)
		}
	} else {
		// Standard session history
		history, err = l.sessions.GetHistory(ctx, session.ID, 50)
		if err != nil {
			return fmt.Errorf("failed to get history: %w", err)
		}
	}

	history = repairTranscript(history)

	// Build messages from history
	state.Messages = make([]CompletionMessage, 0, len(history)+1)
	for _, m := range history {
		state.Messages = append(state.Messages, CompletionMessage{
			Role:        string(m.Role),
			Content:     m.Content,
			ToolCalls:   m.ToolCalls,
			ToolResults: m.ToolResults,
			Attachments: m.Attachments,
		})
	}

	// Add the new message
	role := msg.Role
	if role == "" {
		role = models.RoleUser
	}
	state.Messages = append(state.Messages, CompletionMessage{
		Role:        string(role),
		Content:     msg.Content,
		Attachments: msg.Attachments,
	})

	return nil
}

// RunWithBranch executes the agentic loop on a specific conversation branch.
// The branchID is set on the message before processing.
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
	var textBuilder strings.Builder

	for chunk := range completion {
		if chunk.Error != nil {
			return nil, chunk.Error
		}

		if chunk.Text != "" {
			if textBuilder.Len()+len(chunk.Text) > MaxResponseTextSize {
				return nil, fmt.Errorf("response text exceeds maximum size of %d bytes", MaxResponseTextSize)
			}
			textBuilder.WriteString(chunk.Text)
			chunks <- &ResponseChunk{Text: chunk.Text}
		}

		if chunk.ToolCall != nil {
			if len(toolCalls) >= MaxToolCallsPerIteration {
				return nil, fmt.Errorf("tool calls exceed maximum of %d per iteration", MaxToolCallsPerIteration)
			}
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
	}

	// Store accumulated text for message history
	state.AccumulatedText = textBuilder.String()

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
			attachments := artifactsToAttachments(r.Result.Artifacts)
			toolResults[i] = models.ToolResult{
				ToolCallID:  r.ToolCallID,
				Content:     r.Result.Content,
				IsError:     r.Result.IsError,
				Attachments: attachments,
			}
		}

		// Stream tool result if configured
		if l.config.StreamToolResults {
			chunk := &ResponseChunk{ToolResult: &toolResults[i]}
			// Include artifacts from tool execution
			if r.Result != nil && len(r.Result.Artifacts) > 0 {
				chunk.Artifacts = r.Result.Artifacts
			}
			chunks <- chunk
		}
	}

	return toolResults, nil
}

// continuePhase adds the assistant message with tool calls and tool results to history.
func (l *AgenticLoop) continuePhase(state *LoopState, toolCalls []models.ToolCall, toolResults []models.ToolResult) {
	// Add assistant message with tool calls
	l.addAssistantMessage(state, toolCalls)

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

func (l *AgenticLoop) addAssistantMessage(state *LoopState, toolCalls []models.ToolCall) {
	state.Messages = append(state.Messages, CompletionMessage{
		Role:      "assistant",
		Content:   state.AccumulatedText,
		ToolCalls: toolCalls,
	})
}

func (l *AgenticLoop) persistInboundMessage(ctx context.Context, session *models.Session, msg *models.Message, branchID string) error {
	if msg == nil {
		return errors.New("message is nil")
	}
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.SessionID == "" {
		msg.SessionID = session.ID
	}
	if msg.Channel == "" {
		msg.Channel = session.Channel
	}
	if msg.ChannelID == "" {
		msg.ChannelID = session.ChannelID
	}
	if msg.Role == "" {
		msg.Role = models.RoleUser
	}
	if msg.Direction == "" {
		msg.Direction = models.DirectionInbound
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	if branchID != "" {
		msg.BranchID = branchID
	}
	return l.appendMessage(ctx, session, branchID, msg)
}

func (l *AgenticLoop) persistAssistantMessage(ctx context.Context, session *models.Session, state *LoopState, toolCalls []models.ToolCall) error {
	assistantMsg := &models.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Channel:   session.Channel,
		ChannelID: session.ChannelID,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   state.AccumulatedText,
		ToolCalls: toolCalls,
		CreatedAt: time.Now(),
	}
	if state.BranchID != "" {
		assistantMsg.BranchID = state.BranchID
	}
	return l.appendMessage(ctx, session, state.BranchID, assistantMsg)
}

func (l *AgenticLoop) persistToolMessage(ctx context.Context, session *models.Session, branchID string, toolResults []models.ToolResult) error {
	if len(toolResults) == 0 {
		return nil
	}
	resultsForStorage := make([]models.ToolResult, len(toolResults))
	for i := range toolResults {
		resultsForStorage[i] = toolResults[i]
		resultsForStorage[i].Attachments = nil
	}
	toolMsg := &models.Message{
		ID:          uuid.NewString(),
		SessionID:   session.ID,
		Channel:     session.Channel,
		ChannelID:   session.ChannelID,
		Direction:   models.DirectionInbound,
		Role:        models.RoleTool,
		ToolResults: resultsForStorage,
		CreatedAt:   time.Now(),
	}
	if branchID != "" {
		toolMsg.BranchID = branchID
	}
	return l.appendMessage(ctx, session, branchID, toolMsg)
}

func (l *AgenticLoop) appendMessage(ctx context.Context, session *models.Session, branchID string, msg *models.Message) error {
	if msg == nil {
		return nil
	}
	branch := strings.TrimSpace(branchID)
	if branch == "" {
		branch = strings.TrimSpace(msg.BranchID)
	}
	if l.config != nil && l.config.BranchStore != nil {
		if branch == "" {
			primary, err := l.config.BranchStore.EnsurePrimaryBranch(ctx, session.ID)
			if err != nil {
				return err
			}
			branch = primary.ID
		}
		msg.BranchID = branch
		return l.config.BranchStore.AppendMessageToBranch(ctx, session.ID, branch, msg)
	}
	if l.sessions == nil {
		return errors.New("no session store configured")
	}
	return l.sessions.AppendMessage(ctx, session.ID, msg)
}

// AgenticRuntime wraps the AgenticLoop to provide a Runtime-compatible interface.
// This allows the loop to be used interchangeably with the standard Runtime.
type AgenticRuntime struct {
	loop *AgenticLoop
}

// NewAgenticRuntime creates a new agentic runtime wrapping an AgenticLoop.
func NewAgenticRuntime(provider LLMProvider, sessions sessions.Store, config *LoopConfig) *AgenticRuntime {
	registry := NewToolRegistry()
	loop := NewAgenticLoop(provider, registry, sessions, config)

	return &AgenticRuntime{
		loop: loop,
	}
}

// SetDefaultModel configures the fallback model used when not specified in requests.
func (r *AgenticRuntime) SetDefaultModel(model string) {
	r.loop.SetDefaultModel(model)
}

// SetSystemPrompt configures the fallback system prompt used when not specified in requests.
func (r *AgenticRuntime) SetSystemPrompt(system string) {
	r.loop.SetDefaultSystem(system)
}

// RegisterTool adds a tool to the runtime's tool registry.
func (r *AgenticRuntime) RegisterTool(tool Tool) {
	r.loop.executor.registry.Register(tool)
}

// ConfigureTool sets per-tool configuration for timeout, retry, and priority.
func (r *AgenticRuntime) ConfigureTool(name string, config *ToolConfig) {
	r.loop.ConfigureTool(name, config)
}

// Process handles an incoming message using the agentic loop and streams results.
func (r *AgenticRuntime) Process(ctx context.Context, session *models.Session, msg *models.Message) (<-chan *ResponseChunk, error) {
	return r.loop.Run(ctx, session, msg)
}

// ExecutorMetrics returns a snapshot of metrics from the tool executor.
func (r *AgenticRuntime) ExecutorMetrics() *ExecutorMetricsSnapshot {
	return r.loop.executor.Metrics()
}
