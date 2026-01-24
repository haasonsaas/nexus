package pairing

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSafeChannelKey(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"telegram", "telegram", false},
		{"Telegram", "telegram", false},
		{"DISCORD", "discord", false},
		{"my-channel", "my-channel", false},
		{"my_channel", "my_channel", false},
		{"my/channel", "my_channel", false},
		{"../etc/passwd", "___etc_passwd", false},
		{"", "", true},
		{"   ", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := safeChannelKey(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("safeChannelKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateCode(t *testing.T) {
	code, err := generateCode()
	if err != nil {
		t.Fatalf("generateCode failed: %v", err)
	}

	if len(code) != CodeLength {
		t.Errorf("code length = %d, want %d", len(code), CodeLength)
	}

	// All characters should be from the alphabet
	for _, c := range code {
		if !containsRune(CodeAlphabet, c) {
			t.Errorf("code contains invalid character: %c", c)
		}
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestGenerateUniqueCode(t *testing.T) {
	existing := map[string]bool{
		"AAAAAAAA": true,
		"BBBBBBBB": true,
	}

	code, err := generateUniqueCode(existing)
	if err != nil {
		t.Fatalf("generateUniqueCode failed: %v", err)
	}

	if existing[code] {
		t.Error("generated code conflicts with existing")
	}
}

func TestStore_UpsertAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a request
	code, created, err := store.UpsertRequest("telegram", "user123", map[string]string{"name": "Test"})
	if err != nil {
		t.Fatalf("UpsertRequest failed: %v", err)
	}
	if !created {
		t.Error("expected request to be created")
	}
	if code == "" {
		t.Error("expected non-empty code")
	}

	// List requests
	requests, err := store.ListRequests("telegram")
	if err != nil {
		t.Fatalf("ListRequests failed: %v", err)
	}
	if len(requests) != 1 {
		t.Errorf("expected 1 request, got %d", len(requests))
	}
	if requests[0].ID != "user123" {
		t.Errorf("expected ID user123, got %s", requests[0].ID)
	}
	if requests[0].Code != code {
		t.Errorf("code mismatch")
	}
}

func TestStore_UpsertExisting(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create first request
	code1, created, err := store.UpsertRequest("telegram", "user123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("first request should be created")
	}

	// Upsert same ID
	code2, created, err := store.UpsertRequest("telegram", "user123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("second request should not be created")
	}
	if code2 != code1 {
		t.Error("code should remain the same")
	}
}

func TestStore_ApproveCode(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create request
	code, _, err := store.UpsertRequest("telegram", "user123", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Approve the code
	id, req, err := store.ApproveCode("telegram", code)
	if err != nil {
		t.Fatalf("ApproveCode failed: %v", err)
	}
	if id != "user123" {
		t.Errorf("expected ID user123, got %s", id)
	}
	if req == nil {
		t.Error("expected request to be returned")
	}

	// Request should be removed
	requests, _ := store.ListRequests("telegram")
	if len(requests) != 0 {
		t.Error("request should be removed after approval")
	}

	// Should be in allowlist
	allowed, err := store.IsAllowed("telegram", "user123")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("user should be in allowlist after approval")
	}
}

func TestStore_ApproveCodeNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, _, err := store.ApproveCode("telegram", "INVALIDCODE")
	if err != ErrCodeNotFound {
		t.Errorf("expected ErrCodeNotFound, got %v", err)
	}
}

func TestStore_MaxPending(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create max pending requests
	for i := 0; i < MaxPending; i++ {
		_, _, err := store.UpsertRequest("telegram", "user"+string(rune('0'+i)), nil)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	// Next one should fail
	_, _, err := store.UpsertRequest("telegram", "userOverflow", nil)
	if err != ErrMaxPending {
		t.Errorf("expected ErrMaxPending, got %v", err)
	}
}

func TestStore_Allowlist(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Add to allowlist
	err := store.AddToAllowlist("telegram", "user1")
	if err != nil {
		t.Fatal(err)
	}

	// Check if allowed
	allowed, err := store.IsAllowed("telegram", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("user1 should be allowed")
	}

	// Check not allowed
	allowed, _ = store.IsAllowed("telegram", "user2")
	if allowed {
		t.Error("user2 should not be allowed")
	}

	// Remove from allowlist
	err = store.RemoveFromAllowlist("telegram", "user1")
	if err != nil {
		t.Fatal(err)
	}

	// Should no longer be allowed
	allowed, _ = store.IsAllowed("telegram", "user1")
	if allowed {
		t.Error("user1 should no longer be allowed")
	}
}

func TestStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a request to generate files
	_, _, err := store.UpsertRequest("telegram", "user1", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Check file permissions
	path := filepath.Join(dir, "telegram-pairing.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// Should be 0600
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestRequest_IsExpired(t *testing.T) {
	r := &Request{
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if !r.IsExpired() {
		t.Error("request should be expired")
	}

	r.CreatedAt = time.Now().Add(-30 * time.Minute)
	if r.IsExpired() {
		t.Error("request should not be expired")
	}
}

func TestStore_PrunesExpired(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a request
	_, _, err := store.UpsertRequest("telegram", "user1", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Manually expire it by reading and modifying
	storeData, _ := store.readStore("telegram")
	storeData.Requests[0].CreatedAt = time.Now().Add(-2 * time.Hour)
	store.writeStore("telegram", storeData)

	// List should prune it
	requests, err := store.ListRequests("telegram")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Error("expired request should be pruned")
	}
}

func TestStore_DifferentChannels(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create requests on different channels
	_, _, err := store.UpsertRequest("telegram", "user1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.UpsertRequest("discord", "user2", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should have separate lists
	telegramReqs, _ := store.ListRequests("telegram")
	discordReqs, _ := store.ListRequests("discord")

	if len(telegramReqs) != 1 || telegramReqs[0].ID != "user1" {
		t.Error("telegram requests incorrect")
	}
	if len(discordReqs) != 1 || discordReqs[0].ID != "user2" {
		t.Error("discord requests incorrect")
	}
}
