package canvas

import (
	"encoding/json"
	"sync"
	"time"
)

// StreamMessage is the envelope sent over the canvas realtime stream.
type StreamMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"ts"`
}

// Hub manages realtime subscribers for canvas sessions.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan StreamMessage]struct{}
}

// NewHub creates a new hub.
func NewHub() *Hub {
	return &Hub{subscribers: make(map[string]map[chan StreamMessage]struct{})}
}

// Subscribe registers a listener for a session.
func (h *Hub) Subscribe(sessionID string) (chan StreamMessage, func()) {
	ch := make(chan StreamMessage, 16)
	h.mu.Lock()
	listeners := h.subscribers[sessionID]
	if listeners == nil {
		listeners = make(map[chan StreamMessage]struct{})
		h.subscribers[sessionID] = listeners
	}
	listeners[ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		listeners := h.subscribers[sessionID]
		if listeners != nil {
			delete(listeners, ch)
			if len(listeners) == 0 {
				delete(h.subscribers, sessionID)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// Broadcast delivers a message to all subscribers for the session.
func (h *Hub) Broadcast(msg StreamMessage) {
	if h == nil {
		return
	}
	h.mu.RLock()
	listeners := h.subscribers[msg.SessionID]
	for ch := range listeners {
		select {
		case ch <- msg:
		default:
		}
	}
	h.mu.RUnlock()
}
