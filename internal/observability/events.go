// Package observability provides logging, tracing, and event timeline capabilities.
// This file implements the event timeline for debugging and replaying runs.
package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Additional context keys for correlation IDs
const (
	// RunIDKey is the context key for run IDs (a single agent run/turn).
	RunIDKey ContextKey = "run_id"

	// ToolCallIDKey is the context key for tool call IDs.
	ToolCallIDKey ContextKey = "tool_call_id"

	// EdgeIDKey is the context key for edge daemon IDs.
	EdgeIDKey ContextKey = "edge_id"

	// AgentIDKey is the context key for agent IDs.
	AgentIDKey ContextKey = "agent_id"

	// MessageIDKey is the context key for message IDs.
	MessageIDKey ContextKey = "message_id"
)

// AddRunID adds a run ID to the context.
func AddRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, RunIDKey, runID)
}

// GetRunID retrieves the run ID from the context.
func GetRunID(ctx context.Context) string {
	if id, ok := ctx.Value(RunIDKey).(string); ok {
		return id
	}
	return ""
}

// AddToolCallID adds a tool call ID to the context.
func AddToolCallID(ctx context.Context, toolCallID string) context.Context {
	return context.WithValue(ctx, ToolCallIDKey, toolCallID)
}

// GetToolCallID retrieves the tool call ID from the context.
func GetToolCallID(ctx context.Context) string {
	if id, ok := ctx.Value(ToolCallIDKey).(string); ok {
		return id
	}
	return ""
}

// AddEdgeID adds an edge ID to the context.
func AddEdgeID(ctx context.Context, edgeID string) context.Context {
	return context.WithValue(ctx, EdgeIDKey, edgeID)
}

// GetEdgeID retrieves the edge ID from the context.
func GetEdgeID(ctx context.Context) string {
	if id, ok := ctx.Value(EdgeIDKey).(string); ok {
		return id
	}
	return ""
}

// AddAgentID adds an agent ID to the context.
func AddAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, AgentIDKey, agentID)
}

// GetAgentID retrieves the agent ID from the context.
func GetAgentID(ctx context.Context) string {
	if id, ok := ctx.Value(AgentIDKey).(string); ok {
		return id
	}
	return ""
}

// AddMessageID adds a message ID to the context.
func AddMessageID(ctx context.Context, messageID string) context.Context {
	return context.WithValue(ctx, MessageIDKey, messageID)
}

// GetMessageID retrieves the message ID from the context.
func GetMessageID(ctx context.Context) string {
	if id, ok := ctx.Value(MessageIDKey).(string); ok {
		return id
	}
	return ""
}

// EventType categorizes events for filtering and display.
type EventType string

const (
	EventTypeRunStart       EventType = "run.start"
	EventTypeRunEnd         EventType = "run.end"
	EventTypeRunError       EventType = "run.error"
	EventTypeToolStart      EventType = "tool.start"
	EventTypeToolEnd        EventType = "tool.end"
	EventTypeToolError      EventType = "tool.error"
	EventTypeToolProgress   EventType = "tool.progress"
	EventTypeEdgeConnect    EventType = "edge.connect"
	EventTypeEdgeDisconnect EventType = "edge.disconnect"
	EventTypeEdgeHeartbeat  EventType = "edge.heartbeat"
	EventTypeApprovalReq    EventType = "approval.required"
	EventTypeApprovalDec    EventType = "approval.decided"
	EventTypeLLMRequest     EventType = "llm.request"
	EventTypeLLMResponse    EventType = "llm.response"
	EventTypeLLMError       EventType = "llm.error"
	EventTypeMessage        EventType = "message"
	EventTypeCustom         EventType = "custom"
)

// Event represents a single event in the timeline.
type Event struct {
	ID          string                 `json:"id"`
	Type        EventType              `json:"type"`
	Timestamp   time.Time              `json:"timestamp"`
	RunID       string                 `json:"run_id,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	ToolCallID  string                 `json:"tool_call_id,omitempty"`
	EdgeID      string                 `json:"edge_id,omitempty"`
	AgentID     string                 `json:"agent_id,omitempty"`
	MessageID   string                 `json:"message_id,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Duration    time.Duration          `json:"duration_ns,omitempty"`
	Error       string                 `json:"error,omitempty"`
	ParentID    string                 `json:"parent_id,omitempty"`
	TraceID     string                 `json:"trace_id,omitempty"`
	SpanID      string                 `json:"span_id,omitempty"`
}

// EventStore stores and retrieves events for debugging.
type EventStore interface {
	// Record stores an event.
	Record(event *Event) error

	// GetByRunID returns all events for a run, sorted by timestamp.
	GetByRunID(runID string) ([]*Event, error)

	// GetBySessionID returns all events for a session, sorted by timestamp.
	GetBySessionID(sessionID string) ([]*Event, error)

	// GetByTimeRange returns events within a time range.
	GetByTimeRange(start, end time.Time) ([]*Event, error)

	// GetByType returns events of a specific type.
	GetByType(eventType EventType, limit int) ([]*Event, error)

	// Get returns a single event by ID.
	Get(id string) (*Event, error)

	// Delete removes events older than the given duration.
	Delete(olderThan time.Duration) (int, error)
}

// MemoryEventStore is an in-memory implementation of EventStore.
type MemoryEventStore struct {
	mu       sync.RWMutex
	events   map[string]*Event
	byRunID  map[string][]string // runID -> eventIDs
	bySession map[string][]string // sessionID -> eventIDs
	maxSize  int
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore(maxSize int) *MemoryEventStore {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &MemoryEventStore{
		events:    make(map[string]*Event),
		byRunID:   make(map[string][]string),
		bySession: make(map[string][]string),
		maxSize:   maxSize,
	}
}

func (s *MemoryEventStore) Record(event *Event) error {
	if event == nil {
		return errors.New("event cannot be nil")
	}
	if event.ID == "" {
		event.ID = generateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Enforce max size
	if len(s.events) >= s.maxSize {
		s.evictOldest()
	}

	s.events[event.ID] = event

	if event.RunID != "" {
		s.byRunID[event.RunID] = append(s.byRunID[event.RunID], event.ID)
	}
	if event.SessionID != "" {
		s.bySession[event.SessionID] = append(s.bySession[event.SessionID], event.ID)
	}

	return nil
}

func (s *MemoryEventStore) GetByRunID(runID string) ([]*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byRunID[runID]
	events := make([]*Event, 0, len(ids))
	for _, id := range ids {
		if e, ok := s.events[id]; ok {
			events = append(events, e)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	return events, nil
}

func (s *MemoryEventStore) GetBySessionID(sessionID string) ([]*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.bySession[sessionID]
	events := make([]*Event, 0, len(ids))
	for _, id := range ids {
		if e, ok := s.events[id]; ok {
			events = append(events, e)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	return events, nil
}

func (s *MemoryEventStore) GetByTimeRange(start, end time.Time) ([]*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var events []*Event
	for _, e := range s.events {
		if (e.Timestamp.Equal(start) || e.Timestamp.After(start)) &&
			(e.Timestamp.Equal(end) || e.Timestamp.Before(end)) {
			events = append(events, e)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	return events, nil
}

func (s *MemoryEventStore) GetByType(eventType EventType, limit int) ([]*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var events []*Event
	for _, e := range s.events {
		if e.Type == eventType {
			events = append(events, e)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp) // Most recent first
	})

	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}

	return events, nil
}

func (s *MemoryEventStore) Get(id string) (*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.events[id]
	if !ok {
		return nil, fmt.Errorf("event not found: %s", id)
	}
	return e, nil
}

func (s *MemoryEventStore) Delete(olderThan time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	deleted := 0

	for id, e := range s.events {
		if e.Timestamp.Before(cutoff) {
			delete(s.events, id)
			deleted++
		}
	}

	// Clean up indices
	for runID, ids := range s.byRunID {
		var remaining []string
		for _, id := range ids {
			if _, ok := s.events[id]; ok {
				remaining = append(remaining, id)
			}
		}
		if len(remaining) == 0 {
			delete(s.byRunID, runID)
		} else {
			s.byRunID[runID] = remaining
		}
	}

	for sessionID, ids := range s.bySession {
		var remaining []string
		for _, id := range ids {
			if _, ok := s.events[id]; ok {
				remaining = append(remaining, id)
			}
		}
		if len(remaining) == 0 {
			delete(s.bySession, sessionID)
		} else {
			s.bySession[sessionID] = remaining
		}
	}

	return deleted, nil
}

func (s *MemoryEventStore) evictOldest() {
	// Find and remove oldest 10% of events
	toRemove := s.maxSize / 10
	if toRemove < 1 {
		toRemove = 1
	}

	var events []*Event
	for _, e := range s.events {
		events = append(events, e)
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	for i := 0; i < toRemove && i < len(events); i++ {
		delete(s.events, events[i].ID)
	}
}

// EventRecorder provides a convenient API for recording events.
type EventRecorder struct {
	store  EventStore
	logger *Logger
}

// NewEventRecorder creates a new event recorder.
func NewEventRecorder(store EventStore, logger *Logger) *EventRecorder {
	return &EventRecorder{
		store:  store,
		logger: logger,
	}
}

// Record records an event, extracting correlation IDs from context.
func (r *EventRecorder) Record(ctx context.Context, eventType EventType, name string, data map[string]interface{}) error {
	event := &Event{
		ID:          generateEventID(),
		Type:        eventType,
		Timestamp:   time.Now(),
		RunID:       GetRunID(ctx),
		SessionID:   GetSessionID(ctx),
		ToolCallID:  GetToolCallID(ctx),
		EdgeID:      GetEdgeID(ctx),
		AgentID:     GetAgentID(ctx),
		MessageID:   GetMessageID(ctx),
		Name:        name,
		Data:        data,
		TraceID:     GetTraceID(ctx),
		SpanID:      GetSpanID(ctx),
	}

	if r.logger != nil {
		r.logger.Debug(ctx, "event recorded",
			"event_type", string(eventType),
			"event_name", name,
			"event_id", event.ID,
		)
	}

	return r.store.Record(event)
}

// RecordError records an error event.
func (r *EventRecorder) RecordError(ctx context.Context, eventType EventType, name string, err error, data map[string]interface{}) error {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["error"] = err.Error()

	event := &Event{
		ID:          generateEventID(),
		Type:        eventType,
		Timestamp:   time.Now(),
		RunID:       GetRunID(ctx),
		SessionID:   GetSessionID(ctx),
		ToolCallID:  GetToolCallID(ctx),
		EdgeID:      GetEdgeID(ctx),
		AgentID:     GetAgentID(ctx),
		MessageID:   GetMessageID(ctx),
		Name:        name,
		Data:        data,
		Error:       err.Error(),
		TraceID:     GetTraceID(ctx),
		SpanID:      GetSpanID(ctx),
	}

	if r.logger != nil {
		r.logger.Error(ctx, "error event recorded",
			"event_type", string(eventType),
			"event_name", name,
			"event_id", event.ID,
			"error", err,
		)
	}

	return r.store.Record(event)
}

// RecordToolStart records a tool execution start event.
func (r *EventRecorder) RecordToolStart(ctx context.Context, toolName string, input interface{}) error {
	data := map[string]interface{}{
		"tool_name": toolName,
	}
	if input != nil {
		if b, err := json.Marshal(input); err == nil {
			data["input"] = string(b)
		}
	}
	return r.Record(ctx, EventTypeToolStart, toolName, data)
}

// RecordToolEnd records a tool execution end event.
func (r *EventRecorder) RecordToolEnd(ctx context.Context, toolName string, duration time.Duration, output interface{}, err error) error {
	data := map[string]interface{}{
		"tool_name":   toolName,
		"duration_ms": duration.Milliseconds(),
	}
	if output != nil {
		if b, err := json.Marshal(output); err == nil {
			data["output"] = string(b)
		}
	}

	if err != nil {
		data["error"] = err.Error()
		return r.RecordError(ctx, EventTypeToolError, toolName, err, data)
	}

	return r.Record(ctx, EventTypeToolEnd, toolName, data)
}

// RecordRunStart records a run start event.
func (r *EventRecorder) RecordRunStart(ctx context.Context, runID string, data map[string]interface{}) error {
	ctx = AddRunID(ctx, runID)
	return r.Record(ctx, EventTypeRunStart, "run_start", data)
}

// RecordRunEnd records a run end event.
func (r *EventRecorder) RecordRunEnd(ctx context.Context, duration time.Duration, err error) error {
	data := map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
	}
	if err != nil {
		return r.RecordError(ctx, EventTypeRunError, "run_error", err, data)
	}
	return r.Record(ctx, EventTypeRunEnd, "run_end", data)
}

// RecordEdgeEvent records an edge daemon event.
func (r *EventRecorder) RecordEdgeEvent(ctx context.Context, eventType EventType, edgeID string, data map[string]interface{}) error {
	ctx = AddEdgeID(ctx, edgeID)
	if data == nil {
		data = make(map[string]interface{})
	}
	data["edge_id"] = edgeID
	return r.Record(ctx, eventType, string(eventType), data)
}

// Timeline represents a sequence of events for display.
type Timeline struct {
	RunID     string   `json:"run_id"`
	SessionID string   `json:"session_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  time.Duration `json:"duration"`
	Events    []*Event `json:"events"`
	Summary   *TimelineSummary `json:"summary"`
}

// TimelineSummary provides aggregate statistics for a timeline.
type TimelineSummary struct {
	TotalEvents   int           `json:"total_events"`
	ErrorCount    int           `json:"error_count"`
	ToolCalls     int           `json:"tool_calls"`
	LLMCalls      int           `json:"llm_calls"`
	EdgeEvents    int           `json:"edge_events"`
	TotalDuration time.Duration `json:"total_duration"`
}

// BuildTimeline creates a timeline from events.
func BuildTimeline(events []*Event) *Timeline {
	if len(events) == 0 {
		return &Timeline{Summary: &TimelineSummary{}}
	}

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	timeline := &Timeline{
		Events:    events,
		StartTime: events[0].Timestamp,
		EndTime:   events[len(events)-1].Timestamp,
		Duration:  events[len(events)-1].Timestamp.Sub(events[0].Timestamp),
		Summary:   &TimelineSummary{TotalEvents: len(events)},
	}

	// Extract run/session ID from first event
	for _, e := range events {
		if e.RunID != "" && timeline.RunID == "" {
			timeline.RunID = e.RunID
		}
		if e.SessionID != "" && timeline.SessionID == "" {
			timeline.SessionID = e.SessionID
		}
		if timeline.RunID != "" && timeline.SessionID != "" {
			break
		}
	}

	// Compute summary
	for _, e := range events {
		if e.Error != "" {
			timeline.Summary.ErrorCount++
		}
		switch e.Type {
		case EventTypeToolStart, EventTypeToolEnd, EventTypeToolError:
			if e.Type == EventTypeToolStart {
				timeline.Summary.ToolCalls++
			}
		case EventTypeLLMRequest, EventTypeLLMResponse, EventTypeLLMError:
			if e.Type == EventTypeLLMRequest {
				timeline.Summary.LLMCalls++
			}
		case EventTypeEdgeConnect, EventTypeEdgeDisconnect, EventTypeEdgeHeartbeat:
			timeline.Summary.EdgeEvents++
		}
		timeline.Summary.TotalDuration += e.Duration
	}

	return timeline
}

// FormatTimeline formats a timeline for display.
func FormatTimeline(timeline *Timeline) string {
	if timeline == nil || len(timeline.Events) == 0 {
		return "No events found"
	}

	var result string
	result += fmt.Sprintf("=== Timeline for Run: %s ===\n", timeline.RunID)
	result += fmt.Sprintf("Session: %s\n", timeline.SessionID)
	result += fmt.Sprintf("Duration: %v\n", timeline.Duration)
	result += fmt.Sprintf("Events: %d (Errors: %d)\n", timeline.Summary.TotalEvents, timeline.Summary.ErrorCount)
	result += fmt.Sprintf("Tool calls: %d, LLM calls: %d, Edge events: %d\n\n",
		timeline.Summary.ToolCalls, timeline.Summary.LLMCalls, timeline.Summary.EdgeEvents)

	for i, e := range timeline.Events {
		prefix := "├─"
		if i == len(timeline.Events)-1 {
			prefix = "└─"
		}

		timestamp := e.Timestamp.Format("15:04:05.000")
		errorMark := ""
		if e.Error != "" {
			errorMark = " ❌"
		}

		result += fmt.Sprintf("%s [%s] %s: %s%s\n", prefix, timestamp, e.Type, e.Name, errorMark)

		if e.Duration > 0 {
			result += fmt.Sprintf("   Duration: %v\n", e.Duration)
		}
		if e.EdgeID != "" {
			result += fmt.Sprintf("   Edge: %s\n", e.EdgeID)
		}
		if e.Error != "" {
			result += fmt.Sprintf("   Error: %s\n", e.Error)
		}
	}

	return result
}

var eventIDCounter int64
var eventIDMu sync.Mutex

func generateEventID() string {
	eventIDMu.Lock()
	defer eventIDMu.Unlock()
	eventIDCounter++
	return fmt.Sprintf("evt_%d_%d", time.Now().UnixNano(), eventIDCounter)
}
