package canvas

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryStore provides an in-memory canvas store for testing and local usage.
type MemoryStore struct {
	mu              sync.RWMutex
	sessions        map[string]*Session
	sessionByKey    map[string]string
	states          map[string]*State
	eventsBySession map[string][]*Event
}

// NewMemoryStore creates a new in-memory canvas store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:        map[string]*Session{},
		sessionByKey:    map[string]string{},
		states:          map[string]*State{},
		eventsBySession: map[string][]*Event{},
	}
}

func (s *MemoryStore) CreateSession(_ context.Context, session *Session) error {
	if session == nil {
		return ErrNotFound
	}
	if session.ID == "" {
		session.ID = uuid.NewString()
	}
	if session.Key == "" {
		return ErrNotFound
	}
	now := time.Now()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = session.CreatedAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[session.ID]; exists {
		return ErrAlreadyExists
	}
	if existingID, exists := s.sessionByKey[session.Key]; exists && existingID != session.ID {
		return ErrAlreadyExists
	}
	s.sessions[session.ID] = cloneSession(session)
	s.sessionByKey[session.Key] = session.ID
	return nil
}

func (s *MemoryStore) GetSession(_ context.Context, id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneSession(session), nil
}

func (s *MemoryStore) GetSessionByKey(_ context.Context, key string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.sessionByKey[key]
	if !ok {
		return nil, ErrNotFound
	}
	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneSession(session), nil
}

func (s *MemoryStore) UpdateSession(_ context.Context, session *Session) error {
	if session == nil || session.ID == "" {
		return ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.sessions[session.ID]
	if !ok {
		return ErrNotFound
	}
	if session.Key == "" {
		session.Key = stored.Key
	}
	if existingID, exists := s.sessionByKey[session.Key]; exists && existingID != session.ID {
		return ErrAlreadyExists
	}

	oldKey := stored.Key
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = time.Now()
	}
	s.sessions[session.ID] = cloneSession(session)
	if oldKey != session.Key {
		delete(s.sessionByKey, oldKey)
		s.sessionByKey[session.Key] = session.ID
	}
	return nil
}

func (s *MemoryStore) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.sessions, id)
	delete(s.sessionByKey, session.Key)
	delete(s.states, id)
	delete(s.eventsBySession, id)
	return nil
}

func (s *MemoryStore) UpsertState(_ context.Context, state *State) error {
	if state == nil || state.SessionID == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[state.SessionID]; !ok {
		return ErrNotFound
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now()
	}
	s.states[state.SessionID] = cloneState(state)
	return nil
}

func (s *MemoryStore) GetState(_ context.Context, sessionID string) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneState(state), nil
}

func (s *MemoryStore) DeleteState(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.states[sessionID]; !ok {
		return ErrNotFound
	}
	delete(s.states, sessionID)
	return nil
}

func (s *MemoryStore) AppendEvent(_ context.Context, event *Event) error {
	if event == nil || event.SessionID == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[event.SessionID]; !ok {
		return ErrNotFound
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	s.eventsBySession[event.SessionID] = append(s.eventsBySession[event.SessionID], cloneEvent(event))
	return nil
}

func (s *MemoryStore) ListEvents(_ context.Context, sessionID string, opts EventListOptions) ([]*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events, ok := s.eventsBySession[sessionID]
	if !ok {
		return []*Event{}, nil
	}
	filtered := make([]*Event, 0, len(events))
	for _, event := range events {
		if !opts.Since.IsZero() && event.CreatedAt.Before(opts.Since) {
			continue
		}
		filtered = append(filtered, cloneEvent(event))
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	return filtered, nil
}

func (s *MemoryStore) DeleteEvents(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.eventsBySession[sessionID]; !ok {
		return ErrNotFound
	}
	delete(s.eventsBySession, sessionID)
	return nil
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	clone := *session
	return &clone
}

func cloneState(state *State) *State {
	if state == nil {
		return nil
	}
	clone := *state
	if state.StateJSON != nil {
		clone.StateJSON = append([]byte(nil), state.StateJSON...)
	}
	return &clone
}

func cloneEvent(event *Event) *Event {
	if event == nil {
		return nil
	}
	clone := *event
	if event.Payload != nil {
		clone.Payload = append([]byte(nil), event.Payload...)
	}
	return &clone
}
