package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

var (
	ErrAuthDisabled = errors.New("auth disabled")
	ErrInvalidToken = errors.New("invalid token")
	ErrInvalidKey   = errors.New("invalid api key")
)

// Config configures authentication helpers.
type Config struct {
	JWTSecret   string
	TokenExpiry time.Duration
	APIKeys     []APIKeyConfig
}

// APIKeyConfig declares a static API key and associated identity.
type APIKeyConfig struct {
	Key    string
	UserID string
	Email  string
	Name   string
}

// Service validates JWTs and API keys.
type Service struct {
	mu        sync.RWMutex
	jwt       *JWTService
	apiKeys   map[string]*models.User
	users     UserStore
	providers map[string]OAuthProvider
}

// NewService constructs an auth service from static configuration.
func NewService(cfg Config) *Service {
	service := &Service{}
	if strings.TrimSpace(cfg.JWTSecret) != "" {
		service.jwt = NewJWTService(cfg.JWTSecret, cfg.TokenExpiry)
	}
	service.apiKeys = buildAPIKeyMap(cfg.APIKeys)
	service.providers = map[string]OAuthProvider{}
	return service
}

// Enabled reports whether auth checks should run.
func (s *Service) Enabled() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jwt != nil || len(s.apiKeys) > 0
}

// GenerateJWT issues a signed token for the given user.
func (s *Service) GenerateJWT(user *models.User) (string, error) {
	if s == nil {
		return "", ErrAuthDisabled
	}
	s.mu.RLock()
	jwt := s.jwt
	s.mu.RUnlock()
	if jwt == nil {
		return "", ErrAuthDisabled
	}
	return jwt.Generate(user)
}

// ValidateJWT validates a JWT and returns the associated user.
func (s *Service) ValidateJWT(token string) (*models.User, error) {
	if s == nil {
		return nil, ErrAuthDisabled
	}
	s.mu.RLock()
	jwt := s.jwt
	s.mu.RUnlock()
	if jwt == nil {
		return nil, ErrAuthDisabled
	}
	return jwt.Validate(token)
}

// ValidateAPIKey validates an API key and returns the associated user.
// Uses constant-time comparison to prevent timing attacks.
func (s *Service) ValidateAPIKey(key string) (*models.User, error) {
	if s == nil {
		return nil, ErrAuthDisabled
	}
	s.mu.RLock()
	apiKeys := s.apiKeys
	s.mu.RUnlock()

	if len(apiKeys) == 0 {
		return nil, ErrAuthDisabled
	}
	inputKey := strings.TrimSpace(key)
	// Iterate through all keys using constant-time comparison
	// to prevent timing attacks that could reveal valid keys.
	var matchedUser *models.User
	for storedKey, user := range apiKeys {
		if subtle.ConstantTimeCompare([]byte(inputKey), []byte(storedKey)) == 1 {
			matchedUser = user
		}
	}
	if matchedUser == nil {
		return nil, ErrInvalidKey
	}
	return matchedUser, nil
}

func buildAPIKeyMap(keys []APIKeyConfig) map[string]*models.User {
	out := map[string]*models.User{}
	for _, entry := range keys {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		userID := strings.TrimSpace(entry.UserID)
		if userID == "" {
			sum := sha256.Sum256([]byte(key))
			userID = "api_" + hex.EncodeToString(sum[:8])
		}
		out[key] = &models.User{
			ID:    userID,
			Email: strings.TrimSpace(entry.Email),
			Name:  strings.TrimSpace(entry.Name),
		}
	}
	return out
}
