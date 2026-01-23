package testharness_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/tools/policy"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// Ensure strings is used in tests
var _ = strings.Contains

// TestApprovalFlow_HighRiskRequiresApproval verifies that high-risk tools require approval.
func TestApprovalFlow_HighRiskRequiresApproval(t *testing.T) {
	manager := policy.NewApprovalManager(nil, nil)

	err := manager.CheckApproval(
		context.Background(),
		"edge:shell.exec",
		"edge-1",
		`{"command":"rm -rf /tmp/test"}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_HIGH,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	// Verify request ID is in error message
	if !strings.Contains(err.Error(), "request_id=") {
		t.Errorf("expected error to contain request_id, got %v", err)
	}
}

// TestApprovalFlow_LowRiskAutoApproved verifies that low-risk tools are auto-approved.
func TestApprovalFlow_LowRiskAutoApproved(t *testing.T) {
	manager := policy.NewApprovalManager(nil, nil)

	err := manager.CheckApproval(
		context.Background(),
		"edge:status.check",
		"edge-1",
		`{}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_LOW,
	)

	if err != nil {
		t.Fatalf("expected nil (auto-approved), got %v", err)
	}
}

// TestApprovalFlow_ApproveRequest verifies the approval workflow.
func TestApprovalFlow_ApproveRequest(t *testing.T) {
	manager := policy.NewApprovalManager(nil, nil)

	var capturedRequest *policy.ApprovalRequest
	manager.SetApprovalRequiredHandler(func(req *policy.ApprovalRequest) {
		capturedRequest = req
	})

	err := manager.CheckApproval(
		context.Background(),
		"edge:shell.exec",
		"edge-1",
		`{"command":"ls"}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_HIGH,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	if capturedRequest == nil {
		t.Fatal("expected approval handler to be called")
	}

	// Approve the request
	if err := manager.Approve(capturedRequest.ID, "admin-1"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}

	// Verify request status
	req, err := manager.GetRequest(capturedRequest.ID)
	if err != nil {
		t.Fatalf("GetRequest() error = %v", err)
	}

	if req.Status != policy.ApprovalStatusApproved {
		t.Errorf("expected status Approved, got %v", req.Status)
	}
	if req.DecidedBy != "admin-1" {
		t.Errorf("expected DecidedBy admin-1, got %v", req.DecidedBy)
	}
}

// TestApprovalFlow_DenyRequest verifies request denial.
func TestApprovalFlow_DenyRequest(t *testing.T) {
	manager := policy.NewApprovalManager(nil, nil)

	var capturedRequest *policy.ApprovalRequest
	manager.SetApprovalRequiredHandler(func(req *policy.ApprovalRequest) {
		capturedRequest = req
	})

	err := manager.CheckApproval(
		context.Background(),
		"edge:shell.exec",
		"edge-1",
		`{"command":"sudo rm -rf /"}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_CRITICAL,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	// Deny the request
	denyReason := "dangerous command"
	if err := manager.Deny(capturedRequest.ID, "admin-1", denyReason); err != nil {
		t.Fatalf("Deny() error = %v", err)
	}

	// Verify request status
	req, err := manager.GetRequest(capturedRequest.ID)
	if err != nil {
		t.Fatalf("GetRequest() error = %v", err)
	}

	if req.Status != policy.ApprovalStatusDenied {
		t.Errorf("expected status Denied, got %v", req.Status)
	}
	if req.DenialReason != denyReason {
		t.Errorf("expected DenialReason %q, got %q", denyReason, req.DenialReason)
	}
}

// TestApprovalFlow_WaitForApproval verifies async approval waiting.
func TestApprovalFlow_WaitForApproval(t *testing.T) {
	manager := policy.NewApprovalManager(nil, nil)

	var capturedRequest *policy.ApprovalRequest
	manager.SetApprovalRequiredHandler(func(req *policy.ApprovalRequest) {
		capturedRequest = req
	})

	err := manager.CheckApproval(
		context.Background(),
		"edge:shell.exec",
		"edge-1",
		`{"command":"ls"}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_HIGH,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	// Approve in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		if err := manager.Approve(capturedRequest.ID, "admin-1"); err != nil {
			t.Errorf("background Approve() error = %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = manager.WaitForApproval(ctx, capturedRequest.ID)
	if err != nil {
		t.Fatalf("WaitForApproval() error = %v", err)
	}
}

// TestApprovalFlow_WaitForDenial verifies async denial waiting.
func TestApprovalFlow_WaitForDenial(t *testing.T) {
	manager := policy.NewApprovalManager(nil, nil)

	var capturedRequest *policy.ApprovalRequest
	manager.SetApprovalRequiredHandler(func(req *policy.ApprovalRequest) {
		capturedRequest = req
	})

	err := manager.CheckApproval(
		context.Background(),
		"edge:shell.exec",
		"edge-1",
		`{"command":"rm -rf /"}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_CRITICAL,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	// Deny in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		if err := manager.Deny(capturedRequest.ID, "admin-1", "too dangerous"); err != nil {
			t.Errorf("background Deny() error = %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = manager.WaitForApproval(ctx, capturedRequest.ID)
	if !errors.Is(err, policy.ErrApprovalDenied) {
		t.Fatalf("expected ErrApprovalDenied, got %v", err)
	}
	if !strings.Contains(err.Error(), "too dangerous") {
		t.Errorf("expected denial reason in error, got %v", err)
	}
}

// TestApprovalFlow_Expiration verifies request expiration.
func TestApprovalFlow_Expiration(t *testing.T) {
	// Custom policy with very short timeout
	customPolicy := policy.DefaultApprovalPolicy()
	customPolicy.ApprovalTimeout = 50 * time.Millisecond

	manager := policy.NewApprovalManager(nil, customPolicy)

	var capturedRequest *policy.ApprovalRequest
	manager.SetApprovalRequiredHandler(func(req *policy.ApprovalRequest) {
		capturedRequest = req
	})

	err := manager.CheckApproval(
		context.Background(),
		"edge:shell.exec",
		"edge-1",
		`{"command":"ls"}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_HIGH,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Verify request is expired
	req, err := manager.GetRequest(capturedRequest.ID)
	if err != nil {
		t.Fatalf("GetRequest() error = %v", err)
	}

	if req.Status != policy.ApprovalStatusExpired {
		t.Errorf("expected status Expired, got %v", req.Status)
	}

	// Trying to approve expired request should fail
	err = manager.Approve(capturedRequest.ID, "admin-1")
	if err == nil {
		t.Error("expected error when approving expired request")
	}
	// The error could be ErrApprovalExpired or "request already decided: expired"
	if !errors.Is(err, policy.ErrApprovalExpired) && !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expiration error, got %v", err)
	}
}

// TestApprovalFlow_RateLimiting verifies session-based rate limiting.
func TestApprovalFlow_RateLimiting(t *testing.T) {
	customPolicy := policy.DefaultApprovalPolicy()
	// Medium risk allows 10 auto-approvals per session
	customPolicy.ByRiskLevel[proto.RiskLevel_RISK_LEVEL_MEDIUM] = policy.RiskApprovalPolicy{
		RequireApproval:          false,
		MinTrustLevel:            policy.TrustUntrusted,
		MaxAutoApprovePerSession: 2, // Very low limit for testing
	}

	manager := policy.NewApprovalManager(nil, customPolicy)

	// First two should auto-approve
	for i := 0; i < 2; i++ {
		err := manager.CheckApproval(
			context.Background(),
			"edge:tool.medium",
			"edge-1",
			`{}`,
			"session-1",
			"user-1",
			proto.RiskLevel_RISK_LEVEL_MEDIUM,
		)
		if err != nil {
			t.Fatalf("auto-approval %d should succeed, got %v", i+1, err)
		}
	}

	// Third should require approval (rate limited)
	err := manager.CheckApproval(
		context.Background(),
		"edge:tool.medium",
		"edge-1",
		`{}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_MEDIUM,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired after rate limit, got %v", err)
	}
}

// TestApprovalFlow_AlwaysRequireList verifies explicit approval list.
func TestApprovalFlow_AlwaysRequireList(t *testing.T) {
	customPolicy := policy.DefaultApprovalPolicy()
	customPolicy.AlwaysRequireApprovalFor = []string{"edge:dangerous.*"}

	manager := policy.NewApprovalManager(nil, customPolicy)

	// Should require approval even for low risk
	err := manager.CheckApproval(
		context.Background(),
		"edge:dangerous.tool",
		"edge-1",
		`{}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_LOW,
	)

	if !errors.Is(err, policy.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired for always-require tool, got %v", err)
	}
}

// TestApprovalFlow_NeverRequireList verifies explicit no-approval list.
func TestApprovalFlow_NeverRequireList(t *testing.T) {
	customPolicy := policy.DefaultApprovalPolicy()
	customPolicy.NeverRequireApprovalFor = []string{"edge:safe.*"}

	manager := policy.NewApprovalManager(nil, customPolicy)

	// Should not require approval even for high risk
	err := manager.CheckApproval(
		context.Background(),
		"edge:safe.tool",
		"edge-1",
		`{}`,
		"session-1",
		"user-1",
		proto.RiskLevel_RISK_LEVEL_HIGH,
	)

	if err != nil {
		t.Fatalf("expected auto-approve for never-require tool, got %v", err)
	}
}
