package canvas

import (
	"context"
	"encoding/json"
	"time"
)

// Action describes a UI-originated canvas action.
type Action struct {
	SessionID         string          `json:"session_id"`
	ID                string          `json:"id,omitempty"`
	Name              string          `json:"name"`
	SourceComponentID string          `json:"source_component_id,omitempty"`
	Context           json.RawMessage `json:"context,omitempty"`
	UserID            string          `json:"user_id,omitempty"`
	ReceivedAt        time.Time       `json:"received_at"`
}

// ActionHandler processes a canvas action.
type ActionHandler func(context.Context, Action) error
