package sessions

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
)

// maxMessagesPerSession limits messages stored per session to prevent unbounded memory growth.
// When exceeded, old messages are trimmed to maintain the limit.
const maxMessagesPerSession = 1000

// MemoryStore provides an in-memory Store implementation for testing and local runs.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*models.Session
	byKey    map[string]string
	messages map[string][]*models.Message
}

// NewMemoryStore creates a new in-memory session store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: map[string]*models.Session{},
		byKey:    map[string]string{},
		messages: map[string][]*models.Message{},
	}
}

func (m *MemoryStore) Create(ctx context.Context, session *models.Session) error {
	if session == nil {
		return errors.New("session is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	clone := cloneSession(session)
	if clone.ID == "" {
		clone.ID = uuid.NewString()
	}
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = time.Now()
	}
	clone.UpdatedAt = clone.CreatedAt
	// Reflect generated fields back to caller.
	session.ID = clone.ID
	session.CreatedAt = clone.CreatedAt
	session.UpdatedAt = clone.UpdatedAt
	m.sessions[clone.ID] = clone
	if clone.Key != "" {
		m.byKey[clone.Key] = clone.ID
	}
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id string) (*models.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return cloneSession(session), nil
}

func (m *MemoryStore) Update(ctx context.Context, session *models.Session) error {
	if session == nil {
		return errors.New("session is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.sessions[session.ID]
	if !ok {
		return errors.New("session not found")
	}
	clone := cloneSession(session)
	clone.CreatedAt = existing.CreatedAt
	clone.UpdatedAt = time.Now()
	m.sessions[clone.ID] = clone
	if clone.Key != "" {
		m.byKey[clone.Key] = clone.ID
	}
	return nil
}

func (m *MemoryStore) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[id]
	if !ok {
		return errors.New("session not found")
	}
	delete(m.sessions, id)
	if session.Key != "" {
		delete(m.byKey, session.Key)
	}
	delete(m.messages, id)
	return nil
}

func (m *MemoryStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.byKey[key]
	if !ok {
		return nil, errors.New("session not found")
	}
	session, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	return cloneSession(session), nil
}

func (m *MemoryStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id, ok := m.byKey[key]; ok {
		if session, ok := m.sessions[id]; ok {
			return cloneSession(session), nil
		}
	}

	now := time.Now()
	session := &models.Session{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Channel:   channel,
		ChannelID: channelID,
		Key:       key,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.sessions[session.ID] = session
	m.byKey[key] = session.ID
	return cloneSession(session), nil
}

func (m *MemoryStore) List(ctx context.Context, agentID string, opts ListOptions) ([]*models.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []*models.Session
	for _, session := range m.sessions {
		if agentID != "" && session.AgentID != agentID {
			continue
		}
		if opts.Channel != "" && session.Channel != opts.Channel {
			continue
		}
		out = append(out, cloneSession(session))
	}

	start := opts.Offset
	if start < 0 {
		start = 0
	}
	end := len(out)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	if start > len(out) {
		return []*models.Session{}, nil
	}
	return out[start:end], nil
}

func (m *MemoryStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	if msg == nil {
		return errors.New("message is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sessions[sessionID]; !ok {
		return errors.New("session not found")
	}
	clone := cloneMessage(msg)
	if clone.ID == "" {
		clone.ID = uuid.NewString()
	}
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = time.Now()
	}
	m.messages[sessionID] = append(m.messages[sessionID], clone)

	// Trim old messages if limit is exceeded to prevent unbounded memory growth
	if len(m.messages[sessionID]) > maxMessagesPerSession {
		// Keep the most recent messages
		excess := len(m.messages[sessionID]) - maxMessagesPerSession
		m.messages[sessionID] = m.messages[sessionID][excess:]
	}
	return nil
}

func (m *MemoryStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages := m.messages[sessionID]
	if len(messages) == 0 {
		return []*models.Message{}, nil
	}
	start := 0
	if limit > 0 && len(messages) > limit {
		start = len(messages) - limit
	}
	out := make([]*models.Message, 0, len(messages)-start)
	for _, msg := range messages[start:] {
		out = append(out, cloneMessage(msg))
	}
	return out, nil
}

// deepCloneMap creates a deep copy of a map[string]any to prevent shared references.
func deepCloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	clone := make(map[string]any, len(m))
	for k, v := range m {
		clone[k] = deepCloneValue(v)
	}
	return clone
}

// deepCloneValue recursively clones a value, handling nested maps and slices.
func deepCloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCloneMap(val)
	case []any:
		cloned := make([]any, len(val))
		for i, item := range val {
			cloned[i] = deepCloneValue(item)
		}
		return cloned
	case []string:
		cloned := make([]string, len(val))
		copy(cloned, val)
		return cloned
	case []int:
		cloned := make([]int, len(val))
		copy(cloned, val)
		return cloned
	case []int64:
		cloned := make([]int64, len(val))
		copy(cloned, val)
		return cloned
	case []float64:
		cloned := make([]float64, len(val))
		copy(cloned, val)
		return cloned
	case []bool:
		cloned := make([]bool, len(val))
		copy(cloned, val)
		return cloned
	default:
		// Primitives (string, int, bool, float64, etc.) are safe to copy by value
		return v
	}
}

func cloneSession(session *models.Session) *models.Session {
	if session == nil {
		return nil
	}
	clone := *session
	if session.Metadata != nil {
		clone.Metadata = deepCloneMap(session.Metadata)
	}
	return &clone
}

func cloneMessage(msg *models.Message) *models.Message {
	if msg == nil {
		return nil
	}
	clone := *msg
	if msg.Metadata != nil {
		clone.Metadata = deepCloneMap(msg.Metadata)
	}
	if len(msg.Attachments) > 0 {
		clone.Attachments = append([]models.Attachment{}, msg.Attachments...)
	}
	if len(msg.ToolCalls) > 0 {
		clone.ToolCalls = append([]models.ToolCall{}, msg.ToolCalls...)
	}
	if len(msg.ToolResults) > 0 {
		clone.ToolResults = append([]models.ToolResult{}, msg.ToolResults...)
	}
	return &clone
}
