package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/haasonsaas/nexus/internal/audit"
	"github.com/haasonsaas/nexus/internal/auth"
)

// Manager coordinates persistence and realtime broadcasts.
type Manager struct {
	store       Store
	hub         *Hub
	logger      *slog.Logger
	auditLogger *audit.Logger
	metrics     *Metrics
}

// NewManager creates a canvas manager.
func NewManager(store Store, logger *slog.Logger) *Manager {
	if store == nil {
		store = NewMemoryStore()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		store:  store,
		hub:    NewHub(),
		logger: logger.With("component", "canvas"),
	}
}

// Store returns the configured store.
func (m *Manager) Store() Store {
	if m == nil {
		return nil
	}
	return m.store
}

// Hub returns the realtime hub.
func (m *Manager) Hub() *Hub {
	if m == nil {
		return nil
	}
	return m.hub
}

func (m *Manager) SetAuditLogger(logger *audit.Logger) {
	if m == nil {
		return
	}
	m.auditLogger = logger
}

func (m *Manager) SetMetrics(metrics *Metrics) {
	if m == nil {
		return
	}
	m.metrics = metrics
}

// Push appends an event and broadcasts it to subscribers.
func (m *Manager) Push(ctx context.Context, sessionID string, payload json.RawMessage) (*StreamMessage, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("canvas manager unavailable")
	}
	if err := m.store.AppendEvent(ctx, &Event{
		SessionID: sessionID,
		Type:      "event",
		Payload:   payload,
		CreatedAt: time.Now(),
	}); err != nil {
		return nil, err
	}
	m.recordStateChange(ctx, sessionID, audit.EventCanvasUpdate, "canvas.update", payload)
	if m.metrics != nil {
		m.metrics.RecordUpdate()
	}

	msg := StreamMessage{
		Type:      "event",
		SessionID: sessionID,
		Payload:   payload,
		Timestamp: time.Now(),
	}
	m.hub.Broadcast(msg)
	return &msg, nil
}

// Reset replaces the stored state and broadcasts a reset message.
func (m *Manager) Reset(ctx context.Context, sessionID string, state json.RawMessage) (*StreamMessage, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("canvas manager unavailable")
	}
	if err := m.store.UpsertState(ctx, &State{
		SessionID: sessionID,
		StateJSON: state,
		UpdatedAt: time.Now(),
	}); err != nil {
		return nil, err
	}
	m.recordStateChange(ctx, sessionID, audit.EventCanvasReset, "canvas.reset", state)
	if m.metrics != nil {
		m.metrics.RecordUpdate()
	}

	msg := StreamMessage{
		Type:      "reset",
		SessionID: sessionID,
		Payload:   state,
		Timestamp: time.Now(),
	}
	m.hub.Broadcast(msg)
	return &msg, nil
}

// Snapshot returns the current snapshot state and event log.
func (m *Manager) Snapshot(ctx context.Context, sessionID string) (*State, []*Event, error) {
	if m == nil || m.store == nil {
		return nil, nil, errors.New("canvas manager unavailable")
	}
	var state *State
	stored, err := m.store.GetState(ctx, sessionID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, nil, err
		}
	} else {
		state = stored
	}
	events, err := m.store.ListEvents(ctx, sessionID, EventListOptions{})
	if err != nil {
		return nil, nil, err
	}
	return state, events, nil
}

func (m *Manager) recordStateChange(ctx context.Context, sessionID string, eventType audit.EventType, action string, payload json.RawMessage) {
	if m == nil || m.auditLogger == nil {
		return
	}
	userID := ""
	if user, ok := auth.UserFromContext(ctx); ok && user != nil {
		userID = user.ID
	}
	details := map[string]any{
		"canvas_session_id": sessionID,
		"payload_bytes":     len(payload),
	}
	m.auditLogger.Log(ctx, &audit.Event{
		Type:      eventType,
		Level:     audit.LevelInfo,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Action:    action,
		Details:   details,
		UserID:    userID,
	})
}
