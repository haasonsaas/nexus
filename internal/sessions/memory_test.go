package sessions

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestMemoryStoreSessionLifecycle(t *testing.T) {
	store := NewMemoryStore()
	session := &models.Session{AgentID: "agent", Channel: models.ChannelType("api"), ChannelID: "user", Key: "agent:api:user"}

	if err := store.Create(context.Background(), session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if session.ID == "" {
		t.Fatalf("expected session id to be assigned")
	}

	loaded, err := store.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.Key != session.Key {
		t.Fatalf("expected key %q, got %q", session.Key, loaded.Key)
	}

	loaded.Title = "updated"
	if err := store.Update(context.Background(), loaded); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	updated, err := store.Get(context.Background(), loaded.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if updated.Title != "updated" {
		t.Fatalf("expected title to update")
	}

	if err := store.Delete(context.Background(), updated.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestMemoryStoreMessages(t *testing.T) {
	store := NewMemoryStore()
	session, err := store.GetOrCreate(context.Background(), "agent:api:user", "agent", models.ChannelType("api"), "user")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	msg := &models.Message{SessionID: session.ID, Role: models.RoleUser, Content: "hello"}
	if err := store.AppendMessage(context.Background(), session.ID, msg); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	history, err := store.GetHistory(context.Background(), session.ID, 10)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 message, got %d", len(history))
	}
}
