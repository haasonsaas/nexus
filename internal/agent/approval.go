package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ApprovalDecision represents the result of an approval check for a tool call.
type ApprovalDecision string

const (
	// ApprovalAllowed means the tool call is allowed to execute.
	ApprovalAllowed ApprovalDecision = "allowed"
	// ApprovalDenied means the tool call is denied.
	ApprovalDenied ApprovalDecision = "denied"
	// ApprovalPending means the tool call requires user approval.
	ApprovalPending ApprovalDecision = "pending"
)

// ApprovalRequest represents a pending approval request for a tool call that requires user authorization.
type ApprovalRequest struct {
	ID         string           `json:"id"`
	ToolCallID string           `json:"tool_call_id"`
	ToolName   string           `json:"tool_name"`
	Input      []byte           `json:"input,omitempty"`
	AgentID    string           `json:"agent_id,omitempty"`
	SessionID  string           `json:"session_id,omitempty"`
	Reason     string           `json:"reason,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	ExpiresAt  time.Time        `json:"expires_at,omitempty"`
	Decision   ApprovalDecision `json:"decision"`
	DecidedAt  time.Time        `json:"decided_at,omitempty"`
	DecidedBy  string           `json:"decided_by,omitempty"`
}

// ApprovalPolicy configures approval behavior for tool execution including
// allow/deny lists and default decisions.
type ApprovalPolicy struct {
	// Allowlist contains tools that are always allowed (no approval needed).
	// Supports patterns like "mcp:*", "read_*", etc.
	Allowlist []string `yaml:"allowlist" json:"allowlist"`

	// Denylist contains tools that are always denied.
	Denylist []string `yaml:"denylist" json:"denylist"`

	// RequireApproval contains tools that always require approval.
	RequireApproval []string `yaml:"require_approval" json:"require_approval"`

	// SafeBins are tools that only read stdin and are safe to auto-allow.
	SafeBins []string `yaml:"safe_bins" json:"safe_bins"`

	// SkillAllowlist auto-allows tools defined by enabled skills.
	SkillAllowlist bool `yaml:"skill_allowlist" json:"skill_allowlist"`

	// AskFallback queues approval when UI is unavailable instead of denying.
	AskFallback bool `yaml:"ask_fallback" json:"ask_fallback"`

	// DefaultDecision when no rule matches (default: "pending").
	DefaultDecision ApprovalDecision `yaml:"default_decision" json:"default_decision"`

	// RequestTTL is how long approval requests remain valid (default: 5m).
	RequestTTL time.Duration `yaml:"request_ttl" json:"request_ttl"`
}

// DefaultApprovalPolicy returns sensible defaults with common safe binaries allowed.
func DefaultApprovalPolicy() *ApprovalPolicy {
	return &ApprovalPolicy{
		Allowlist:       []string{},
		Denylist:        []string{},
		RequireApproval: []string{},
		SafeBins:        []string{"cat", "head", "tail", "wc", "sort", "uniq", "grep"},
		SkillAllowlist:  true,
		AskFallback:     true,
		DefaultDecision: ApprovalPending,
		RequestTTL:      5 * time.Minute,
	}
}

// ApprovalChecker evaluates tool calls against approval policies to determine
// if they should be allowed, denied, or require user approval.
type ApprovalChecker struct {
	mu            sync.RWMutex
	agentPolicies map[string]*ApprovalPolicy // per-agent policies
	defaultPolicy *ApprovalPolicy
	skillTools    map[string]struct{} // tools provided by skills
	pendingStore  ApprovalStore
	uiAvailable   func() bool // callback to check if UI can handle approvals
}

// ApprovalStore persists pending approval requests for tools requiring user authorization.
type ApprovalStore interface {
	Create(ctx context.Context, req *ApprovalRequest) error
	Get(ctx context.Context, id string) (*ApprovalRequest, error)
	Update(ctx context.Context, req *ApprovalRequest) error
	ListPending(ctx context.Context, agentID string) ([]*ApprovalRequest, error)
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
}

// NewApprovalChecker creates a new approval checker with the given default policy.
// If defaultPolicy is nil, DefaultApprovalPolicy is used.
func NewApprovalChecker(defaultPolicy *ApprovalPolicy) *ApprovalChecker {
	defaultPolicy = normalizeApprovalPolicy(defaultPolicy)
	return &ApprovalChecker{
		agentPolicies: make(map[string]*ApprovalPolicy),
		defaultPolicy: defaultPolicy,
		skillTools:    make(map[string]struct{}),
	}
}

// SetStore sets the approval request store for persisting pending requests.
func (c *ApprovalChecker) SetStore(store ApprovalStore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingStore = store
}

// SetUIAvailableCheck sets the callback used to determine if a UI is available to handle approval requests.
func (c *ApprovalChecker) SetUIAvailableCheck(fn func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uiAvailable = fn
}

// SetAgentPolicy sets a custom approval policy for a specific agent, overriding the default.
func (c *ApprovalChecker) SetAgentPolicy(agentID string, policy *ApprovalPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agentPolicies[agentID] = normalizeApprovalPolicy(policy)
}

// RegisterSkillTools registers tools provided by skills for automatic allowlisting
// when SkillAllowlist is enabled in the policy.
func (c *ApprovalChecker) RegisterSkillTools(tools []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range tools {
		c.skillTools[t] = struct{}{}
	}
}

// PolicyFor returns the effective approval policy for the given agent.
// The returned policy should be treated as read-only.
func (c *ApprovalChecker) PolicyFor(agentID string) *ApprovalPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if policy, ok := c.agentPolicies[agentID]; ok && policy != nil {
		return policy
	}
	return c.defaultPolicy
}

// Check evaluates whether a tool call should be allowed, denied, or requires approval.
// Returns the decision and a reason string explaining the decision.
func (c *ApprovalChecker) Check(ctx context.Context, agentID string, toolCall models.ToolCall) (ApprovalDecision, string) {
	policy := c.PolicyFor(agentID)
	c.mu.RLock()
	skillTools := c.skillTools
	c.mu.RUnlock()

	toolName := toolCall.Name

	// 1. Check denylist first (highest priority)
	if matchesPattern(policy.Denylist, toolName) {
		return ApprovalDenied, "tool in denylist"
	}

	// 2. Check explicit allowlist
	if matchesPattern(policy.Allowlist, toolName) {
		return ApprovalAllowed, "tool in allowlist"
	}

	// 3. Check skill tools (if skill_allowlist is enabled)
	if policy.SkillAllowlist {
		if _, ok := skillTools[toolName]; ok {
			return ApprovalAllowed, "tool provided by skill"
		}
	}

	// 4. Check safe bins
	if matchesPattern(policy.SafeBins, toolName) {
		return ApprovalAllowed, "tool is safe bin"
	}

	// 5. Check require_approval list
	if matchesPattern(policy.RequireApproval, toolName) {
		if !policy.AskFallback && !c.IsUIAvailable() {
			return ApprovalDenied, "approval unavailable"
		}
		return ApprovalPending, "tool requires approval"
	}

	// 6. Default decision
	if policy.DefaultDecision == "" {
		if !policy.AskFallback && !c.IsUIAvailable() {
			return ApprovalDenied, "approval unavailable"
		}
		return ApprovalPending, "default policy"
	}
	if policy.DefaultDecision == ApprovalPending && !policy.AskFallback && !c.IsUIAvailable() {
		return ApprovalDenied, "approval unavailable"
	}
	return policy.DefaultDecision, "default policy"
}

// CreateApprovalRequest creates and persists a pending approval request for a tool call.
func (c *ApprovalChecker) CreateApprovalRequest(ctx context.Context, agentID, sessionID string, toolCall models.ToolCall, reason string) (*ApprovalRequest, error) {
	c.mu.RLock()
	policy := c.agentPolicies[agentID]
	if policy == nil {
		policy = c.defaultPolicy
	}
	store := c.pendingStore
	c.mu.RUnlock()

	ttl := policy.RequestTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	req := &ApprovalRequest{
		ID:         toolCall.ID + "-approval",
		ToolCallID: toolCall.ID,
		ToolName:   toolCall.Name,
		Input:      toolCall.Input,
		AgentID:    agentID,
		SessionID:  sessionID,
		Reason:     reason,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(ttl),
		Decision:   ApprovalPending,
	}

	if store != nil {
		if err := store.Create(ctx, req); err != nil {
			return nil, err
		}
	}

	return req, nil
}

// Approve approves a pending approval request, allowing the tool call to proceed.
func (c *ApprovalChecker) Approve(ctx context.Context, requestID, decidedBy string) error {
	c.mu.RLock()
	store := c.pendingStore
	c.mu.RUnlock()

	if store == nil {
		return nil
	}

	req, err := store.Get(ctx, requestID)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}

	req.Decision = ApprovalAllowed
	req.DecidedAt = time.Now()
	req.DecidedBy = decidedBy
	return store.Update(ctx, req)
}

// Deny denies a pending approval request, preventing the tool call from executing.
func (c *ApprovalChecker) Deny(ctx context.Context, requestID, decidedBy string) error {
	c.mu.RLock()
	store := c.pendingStore
	c.mu.RUnlock()

	if store == nil {
		return nil
	}

	req, err := store.Get(ctx, requestID)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}

	req.Decision = ApprovalDenied
	req.DecidedAt = time.Now()
	req.DecidedBy = decidedBy
	return store.Update(ctx, req)
}

// GetPendingRequests returns all pending approval requests for the specified agent.
func (c *ApprovalChecker) GetPendingRequests(ctx context.Context, agentID string) ([]*ApprovalRequest, error) {
	c.mu.RLock()
	store := c.pendingStore
	c.mu.RUnlock()

	if store == nil {
		return nil, nil
	}
	return store.ListPending(ctx, agentID)
}

// IsUIAvailable returns whether a UI is available to handle approval requests.
func (c *ApprovalChecker) IsUIAvailable() bool {
	c.mu.RLock()
	fn := c.uiAvailable
	c.mu.RUnlock()

	if fn == nil {
		return false
	}
	return fn()
}

// matchesPattern checks if toolName matches any pattern in the list.
// Supports: exact match, prefix* match, *suffix match, * (all), and mcp:* prefix.
func matchesPattern(patterns []string, toolName string) bool {
	normalizedTool := policy.NormalizeTool(toolName)
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		normalizedPattern := policy.NormalizeTool(pattern)
		// Wildcard matches everything
		if normalizedPattern == "*" {
			return true
		}
		if normalizedPattern == normalizedTool {
			return true
		}
		// Handle mcp:* pattern
		if normalizedPattern == "mcp:*" && strings.HasPrefix(normalizedTool, "mcp:") {
			return true
		}
		// Handle prefix* pattern
		if len(normalizedPattern) > 1 && normalizedPattern[len(normalizedPattern)-1] == '*' {
			prefix := normalizedPattern[:len(normalizedPattern)-1]
			if len(normalizedTool) >= len(prefix) && normalizedTool[:len(prefix)] == prefix {
				return true
			}
		}
		// Handle *suffix pattern
		if len(normalizedPattern) > 1 && normalizedPattern[0] == '*' {
			suffix := normalizedPattern[1:]
			if len(normalizedTool) >= len(suffix) && normalizedTool[len(normalizedTool)-len(suffix):] == suffix {
				return true
			}
		}
	}
	return false
}

func normalizeApprovalPolicy(policy *ApprovalPolicy) *ApprovalPolicy {
	defaults := DefaultApprovalPolicy()
	if policy == nil {
		return defaults
	}

	if len(policy.SafeBins) > 0 {
		clone := *policy
		clone.Allowlist = append([]string(nil), policy.Allowlist...)
		clone.Denylist = append([]string(nil), policy.Denylist...)
		clone.RequireApproval = append([]string(nil), policy.RequireApproval...)
		clone.SafeBins = append([]string(nil), policy.SafeBins...)
		return &clone
	}

	merged := *defaults

	if len(policy.Allowlist) > 0 {
		merged.Allowlist = append([]string(nil), policy.Allowlist...)
	}
	if len(policy.Denylist) > 0 {
		merged.Denylist = append([]string(nil), policy.Denylist...)
	}
	if len(policy.RequireApproval) > 0 {
		merged.RequireApproval = append([]string(nil), policy.RequireApproval...)
	}
	if len(policy.SafeBins) > 0 {
		merged.SafeBins = append([]string(nil), policy.SafeBins...)
	}
	if policy.DefaultDecision != "" {
		merged.DefaultDecision = policy.DefaultDecision
	}
	if policy.RequestTTL > 0 {
		merged.RequestTTL = policy.RequestTTL
	}

	if policy.AskFallback || len(policy.RequireApproval) > 0 {
		merged.AskFallback = policy.AskFallback
	}

	return &merged
}

// MemoryApprovalStore is a thread-safe in-memory implementation of ApprovalStore.
type MemoryApprovalStore struct {
	mu       sync.RWMutex
	requests map[string]*ApprovalRequest
}

// NewMemoryApprovalStore creates a new in-memory approval store for testing or single-instance deployments.
func NewMemoryApprovalStore() *MemoryApprovalStore {
	return &MemoryApprovalStore{
		requests: make(map[string]*ApprovalRequest),
	}
}

// Create stores an approval request in memory.
func (s *MemoryApprovalStore) Create(ctx context.Context, req *ApprovalRequest) error {
	if req == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.ID] = req
	return nil
}

// Get returns an approval request by ID, or nil if not found.
func (s *MemoryApprovalStore) Get(ctx context.Context, id string) (*ApprovalRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.requests[id], nil
}

// Update updates an existing approval request in memory.
func (s *MemoryApprovalStore) Update(ctx context.Context, req *ApprovalRequest) error {
	if req == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.ID] = req
	return nil
}

// ListPending returns all pending, non-expired approval requests for the specified agent.
func (s *MemoryApprovalStore) ListPending(ctx context.Context, agentID string) ([]*ApprovalRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*ApprovalRequest
	now := time.Now()
	for _, req := range s.requests {
		if req.Decision != ApprovalPending {
			continue
		}
		if !req.ExpiresAt.IsZero() && req.ExpiresAt.Before(now) {
			continue
		}
		if agentID != "" && req.AgentID != agentID {
			continue
		}
		result = append(result, req)
	}
	return result, nil
}

// Prune removes approval requests older than the specified duration and returns the count removed.
func (s *MemoryApprovalStore) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	var pruned int64
	for id, req := range s.requests {
		if req.CreatedAt.Before(cutoff) {
			delete(s.requests, id)
			pruned++
		}
	}
	return pruned, nil
}
