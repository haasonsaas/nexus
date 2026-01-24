// Package plugins provides a plugin system with lifecycle hooks.
// Plugins can register handlers for various events to observe and modify
// system behavior without changing core code.
package plugins

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// HookName identifies a specific lifecycle hook.
type HookName string

const (
	// Agent lifecycle hooks
	HookBeforeAgentStart HookName = "before_agent_start"
	HookAgentEnd         HookName = "agent_end"
	HookBeforeCompaction HookName = "before_compaction"
	HookAfterCompaction  HookName = "after_compaction"

	// Message lifecycle hooks
	HookMessageReceived HookName = "message_received"
	HookMessageSending  HookName = "message_sending"
	HookMessageSent     HookName = "message_sent"

	// Tool lifecycle hooks
	HookBeforeToolCall    HookName = "before_tool_call"
	HookAfterToolCall     HookName = "after_tool_call"
	HookToolResultPersist HookName = "tool_result_persist"

	// Session lifecycle hooks
	HookSessionStart HookName = "session_start"
	HookSessionEnd   HookName = "session_end"

	// Gateway lifecycle hooks
	HookGatewayStart HookName = "gateway_start"
	HookGatewayStop  HookName = "gateway_stop"
)

// HookHandler is a function that handles a hook event.
// Returns a result that may modify behavior (for modifying hooks) or nil.
type HookHandler func(ctx context.Context, event HookEvent) (HookResult, error)

// HookEvent contains data passed to hook handlers.
type HookEvent struct {
	// Common fields
	Type      HookName               `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`

	// Context fields (populated based on hook type)
	AgentID    string `json:"agent_id,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	ChannelID  string `json:"channel_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// HookResult contains the result returned by a hook handler.
// Different hooks use different fields.
type HookResult struct {
	// For before_agent_start: inject context
	SystemPrompt   string `json:"system_prompt,omitempty"`
	PrependContext string `json:"prepend_context,omitempty"`

	// For message_sending: modify or cancel
	Content string `json:"content,omitempty"`
	Cancel  bool   `json:"cancel,omitempty"`

	// For before_tool_call: modify or block
	Params      map[string]interface{} `json:"params,omitempty"`
	Block       bool                   `json:"block,omitempty"`
	BlockReason string                 `json:"block_reason,omitempty"`

	// For tool_result_persist: transform message
	Message interface{} `json:"message,omitempty"`
}

// HookRegistration represents a registered hook handler.
type HookRegistration struct {
	PluginID string
	HookName HookName
	Handler  HookHandler
	Priority int // Higher priority runs first
}

// HookRunner manages hook execution.
type HookRunner struct {
	hooks       map[HookName][]*HookRegistration
	mu          sync.RWMutex
	catchErrors bool
	logger      HookLogger
}

// HookLogger interface for hook runner logging.
// Compatible with the Logger interface in plugin.go.
type HookLogger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// HookRunnerConfig configures the hook runner.
type HookRunnerConfig struct {
	CatchErrors bool
	Logger      HookLogger
}

// NewHookRunner creates a new hook runner.
func NewHookRunner(cfg HookRunnerConfig) *HookRunner {
	return &HookRunner{
		hooks:       make(map[HookName][]*HookRegistration),
		catchErrors: cfg.CatchErrors,
		logger:      cfg.Logger,
	}
}

// Register adds a hook handler.
func (r *HookRunner) Register(pluginID string, hookName HookName, handler HookHandler, priority int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	reg := &HookRegistration{
		PluginID: pluginID,
		HookName: hookName,
		Handler:  handler,
		Priority: priority,
	}

	r.hooks[hookName] = append(r.hooks[hookName], reg)

	// Sort by priority (higher first)
	sort.Slice(r.hooks[hookName], func(i, j int) bool {
		return r.hooks[hookName][i].Priority > r.hooks[hookName][j].Priority
	})
}

// Unregister removes all hooks for a plugin.
func (r *HookRunner) Unregister(pluginID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for hookName, regs := range r.hooks {
		filtered := make([]*HookRegistration, 0, len(regs))
		for _, reg := range regs {
			if reg.PluginID != pluginID {
				filtered = append(filtered, reg)
			}
		}
		r.hooks[hookName] = filtered
	}
}

// HasHooks returns true if any handlers are registered for the hook.
func (r *HookRunner) HasHooks(hookName HookName) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hooks[hookName]) > 0
}

// RunVoid executes a void hook (fire-and-forget, parallel execution).
// Used for: message_received, message_sent, agent_end, session_start, session_end,
// gateway_start, gateway_stop, after_tool_call, before_compaction, after_compaction
func (r *HookRunner) RunVoid(ctx context.Context, hookName HookName, event HookEvent) {
	r.mu.RLock()
	regs := r.hooks[hookName]
	r.mu.RUnlock()

	if len(regs) == 0 {
		return
	}

	if r.logger != nil {
		r.logger.Info("running hook %s (%d handlers)", hookName, len(regs))
	}

	event.Type = hookName

	var wg sync.WaitGroup
	for _, reg := range regs {
		wg.Add(1)
		go func(reg *HookRegistration) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					if r.logger != nil {
						r.logger.Error("hook %s from %s panicked: %v", hookName, reg.PluginID, rec)
					}
				}
			}()

			_, err := reg.Handler(ctx, event)
			if err != nil {
				msg := fmt.Sprintf("hook %s from %s failed: %v", hookName, reg.PluginID, err)
				if r.catchErrors {
					if r.logger != nil {
						r.logger.Error(msg)
					}
				} else {
					panic(msg)
				}
			}
		}(reg)
	}

	wg.Wait()
}

// RunModifying executes a modifying hook (sequential, results merged).
// Used for: before_agent_start, message_sending, before_tool_call
func (r *HookRunner) RunModifying(ctx context.Context, hookName HookName, event HookEvent) (*HookResult, error) {
	r.mu.RLock()
	regs := r.hooks[hookName]
	r.mu.RUnlock()

	if len(regs) == 0 {
		return nil, nil
	}

	if r.logger != nil {
		r.logger.Info("running hook %s (%d handlers, sequential)", hookName, len(regs))
	}

	event.Type = hookName
	var result *HookResult

	for _, reg := range regs {
		handlerResult, err := func() (res HookResult, err error) {
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("panic: %v", rec)
				}
			}()
			return reg.Handler(ctx, event)
		}()

		if err != nil {
			msg := fmt.Sprintf("hook %s from %s failed: %v", hookName, reg.PluginID, err)
			if r.catchErrors {
				if r.logger != nil {
					r.logger.Error(msg)
				}
				continue
			}
			return nil, errors.New(msg)
		}

		// Merge results based on hook type
		result = r.mergeResult(hookName, result, &handlerResult)
	}

	return result, nil
}

// mergeResult merges hook results based on hook type.
func (r *HookRunner) mergeResult(hookName HookName, acc, next *HookResult) *HookResult {
	if next == nil {
		return acc
	}
	if acc == nil {
		return next
	}

	switch hookName {
	case HookBeforeAgentStart:
		// Later handlers override systemPrompt, append prependContext
		if next.SystemPrompt != "" {
			acc.SystemPrompt = next.SystemPrompt
		}
		if next.PrependContext != "" {
			if acc.PrependContext != "" {
				acc.PrependContext = acc.PrependContext + "\n\n" + next.PrependContext
			} else {
				acc.PrependContext = next.PrependContext
			}
		}

	case HookMessageSending:
		// Later handlers override content/cancel
		if next.Content != "" {
			acc.Content = next.Content
		}
		if next.Cancel {
			acc.Cancel = true
		}

	case HookBeforeToolCall:
		// Later handlers override params/block
		if next.Params != nil {
			acc.Params = next.Params
		}
		if next.Block {
			acc.Block = true
			acc.BlockReason = next.BlockReason
		}

	default:
		// For other hooks, just use the latest result
		return next
	}

	return acc
}

// RunBeforeAgentStart is a convenience method for the before_agent_start hook.
func (r *HookRunner) RunBeforeAgentStart(ctx context.Context, prompt string, messages []interface{}, agentID, sessionKey string) (*HookResult, error) {
	event := HookEvent{
		AgentID:    agentID,
		SessionKey: sessionKey,
		Data: map[string]interface{}{
			"prompt":   prompt,
			"messages": messages,
		},
	}
	return r.RunModifying(ctx, HookBeforeAgentStart, event)
}

// RunAgentEnd is a convenience method for the agent_end hook.
func (r *HookRunner) RunAgentEnd(ctx context.Context, messages []interface{}, success bool, errMsg string, durationMs int64, agentID, sessionKey string) {
	event := HookEvent{
		AgentID:    agentID,
		SessionKey: sessionKey,
		Data: map[string]interface{}{
			"messages":    messages,
			"success":     success,
			"error":       errMsg,
			"duration_ms": durationMs,
		},
	}
	r.RunVoid(ctx, HookAgentEnd, event)
}

// RunMessageReceived is a convenience method for the message_received hook.
func (r *HookRunner) RunMessageReceived(ctx context.Context, from, content, channelID string, metadata map[string]interface{}) {
	event := HookEvent{
		ChannelID: channelID,
		Data: map[string]interface{}{
			"from":     from,
			"content":  content,
			"metadata": metadata,
		},
	}
	r.RunVoid(ctx, HookMessageReceived, event)
}

// RunMessageSending is a convenience method for the message_sending hook.
func (r *HookRunner) RunMessageSending(ctx context.Context, to, content, channelID string, metadata map[string]interface{}) (*HookResult, error) {
	event := HookEvent{
		ChannelID: channelID,
		Data: map[string]interface{}{
			"to":       to,
			"content":  content,
			"metadata": metadata,
		},
	}
	return r.RunModifying(ctx, HookMessageSending, event)
}

// RunBeforeToolCall is a convenience method for the before_tool_call hook.
func (r *HookRunner) RunBeforeToolCall(ctx context.Context, toolName, toolCallID string, params map[string]interface{}, agentID, sessionKey string) (*HookResult, error) {
	event := HookEvent{
		AgentID:    agentID,
		SessionKey: sessionKey,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Data: map[string]interface{}{
			"params": params,
		},
	}
	return r.RunModifying(ctx, HookBeforeToolCall, event)
}

// RunAfterToolCall is a convenience method for the after_tool_call hook.
func (r *HookRunner) RunAfterToolCall(ctx context.Context, toolName, toolCallID string, params map[string]interface{}, result interface{}, errMsg string, durationMs int64, agentID, sessionKey string) {
	event := HookEvent{
		AgentID:    agentID,
		SessionKey: sessionKey,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Data: map[string]interface{}{
			"params":      params,
			"result":      result,
			"error":       errMsg,
			"duration_ms": durationMs,
		},
	}
	r.RunVoid(ctx, HookAfterToolCall, event)
}

// RunSessionStart is a convenience method for the session_start hook.
func (r *HookRunner) RunSessionStart(ctx context.Context, sessionID, agentID string, resumedFrom string) {
	event := HookEvent{
		SessionID: sessionID,
		AgentID:   agentID,
		Data: map[string]interface{}{
			"resumed_from": resumedFrom,
		},
	}
	r.RunVoid(ctx, HookSessionStart, event)
}

// RunSessionEnd is a convenience method for the session_end hook.
func (r *HookRunner) RunSessionEnd(ctx context.Context, sessionID, agentID string, messageCount int, durationMs int64) {
	event := HookEvent{
		SessionID: sessionID,
		AgentID:   agentID,
		Data: map[string]interface{}{
			"message_count": messageCount,
			"duration_ms":   durationMs,
		},
	}
	r.RunVoid(ctx, HookSessionEnd, event)
}

// Global hook runner instance
var (
	globalHookRunner *HookRunner
	globalHookMu     sync.RWMutex
)

// InitGlobalHookRunner initializes the global hook runner.
func InitGlobalHookRunner(cfg HookRunnerConfig) {
	globalHookMu.Lock()
	defer globalHookMu.Unlock()
	globalHookRunner = NewHookRunner(cfg)
}

// GetGlobalHookRunner returns the global hook runner.
func GetGlobalHookRunner() *HookRunner {
	globalHookMu.RLock()
	defer globalHookMu.RUnlock()
	return globalHookRunner
}

// RegisterHook registers a hook with the global runner.
func RegisterHook(pluginID string, hookName HookName, handler HookHandler, priority int) {
	runner := GetGlobalHookRunner()
	if runner != nil {
		runner.Register(pluginID, hookName, handler, priority)
	}
}
