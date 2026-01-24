package heartbeat

import (
	"testing"
	"time"
)

func TestStatus_IsStale(t *testing.T) {
	s := &Status{LastSeen: time.Now().Add(-time.Hour)}
	if !s.IsStale(30 * time.Minute) {
		t.Error("status should be stale after 1 hour")
	}

	s.LastSeen = time.Now().Add(-10 * time.Minute)
	if s.IsStale(30 * time.Minute) {
		t.Error("status should not be stale after 10 minutes")
	}
}

func TestMonitor_Record(t *testing.T) {
	m := NewMonitor(DefaultConfig())

	m.Record("agent1", "HEARTBEAT_OK")

	status := m.GetStatus("agent1")
	if status == nil {
		t.Fatal("expected status")
	}
	if !status.Healthy {
		t.Error("agent should be healthy after recording")
	}
	if status.MissedCount != 0 {
		t.Error("missed count should be 0")
	}
}

func TestMonitor_MarkMissed(t *testing.T) {
	config := DefaultConfig()
	config.MissedThreshold = 2
	m := NewMonitor(config)

	// First record
	m.Record("agent1", "ok")

	// Miss once - still healthy
	m.MarkMissed("agent1")
	status := m.GetStatus("agent1")
	if !status.Healthy {
		t.Error("agent should still be healthy after 1 miss")
	}

	// Miss again - now unhealthy
	m.MarkMissed("agent1")
	status = m.GetStatus("agent1")
	if status.Healthy {
		t.Error("agent should be unhealthy after 2 misses")
	}
}

func TestMonitor_GetAllStatuses(t *testing.T) {
	m := NewMonitor(DefaultConfig())

	m.Record("agent1", "ok")
	m.Record("agent2", "ok")
	m.Record("agent3", "ok")

	statuses := m.GetAllStatuses()
	if len(statuses) != 3 {
		t.Errorf("expected 3 statuses, got %d", len(statuses))
	}
}

func TestMonitor_Remove(t *testing.T) {
	m := NewMonitor(DefaultConfig())

	m.Record("agent1", "ok")
	m.Remove("agent1")

	status := m.GetStatus("agent1")
	if status != nil {
		t.Error("status should be nil after removal")
	}
}

func TestMonitor_GetHealthyCount(t *testing.T) {
	config := DefaultConfig()
	config.MissedThreshold = 1
	m := NewMonitor(config)

	m.Record("agent1", "ok")
	m.Record("agent2", "ok")
	m.Record("agent3", "ok")

	if m.GetHealthyCount() != 3 {
		t.Error("all agents should be healthy")
	}

	m.MarkMissed("agent1")

	if m.GetHealthyCount() != 2 {
		t.Error("two agents should be healthy")
	}
}

func TestStripToken_EmptyInput(t *testing.T) {
	result := StripToken("", 100)
	if !result.ShouldSkip {
		t.Error("empty input should skip")
	}
}

func TestStripToken_NoToken(t *testing.T) {
	result := StripToken("Regular message without token", 100)
	if result.ShouldSkip {
		t.Error("message without token should not skip")
	}
	if result.DidStrip {
		t.Error("should not have stripped anything")
	}
	if result.Text != "Regular message without token" {
		t.Errorf("text mismatch: %s", result.Text)
	}
}

func TestStripToken_OnlyToken(t *testing.T) {
	result := StripToken("HEARTBEAT_OK", 100)
	if !result.ShouldSkip {
		t.Error("message with only token should skip")
	}
	if !result.DidStrip {
		t.Error("should have stripped token")
	}
}

func TestStripToken_TokenAtStart(t *testing.T) {
	result := StripToken("HEARTBEAT_OK Some additional message", 10)
	// Text is 25 chars, more than maxAckChars
	if result.ShouldSkip {
		t.Error("message with content should not skip")
	}
	if result.Text != "Some additional message" {
		t.Errorf("expected 'Some additional message', got %q", result.Text)
	}
}

func TestStripToken_TokenAtEnd(t *testing.T) {
	result := StripToken("Some message HEARTBEAT_OK", 10)
	if result.ShouldSkip {
		t.Error("message with content should not skip")
	}
	if result.Text != "Some message" {
		t.Errorf("expected 'Some message', got %q", result.Text)
	}
}

func TestStripToken_ShortAck(t *testing.T) {
	result := StripToken("HEARTBEAT_OK All good", 100)
	// "All good" is 8 chars, less than maxAckChars
	if !result.ShouldSkip {
		t.Error("short ack should skip")
	}
}

func TestStripToken_MarkdownWrapped(t *testing.T) {
	result := StripToken("**HEARTBEAT_OK**", 100)
	if !result.ShouldSkip {
		t.Error("markdown wrapped token should skip")
	}
}

func TestStripToken_HTMLWrapped(t *testing.T) {
	result := StripToken("<b>HEARTBEAT_OK</b>", 100)
	if !result.ShouldSkip {
		t.Error("HTML wrapped token should skip")
	}
}

func TestResolvePrompt_Default(t *testing.T) {
	prompt := ResolvePrompt("")
	if prompt != DefaultPrompt {
		t.Error("empty prompt should return default")
	}
}

func TestResolvePrompt_Custom(t *testing.T) {
	custom := "Check workspace status"
	prompt := ResolvePrompt(custom)
	if prompt != custom {
		t.Error("custom prompt should be returned")
	}
}

func TestResolvePrompt_Whitespace(t *testing.T) {
	prompt := ResolvePrompt("   ")
	if prompt != DefaultPrompt {
		t.Error("whitespace-only prompt should return default")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.Enabled {
		t.Error("heartbeat should be disabled by default")
	}
	if config.Interval != DefaultInterval {
		t.Error("wrong default interval")
	}
	if config.Prompt != DefaultPrompt {
		t.Error("wrong default prompt")
	}
}

func TestMonitor_CheckStale(t *testing.T) {
	config := DefaultConfig()
	config.Interval = 100 * time.Millisecond
	m := NewMonitor(config)

	m.Record("agent1", "ok")

	// Wait for staleness
	time.Sleep(250 * time.Millisecond)

	status := m.Check("agent1")
	if status.Healthy {
		t.Error("stale agent should be unhealthy")
	}
	if status.MissedCount != 1 {
		t.Errorf("missed count should be 1, got %d", status.MissedCount)
	}
}

func TestMonitor_CheckUnknownAgent(t *testing.T) {
	m := NewMonitor(DefaultConfig())

	status := m.Check("unknown")
	if status == nil {
		t.Fatal("expected status for unknown agent")
	}
	if status.Healthy {
		t.Error("unknown agent should be unhealthy")
	}
}
