package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestScopedStore_GetOrCreateScoped_DMScopeMain(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{
		DMScope: DMScopeMain,
	})

	ctx := context.Background()

	// Two different users from different channels should get the same session
	session1, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "U123", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	session2, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelDiscord, "user456", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	if session1.ID != session2.ID {
		t.Errorf("Expected same session for all DMs in main scope, got different IDs: %s vs %s", session1.ID, session2.ID)
	}
}

func TestScopedStore_GetOrCreateScoped_DMScopePerPeer(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{
		DMScope: DMScopePerPeer,
	})

	ctx := context.Background()

	// Different users should get different sessions
	session1, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "U123", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	session2, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "U456", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	if session1.ID == session2.ID {
		t.Error("Expected different sessions for different peers in per-peer scope")
	}
}

func TestScopedStore_GetOrCreateScoped_IdentityLinks(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{
		DMScope: DMScopePerPeer,
		IdentityLinks: map[string][]string{
			"jonathan": {"slack:U123", "discord:user456"},
		},
	})

	ctx := context.Background()

	// Same user from different channels should get the same session
	session1, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "U123", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	session2, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelDiscord, "user456", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	if session1.ID != session2.ID {
		t.Errorf("Expected same session for linked identities, got different IDs: %s vs %s", session1.ID, session2.ID)
	}
}

func TestScopedStore_GetOrCreateScoped_GroupConversations(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{
		DMScope: DMScopeMain, // Should not affect groups
	})

	ctx := context.Background()

	// Different groups should get different sessions
	session1, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "C123", true, "", ConvTypeGroup)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	session2, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "C456", true, "", ConvTypeGroup)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	if session1.ID == session2.ID {
		t.Error("Expected different sessions for different groups")
	}
}

func TestScopedStore_GetOrCreateScoped_WithThread(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{
		DMScope: DMScopeMain,
	})

	ctx := context.Background()

	// Same group, different threads should get different sessions
	session1, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "C123", true, "thread1", ConvTypeThread)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	session2, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "C123", true, "thread2", ConvTypeThread)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	if session1.ID == session2.ID {
		t.Error("Expected different sessions for different threads")
	}
}

func TestScopedStore_GetOrCreateScoped_ExpiryResets(t *testing.T) {
	store := NewMemoryStore()
	cfg := ScopeConfig{
		DMScope: DMScopePerPeer,
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	}
	scopedStore := NewScopedStore(store, cfg)

	ctx := context.Background()

	// Create a session
	session1, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "U123", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}
	session1ID := session1.ID

	// Move the clock forward 60 minutes to make the session appear expired
	scopedStore.expiry.SetNowFunc(func() time.Time {
		return time.Now().Add(60 * time.Minute)
	})

	// Get the session again - it should be reset due to idle expiry
	session2, err := scopedStore.GetOrCreateScoped(ctx, "agent1", models.ChannelSlack, "U123", false, "", ConvTypeDM)
	if err != nil {
		t.Fatalf("GetOrCreateScoped() error = %v", err)
	}

	if session2.ID == session1ID {
		t.Error("Expected new session after expiry, got same session ID")
	}
}

func TestScopedStore_GetSessionWithExpiryCheck(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	})

	ctx := context.Background()

	// Create a session with explicit Channel
	session := &models.Session{
		AgentID:   "agent1",
		Channel:   models.ChannelSlack,
		ChannelID: "U123",
		Key:       "test-key",
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Fresh session should not be expired
	retrieved, shouldReset, err := scopedStore.GetSessionWithExpiryCheck(ctx, session.ID, ConvTypeDM)
	if err != nil {
		t.Fatalf("GetSessionWithExpiryCheck() error = %v", err)
	}
	if shouldReset {
		t.Error("Fresh session should not be marked for reset")
	}
	if retrieved.ID != session.ID {
		t.Error("Retrieved session ID should match")
	}

	// Move the clock forward 60 minutes to make the session appear expired
	scopedStore.expiry.SetNowFunc(func() time.Time {
		return time.Now().Add(60 * time.Minute)
	})

	// Old session should be marked for reset
	_, shouldReset, err = scopedStore.GetSessionWithExpiryCheck(ctx, session.ID, ConvTypeDM)
	if err != nil {
		t.Fatalf("GetSessionWithExpiryCheck() error = %v", err)
	}
	if !shouldReset {
		t.Error("Old session should be marked for reset")
	}
}

func TestScopedStore_ResolveIdentity(t *testing.T) {
	scopedStore := NewScopedStore(NewMemoryStore(), ScopeConfig{
		IdentityLinks: map[string][]string{
			"jonathan": {"slack:U123", "discord:user456"},
		},
	})

	got := scopedStore.ResolveIdentity("slack", "U123")
	if got != "jonathan" {
		t.Errorf("ResolveIdentity() = %q, want %q", got, "jonathan")
	}

	got = scopedStore.ResolveIdentity("telegram", "unknown")
	if got != "telegram:unknown" {
		t.Errorf("ResolveIdentity() for unknown = %q, want %q", got, "telegram:unknown")
	}
}

func TestScopedStore_GetCanonicalID(t *testing.T) {
	scopedStore := NewScopedStore(NewMemoryStore(), ScopeConfig{
		IdentityLinks: map[string][]string{
			"jonathan": {"slack:U123"},
		},
	})

	got := scopedStore.GetCanonicalID("slack", "U123")
	if got != "jonathan" {
		t.Errorf("GetCanonicalID() = %q, want %q", got, "jonathan")
	}

	got = scopedStore.GetCanonicalID("telegram", "unknown")
	if got != "" {
		t.Errorf("GetCanonicalID() for unknown = %q, want empty string", got)
	}
}

func TestScopedStore_GetLinkedPeers(t *testing.T) {
	scopedStore := NewScopedStore(NewMemoryStore(), ScopeConfig{
		IdentityLinks: map[string][]string{
			"jonathan": {"slack:U123", "discord:user456"},
		},
	})

	peers := scopedStore.GetLinkedPeers("jonathan")
	if len(peers) != 2 {
		t.Errorf("GetLinkedPeers() returned %d peers, want 2", len(peers))
	}
}

func TestScopedStore_BuildKey(t *testing.T) {
	scopedStore := NewScopedStore(NewMemoryStore(), ScopeConfig{
		DMScope: DMScopePerPeer,
	})

	key := scopedStore.BuildKey("agent1", models.ChannelSlack, "U123", false, "")
	expected := "agent1:dm:slack:U123"
	if key != expected {
		t.Errorf("BuildKey() = %q, want %q", key, expected)
	}
}

func TestScopedStore_CheckExpiry(t *testing.T) {
	scopedStore := NewScopedStore(NewMemoryStore(), ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	})

	oldSession := &models.Session{
		Channel:   models.ChannelSlack,
		UpdatedAt: time.Now().Add(-60 * time.Minute),
	}

	if !scopedStore.CheckExpiry(oldSession, ConvTypeDM) {
		t.Error("CheckExpiry() should return true for old session")
	}

	freshSession := &models.Session{
		Channel:   models.ChannelSlack,
		UpdatedAt: time.Now(),
	}

	if scopedStore.CheckExpiry(freshSession, ConvTypeDM) {
		t.Error("CheckExpiry() should return false for fresh session")
	}
}

func TestScopedStore_GetNextResetTime(t *testing.T) {
	scopedStore := NewScopedStore(NewMemoryStore(), ScopeConfig{
		Reset: ResetConfig{
			Mode:   ResetModeDaily,
			AtHour: 9,
		},
	})

	nextReset := scopedStore.GetNextResetTime(models.ChannelSlack, ConvTypeDM)
	if nextReset.IsZero() {
		t.Error("GetNextResetTime() should return non-zero time for daily mode")
	}
}

func TestScopedStore_Store(t *testing.T) {
	memStore := NewMemoryStore()
	scopedStore := NewScopedStore(memStore, ScopeConfig{})

	if scopedStore.Store() != memStore {
		t.Error("Store() should return the underlying store")
	}
}

func TestScopedStore_DelegatedMethods(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{})

	ctx := context.Background()

	// Test Create
	session := &models.Session{
		AgentID: "agent1",
		Channel: models.ChannelSlack,
		Key:     "test-key",
	}
	if err := scopedStore.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Test Get
	retrieved, err := scopedStore.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if retrieved.ID != session.ID {
		t.Error("Get() returned wrong session")
	}

	// Test Update
	session.Title = "Updated Title"
	if err := scopedStore.Update(ctx, session); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Test GetByKey
	byKey, err := scopedStore.GetByKey(ctx, "test-key")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if byKey.Title != "Updated Title" {
		t.Error("GetByKey() returned stale data")
	}

	// Test List
	sessions, err := scopedStore.List(ctx, "agent1", ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("List() returned %d sessions, want 1", len(sessions))
	}

	// Test AppendMessage
	msg := &models.Message{
		Role:    models.RoleUser,
		Content: "Hello",
	}
	if err := scopedStore.AppendMessage(ctx, session.ID, msg); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	// Test GetHistory
	history, err := scopedStore.GetHistory(ctx, session.ID, 10)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 1 {
		t.Errorf("GetHistory() returned %d messages, want 1", len(history))
	}

	// Test Delete
	if err := scopedStore.Delete(ctx, session.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = scopedStore.Get(ctx, session.ID)
	if err == nil {
		t.Error("Get() after Delete() should return error")
	}
}

func TestScopedStore_GetOrCreate(t *testing.T) {
	store := NewMemoryStore()
	scopedStore := NewScopedStore(store, ScopeConfig{})

	ctx := context.Background()

	session, err := scopedStore.GetOrCreate(ctx, "key1", "agent1", models.ChannelSlack, "channel1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if session.Key != "key1" {
		t.Errorf("GetOrCreate() key = %q, want %q", session.Key, "key1")
	}

	// Second call should return the same session
	session2, err := scopedStore.GetOrCreate(ctx, "key1", "agent1", models.ChannelSlack, "channel1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if session2.ID != session.ID {
		t.Error("GetOrCreate() should return existing session")
	}
}

func TestSessionKeyWithScoping(t *testing.T) {
	tests := []struct {
		name     string
		agentID  string
		channel  models.ChannelType
		peerID   string
		isGroup  bool
		threadID string
		cfg      ScopeConfig
		expected string
	}{
		{
			name:     "Main DM scope",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			cfg:      ScopeConfig{DMScope: DMScopeMain},
			expected: "agent1:dm:main",
		},
		{
			name:     "Per-peer DM scope",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "U123",
			isGroup:  false,
			cfg:      ScopeConfig{DMScope: DMScopePerPeer},
			expected: "agent1:dm:slack:U123",
		},
		{
			name:     "Group with thread",
			agentID:  "agent1",
			channel:  models.ChannelSlack,
			peerID:   "C123",
			isGroup:  true,
			threadID: "thread1",
			cfg:      ScopeConfig{DMScope: DMScopeMain},
			expected: "agent1:slack:group:C123:thread1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionKeyWithScoping(tt.agentID, tt.channel, tt.peerID, tt.isGroup, tt.threadID, tt.cfg)
			if got != tt.expected {
				t.Errorf("SessionKeyWithScoping() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestNewScopedStoreWithLocation(t *testing.T) {
	store := NewMemoryStore()
	loc, _ := time.LoadLocation("America/New_York")

	scopedStore := NewScopedStoreWithLocation(store, ScopeConfig{
		Reset: ResetConfig{
			Mode:   ResetModeDaily,
			AtHour: 9,
		},
	}, loc)

	// The expiry checker should use the specified location
	nextReset := scopedStore.GetNextResetTime(models.ChannelSlack, ConvTypeDM)
	if nextReset.IsZero() {
		t.Error("GetNextResetTime() should return non-zero time")
	}
}
