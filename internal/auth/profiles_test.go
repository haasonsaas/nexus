package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewProfileStore(t *testing.T) {
	store := newProfileStore()
	if store == nil {
		t.Fatal("newProfileStore returned nil")
	}
	if store.Version != profilesVersion {
		t.Errorf("Version = %d, want %d", store.Version, profilesVersion)
	}
	if store.Profiles == nil {
		t.Error("Profiles map is nil")
	}
	if store.Order == nil {
		t.Error("Order map is nil")
	}
	if store.LastGood == nil {
		t.Error("LastGood map is nil")
	}
	if store.UsageStats == nil {
		t.Error("UsageStats map is nil")
	}
}

func TestAddProfile(t *testing.T) {
	store := newProfileStore()

	cred := ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-test123",
		Email:    "test@example.com",
	}

	store.AddProfile("openai-main", cred)

	if len(store.Profiles) != 1 {
		t.Errorf("Profiles count = %d, want 1", len(store.Profiles))
	}

	got, ok := store.Profiles["openai-main"]
	if !ok {
		t.Fatal("profile not found")
	}
	if got.Key != "sk-test123" {
		t.Errorf("Key = %q, want %q", got.Key, "sk-test123")
	}

	// Check order was updated
	order := store.Order["openai"]
	if len(order) != 1 || order[0] != "openai-main" {
		t.Errorf("Order = %v, want [openai-main]", order)
	}
}

func TestAddProfileDuplicateOrder(t *testing.T) {
	store := newProfileStore()

	cred := ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-test123",
	}

	store.AddProfile("openai-main", cred)
	store.AddProfile("openai-main", cred) // Add again

	// Should not duplicate in order
	order := store.Order["openai"]
	if len(order) != 1 {
		t.Errorf("Order length = %d, want 1", len(order))
	}
}

func TestRemoveProfile(t *testing.T) {
	store := newProfileStore()

	cred := ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-test123",
	}

	store.AddProfile("openai-main", cred)
	store.MarkSuccess("openai-main")

	store.RemoveProfile("openai-main")

	if len(store.Profiles) != 0 {
		t.Errorf("Profiles count = %d, want 0", len(store.Profiles))
	}
	if len(store.Order["openai"]) != 0 {
		t.Errorf("Order still has entries: %v", store.Order["openai"])
	}
	if _, ok := store.LastGood["openai"]; ok {
		t.Error("LastGood should be cleared")
	}
	if _, ok := store.UsageStats["openai-main"]; ok {
		t.Error("UsageStats should be cleared")
	}
}

func TestGetCredential(t *testing.T) {
	store := newProfileStore()

	cred := ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-test123",
	}
	store.AddProfile("openai-main", cred)

	got, profileID, err := store.GetCredential("openai")
	if err != nil {
		t.Fatalf("GetCredential error: %v", err)
	}
	if profileID != "openai-main" {
		t.Errorf("profileID = %q, want %q", profileID, "openai-main")
	}
	if got.Key != "sk-test123" {
		t.Errorf("Key = %q, want %q", got.Key, "sk-test123")
	}
}

func TestGetCredentialNoProfiles(t *testing.T) {
	store := newProfileStore()

	_, _, err := store.GetCredential("openai")
	if err != ErrNoProfiles {
		t.Errorf("error = %v, want ErrNoProfiles", err)
	}
}

func TestGetCredentialNilStore(t *testing.T) {
	var store *ProfileStore
	_, _, err := store.GetCredential("openai")
	if err != ErrNoProfiles {
		t.Errorf("error = %v, want ErrNoProfiles", err)
	}
}

func TestGetCredentialPrefersLastGood(t *testing.T) {
	store := newProfileStore()

	// Add two profiles
	store.AddProfile("openai-a", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-a",
	})
	store.AddProfile("openai-b", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-b",
	})

	// Set order: a, b
	store.SetOrder("openai", []string{"openai-a", "openai-b"})

	// Mark b as lastGood
	store.MarkSuccess("openai-b")

	// Should prefer lastGood
	got, profileID, err := store.GetCredential("openai")
	if err != nil {
		t.Fatalf("GetCredential error: %v", err)
	}
	if profileID != "openai-b" {
		t.Errorf("profileID = %q, want %q (lastGood)", profileID, "openai-b")
	}
	if got.Key != "sk-b" {
		t.Errorf("Key = %q, want %q", got.Key, "sk-b")
	}
}

func TestGetCredentialRotatesOnFailure(t *testing.T) {
	store := newProfileStore()
	store.CooldownSecs = 1 // Short cooldown for testing

	// Add two profiles
	store.AddProfile("openai-a", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-a",
	})
	store.AddProfile("openai-b", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-b",
	})

	store.SetOrder("openai", []string{"openai-a", "openai-b"})

	// Get first (should be a)
	_, profileID, err := store.GetCredential("openai")
	if err != nil {
		t.Fatalf("GetCredential error: %v", err)
	}
	if profileID != "openai-a" {
		t.Errorf("first profileID = %q, want %q", profileID, "openai-a")
	}

	// Mark a as failed
	store.MarkFailure("openai-a")

	// Should now get b
	_, profileID, err = store.GetCredential("openai")
	if err != nil {
		t.Fatalf("GetCredential error: %v", err)
	}
	if profileID != "openai-b" {
		t.Errorf("after failure profileID = %q, want %q", profileID, "openai-b")
	}
}

func TestGetCredentialAllInCooldown(t *testing.T) {
	store := newProfileStore()
	store.CooldownSecs = 60 // Longer cooldown

	store.AddProfile("openai-a", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-a",
	})

	store.MarkFailure("openai-a")

	_, _, err := store.GetCredential("openai")
	if err != ErrAllInCooldown {
		t.Errorf("error = %v, want ErrAllInCooldown", err)
	}
}

func TestCooldownExpiry(t *testing.T) {
	store := newProfileStore()
	store.CooldownSecs = 1 // 1 second cooldown

	store.AddProfile("openai-a", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-a",
	})

	store.MarkFailure("openai-a")

	// Wait for cooldown to expire
	time.Sleep(1100 * time.Millisecond)

	_, profileID, err := store.GetCredential("openai")
	if err != nil {
		t.Fatalf("GetCredential error after cooldown: %v", err)
	}
	if profileID != "openai-a" {
		t.Errorf("profileID = %q, want %q", profileID, "openai-a")
	}
}

func TestSuccessResetsCooldown(t *testing.T) {
	store := newProfileStore()
	store.CooldownSecs = 60

	store.AddProfile("openai-a", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-a",
	})

	store.MarkFailure("openai-a")

	// Verify in cooldown
	_, _, err := store.GetCredential("openai")
	if err != ErrAllInCooldown {
		t.Fatalf("expected to be in cooldown")
	}

	// Mark success
	store.MarkSuccess("openai-a")

	// Should now be available
	_, profileID, err := store.GetCredential("openai")
	if err != nil {
		t.Fatalf("GetCredential error: %v", err)
	}
	if profileID != "openai-a" {
		t.Errorf("profileID = %q, want %q", profileID, "openai-a")
	}

	// FailCount should be reset
	stats := store.GetStats("openai-a")
	if stats.FailCount != 0 {
		t.Errorf("FailCount = %d, want 0", stats.FailCount)
	}
}

func TestMarkSuccessUpdatesLastGood(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("openai-a", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-a",
	})

	store.MarkSuccess("openai-a")

	if store.LastGood["openai"] != "openai-a" {
		t.Errorf("LastGood = %q, want %q", store.LastGood["openai"], "openai-a")
	}
}

func TestMarkFailureClearsLastGood(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("openai-a", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-a",
	})

	store.MarkSuccess("openai-a")
	store.MarkFailure("openai-a")

	if _, ok := store.LastGood["openai"]; ok {
		t.Error("LastGood should be cleared after failure")
	}
}

func TestResolveProfileOrder(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("openai-c", ProfileCredential{Provider: "openai"})
	store.AddProfile("openai-a", ProfileCredential{Provider: "openai"})
	store.AddProfile("openai-b", ProfileCredential{Provider: "openai"})

	t.Run("no explicit order falls back to alphabetical", func(t *testing.T) {
		store.Order = make(map[string][]string) // Clear order
		order := store.ResolveProfileOrder("openai")
		expected := []string{"openai-a", "openai-b", "openai-c"}
		if len(order) != len(expected) {
			t.Fatalf("order length = %d, want %d", len(order), len(expected))
		}
		for i, id := range expected {
			if order[i] != id {
				t.Errorf("order[%d] = %q, want %q", i, order[i], id)
			}
		}
	})

	t.Run("explicit order is respected", func(t *testing.T) {
		store.SetOrder("openai", []string{"openai-b", "openai-c", "openai-a"})
		order := store.ResolveProfileOrder("openai")
		expected := []string{"openai-b", "openai-c", "openai-a"}
		if len(order) != len(expected) {
			t.Fatalf("order length = %d, want %d", len(order), len(expected))
		}
		for i, id := range expected {
			if order[i] != id {
				t.Errorf("order[%d] = %q, want %q", i, order[i], id)
			}
		}
	})
}

func TestResolveProfileOrderFiltersMissing(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("openai-a", ProfileCredential{Provider: "openai"})
	store.SetOrder("openai", []string{"openai-missing", "openai-a"})

	order := store.ResolveProfileOrder("openai")
	if len(order) != 1 || order[0] != "openai-a" {
		t.Errorf("order = %v, want [openai-a]", order)
	}
}

func TestGetProfile(t *testing.T) {
	store := newProfileStore()

	cred := ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-test",
	}
	store.AddProfile("openai-main", cred)

	got, err := store.GetProfile("openai-main")
	if err != nil {
		t.Fatalf("GetProfile error: %v", err)
	}
	if got.Key != "sk-test" {
		t.Errorf("Key = %q, want %q", got.Key, "sk-test")
	}
}

func TestGetProfileNotFound(t *testing.T) {
	store := newProfileStore()

	_, err := store.GetProfile("missing")
	if err != ErrProfileNotFound {
		t.Errorf("error = %v, want ErrProfileNotFound", err)
	}
}

func TestListProviders(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("openai-a", ProfileCredential{Provider: "openai"})
	store.AddProfile("anthropic-a", ProfileCredential{Provider: "anthropic"})
	store.AddProfile("openai-b", ProfileCredential{Provider: "openai"})

	providers := store.ListProviders()
	if len(providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(providers))
	}
	// Should be sorted
	if providers[0] != "anthropic" || providers[1] != "openai" {
		t.Errorf("providers = %v, want [anthropic, openai]", providers)
	}
}

func TestListProfiles(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("openai-c", ProfileCredential{Provider: "openai"})
	store.AddProfile("anthropic-a", ProfileCredential{Provider: "anthropic"})
	store.AddProfile("openai-a", ProfileCredential{Provider: "openai"})

	profiles := store.ListProfiles("openai")
	if len(profiles) != 2 {
		t.Fatalf("profiles count = %d, want 2", len(profiles))
	}
	// Should be sorted
	if profiles[0] != "openai-a" || profiles[1] != "openai-c" {
		t.Errorf("profiles = %v, want [openai-a, openai-c]", profiles)
	}
}

func TestLoadSaveProfileStore(t *testing.T) {
	tmpDir := t.TempDir()

	// Create and save a store
	store := newProfileStore()
	store.AddProfile("openai-main", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "sk-test123",
		Email:    "test@example.com",
	})
	store.MarkSuccess("openai-main")

	if err := SaveProfileStore(store, tmpDir); err != nil {
		t.Fatalf("SaveProfileStore error: %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmpDir, profilesFilename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("profiles file not created")
	}

	// Load and verify
	loaded, err := LoadProfileStore(tmpDir)
	if err != nil {
		t.Fatalf("LoadProfileStore error: %v", err)
	}

	cred, ok := loaded.Profiles["openai-main"]
	if !ok {
		t.Fatal("profile not found after load")
	}
	if cred.Key != "sk-test123" {
		t.Errorf("Key = %q, want %q", cred.Key, "sk-test123")
	}
	if loaded.LastGood["openai"] != "openai-main" {
		t.Errorf("LastGood = %q, want %q", loaded.LastGood["openai"], "openai-main")
	}
}

func TestLoadProfileStoreNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := LoadProfileStore(tmpDir)
	if err != nil {
		t.Fatalf("LoadProfileStore error: %v", err)
	}
	if store == nil {
		t.Fatal("store should not be nil")
	}
	if len(store.Profiles) != 0 {
		t.Error("store should be empty")
	}
}

func TestSaveProfileStoreNil(t *testing.T) {
	tmpDir := t.TempDir()

	err := SaveProfileStore(nil, tmpDir)
	if err != nil {
		t.Errorf("SaveProfileStore(nil) error: %v", err)
	}
}

func TestSaveProfileStoreCreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "nested", "state")

	store := newProfileStore()
	store.AddProfile("test", ProfileCredential{Provider: "test"})

	if err := SaveProfileStore(store, stateDir); err != nil {
		t.Fatalf("SaveProfileStore error: %v", err)
	}

	path := filepath.Join(stateDir, profilesFilename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("profiles file not created in nested dir")
	}
}

func TestCredentialTypes(t *testing.T) {
	tests := []struct {
		name   string
		cred   ProfileCredential
		verify func(t *testing.T, cred ProfileCredential)
	}{
		{
			name: "api_key",
			cred: ProfileCredential{
				Type:     CredentialAPIKey,
				Provider: "openai",
				Key:      "sk-test",
			},
			verify: func(t *testing.T, cred ProfileCredential) {
				if cred.Type != CredentialAPIKey {
					t.Errorf("Type = %q, want %q", cred.Type, CredentialAPIKey)
				}
				if cred.Key == "" {
					t.Error("Key should be set for api_key type")
				}
			},
		},
		{
			name: "oauth",
			cred: ProfileCredential{
				Type:     CredentialOAuth,
				Provider: "google",
				Access:   "access-token",
				Refresh:  "refresh-token",
				Expires:  time.Now().Add(time.Hour).Unix(),
			},
			verify: func(t *testing.T, cred ProfileCredential) {
				if cred.Type != CredentialOAuth {
					t.Errorf("Type = %q, want %q", cred.Type, CredentialOAuth)
				}
				if cred.Access == "" || cred.Refresh == "" {
					t.Error("Access and Refresh should be set for oauth type")
				}
			},
		},
		{
			name: "token",
			cred: ProfileCredential{
				Type:     CredentialToken,
				Provider: "github",
				Token:    "ghp_xxxx",
			},
			verify: func(t *testing.T, cred ProfileCredential) {
				if cred.Type != CredentialToken {
					t.Errorf("Type = %q, want %q", cred.Type, CredentialToken)
				}
				if cred.Token == "" {
					t.Error("Token should be set for token type")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newProfileStore()
			store.AddProfile("test-"+tt.name, tt.cred)

			got, err := store.GetProfile("test-" + tt.name)
			if err != nil {
				t.Fatalf("GetProfile error: %v", err)
			}
			tt.verify(t, *got)
		})
	}
}

func TestNilStoreMethods(t *testing.T) {
	var store *ProfileStore

	// These should not panic
	store.MarkSuccess("test")
	store.MarkFailure("test")
	store.AddProfile("test", ProfileCredential{})
	store.RemoveProfile("test")
	store.SetOrder("test", nil)

	if store.ResolveProfileOrder("test") != nil {
		t.Error("ResolveProfileOrder should return nil for nil store")
	}
	if store.ListProviders() != nil {
		t.Error("ListProviders should return nil for nil store")
	}
	if store.ListProfiles("test") != nil {
		t.Error("ListProfiles should return nil for nil store")
	}

	_, err := store.GetProfile("test")
	if err != ErrProfileNotFound {
		t.Errorf("GetProfile error = %v, want ErrProfileNotFound", err)
	}

	stats := store.GetStats("test")
	if stats.LastUsed != 0 {
		t.Error("GetStats should return zero stats for nil store")
	}
}

func TestGetCredentialCopiesData(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("test", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "original",
	})

	cred, _, err := store.GetCredential("openai")
	if err != nil {
		t.Fatalf("GetCredential error: %v", err)
	}

	// Modify returned credential
	cred.Key = "modified"

	// Original should be unchanged
	original, _ := store.GetProfile("test")
	if original.Key != "original" {
		t.Error("GetCredential should return a copy, not modify original")
	}
}

func TestGetProfileCopiesData(t *testing.T) {
	store := newProfileStore()

	store.AddProfile("test", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "original",
	})

	cred, err := store.GetProfile("test")
	if err != nil {
		t.Fatalf("GetProfile error: %v", err)
	}

	// Modify returned credential
	cred.Key = "modified"

	// Original should be unchanged
	original, _ := store.GetProfile("test")
	if original.Key != "original" {
		t.Error("GetProfile should return a copy, not modify original")
	}
}

func TestFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()

	store := newProfileStore()
	store.AddProfile("test", ProfileCredential{
		Type:     CredentialAPIKey,
		Provider: "openai",
		Key:      "secret-key",
	})

	if err := SaveProfileStore(store, tmpDir); err != nil {
		t.Fatalf("SaveProfileStore error: %v", err)
	}

	path := filepath.Join(tmpDir, profilesFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}

	// Should have restrictive permissions (0600)
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}
