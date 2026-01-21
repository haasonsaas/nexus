package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/haasonsaas/nexus/pkg/models"
)

type stubProvider struct {
	user *UserInfo
}

func (p *stubProvider) AuthURL(state string) string { return "https://example.com/auth?state=" + state }
func (p *stubProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "token"}, nil
}
func (p *stubProvider) UserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	return p.user, nil
}

type stubUserStore struct{}

func (s stubUserStore) FindOrCreate(ctx context.Context, info *UserInfo) (*models.User, error) {
	return &models.User{ID: info.ID, Email: info.Email, Name: info.Name}, nil
}

func TestHandleCallback(t *testing.T) {
	service := NewService(Config{JWTSecret: "secret", TokenExpiry: time.Hour})
	service.RegisterProvider("google", &stubProvider{user: &UserInfo{ID: "u1", Email: "user@example.com", Name: "User"}})
	service.SetUserStore(stubUserStore{})

	result, err := service.HandleCallback(context.Background(), "google", "code")
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if result.User == nil || result.User.ID != "u1" {
		t.Fatalf("expected user id u1")
	}
	if result.Token == "" {
		t.Fatalf("expected jwt token")
	}
}

func TestGenericOAuthProviderUserInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":     "123",
			"email":   "user@example.com",
			"name":    "User",
			"picture": "https://example.com/avatar.png",
		})
	}))
	defer server.Close()

	provider := NewGenericOAuthProvider(OAuthProviderConfig{
		ClientID:     "id",
		ClientSecret: "secret",
		RedirectURL:  "http://localhost/callback",
		AuthURL:      server.URL + "/auth",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL,
		Scopes:       []string{"email"},
	}, parseGoogleUser)

	info, err := provider.UserInfo(context.Background(), &oauth2.Token{AccessToken: "token"})
	if err != nil {
		t.Fatalf("UserInfo() error = %v", err)
	}
	if info.ID != "123" {
		t.Fatalf("expected id 123, got %q", info.ID)
	}
}
