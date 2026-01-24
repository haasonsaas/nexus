package canvas

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrTokenInvalid = errors.New("canvas: token invalid")
	ErrTokenExpired = errors.New("canvas: token expired")
	ErrUnauthorized = errors.New("canvas: unauthorized")
)

// AccessToken describes a signed canvas access token.
type AccessToken struct {
	SessionID string `json:"sid"`
	UserID    string `json:"uid,omitempty"`
	Role      string `json:"role,omitempty"`
	ExpiresAt int64  `json:"exp"`
}

// SignAccessToken encodes and signs a token using the given secret.
func SignAccessToken(secret []byte, token AccessToken) (string, error) {
	if len(secret) == 0 {
		return "", ErrTokenInvalid
	}
	payload, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	sig := hmacSignature(secret, payloadEnc)
	sigEnc := base64.RawURLEncoding.EncodeToString(sig)
	return payloadEnc + "." + sigEnc, nil
}

// ParseAccessToken verifies a token and returns the decoded payload.
func ParseAccessToken(secret []byte, raw string) (*AccessToken, error) {
	if len(secret) == 0 {
		return nil, ErrTokenInvalid
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, ErrTokenInvalid
	}
	payloadEnc := parts[0]
	sigEnc := parts[1]

	signature, err := base64.RawURLEncoding.DecodeString(sigEnc)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	if !hmac.Equal(signature, hmacSignature(secret, payloadEnc)) {
		return nil, ErrTokenInvalid
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(payloadEnc)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	var token AccessToken
	if err := json.Unmarshal(payloadRaw, &token); err != nil {
		return nil, ErrTokenInvalid
	}
	if token.ExpiresAt > 0 && time.Now().After(time.Unix(token.ExpiresAt, 0)) {
		return nil, ErrTokenExpired
	}
	if strings.TrimSpace(token.SessionID) == "" {
		return nil, ErrTokenInvalid
	}
	return &token, nil
}

func hmacSignature(secret []byte, payload string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}
