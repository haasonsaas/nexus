package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreSessionLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &Session{
		Key:         "slack:workspace:channel",
		WorkspaceID: "workspace",
		ChannelID:   "channel",
		OwnerID:     "user-1",
	}

	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.ID == "" {
		t.Fatalf("expected session ID to be set")
	}

	got, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.Key != session.Key {
		t.Fatalf("expected session key %q, got %q", session.Key, got.Key)
	}

	byKey, err := store.GetSessionByKey(ctx, session.Key)
	if err != nil {
		t.Fatalf("GetSessionByKey() error = %v", err)
	}
	if byKey.ID != session.ID {
		t.Fatalf("expected session id %q, got %q", session.ID, byKey.ID)
	}

	session.OwnerID = "user-2"
	if err := store.UpdateSession(ctx, session); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}
	updated, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession(after update) error = %v", err)
	}
	if updated.OwnerID != "user-2" {
		t.Fatalf("expected owner_id to update")
	}

	if err := store.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := store.GetSession(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestMemoryStoreStateAndEvents(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &Session{
		Key:         "slack:workspace:channel:thread",
		WorkspaceID: "workspace",
		ChannelID:   "channel",
		ThreadTS:    "123.456",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	state := &State{
		SessionID: session.ID,
		StateJSON: json.RawMessage(`{"view":"ok"}`),
	}
	if err := store.UpsertState(ctx, state); err != nil {
		t.Fatalf("UpsertState() error = %v", err)
	}
	stored, err := store.GetState(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if string(stored.StateJSON) != string(state.StateJSON) {
		t.Fatalf("expected state to round-trip")
	}

	now := time.Now()
	first := &Event{
		SessionID: session.ID,
		Type:      "state",
		Payload:   json.RawMessage(`{"step":1}`),
		CreatedAt: now.Add(-time.Minute),
	}
	second := &Event{
		SessionID: session.ID,
		Type:      "state",
		Payload:   json.RawMessage(`{"step":2}`),
		CreatedAt: now,
	}
	if err := store.AppendEvent(ctx, second); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := store.AppendEvent(ctx, first); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	events, err := store.ListEvents(ctx, session.ID, EventListOptions{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].CreatedAt.After(events[1].CreatedAt) {
		t.Fatalf("expected events ordered by created_at")
	}

	filtered, err := store.ListEvents(ctx, session.ID, EventListOptions{Since: now.Add(-30 * time.Second)})
	if err != nil {
		t.Fatalf("ListEvents(filter) error = %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered event, got %d", len(filtered))
	}

	if err := store.DeleteEvents(ctx, session.ID); err != nil {
		t.Fatalf("DeleteEvents() error = %v", err)
	}
	cleared, err := store.ListEvents(ctx, session.ID, EventListOptions{})
	if err != nil {
		t.Fatalf("ListEvents(after delete) error = %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("expected no events after delete")
	}

	if err := store.DeleteState(ctx, session.ID); err != nil {
		t.Fatalf("DeleteState() error = %v", err)
	}
	if _, err := store.GetState(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after state delete, got %v", err)
	}
}
