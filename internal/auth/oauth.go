package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"

	"github.com/haasonsaas/nexus/pkg/models"
)

var (
	ErrUnknownProvider  = errors.New("unknown oauth provider")
	ErrUserStoreMissing = errors.New("user store not configured")
)

// UserInfo represents user identity data returned by OAuth providers.
type UserInfo struct {
	ID        string
	Provider  string
	Email     string
	Name      string
	AvatarURL string
}

// OAuthProvider implements the OAuth flow for a provider.
type OAuthProvider interface {
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	UserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error)
}

// UserStore resolves and persists users for OAuth flows.
type UserStore interface {
	FindOrCreate(ctx context.Context, info *UserInfo) (*models.User, error)
}

// AuthResult contains the authenticated user and JWT token.
type AuthResult struct {
	User  *models.User
	Token string
}

// RegisterProvider adds an OAuth provider to the auth service.
func (s *Service) RegisterProvider(name string, provider OAuthProvider) {
	if s == nil || provider == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.providers == nil {
		s.providers = map[string]OAuthProvider{}
	}
	s.providers[strings.ToLower(strings.TrimSpace(name))] = provider
}

// SetUserStore sets the backing user store used for OAuth flows.
func (s *Service) SetUserStore(store UserStore) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = store
}

// OAuth parameter limits to prevent abuse
const (
	maxProviderLength = 64
	maxCodeLength     = 4096
)

// HandleCallback completes an OAuth flow and returns an auth result.
func (s *Service) HandleCallback(ctx context.Context, provider, code string) (*AuthResult, error) {
	if s == nil {
		return nil, ErrAuthDisabled
	}

	// Validate input lengths
	if len(provider) > maxProviderLength {
		return nil, errors.New("provider name too long")
	}
	if len(code) > maxCodeLength {
		return nil, errors.New("authorization code too long")
	}

	// Read providers and users under lock
	s.mu.RLock()
	users := s.users
	jwt := s.jwt
	p := s.providers[strings.ToLower(strings.TrimSpace(provider))]
	s.mu.RUnlock()

	if users == nil {
		return nil, ErrUserStoreMissing
	}
	if jwt == nil {
		return nil, ErrAuthDisabled
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("authorization code required")
	}
	if p == nil {
		return nil, ErrUnknownProvider
	}

	// Exchange code for token
	token, err := p.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	// Get user info
	info, err := p.UserInfo(ctx, token)
	if err != nil {
		return nil, err
	}

	// Find or create user
	user, err := users.FindOrCreate(ctx, info)
	if err != nil {
		return nil, err
	}

	jwtToken, err := jwt.Generate(user)
	if err != nil {
		return nil, err
	}

	return &AuthResult{User: user, Token: jwtToken}, nil
}

// OAuthProviderConfig configures a generic OAuth provider.
type OAuthProviderConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
}

// GenericOAuthProvider implements OAuthProvider with configurable endpoints.
type GenericOAuthProvider struct {
	config      oauth2.Config
	userInfoURL string
	parser      func([]byte) (*UserInfo, error)
}

// NewGenericOAuthProvider creates a provider with the given config and parser.
func NewGenericOAuthProvider(cfg OAuthProviderConfig, parser func([]byte) (*UserInfo, error)) *GenericOAuthProvider {
	provider := &GenericOAuthProvider{
		config: oauth2.Config{
			ClientID:     strings.TrimSpace(cfg.ClientID),
			ClientSecret: strings.TrimSpace(cfg.ClientSecret),
			RedirectURL:  strings.TrimSpace(cfg.RedirectURL),
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  strings.TrimSpace(cfg.AuthURL),
				TokenURL: strings.TrimSpace(cfg.TokenURL),
			},
		},
		userInfoURL: strings.TrimSpace(cfg.UserInfoURL),
		parser:      parser,
	}
	return provider
}

// AuthURL returns the provider auth URL with the given state.
func (p *GenericOAuthProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange exchanges an auth code for a token.
func (p *GenericOAuthProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

// UserInfo fetches user info for the access token.
func (p *GenericOAuthProvider) UserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	if p.userInfoURL == "" {
		return nil, errors.New("user info url not configured")
	}
	client := p.config.Client(ctx, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("user info request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user info request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
		if readErr != nil {
			return nil, fmt.Errorf("user info request failed with status %d and unreadable body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("user info request failed: %s", strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if p.parser == nil {
		return nil, errors.New("user info parser not configured")
	}
	return p.parser(data)
}

// NewGoogleProvider builds a provider with Google endpoints.
func NewGoogleProvider(cfg OAuthProviderConfig) *GenericOAuthProvider {
	return NewGenericOAuthProvider(OAuthProviderConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://www.googleapis.com/oauth2/v3/userinfo",
		Scopes:       []string{"openid", "email", "profile"},
	}, parseGoogleUser)
}

// NewGitHubProvider builds a provider with GitHub endpoints.
func NewGitHubProvider(cfg OAuthProviderConfig) *GenericOAuthProvider {
	return NewGenericOAuthProvider(OAuthProviderConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		AuthURL:      "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		UserInfoURL:  "https://api.github.com/user",
		Scopes:       []string{"user:email"},
	}, parseGitHubUser)
}

func parseGoogleUser(data []byte) (*UserInfo, error) {
	var payload struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return &UserInfo{
		ID:        payload.Sub,
		Provider:  "google",
		Email:     payload.Email,
		Name:      payload.Name,
		AvatarURL: payload.Picture,
	}, nil
}

func parseGitHubUser(data []byte) (*UserInfo, error) {
	var payload struct {
		ID        any    `json:"id"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%v", payload.ID)
	name := payload.Name
	if strings.TrimSpace(name) == "" {
		name = payload.Login
	}
	return &UserInfo{
		ID:        id,
		Provider:  "github",
		Email:     payload.Email,
		Name:      name,
		AvatarURL: payload.AvatarURL,
	}, nil
}
