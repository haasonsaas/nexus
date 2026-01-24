package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("canvas: not found")
	ErrAlreadyExists = errors.New("canvas: already exists")
)

// Session represents a canvas session scope.
type Session struct {
	ID          string
	Key         string
	WorkspaceID string
	ChannelID   string
	ThreadTS    string
	OwnerID     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// State stores a full snapshot of canvas state.
type State struct {
	SessionID string
	StateJSON json.RawMessage
	UpdatedAt time.Time
}

// Event represents an incremental update in the canvas event log.
type Event struct {
	ID        string
	SessionID string
	Type      string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// EventListOptions configures event listing.
type EventListOptions struct {
	Since time.Time
	Limit int
}

// Store persists canvas sessions, state, and events.
type Store interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessionByKey(ctx context.Context, key string) (*Session, error)
	UpdateSession(ctx context.Context, session *Session) error
	DeleteSession(ctx context.Context, id string) error

	UpsertState(ctx context.Context, state *State) error
	GetState(ctx context.Context, sessionID string) (*State, error)
	DeleteState(ctx context.Context, sessionID string) error

	AppendEvent(ctx context.Context, event *Event) error
	ListEvents(ctx context.Context, sessionID string, opts EventListOptions) ([]*Event, error)
	DeleteEvents(ctx context.Context, sessionID string) error
}
