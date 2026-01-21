package auth

import "testing"

func TestServiceValidateAPIKey(t *testing.T) {
	service := NewService(Config{APIKeys: []APIKeyConfig{{Key: "abc123", UserID: "user-1", Email: "user@example.com"}}})
	user, err := service.ValidateAPIKey("abc123")
	if err != nil {
		t.Fatalf("ValidateAPIKey() error = %v", err)
	}
	if user.ID != "user-1" {
		t.Fatalf("expected user id, got %q", user.ID)
	}
	if user.Email != "user@example.com" {
		t.Fatalf("expected email, got %q", user.Email)
	}
}
