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
