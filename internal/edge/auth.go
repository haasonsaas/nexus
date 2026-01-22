package edge

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// Common authentication errors.
var (
	ErrInvalidToken     = errors.New("invalid authentication token")
	ErrEdgeNotAllowed   = errors.New("edge not in allowed list")
	ErrEdgeIDConflict   = errors.New("edge ID already in use")
	ErrTOFUPending      = errors.New("TOFU approval pending")
	ErrTOFURejected     = errors.New("TOFU request rejected")
)

// TokenAuthenticator validates edges using pre-shared tokens.
type TokenAuthenticator struct {
	mu sync.RWMutex

	// tokens maps edge_id to expected token
	tokens map[string]string

	// allowAny allows any token if true (development mode)
	allowAny bool
}

// NewTokenAuthenticator creates a token-based authenticator.
func NewTokenAuthenticator(tokens map[string]string) *TokenAuthenticator {
	t := make(map[string]string, len(tokens))
	for k, v := range tokens {
		t[k] = v
	}
	return &TokenAuthenticator{
		tokens: t,
	}
}

// NewDevAuthenticator creates an authenticator that accepts any token.
// Only use in development.
func NewDevAuthenticator() *TokenAuthenticator {
	return &TokenAuthenticator{
		tokens:   make(map[string]string),
		allowAny: true,
	}
}

// Authenticate validates an edge registration.
func (a *TokenAuthenticator) Authenticate(ctx context.Context, reg *pb.EdgeRegister) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.allowAny {
		return reg.EdgeId, nil
	}

	expectedToken, ok := a.tokens[reg.EdgeId]
	if !ok {
		return "", ErrEdgeNotAllowed
	}

	if subtle.ConstantTimeCompare([]byte(reg.AuthToken), []byte(expectedToken)) != 1 {
		return "", ErrInvalidToken
	}

	return reg.EdgeId, nil
}

// AddEdge adds an edge to the allowed list.
func (a *TokenAuthenticator) AddEdge(edgeID, token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokens[edgeID] = token
}

// RemoveEdge removes an edge from the allowed list.
func (a *TokenAuthenticator) RemoveEdge(edgeID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tokens, edgeID)
}

// TOFUAuthenticator implements Trust-On-First-Use authentication.
// New edges are held pending until manually approved.
type TOFUAuthenticator struct {
	mu sync.RWMutex

	// approved maps edge_id to approval info
	approved map[string]*ApprovalInfo

	// pending maps edge_id to pending request
	pending map[string]*PendingApproval

	// onPending is called when a new edge needs approval
	onPending func(edgeID, name string)
}

// ApprovalInfo stores information about an approved edge.
type ApprovalInfo struct {
	EdgeID     string
	Name       string
	Token      string
	ApprovedAt time.Time
	ApprovedBy string
}

// PendingApproval tracks a pending TOFU request.
type PendingApproval struct {
	EdgeID    string
	Name      string
	Token     string
	RequestAt time.Time
	Approved  chan struct{}
	Rejected  chan struct{}
}

// NewTOFUAuthenticator creates a TOFU authenticator.
func NewTOFUAuthenticator(onPending func(edgeID, name string)) *TOFUAuthenticator {
	return &TOFUAuthenticator{
		approved:  make(map[string]*ApprovalInfo),
		pending:   make(map[string]*PendingApproval),
		onPending: onPending,
	}
}

// Authenticate validates or starts a TOFU flow.
func (a *TOFUAuthenticator) Authenticate(ctx context.Context, reg *pb.EdgeRegister) (string, error) {
	a.mu.Lock()

	// Check if already approved
	if info, ok := a.approved[reg.EdgeId]; ok {
		a.mu.Unlock()
		// Validate token matches
		if subtle.ConstantTimeCompare([]byte(reg.AuthToken), []byte(info.Token)) != 1 {
			return "", ErrInvalidToken
		}
		return reg.EdgeId, nil
	}

	// Check if already pending
	if pending, ok := a.pending[reg.EdgeId]; ok {
		a.mu.Unlock()
		// Wait for approval
		select {
		case <-pending.Approved:
			return reg.EdgeId, nil
		case <-pending.Rejected:
			return "", ErrTOFURejected
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// Create pending request
	pending := &PendingApproval{
		EdgeID:    reg.EdgeId,
		Name:      reg.Name,
		Token:     reg.AuthToken,
		RequestAt: time.Now(),
		Approved:  make(chan struct{}),
		Rejected:  make(chan struct{}),
	}
	a.pending[reg.EdgeId] = pending
	a.mu.Unlock()

	// Notify about pending approval
	if a.onPending != nil {
		a.onPending(reg.EdgeId, reg.Name)
	}

	// Wait for approval
	select {
	case <-pending.Approved:
		return reg.EdgeId, nil
	case <-pending.Rejected:
		return "", ErrTOFURejected
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Approve approves a pending edge.
func (a *TOFUAuthenticator) Approve(edgeID, approvedBy string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	pending, ok := a.pending[edgeID]
	if !ok {
		return fmt.Errorf("no pending request for edge: %s", edgeID)
	}

	// Move to approved
	a.approved[edgeID] = &ApprovalInfo{
		EdgeID:     edgeID,
		Name:       pending.Name,
		Token:      pending.Token,
		ApprovedAt: time.Now(),
		ApprovedBy: approvedBy,
	}
	delete(a.pending, edgeID)

	// Signal approval
	close(pending.Approved)
	return nil
}

// Reject rejects a pending edge.
func (a *TOFUAuthenticator) Reject(edgeID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	pending, ok := a.pending[edgeID]
	if !ok {
		return fmt.Errorf("no pending request for edge: %s", edgeID)
	}

	delete(a.pending, edgeID)
	close(pending.Rejected)
	return nil
}

// Revoke revokes an approved edge.
func (a *TOFUAuthenticator) Revoke(edgeID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.approved, edgeID)
}

// ListApproved returns all approved edges.
func (a *TOFUAuthenticator) ListApproved() []*ApprovalInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]*ApprovalInfo, 0, len(a.approved))
	for _, info := range a.approved {
		result = append(result, info)
	}
	return result
}

// ListPending returns all pending edges.
func (a *TOFUAuthenticator) ListPending() []*PendingApproval {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]*PendingApproval, 0, len(a.pending))
	for _, p := range a.pending {
		result = append(result, p)
	}
	return result
}

// CompositeAuthenticator tries multiple authenticators in order.
type CompositeAuthenticator struct {
	auths []Authenticator
}

// NewCompositeAuthenticator creates a composite authenticator.
func NewCompositeAuthenticator(auths ...Authenticator) *CompositeAuthenticator {
	return &CompositeAuthenticator{auths: auths}
}

// Authenticate tries each authenticator until one succeeds.
func (a *CompositeAuthenticator) Authenticate(ctx context.Context, reg *pb.EdgeRegister) (string, error) {
	var lastErr error
	for _, auth := range a.auths {
		edgeID, err := auth.Authenticate(ctx, reg)
		if err == nil {
			return edgeID, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return "", errors.New("no authenticators configured")
	}
	return "", lastErr
}
