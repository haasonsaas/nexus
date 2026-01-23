package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestMemoryAgentStoreLifecycle(t *testing.T) {
	store := NewMemoryAgentStore()
	agent := &models.Agent{
		ID:        uuid.NewString(),
		UserID:    "user-1",
		Name:      "Agent",
		Model:     "test-model",
		Provider:  "openai",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := store.Create(context.Background(), agent); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Get(context.Background(), agent.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != agent.Name {
		t.Fatalf("Get() name = %q", got.Name)
	}

	agent.Name = "Updated"
	agent.UpdatedAt = time.Now()
	if err := store.Update(context.Background(), agent); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	list, total, err := store.List(context.Background(), "user-1", 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("List() expected 1, got %d/%d", len(list), total)
	}

	if err := store.Delete(context.Background(), agent.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestMemoryChannelConnectionStoreLifecycle(t *testing.T) {
	store := NewMemoryChannelConnectionStore()
	conn := &models.ChannelConnection{
		ID:          uuid.NewString(),
		UserID:      "user-1",
		ChannelType: models.ChannelSlack,
		ChannelID:   "channel-1",
		Status:      models.ConnectionStatusConnected,
		ConnectedAt: time.Now(),
	}

	if err := store.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Get(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ChannelID != conn.ChannelID {
		t.Fatalf("Get() channel_id = %q", got.ChannelID)
	}

	conn.Status = models.ConnectionStatusDisconnected
	if err := store.Update(context.Background(), conn); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	list, total, err := store.List(context.Background(), "user-1", 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("List() expected 1, got %d/%d", len(list), total)
	}

	if err := store.Delete(context.Background(), conn.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestMemoryUserStoreFindOrCreate(t *testing.T) {
	store := NewMemoryUserStore()
	info := &auth.UserInfo{
		Provider:  "google",
		ID:        "abc",
		Email:     "user@example.com",
		Name:      "User",
		AvatarURL: "avatar",
	}

	user, err := store.FindOrCreate(context.Background(), info)
	if err != nil {
		t.Fatalf("FindOrCreate() error = %v", err)
	}
	if user.Email != "user@example.com" {
		t.Fatalf("FindOrCreate() email = %q", user.Email)
	}
	if user.Provider != "google" || user.ProviderID != "abc" {
		t.Fatalf("FindOrCreate() provider mismatch")
	}

	info.Name = "User Updated"
	user2, err := store.FindOrCreate(context.Background(), info)
	if err != nil {
		t.Fatalf("FindOrCreate() repeat error = %v", err)
	}
	if user2.ID != user.ID {
		t.Fatalf("expected same user ID")
	}
	if user2.Name != "User Updated" {
		t.Fatalf("expected updated name, got %q", user2.Name)
	}
}

func TestMemoryAgentStore_GetNotFound(t *testing.T) {
	store := NewMemoryAgentStore()
	_, err := store.Get(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryAgentStore_UpdateNotFound(t *testing.T) {
	store := NewMemoryAgentStore()
	agent := &models.Agent{ID: "nonexistent"}
	err := store.Update(context.Background(), agent)
	if err != ErrNotFound {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryAgentStore_DeleteNotFound(t *testing.T) {
	store := NewMemoryAgentStore()
	err := store.Delete(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryAgentStore_CreateDuplicate(t *testing.T) {
	store := NewMemoryAgentStore()
	agent := &models.Agent{
		ID:     "agent-1",
		UserID: "user-1",
		Name:   "Agent",
	}
	if err := store.Create(context.Background(), agent); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err := store.Create(context.Background(), agent)
	if err != ErrAlreadyExists {
		t.Errorf("Create() duplicate error = %v, want ErrAlreadyExists", err)
	}
}

func TestMemoryAgentStore_ListPagination(t *testing.T) {
	store := NewMemoryAgentStore()
	for i := 0; i < 5; i++ {
		agent := &models.Agent{
			ID:     uuid.NewString(),
			UserID: "user-1",
			Name:   "Agent",
		}
		store.Create(context.Background(), agent)
	}

	// Test limit
	list, total, _ := store.List(context.Background(), "user-1", 2, 0)
	if len(list) != 2 {
		t.Errorf("List() limit: got %d, want 2", len(list))
	}
	if total != 5 {
		t.Errorf("List() total: got %d, want 5", total)
	}

	// Test offset
	list, _, _ = store.List(context.Background(), "user-1", 10, 3)
	if len(list) != 2 {
		t.Errorf("List() offset: got %d, want 2", len(list))
	}

	// Test different user
	list, total, _ = store.List(context.Background(), "user-2", 10, 0)
	if len(list) != 0 || total != 0 {
		t.Errorf("List() different user: got %d/%d, want 0/0", len(list), total)
	}
}

func TestMemoryChannelConnectionStore_GetNotFound(t *testing.T) {
	store := NewMemoryChannelConnectionStore()
	_, err := store.Get(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryChannelConnectionStore_UpdateNotFound(t *testing.T) {
	store := NewMemoryChannelConnectionStore()
	conn := &models.ChannelConnection{ID: "nonexistent"}
	err := store.Update(context.Background(), conn)
	if err != ErrNotFound {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryChannelConnectionStore_DeleteNotFound(t *testing.T) {
	store := NewMemoryChannelConnectionStore()
	err := store.Delete(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryChannelConnectionStore_CreateDuplicate(t *testing.T) {
	store := NewMemoryChannelConnectionStore()
	conn := &models.ChannelConnection{
		ID:     "conn-1",
		UserID: "user-1",
	}
	if err := store.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err := store.Create(context.Background(), conn)
	if err != ErrAlreadyExists {
		t.Errorf("Create() duplicate error = %v, want ErrAlreadyExists", err)
	}
}

func TestMemoryUserStore_GetNotFound(t *testing.T) {
	store := NewMemoryUserStore()
	_, err := store.Get(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestMemoryUserStore_GetExisting(t *testing.T) {
	store := NewMemoryUserStore()
	info := &auth.UserInfo{
		Provider: "google",
		ID:       "user-123",
		Email:    "test@example.com",
	}

	user, _ := store.FindOrCreate(context.Background(), info)

	got, err := store.Get(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Email != "test@example.com" {
		t.Errorf("Get() email = %q, want %q", got.Email, "test@example.com")
	}
}

func TestStoreSet_Close(t *testing.T) {
	t.Run("nil closer", func(t *testing.T) {
		set := StoreSet{}
		err := set.Close()
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	t.Run("with closer", func(t *testing.T) {
		closed := false
		set := StoreSet{
			closer: func() error {
				closed = true
				return nil
			},
		}
		err := set.Close()
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
		if !closed {
			t.Error("closer was not called")
		}
	})
}

func TestErrNotFound(t *testing.T) {
	if ErrNotFound.Error() != "not found" {
		t.Errorf("ErrNotFound = %q, want %q", ErrNotFound.Error(), "not found")
	}
}

func TestErrAlreadyExists(t *testing.T) {
	if ErrAlreadyExists.Error() != "already exists" {
		t.Errorf("ErrAlreadyExists = %q, want %q", ErrAlreadyExists.Error(), "already exists")
	}
}
