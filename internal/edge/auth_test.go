package edge

import (
	"context"
	"testing"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

func TestTokenAuthenticator(t *testing.T) {
	tokens := map[string]string{
		"edge1": "secret1",
		"edge2": "secret2",
	}
	auth := NewTokenAuthenticator(tokens)

	tests := []struct {
		name      string
		edgeID    string
		token     string
		wantErr   bool
		wantEdge  string
	}{
		{"valid edge1", "edge1", "secret1", false, "edge1"},
		{"valid edge2", "edge2", "secret2", false, "edge2"},
		{"wrong token", "edge1", "wrong", true, ""},
		{"unknown edge", "edge3", "secret3", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &pb.EdgeRegister{
				EdgeId:    tt.edgeID,
				AuthToken: tt.token,
			}
			edgeID, err := auth.Authenticate(context.Background(), reg)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.wantErr && edgeID != tt.wantEdge {
				t.Errorf("expected edge %s, got %s", tt.wantEdge, edgeID)
			}
		})
	}
}

func TestTokenAuthenticatorAddRemove(t *testing.T) {
	auth := NewTokenAuthenticator(nil)

	// Initially should fail
	reg := &pb.EdgeRegister{EdgeId: "edge1", AuthToken: "secret1"}
	_, err := auth.Authenticate(context.Background(), reg)
	if err == nil {
		t.Error("expected error before adding edge")
	}

	// Add edge
	auth.AddEdge("edge1", "secret1")
	edgeID, err := auth.Authenticate(context.Background(), reg)
	if err != nil {
		t.Errorf("unexpected error after adding: %v", err)
	}
	if edgeID != "edge1" {
		t.Errorf("expected edge1, got %s", edgeID)
	}

	// Remove edge
	auth.RemoveEdge("edge1")
	_, err = auth.Authenticate(context.Background(), reg)
	if err == nil {
		t.Error("expected error after removing edge")
	}
}

func TestDevAuthenticator(t *testing.T) {
	auth := NewDevAuthenticator()

	// Should accept any edge
	tests := []struct {
		edgeID string
		token  string
	}{
		{"edge1", "any-token"},
		{"edge2", ""},
		{"random", "random-token"},
	}

	for _, tt := range tests {
		reg := &pb.EdgeRegister{EdgeId: tt.edgeID, AuthToken: tt.token}
		edgeID, err := auth.Authenticate(context.Background(), reg)
		if err != nil {
			t.Errorf("unexpected error for %s: %v", tt.edgeID, err)
		}
		if edgeID != tt.edgeID {
			t.Errorf("expected %s, got %s", tt.edgeID, edgeID)
		}
	}
}

func TestTOFUAuthenticator(t *testing.T) {
	pendingCalled := false
	auth := NewTOFUAuthenticator(func(edgeID, name string) {
		pendingCalled = true
	})

	// First connection should go pending
	ctx, cancel := context.WithCancel(context.Background())
	reg := &pb.EdgeRegister{
		EdgeId:    "edge1",
		Name:      "Test Edge",
		AuthToken: "token1",
	}

	// Start authentication in goroutine
	done := make(chan error, 1)
	go func() {
		_, err := auth.Authenticate(ctx, reg)
		done <- err
	}()

	// Wait a bit for pending to be called
	// In a real test we'd use channels properly
	cancel() // Cancel to unblock

	<-done

	if !pendingCalled {
		t.Error("expected pending callback to be called")
	}
}

func TestTOFUAuthenticatorApprove(t *testing.T) {
	auth := NewTOFUAuthenticator(nil)

	// Approve without pending should fail
	err := auth.Approve("edge1", "admin")
	if err == nil {
		t.Error("expected error approving non-pending edge")
	}

	// Reject without pending should fail
	err = auth.Reject("edge1")
	if err == nil {
		t.Error("expected error rejecting non-pending edge")
	}
}

func TestTOFUAuthenticatorLists(t *testing.T) {
	auth := NewTOFUAuthenticator(nil)

	// Initially empty
	if len(auth.ListApproved()) != 0 {
		t.Error("expected empty approved list")
	}
	if len(auth.ListPending()) != 0 {
		t.Error("expected empty pending list")
	}
}

func TestCompositeAuthenticator(t *testing.T) {
	// First auth rejects, second accepts
	tokens := map[string]string{"edge2": "secret2"}
	auth1 := NewTokenAuthenticator(nil)         // Rejects all
	auth2 := NewTokenAuthenticator(tokens)      // Accepts edge2

	composite := NewCompositeAuthenticator(auth1, auth2)

	// Should fail for unknown edge
	reg1 := &pb.EdgeRegister{EdgeId: "edge1", AuthToken: "secret1"}
	_, err := composite.Authenticate(context.Background(), reg1)
	if err == nil {
		t.Error("expected error for unknown edge")
	}

	// Should succeed for edge2 via second authenticator
	reg2 := &pb.EdgeRegister{EdgeId: "edge2", AuthToken: "secret2"}
	edgeID, err := composite.Authenticate(context.Background(), reg2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if edgeID != "edge2" {
		t.Errorf("expected edge2, got %s", edgeID)
	}
}

func TestCompositeAuthenticatorEmpty(t *testing.T) {
	composite := NewCompositeAuthenticator()

	reg := &pb.EdgeRegister{EdgeId: "edge1", AuthToken: "token"}
	_, err := composite.Authenticate(context.Background(), reg)
	if err == nil {
		t.Error("expected error with no authenticators")
	}
}
