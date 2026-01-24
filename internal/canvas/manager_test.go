package canvas

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestHubBroadcastFanout(t *testing.T) {
	hub := NewHub()
	ch1, cancel1 := hub.Subscribe("session")
	ch2, cancel2 := hub.Subscribe("session")
	defer cancel1()
	defer cancel2()

	msg := StreamMessage{Type: "event", SessionID: "session", Timestamp: time.Now()}
	hub.Broadcast(msg)

	select {
	case <-ch1:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected message on ch1")
	}

	select {
	case <-ch2:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected message on ch2")
	}
}

func TestManagerPushBroadcast(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	manager := NewManager(store, nil)

	session := &Session{Key: "slack:workspace:channel", WorkspaceID: "workspace", ChannelID: "channel"}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, cancel := manager.Hub().Subscribe(session.ID)
	defer cancel()

	payload := json.RawMessage(`{"ok":true}`)
	if _, err := manager.Push(ctx, session.ID, payload); err != nil {
		t.Fatalf("Push: %v", err)
	}

	select {
	case msg := <-stream:
		if msg.SessionID != session.ID {
			t.Fatalf("expected session %q, got %q", session.ID, msg.SessionID)
		}
		if msg.Type != "event" {
			t.Fatalf("expected type event, got %q", msg.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected broadcast message")
	}

	events, err := store.ListEvents(ctx, session.ID, EventListOptions{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}
