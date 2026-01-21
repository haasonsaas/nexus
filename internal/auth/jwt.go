package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/haasonsaas/nexus/pkg/models"
)

// JWTService handles token signing and verification.
type JWTService struct {
	secret []byte
	expiry time.Duration
}

// NewJWTService builds a JWT helper with the given secret and expiry.
func NewJWTService(secret string, expiry time.Duration) *JWTService {
	return &JWTService{secret: []byte(secret), expiry: expiry}
}

type Claims struct {
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	jwt.RegisteredClaims
}

// Generate issues a signed token for the given user.
func (s *JWTService) Generate(user *models.User) (string, error) {
	if s == nil || len(s.secret) == 0 {
		return "", ErrAuthDisabled
	}
	if user == nil || strings.TrimSpace(user.ID) == "" {
		return "", errors.New("user id required")
	}

	claims := Claims{
		Email: strings.TrimSpace(user.Email),
		Name:  strings.TrimSpace(user.Name),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.expiry)),
		},
	}
	if s.expiry <= 0 {
		claims.ExpiresAt = nil
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// Validate parses and validates a JWT and returns the user embedded in it.
func (s *JWTService) Validate(token string) (*models.User, error) {
	if s == nil || len(s.secret) == 0 {
		return nil, ErrAuthDisabled
	}

	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return nil, ErrInvalidToken
	}
	return &models.User{
		ID:    claims.Subject,
		Email: strings.TrimSpace(claims.Email),
		Name:  strings.TrimSpace(claims.Name),
	}, nil
}
