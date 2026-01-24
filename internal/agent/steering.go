// Package agent provides steering and follow-up message capabilities for the agent runtime.
// These features enable real-time intervention during agent execution.
package agent

import (
	"context"
	"sync"

	"github.com/haasonsaas/nexus/pkg/models"
)

// SteeringMessage represents a message that can be injected mid-run to interrupt the agent.
// When a steering message is delivered, remaining tool calls are skipped and the agent
// processes the steering message before continuing.
type SteeringMessage struct {
	// Content is the message text to inject
	Content string

	// Role defaults to "user" if empty
	Role string

	// Attachments contains any images/files
	Attachments []models.Attachment

	// Priority affects ordering when multiple steering messages queue (higher = first)
	Priority int

	// SkipRemainingTools when true skips remaining tool calls in current batch
	SkipRemainingTools bool
}

// FollowUpMessage represents a message queued for processing after the agent finishes.
// Unlike steering messages, follow-up messages wait for the current run to complete.
type FollowUpMessage struct {
	// Content is the message text
	Content string

	// Role defaults to "user" if empty
	Role string

	// Attachments contains any images/files
	Attachments []models.Attachment
}

// SteeringMode controls how steering messages are delivered.
type SteeringMode string

const (
	// SteeringModeOneAtATime delivers one steering message per turn
	SteeringModeOneAtATime SteeringMode = "one-at-a-time"

	// SteeringModeAll delivers all queued steering messages at once
	SteeringModeAll SteeringMode = "all"
)

// FollowUpMode controls how follow-up messages are delivered.
type FollowUpMode string

const (
	// FollowUpModeOneAtATime processes one follow-up message per agent run
	FollowUpModeOneAtATime FollowUpMode = "one-at-a-time"

	// FollowUpModeAll processes all queued follow-up messages at once
	FollowUpModeAll FollowUpMode = "all"
)

// SteeringQueue manages steering and follow-up messages for an agent session.
// It is safe for concurrent use.
type SteeringQueue struct {
	mu sync.Mutex

	// Steering messages (injected mid-run)
	steering []*SteeringMessage

	// Follow-up messages (processed after run completes)
	followUp []*FollowUpMessage

	// Modes
	steeringMode SteeringMode
	followUpMode FollowUpMode
}

// NewSteeringQueue creates a new steering queue with default modes.
func NewSteeringQueue() *SteeringQueue {
	return &SteeringQueue{
		steeringMode: SteeringModeOneAtATime,
		followUpMode: FollowUpModeOneAtATime,
	}
}

// SetSteeringMode configures how steering messages are delivered.
func (q *SteeringQueue) SetSteeringMode(mode SteeringMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steeringMode = mode
}

// SetFollowUpMode configures how follow-up messages are delivered.
func (q *SteeringQueue) SetFollowUpMode(mode FollowUpMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.followUpMode = mode
}

// Steer queues a steering message to interrupt the agent mid-run.
// The message is delivered after the current tool execution completes.
func (q *SteeringQueue) Steer(msg *SteeringMessage) {
	if msg == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steering = append(q.steering, msg)
}

// SteerText is a convenience method to queue a text steering message.
func (q *SteeringQueue) SteerText(content string) {
	q.Steer(&SteeringMessage{Content: content})
}

// FollowUp queues a follow-up message to process after the agent finishes.
func (q *SteeringQueue) FollowUp(msg *FollowUpMessage) {
	if msg == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.followUp = append(q.followUp, msg)
}

// FollowUpText is a convenience method to queue a text follow-up message.
func (q *SteeringQueue) FollowUpText(content string) {
	q.FollowUp(&FollowUpMessage{Content: content})
}

// GetSteeringMessages retrieves pending steering messages based on the configured mode.
// Called after each tool execution to check for interruptions.
func (q *SteeringQueue) GetSteeringMessages() []*SteeringMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.steering) == 0 {
		return nil
	}

	switch q.steeringMode {
	case SteeringModeAll:
		msgs := q.steering
		q.steering = nil
		return msgs
	default: // SteeringModeOneAtATime
		msg := q.steering[0]
		q.steering = q.steering[1:]
		return []*SteeringMessage{msg}
	}
}

// GetFollowUpMessages retrieves pending follow-up messages based on the configured mode.
// Called when the agent would otherwise stop to check for queued work.
func (q *SteeringQueue) GetFollowUpMessages() []*FollowUpMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.followUp) == 0 {
		return nil
	}

	switch q.followUpMode {
	case FollowUpModeAll:
		msgs := q.followUp
		q.followUp = nil
		return msgs
	default: // FollowUpModeOneAtATime
		msg := q.followUp[0]
		q.followUp = q.followUp[1:]
		return []*FollowUpMessage{msg}
	}
}

// HasSteering returns true if steering messages are queued.
func (q *SteeringQueue) HasSteering() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steering) > 0
}

// HasFollowUp returns true if follow-up messages are queued.
func (q *SteeringQueue) HasFollowUp() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.followUp) > 0
}

// Clear removes all queued steering and follow-up messages.
func (q *SteeringQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steering = nil
	q.followUp = nil
}

// ClearSteering removes all queued steering messages.
func (q *SteeringQueue) ClearSteering() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steering = nil
}

// ClearFollowUp removes all queued follow-up messages.
func (q *SteeringQueue) ClearFollowUp() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.followUp = nil
}

// SteeringQueueKey is used to store steering queue in context.
type steeringQueueKey struct{}

// WithSteeringQueue stores a steering queue in the context.
func WithSteeringQueue(ctx context.Context, queue *SteeringQueue) context.Context {
	return context.WithValue(ctx, steeringQueueKey{}, queue)
}

// SteeringQueueFromContext retrieves the steering queue from context.
func SteeringQueueFromContext(ctx context.Context) *SteeringQueue {
	queue, ok := ctx.Value(steeringQueueKey{}).(*SteeringQueue)
	if !ok {
		return nil
	}
	return queue
}

// ContextTransformFunc transforms messages before sending to the LLM.
// Use for context window management, injecting external context, etc.
type ContextTransformFunc func(ctx context.Context, messages []CompletionMessage) ([]CompletionMessage, error)

// contextTransformKey is used to store context transform in context.
type contextTransformKey struct{}

// WithContextTransform stores a context transform function in the context.
func WithContextTransform(ctx context.Context, transform ContextTransformFunc) context.Context {
	return context.WithValue(ctx, contextTransformKey{}, transform)
}

// ContextTransformFromContext retrieves the context transform from context.
func ContextTransformFromContext(ctx context.Context) ContextTransformFunc {
	transform, ok := ctx.Value(contextTransformKey{}).(ContextTransformFunc)
	if !ok {
		return nil
	}
	return transform
}

// APIKeyResolver resolves API keys dynamically for each LLM call.
// Useful for short-lived OAuth tokens that may expire during long-running operations.
type APIKeyResolver func(ctx context.Context, provider string) (string, error)

// apiKeyResolverKey is used to store API key resolver in context.
type apiKeyResolverKey struct{}

// WithAPIKeyResolver stores an API key resolver in the context.
func WithAPIKeyResolver(ctx context.Context, resolver APIKeyResolver) context.Context {
	return context.WithValue(ctx, apiKeyResolverKey{}, resolver)
}

// APIKeyResolverFromContext retrieves the API key resolver from context.
func APIKeyResolverFromContext(ctx context.Context) APIKeyResolver {
	resolver, ok := ctx.Value(apiKeyResolverKey{}).(APIKeyResolver)
	if !ok {
		return nil
	}
	return resolver
}

// resolvedAPIKeyKey is used to store a pre-resolved API key in context.
type resolvedAPIKeyKey struct{}

// WithResolvedAPIKey stores a pre-resolved API key in the context.
// This is used by the runtime to pass the resolved key to providers.
func WithResolvedAPIKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, resolvedAPIKeyKey{}, key)
}

// ResolvedAPIKeyFromContext retrieves the pre-resolved API key from context.
// Returns empty string if not set.
func ResolvedAPIKeyFromContext(ctx context.Context) string {
	key, ok := ctx.Value(resolvedAPIKeyKey{}).(string)
	if !ok {
		return ""
	}
	return key
}

// ThinkingLevel configures the reasoning/thinking depth for supported models.
type ThinkingLevel string

const (
	// ThinkingOff disables extended thinking
	ThinkingOff ThinkingLevel = "off"

	// ThinkingMinimal uses minimal thinking tokens
	ThinkingMinimal ThinkingLevel = "minimal"

	// ThinkingLow uses low thinking budget
	ThinkingLow ThinkingLevel = "low"

	// ThinkingMedium uses medium thinking budget
	ThinkingMedium ThinkingLevel = "medium"

	// ThinkingHigh uses high thinking budget
	ThinkingHigh ThinkingLevel = "high"

	// ThinkingMax uses maximum thinking budget
	ThinkingMax ThinkingLevel = "max"
)

// ThinkingBudgets maps thinking levels to token budgets.
var ThinkingBudgets = map[ThinkingLevel]int{
	ThinkingOff:     0,
	ThinkingMinimal: 1024,
	ThinkingLow:     4096,
	ThinkingMedium:  16384,
	ThinkingHigh:    65536,
	ThinkingMax:     100000,
}

// GetThinkingBudget returns the token budget for a thinking level.
func GetThinkingBudget(level ThinkingLevel) int {
	if budget, ok := ThinkingBudgets[level]; ok {
		return budget
	}
	return 0
}

// thinkingLevelKey is used to store thinking level in context.
type thinkingLevelKey struct{}

// WithThinkingLevel stores a thinking level in the context.
func WithThinkingLevel(ctx context.Context, level ThinkingLevel) context.Context {
	return context.WithValue(ctx, thinkingLevelKey{}, level)
}

// ThinkingLevelFromContext retrieves the thinking level from context.
func ThinkingLevelFromContext(ctx context.Context) ThinkingLevel {
	level, ok := ctx.Value(thinkingLevelKey{}).(ThinkingLevel)
	if !ok {
		return ThinkingOff
	}
	return level
}

// SkippedToolResult returns a tool result for a skipped tool call.
// Used when steering interrupts remaining tool calls.
func SkippedToolResult(toolCallID string, reason string) *models.ToolResult {
	if reason == "" {
		reason = "Skipped due to steering message"
	}
	return &models.ToolResult{
		ToolCallID: toolCallID,
		Content:    reason,
		IsError:    true,
	}
}

// TurnEvent represents an event in the agent turn lifecycle.
type TurnEvent string

const (
	// TurnEventStart signals the beginning of an agent turn
	TurnEventStart TurnEvent = "turn_start"

	// TurnEventEnd signals the end of an agent turn
	TurnEventEnd TurnEvent = "turn_end"

	// TurnEventSteering signals steering messages were injected
	TurnEventSteering TurnEvent = "turn_steering"

	// TurnEventToolsSkipped signals tools were skipped due to steering
	TurnEventToolsSkipped TurnEvent = "turn_tools_skipped"
)

// TurnCallback is called for turn lifecycle events.
type TurnCallback func(ctx context.Context, event TurnEvent, data map[string]any)

// turnCallbackKey is used to store turn callback in context.
type turnCallbackKey struct{}

// WithTurnCallback stores a turn callback in the context.
func WithTurnCallback(ctx context.Context, callback TurnCallback) context.Context {
	return context.WithValue(ctx, turnCallbackKey{}, callback)
}

// TurnCallbackFromContext retrieves the turn callback from context.
func TurnCallbackFromContext(ctx context.Context) TurnCallback {
	callback, ok := ctx.Value(turnCallbackKey{}).(TurnCallback)
	if !ok {
		return nil
	}
	return callback
}
