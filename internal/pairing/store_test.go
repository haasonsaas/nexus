package pairing

import "testing"

func TestStoreGetOrCreateRequestReusesPending(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreWithDir("telegram", dir)

	req1, created1, err := store.GetOrCreateRequest("user-1", "Alice")
	if err != nil {
		t.Fatalf("GetOrCreateRequest() error = %v", err)
	}
	if !created1 {
		t.Fatalf("expected first request to be created")
	}

	req2, created2, err := store.GetOrCreateRequest("user-1", "Alice")
	if err != nil {
		t.Fatalf("GetOrCreateRequest() error = %v", err)
	}
	if created2 {
		t.Fatalf("expected second request to reuse pending request")
	}
	if req1.Code != req2.Code {
		t.Fatalf("expected same code, got %q and %q", req1.Code, req2.Code)
	}
}

func TestStoreApproveMovesToAllowlist(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreWithDir("discord", dir)

	req, _, err := store.GetOrCreateRequest("user-2", "")
	if err != nil {
		t.Fatalf("GetOrCreateRequest() error = %v", err)
	}

	if _, err := store.Approve(req.Code); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}

	allowlist, err := store.LoadAllowlist()
	if err != nil {
		t.Fatalf("LoadAllowlist() error = %v", err)
	}
	if len(allowlist) != 1 || allowlist[0] != "user-2" {
		t.Fatalf("expected allowlist to contain user-2, got %v", allowlist)
	}

	pending, err := store.Pending()
	if err != nil {
		t.Fatalf("Pending() error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected pending to be empty, got %v", pending)
	}
}

func TestStoreDenyRemovesPending(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreWithDir("slack", dir)

	req, _, err := store.GetOrCreateRequest("user-3", "")
	if err != nil {
		t.Fatalf("GetOrCreateRequest() error = %v", err)
	}

	if _, err := store.Deny(req.Code); err != nil {
		t.Fatalf("Deny() error = %v", err)
	}

	pending, err := store.Pending()
	if err != nil {
		t.Fatalf("Pending() error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected pending to be empty, got %v", pending)
	}
}
