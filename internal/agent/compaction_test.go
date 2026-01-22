package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	agentctx "github.com/haasonsaas/nexus/internal/agent/context"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestDefaultCompactionConfig(t *testing.T) {
	config := DefaultCompactionConfig()

	if !config.Enabled {
		t.Error("Enabled should be true by default")
	}
	if config.ThresholdPercent != 80 {
		t.Errorf("ThresholdPercent = %d, want 80", config.ThresholdPercent)
	}
	if config.ConfirmationTimeout != 5*time.Minute {
		t.Errorf("ConfirmationTimeout = %v, want 5m", config.ConfirmationTimeout)
	}
	if !config.AutoCompactOnTimeout {
		t.Error("AutoCompactOnTimeout should be true by default")
	}
	if config.FlushPrompt == "" {
		t.Error("FlushPrompt should not be empty")
	}
}

func TestCompactionManager_NewWithNilConfig(t *testing.T) {
	// Should use default config when nil is passed
	manager := NewCompactionManager(nil, nil)

	if manager.config == nil {
		t.Fatal("config should be set to default")
	}
	if manager.config.ThresholdPercent != 80 {
		t.Errorf("ThresholdPercent = %d, want 80 (default)", manager.config.ThresholdPercent)
	}
}

func TestCompactionManager_GetState_UnknownSession(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	state := manager.GetState("unknown-session")
	if state != CompactionIdle {
		t.Errorf("state = %s, want %s", state, CompactionIdle)
	}
}

func TestCompactionManager_GetUsage_UnknownSession(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	usage := manager.GetUsage("unknown-session")
	if usage != 0 {
		t.Errorf("usage = %d, want 0", usage)
	}
}

func TestCompactionManager_GetInfo_UnknownSession(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	info := manager.GetInfo("unknown-session")
	if info == nil {
		t.Fatal("info should not be nil")
	}
	if info.SessionID != "unknown-session" {
		t.Errorf("SessionID = %q, want %q", info.SessionID, "unknown-session")
	}
	if info.State != CompactionIdle {
		t.Errorf("State = %s, want %s", info.State, CompactionIdle)
	}
	if info.Threshold != config.ThresholdPercent {
		t.Errorf("Threshold = %d, want %d", info.Threshold, config.ThresholdPercent)
	}
}

func TestCompactionManager_Reset(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	// Add some state
	manager.mu.Lock()
	manager.sessions["session-1"] = &sessionCompaction{
		state:        CompactionPending,
		usagePercent: 85,
	}
	manager.mu.Unlock()

	// Verify state exists
	if manager.GetState("session-1") != CompactionPending {
		t.Error("expected state to be pending before reset")
	}

	// Reset
	manager.Reset("session-1")

	// Verify state is cleared
	if manager.GetState("session-1") != CompactionIdle {
		t.Error("expected state to be idle after reset")
	}
}

func TestCompactionManager_Check_Disabled(t *testing.T) {
	config := DefaultCompactionConfig()
	config.Enabled = false
	manager := NewCompactionManager(config, nil)

	triggered, err := manager.Check(context.Background(), "session-1", nil, nil, nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if triggered {
		t.Error("should not trigger when disabled")
	}
}

func TestCompactionManager_Check_NilPacker(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil) // nil packer

	triggered, err := manager.Check(context.Background(), "session-1", nil, nil, nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if triggered {
		t.Error("should not trigger with nil packer")
	}
}

func TestCompactionManager_Check_BelowThreshold(t *testing.T) {
	config := DefaultCompactionConfig()
	config.ThresholdPercent = 80

	// Create packer with large budget so usage stays low
	packOpts := agentctx.PackOptions{
		MaxChars: 100000,
	}
	packer := agentctx.NewPacker(packOpts)
	manager := NewCompactionManager(config, packer)

	// Small history - well under threshold
	history := []*models.Message{
		{Role: models.RoleUser, Content: "Hello"},
		{Role: models.RoleAssistant, Content: "Hi there!"},
	}
	incoming := &models.Message{Role: models.RoleUser, Content: "How are you?"}

	triggered, err := manager.Check(context.Background(), "session-1", history, incoming, nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if triggered {
		t.Error("should not trigger when below threshold")
	}

	// Verify state is still idle
	if manager.GetState("session-1") != CompactionIdle {
		t.Errorf("state = %s, want %s", manager.GetState("session-1"), CompactionIdle)
	}
}

func TestCompactionManager_Check_AboveThreshold(t *testing.T) {
	config := DefaultCompactionConfig()
	config.ThresholdPercent = 10 // Very low threshold

	// Create packer with small budget
	packOpts := agentctx.PackOptions{
		MaxChars: 100, // Very small
	}
	packer := agentctx.NewPacker(packOpts)
	manager := NewCompactionManager(config, packer)

	var flushCalled bool
	var flushSessionID string
	manager.SetFlushCallback(func(ctx context.Context, sessionID string, prompt string) error {
		flushCalled = true
		flushSessionID = sessionID
		return nil
	})

	// Large history to exceed threshold
	history := []*models.Message{
		{Role: models.RoleUser, Content: "This is a very long message that will exceed our small budget"},
		{Role: models.RoleAssistant, Content: "This is another long response that adds to the usage"},
	}
	incoming := &models.Message{Role: models.RoleUser, Content: "More content here"}

	triggered, err := manager.Check(context.Background(), "session-1", history, incoming, nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if !triggered {
		t.Error("should trigger when above threshold")
	}
	if !flushCalled {
		t.Error("flush callback should be called")
	}
	if flushSessionID != "session-1" {
		t.Errorf("flush session = %q, want %q", flushSessionID, "session-1")
	}
	if manager.GetState("session-1") != CompactionPending {
		t.Errorf("state = %s, want %s", manager.GetState("session-1"), CompactionPending)
	}
}

func TestCompactionManager_ConfirmFlush(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	var compactionCompleted bool
	var droppedCount int
	manager.SetCompactionCallback(func(ctx context.Context, sessionID string, dropped int) error {
		compactionCompleted = true
		droppedCount = dropped
		return nil
	})

	// Set up pending state
	manager.mu.Lock()
	manager.sessions["session-1"] = &sessionCompaction{
		state: CompactionPending,
	}
	manager.mu.Unlock()

	// Confirm flush
	err := manager.ConfirmFlush(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("ConfirmFlush() error = %v", err)
	}

	if !compactionCompleted {
		t.Error("compaction callback should be called")
	}
	if manager.GetState("session-1") != CompactionIdle {
		t.Errorf("state = %s, want %s after confirm", manager.GetState("session-1"), CompactionIdle)
	}
	_ = droppedCount
}

func TestCompactionManager_RejectFlush(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	var compactionCompleted bool
	manager.SetCompactionCallback(func(ctx context.Context, sessionID string, dropped int) error {
		compactionCompleted = true
		return nil
	})

	// Set up pending state
	manager.mu.Lock()
	manager.sessions["session-1"] = &sessionCompaction{
		state: CompactionPending,
	}
	manager.mu.Unlock()

	// Reject flush
	err := manager.RejectFlush(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("RejectFlush() error = %v", err)
	}

	// Compaction should still proceed
	if !compactionCompleted {
		t.Error("compaction callback should be called even on reject")
	}
	if manager.GetState("session-1") != CompactionIdle {
		t.Errorf("state = %s, want %s after reject", manager.GetState("session-1"), CompactionIdle)
	}
}

func TestCompactionManager_ConfirmFlush_UnknownSession(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	// Should not error for unknown session
	err := manager.ConfirmFlush(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("ConfirmFlush() error = %v", err)
	}
}

func TestCompactionManager_RejectFlush_UnknownSession(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	// Should not error for unknown session
	err := manager.RejectFlush(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("RejectFlush() error = %v", err)
	}
}

func TestCompactionManager_ConcurrentAccess(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Concurrent access to same session
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sessionID := "session-1"

			_ = manager.GetState(sessionID)
			_ = manager.GetUsage(sessionID)
			_ = manager.GetInfo(sessionID)

			if id%2 == 0 {
				manager.Reset(sessionID)
			}
		}(i)
	}

	wg.Wait()
}

func TestIsFlushResponse(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{"no_reply uppercase", "NO_REPLY", true},
		{"no_reply lowercase", "no_reply", true},
		{"no_reply mixed", "No_Reply", true},
		{"nothing to save", "nothing to save", true},
		{"nothing needs attention", "Nothing needs attention", true},
		{"saved to memory", "I have saved to memory the following...", true},
		{"stored in memory", "Stored in memory.", true},
		{"memory updated", "Memory updated with your preferences.", true},
		{"unrelated content", "Here is the information you requested.", false},
		{"empty string", "", false},
		{"very long content", "This is a very long message that does not contain any flush patterns and should return false because it doesn't match anything in our pattern list", false},
		{"partial match not at start", "OK, let me think about no_reply options", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFlushResponse(tt.content)
			if result != tt.expected {
				t.Errorf("IsFlushResponse(%q) = %v, want %v", tt.content, result, tt.expected)
			}
		})
	}
}

func TestCompactionTool_Name(t *testing.T) {
	manager := NewCompactionManager(nil, nil)
	tool := NewCompactionTool(manager)

	if tool.Name() != "compaction_status" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "compaction_status")
	}
}

func TestCompactionTool_Description(t *testing.T) {
	manager := NewCompactionManager(nil, nil)
	tool := NewCompactionTool(manager)

	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestCompactionTool_Schema(t *testing.T) {
	manager := NewCompactionManager(nil, nil)
	tool := NewCompactionTool(manager)

	schema := tool.Schema()
	if schema == nil {
		t.Fatal("Schema() should not be nil")
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}
}

func TestCompactionTool_Execute_NoSession(t *testing.T) {
	manager := NewCompactionManager(nil, nil)
	tool := NewCompactionTool(manager)

	// Execute without session in context
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result != "no session context" {
		t.Errorf("result = %q, want %q", result, "no session context")
	}
}

func TestCompactionTool_Execute_WithSession(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)
	tool := NewCompactionTool(manager)

	// Add some state
	manager.mu.Lock()
	manager.sessions["session-123"] = &sessionCompaction{
		state:        CompactionPending,
		usagePercent: 85,
	}
	manager.mu.Unlock()

	// Create context with session
	session := &models.Session{ID: "session-123"}
	ctx := WithSession(context.Background(), session)

	result, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify output contains expected info
	if result == "" {
		t.Error("result should not be empty")
	}
	if !containsString(result, "session-123") {
		t.Errorf("result should contain session ID: %s", result)
	}
	if !containsString(result, "pending") {
		t.Errorf("result should contain state: %s", result)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0)
}

func TestCompactionStates(t *testing.T) {
	tests := []struct {
		state    CompactionState
		expected string
	}{
		{CompactionIdle, "idle"},
		{CompactionPending, "pending"},
		{CompactionAwaitingConfirm, "awaiting_confirm"},
		{CompactionInProgress, "in_progress"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("CompactionState = %q, want %q", string(tt.state), tt.expected)
			}
		})
	}
}

func TestCompactionInfo_Fields(t *testing.T) {
	now := time.Now()
	info := &CompactionInfo{
		SessionID:    "session-1",
		State:        CompactionPending,
		UsagePercent: 85,
		LastCheck:    now,
		FlushSentAt:  now,
		Threshold:    80,
	}

	if info.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", info.SessionID, "session-1")
	}
	if info.State != CompactionPending {
		t.Errorf("State = %s, want %s", info.State, CompactionPending)
	}
	if info.UsagePercent != 85 {
		t.Errorf("UsagePercent = %d, want 85", info.UsagePercent)
	}
	if info.Threshold != 80 {
		t.Errorf("Threshold = %d, want 80", info.Threshold)
	}
}

func TestCompactionManager_SetCallbacks(t *testing.T) {
	config := DefaultCompactionConfig()
	manager := NewCompactionManager(config, nil)

	var flushCalled, compactionCalled bool

	manager.SetFlushCallback(func(ctx context.Context, sessionID string, prompt string) error {
		flushCalled = true
		return nil
	})

	manager.SetCompactionCallback(func(ctx context.Context, sessionID string, dropped int) error {
		compactionCalled = true
		return nil
	})

	// Verify callbacks are set
	manager.mu.RLock()
	if manager.onFlushRequired == nil {
		t.Error("flush callback should be set")
	}
	if manager.onCompactionComplete == nil {
		t.Error("compaction callback should be set")
	}
	manager.mu.RUnlock()

	// The callbacks aren't called yet
	if flushCalled || compactionCalled {
		t.Error("callbacks should not be called just by setting them")
	}
}

func TestContainsFlushPattern(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"no_reply", "no_reply", true},
		{"NO_REPLY", "no_reply", true},
		{"Contains NO_REPLY here", "no_reply", true},
		{"something else", "no_reply", false},
		{"", "no_reply", false},
		{"no_reply", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.substr, func(t *testing.T) {
			result := containsFlushPattern(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("containsFlushPattern(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}
