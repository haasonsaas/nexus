// Package auth provides authentication services including edge daemon authentication.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Edge authentication errors
var (
	ErrEdgeNotFound     = errors.New("edge device not found")
	ErrEdgeBanned       = errors.New("edge device is banned")
	ErrInvalidSecret    = errors.New("invalid shared secret")
	ErrInvalidPublicKey = errors.New("invalid public key")
	ErrSignatureInvalid = errors.New("signature verification failed")
	ErrTOFUPending      = errors.New("TOFU verification pending")
	ErrSessionExpired   = errors.New("edge session expired")
	ErrSessionNotFound  = errors.New("edge session not found")
	ErrRateLimited      = errors.New("edge rate limited")
	ErrProtocolMismatch = errors.New("protocol version mismatch")
)

// EdgeTrustLevel defines the trust level for an edge device.
type EdgeTrustLevel string

const (
	// TrustUntrusted means tools require explicit approval for each use.
	TrustUntrusted EdgeTrustLevel = "untrusted"

	// TrustTOFUPending means trust-on-first-use is awaiting verification.
	TrustTOFUPending EdgeTrustLevel = "tofu_pending"

	// TrustTOFU means trust-on-first-use; approved after first successful auth.
	TrustTOFU EdgeTrustLevel = "tofu"

	// TrustTrusted means tools are trusted and can be used without approval.
	TrustTrusted EdgeTrustLevel = "trusted"

	// TrustPrivileged means can access sensitive/privileged tools.
	TrustPrivileged EdgeTrustLevel = "privileged"
)

// EdgeAuthMethod defines how an edge authenticates.
type EdgeAuthMethod string

const (
	AuthMethodSharedSecret EdgeAuthMethod = "shared_secret"
	AuthMethodTOFU         EdgeAuthMethod = "tofu"
	AuthMethodCertificate  EdgeAuthMethod = "certificate"
)

// EdgeDevice represents a registered edge daemon.
type EdgeDevice struct {
	// ID is the unique identifier for this edge device.
	ID string `json:"id"`

	// Name is a human-readable name.
	Name string `json:"name"`

	// AuthMethod is how this device authenticates.
	AuthMethod EdgeAuthMethod `json:"auth_method"`

	// SharedSecretHash stores the hashed shared secret (for shared_secret auth).
	SharedSecretHash string `json:"shared_secret_hash,omitempty"`

	// PublicKey stores the ed25519 public key (for TOFU auth).
	PublicKey []byte `json:"public_key,omitempty"`

	// TrustLevel is the current trust level.
	TrustLevel EdgeTrustLevel `json:"trust_level"`

	// FirstSeenAt is when this device was first registered.
	FirstSeenAt time.Time `json:"first_seen_at"`

	// LastSeenAt is when this device last authenticated.
	LastSeenAt time.Time `json:"last_seen_at"`

	// Banned indicates if this device is banned.
	Banned bool `json:"banned"`

	// BanReason explains why the device was banned.
	BanReason string `json:"ban_reason,omitempty"`

	// Metadata stores additional device info.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// EdgeSession represents an authenticated edge session.
type EdgeSession struct {
	// Token is the session token.
	Token string `json:"token"`

	// EdgeID is the authenticated edge device ID.
	EdgeID string `json:"edge_id"`

	// CreatedAt is when the session was created.
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt is when the session expires.
	ExpiresAt time.Time `json:"expires_at"`

	// TrustLevel is the trust level for this session.
	TrustLevel EdgeTrustLevel `json:"trust_level"`
}

// EdgeAuthRequest is a request to authenticate an edge device.
type EdgeAuthRequest struct {
	EdgeID          string
	EdgeName        string
	AuthMethod      EdgeAuthMethod
	SharedSecret    string
	PublicKey       []byte
	Signature       []byte // For TOFU: signature of challenge
	Challenge       []byte // For TOFU: challenge bytes
	ProtocolVersion string
}

// EdgeAuthResponse is the response to an edge authentication request.
type EdgeAuthResponse struct {
	Success      bool
	Session      *EdgeSession
	TrustLevel   EdgeTrustLevel
	ErrorMessage string
	Challenge    []byte // For TOFU: challenge to sign
}

// EdgeStore persists edge device registrations.
type EdgeStore interface {
	GetEdge(id string) (*EdgeDevice, error)
	SaveEdge(device *EdgeDevice) error
	DeleteEdge(id string) error
	ListEdges() ([]*EdgeDevice, error)
}

// EdgeAuthService handles edge daemon authentication.
type EdgeAuthService struct {
	mu              sync.RWMutex
	store           EdgeStore
	sessions        map[string]*EdgeSession
	sessionExpiry   time.Duration
	minProtocolVer  string
	pendingTOFU     map[string][]byte // edgeID -> challenge
	rateLimits      map[string]int    // edgeID -> failure count
	rateLimitWindow time.Duration
	rateLimitMax    int
}

// EdgeAuthConfig configures the edge auth service.
type EdgeAuthConfig struct {
	Store           EdgeStore
	SessionExpiry   time.Duration
	MinProtocolVer  string
	RateLimitWindow time.Duration
	RateLimitMax    int
}

// NewEdgeAuthService creates a new edge auth service.
func NewEdgeAuthService(cfg EdgeAuthConfig) *EdgeAuthService {
	if cfg.SessionExpiry == 0 {
		cfg.SessionExpiry = 24 * time.Hour
	}
	if cfg.MinProtocolVer == "" {
		cfg.MinProtocolVer = "1.0"
	}
	if cfg.RateLimitWindow == 0 {
		cfg.RateLimitWindow = 15 * time.Minute
	}
	if cfg.RateLimitMax == 0 {
		cfg.RateLimitMax = 5
	}

	return &EdgeAuthService{
		store:           cfg.Store,
		sessions:        make(map[string]*EdgeSession),
		sessionExpiry:   cfg.SessionExpiry,
		minProtocolVer:  cfg.MinProtocolVer,
		pendingTOFU:     make(map[string][]byte),
		rateLimits:      make(map[string]int),
		rateLimitWindow: cfg.RateLimitWindow,
		rateLimitMax:    cfg.RateLimitMax,
	}
}

// Authenticate authenticates an edge device.
func (s *EdgeAuthService) Authenticate(req EdgeAuthRequest) (*EdgeAuthResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check rate limit
	if s.isRateLimited(req.EdgeID) {
		return &EdgeAuthResponse{
			Success:      false,
			ErrorMessage: "too many failed attempts",
		}, ErrRateLimited
	}

	// Look up existing device
	var device *EdgeDevice
	if s.store != nil {
		var err error
		device, err = s.store.GetEdge(req.EdgeID)
		if err != nil && !errors.Is(err, ErrEdgeNotFound) {
			return nil, fmt.Errorf("edge lookup: %w", err)
		}
	}

	// Check if banned
	if device != nil && device.Banned {
		return &EdgeAuthResponse{
			Success:      false,
			ErrorMessage: device.BanReason,
		}, ErrEdgeBanned
	}

	switch req.AuthMethod {
	case AuthMethodSharedSecret:
		return s.authenticateSharedSecret(req, device)
	case AuthMethodTOFU:
		return s.authenticateTOFU(req, device)
	default:
		s.recordFailure(req.EdgeID)
		return &EdgeAuthResponse{
			Success:      false,
			ErrorMessage: "unsupported auth method",
		}, errors.New("unsupported auth method")
	}
}

func (s *EdgeAuthService) authenticateSharedSecret(req EdgeAuthRequest, device *EdgeDevice) (*EdgeAuthResponse, error) {
	if device == nil {
		// New device - register with shared secret
		return s.registerNewDevice(req, AuthMethodSharedSecret)
	}

	if device.AuthMethod != AuthMethodSharedSecret {
		s.recordFailure(req.EdgeID)
		return &EdgeAuthResponse{
			Success:      false,
			ErrorMessage: "auth method mismatch",
		}, errors.New("auth method mismatch")
	}

	// Verify shared secret
	inputHash := hashSecret(req.SharedSecret)
	if subtle.ConstantTimeCompare([]byte(inputHash), []byte(device.SharedSecretHash)) != 1 {
		s.recordFailure(req.EdgeID)
		return &EdgeAuthResponse{
			Success:      false,
			ErrorMessage: "invalid shared secret",
		}, ErrInvalidSecret
	}

	// Success - create session
	session := s.createSession(device)
	s.clearFailures(req.EdgeID)

	// Update last seen
	device.LastSeenAt = time.Now()
	if s.store != nil {
		_ = s.store.SaveEdge(device)
	}

	return &EdgeAuthResponse{
		Success:    true,
		Session:    session,
		TrustLevel: device.TrustLevel,
	}, nil
}

func (s *EdgeAuthService) authenticateTOFU(req EdgeAuthRequest, device *EdgeDevice) (*EdgeAuthResponse, error) {
	// If no existing device, start TOFU process
	if device == nil {
		// First time seeing this device
		if len(req.PublicKey) != ed25519.PublicKeySize {
			s.recordFailure(req.EdgeID)
			return &EdgeAuthResponse{
				Success:      false,
				ErrorMessage: "invalid public key",
			}, ErrInvalidPublicKey
		}

		// Generate challenge for the device to sign
		challenge := make([]byte, 32)
		if _, err := rand.Read(challenge); err != nil {
			return nil, fmt.Errorf("generate challenge: %w", err)
		}

		// Store pending TOFU
		s.pendingTOFU[req.EdgeID] = challenge

		// Register device in TOFU pending state
		newDevice := &EdgeDevice{
			ID:          req.EdgeID,
			Name:        req.EdgeName,
			AuthMethod:  AuthMethodTOFU,
			PublicKey:   req.PublicKey,
			TrustLevel:  TrustTOFUPending,
			FirstSeenAt: time.Now(),
			LastSeenAt:  time.Now(),
			Metadata:    map[string]string{"protocol_version": req.ProtocolVersion},
		}

		if s.store != nil {
			if err := s.store.SaveEdge(newDevice); err != nil {
				return nil, fmt.Errorf("save edge: %w", err)
			}
		}

		return &EdgeAuthResponse{
			Success:      false,
			TrustLevel:   TrustTOFUPending,
			ErrorMessage: "TOFU: sign the challenge with your private key",
			Challenge:    challenge,
		}, ErrTOFUPending
	}

	// Existing device - verify signature
	if device.AuthMethod != AuthMethodTOFU {
		s.recordFailure(req.EdgeID)
		return &EdgeAuthResponse{
			Success:      false,
			ErrorMessage: "auth method mismatch",
		}, errors.New("auth method mismatch")
	}

	// Check for pending challenge
	challenge, hasPending := s.pendingTOFU[req.EdgeID]
	if !hasPending {
		// No pending challenge - generate new one
		challenge = make([]byte, 32)
		if _, err := rand.Read(challenge); err != nil {
			return nil, fmt.Errorf("generate challenge: %w", err)
		}
		s.pendingTOFU[req.EdgeID] = challenge

		return &EdgeAuthResponse{
			Success:      false,
			TrustLevel:   device.TrustLevel,
			ErrorMessage: "sign the challenge",
			Challenge:    challenge,
		}, ErrTOFUPending
	}

	// Verify signature
	if len(req.Signature) == 0 {
		return &EdgeAuthResponse{
			Success:      false,
			TrustLevel:   device.TrustLevel,
			ErrorMessage: "signature required",
			Challenge:    challenge,
		}, ErrSignatureInvalid
	}

	if !ed25519.Verify(device.PublicKey, challenge, req.Signature) {
		s.recordFailure(req.EdgeID)
		return &EdgeAuthResponse{
			Success:      false,
			ErrorMessage: "signature verification failed",
		}, ErrSignatureInvalid
	}

	// Clear pending challenge
	delete(s.pendingTOFU, req.EdgeID)

	// Upgrade trust level if still pending
	if device.TrustLevel == TrustTOFUPending {
		device.TrustLevel = TrustTOFU
	}

	// Create session
	session := s.createSession(device)
	s.clearFailures(req.EdgeID)

	// Update last seen
	device.LastSeenAt = time.Now()
	if s.store != nil {
		_ = s.store.SaveEdge(device)
	}

	return &EdgeAuthResponse{
		Success:    true,
		Session:    session,
		TrustLevel: device.TrustLevel,
	}, nil
}

func (s *EdgeAuthService) registerNewDevice(req EdgeAuthRequest, method EdgeAuthMethod) (*EdgeAuthResponse, error) {
	device := &EdgeDevice{
		ID:          req.EdgeID,
		Name:        req.EdgeName,
		AuthMethod:  method,
		TrustLevel:  TrustUntrusted, // Start untrusted
		FirstSeenAt: time.Now(),
		LastSeenAt:  time.Now(),
		Metadata:    map[string]string{"protocol_version": req.ProtocolVersion},
	}

	if method == AuthMethodSharedSecret {
		device.SharedSecretHash = hashSecret(req.SharedSecret)
	}

	if s.store != nil {
		if err := s.store.SaveEdge(device); err != nil {
			return nil, fmt.Errorf("save edge: %w", err)
		}
	}

	session := s.createSession(device)

	return &EdgeAuthResponse{
		Success:    true,
		Session:    session,
		TrustLevel: device.TrustLevel,
	}, nil
}

func (s *EdgeAuthService) createSession(device *EdgeDevice) *EdgeSession {
	token := generateSessionToken()
	session := &EdgeSession{
		Token:      token,
		EdgeID:     device.ID,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(s.sessionExpiry),
		TrustLevel: device.TrustLevel,
	}
	s.sessions[token] = session
	return session
}

// ValidateSession validates an edge session token.
func (s *EdgeAuthService) ValidateSession(token string) (*EdgeSession, error) {
	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil, ErrSessionExpired
	}

	return session, nil
}

// InvalidateSession invalidates an edge session.
func (s *EdgeAuthService) InvalidateSession(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// SetTrustLevel sets the trust level for an edge device.
func (s *EdgeAuthService) SetTrustLevel(edgeID string, level EdgeTrustLevel) error {
	if s.store == nil {
		return errors.New("no edge store configured")
	}

	device, err := s.store.GetEdge(edgeID)
	if err != nil {
		return err
	}

	device.TrustLevel = level
	return s.store.SaveEdge(device)
}

// BanEdge bans an edge device.
func (s *EdgeAuthService) BanEdge(edgeID, reason string) error {
	if s.store == nil {
		return errors.New("no edge store configured")
	}

	device, err := s.store.GetEdge(edgeID)
	if err != nil {
		return err
	}

	device.Banned = true
	device.BanReason = reason

	// Invalidate any active sessions
	s.mu.Lock()
	for token, session := range s.sessions {
		if session.EdgeID == edgeID {
			delete(s.sessions, token)
		}
	}
	s.mu.Unlock()

	return s.store.SaveEdge(device)
}

// UnbanEdge unbans an edge device.
func (s *EdgeAuthService) UnbanEdge(edgeID string) error {
	if s.store == nil {
		return errors.New("no edge store configured")
	}

	device, err := s.store.GetEdge(edgeID)
	if err != nil {
		return err
	}

	device.Banned = false
	device.BanReason = ""
	return s.store.SaveEdge(device)
}

// GetEdge returns an edge device by ID.
func (s *EdgeAuthService) GetEdge(edgeID string) (*EdgeDevice, error) {
	if s.store == nil {
		return nil, errors.New("no edge store configured")
	}
	return s.store.GetEdge(edgeID)
}

// ListEdges returns all registered edge devices.
func (s *EdgeAuthService) ListEdges() ([]*EdgeDevice, error) {
	if s.store == nil {
		return nil, errors.New("no edge store configured")
	}
	return s.store.ListEdges()
}

// Rate limiting helpers

func (s *EdgeAuthService) isRateLimited(edgeID string) bool {
	count := s.rateLimits[edgeID]
	return count >= s.rateLimitMax
}

func (s *EdgeAuthService) recordFailure(edgeID string) {
	s.rateLimits[edgeID]++

	// Schedule cleanup after window
	go func() {
		time.Sleep(s.rateLimitWindow)
		s.mu.Lock()
		if s.rateLimits[edgeID] > 0 {
			s.rateLimits[edgeID]--
		}
		s.mu.Unlock()
	}()
}

func (s *EdgeAuthService) clearFailures(edgeID string) {
	delete(s.rateLimits, edgeID)
}

// Cleanup removes expired sessions.
func (s *EdgeAuthService) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, token)
		}
	}
}

// Helper functions

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func generateSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based token
		return fmt.Sprintf("edge_%d", time.Now().UnixNano())
	}
	return "edge_" + base64.RawURLEncoding.EncodeToString(b)
}

// MemoryEdgeStore is an in-memory implementation of EdgeStore for testing.
type MemoryEdgeStore struct {
	mu      sync.RWMutex
	devices map[string]*EdgeDevice
}

// NewMemoryEdgeStore creates a new in-memory edge store.
func NewMemoryEdgeStore() *MemoryEdgeStore {
	return &MemoryEdgeStore{
		devices: make(map[string]*EdgeDevice),
	}
}

func (s *MemoryEdgeStore) GetEdge(id string) (*EdgeDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	device, ok := s.devices[id]
	if !ok {
		return nil, ErrEdgeNotFound
	}

	// Return a copy
	copy := *device
	return &copy, nil
}

func (s *MemoryEdgeStore) SaveEdge(device *EdgeDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy
	copy := *device
	s.devices[device.ID] = &copy
	return nil
}

func (s *MemoryEdgeStore) DeleteEdge(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.devices, id)
	return nil
}

func (s *MemoryEdgeStore) ListEdges() ([]*EdgeDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*EdgeDevice, 0, len(s.devices))
	for _, device := range s.devices {
		copy := *device
		result = append(result, &copy)
	}
	return result, nil
}
