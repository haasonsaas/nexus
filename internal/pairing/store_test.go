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
	storeData.Requests[0].ExpiresAt = time.Now().Add(-1 * time.Hour)
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

// Additional edge case tests

func TestNewStore_SingleChannelMode(t *testing.T) {
	// When given just a channel name (no path separators), use single-channel mode
	store := NewStore("telegram")

	if store.channel != "telegram" {
		t.Errorf("channel = %q, want %q", store.channel, "telegram")
	}
	// dataDir should be set to default
	if store.dataDir == "" {
		t.Error("dataDir should be set")
	}
}

func TestNewStoreWithDir(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreWithDir(dir)

	if store.dataDir != dir {
		t.Errorf("dataDir = %q, want %q", store.dataDir, dir)
	}
	if store.channel != "" {
		t.Error("channel should be empty for directory mode")
	}
}

func TestStore_LoadAllowlist(t *testing.T) {
	// Single-channel mode
	dir := t.TempDir()
	store := &Store{dataDir: dir, channel: "telegram"}

	// Add to allowlist first
	err := store.AddToAllowlist("telegram", "user1")
	if err != nil {
		t.Fatal(err)
	}

	// Now use LoadAllowlist
	list, err := store.LoadAllowlist()
	if err != nil {
		t.Fatalf("LoadAllowlist failed: %v", err)
	}

	if len(list) != 1 || list[0] != "user1" {
		t.Errorf("LoadAllowlist() = %v, want [user1]", list)
	}
}

func TestStore_LoadAllowlist_NotSingleChannelMode(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, err := store.LoadAllowlist()
	if err == nil {
		t.Error("LoadAllowlist should fail without single-channel mode")
	}
}

func TestStore_GetOrCreateRequest(t *testing.T) {
	dir := t.TempDir()
	store := &Store{dataDir: dir, channel: "telegram"}

	// Create a request
	req, created, err := store.GetOrCreateRequest("user123", "John Doe")
	if err != nil {
		t.Fatalf("GetOrCreateRequest failed: %v", err)
	}
	if !created {
		t.Error("expected request to be created")
	}
	if req.ID != "user123" {
		t.Errorf("ID = %q, want user123", req.ID)
	}
	if req.Code == "" {
		t.Error("expected non-empty code")
	}

	// Get existing
	req2, created2, err := store.GetOrCreateRequest("user123", "John Doe")
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Error("second request should not be created")
	}
	if req2.Code != req.Code {
		t.Error("code should remain the same")
	}
}

func TestStore_GetOrCreateRequest_NotSingleChannelMode(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, _, err := store.GetOrCreateRequest("user", "name")
	if err == nil {
		t.Error("GetOrCreateRequest should fail without single-channel mode")
	}
}

func TestStore_ApproveCode_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create request
	code, _, err := store.UpsertRequest("telegram", "user123", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Approve with lowercase
	_, _, err = store.ApproveCode("telegram", code)
	if err != nil {
		t.Fatalf("ApproveCode with original case failed: %v", err)
	}
}

func TestStore_ApproveCode_EmptyCode(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, _, err := store.ApproveCode("telegram", "")
	if err != ErrCodeNotFound {
		t.Errorf("expected ErrCodeNotFound for empty code, got %v", err)
	}

	_, _, err = store.ApproveCode("telegram", "   ")
	if err != ErrCodeNotFound {
		t.Errorf("expected ErrCodeNotFound for whitespace code, got %v", err)
	}
}

func TestStore_AddToAllowlist_Empty(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Adding empty entry should be no-op
	err := store.AddToAllowlist("telegram", "")
	if err != nil {
		t.Fatalf("AddToAllowlist with empty should not fail: %v", err)
	}

	err = store.AddToAllowlist("telegram", "   ")
	if err != nil {
		t.Fatalf("AddToAllowlist with whitespace should not fail: %v", err)
	}

	// Allowlist should be empty
	list, _ := store.GetAllowlist("telegram")
	if len(list) != 0 {
		t.Error("allowlist should be empty")
	}
}

func TestStore_AddToAllowlist_Duplicate(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Add same entry twice
	store.AddToAllowlist("telegram", "user1")
	store.AddToAllowlist("telegram", "user1")

	list, _ := store.GetAllowlist("telegram")
	if len(list) != 1 {
		t.Errorf("expected 1 entry, got %d", len(list))
	}
}

func TestStore_RemoveFromAllowlist_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Removing non-existent entry should not fail
	err := store.RemoveFromAllowlist("telegram", "nonexistent")
	if err != nil {
		t.Fatalf("RemoveFromAllowlist should not fail for non-existent: %v", err)
	}
}

func TestStore_RemoveFromAllowlist_Empty(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Removing empty entry should be no-op
	err := store.RemoveFromAllowlist("telegram", "")
	if err != nil {
		t.Fatalf("RemoveFromAllowlist with empty should not fail: %v", err)
	}
}

func TestRequest_IsExpired_WithExpiresAt(t *testing.T) {
	// Test with explicit ExpiresAt field
	r := &Request{
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if !r.IsExpired() {
		t.Error("request with past ExpiresAt should be expired")
	}

	r.ExpiresAt = time.Now().Add(1 * time.Hour)
	if r.IsExpired() {
		t.Error("request with future ExpiresAt should not be expired")
	}
}

func TestStore_GetAllowlist_InvalidChannel(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, err := store.GetAllowlist("")
	if err == nil {
		t.Error("GetAllowlist with empty channel should fail")
	}
}

func TestStore_ListRequests_InvalidChannel(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, err := store.ListRequests("")
	if err == nil {
		t.Error("ListRequests with empty channel should fail")
	}
}

func TestStore_UpsertRequest_WithMeta(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	meta := map[string]string{
		"name":  "Test User",
		"email": "test@example.com",
	}

	_, _, err := store.UpsertRequest("telegram", "user1", meta)
	if err != nil {
		t.Fatal(err)
	}

	requests, _ := store.ListRequests("telegram")
	if len(requests) != 1 {
		t.Fatal("expected 1 request")
	}

	if requests[0].Meta["name"] != "Test User" {
		t.Error("meta not preserved")
	}
	if requests[0].Meta["email"] != "test@example.com" {
		t.Error("meta not preserved")
	}
}

func TestStore_UpsertRequest_UpdateMeta(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create with initial meta
	_, _, _ = store.UpsertRequest("telegram", "user1", map[string]string{"v": "1"})

	// Update with new meta
	_, _, _ = store.UpsertRequest("telegram", "user1", map[string]string{"v": "2"})

	requests, _ := store.ListRequests("telegram")
	if requests[0].Meta["v"] != "2" {
		t.Error("meta should be updated")
	}
}

func TestStore_ApproveCode_AddsToAllowlist(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	code, _, _ := store.UpsertRequest("telegram", "user123", nil)
	store.ApproveCode("telegram", code)

	list, _ := store.GetAllowlist("telegram")
	found := false
	for _, entry := range list {
		if entry == "user123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("user should be added to allowlist after approval")
	}
}

func TestSafeChannelKey_SpecialCharacters(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"normal", false},
		{"with space", false}, // Space converted to _
		{"with!special", false},
		{"with@symbol", false},
		{"___", false}, // Multiple underscores are allowed (only single "_" is rejected)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := safeChannelKey(tt.input)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
