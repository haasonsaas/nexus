package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"
)

// ToolCall represents a tool call made by the LLM.
type ToolCall struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	MessageID string          `json:"message_id,omitempty"`
	ToolName  string          `json:"tool_name"`
	InputJSON json.RawMessage `json:"input_json"`
	CreatedAt time.Time       `json:"created_at"`
}

// ToolResult represents a tool execution result.
type ToolResult struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	MessageID  string    `json:"message_id,omitempty"`
	ToolCallID string    `json:"tool_call_id"`
	IsError    bool      `json:"is_error"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// ToolEventStore persists tool calls and results.
type ToolEventStore interface {
	// AddToolCall records a tool call from the LLM.
	AddToolCall(ctx context.Context, sessionID, messageID string, call *ToolCall) error

	// AddToolResult records a tool execution result.
	AddToolResult(ctx context.Context, sessionID, messageID string, callID string, result *ToolResult) error

	// GetToolCalls retrieves tool calls for a session.
	GetToolCalls(ctx context.Context, sessionID string, limit int) ([]ToolCall, error)

	// GetToolResults retrieves tool results for a session.
	GetToolResults(ctx context.Context, sessionID string, limit int) ([]ToolResult, error)

	// GetToolCallsByMessage retrieves tool calls for a specific message.
	GetToolCallsByMessage(ctx context.Context, messageID string) ([]ToolCall, error)
}

// SQLToolEventStore implements ToolEventStore using SQL.
type SQLToolEventStore struct {
	db *sql.DB
}

// NewSQLToolEventStore creates a new SQL-backed tool event store.
func NewSQLToolEventStore(db *sql.DB) *SQLToolEventStore {
	return &SQLToolEventStore{db: db}
}

// AddToolCall records a tool call.
func (s *SQLToolEventStore) AddToolCall(ctx context.Context, sessionID, messageID string, call *ToolCall) error {
	if call == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_calls (id, session_id, message_id, tool_name, input_json)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5)
		ON CONFLICT (id) DO NOTHING
	`, call.ID, sessionID, messageID, call.ToolName, call.InputJSON)

	return err
}

// AddToolResult records a tool result.
func (s *SQLToolEventStore) AddToolResult(ctx context.Context, sessionID, messageID string, callID string, result *ToolResult) error {
	if result == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_results (session_id, message_id, tool_call_id, is_error, content)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5)
	`, sessionID, messageID, callID, result.IsError, result.Content)

	return err
}

// GetToolCalls retrieves recent tool calls for a session.
func (s *SQLToolEventStore) GetToolCalls(ctx context.Context, sessionID string, limit int) ([]ToolCall, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, COALESCE(message_id, ''), tool_name, input_json, created_at
		FROM tool_calls
		WHERE session_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var calls []ToolCall
	for rows.Next() {
		var call ToolCall
		if err := rows.Scan(&call.ID, &call.SessionID, &call.MessageID, &call.ToolName, &call.InputJSON, &call.CreatedAt); err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}

	return calls, rows.Err()
}

// GetToolResults retrieves recent tool results for a session.
func (s *SQLToolEventStore) GetToolResults(ctx context.Context, sessionID string, limit int) ([]ToolResult, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, COALESCE(message_id, ''), tool_call_id, is_error, content, created_at
		FROM tool_results
		WHERE session_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ToolResult
	for rows.Next() {
		var result ToolResult
		if err := rows.Scan(&result.ID, &result.SessionID, &result.MessageID, &result.ToolCallID, &result.IsError, &result.Content, &result.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

// GetToolCallsByMessage retrieves tool calls for a specific message.
func (s *SQLToolEventStore) GetToolCallsByMessage(ctx context.Context, messageID string) ([]ToolCall, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, message_id, tool_name, input_json, created_at
		FROM tool_calls
		WHERE message_id = $1
		ORDER BY created_at ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var calls []ToolCall
	for rows.Next() {
		var call ToolCall
		var msgID sql.NullString
		if err := rows.Scan(&call.ID, &call.SessionID, &msgID, &call.ToolName, &call.InputJSON, &call.CreatedAt); err != nil {
			return nil, err
		}
		if msgID.Valid {
			call.MessageID = msgID.String
		}
		calls = append(calls, call)
	}

	return calls, rows.Err()
}

// MemoryToolEventStore implements ToolEventStore in memory for testing.
type MemoryToolEventStore struct {
	mu      sync.RWMutex
	calls   []ToolCall
	results []ToolResult
}

// NewMemoryToolEventStore creates a new in-memory tool event store.
func NewMemoryToolEventStore() *MemoryToolEventStore {
	return &MemoryToolEventStore{}
}

// AddToolCall records a tool call in memory.
func (s *MemoryToolEventStore) AddToolCall(ctx context.Context, sessionID, messageID string, call *ToolCall) error {
	if call == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tc := *call
	tc.SessionID = sessionID
	tc.MessageID = messageID
	tc.CreatedAt = time.Now()
	s.calls = append(s.calls, tc)
	return nil
}

// AddToolResult records a tool result in memory.
func (s *MemoryToolEventStore) AddToolResult(ctx context.Context, sessionID, messageID string, callID string, result *ToolResult) error {
	if result == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tr := *result
	tr.SessionID = sessionID
	tr.MessageID = messageID
	tr.ToolCallID = callID
	tr.CreatedAt = time.Now()
	s.results = append(s.results, tr)
	return nil
}

// GetToolCalls retrieves tool calls from memory.
func (s *MemoryToolEventStore) GetToolCalls(ctx context.Context, sessionID string, limit int) ([]ToolCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var calls []ToolCall
	for _, c := range s.calls {
		if c.SessionID == sessionID {
			calls = append(calls, c)
		}
	}

	// Return most recent first, limited
	if limit > 0 && len(calls) > limit {
		calls = calls[len(calls)-limit:]
	}

	// Reverse to get most recent first
	for i, j := 0, len(calls)-1; i < j; i, j = i+1, j-1 {
		calls[i], calls[j] = calls[j], calls[i]
	}

	return calls, nil
}

// GetToolResults retrieves tool results from memory.
func (s *MemoryToolEventStore) GetToolResults(ctx context.Context, sessionID string, limit int) ([]ToolResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []ToolResult
	for _, r := range s.results {
		if r.SessionID == sessionID {
			results = append(results, r)
		}
	}

	// Return most recent first, limited
	if limit > 0 && len(results) > limit {
		results = results[len(results)-limit:]
	}

	// Reverse to get most recent first
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	return results, nil
}

// GetToolCallsByMessage retrieves tool calls for a specific message from memory.
func (s *MemoryToolEventStore) GetToolCallsByMessage(ctx context.Context, messageID string) ([]ToolCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var calls []ToolCall
	for _, c := range s.calls {
		if c.MessageID == messageID {
			calls = append(calls, c)
		}
	}
	return calls, nil
}
