package identity

import (
	"context"
	"testing"
)

func TestNewMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.identities == nil {
		t.Error("identities map should be initialized")
	}
	if store.peerIndex == nil {
		t.Error("peerIndex map should be initialized")
	}
}

func TestMemoryStore_Create(t *testing.T) {
	t.Run("creates identity successfully", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		identity := &Identity{
			CanonicalID: "user-1",
			DisplayName: "Test User",
			Email:       "test@example.com",
			LinkedPeers: []string{"telegram:123"},
			Metadata:    map[string]string{"key": "value"},
		}

		err := store.Create(ctx, identity)
		if err != nil {
			t.Fatalf("Create error: %v", err)
		}

		// Verify identity was stored
		stored, err := store.Get(ctx, "user-1")
		if err != nil {
			t.Fatalf("Get error: %v", err)
		}
		if stored == nil {
			t.Fatal("expected stored identity")
		}
		if stored.DisplayName != "Test User" {
			t.Errorf("DisplayName = %q, want %q", stored.DisplayName, "Test User")
		}
		if stored.Email != "test@example.com" {
			t.Errorf("Email = %q, want %q", stored.Email, "test@example.com")
		}
		if stored.CreatedAt.IsZero() {
			t.Error("CreatedAt should be set")
		}
		if stored.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should be set")
		}
	})

	t.Run("fails for duplicate canonical ID", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		identity := &Identity{CanonicalID: "user-1"}
		if err := store.Create(ctx, identity); err != nil {
			t.Fatalf("first Create error: %v", err)
		}

		err := store.Create(ctx, &Identity{CanonicalID: "user-1"})
		if err == nil {
			t.Error("expected error for duplicate ID")
		}
	})

	t.Run("indexes linked peers", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		identity := &Identity{
			CanonicalID: "user-1",
			LinkedPeers: []string{"telegram:123", "discord:456"},
		}
		if err := store.Create(ctx, identity); err != nil {
			t.Fatalf("Create error: %v", err)
		}

		// Verify peer index
		resolved, err := store.ResolveByPeer(ctx, "telegram", "123")
		if err != nil {
			t.Fatalf("ResolveByPeer error: %v", err)
		}
		if resolved == nil || resolved.CanonicalID != "user-1" {
			t.Error("peer should resolve to user-1")
		}
	})

	t.Run("clones data to prevent external modification", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		metadata := map[string]string{"key": "original"}
		identity := &Identity{
			CanonicalID: "user-1",
			Metadata:    metadata,
		}
		if err := store.Create(ctx, identity); err != nil {
			t.Fatalf("Create error: %v", err)
		}

		// Modify original metadata
		metadata["key"] = "modified"

		// Stored data should be unchanged
		stored, _ := store.Get(ctx, "user-1")
		if stored.Metadata["key"] != "original" {
			t.Error("stored metadata should not be affected by external changes")
		}
	})
}

func TestMemoryStore_Get(t *testing.T) {
	t.Run("returns nil for nonexistent identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		identity, err := store.Get(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Get error: %v", err)
		}
		if identity != nil {
			t.Error("expected nil for nonexistent identity")
		}
	})

	t.Run("returns clone to prevent external modification", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		identity := &Identity{
			CanonicalID: "user-1",
			DisplayName: "Original",
		}
		store.Create(ctx, identity)

		// Get and modify
		retrieved, _ := store.Get(ctx, "user-1")
		retrieved.DisplayName = "Modified"

		// Get again - should still be original
		retrieved2, _ := store.Get(ctx, "user-1")
		if retrieved2.DisplayName != "Original" {
			t.Error("stored identity should not be affected by modifications to returned clone")
		}
	})
}

func TestMemoryStore_Update(t *testing.T) {
	t.Run("updates existing identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			DisplayName: "Original",
		})

		err := store.Update(ctx, &Identity{
			CanonicalID: "user-1",
			DisplayName: "Updated",
		})
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}

		stored, _ := store.Get(ctx, "user-1")
		if stored.DisplayName != "Updated" {
			t.Errorf("DisplayName = %q, want %q", stored.DisplayName, "Updated")
		}
	})

	t.Run("fails for nonexistent identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		err := store.Update(ctx, &Identity{CanonicalID: "nonexistent"})
		if err == nil {
			t.Error("expected error for nonexistent identity")
		}
	})

	t.Run("preserves CreatedAt", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{CanonicalID: "user-1"})
		original, _ := store.Get(ctx, "user-1")
		createdAt := original.CreatedAt

		store.Update(ctx, &Identity{CanonicalID: "user-1", DisplayName: "Updated"})
		updated, _ := store.Get(ctx, "user-1")

		if !updated.CreatedAt.Equal(createdAt) {
			t.Error("CreatedAt should be preserved after update")
		}
	})

	t.Run("updates peer index", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			LinkedPeers: []string{"telegram:123"},
		})

		store.Update(ctx, &Identity{
			CanonicalID: "user-1",
			LinkedPeers: []string{"discord:456"},
		})

		// Old peer should not resolve
		oldResolved, _ := store.ResolveByPeer(ctx, "telegram", "123")
		if oldResolved != nil {
			t.Error("old peer should not resolve after update")
		}

		// New peer should resolve
		newResolved, _ := store.ResolveByPeer(ctx, "discord", "456")
		if newResolved == nil || newResolved.CanonicalID != "user-1" {
			t.Error("new peer should resolve to user-1")
		}
	})
}

func TestMemoryStore_Delete(t *testing.T) {
	t.Run("deletes existing identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{CanonicalID: "user-1"})

		err := store.Delete(ctx, "user-1")
		if err != nil {
			t.Fatalf("Delete error: %v", err)
		}

		stored, _ := store.Get(ctx, "user-1")
		if stored != nil {
			t.Error("identity should be deleted")
		}
	})

	t.Run("no error for nonexistent identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		err := store.Delete(ctx, "nonexistent")
		if err != nil {
			t.Errorf("Delete should not error for nonexistent: %v", err)
		}
	})

	t.Run("removes peer index entries", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			LinkedPeers: []string{"telegram:123"},
		})

		store.Delete(ctx, "user-1")

		resolved, _ := store.ResolveByPeer(ctx, "telegram", "123")
		if resolved != nil {
			t.Error("peer should not resolve after identity deletion")
		}
	})
}

func TestMemoryStore_List(t *testing.T) {
	t.Run("returns empty list for empty store", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		identities, total, err := store.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List error: %v", err)
		}
		if len(identities) != 0 {
			t.Errorf("expected 0 identities, got %d", len(identities))
		}
		if total != 0 {
			t.Errorf("expected total 0, got %d", total)
		}
	})

	t.Run("returns all identities with pagination", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		for i := 0; i < 5; i++ {
			store.Create(ctx, &Identity{
				CanonicalID: "user-" + string(rune('A'+i)),
			})
		}

		// First page
		page1, total, _ := store.List(ctx, 2, 0)
		if len(page1) != 2 {
			t.Errorf("page 1: expected 2 identities, got %d", len(page1))
		}
		if total != 5 {
			t.Errorf("expected total 5, got %d", total)
		}

		// Second page
		page2, _, _ := store.List(ctx, 2, 2)
		if len(page2) != 2 {
			t.Errorf("page 2: expected 2 identities, got %d", len(page2))
		}

		// Third page (partial)
		page3, _, _ := store.List(ctx, 2, 4)
		if len(page3) != 1 {
			t.Errorf("page 3: expected 1 identity, got %d", len(page3))
		}
	})

	t.Run("handles offset beyond list", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{CanonicalID: "user-1"})

		identities, total, _ := store.List(ctx, 10, 100)
		if len(identities) != 0 {
			t.Errorf("expected 0 identities for offset beyond list, got %d", len(identities))
		}
		if total != 1 {
			t.Errorf("expected total 1, got %d", total)
		}
	})
}

func TestMemoryStore_LinkPeer(t *testing.T) {
	t.Run("links peer to identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{CanonicalID: "user-1"})

		err := store.LinkPeer(ctx, "user-1", "telegram", "123")
		if err != nil {
			t.Fatalf("LinkPeer error: %v", err)
		}

		peers, _ := store.GetLinkedPeers(ctx, "user-1")
		if len(peers) != 1 || peers[0] != "telegram:123" {
			t.Errorf("expected [telegram:123], got %v", peers)
		}
	})

	t.Run("fails for nonexistent identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		err := store.LinkPeer(ctx, "nonexistent", "telegram", "123")
		if err == nil {
			t.Error("expected error for nonexistent identity")
		}
	})

	t.Run("fails if peer linked to different identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{CanonicalID: "user-1"})
		store.Create(ctx, &Identity{CanonicalID: "user-2"})
		store.LinkPeer(ctx, "user-1", "telegram", "123")

		err := store.LinkPeer(ctx, "user-2", "telegram", "123")
		if err == nil {
			t.Error("expected error when peer is linked to different identity")
		}
	})

	t.Run("no-op if already linked to same identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{CanonicalID: "user-1"})
		store.LinkPeer(ctx, "user-1", "telegram", "123")

		err := store.LinkPeer(ctx, "user-1", "telegram", "123")
		if err != nil {
			t.Errorf("linking same peer again should not error: %v", err)
		}

		peers, _ := store.GetLinkedPeers(ctx, "user-1")
		if len(peers) != 1 {
			t.Errorf("should still have only 1 peer, got %d", len(peers))
		}
	})
}

func TestMemoryStore_UnlinkPeer(t *testing.T) {
	t.Run("unlinks peer from identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			LinkedPeers: []string{"telegram:123", "discord:456"},
		})

		err := store.UnlinkPeer(ctx, "user-1", "telegram", "123")
		if err != nil {
			t.Fatalf("UnlinkPeer error: %v", err)
		}

		peers, _ := store.GetLinkedPeers(ctx, "user-1")
		if len(peers) != 1 || peers[0] != "discord:456" {
			t.Errorf("expected [discord:456], got %v", peers)
		}

		// Verify peer no longer resolves
		resolved, _ := store.ResolveByPeer(ctx, "telegram", "123")
		if resolved != nil {
			t.Error("unlinked peer should not resolve")
		}
	})

	t.Run("fails for nonexistent identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		err := store.UnlinkPeer(ctx, "nonexistent", "telegram", "123")
		if err == nil {
			t.Error("expected error for nonexistent identity")
		}
	})
}

func TestMemoryStore_ResolveByPeer(t *testing.T) {
	t.Run("resolves existing peer", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			DisplayName: "Test User",
			LinkedPeers: []string{"telegram:123"},
		})

		identity, err := store.ResolveByPeer(ctx, "telegram", "123")
		if err != nil {
			t.Fatalf("ResolveByPeer error: %v", err)
		}
		if identity == nil {
			t.Fatal("expected identity")
		}
		if identity.CanonicalID != "user-1" {
			t.Errorf("CanonicalID = %q, want %q", identity.CanonicalID, "user-1")
		}
	})

	t.Run("returns nil for unknown peer", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		identity, err := store.ResolveByPeer(ctx, "telegram", "unknown")
		if err != nil {
			t.Fatalf("ResolveByPeer error: %v", err)
		}
		if identity != nil {
			t.Error("expected nil for unknown peer")
		}
	})
}

func TestMemoryStore_GetLinkedPeers(t *testing.T) {
	t.Run("returns linked peers", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			LinkedPeers: []string{"telegram:123", "discord:456"},
		})

		peers, err := store.GetLinkedPeers(ctx, "user-1")
		if err != nil {
			t.Fatalf("GetLinkedPeers error: %v", err)
		}
		if len(peers) != 2 {
			t.Errorf("expected 2 peers, got %d", len(peers))
		}
	})

	t.Run("fails for nonexistent identity", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		_, err := store.GetLinkedPeers(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent identity")
		}
	})

	t.Run("returns clone to prevent modification", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			LinkedPeers: []string{"telegram:123"},
		})

		peers, _ := store.GetLinkedPeers(ctx, "user-1")
		peers[0] = "modified"

		peersAgain, _ := store.GetLinkedPeers(ctx, "user-1")
		if peersAgain[0] != "telegram:123" {
			t.Error("stored peers should not be affected by external modification")
		}
	})
}

func TestMemoryStore_ExportToConfig(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	store.Create(ctx, &Identity{
		CanonicalID: "user-1",
		LinkedPeers: []string{"telegram:123", "discord:456"},
	})
	store.Create(ctx, &Identity{
		CanonicalID: "user-2",
		LinkedPeers: []string{"slack:789"},
	})
	store.Create(ctx, &Identity{
		CanonicalID: "user-3",
		// No linked peers
	})

	config := store.ExportToConfig()

	if len(config) != 2 {
		t.Errorf("expected 2 entries (excluding empty), got %d", len(config))
	}
	if len(config["user-1"]) != 2 {
		t.Errorf("user-1 should have 2 peers, got %d", len(config["user-1"]))
	}
	if len(config["user-2"]) != 1 {
		t.Errorf("user-2 should have 1 peer, got %d", len(config["user-2"]))
	}
}

func TestMemoryStore_ImportFromConfig(t *testing.T) {
	t.Run("imports new identities", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		config := map[string][]string{
			"user-1": {"telegram:123", "discord:456"},
			"user-2": {"slack:789"},
		}

		err := store.ImportFromConfig(ctx, config)
		if err != nil {
			t.Fatalf("ImportFromConfig error: %v", err)
		}

		identity1, _ := store.Get(ctx, "user-1")
		if identity1 == nil {
			t.Fatal("user-1 should exist")
		}
		if len(identity1.LinkedPeers) != 2 {
			t.Errorf("user-1 should have 2 peers, got %d", len(identity1.LinkedPeers))
		}
	})

	t.Run("updates existing identities", func(t *testing.T) {
		store := NewMemoryStore()
		ctx := context.Background()

		store.Create(ctx, &Identity{
			CanonicalID: "user-1",
			DisplayName: "Original",
			LinkedPeers: []string{"telegram:old"},
		})

		config := map[string][]string{
			"user-1": {"telegram:new"},
		}

		err := store.ImportFromConfig(ctx, config)
		if err != nil {
			t.Fatalf("ImportFromConfig error: %v", err)
		}

		identity, _ := store.Get(ctx, "user-1")
		if len(identity.LinkedPeers) != 1 || identity.LinkedPeers[0] != "telegram:new" {
			t.Errorf("expected [telegram:new], got %v", identity.LinkedPeers)
		}
	})
}
