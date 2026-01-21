package auth

import (
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestJWTServiceGenerateValidate(t *testing.T) {
	service := NewJWTService("secret", time.Hour)
	token, err := service.Generate(&models.User{ID: "user-1", Email: "user@example.com", Name: "User"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	user, err := service.Validate(token)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if user.ID != "user-1" {
		t.Fatalf("expected user id, got %q", user.ID)
	}
	if user.Email != "user@example.com" {
		t.Fatalf("expected email, got %q", user.Email)
	}
	if user.Name != "User" {
		t.Fatalf("expected name, got %q", user.Name)
	}
}
