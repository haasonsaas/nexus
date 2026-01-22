package multiagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Orchestrator manages multi-agent conversations, handling agent selection,
// handoffs, and context sharing between specialized agents.
//
// The orchestrator can operate in two modes:
//   - Supervisor mode: A central coordinator agent delegates to specialists
//   - Peer-to-peer mode: Agents hand off directly to each other
//
// Usage:
//
//	config := LoadConfig("agents.yaml")
//	orch := NewOrchestrator(config, provider, sessions)
//	orch.RegisterAgent(agent1)
//	orch.RegisterAgent(agent2)
//
//	chunks, _ := orch.Process(ctx, session, msg)
type Orchestrator struct {
	mu sync.RWMutex

	// config holds the multi-agent system configuration.
	config *MultiAgentConfig

	// agents maps agent IDs to their definitions.
	agents map[string]*AgentDefinition

	// runtimes maps agent IDs to their runtime instances.
	runtimes map[string]*agent.Runtime

	// provider is the default LLM provider.
	provider agent.LLMProvider

	// sessions is the session store.
	sessions sessions.Store

	// router handles agent selection and routing.
	router *Router

	// contextManager handles context sharing.
	contextManager *ContextManager

	// supervisor handles supervisor pattern coordination.
	supervisor *Supervisor

	// handoffTool is the tool that allows agents to request handoffs.
	handoffTool *HandoffTool

	// policyResolver resolves tool policies.
	policyResolver *policy.Resolver

	// eventCallback is called for orchestration events.
	eventCallback func(*OrchestratorEvent)
}

// OrchestratorEvent represents events in the orchestration lifecycle.
type OrchestratorEvent struct {
	// Type is the event type.
	Type OrchestratorEventType `json:"type"`

	// AgentID is the agent involved.
	AgentID string `json:"agent_id,omitempty"`

	// FromAgentID is the source agent for handoffs.
	FromAgentID string `json:"from_agent_id,omitempty"`

	// ToAgentID is the target agent for handoffs.
	ToAgentID string `json:"to_agent_id,omitempty"`

	// Message contains additional context.
	Message string `json:"message,omitempty"`

	// Timestamp when the event occurred.
	Timestamp time.Time `json:"timestamp"`
}

// OrchestratorEventType defines orchestrator event types.
type OrchestratorEventType string

const (
	// EventAgentSelected fires when an agent is selected for a message.
	EventAgentSelected OrchestratorEventType = "agent_selected"

	// EventHandoffInitiated fires when a handoff is requested.
	EventHandoffInitiated OrchestratorEventType = "handoff_initiated"

	// EventHandoffCompleted fires when a handoff succeeds.
	EventHandoffCompleted OrchestratorEventType = "handoff_completed"

	// EventHandoffFailed fires when a handoff fails.
	EventHandoffFailed OrchestratorEventType = "handoff_failed"

	// EventContextShared fires when context is shared between agents.
	EventContextShared OrchestratorEventType = "context_shared"

	// EventAgentError fires when an agent encounters an error.
	EventAgentError OrchestratorEventType = "agent_error"
)

// NewOrchestrator creates a new multi-agent orchestrator.
func NewOrchestrator(config *MultiAgentConfig, provider agent.LLMProvider, sessions sessions.Store) *Orchestrator {
	if config == nil {
		config = &MultiAgentConfig{
			DefaultContextMode: ContextFull,
			MaxHandoffDepth:    10,
			HandoffTimeout:     5 * time.Minute,
			EnablePeerHandoffs: true,
		}
	}

	orch := &Orchestrator{
		config:         config,
		agents:         make(map[string]*AgentDefinition),
		runtimes:       make(map[string]*agent.Runtime),
		provider:       provider,
		sessions:       sessions,
		policyResolver: policy.NewResolver(),
	}

	// Initialize context manager
	orch.contextManager = NewContextManager(orch)

	// Initialize router
	orch.router = NewRouter(orch)

	// Initialize handoff tool (will be added to agents)
	orch.handoffTool = NewHandoffTool(orch)

	// Initialize supervisor if configured
	if config.SupervisorAgentID != "" {
		orch.supervisor = NewSupervisor(orch, config.SupervisorAgentID)
	}

	// Register configured agents
	for i := range config.Agents {
		if err := orch.RegisterAgent(&config.Agents[i]); err != nil {
			panic(fmt.Sprintf("failed to register agent %q: %v", config.Agents[i].ID, err))
		}
	}

	return orch
}

// RegisterAgent adds an agent to the orchestrator.
func (o *Orchestrator) RegisterAgent(def *AgentDefinition) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if def == nil {
		return fmt.Errorf("agent definition cannot be nil")
	}
	if def.ID == "" {
		return fmt.Errorf("agent ID cannot be empty")
	}

	// Clone to avoid external mutations
	agentDef := def.Clone()

	// Create a runtime for this agent
	runtime := agent.NewRuntime(o.provider, o.sessions)

	// Configure the runtime
	if agentDef.SystemPrompt != "" {
		runtime.SetSystemPrompt(agentDef.SystemPrompt)
	}
	if agentDef.Model != "" {
		runtime.SetDefaultModel(agentDef.Model)
	}
	if agentDef.MaxIterations > 0 {
		runtime.SetMaxIterations(agentDef.MaxIterations)
	}

	// Register the handoff tool for this agent (if peer handoffs enabled)
	if o.config.EnablePeerHandoffs {
		runtime.RegisterTool(o.handoffTool)
	}

	// Store the agent
	o.agents[agentDef.ID] = agentDef
	o.runtimes[agentDef.ID] = runtime

	return nil
}

// GetAgent returns an agent definition by ID.
func (o *Orchestrator) GetAgent(id string) (*AgentDefinition, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	agent, ok := o.agents[id]
	return agent, ok
}

// GetRuntime returns an agent's runtime by ID.
func (o *Orchestrator) GetRuntime(id string) (*agent.Runtime, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	runtime, ok := o.runtimes[id]
	return runtime, ok
}

// ListAgents returns all registered agent definitions.
func (o *Orchestrator) ListAgents() []*AgentDefinition {
	o.mu.RLock()
	defer o.mu.RUnlock()

	agents := make([]*AgentDefinition, 0, len(o.agents))
	for _, a := range o.agents {
		agents = append(agents, a)
	}
	return agents
}

// Process handles an incoming message in the multi-agent system.
//
// The orchestrator:
//  1. Determines the current or selects an appropriate agent
//  2. Builds the context for the agent
//  3. Processes the message through the agent's runtime
//  4. Handles any handoffs requested by the agent
//  5. Streams responses back to the caller
func (o *Orchestrator) Process(ctx context.Context, session *models.Session, msg *models.Message) (<-chan *agent.ResponseChunk, error) {
	chunks := make(chan *agent.ResponseChunk, 10)

	go func() {
		defer close(chunks)

		// Get current session state
		sessionMeta := o.getSessionMetadata(session)

		// Determine which agent should handle this message
		agentID, err := o.selectAgent(ctx, session, msg, sessionMeta)
		if err != nil {
			chunks <- &agent.ResponseChunk{Error: fmt.Errorf("agent selection failed: %w", err)}
			return
		}

		o.emitEvent(&OrchestratorEvent{
			Type:      EventAgentSelected,
			AgentID:   agentID,
			Timestamp: time.Now(),
		})

		// Process through the selected agent
		err = o.processWithAgent(ctx, session, msg, agentID, sessionMeta, chunks)
		if err != nil {
			chunks <- &agent.ResponseChunk{Error: err}
			return
		}
	}()

	return chunks, nil
}

// selectAgent determines which agent should handle the message.
func (o *Orchestrator) selectAgent(ctx context.Context, session *models.Session, msg *models.Message, meta *SessionMetadata) (string, error) {
	// If supervisor mode is active, let the supervisor decide
	if o.supervisor != nil && o.config.SupervisorAgentID != "" {
		return o.supervisor.SelectAgent(ctx, session, msg, meta)
	}

	// If there's a current agent, continue with it unless routing says otherwise
	if meta.CurrentAgentID != "" {
		// Check if the router wants to change agents
		newAgentID, shouldRoute := o.router.Route(ctx, session, msg, meta.CurrentAgentID)
		if shouldRoute && newAgentID != "" {
			return newAgentID, nil
		}
		return meta.CurrentAgentID, nil
	}

	// Use router to select initial agent
	agentID, _ := o.router.Route(ctx, session, msg, "")
	if agentID != "" {
		return agentID, nil
	}

	// Fall back to default agent
	if o.config.DefaultAgentID != "" {
		return o.config.DefaultAgentID, nil
	}

	// Return first registered agent as last resort
	o.mu.RLock()
	defer o.mu.RUnlock()
	for id := range o.agents {
		return id, nil
	}

	return "", fmt.Errorf("no agents available")
}

// processWithAgent processes a message through a specific agent.
func (o *Orchestrator) processWithAgent(ctx context.Context, session *models.Session, msg *models.Message, agentID string, meta *SessionMetadata, chunks chan<- *agent.ResponseChunk) error {
	runtime, ok := o.GetRuntime(agentID)
	if !ok {
		return fmt.Errorf("agent runtime not found: %s", agentID)
	}

	// Update session metadata with current agent
	meta.CurrentAgentID = agentID
	o.updateSessionMetadata(session, meta)

	// Build context for the agent
	agentCtx := o.buildAgentContext(ctx, agentID, meta)

	// Process through the runtime
	agentChunks, err := runtime.Process(agentCtx, session, msg)
	if err != nil {
		return fmt.Errorf("agent processing failed: %w", err)
	}

	// Forward chunks and handle handoffs
	for chunk := range agentChunks {
		if chunk == nil {
			continue
		}

		// Check for handoff tool results
		if chunk.ToolResult != nil && o.isHandoffResult(chunk.ToolResult) {
			handoffResult, err := o.handleHandoff(ctx, session, chunk.ToolResult, meta, chunks)
			if err != nil {
				o.emitEvent(&OrchestratorEvent{
					Type:      EventHandoffFailed,
					AgentID:   agentID,
					Message:   err.Error(),
					Timestamp: time.Now(),
				})
				chunks <- &agent.ResponseChunk{Error: err}
				return nil
			}

			// If handoff was successful and returned control, continue
			if handoffResult != nil && handoffResult.ShouldReturn {
				// Continue processing with the returning context
				continue
			}

			// Handoff completed without return expected
			return nil
		}

		chunks <- chunk
	}

	return nil
}

// handleHandoff processes a handoff request from an agent.
func (o *Orchestrator) handleHandoff(ctx context.Context, session *models.Session, result *models.ToolResult, meta *SessionMetadata, chunks chan<- *agent.ResponseChunk) (*HandoffResult, error) {
	// Parse the handoff request from the tool result
	request, err := o.handoffTool.ParseResult(result)
	if err != nil {
		return nil, fmt.Errorf("invalid handoff request: %w", err)
	}

	o.emitEvent(&OrchestratorEvent{
		Type:        EventHandoffInitiated,
		FromAgentID: request.FromAgentID,
		ToAgentID:   request.ToAgentID,
		Message:     request.Reason,
		Timestamp:   time.Now(),
	})

	// Check handoff depth to prevent loops
	if len(meta.ActiveHandoffStack) >= o.config.MaxHandoffDepth {
		return nil, fmt.Errorf("maximum handoff depth (%d) exceeded", o.config.MaxHandoffDepth)
	}

	// Validate target agent exists and can receive handoffs
	targetAgent, ok := o.GetAgent(request.ToAgentID)
	if !ok {
		return nil, fmt.Errorf("target agent not found: %s", request.ToAgentID)
	}
	if !targetAgent.CanReceiveHandoffs {
		return nil, fmt.Errorf("target agent cannot receive handoffs: %s", request.ToAgentID)
	}

	// Build shared context
	sharedCtx, err := o.contextManager.BuildSharedContext(ctx, session, request)
	if err != nil {
		return nil, fmt.Errorf("failed to build shared context: %w", err)
	}
	request.Context = sharedCtx

	o.emitEvent(&OrchestratorEvent{
		Type:        EventContextShared,
		FromAgentID: request.FromAgentID,
		ToAgentID:   request.ToAgentID,
		Timestamp:   time.Now(),
	})

	// Update handoff stack if return is expected
	if request.ReturnExpected {
		meta.ActiveHandoffStack = append(meta.ActiveHandoffStack, request.FromAgentID)
	}

	// Update agent history
	now := time.Now()
	meta.AgentHistory = append(meta.AgentHistory, AgentHistoryEntry{
		AgentID:       request.FromAgentID,
		StartedAt:     meta.LastHandoffAt.Add(0),
		EndedAt:       &now,
		HandoffTo:     request.ToAgentID,
		HandoffReason: request.Reason,
	})
	meta.CurrentAgentID = request.ToAgentID
	meta.HandoffCount++
	meta.LastHandoffAt = &now

	o.updateSessionMetadata(session, meta)

	// Create a handoff message for the target agent
	handoffMsg := &models.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      models.RoleSystem,
		Content:   o.buildHandoffMessage(request),
		CreatedAt: time.Now(),
	}

	// Process with the target agent
	err = o.processWithAgent(ctx, session, handoffMsg, request.ToAgentID, meta, chunks)
	if err != nil {
		return nil, fmt.Errorf("target agent processing failed: %w", err)
	}

	result2 := &HandoffResult{
		Success:      true,
		FromAgentID:  request.FromAgentID,
		ToAgentID:    request.ToAgentID,
		ShouldReturn: request.ReturnExpected && len(meta.ActiveHandoffStack) > 0,
		Duration:     time.Since(request.Timestamp),
	}

	o.emitEvent(&OrchestratorEvent{
		Type:        EventHandoffCompleted,
		FromAgentID: request.FromAgentID,
		ToAgentID:   request.ToAgentID,
		Timestamp:   time.Now(),
	})

	return result2, nil
}

// buildHandoffMessage creates a system message for the handoff.
func (o *Orchestrator) buildHandoffMessage(request *HandoffRequest) string {
	msg := fmt.Sprintf("You are receiving control from agent '%s'.\nReason: %s", request.FromAgentID, request.Reason)

	if request.Context != nil {
		if request.Context.Task != "" {
			msg += fmt.Sprintf("\n\nCurrent task: %s", request.Context.Task)
		}
		if request.Context.Summary != "" {
			msg += fmt.Sprintf("\n\nConversation summary:\n%s", request.Context.Summary)
		}
	}

	if request.ReturnExpected {
		msg += "\n\nNote: Control should be returned to the previous agent when your task is complete."
	}

	return msg
}

// buildAgentContext creates a context for agent processing.
func (o *Orchestrator) buildAgentContext(ctx context.Context, agentID string, meta *SessionMetadata) context.Context {
	// Add agent ID to context
	ctx = WithCurrentAgent(ctx, agentID)

	// Add shared context if available
	if meta != nil && len(meta.ActiveHandoffStack) > 0 {
		ctx = WithHandoffStack(ctx, meta.ActiveHandoffStack)
	}

	// Add tool policy if agent has one
	if agentDef, ok := o.GetAgent(agentID); ok && agentDef.ToolPolicy != nil {
		ctx = agent.WithToolPolicy(ctx, o.policyResolver, agentDef.ToolPolicy)
	}

	return ctx
}

// isHandoffResult checks if a tool result is from the handoff tool.
func (o *Orchestrator) isHandoffResult(result *models.ToolResult) bool {
	// The handoff tool sets a specific prefix in results
	return result != nil && len(result.Content) > 0 && result.Content[0] == '{'
}

// getSessionMetadata retrieves multi-agent metadata from session.
func (o *Orchestrator) getSessionMetadata(session *models.Session) *SessionMetadata {
	if session.Metadata == nil {
		return &SessionMetadata{}
	}

	meta := &SessionMetadata{}

	if v, ok := session.Metadata["current_agent_id"].(string); ok {
		meta.CurrentAgentID = v
	}
	if v, ok := session.Metadata["handoff_count"].(int); ok {
		meta.HandoffCount = v
	}
	if v, ok := session.Metadata["active_handoff_stack"].([]string); ok {
		meta.ActiveHandoffStack = v
	}

	return meta
}

// updateSessionMetadata updates the session with multi-agent metadata.
func (o *Orchestrator) updateSessionMetadata(session *models.Session, meta *SessionMetadata) {
	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}

	session.Metadata["current_agent_id"] = meta.CurrentAgentID
	session.Metadata["handoff_count"] = meta.HandoffCount
	session.Metadata["active_handoff_stack"] = meta.ActiveHandoffStack
	if meta.LastHandoffAt != nil {
		session.Metadata["last_handoff_at"] = meta.LastHandoffAt
	}
}

// SetEventCallback sets a callback for orchestration events.
func (o *Orchestrator) SetEventCallback(callback func(*OrchestratorEvent)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.eventCallback = callback
}

// emitEvent sends an event to the callback if configured.
func (o *Orchestrator) emitEvent(event *OrchestratorEvent) {
	o.mu.RLock()
	callback := o.eventCallback
	o.mu.RUnlock()

	if callback != nil {
		callback(event)
	}
}

// RegisterToolForAgent registers a tool with a specific agent's runtime.
func (o *Orchestrator) RegisterToolForAgent(agentID string, tool agent.Tool) error {
	runtime, ok := o.GetRuntime(agentID)
	if !ok {
		return fmt.Errorf("agent not found: %s", agentID)
	}
	runtime.RegisterTool(tool)
	return nil
}

// RegisterToolForAll registers a tool with all agent runtimes.
func (o *Orchestrator) RegisterToolForAll(tool agent.Tool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for _, runtime := range o.runtimes {
		runtime.RegisterTool(tool)
	}
}

// Config returns the orchestrator's configuration.
func (o *Orchestrator) Config() *MultiAgentConfig {
	return o.config
}

// Sessions returns the session store.
func (o *Orchestrator) Sessions() sessions.Store {
	return o.sessions
}

// Provider returns the LLM provider.
func (o *Orchestrator) Provider() agent.LLMProvider {
	return o.provider
}

// Context keys for multi-agent information.
type currentAgentKey struct{}
type handoffStackKey struct{}

// WithCurrentAgent adds the current agent ID to the context.
func WithCurrentAgent(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, currentAgentKey{}, agentID)
}

// CurrentAgentFromContext retrieves the current agent ID from context.
func CurrentAgentFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(currentAgentKey{}).(string)
	return v, ok
}

// WithHandoffStack adds the handoff stack to the context.
func WithHandoffStack(ctx context.Context, stack []string) context.Context {
	return context.WithValue(ctx, handoffStackKey{}, stack)
}

// HandoffStackFromContext retrieves the handoff stack from context.
func HandoffStackFromContext(ctx context.Context) []string {
	v, ok := ctx.Value(handoffStackKey{}).([]string)
	if !ok {
		return nil
	}
	return v
}
