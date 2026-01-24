package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// Manager coordinates persistence and realtime broadcasts.
type Manager struct {
	store  Store
	hub    *Hub
	logger *slog.Logger
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
