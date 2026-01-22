package auth

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestEdgeAuthService_SharedSecret(t *testing.T) {
	store := NewMemoryEdgeStore()
	service := NewEdgeAuthService(EdgeAuthConfig{
		Store:         store,
		SessionExpiry: time.Hour,
	})

	t.Run("new device registration", func(t *testing.T) {
		resp, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "device1",
			EdgeName:        "Test Device",
			AuthMethod:      AuthMethodSharedSecret,
			SharedSecret:    "my-secret-key-123",
			ProtocolVersion: "1.0",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.ErrorMessage)
		}
		if resp.Session == nil {
			t.Error("expected session, got nil")
		}
		if resp.TrustLevel != TrustUntrusted {
			t.Errorf("expected untrusted trust level, got %s", resp.TrustLevel)
		}

		// Verify device was stored
		device, err := store.GetEdge("device1")
		if err != nil {
			t.Fatalf("device not stored: %v", err)
		}
		if device.Name != "Test Device" {
			t.Errorf("expected name 'Test Device', got %s", device.Name)
		}
	})

	t.Run("valid authentication", func(t *testing.T) {
		resp, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "device1",
			EdgeName:        "Test Device",
			AuthMethod:      AuthMethodSharedSecret,
			SharedSecret:    "my-secret-key-123",
			ProtocolVersion: "1.0",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Success {
			t.Errorf("expected success, got failure: %s", resp.ErrorMessage)
		}
	})

	t.Run("invalid secret", func(t *testing.T) {
		resp, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "device1",
			EdgeName:        "Test Device",
			AuthMethod:      AuthMethodSharedSecret,
			SharedSecret:    "wrong-secret",
			ProtocolVersion: "1.0",
		})

		if err != ErrInvalidSecret {
			t.Errorf("expected ErrInvalidSecret, got %v", err)
		}
		if resp.Success {
			t.Error("expected failure")
		}
	})
}

func TestEdgeAuthService_TOFU(t *testing.T) {
	store := NewMemoryEdgeStore()
	service := NewEdgeAuthService(EdgeAuthConfig{
		Store:         store,
		SessionExpiry: time.Hour,
	})

	// Generate key pair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	t.Run("initial TOFU registration", func(t *testing.T) {
		resp, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "tofu-device",
			EdgeName:        "TOFU Test Device",
			AuthMethod:      AuthMethodTOFU,
			PublicKey:       publicKey,
			ProtocolVersion: "1.0",
		})

		if err != ErrTOFUPending {
			t.Errorf("expected ErrTOFUPending, got %v", err)
		}
		if resp.Success {
			t.Error("expected pending state, not success")
		}
		if resp.TrustLevel != TrustTOFUPending {
			t.Errorf("expected TOFU pending trust level, got %s", resp.TrustLevel)
		}
		if len(resp.Challenge) == 0 {
			t.Error("expected challenge to be returned")
		}

		// Verify device was stored in pending state
		device, err := store.GetEdge("tofu-device")
		if err != nil {
			t.Fatalf("device not stored: %v", err)
		}
		if device.TrustLevel != TrustTOFUPending {
			t.Errorf("expected TOFU pending, got %s", device.TrustLevel)
		}
	})

	t.Run("complete TOFU with signature", func(t *testing.T) {
		// First request to get challenge
		resp1, _ := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "tofu-device",
			EdgeName:        "TOFU Test Device",
			AuthMethod:      AuthMethodTOFU,
			PublicKey:       publicKey,
			ProtocolVersion: "1.0",
		})

		if len(resp1.Challenge) == 0 {
			t.Fatal("no challenge returned")
		}

		// Sign the challenge
		signature := ed25519.Sign(privateKey, resp1.Challenge)

		// Complete authentication with signature
		resp2, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "tofu-device",
			EdgeName:        "TOFU Test Device",
			AuthMethod:      AuthMethodTOFU,
			PublicKey:       publicKey,
			Signature:       signature,
			ProtocolVersion: "1.0",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp2.Success {
			t.Errorf("expected success, got failure: %s", resp2.ErrorMessage)
		}
		if resp2.Session == nil {
			t.Error("expected session")
		}
		if resp2.TrustLevel != TrustTOFU {
			t.Errorf("expected TOFU trust level, got %s", resp2.TrustLevel)
		}

		// Verify device trust level was upgraded
		device, _ := store.GetEdge("tofu-device")
		if device.TrustLevel != TrustTOFU {
			t.Errorf("expected device trust level TOFU, got %s", device.TrustLevel)
		}
	})

	t.Run("invalid signature rejected", func(t *testing.T) {
		// Register another device
		_, _ = service.Authenticate(EdgeAuthRequest{
			EdgeID:          "tofu-device2",
			EdgeName:        "TOFU Test Device 2",
			AuthMethod:      AuthMethodTOFU,
			PublicKey:       publicKey,
			ProtocolVersion: "1.0",
		})

		// Get challenge
		resp1, _ := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "tofu-device2",
			AuthMethod:      AuthMethodTOFU,
			PublicKey:       publicKey,
			ProtocolVersion: "1.0",
		})

		// Wrong signature
		wrongSig := make([]byte, 64)

		resp2, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "tofu-device2",
			AuthMethod:      AuthMethodTOFU,
			PublicKey:       publicKey,
			Signature:       wrongSig,
			ProtocolVersion: "1.0",
		})

		_ = resp1 // silence unused warning
		if err != ErrSignatureInvalid {
			t.Errorf("expected ErrSignatureInvalid, got %v", err)
		}
		if resp2.Success {
			t.Error("expected failure")
		}
	})
}

func TestEdgeAuthService_SessionValidation(t *testing.T) {
	store := NewMemoryEdgeStore()
	service := NewEdgeAuthService(EdgeAuthConfig{
		Store:         store,
		SessionExpiry: time.Hour,
	})

	// Register and authenticate
	resp, _ := service.Authenticate(EdgeAuthRequest{
		EdgeID:          "session-test",
		EdgeName:        "Session Test",
		AuthMethod:      AuthMethodSharedSecret,
		SharedSecret:    "test-secret",
		ProtocolVersion: "1.0",
	})

	token := resp.Session.Token

	t.Run("valid session", func(t *testing.T) {
		session, err := service.ValidateSession(token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.EdgeID != "session-test" {
			t.Errorf("expected edge ID 'session-test', got %s", session.EdgeID)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := service.ValidateSession("invalid-token")
		if err != ErrSessionNotFound {
			t.Errorf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("invalidated session", func(t *testing.T) {
		service.InvalidateSession(token)

		_, err := service.ValidateSession(token)
		if err != ErrSessionNotFound {
			t.Errorf("expected ErrSessionNotFound after invalidation, got %v", err)
		}
	})
}

func TestEdgeAuthService_TrustLevel(t *testing.T) {
	store := NewMemoryEdgeStore()
	service := NewEdgeAuthService(EdgeAuthConfig{
		Store:         store,
		SessionExpiry: time.Hour,
	})

	// Register device
	_, _ = service.Authenticate(EdgeAuthRequest{
		EdgeID:          "trust-test",
		EdgeName:        "Trust Test",
		AuthMethod:      AuthMethodSharedSecret,
		SharedSecret:    "test-secret",
		ProtocolVersion: "1.0",
	})

	t.Run("set trust level", func(t *testing.T) {
		err := service.SetTrustLevel("trust-test", TrustTrusted)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		device, _ := store.GetEdge("trust-test")
		if device.TrustLevel != TrustTrusted {
			t.Errorf("expected TrustTrusted, got %s", device.TrustLevel)
		}
	})
}

func TestEdgeAuthService_Ban(t *testing.T) {
	store := NewMemoryEdgeStore()
	service := NewEdgeAuthService(EdgeAuthConfig{
		Store:         store,
		SessionExpiry: time.Hour,
	})

	// Register device
	resp, _ := service.Authenticate(EdgeAuthRequest{
		EdgeID:          "ban-test",
		EdgeName:        "Ban Test",
		AuthMethod:      AuthMethodSharedSecret,
		SharedSecret:    "test-secret",
		ProtocolVersion: "1.0",
	})
	token := resp.Session.Token

	t.Run("ban device", func(t *testing.T) {
		err := service.BanEdge("ban-test", "suspicious activity")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Session should be invalidated
		_, err = service.ValidateSession(token)
		if err != ErrSessionNotFound {
			t.Errorf("expected session to be invalidated, got %v", err)
		}

		// Further auth should fail
		resp, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "ban-test",
			EdgeName:        "Ban Test",
			AuthMethod:      AuthMethodSharedSecret,
			SharedSecret:    "test-secret",
			ProtocolVersion: "1.0",
		})

		if err != ErrEdgeBanned {
			t.Errorf("expected ErrEdgeBanned, got %v", err)
		}
		if resp.Success {
			t.Error("expected failure for banned device")
		}
	})

	t.Run("unban device", func(t *testing.T) {
		err := service.UnbanEdge("ban-test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Auth should work again
		resp, err := service.Authenticate(EdgeAuthRequest{
			EdgeID:          "ban-test",
			EdgeName:        "Ban Test",
			AuthMethod:      AuthMethodSharedSecret,
			SharedSecret:    "test-secret",
			ProtocolVersion: "1.0",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Success {
			t.Errorf("expected success after unban, got %s", resp.ErrorMessage)
		}
	})
}

func TestEdgeAuthService_RateLimiting(t *testing.T) {
	store := NewMemoryEdgeStore()
	service := NewEdgeAuthService(EdgeAuthConfig{
		Store:           store,
		SessionExpiry:   time.Hour,
		RateLimitMax:    3,
		RateLimitWindow: time.Minute,
	})

	// Register device
	_, _ = service.Authenticate(EdgeAuthRequest{
		EdgeID:          "rate-test",
		EdgeName:        "Rate Test",
		AuthMethod:      AuthMethodSharedSecret,
		SharedSecret:    "correct-secret",
		ProtocolVersion: "1.0",
	})

	// Make failed attempts
	for i := 0; i < 3; i++ {
		_, _ = service.Authenticate(EdgeAuthRequest{
			EdgeID:          "rate-test",
			AuthMethod:      AuthMethodSharedSecret,
			SharedSecret:    "wrong-secret",
			ProtocolVersion: "1.0",
		})
	}

	// Next attempt should be rate limited
	resp, err := service.Authenticate(EdgeAuthRequest{
		EdgeID:          "rate-test",
		AuthMethod:      AuthMethodSharedSecret,
		SharedSecret:    "correct-secret", // Even correct secret should fail
		ProtocolVersion: "1.0",
	})

	if err != ErrRateLimited {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	if resp.Success {
		t.Error("expected failure due to rate limiting")
	}
}

func TestMemoryEdgeStore(t *testing.T) {
	store := NewMemoryEdgeStore()

	t.Run("save and get", func(t *testing.T) {
		device := &EdgeDevice{
			ID:         "test1",
			Name:       "Test 1",
			TrustLevel: TrustTrusted,
		}

		err := store.SaveEdge(device)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := store.GetEdge("test1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "Test 1" {
			t.Errorf("expected 'Test 1', got %s", got.Name)
		}
	})

	t.Run("get non-existent", func(t *testing.T) {
		_, err := store.GetEdge("nonexistent")
		if err != ErrEdgeNotFound {
			t.Errorf("expected ErrEdgeNotFound, got %v", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		store.SaveEdge(&EdgeDevice{ID: "list1", Name: "List 1"})
		store.SaveEdge(&EdgeDevice{ID: "list2", Name: "List 2"})

		devices, err := store.ListEdges()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devices) < 2 {
			t.Errorf("expected at least 2 devices, got %d", len(devices))
		}
	})

	t.Run("delete", func(t *testing.T) {
		store.SaveEdge(&EdgeDevice{ID: "delete1", Name: "Delete 1"})

		err := store.DeleteEdge("delete1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = store.GetEdge("delete1")
		if err != ErrEdgeNotFound {
			t.Errorf("expected ErrEdgeNotFound after delete, got %v", err)
		}
	})
}
