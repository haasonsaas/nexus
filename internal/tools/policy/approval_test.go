package policy

import (
	"context"
	"strings"
	"testing"
	"time"

	proto "github.com/haasonsaas/nexus/pkg/proto"
)

func TestApprovalManager_NoApprovalNeeded(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: false,
		RequireApprovalForHighRisk:  false,
		ApprovalTimeout:             time.Minute,
	})

	err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	if err != nil {
		t.Errorf("expected no approval needed, got %v", err)
	}
}

func TestApprovalManager_ApprovalRequired(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             time.Minute,
	})

	err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	if err == nil {
		t.Error("expected approval required error")
	}
	if !strings.Contains(err.Error(), "approval required") {
		t.Errorf("expected 'approval required' in error, got %v", err)
	}
}

func TestApprovalManager_ApproveAndDeny(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             time.Minute,
	})

	t.Run("approve request", func(t *testing.T) {
		err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
		if err == nil {
			t.Fatal("expected approval required error")
		}

		// Extract request ID
		requestID := extractRequestID(err.Error())
		if requestID == "" {
			t.Fatal("could not extract request ID from error")
		}

		// Approve
		err = manager.Approve(requestID, "admin")
		if err != nil {
			t.Fatalf("unexpected error approving: %v", err)
		}

		// Verify status
		req, err := manager.GetRequest(requestID)
		if err != nil {
			t.Fatalf("unexpected error getting request: %v", err)
		}
		if req.Status != ApprovalStatusApproved {
			t.Errorf("expected approved status, got %s", req.Status)
		}
		if req.DecidedBy != "admin" {
			t.Errorf("expected decided by 'admin', got %s", req.DecidedBy)
		}
	})

	t.Run("deny request", func(t *testing.T) {
		err := manager.CheckApproval(context.Background(), "edge:device.tool2", "device", "{}", "session2", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
		requestID := extractRequestID(err.Error())

		err = manager.Deny(requestID, "admin", "security concern")
		if err != nil {
			t.Fatalf("unexpected error denying: %v", err)
		}

		req, err := manager.GetRequest(requestID)
		if err != nil {
			t.Fatalf("unexpected error getting request: %v", err)
		}
		if req.Status != ApprovalStatusDenied {
			t.Errorf("expected denied status, got %s", req.Status)
		}
		if req.DenialReason != "security concern" {
			t.Errorf("expected denial reason 'security concern', got %s", req.DenialReason)
		}
	})
}

func TestApprovalManager_Expiration(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             10 * time.Millisecond, // Very short for testing
	})

	err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	requestID := extractRequestID(err.Error())

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Check that request is expired
	req, err := manager.GetRequest(requestID)
	if err != nil {
		t.Fatalf("unexpected error getting request: %v", err)
	}
	if req.Status != ApprovalStatusExpired {
		t.Errorf("expected expired status, got %s", req.Status)
	}

	// Trying to approve should fail
	err = manager.Approve(requestID, "admin")
	if err == nil || !strings.Contains(err.Error(), "already decided") {
		t.Errorf("expected 'already decided' error, got %v", err)
	}
}

func TestApprovalManager_TrustLevels(t *testing.T) {
	registry := NewToolRegistry(nil)
	registry.RegisterEdgeServer("trusted-device", []string{"safe_tool"}, TrustTrusted)
	registry.RegisterEdgeServer("tofu-device", []string{"medium_tool"}, TrustTOFU)
	registry.RegisterEdgeServer("untrusted-device", []string{"risky_tool"}, TrustUntrusted)

	manager := NewApprovalManager(registry, &ApprovalPolicy{
		ApprovalTimeout: time.Minute,
		ByRiskLevel: map[proto.RiskLevel]RiskApprovalPolicy{
			proto.RiskLevel_RISK_LEVEL_LOW: {
				RequireApproval: false,
				MinTrustLevel:   TrustUntrusted, // No minimum for low risk
			},
			proto.RiskLevel_RISK_LEVEL_MEDIUM: {
				RequireApproval: false,
				MinTrustLevel:   TrustTOFU, // Need at least TOFU
			},
			proto.RiskLevel_RISK_LEVEL_HIGH: {
				RequireApproval: true,
				MinTrustLevel:   TrustTrusted, // Need fully trusted
			},
		},
	})

	tests := []struct {
		name        string
		edgeID      string
		riskLevel   proto.RiskLevel
		wantApproval bool
	}{
		{"trusted + low risk", "trusted-device", proto.RiskLevel_RISK_LEVEL_LOW, false},
		{"trusted + medium risk", "trusted-device", proto.RiskLevel_RISK_LEVEL_MEDIUM, false},
		{"trusted + high risk", "trusted-device", proto.RiskLevel_RISK_LEVEL_HIGH, false}, // Trusted bypasses high risk
		{"tofu + low risk", "tofu-device", proto.RiskLevel_RISK_LEVEL_LOW, false},
		{"tofu + medium risk", "tofu-device", proto.RiskLevel_RISK_LEVEL_MEDIUM, false},
		{"tofu + high risk", "tofu-device", proto.RiskLevel_RISK_LEVEL_HIGH, true}, // TOFU not enough for high risk
		{"untrusted + low risk", "untrusted-device", proto.RiskLevel_RISK_LEVEL_LOW, false},
		{"untrusted + medium risk", "untrusted-device", proto.RiskLevel_RISK_LEVEL_MEDIUM, true}, // Need TOFU for medium
		{"untrusted + high risk", "untrusted-device", proto.RiskLevel_RISK_LEVEL_HIGH, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolName := "edge:" + tt.edgeID + ".tool"
			err := manager.CheckApproval(context.Background(), toolName, tt.edgeID, "{}", "session-"+tt.name, "user1", tt.riskLevel)
			gotApproval := err != nil && strings.Contains(err.Error(), "approval required")
			if gotApproval != tt.wantApproval {
				t.Errorf("expected approval=%v, got error=%v", tt.wantApproval, err)
			}
		})
	}
}

func TestApprovalManager_AlwaysNeverLists(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: false,
		ApprovalTimeout:             time.Minute,
		AlwaysRequireApprovalFor:    []string{"edge:device.dangerous_tool"},
		NeverRequireApprovalFor:     []string{"edge:device.safe_tool"},
	})

	t.Run("always requires approval", func(t *testing.T) {
		err := manager.CheckApproval(context.Background(), "edge:device.dangerous_tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
		if err == nil || !strings.Contains(err.Error(), "approval required") {
			t.Error("expected approval required for always-approve tool")
		}
	})

	t.Run("never requires approval", func(t *testing.T) {
		err := manager.CheckApproval(context.Background(), "edge:device.safe_tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_HIGH)
		if err != nil {
			t.Errorf("expected no approval for never-approve tool, got %v", err)
		}
	})
}

func TestApprovalManager_RateLimit(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		ApprovalTimeout: time.Minute,
		ByRiskLevel: map[proto.RiskLevel]RiskApprovalPolicy{
			proto.RiskLevel_RISK_LEVEL_MEDIUM: {
				RequireApproval:          false,
				MinTrustLevel:            TrustUntrusted,
				MaxAutoApprovePerSession: 2,
			},
		},
	})

	sessionID := "rate-limit-session"

	// First two should be auto-approved
	for i := 0; i < 2; i++ {
		err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", sessionID, "user1", proto.RiskLevel_RISK_LEVEL_MEDIUM)
		if err != nil {
			t.Errorf("request %d should be auto-approved, got %v", i+1, err)
		}
	}

	// Third should require approval (rate limit hit)
	err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", sessionID, "user1", proto.RiskLevel_RISK_LEVEL_MEDIUM)
	if err == nil || !strings.Contains(err.Error(), "approval required") {
		t.Error("expected approval required after rate limit")
	}
}

func TestApprovalManager_ListPending(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             time.Minute,
	})

	// Create multiple pending requests
	for i := 0; i < 3; i++ {
		manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	}

	pending := manager.ListPending()
	if len(pending) != 3 {
		t.Errorf("expected 3 pending requests, got %d", len(pending))
	}
}

func TestApprovalManager_ListBySession(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             time.Minute,
	})

	// Create requests for different sessions
	manager.CheckApproval(context.Background(), "edge:device.tool1", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	manager.CheckApproval(context.Background(), "edge:device.tool2", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	manager.CheckApproval(context.Background(), "edge:device.tool3", "device", "{}", "session2", "user1", proto.RiskLevel_RISK_LEVEL_LOW)

	session1Requests := manager.ListBySession("session1")
	if len(session1Requests) != 2 {
		t.Errorf("expected 2 requests for session1, got %d", len(session1Requests))
	}

	session2Requests := manager.ListBySession("session2")
	if len(session2Requests) != 1 {
		t.Errorf("expected 1 request for session2, got %d", len(session2Requests))
	}
}

func TestApprovalManager_Callbacks(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             time.Minute,
	})

	var requiredCalled, decidedCalled bool
	var lastRequired, lastDecided *ApprovalRequest

	manager.SetApprovalRequiredHandler(func(req *ApprovalRequest) {
		requiredCalled = true
		lastRequired = req
	})

	manager.SetApprovalDecidedHandler(func(req *ApprovalRequest) {
		decidedCalled = true
		lastDecided = req
	})

	// Trigger approval required
	err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	if !requiredCalled {
		t.Error("expected approval required callback to be called")
	}
	if lastRequired == nil || lastRequired.ToolName != "edge:device.tool" {
		t.Error("callback received wrong request")
	}

	// Approve
	requestID := extractRequestID(err.Error())
	manager.Approve(requestID, "admin")

	if !decidedCalled {
		t.Error("expected approval decided callback to be called")
	}
	if lastDecided == nil || lastDecided.Status != ApprovalStatusApproved {
		t.Error("callback received wrong decision")
	}
}

func TestApprovalManager_WaitForApproval(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             time.Minute,
	})

	t.Run("approved", func(t *testing.T) {
		err := manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
		requestID := extractRequestID(err.Error())

		// Approve in background
		go func() {
			time.Sleep(50 * time.Millisecond)
			manager.Approve(requestID, "admin")
		}()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err = manager.WaitForApproval(ctx, requestID)
		if err != nil {
			t.Errorf("expected no error after approval, got %v", err)
		}
	})

	t.Run("denied", func(t *testing.T) {
		err := manager.CheckApproval(context.Background(), "edge:device.tool2", "device", "{}", "session2", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
		requestID := extractRequestID(err.Error())

		// Deny in background
		go func() {
			time.Sleep(50 * time.Millisecond)
			manager.Deny(requestID, "admin", "not allowed")
		}()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err = manager.WaitForApproval(ctx, requestID)
		if err == nil {
			t.Error("expected error after denial")
		}
		if !strings.Contains(err.Error(), "denied") {
			t.Errorf("expected denial error, got %v", err)
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		err := manager.CheckApproval(context.Background(), "edge:device.tool3", "device", "{}", "session3", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
		requestID := extractRequestID(err.Error())

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err = manager.WaitForApproval(ctx, requestID)
		if err == nil || err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestApprovalManager_CleanupExpired(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             10 * time.Millisecond,
	})

	// Create some requests
	for i := 0; i < 3; i++ {
		manager.CheckApproval(context.Background(), "edge:device.tool", "device", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_LOW)
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Verify they're expired
	pending := manager.ListPending()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after expiration, got %d", len(pending))
	}
}

func TestApprovalManager_NonEdgeTool(t *testing.T) {
	manager := NewApprovalManager(nil, &ApprovalPolicy{
		RequireApprovalForUntrusted: true,
		ApprovalTimeout:             time.Minute,
	})

	// Non-edge tools should not require approval via this system
	err := manager.CheckApproval(context.Background(), "core.read", "", "{}", "session1", "user1", proto.RiskLevel_RISK_LEVEL_HIGH)
	if err != nil {
		t.Errorf("non-edge tool should not require approval, got %v", err)
	}
}

func TestTrustMeetsMinimum(t *testing.T) {
	tests := []struct {
		actual   TrustLevel
		minimum  TrustLevel
		expected bool
	}{
		{TrustTrusted, TrustUntrusted, true},
		{TrustTrusted, TrustTOFU, true},
		{TrustTrusted, TrustTrusted, true},
		{TrustTOFU, TrustUntrusted, true},
		{TrustTOFU, TrustTOFU, true},
		{TrustTOFU, TrustTrusted, false},
		{TrustUntrusted, TrustUntrusted, true},
		{TrustUntrusted, TrustTOFU, false},
		{TrustUntrusted, TrustTrusted, false},
	}

	for _, tt := range tests {
		name := string(tt.actual) + " >= " + string(tt.minimum)
		t.Run(name, func(t *testing.T) {
			result := trustMeetsMinimum(tt.actual, tt.minimum)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestDefaultApprovalPolicy(t *testing.T) {
	policy := DefaultApprovalPolicy()

	if !policy.RequireApprovalForUntrusted {
		t.Error("expected RequireApprovalForUntrusted to be true")
	}
	if !policy.RequireApprovalForHighRisk {
		t.Error("expected RequireApprovalForHighRisk to be true")
	}
	if policy.ApprovalTimeout != 5*time.Minute {
		t.Errorf("expected 5 minute timeout, got %v", policy.ApprovalTimeout)
	}
	if len(policy.ByRiskLevel) == 0 {
		t.Error("expected ByRiskLevel to be populated")
	}
}

func extractRequestID(errMsg string) string {
	// Format: "approval required: request_id=apr_xxx"
	parts := strings.Split(errMsg, "request_id=")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
