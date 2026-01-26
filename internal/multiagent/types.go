// Package multiagent provides multi-agent orchestration for Nexus.
//
// This package implements support for multiple specialized agents that can
// collaborate on tasks through handoffs and context sharing. It supports
// both supervisor patterns (central coordinator) and peer-to-peer handoffs.
//
// # Architecture Overview
//
//	                    ┌─────────────────┐
//	                    │   Orchestrator  │
//	                    │   (Supervisor)  │
//	                    └────────┬────────┘
//	                             │
//	       ┌─────────────────────┼─────────────────────┐
//	       │                     │                     │
//	       ▼                     ▼                     ▼
//	┌──────────────┐    ┌──────────────┐    ┌──────────────┐
//	│   Agent A    │◄──►│   Agent B    │◄──►│   Agent C    │
//	│ (Specialist) │    │ (Specialist) │    │ (Specialist) │
//	└──────────────┘    └──────────────┘    └──────────────┘
//	     Peer-to-peer handoffs also supported
//
// # Key Concepts
//
//   - AgentDefinition: Defines an agent's identity, capabilities, and tools
//   - HandoffRule: Specifies when and how to transfer control between agents
//   - RoutingTrigger: Conditions that determine agent selection
//   - ContextSharingMode: How context is shared during handoffs
package multiagent

import (
	"encoding/json"
	"time"

	"github.com/haasonsaas/nexus/internal/tools/policy"
)

// AgentDefinition describes a specialized agent in the multi-agent system.
type AgentDefinition struct {
	// ID is the unique identifier for this agent.
	ID string `json:"id" yaml:"id"`

	// Name is the human-readable name for this agent.
	Name string `json:"name" yaml:"name"`

	// Description explains what this agent specializes in.
	// Used by supervisors and other agents to decide on handoffs.
	Description string `json:"description" yaml:"description"`

	// SystemPrompt is the agent's base system prompt.
	SystemPrompt string `json:"system_prompt" yaml:"system_prompt"`

	// Model specifies the LLM model to use (optional, falls back to default).
	Model string `json:"model,omitempty" yaml:"model"`

	// Provider specifies the LLM provider (optional, falls back to default).
	Provider string `json:"provider,omitempty" yaml:"provider"`

	// AgentDir is the state directory for this agent (optional).
	AgentDir string `json:"agent_dir,omitempty" yaml:"agent_dir"`

	// Tools lists the tools this agent has access to.
	Tools []string `json:"tools,omitempty" yaml:"tools"`

	// ToolPolicy defines tool access rules for this agent.
	ToolPolicy *policy.Policy `json:"tool_policy,omitempty" yaml:"tool_policy"`

	// HandoffRules defines when this agent should hand off to others.
	HandoffRules []HandoffRule `json:"handoff_rules,omitempty" yaml:"handoff_rules"`

	// CanReceiveHandoffs indicates if other agents can hand off to this one.
	CanReceiveHandoffs bool `json:"can_receive_handoffs" yaml:"can_receive_handoffs"`

	// MaxIterations limits the agent's agentic loop iterations.
	MaxIterations int `json:"max_iterations,omitempty" yaml:"max_iterations"`

	// SwarmRole configures how this agent participates in swarm execution (optional).
	SwarmRole SwarmRole `json:"swarm_role,omitempty" yaml:"swarm_role,omitempty"`

	// DependsOn lists agent IDs that must complete before this agent runs (swarm mode).
	DependsOn []string `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`

	// CanTrigger lists agent IDs that this agent can trigger (swarm mode).
	CanTrigger []string `json:"can_trigger,omitempty" yaml:"can_trigger,omitempty"`

	// Metadata contains additional agent configuration.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata"`
}

// HandoffRule defines conditions for transferring control to another agent.
type HandoffRule struct {
	// TargetAgentID is the agent to hand off to.
	TargetAgentID string `json:"target_agent_id" yaml:"target_agent_id"`

	// Triggers define conditions that activate this handoff.
	Triggers []RoutingTrigger `json:"triggers" yaml:"triggers"`

	// Priority determines order when multiple rules match (higher = first).
	Priority int `json:"priority,omitempty" yaml:"priority"`

	// ContextMode specifies how context is shared during handoff.
	ContextMode ContextSharingMode `json:"context_mode,omitempty" yaml:"context_mode"`

	// SummaryPrompt is used to generate context summary for partial sharing.
	SummaryPrompt string `json:"summary_prompt,omitempty" yaml:"summary_prompt"`

	// ReturnToSender indicates if control should return after the target completes.
	ReturnToSender bool `json:"return_to_sender,omitempty" yaml:"return_to_sender"`

	// Message is an optional message to include with the handoff.
	Message string `json:"message,omitempty" yaml:"message"`
}

// RoutingTrigger defines a condition that activates agent routing.
type RoutingTrigger struct {
	// Type specifies the kind of trigger.
	Type TriggerType `json:"type" yaml:"type"`

	// Value is the trigger-specific value (pattern, keyword, etc.).
	Value string `json:"value,omitempty" yaml:"value"`

	// Values allows multiple values for certain trigger types.
	Values []string `json:"values,omitempty" yaml:"values"`

	// Threshold is used for score-based triggers (0.0 to 1.0).
	Threshold float64 `json:"threshold,omitempty" yaml:"threshold"`

	// Metadata contains additional trigger configuration.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata"`
}

// TriggerType defines the type of routing trigger.
type TriggerType string

const (
	// TriggerKeyword matches specific keywords in the message.
	TriggerKeyword TriggerType = "keyword"

	// TriggerPattern matches a regex pattern against the message.
	TriggerPattern TriggerType = "pattern"

	// TriggerIntent uses LLM classification to detect intent.
	TriggerIntent TriggerType = "intent"

	// TriggerToolUse triggers when specific tools are invoked.
	TriggerToolUse TriggerType = "tool_use"

	// TriggerExplicit triggers on explicit handoff requests.
	TriggerExplicit TriggerType = "explicit"

	// TriggerFallback triggers when no other agent matches.
	TriggerFallback TriggerType = "fallback"

	// TriggerAlways always triggers (used for supervisor delegation).
	TriggerAlways TriggerType = "always"

	// TriggerTaskComplete triggers when a task is marked complete.
	TriggerTaskComplete TriggerType = "task_complete"

	// TriggerError triggers on error conditions.
	TriggerError TriggerType = "error"
)

// ContextSharingMode defines how context is shared during handoffs.
type ContextSharingMode string

const (
	// ContextFull shares the entire conversation history.
	ContextFull ContextSharingMode = "full"

	// ContextSummary shares only a summary of the conversation.
	ContextSummary ContextSharingMode = "summary"

	// ContextFiltered shares messages matching specific criteria.
	ContextFiltered ContextSharingMode = "filtered"

	// ContextNone shares no historical context.
	ContextNone ContextSharingMode = "none"

	// ContextLastN shares the last N messages.
	ContextLastN ContextSharingMode = "last_n"
)

// HandoffRequest represents a request to transfer control to another agent.
type HandoffRequest struct {
	// FromAgentID is the agent initiating the handoff.
	FromAgentID string `json:"from_agent_id"`

	// ToAgentID is the target agent to hand off to.
	ToAgentID string `json:"to_agent_id"`

	// Reason explains why the handoff is happening.
	Reason string `json:"reason"`

	// Context contains any context to pass to the target agent.
	Context *SharedContext `json:"context,omitempty"`

	// ReturnExpected indicates if the source agent expects control back.
	ReturnExpected bool `json:"return_expected,omitempty"`

	// Priority of this handoff (higher = more urgent).
	Priority int `json:"priority,omitempty"`

	// Timestamp when the handoff was requested.
	Timestamp time.Time `json:"timestamp"`
}

// HandoffResult represents the outcome of a handoff.
type HandoffResult struct {
	// Success indicates if the handoff completed successfully.
	Success bool `json:"success"`

	// FromAgentID is the agent that initiated the handoff.
	FromAgentID string `json:"from_agent_id"`

	// ToAgentID is the agent that received control.
	ToAgentID string `json:"to_agent_id"`

	// Response is any final message from the target agent.
	Response string `json:"response,omitempty"`

	// ShouldReturn indicates if control should return to the source.
	ShouldReturn bool `json:"should_return,omitempty"`

	// Error describes what went wrong if the handoff failed.
	Error string `json:"error,omitempty"`

	// Duration is how long the handoff took.
	Duration time.Duration `json:"duration"`
}

// SharedContext contains context shared between agents during handoffs.
type SharedContext struct {
	// Summary is a brief summary of the conversation so far.
	Summary string `json:"summary,omitempty"`

	// Messages contains the conversation messages (if sharing full context).
	Messages []SharedMessage `json:"messages,omitempty"`

	// Variables are key-value pairs extracted from the conversation.
	Variables map[string]any `json:"variables,omitempty"`

	// Task describes the current task or goal.
	Task string `json:"task,omitempty"`

	// PreviousAgents lists agents that have handled this conversation.
	PreviousAgents []string `json:"previous_agents,omitempty"`

	// Metadata contains additional context data.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SharedMessage represents a message in shared context.
type SharedMessage struct {
	// Role is the message role (user, assistant, system, tool).
	Role string `json:"role"`

	// Content is the message content.
	Content string `json:"content"`

	// AgentID identifies which agent sent assistant messages.
	AgentID string `json:"agent_id,omitempty"`

	// Timestamp when the message was created.
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// AgentState tracks the current state of an agent in a session.
type AgentState struct {
	// AgentID is the current active agent.
	AgentID string `json:"agent_id"`

	// Status is the agent's current status.
	Status AgentStatus `json:"status"`

	// Iteration is the current agentic loop iteration.
	Iteration int `json:"iteration"`

	// StartedAt is when this agent became active.
	StartedAt time.Time `json:"started_at"`

	// HandoffStack tracks the chain of handoffs for return.
	HandoffStack []string `json:"handoff_stack,omitempty"`

	// SharedContext is the context passed to this agent.
	SharedContext *SharedContext `json:"shared_context,omitempty"`

	// Metadata contains additional state data.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AgentStatus represents an agent's operational status.
type AgentStatus string

const (
	// StatusActive means the agent is currently processing.
	StatusActive AgentStatus = "active"

	// StatusWaiting means the agent is waiting for user input.
	StatusWaiting AgentStatus = "waiting"

	// StatusHandedOff means the agent has handed off to another.
	StatusHandedOff AgentStatus = "handed_off"

	// StatusComplete means the agent has finished its task.
	StatusComplete AgentStatus = "complete"

	// StatusError means the agent encountered an error.
	StatusError AgentStatus = "error"
)

// MultiAgentConfig contains the overall multi-agent system configuration.
type MultiAgentConfig struct {
	// DefaultAgentID is the initial agent for new conversations.
	DefaultAgentID string `json:"default_agent_id" yaml:"default_agent_id"`

	// SupervisorAgentID is the coordinator agent (if using supervisor pattern).
	SupervisorAgentID string `json:"supervisor_agent_id,omitempty" yaml:"supervisor_agent_id"`

	// Agents contains all agent definitions.
	Agents []AgentDefinition `json:"agents" yaml:"agents"`

	// GlobalHandoffRules apply to all agents.
	GlobalHandoffRules []HandoffRule `json:"global_handoff_rules,omitempty" yaml:"global_handoff_rules"`

	// DefaultContextMode is the default context sharing mode for handoffs.
	DefaultContextMode ContextSharingMode `json:"default_context_mode,omitempty" yaml:"default_context_mode"`

	// MaxHandoffDepth limits the handoff chain depth to prevent loops.
	MaxHandoffDepth int `json:"max_handoff_depth,omitempty" yaml:"max_handoff_depth"`

	// HandoffTimeout is the maximum time allowed for a single handoff.
	HandoffTimeout time.Duration `json:"handoff_timeout,omitempty" yaml:"handoff_timeout"`

	// EnablePeerHandoffs allows agents to hand off directly to each other.
	EnablePeerHandoffs bool `json:"enable_peer_handoffs" yaml:"enable_peer_handoffs"`

	// Swarm configures optional swarm-mode execution.
	Swarm SwarmConfig `json:"swarm,omitempty" yaml:"swarm,omitempty"`

	// Metadata contains additional configuration.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata"`
}

// SessionMetadata extends session metadata for multi-agent support.
type SessionMetadata struct {
	// CurrentAgentID is the currently active agent.
	CurrentAgentID string `json:"current_agent_id"`

	// AgentHistory tracks all agents that have handled this session.
	AgentHistory []AgentHistoryEntry `json:"agent_history,omitempty"`

	// HandoffCount is the total number of handoffs in this session.
	HandoffCount int `json:"handoff_count"`

	// LastHandoffAt is when the last handoff occurred.
	LastHandoffAt *time.Time `json:"last_handoff_at,omitempty"`

	// ActiveHandoffStack is the current chain of pending returns.
	ActiveHandoffStack []string `json:"active_handoff_stack,omitempty"`
}

// AgentHistoryEntry records an agent's involvement in a session.
type AgentHistoryEntry struct {
	// AgentID is the agent that was active.
	AgentID string `json:"agent_id"`

	// StartedAt is when the agent became active.
	StartedAt time.Time `json:"started_at"`

	// EndedAt is when the agent handed off or completed.
	EndedAt *time.Time `json:"ended_at,omitempty"`

	// HandoffTo is the agent control was transferred to.
	HandoffTo string `json:"handoff_to,omitempty"`

	// HandoffReason explains why the handoff occurred.
	HandoffReason string `json:"handoff_reason,omitempty"`
}

// HandoffToolInput is the input schema for the handoff tool.
type HandoffToolInput struct {
	// TargetAgent is the ID or name of the agent to hand off to.
	TargetAgent string `json:"target_agent"`

	// Reason explains why the handoff is needed.
	Reason string `json:"reason"`

	// Context is optional additional context for the target agent.
	Context string `json:"context,omitempty"`

	// ReturnExpected indicates if you expect control to return.
	ReturnExpected bool `json:"return_expected,omitempty"`
}

// AgentManifest represents an AGENTS.md parsed structure.
type AgentManifest struct {
	// Agents defined in the manifest.
	Agents []AgentDefinition `json:"agents"`

	// GlobalConfig contains system-wide settings.
	GlobalConfig *MultiAgentConfig `json:"global_config,omitempty"`

	// Source is the file path this manifest was loaded from.
	Source string `json:"source,omitempty"`
}

// ToJSON serializes the agent definition to JSON.
func (a *AgentDefinition) ToJSON() ([]byte, error) {
	return json.Marshal(a)
}

// Clone creates a deep copy of the agent definition.
func (a *AgentDefinition) Clone() *AgentDefinition {
	if a == nil {
		return nil
	}
	clone := *a
	if a.Tools != nil {
		clone.Tools = make([]string, len(a.Tools))
		copy(clone.Tools, a.Tools)
	}
	if a.HandoffRules != nil {
		clone.HandoffRules = make([]HandoffRule, len(a.HandoffRules))
		copy(clone.HandoffRules, a.HandoffRules)
	}
	if a.DependsOn != nil {
		clone.DependsOn = make([]string, len(a.DependsOn))
		copy(clone.DependsOn, a.DependsOn)
	}
	if a.CanTrigger != nil {
		clone.CanTrigger = make([]string, len(a.CanTrigger))
		copy(clone.CanTrigger, a.CanTrigger)
	}
	if a.Metadata != nil {
		clone.Metadata = make(map[string]any)
		for k, v := range a.Metadata {
			clone.Metadata[k] = v
		}
	}
	return &clone
}

// HasTool checks if the agent has access to a specific tool.
func (a *AgentDefinition) HasTool(toolName string) bool {
	for _, t := range a.Tools {
		if t == toolName {
			return true
		}
	}
	return false
}

// GetHandoffTarget returns the target agent for a given trigger, if any rule matches.
func (a *AgentDefinition) GetHandoffTarget(trigger TriggerType, value string) *HandoffRule {
	for i := range a.HandoffRules {
		rule := &a.HandoffRules[i]
		for _, t := range rule.Triggers {
			if t.Type == trigger {
				if t.Value == "" || t.Value == value || containsValue(t.Values, value) {
					return rule
				}
			}
		}
	}
	return nil
}

// containsValue checks if a slice contains a value.
func containsValue(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}
