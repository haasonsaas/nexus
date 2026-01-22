package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/observability"
)

// Logger provides structured audit logging with support for various output formats
// and configurable privacy controls. Inspired by Clawdbot patterns for comprehensive
// event tracking of agent actions, tool invocations, and permission decisions.
//
// Key features:
//   - Structured logging with JSON, logfmt, or text output
//   - Hierarchical session key support (agent:agentId:mainKey)
//   - Privacy controls for sensitive data (input hashing, field truncation)
//   - Async buffered writes for performance
//   - Distributed tracing correlation (trace_id, span_id)
//   - Configurable event filtering and sampling
//
// Usage:
//
//	logger := audit.NewLogger(audit.Config{
//	    Enabled: true,
//	    Level:   audit.LevelInfo,
//	    Format:  audit.FormatJSON,
//	    Output:  "stdout",
//	})
//	defer logger.Close()
//
//	logger.LogToolInvocation(ctx, "web_search", "call-123", input)
type Logger struct {
	config     Config
	output     io.WriteCloser
	slogger    *slog.Logger
	buffer     chan *Event
	wg         sync.WaitGroup
	done       chan struct{}
	eventTypes map[EventType]bool
}

// NewLogger creates a new audit logger with the given configuration.
func NewLogger(config Config) (*Logger, error) {
	if !config.Enabled {
		return &Logger{config: config}, nil
	}

	// Set defaults
	if config.SampleRate == 0 {
		config.SampleRate = 1.0
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.FlushInterval == 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.MaxFieldSize == 0 {
		config.MaxFieldSize = 1024
	}

	// Open output
	var output io.WriteCloser
	switch {
	case config.Output == "stdout" || config.Output == "":
		output = os.Stdout
	case config.Output == "stderr":
		output = os.Stderr
	case strings.HasPrefix(config.Output, "file:"):
		path := strings.TrimPrefix(config.Output, "file:")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open audit log file: %w", err)
		}
		output = f
	default:
		return nil, fmt.Errorf("unsupported audit output: %s", config.Output)
	}

	// Build event type filter map
	eventTypes := make(map[EventType]bool)
	for _, et := range config.EventTypes {
		eventTypes[et] = true
	}

	l := &Logger{
		config:     config,
		output:     output,
		buffer:     make(chan *Event, config.BufferSize),
		done:       make(chan struct{}),
		eventTypes: eventTypes,
	}

	// Create underlying slog logger for structured output
	var handler slog.Handler
	switch config.Format {
	case FormatJSON:
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level: l.slogLevel(),
		})
	case FormatText:
		handler = slog.NewTextHandler(output, &slog.HandlerOptions{
			Level: l.slogLevel(),
		})
	default:
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level: l.slogLevel(),
		})
	}
	l.slogger = slog.New(handler).With("component", "audit")

	// Start async writer
	l.wg.Add(1)
	go l.writeLoop()

	return l, nil
}

// Close flushes remaining events and closes the logger.
func (l *Logger) Close() error {
	if !l.config.Enabled {
		return nil
	}

	close(l.done)
	l.wg.Wait()

	if l.output != os.Stdout && l.output != os.Stderr {
		return l.output.Close()
	}
	return nil
}

// Log writes an audit event to the log.
func (l *Logger) Log(ctx context.Context, event *Event) {
	if !l.config.Enabled {
		return
	}

	// Check sampling
	if l.config.SampleRate < 1.0 && rand.Float64() > l.config.SampleRate {
		return
	}

	// Check event type filter
	if len(l.eventTypes) > 0 && !l.eventTypes[event.Type] {
		return
	}

	// Check level
	if !l.shouldLog(event.Level) {
		return
	}

	// Set defaults
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Add trace context
	if event.TraceID == "" {
		event.TraceID = observability.GetTraceID(ctx)
	}
	if event.SpanID == "" {
		event.SpanID = observability.GetSpanID(ctx)
	}

	// Non-blocking write to buffer
	select {
	case l.buffer <- event:
	default:
		// Buffer full, log directly (slower but doesn't drop)
		l.writeEvent(event)
	}
}

// LogToolInvocation logs a tool invocation event.
func (l *Logger) LogToolInvocation(ctx context.Context, toolName, toolCallID string, input json.RawMessage, sessionKey string) {
	details := map[string]any{
		"tool_name":    toolName,
		"tool_call_id": toolCallID,
	}

	if l.config.IncludeToolInput && input != nil {
		inputStr := string(input)
		if len(inputStr) > l.config.MaxFieldSize {
			inputStr = inputStr[:l.config.MaxFieldSize] + "...(truncated)"
		}
		details["input"] = inputStr
	} else if input != nil {
		// Hash input for privacy
		details["input_hash"] = hashString(string(input))
	}

	l.Log(ctx, &Event{
		Type:       EventToolInvocation,
		Level:      LevelInfo,
		SessionKey: sessionKey,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Action:     "tool_invoked",
		Details:    details,
	})
}

// LogToolCompletion logs a tool completion event.
func (l *Logger) LogToolCompletion(ctx context.Context, toolName, toolCallID string, success bool, output string, duration time.Duration, sessionKey string) {
	level := LevelInfo
	if !success {
		level = LevelWarn
	}

	details := map[string]any{
		"tool_name":    toolName,
		"tool_call_id": toolCallID,
		"success":      success,
		"duration_ms":  duration.Milliseconds(),
	}

	if l.config.IncludeToolOutput && output != "" {
		outputStr := output
		if len(outputStr) > l.config.MaxFieldSize {
			outputStr = outputStr[:l.config.MaxFieldSize] + "...(truncated)"
		}
		details["output"] = outputStr
	} else if output != "" {
		details["output_size"] = len(output)
	}

	l.Log(ctx, &Event{
		Type:       EventToolCompletion,
		Level:      level,
		SessionKey: sessionKey,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Action:     "tool_completed",
		Details:    details,
		Duration:   duration,
	})
}

// LogToolDenied logs a tool denial event.
func (l *Logger) LogToolDenied(ctx context.Context, toolName, toolCallID, reason, policyMatched, sessionKey string) {
	l.Log(ctx, &Event{
		Type:       EventToolDenied,
		Level:      LevelWarn,
		SessionKey: sessionKey,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Action:     "tool_denied",
		Details: map[string]any{
			"reason":         reason,
			"policy_matched": policyMatched,
		},
	})
}

// LogPermissionDecision logs a permission grant or denial.
func (l *Logger) LogPermissionDecision(ctx context.Context, granted bool, permission, resource, action, reason, sessionKey string) {
	eventType := EventPermissionGranted
	level := LevelInfo
	if !granted {
		eventType = EventPermissionDenied
		level = LevelWarn
	}

	l.Log(ctx, &Event{
		Type:       eventType,
		Level:      level,
		SessionKey: sessionKey,
		Action:     "permission_" + action,
		Details: map[string]any{
			"permission": permission,
			"resource":   resource,
			"granted":    granted,
			"reason":     reason,
		},
	})
}

// LogAgentHandoff logs an agent handoff event.
func (l *Logger) LogAgentHandoff(ctx context.Context, fromAgent, toAgent, reason, contextMode string, depth int, sessionKey string) {
	l.Log(ctx, &Event{
		Type:       EventAgentHandoff,
		Level:      LevelInfo,
		SessionKey: sessionKey,
		AgentID:    toAgent,
		Action:     "agent_handoff",
		Details: map[string]any{
			"from_agent_id": fromAgent,
			"to_agent_id":   toAgent,
			"reason":        reason,
			"context_mode":  contextMode,
			"handoff_depth": depth,
		},
	})
}

// LogSessionCompact logs a session compaction event.
func (l *Logger) LogSessionCompact(ctx context.Context, sessionID, sessionKey string, before, after, tokensSaved int, strategy string) {
	l.Log(ctx, &Event{
		Type:       EventSessionCompact,
		Level:      LevelInfo,
		SessionID:  sessionID,
		SessionKey: sessionKey,
		Action:     "session_compacted",
		Details: map[string]any{
			"messages_before_compact": before,
			"messages_after_compact":  after,
			"tokens_saved":            tokensSaved,
			"compaction_strategy":     strategy,
		},
	})
}

// LogAgentAction logs a general agent action.
func (l *Logger) LogAgentAction(ctx context.Context, agentID, action, description string, details map[string]any, sessionKey string) {
	if details == nil {
		details = make(map[string]any)
	}
	details["description"] = description

	l.Log(ctx, &Event{
		Type:       EventAgentAction,
		Level:      LevelInfo,
		SessionKey: sessionKey,
		AgentID:    agentID,
		Action:     action,
		Details:    details,
	})
}

// LogError logs an error event.
func (l *Logger) LogError(ctx context.Context, eventType EventType, action, errorMsg string, details map[string]any, sessionKey string) {
	l.Log(ctx, &Event{
		Type:       eventType,
		Level:      LevelError,
		SessionKey: sessionKey,
		Action:     action,
		Error:      errorMsg,
		Details:    details,
	})
}

// WithSessionKey returns a context-bound logger with the session key pre-set.
func (l *Logger) WithSessionKey(sessionKey string) *SessionLogger {
	return &SessionLogger{
		logger:     l,
		sessionKey: sessionKey,
	}
}

// writeLoop processes buffered events.
func (l *Logger) writeLoop() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case event := <-l.buffer:
			l.writeEvent(event)
		case <-ticker.C:
			// Flush any remaining buffered events
			l.flushBuffer()
		case <-l.done:
			// Drain remaining events
			l.flushBuffer()
			return
		}
	}
}

// flushBuffer drains all buffered events.
func (l *Logger) flushBuffer() {
	for {
		select {
		case event := <-l.buffer:
			l.writeEvent(event)
		default:
			return
		}
	}
}

// writeEvent writes a single event to the output.
func (l *Logger) writeEvent(event *Event) {
	attrs := []any{
		"audit_id", event.ID,
		"audit_type", event.Type,
		"action", event.Action,
		"timestamp", event.Timestamp.Format(time.RFC3339Nano),
	}

	if event.SessionID != "" {
		attrs = append(attrs, "session_id", event.SessionID)
	}
	if event.SessionKey != "" {
		attrs = append(attrs, "session_key", event.SessionKey)
	}
	if event.AgentID != "" {
		attrs = append(attrs, "agent_id", event.AgentID)
	}
	if event.ToolName != "" {
		attrs = append(attrs, "tool_name", event.ToolName)
	}
	if event.ToolCallID != "" {
		attrs = append(attrs, "tool_call_id", event.ToolCallID)
	}
	if event.UserID != "" {
		attrs = append(attrs, "user_id", event.UserID)
	}
	if event.Channel != "" {
		attrs = append(attrs, "channel", event.Channel)
	}
	if event.TraceID != "" {
		attrs = append(attrs, "trace_id", event.TraceID)
	}
	if event.SpanID != "" {
		attrs = append(attrs, "span_id", event.SpanID)
	}
	if event.ParentEventID != "" {
		attrs = append(attrs, "parent_event_id", event.ParentEventID)
	}
	if event.Duration > 0 {
		attrs = append(attrs, "duration_ms", event.Duration.Milliseconds())
	}
	if event.Error != "" {
		attrs = append(attrs, "error", event.Error)
	}

	// Add details as individual attributes for better querying
	for k, v := range event.Details {
		attrs = append(attrs, k, v)
	}

	switch event.Level {
	case LevelDebug:
		l.slogger.Debug("audit", attrs...)
	case LevelInfo:
		l.slogger.Info("audit", attrs...)
	case LevelWarn:
		l.slogger.Warn("audit", attrs...)
	case LevelError:
		l.slogger.Error("audit", attrs...)
	}
}

// shouldLog checks if an event at the given level should be logged.
func (l *Logger) shouldLog(level Level) bool {
	levels := map[Level]int{
		LevelDebug: 0,
		LevelInfo:  1,
		LevelWarn:  2,
		LevelError: 3,
	}
	return levels[level] >= levels[l.config.Level]
}

// slogLevel converts audit level to slog level.
func (l *Logger) slogLevel() slog.Level {
	switch l.config.Level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// hashString creates a SHA256 hash of a string (first 16 chars).
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// SessionLogger is a logger bound to a specific session.
type SessionLogger struct {
	logger     *Logger
	sessionKey string
}

// LogToolInvocation logs a tool invocation with the pre-set session key.
func (s *SessionLogger) LogToolInvocation(ctx context.Context, toolName, toolCallID string, input json.RawMessage) {
	s.logger.LogToolInvocation(ctx, toolName, toolCallID, input, s.sessionKey)
}

// LogToolCompletion logs a tool completion with the pre-set session key.
func (s *SessionLogger) LogToolCompletion(ctx context.Context, toolName, toolCallID string, success bool, output string, duration time.Duration) {
	s.logger.LogToolCompletion(ctx, toolName, toolCallID, success, output, duration, s.sessionKey)
}

// LogToolDenied logs a tool denial with the pre-set session key.
func (s *SessionLogger) LogToolDenied(ctx context.Context, toolName, toolCallID, reason, policyMatched string) {
	s.logger.LogToolDenied(ctx, toolName, toolCallID, reason, policyMatched, s.sessionKey)
}

// LogPermissionDecision logs a permission decision with the pre-set session key.
func (s *SessionLogger) LogPermissionDecision(ctx context.Context, granted bool, permission, resource, action, reason string) {
	s.logger.LogPermissionDecision(ctx, granted, permission, resource, action, reason, s.sessionKey)
}

// LogAgentHandoff logs an agent handoff with the pre-set session key.
func (s *SessionLogger) LogAgentHandoff(ctx context.Context, fromAgent, toAgent, reason, contextMode string, depth int) {
	s.logger.LogAgentHandoff(ctx, fromAgent, toAgent, reason, contextMode, depth, s.sessionKey)
}

// LogAgentAction logs an agent action with the pre-set session key.
func (s *SessionLogger) LogAgentAction(ctx context.Context, agentID, action, description string, details map[string]any) {
	s.logger.LogAgentAction(ctx, agentID, action, description, details, s.sessionKey)
}

// LogError logs an error with the pre-set session key.
func (s *SessionLogger) LogError(ctx context.Context, eventType EventType, action, errorMsg string, details map[string]any) {
	s.logger.LogError(ctx, eventType, action, errorMsg, details, s.sessionKey)
}

// Global logger instance for convenience.
var globalLogger *Logger
var globalMu sync.RWMutex

// SetGlobalLogger sets the global audit logger.
func SetGlobalLogger(logger *Logger) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalLogger = logger
}

// GetGlobalLogger returns the global audit logger.
func GetGlobalLogger() *Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalLogger
}

// Log logs an event using the global logger.
func Log(ctx context.Context, event *Event) {
	if l := GetGlobalLogger(); l != nil {
		l.Log(ctx, event)
	}
}
