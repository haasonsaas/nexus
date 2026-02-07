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

func TestMemoryStore_GetNonExistent(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Get(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
}

func TestMemoryStore_DeleteNonExistent(t *testing.T) {
	store := NewMemoryStore()
	err := store.Delete(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for deleting non-existent session")
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{AgentID: "agent", Channel: models.ChannelType("api"), ChannelID: "user", Key: "agent:api:user"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	done := make(chan struct{})
	// Writer goroutine
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			msg := &models.Message{SessionID: session.ID, Role: models.RoleUser, Content: "msg"}
			_ = store.AppendMessage(ctx, session.ID, msg)
		}
	}()

	// Reader goroutine
	for i := 0; i < 100; i++ {
		_, _ = store.Get(ctx, session.ID)
		_, _ = store.GetHistory(ctx, session.ID, 10)
	}
	<-done

	// Verify session is still accessible
	got, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() after concurrent access error = %v", err)
	}
	if got.ID != session.ID {
		t.Fatalf("expected session ID %q, got %q", session.ID, got.ID)
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
