package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/policy"
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

	// MaxToolCalls limits the total tool calls per run (0 = unlimited)
	// Default: 0
	MaxToolCalls int

	// MaxWallTime limits total run duration (0 = no limit)
	// Default: 0
	MaxWallTime time.Duration

	// ExecutorConfig configures the parallel tool executor
	ExecutorConfig *ExecutorConfig

	// EnableBackpressure enables backpressure handling for slow tools
	// Default: true
	EnableBackpressure bool

	// StreamToolResults streams tool results as they complete
	// Default: true
	StreamToolResults bool

	// DisableToolEvents disables streaming ToolEvent chunks
	// Default: false
	DisableToolEvents bool

	// RequireApproval lists tool names/patterns that require approval.
	RequireApproval []string

	// ApprovalChecker evaluates approval policy for tool calls when set.
	ApprovalChecker *ApprovalChecker

	// ElevatedTools lists tool patterns eligible for elevated full bypass.
	ElevatedTools []string

	// AsyncTools lists tool names to execute asynchronously as jobs.
	AsyncTools []string

	// JobStore receives async tool job updates.
	JobStore jobs.Store

	// ToolResultGuard redacts tool results before persistence.
	ToolResultGuard ToolResultGuard

	// ToolEvents persists tool call/result events when set.
	ToolEvents ToolEventStore

	// BranchStore provides branch-aware storage operations
	// If nil, standard session history is used
	BranchStore sessions.BranchStore
}

// DefaultLoopConfig returns the default loop configuration.
func DefaultLoopConfig() *LoopConfig {
	return &LoopConfig{
		MaxIterations:      10,
		MaxTokens:          4096,
		MaxToolCalls:       0,
		MaxWallTime:        0,
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
	if cfg.MaxToolCalls < 0 {
		cfg.MaxToolCalls = 0
	}
	if cfg.MaxWallTime < 0 {
		cfg.MaxWallTime = 0
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

	jobSem chan struct{}
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
		jobSem:   make(chan struct{}, maxConcurrentJobs),
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
	TotalToolCalls  int
	Messages        []CompletionMessage
	PendingTools    []models.ToolCall
	ToolResults     []models.ToolResult
	AccumulatedText string
	LastError       error
	BranchID        string // Current branch for branch-aware loops
	AssistantMsgID  string
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

	runCtx := ctx
	var cancel context.CancelFunc
	if l.config.MaxWallTime > 0 {
		runCtx, cancel = context.WithTimeout(ctx, l.config.MaxWallTime)
	}
	runCtx = WithSession(runCtx, session)

	chunks := make(chan *ResponseChunk, processBufferSize)

	go func() {
		defer close(chunks)
		if cancel != nil {
			defer cancel()
		}

		state := &LoopState{
			Phase:     PhaseInit,
			Iteration: 0,
		}

		// Initialize: Load history and build initial messages
		if err := l.initializeState(runCtx, session, msg, state); err != nil {
			chunks <- &ResponseChunk{Error: &LoopError{
				Phase:     PhaseInit,
				Iteration: 0,
				Cause:     err,
			}}
			return
		}

		if err := l.persistInboundMessage(runCtx, session, msg, state.BranchID); err != nil {
			chunks <- &ResponseChunk{Error: &LoopError{
				Phase:     PhaseInit,
				Iteration: 0,
				Cause:     err,
			}}
			return
		}

		steeringQueue := SteeringQueueFromContext(runCtx)

		// Main loop
		for state.Iteration < l.config.MaxIterations {
			select {
			case <-runCtx.Done():
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     state.Phase,
					Iteration: state.Iteration,
					Cause:     runCtx.Err(),
				}}
				return
			default:
			}

			// Stream phase: Call LLM and collect response
			state.Phase = PhaseStream
			toolCalls, err := l.streamPhase(runCtx, state, chunks)
			if err != nil {
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     PhaseStream,
					Iteration: state.Iteration,
					Cause:     err,
				}}
				return
			}

			if l.config.MaxToolCalls > 0 && state.TotalToolCalls+len(toolCalls) > l.config.MaxToolCalls {
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     PhaseStream,
					Iteration: state.Iteration,
					Cause:     fmt.Errorf("tool calls exceed maximum of %d for run", l.config.MaxToolCalls),
				}}
				return
			}
			state.TotalToolCalls += len(toolCalls)

			assistantMsgID, err := l.persistAssistantMessage(runCtx, session, state, toolCalls)
			if err != nil {
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     PhaseStream,
					Iteration: state.Iteration,
					Cause:     err,
				}}
				return
			}
			state.AssistantMsgID = assistantMsgID

			l.persistToolCalls(runCtx, session, assistantMsgID, toolCalls)

			// If no tool calls, we're done (unless follow-ups are queued)
			if len(toolCalls) == 0 {
				l.addAssistantMessage(state, toolCalls)
				state.AccumulatedText = ""
				if steeringQueue != nil {
					if followUps := steeringQueue.GetFollowUpMessages(); len(followUps) > 0 {
						for _, followUp := range followUps {
							role := followUp.Role
							if role == "" {
								role = "user"
							}
							state.Messages = append(state.Messages, CompletionMessage{
								Role:        role,
								Content:     followUp.Content,
								Attachments: followUp.Attachments,
							})
						}
						state.Iteration++
						continue
					}
				}
				state.Phase = PhaseComplete
				return
			}

			// Execute tools phase
			state.Phase = PhaseExecuteTools
			state.PendingTools = toolCalls

			toolResults, err := l.executeToolsPhase(runCtx, session, state, chunks)
			if err != nil {
				chunks <- &ResponseChunk{Error: &LoopError{
					Phase:     PhaseExecuteTools,
					Iteration: state.Iteration,
					Cause:     err,
				}}
				return
			}

			if err := l.persistToolMessage(runCtx, session, state.BranchID, toolCalls, toolResults); err != nil {
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

			if steeringQueue != nil {
				if steeringMsgs := steeringQueue.GetSteeringMessages(); len(steeringMsgs) > 0 {
					skipRemaining := false
					for _, steering := range steeringMsgs {
						role := steering.Role
						if role == "" {
							role = "user"
						}
						state.Messages = append(state.Messages, CompletionMessage{
							Role:        role,
							Content:     steering.Content,
							Attachments: steering.Attachments,
						})
						if steering.SkipRemainingTools {
							skipRemaining = true
						}
					}
					if skipRemaining {
						state.Iteration++
						continue
					}
				}
			}

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
	tools := l.executor.registry.AsLLMTools()
	if resolver, toolPolicy, ok := toolPolicyFromContext(ctx); ok {
		tools = filterToolsByPolicy(resolver, toolPolicy, tools)
	}

	// Build completion request
	req := &CompletionRequest{
		Model:     l.defaultModel,
		System:    l.defaultSystem,
		Messages:  state.Messages,
		Tools:     tools,
		MaxTokens: l.config.MaxTokens,
	}

	// Apply context overrides
	if system, ok := systemPromptFromContext(ctx); ok {
		req.System = system
	}
	if model, ok := modelFromContext(ctx); ok {
		req.Model = model
	}
	if thinkingLevel := ThinkingLevelFromContext(ctx); thinkingLevel != ThinkingOff {
		budget := GetThinkingBudget(thinkingLevel)
		if budget > 0 {
			req.EnableThinking = true
			req.ThinkingBudgetTokens = budget
		}
	}

	// Call LLM (resolve API key if needed)
	completionCtx := ctx
	if resolver := APIKeyResolverFromContext(ctx); resolver != nil {
		resolvedKey, keyErr := resolver(ctx, l.provider.Name())
		if keyErr != nil {
			return nil, fmt.Errorf("API key resolution failed: %w", keyErr)
		}
		if resolvedKey != "" {
			completionCtx = WithResolvedAPIKey(ctx, resolvedKey)
		}
	}

	completion, err := l.provider.Complete(completionCtx, req)
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

		if chunk.ThinkingStart {
			chunks <- &ResponseChunk{ThinkingStart: true}
		}
		if chunk.Thinking != "" {
			chunks <- &ResponseChunk{Thinking: chunk.Thinking}
		}
		if chunk.ThinkingEnd {
			chunks <- &ResponseChunk{ThinkingEnd: true}
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
func (l *AgenticLoop) executeToolsPhase(ctx context.Context, session *models.Session, state *LoopState, chunks chan<- *ResponseChunk) ([]models.ToolResult, error) {
	if len(state.PendingTools) == 0 {
		return nil, nil
	}

	resolver, toolPolicy, hasPolicy := toolPolicyFromContext(ctx)
	approvalChecker := l.config.ApprovalChecker
	elevatedMode := ElevatedFromContext(ctx)

	results := make([]models.ToolResult, len(state.PendingTools))
	artifacts := make([][]Artifact, len(state.PendingTools))
	allowedCalls := make([]models.ToolCall, 0, len(state.PendingTools))
	allowedToOriginal := make([]int, 0, len(state.PendingTools))

	for i := range state.PendingTools {
		tc := state.PendingTools[i]

		l.emitToolEvent(chunks, &models.ToolEvent{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Stage:      models.ToolEventRequested,
			Input:      tc.Input,
		})

		if hasPolicy && !resolver.IsAllowed(toolPolicy, tc.Name) {
			res := models.ToolResult{
				ToolCallID: tc.ID,
				Content:    "tool not allowed: " + tc.Name,
				IsError:    true,
			}
			results[i] = res
			l.emitToolEvent(chunks, &models.ToolEvent{
				ToolCallID:   tc.ID,
				ToolName:     tc.Name,
				Stage:        models.ToolEventDenied,
				Error:        res.Content,
				PolicyReason: "tool not allowed by policy",
				FinishedAt:   time.Now(),
			})
			l.persistToolResult(ctx, session, state.AssistantMsgID, tc, res, resolver)
			continue
		}

		if approvalChecker != nil {
			decision, reason := approvalChecker.Check(ctx, session.AgentID, tc)
			if decision == ApprovalPending && elevatedMode == ElevatedFull && matchesToolPatterns(l.config.ElevatedTools, tc.Name, resolver) {
				decision = ApprovalAllowed
				reason = "elevated full"
			}
			switch decision {
			case ApprovalDenied:
				res := models.ToolResult{
					ToolCallID: tc.ID,
					Content:    "tool denied by approval policy: " + reason,
					IsError:    true,
				}
				results[i] = res
				l.emitToolEvent(chunks, &models.ToolEvent{
					ToolCallID:   tc.ID,
					ToolName:     tc.Name,
					Stage:        models.ToolEventDenied,
					Error:        res.Content,
					PolicyReason: reason,
					FinishedAt:   time.Now(),
				})
				l.persistToolResult(ctx, session, state.AssistantMsgID, tc, res, resolver)
				continue
			case ApprovalPending:
				var approvalID string
				if req, err := approvalChecker.CreateApprovalRequest(ctx, session.AgentID, session.ID, tc, reason); err == nil && req != nil {
					approvalID = req.ID
				}
				content := "approval required for tool: " + tc.Name
				if approvalID != "" {
					content = fmt.Sprintf("%s (id: %s)", content, approvalID)
				}
				res := models.ToolResult{
					ToolCallID: tc.ID,
					Content:    content,
					IsError:    true,
				}
				results[i] = res
				l.emitToolEvent(chunks, &models.ToolEvent{
					ToolCallID:   tc.ID,
					ToolName:     tc.Name,
					Stage:        models.ToolEventApprovalRequired,
					Error:        res.Content,
					PolicyReason: reason,
					FinishedAt:   time.Now(),
				})
				l.persistToolResult(ctx, session, state.AssistantMsgID, tc, res, resolver)
				continue
			}
		} else if matchesToolPatterns(l.config.RequireApproval, tc.Name, resolver) {
			if elevatedMode == ElevatedFull && matchesToolPatterns(l.config.ElevatedTools, tc.Name, resolver) {
				// bypass
			} else {
				res := models.ToolResult{
					ToolCallID: tc.ID,
					Content:    "approval required for tool: " + tc.Name,
					IsError:    true,
				}
				results[i] = res
				l.emitToolEvent(chunks, &models.ToolEvent{
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Stage:      models.ToolEventApprovalRequired,
					Error:      res.Content,
					FinishedAt: time.Now(),
				})
				l.persistToolResult(ctx, session, state.AssistantMsgID, tc, res, resolver)
				continue
			}
		}

		if l.isAsyncTool(tc.Name, resolver) && l.config.JobStore != nil {
			res := l.queueAsyncJob(tc)
			results[i] = res
			l.emitToolEvent(chunks, &models.ToolEvent{
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Stage:      models.ToolEventSucceeded,
				Output:     res.Content,
				FinishedAt: time.Now(),
			})
			l.persistToolResult(ctx, session, state.AssistantMsgID, tc, res, resolver)
			continue
		}

		allowedCalls = append(allowedCalls, tc)
		allowedToOriginal = append(allowedToOriginal, i)
	}

	for _, idx := range allowedToOriginal {
		tc := state.PendingTools[idx]
		l.emitToolEvent(chunks, &models.ToolEvent{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Stage:      models.ToolEventStarted,
			StartedAt:  time.Now(),
		})
	}

	execResults := l.executor.ExecuteAll(ctx, allowedCalls)
	for i, r := range execResults {
		origIdx := allowedToOriginal[i]
		tc := state.PendingTools[origIdx]
		if r == nil {
			results[origIdx] = models.ToolResult{
				ToolCallID: tc.ID,
				Content:    "tool execution failed",
				IsError:    true,
			}
			l.emitToolEvent(chunks, &models.ToolEvent{
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Stage:      models.ToolEventFailed,
				Error:      results[origIdx].Content,
				FinishedAt: time.Now(),
			})
		} else if r.Error != nil {
			results[origIdx] = models.ToolResult{
				ToolCallID: r.ToolCallID,
				Content:    r.Error.Error(),
				IsError:    true,
			}
			l.emitToolEvent(chunks, &models.ToolEvent{
				ToolCallID: r.ToolCallID,
				ToolName:   tc.Name,
				Stage:      models.ToolEventFailed,
				Error:      results[origIdx].Content,
				FinishedAt: time.Now(),
			})
		} else if r.Result != nil {
			attachments := artifactsToAttachments(r.Result.Artifacts)
			results[origIdx] = models.ToolResult{
				ToolCallID:  r.ToolCallID,
				Content:     r.Result.Content,
				IsError:     r.Result.IsError,
				Attachments: attachments,
			}
			artifacts[origIdx] = r.Result.Artifacts
			stage := models.ToolEventSucceeded
			if r.Result.IsError {
				stage = models.ToolEventFailed
			}
			l.emitToolEvent(chunks, &models.ToolEvent{
				ToolCallID: r.ToolCallID,
				ToolName:   tc.Name,
				Stage:      stage,
				Output:     r.Result.Content,
				FinishedAt: time.Now(),
			})
		}
		l.persistToolResult(ctx, session, state.AssistantMsgID, tc, results[origIdx], resolver)
	}

	for i := range results {
		if results[i].ToolCallID == "" && i < len(state.PendingTools) {
			results[i].ToolCallID = state.PendingTools[i].ID
		}
	}

	if l.config.StreamToolResults {
		for i := range results {
			chunk := &ResponseChunk{ToolResult: &results[i]}
			if len(artifacts[i]) > 0 {
				chunk.Artifacts = artifacts[i]
			}
			chunks <- chunk
		}
	}

	return results, nil
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

func (l *AgenticLoop) persistAssistantMessage(ctx context.Context, session *models.Session, state *LoopState, toolCalls []models.ToolCall) (string, error) {
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
	if err := l.appendMessage(ctx, session, state.BranchID, assistantMsg); err != nil {
		return "", err
	}
	return assistantMsg.ID, nil
}

func (l *AgenticLoop) persistToolMessage(ctx context.Context, session *models.Session, branchID string, toolCalls []models.ToolCall, toolResults []models.ToolResult) error {
	if len(toolResults) == 0 {
		return nil
	}
	resolver, _, _ := toolPolicyFromContext(ctx)
	persistResults := guardToolResults(l.config.ToolResultGuard, toolCalls, toolResults, resolver)
	resultsForStorage := make([]models.ToolResult, len(persistResults))
	for i := range persistResults {
		resultsForStorage[i] = persistResults[i]
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

func (l *AgenticLoop) emitToolEvent(chunks chan<- *ResponseChunk, event *models.ToolEvent) {
	if l.config.DisableToolEvents || event == nil {
		return
	}
	chunks <- &ResponseChunk{ToolEvent: event}
}

func (l *AgenticLoop) persistToolCalls(ctx context.Context, session *models.Session, assistantMsgID string, toolCalls []models.ToolCall) {
	if l.config.ToolEvents == nil || session == nil {
		return
	}
	for i := range toolCalls {
		tc := toolCalls[i]
		_ = l.config.ToolEvents.AddToolCall(ctx, session.ID, assistantMsgID, &tc)
	}
}

func (l *AgenticLoop) persistToolResult(ctx context.Context, session *models.Session, assistantMsgID string, tc models.ToolCall, res models.ToolResult, resolver *policy.Resolver) {
	if l.config.ToolEvents == nil || session == nil {
		return
	}
	guarded := guardToolResult(l.config.ToolResultGuard, tc.Name, res, resolver)
	_ = l.config.ToolEvents.AddToolResult(ctx, session.ID, assistantMsgID, &tc, &guarded)
}

func (l *AgenticLoop) isAsyncTool(name string, resolver *policy.Resolver) bool {
	return matchesToolPatterns(l.config.AsyncTools, name, resolver)
}

func (l *AgenticLoop) queueAsyncJob(tc models.ToolCall) models.ToolResult {
	job := &jobs.Job{
		ID:         uuid.NewString(),
		ToolName:   tc.Name,
		ToolCallID: tc.ID,
		Status:     jobs.StatusQueued,
		CreatedAt:  time.Now(),
	}
	if l.config.JobStore != nil {
		_ = l.config.JobStore.Create(context.Background(), job)
	}

	payload, err := json.Marshal(map[string]any{
		"job_id": job.ID,
		"status": job.Status,
	})
	res := models.ToolResult{
		ToolCallID: tc.ID,
		IsError:    false,
	}
	if err != nil {
		res.Content = fmt.Sprintf("failed to encode job payload: %v", err)
		res.IsError = true
	} else {
		res.Content = string(payload)
	}

	if l.config.JobStore != nil {
		if l.jobSem == nil {
			go l.runToolJob(tc, job)
		} else {
			select {
			case l.jobSem <- struct{}{}:
				go func() {
					defer func() { <-l.jobSem }()
					l.runToolJob(tc, job)
				}()
			default:
				go l.runToolJob(tc, job)
			}
		}
	}

	return res
}

func (l *AgenticLoop) runToolJob(tc models.ToolCall, job *jobs.Job) {
	if job == nil || l.config.JobStore == nil {
		return
	}
	ctx := context.Background()
	job.Status = jobs.StatusRunning
	job.StartedAt = time.Now()
	_ = l.config.JobStore.Update(ctx, job)

	execResult := l.executor.Execute(ctx, tc)
	if execResult.Error != nil {
		job.Status = jobs.StatusFailed
		job.Error = execResult.Error.Error()
		job.FinishedAt = time.Now()
		_ = l.config.JobStore.Update(ctx, job)
		return
	}

	if execResult.Result != nil {
		res := models.ToolResult{
			ToolCallID:  tc.ID,
			Content:     execResult.Result.Content,
			IsError:     execResult.Result.IsError,
			Attachments: artifactsToAttachments(execResult.Result.Artifacts),
		}
		if res.IsError {
			job.Status = jobs.StatusFailed
			job.Error = res.Content
		} else {
			job.Status = jobs.StatusSucceeded
			job.Result = &res
		}
	} else {
		job.Status = jobs.StatusFailed
		job.Error = "tool execution failed"
	}

	job.FinishedAt = time.Now()
	_ = l.config.JobStore.Update(ctx, job)
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
