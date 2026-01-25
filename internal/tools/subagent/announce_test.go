package subagent

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDurationShort(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"zero duration", 0, "n/a"},
		{"negative duration", -1 * time.Second, "n/a"},
		{"seconds only", 45 * time.Second, "45s"},
		{"one second", 1 * time.Second, "1s"},
		{"minutes and seconds", 3*time.Minute + 25*time.Second, "3m25s"},
		{"one minute", 1 * time.Minute, "1m0s"},
		{"hours and minutes", 2*time.Hour + 15*time.Minute, "2h15m"},
		{"one hour", 1 * time.Hour, "1h0m"},
		{"many hours", 25*time.Hour + 30*time.Minute, "25h30m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDurationShort(tt.duration)
			if result != tt.expected {
				t.Errorf("FormatDurationShort(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		expected string
	}{
		{"zero", 0, "0"},
		{"negative", -100, "0"},
		{"small number", 500, "500"},
		{"exactly 1000", 1000, "1.0k"},
		{"thousands", 5500, "5.5k"},
		{"ten thousands", 15000, "15.0k"},
		{"hundred thousands", 150000, "150.0k"},
		{"exactly million", 1000000, "1.0m"},
		{"millions", 2500000, "2.5m"},
		{"large millions", 15000000, "15.0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatTokenCount(tt.count)
			if result != tt.expected {
				t.Errorf("FormatTokenCount(%d) = %q, want %q", tt.count, result, tt.expected)
			}
		})
	}
}

func TestFormatUSD(t *testing.T) {
	tests := []struct {
		name     string
		amount   float64
		expected string
	}{
		{"zero", 0.0, ""},
		{"negative", -1.0, ""},
		{"less than cent", 0.005, "$0.0050"},
		{"small cents", 0.01, "$0.01"},
		{"cents", 0.25, "$0.25"},
		{"one dollar", 1.0, "$1.00"},
		{"dollars and cents", 5.99, "$5.99"},
		{"large amount", 150.50, "$150.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatUSD(tt.amount)
			if result != tt.expected {
				t.Errorf("FormatUSD(%f) = %q, want %q", tt.amount, result, tt.expected)
			}
		})
	}
}

func TestBuildStatsLine(t *testing.T) {
	t.Run("basic stats", func(t *testing.T) {
		stats := &StatsLine{
			Runtime:    "5m30s",
			SessionKey: "session-123",
		}
		result := BuildStatsLine(stats)

		if !strings.Contains(result, "Stats:") {
			t.Error("should contain 'Stats:' prefix")
		}
		if !strings.Contains(result, "runtime 5m30s") {
			t.Error("should contain runtime")
		}
		if !strings.Contains(result, "sessionKey session-123") {
			t.Error("should contain sessionKey")
		}
		if !strings.Contains(result, "tokens n/a") {
			t.Error("should contain 'tokens n/a' when no tokens")
		}
	})

	t.Run("with tokens", func(t *testing.T) {
		stats := &StatsLine{
			Runtime:      "2m0s",
			InputTokens:  5000,
			OutputTokens: 1500,
			TotalTokens:  6500,
			SessionKey:   "session-456",
		}
		result := BuildStatsLine(stats)

		if !strings.Contains(result, "tokens 6.5k") {
			t.Errorf("should contain formatted total tokens, got: %s", result)
		}
		if !strings.Contains(result, "in 5.0k") {
			t.Errorf("should contain formatted input tokens, got: %s", result)
		}
		if !strings.Contains(result, "out 1.5k") {
			t.Errorf("should contain formatted output tokens, got: %s", result)
		}
	})

	t.Run("with cost", func(t *testing.T) {
		stats := &StatsLine{
			Runtime:    "1m0s",
			Cost:       0.15,
			SessionKey: "session-789",
		}
		result := BuildStatsLine(stats)

		if !strings.Contains(result, "est $0.15") {
			t.Errorf("should contain formatted cost, got: %s", result)
		}
	})

	t.Run("with session ID", func(t *testing.T) {
		stats := &StatsLine{
			Runtime:    "30s",
			SessionKey: "session-abc",
			SessionID:  "sid-12345",
		}
		result := BuildStatsLine(stats)

		if !strings.Contains(result, "sessionId sid-12345") {
			t.Error("should contain sessionId")
		}
	})

	t.Run("with transcript path", func(t *testing.T) {
		stats := &StatsLine{
			Runtime:        "1m30s",
			SessionKey:     "session-xyz",
			TranscriptPath: "/var/log/transcripts/abc.json",
		}
		result := BuildStatsLine(stats)

		if !strings.Contains(result, "transcript /var/log/transcripts/abc.json") {
			t.Error("should contain transcript path")
		}
	})

	t.Run("full stats", func(t *testing.T) {
		stats := &StatsLine{
			Runtime:        "10m5s",
			InputTokens:   25000,
			OutputTokens:  8000,
			TotalTokens:   33000,
			Cost:          1.25,
			SessionKey:    "session-full",
			SessionID:     "sid-full",
			TranscriptPath: "/path/to/transcript",
		}
		result := BuildStatsLine(stats)

		// Check all parts are present and separated by bullet
		if !strings.Contains(result, "\u2022") {
			t.Error("should contain bullet separator")
		}
		if !strings.Contains(result, "runtime 10m5s") {
			t.Error("should contain runtime")
		}
		if !strings.Contains(result, "tokens 33.0k") {
			t.Error("should contain total tokens")
		}
		if !strings.Contains(result, "est $1.25") {
			t.Error("should contain cost")
		}
	})
}

func TestBuildSubagentSystemPrompt(t *testing.T) {
	t.Run("minimal params", func(t *testing.T) {
		params := SubagentSystemPromptParams{
			ChildSessionKey: "child-session",
		}
		result := BuildSubagentSystemPrompt(params)

		if !strings.Contains(result, "# Subagent Context") {
			t.Error("should contain header")
		}
		if !strings.Contains(result, "**subagent**") {
			t.Error("should explain it's a subagent")
		}
		if !strings.Contains(result, "{{TASK_DESCRIPTION}}") {
			t.Error("should have placeholder when no task")
		}
		if !strings.Contains(result, "Your session: child-session") {
			t.Error("should contain child session key")
		}
	})

	t.Run("with task", func(t *testing.T) {
		params := SubagentSystemPromptParams{
			ChildSessionKey: "child-session",
			Task:            "Research the topic X",
		}
		result := BuildSubagentSystemPrompt(params)

		if !strings.Contains(result, "Research the topic X") {
			t.Error("should contain task description")
		}
		if strings.Contains(result, "{{TASK_DESCRIPTION}}") {
			t.Error("should not have placeholder when task provided")
		}
	})

	t.Run("with label", func(t *testing.T) {
		params := SubagentSystemPromptParams{
			ChildSessionKey: "child-session",
			Label:           "researcher",
		}
		result := BuildSubagentSystemPrompt(params)

		if !strings.Contains(result, "Label: researcher") {
			t.Error("should contain label")
		}
	})

	t.Run("with requester session key", func(t *testing.T) {
		params := SubagentSystemPromptParams{
			ChildSessionKey:     "child-session",
			RequesterSessionKey: "parent-session",
		}
		result := BuildSubagentSystemPrompt(params)

		if !strings.Contains(result, "Requester session: parent-session") {
			t.Error("should contain requester session key")
		}
	})

	t.Run("with origin channel", func(t *testing.T) {
		params := SubagentSystemPromptParams{
			ChildSessionKey: "child-session",
			RequesterOrigin: &DeliveryContext{
				Channel: "slack",
			},
		}
		result := BuildSubagentSystemPrompt(params)

		if !strings.Contains(result, "Requester channel: slack") {
			t.Error("should contain origin channel")
		}
	})

	t.Run("rules section exists", func(t *testing.T) {
		params := SubagentSystemPromptParams{
			ChildSessionKey: "child-session",
		}
		result := BuildSubagentSystemPrompt(params)

		if !strings.Contains(result, "## Rules") {
			t.Error("should contain rules section")
		}
		if !strings.Contains(result, "Stay focused") {
			t.Error("should contain stay focused rule")
		}
		if !strings.Contains(result, "Be ephemeral") {
			t.Error("should contain ephemeral rule")
		}
	})

	t.Run("what you don't do section exists", func(t *testing.T) {
		params := SubagentSystemPromptParams{
			ChildSessionKey: "child-session",
		}
		result := BuildSubagentSystemPrompt(params)

		if !strings.Contains(result, "## What You DON'T Do") {
			t.Error("should contain don't do section")
		}
		if !strings.Contains(result, "NO user conversations") {
			t.Error("should mention no user conversations")
		}
		if !strings.Contains(result, "NO using the `message` tool directly") {
			t.Error("should mention no message tool")
		}
	})
}

func TestBuildTriggerMessage(t *testing.T) {
	t.Run("ok outcome with label", func(t *testing.T) {
		params := TriggerMessageParams{
			Label:     "research task",
			Outcome:   &SubagentRunOutcome{Status: "ok"},
			Reply:     "Found 5 relevant articles.",
			StatsLine: "Stats: runtime 2m30s",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, `"research task"`) {
			t.Error("should contain label in quotes")
		}
		if !strings.Contains(result, "completed successfully") {
			t.Error("should indicate success")
		}
		if !strings.Contains(result, "Found 5 relevant articles") {
			t.Error("should contain reply")
		}
		if !strings.Contains(result, "Stats: runtime 2m30s") {
			t.Error("should contain stats line")
		}
	})

	t.Run("ok outcome with task fallback", func(t *testing.T) {
		params := TriggerMessageParams{
			Task:      "analyze data",
			Outcome:   &SubagentRunOutcome{Status: "ok"},
			Reply:     "Analysis complete.",
			StatsLine: "Stats: test",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, `"analyze data"`) {
			t.Error("should fall back to task when no label")
		}
	})

	t.Run("ok outcome with default label", func(t *testing.T) {
		params := TriggerMessageParams{
			Outcome:   &SubagentRunOutcome{Status: "ok"},
			Reply:     "Done.",
			StatsLine: "Stats: test",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, `"background task"`) {
			t.Error("should use default label when neither label nor task provided")
		}
	})

	t.Run("timeout outcome", func(t *testing.T) {
		params := TriggerMessageParams{
			Label:     "slow task",
			Outcome:   &SubagentRunOutcome{Status: "timeout"},
			Reply:     "",
			StatsLine: "Stats: test",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, "timed out") {
			t.Error("should indicate timeout")
		}
		if !strings.Contains(result, "(no output)") {
			t.Error("should have no output placeholder when reply empty")
		}
	})

	t.Run("error outcome with error message", func(t *testing.T) {
		params := TriggerMessageParams{
			Label:     "failing task",
			Outcome:   &SubagentRunOutcome{Status: "error", Error: "connection refused"},
			Reply:     "Partial results...",
			StatsLine: "Stats: test",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, "failed: connection refused") {
			t.Error("should contain error message")
		}
	})

	t.Run("error outcome without error message", func(t *testing.T) {
		params := TriggerMessageParams{
			Label:     "failing task",
			Outcome:   &SubagentRunOutcome{Status: "error"},
			Reply:     "",
			StatsLine: "Stats: test",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, "failed: unknown error") {
			t.Error("should have unknown error when no error message")
		}
	})

	t.Run("unknown outcome", func(t *testing.T) {
		params := TriggerMessageParams{
			Label:     "mystery task",
			Outcome:   &SubagentRunOutcome{Status: "unknown"},
			Reply:     "???",
			StatsLine: "Stats: test",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, "finished with unknown status") {
			t.Error("should indicate unknown status")
		}
	})

	t.Run("contains instructions for summarization", func(t *testing.T) {
		params := TriggerMessageParams{
			Label:     "test",
			Outcome:   &SubagentRunOutcome{Status: "ok"},
			Reply:     "Done",
			StatsLine: "Stats: test",
		}
		result := BuildTriggerMessage(params)

		if !strings.Contains(result, "Summarize this naturally") {
			t.Error("should contain summarization instruction")
		}
		if !strings.Contains(result, "Keep it brief") {
			t.Error("should mention keeping it brief")
		}
		if !strings.Contains(result, "NO_REPLY") {
			t.Error("should mention NO_REPLY option")
		}
		if !strings.Contains(result, "Do not mention technical details") {
			t.Error("should instruct not to mention technical details")
		}
	})
}

func TestSubagentRunOutcome(t *testing.T) {
	t.Run("can create outcome with all fields", func(t *testing.T) {
		outcome := &SubagentRunOutcome{
			Status: "error",
			Error:  "something went wrong",
		}
		if outcome.Status != "error" {
			t.Errorf("Status = %q, want %q", outcome.Status, "error")
		}
		if outcome.Error != "something went wrong" {
			t.Errorf("Error = %q, want %q", outcome.Error, "something went wrong")
		}
	})
}

func TestDeliveryContext(t *testing.T) {
	t.Run("can create context with all fields", func(t *testing.T) {
		ctx := &DeliveryContext{
			Channel:   "slack",
			AccountID: "acc-123",
			To:        "user@example.com",
			ThreadID:  "thread-456",
		}
		if ctx.Channel != "slack" {
			t.Errorf("Channel = %q, want %q", ctx.Channel, "slack")
		}
		if ctx.AccountID != "acc-123" {
			t.Errorf("AccountID = %q, want %q", ctx.AccountID, "acc-123")
		}
		if ctx.To != "user@example.com" {
			t.Errorf("To = %q, want %q", ctx.To, "user@example.com")
		}
		if ctx.ThreadID != "thread-456" {
			t.Errorf("ThreadID = %q, want %q", ctx.ThreadID, "thread-456")
		}
	})
}

func TestAnnounceParams(t *testing.T) {
	t.Run("can create params with all fields", func(t *testing.T) {
		now := time.Now()
		params := &AnnounceParams{
			ChildSessionKey:     "child-key",
			ChildRunID:          "run-123",
			RequesterSessionKey: "parent-key",
			RequesterOrigin:     &DeliveryContext{Channel: "email"},
			RequesterDisplayKey: "display-key",
			Task:                "do something",
			TimeoutMs:           30000,
			Cleanup:             "delete",
			RoundOneReply:       "initial reply",
			WaitForCompletion:   true,
			StartedAt:           now,
			EndedAt:             now.Add(5 * time.Minute),
			Label:               "my task",
			Outcome:             &SubagentRunOutcome{Status: "ok"},
		}

		if params.ChildSessionKey != "child-key" {
			t.Error("ChildSessionKey not set correctly")
		}
		if params.WaitForCompletion != true {
			t.Error("WaitForCompletion not set correctly")
		}
		if params.Cleanup != "delete" {
			t.Error("Cleanup not set correctly")
		}
	})
}

func TestAnnounceResult(t *testing.T) {
	t.Run("can create result", func(t *testing.T) {
		result := &AnnounceResult{
			Announced: true,
			Outcome:   &SubagentRunOutcome{Status: "ok"},
		}
		if !result.Announced {
			t.Error("Announced should be true")
		}
		if result.Outcome.Status != "ok" {
			t.Error("Outcome status should be ok")
		}
	})
}

func TestStatsLine(t *testing.T) {
	t.Run("can create stats line struct", func(t *testing.T) {
		stats := &StatsLine{
			Runtime:        "5m0s",
			InputTokens:    10000,
			OutputTokens:   3000,
			TotalTokens:    13000,
			Cost:           0.50,
			SessionKey:     "sess-key",
			SessionID:      "sess-id",
			TranscriptPath: "/path/to/file",
		}
		if stats.Runtime != "5m0s" {
			t.Error("Runtime not set correctly")
		}
		if stats.TotalTokens != 13000 {
			t.Error("TotalTokens not set correctly")
		}
	})
}
