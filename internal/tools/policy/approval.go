// Package policy provides tool authorization and access control.
// This file implements the approval workflow for edge tools.
package policy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	proto "github.com/haasonsaas/nexus/pkg/proto"
)

var (
	ErrApprovalRequired = errors.New("approval required")
	ErrApprovalDenied   = errors.New("approval denied")
	ErrApprovalExpired  = errors.New("approval expired")
	ErrApprovalPending  = errors.New("approval pending")
)

// ApprovalRequest represents a request for tool execution approval.
type ApprovalRequest struct {
	ID           string
	ToolName     string
	EdgeID       string
	Input        string // JSON-encoded input
	RiskLevel    proto.RiskLevel
	TrustLevel   TrustLevel
	SessionID    string
	UserID       string
	RequestedAt  time.Time
	ExpiresAt    time.Time
	Status       ApprovalStatus
	DecidedAt    *time.Time
	DecidedBy    string
	DenialReason string
}

// ApprovalStatus represents the current status of an approval request.
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusDenied   ApprovalStatus = "denied"
	ApprovalStatusExpired  ApprovalStatus = "expired"
)

// ApprovalPolicy defines when approval is required for tool execution.
type ApprovalPolicy struct {
	// RequireApprovalForUntrusted requires approval for all tools from untrusted edges.
	RequireApprovalForUntrusted bool

	// RequireApprovalForHighRisk requires approval for high/critical risk tools.
	RequireApprovalForHighRisk bool

	// AlwaysRequireApprovalFor lists tools that always require approval.
	AlwaysRequireApprovalFor []string

	// NeverRequireApprovalFor lists tools that never require approval (trusted).
	NeverRequireApprovalFor []string

	// ApprovalTimeout is how long approval requests remain valid.
	ApprovalTimeout time.Duration

	// AutoApproveForTrusted auto-approves low-risk tools from trusted edges.
	AutoApproveForTrusted bool

	// ByRiskLevel defines approval requirements by risk level.
	ByRiskLevel map[proto.RiskLevel]RiskApprovalPolicy
}

// RiskApprovalPolicy defines approval requirements for a specific risk level.
type RiskApprovalPolicy struct {
	// RequireApproval always requires approval regardless of trust.
	RequireApproval bool

	// MinTrustLevel is the minimum trust level to skip approval.
	MinTrustLevel TrustLevel

	// MaxAutoApprovePerSession limits auto-approvals per session.
	MaxAutoApprovePerSession int
}

// DefaultApprovalPolicy returns sensible default approval settings.
func DefaultApprovalPolicy() *ApprovalPolicy {
	return &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		RequireApprovalForHighRisk:  true,
		ApprovalTimeout:             5 * time.Minute,
		AutoApproveForTrusted:       true,
		ByRiskLevel: map[proto.RiskLevel]RiskApprovalPolicy{
			proto.RiskLevel_RISK_LEVEL_LOW: {
				RequireApproval: false,
				MinTrustLevel:   TrustUntrusted,
			},
			proto.RiskLevel_RISK_LEVEL_MEDIUM: {
				RequireApproval:          false,
				MinTrustLevel:            TrustTOFU,
				MaxAutoApprovePerSession: 10,
			},
			proto.RiskLevel_RISK_LEVEL_HIGH: {
				RequireApproval:          true,
				MinTrustLevel:            TrustTrusted,
				MaxAutoApprovePerSession: 3,
			},
			proto.RiskLevel_RISK_LEVEL_CRITICAL: {
				RequireApproval: true,
				MinTrustLevel:   TrustTrusted,
			},
		},
	}
}

// ApprovalManager manages approval workflows for edge tool executions.
type ApprovalManager struct {
	mu       sync.RWMutex
	policy   *ApprovalPolicy
	requests map[string]*ApprovalRequest
	registry *ToolRegistry

	// Callbacks for approval workflow
	onApprovalRequired func(*ApprovalRequest)
	onApprovalDecided  func(*ApprovalRequest)

	// Session tracking for rate limiting
	sessionApprovals map[string]map[proto.RiskLevel]int // sessionID -> riskLevel -> count
}

// NewApprovalManager creates a new approval manager.
func NewApprovalManager(registry *ToolRegistry, policy *ApprovalPolicy) *ApprovalManager {
	if policy == nil {
		policy = DefaultApprovalPolicy()
	}
	return &ApprovalManager{
		policy:           policy,
		requests:         make(map[string]*ApprovalRequest),
		registry:         registry,
		sessionApprovals: make(map[string]map[proto.RiskLevel]int),
	}
}

// SetApprovalRequiredHandler sets the callback for when approval is required.
func (m *ApprovalManager) SetApprovalRequiredHandler(fn func(*ApprovalRequest)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onApprovalRequired = fn
}

// SetApprovalDecidedHandler sets the callback for when approval is decided.
func (m *ApprovalManager) SetApprovalDecidedHandler(fn func(*ApprovalRequest)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onApprovalDecided = fn
}

// CheckApproval determines if tool execution requires approval and handles the workflow.
// Returns nil if execution can proceed, or an error indicating the approval status.
func (m *ApprovalManager) CheckApproval(ctx context.Context, toolName, edgeID, input, sessionID, userID string, riskLevel proto.RiskLevel) error {
	if !IsEdgeTool(toolName) && edgeID == "" {
		// Not an edge tool, no approval needed via this system
		return nil
	}

	// Get trust level for the edge
	trustLevel := TrustUntrusted
	if m.registry != nil {
		trustLevel = m.registry.GetEdgeTrustLevel(edgeID)
	}

	// Check if approval is needed
	needsApproval := m.needsApproval(toolName, edgeID, riskLevel, trustLevel, sessionID)
	if !needsApproval {
		// Track auto-approval for rate limiting
		m.trackAutoApproval(sessionID, riskLevel)
		return nil
	}

	// Create approval request
	req := &ApprovalRequest{
		ID:          generateApprovalID(),
		ToolName:    toolName,
		EdgeID:      edgeID,
		Input:       input,
		RiskLevel:   riskLevel,
		TrustLevel:  trustLevel,
		SessionID:   sessionID,
		UserID:      userID,
		RequestedAt: time.Now(),
		ExpiresAt:   time.Now().Add(m.policy.ApprovalTimeout),
		Status:      ApprovalStatusPending,
	}

	m.mu.Lock()
	m.requests[req.ID] = req
	callback := m.onApprovalRequired
	m.mu.Unlock()

	// Notify that approval is required
	if callback != nil {
		callback(req)
	}

	return fmt.Errorf("%w: request_id=%s", ErrApprovalRequired, req.ID)
}

// GetRequest returns an approval request by ID.
func (m *ApprovalManager) GetRequest(id string) (*ApprovalRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	req, ok := m.requests[id]
	if !ok {
		return nil, fmt.Errorf("approval request not found: %s", id)
	}

	// Check expiration
	if req.Status == ApprovalStatusPending && time.Now().After(req.ExpiresAt) {
		req.Status = ApprovalStatusExpired
	}

	return req, nil
}

// Approve approves an approval request.
func (m *ApprovalManager) Approve(id, approverID string) error {
	m.mu.Lock()
	req, ok := m.requests[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("approval request not found: %s", id)
	}

	if req.Status != ApprovalStatusPending {
		m.mu.Unlock()
		return fmt.Errorf("request already decided: %s", req.Status)
	}

	if time.Now().After(req.ExpiresAt) {
		req.Status = ApprovalStatusExpired
		m.mu.Unlock()
		return ErrApprovalExpired
	}

	now := time.Now()
	req.Status = ApprovalStatusApproved
	req.DecidedAt = &now
	req.DecidedBy = approverID
	callback := m.onApprovalDecided
	m.mu.Unlock()

	// Track approval for rate limiting
	m.trackAutoApproval(req.SessionID, req.RiskLevel)

	// Notify
	if callback != nil {
		callback(req)
	}

	return nil
}

// Deny denies an approval request.
func (m *ApprovalManager) Deny(id, denierID, reason string) error {
	m.mu.Lock()
	req, ok := m.requests[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("approval request not found: %s", id)
	}

	if req.Status != ApprovalStatusPending {
		m.mu.Unlock()
		return fmt.Errorf("request already decided: %s", req.Status)
	}

	now := time.Now()
	req.Status = ApprovalStatusDenied
	req.DecidedAt = &now
	req.DecidedBy = denierID
	req.DenialReason = reason
	callback := m.onApprovalDecided
	m.mu.Unlock()

	// Notify
	if callback != nil {
		callback(req)
	}

	return nil
}

// WaitForApproval waits for an approval decision with context cancellation support.
func (m *ApprovalManager) WaitForApproval(ctx context.Context, requestID string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			req, err := m.GetRequest(requestID)
			if err != nil {
				return err
			}

			switch req.Status {
			case ApprovalStatusApproved:
				return nil
			case ApprovalStatusDenied:
				if req.DenialReason != "" {
					return fmt.Errorf("%w: %s", ErrApprovalDenied, req.DenialReason)
				}
				return ErrApprovalDenied
			case ApprovalStatusExpired:
				return ErrApprovalExpired
			case ApprovalStatusPending:
				continue
			}
		}
	}
}

// ListPending returns all pending approval requests.
func (m *ApprovalManager) ListPending() []*ApprovalRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []*ApprovalRequest
	now := time.Now()
	for _, req := range m.requests {
		if req.Status == ApprovalStatusPending {
			if now.After(req.ExpiresAt) {
				req.Status = ApprovalStatusExpired
			} else {
				pending = append(pending, req)
			}
		}
	}
	return pending
}

// ListBySession returns all approval requests for a session.
func (m *ApprovalManager) ListBySession(sessionID string) []*ApprovalRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*ApprovalRequest
	for _, req := range m.requests {
		if req.SessionID == sessionID {
			results = append(results, req)
		}
	}
	return results
}

// CleanupExpired removes expired approval requests.
func (m *ApprovalManager) CleanupExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	now := time.Now()
	for id, req := range m.requests {
		if req.Status == ApprovalStatusPending && now.After(req.ExpiresAt) {
			req.Status = ApprovalStatusExpired
		}
		// Remove old decided/expired requests
		if req.Status != ApprovalStatusPending && time.Since(req.ExpiresAt) > time.Hour {
			delete(m.requests, id)
			count++
		}
	}
	return count
}

func (m *ApprovalManager) needsApproval(toolName, edgeID string, riskLevel proto.RiskLevel, trustLevel TrustLevel, sessionID string) bool {
	// Check explicit always/never lists
	for _, t := range m.policy.AlwaysRequireApprovalFor {
		if t == toolName || matchToolPattern(t, toolName) {
			return true
		}
	}
	for _, t := range m.policy.NeverRequireApprovalFor {
		if t == toolName || matchToolPattern(t, toolName) {
			return false
		}
	}

	// Check risk-specific policy
	if riskPolicy, ok := m.policy.ByRiskLevel[riskLevel]; ok {
		// Always require approval for this risk level
		if riskPolicy.RequireApproval {
			// Unless trust level is high enough
			if trustMeetsMinimum(trustLevel, riskPolicy.MinTrustLevel) {
				// Check rate limit
				if riskPolicy.MaxAutoApprovePerSession > 0 {
					count := m.getSessionApprovalCount(sessionID, riskLevel)
					if count >= riskPolicy.MaxAutoApprovePerSession {
						return true
					}
				}
				return false
			}
			return true
		}

		// Check if trust level is sufficient
		if !trustMeetsMinimum(trustLevel, riskPolicy.MinTrustLevel) {
			return true
		}

		// Check rate limit
		if riskPolicy.MaxAutoApprovePerSession > 0 {
			count := m.getSessionApprovalCount(sessionID, riskLevel)
			if count >= riskPolicy.MaxAutoApprovePerSession {
				return true
			}
		}

		return false
	}

	// Fallback to general policies
	if m.policy.RequireApprovalForUntrusted && trustLevel == TrustUntrusted {
		return true
	}

	if m.policy.RequireApprovalForHighRisk &&
		(riskLevel == proto.RiskLevel_RISK_LEVEL_HIGH || riskLevel == proto.RiskLevel_RISK_LEVEL_CRITICAL) {
		return true
	}

	return false
}

func (m *ApprovalManager) trackAutoApproval(sessionID string, riskLevel proto.RiskLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessionApprovals[sessionID] == nil {
		m.sessionApprovals[sessionID] = make(map[proto.RiskLevel]int)
	}
	m.sessionApprovals[sessionID][riskLevel]++
}

func (m *ApprovalManager) getSessionApprovalCount(sessionID string, riskLevel proto.RiskLevel) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.sessionApprovals[sessionID] == nil {
		return 0
	}
	return m.sessionApprovals[sessionID][riskLevel]
}

// ResetSessionApprovals resets the approval count for a session.
func (m *ApprovalManager) ResetSessionApprovals(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessionApprovals, sessionID)
}

// trustMeetsMinimum checks if actual trust meets minimum required.
func trustMeetsMinimum(actual, minimum TrustLevel) bool {
	trustOrder := map[TrustLevel]int{
		TrustUntrusted: 0,
		TrustTOFU:      1,
		TrustTrusted:   2,
	}
	return trustOrder[actual] >= trustOrder[minimum]
}

var approvalIDCounter int64
var approvalIDMu sync.Mutex

func generateApprovalID() string {
	approvalIDMu.Lock()
	defer approvalIDMu.Unlock()
	approvalIDCounter++
	return fmt.Sprintf("apr_%d_%d", time.Now().UnixNano(), approvalIDCounter)
}
