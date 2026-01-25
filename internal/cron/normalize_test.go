package cron

import (
	"strings"
	"testing"
	"time"
)

func parseRelativeTime(raw string) (int64, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if !strings.HasPrefix(raw, "in ") {
		return 0, false
	}

	durationStr := strings.TrimPrefix(raw, "in ")
	d, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, false
	}

	return time.Now().Add(d).UnixMilli(), true
}

func TestParseAbsoluteTimeMs(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   int64
		wantOk bool
	}{
		{
			name:   "empty string",
			input:  "",
			want:   0,
			wantOk: false,
		},
		{
			name:   "whitespace only",
			input:  "   ",
			want:   0,
			wantOk: false,
		},
		{
			name:   "unix timestamp in seconds",
			input:  "1704067200",
			want:   1704067200000,
			wantOk: true,
		},
		{
			name:   "unix timestamp in milliseconds",
			input:  "1704067200000",
			want:   1704067200000,
			wantOk: true,
		},
		{
			name:   "ISO8601 with Z",
			input:  "2024-01-01T00:00:00Z",
			want:   1704067200000,
			wantOk: true,
		},
		{
			name:   "ISO8601 without timezone (assumes UTC)",
			input:  "2024-01-01T00:00:00",
			want:   1704067200000,
			wantOk: true,
		},
		{
			name:   "ISO8601 date only",
			input:  "2024-01-01",
			want:   1704067200000,
			wantOk: true,
		},
		{
			name:   "ISO8601 with positive offset",
			input:  "2024-01-01T01:00:00+01:00",
			want:   1704067200000,
			wantOk: true,
		},
		{
			name:   "invalid format",
			input:  "not-a-date",
			want:   0,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseAbsoluteTimeMs(tt.input)
			if ok != tt.wantOk {
				t.Errorf("ParseAbsoluteTimeMs(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Errorf("ParseAbsoluteTimeMs(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRelativeTime(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOk bool
	}{
		{
			name:   "valid relative time",
			input:  "in 5m",
			wantOk: true,
		},
		{
			name:   "valid relative time with hours",
			input:  "in 1h30m",
			wantOk: true,
		},
		{
			name:   "uppercase",
			input:  "IN 5M",
			wantOk: true,
		},
		{
			name:   "no prefix",
			input:  "5m",
			wantOk: false,
		},
		{
			name:   "invalid duration",
			input:  "in abc",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			got, ok := parseRelativeTime(tt.input)
			if ok != tt.wantOk {
				t.Errorf("parseRelativeTime(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
			}
			if ok {
				// Check that the time is in the future
				if got <= now.UnixMilli() {
					t.Errorf("parseRelativeTime(%q) should return future time", tt.input)
				}
			}
		})
	}
}

func TestNormalizeCronJobCreate(t *testing.T) {
	t.Run("basic job creation", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "test-job",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"sessionTarget": "main",
			"wakeMode":      "next-heartbeat",
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "hello",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Name != "test-job" {
			t.Errorf("Name = %q, want %q", result.Name, "test-job")
		}
		if !result.Enabled {
			t.Error("Enabled should be true")
		}
		if result.Schedule == nil {
			t.Fatal("Schedule should not be nil")
		}
		if result.Schedule.Kind != ScheduleCron {
			t.Errorf("Schedule.Kind = %q, want %q", result.Schedule.Kind, ScheduleCron)
		}
		if result.Schedule.Expr != "* * * * *" {
			t.Errorf("Schedule.Expr = %q, want %q", result.Schedule.Expr, "* * * * *")
		}
	})

	t.Run("maps legacy payload.provider to payload.channel", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "legacy",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"sessionTarget": "isolated",
			"wakeMode":      "immediate",
			"payload": map[string]interface{}{
				"kind":     "agentTurn",
				"message":  "hi",
				"deliver":  true,
				"provider": " TeLeGrAm ",
				"to":       "7200373102",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Payload == nil {
			t.Fatal("Payload should not be nil")
		}
		if result.Payload.Channel != "telegram" {
			t.Errorf("Payload.Channel = %q, want %q", result.Payload.Channel, "telegram")
		}
	})

	t.Run("normalizes agentId and handles null", func(t *testing.T) {
		// Test with agent ID set
		raw := map[string]interface{}{
			"name":    "agent-set",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"agentId":       " Ops ",
			"sessionTarget": "isolated",
			"wakeMode":      "immediate",
			"payload": map[string]interface{}{
				"kind":    "agentTurn",
				"message": "hi",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.AgentID == nil {
			t.Fatal("AgentID should not be nil")
		}
		if *result.AgentID != "Ops" {
			t.Errorf("AgentID = %q, want %q", *result.AgentID, "Ops")
		}

		// Test with agent ID explicitly null
		rawNull := map[string]interface{}{
			"name":    "agent-clear",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"agentId":       nil,
			"sessionTarget": "isolated",
			"wakeMode":      "immediate",
			"payload": map[string]interface{}{
				"kind":    "agentTurn",
				"message": "hi",
			},
		}

		resultNull := NormalizeCronJobCreate(rawNull)

		if resultNull.AgentID != nil {
			t.Error("AgentID should be nil when explicitly set to null")
		}
	})

	t.Run("canonicalizes payload.channel casing", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "legacy provider",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"sessionTarget": "isolated",
			"wakeMode":      "immediate",
			"payload": map[string]interface{}{
				"kind":    "agentTurn",
				"message": "hi",
				"deliver": true,
				"channel": "Telegram",
				"to":      "7200373102",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.Payload.Channel != "telegram" {
			t.Errorf("Payload.Channel = %q, want %q", result.Payload.Channel, "telegram")
		}
	})

	t.Run("coerces ISO schedule.at to atMs (UTC)", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":          "iso at",
			"enabled":       true,
			"schedule":      map[string]interface{}{"at": "2026-01-12T18:00:00"},
			"sessionTarget": "main",
			"wakeMode":      "next-heartbeat",
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "hi",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.Schedule == nil {
			t.Fatal("Schedule should not be nil")
		}
		if result.Schedule.Kind != ScheduleAt {
			t.Errorf("Schedule.Kind = %q, want %q", result.Schedule.Kind, ScheduleAt)
		}
		expected, _ := time.Parse(time.RFC3339, "2026-01-12T18:00:00Z")
		if result.Schedule.AtMs != expected.UnixMilli() {
			t.Errorf("Schedule.AtMs = %d, want %d", result.Schedule.AtMs, expected.UnixMilli())
		}
	})

	t.Run("coerces ISO schedule.atMs string to atMs (UTC)", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "iso atMs",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "at",
				"atMs": "2026-01-12T18:00:00",
			},
			"sessionTarget": "main",
			"wakeMode":      "next-heartbeat",
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "hi",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.Schedule.Kind != ScheduleAt {
			t.Errorf("Schedule.Kind = %q, want %q", result.Schedule.Kind, ScheduleAt)
		}
		expected, _ := time.Parse(time.RFC3339, "2026-01-12T18:00:00Z")
		if result.Schedule.AtMs != expected.UnixMilli() {
			t.Errorf("Schedule.AtMs = %d, want %d", result.Schedule.AtMs, expected.UnixMilli())
		}
	})

	t.Run("applies defaults when ApplyDefaults is true", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "defaults-test",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "hi",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.WakeMode != WakeNextHeartbeat {
			t.Errorf("WakeMode = %q, want %q", result.WakeMode, WakeNextHeartbeat)
		}
		if result.SessionTarget != SessionMain {
			t.Errorf("SessionTarget = %q, want %q (for systemEvent)", result.SessionTarget, SessionMain)
		}
	})

	t.Run("applies isolated session target for agentTurn", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "agent-turn-test",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"payload": map[string]interface{}{
				"kind":    "agentTurn",
				"message": "hi",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.SessionTarget != SessionIsolated {
			t.Errorf("SessionTarget = %q, want %q (for agentTurn)", result.SessionTarget, SessionIsolated)
		}
	})

	t.Run("handles everyMs schedule", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "every-test",
			"enabled": true,
			"schedule": map[string]interface{}{
				"everyMs": float64(60000),
			},
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "hi",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.Schedule.Kind != ScheduleEvery {
			t.Errorf("Schedule.Kind = %q, want %q", result.Schedule.Kind, ScheduleEvery)
		}
		if result.Schedule.EveryMs != 60000 {
			t.Errorf("Schedule.EveryMs = %d, want %d", result.Schedule.EveryMs, 60000)
		}
	})

	t.Run("unwraps job from data field", func(t *testing.T) {
		raw := map[string]interface{}{
			"data": map[string]interface{}{
				"name":    "wrapped-job",
				"enabled": true,
				"schedule": map[string]interface{}{
					"kind": "cron",
					"expr": "* * * * *",
				},
				"payload": map[string]interface{}{
					"kind": "systemEvent",
					"text": "hi",
				},
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.Name != "wrapped-job" {
			t.Errorf("Name = %q, want %q", result.Name, "wrapped-job")
		}
	})

	t.Run("unwraps job from job field", func(t *testing.T) {
		raw := map[string]interface{}{
			"job": map[string]interface{}{
				"name":    "job-wrapped",
				"enabled": true,
				"schedule": map[string]interface{}{
					"kind": "cron",
					"expr": "* * * * *",
				},
				"payload": map[string]interface{}{
					"kind": "systemEvent",
					"text": "hi",
				},
			},
		}

		result := NormalizeCronJobCreate(raw)

		if result.Name != "job-wrapped" {
			t.Errorf("Name = %q, want %q", result.Name, "job-wrapped")
		}
	})

	t.Run("handles string enabled value", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "string-enabled",
			"enabled": "true",
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "hi",
			},
		}

		result := NormalizeCronJobCreate(raw)

		if !result.Enabled {
			t.Error("Enabled should be true when string 'true' is provided")
		}

		raw["enabled"] = "false"
		result = NormalizeCronJobCreate(raw)

		if result.Enabled {
			t.Error("Enabled should be false when string 'false' is provided")
		}
	})
}

func TestNormalizeCronJobPatch(t *testing.T) {
	t.Run("partial update with enabled only", func(t *testing.T) {
		raw := map[string]interface{}{
			"enabled": false,
		}

		result := NormalizeCronJobPatch(raw)

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Enabled == nil {
			t.Fatal("Enabled should not be nil")
		}
		if *result.Enabled != false {
			t.Error("Enabled should be false")
		}
	})

	t.Run("does not apply defaults", func(t *testing.T) {
		raw := map[string]interface{}{
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "updated",
			},
		}

		result := NormalizeCronJobPatch(raw)

		// WakeMode and SessionTarget should remain empty (not defaulted)
		if result.WakeMode != "" {
			t.Errorf("WakeMode should be empty in patch, got %q", result.WakeMode)
		}
		if result.SessionTarget != "" {
			t.Errorf("SessionTarget should be empty in patch, got %q", result.SessionTarget)
		}
	})

	t.Run("updates schedule", func(t *testing.T) {
		raw := map[string]interface{}{
			"schedule": map[string]interface{}{
				"kind":    "every",
				"everyMs": float64(30000),
			},
		}

		result := NormalizeCronJobPatch(raw)

		if result.Schedule == nil {
			t.Fatal("Schedule should not be nil")
		}
		if result.Schedule.Kind != ScheduleEvery {
			t.Errorf("Schedule.Kind = %q, want %q", result.Schedule.Kind, ScheduleEvery)
		}
		if result.Schedule.EveryMs != 30000 {
			t.Errorf("Schedule.EveryMs = %d, want %d", result.Schedule.EveryMs, 30000)
		}
	})

	t.Run("nil input returns nil", func(t *testing.T) {
		result := NormalizeCronJobPatch(nil)
		if result != nil {
			t.Error("expected nil result for nil input")
		}
	})
}

func TestCoerceSchedule(t *testing.T) {
	t.Run("infers kind from atMs", func(t *testing.T) {
		input := map[string]interface{}{
			"atMs": float64(1704067200000),
		}

		result := coerceSchedule(input)

		if result.Kind != ScheduleAt {
			t.Errorf("Kind = %q, want %q", result.Kind, ScheduleAt)
		}
		if result.AtMs != 1704067200000 {
			t.Errorf("AtMs = %d, want %d", result.AtMs, 1704067200000)
		}
	})

	t.Run("infers kind from everyMs", func(t *testing.T) {
		input := map[string]interface{}{
			"everyMs": float64(60000),
		}

		result := coerceSchedule(input)

		if result.Kind != ScheduleEvery {
			t.Errorf("Kind = %q, want %q", result.Kind, ScheduleEvery)
		}
	})

	t.Run("infers kind from expr", func(t *testing.T) {
		input := map[string]interface{}{
			"expr": "0 9 * * *",
		}

		result := coerceSchedule(input)

		if result.Kind != ScheduleCron {
			t.Errorf("Kind = %q, want %q", result.Kind, ScheduleCron)
		}
		if result.Expr != "0 9 * * *" {
			t.Errorf("Expr = %q, want %q", result.Expr, "0 9 * * *")
		}
	})

	t.Run("preserves explicit kind", func(t *testing.T) {
		input := map[string]interface{}{
			"kind":    "at",
			"atMs":    float64(1704067200000),
			"everyMs": float64(60000), // Should be ignored
		}

		result := coerceSchedule(input)

		if result.Kind != ScheduleAt {
			t.Errorf("Kind = %q, want %q", result.Kind, ScheduleAt)
		}
	})

	t.Run("handles int64 values", func(t *testing.T) {
		input := map[string]interface{}{
			"atMs": int64(1704067200000),
		}

		result := coerceSchedule(input)

		if result.AtMs != 1704067200000 {
			t.Errorf("AtMs = %d, want %d", result.AtMs, 1704067200000)
		}
	})

	t.Run("parses at string to atMs", func(t *testing.T) {
		input := map[string]interface{}{
			"at": "2024-01-01T00:00:00Z",
		}

		result := coerceSchedule(input)

		if result.Kind != ScheduleAt {
			t.Errorf("Kind = %q, want %q", result.Kind, ScheduleAt)
		}
		if result.AtMs != 1704067200000 {
			t.Errorf("AtMs = %d, want %d", result.AtMs, 1704067200000)
		}
	})

	t.Run("handles timezone", func(t *testing.T) {
		input := map[string]interface{}{
			"kind": "cron",
			"expr": "0 9 * * *",
			"tz":   "America/New_York",
		}

		result := coerceSchedule(input)

		if result.Tz != "America/New_York" {
			t.Errorf("Tz = %q, want %q", result.Tz, "America/New_York")
		}
	})
}

func TestCoercePayload(t *testing.T) {
	t.Run("normalizes channel to lowercase", func(t *testing.T) {
		input := map[string]interface{}{
			"kind":    "agentTurn",
			"message": "hi",
			"channel": "  TeLegRaM  ",
		}

		result := coercePayload(input)

		if result.Channel != "telegram" {
			t.Errorf("Channel = %q, want %q", result.Channel, "telegram")
		}
	})

	t.Run("migrates provider to channel", func(t *testing.T) {
		input := map[string]interface{}{
			"kind":     "agentTurn",
			"message":  "hi",
			"provider": "WhatsApp",
		}

		result := coercePayload(input)

		if result.Channel != "whatsapp" {
			t.Errorf("Channel = %q, want %q", result.Channel, "whatsapp")
		}
	})

	t.Run("prefers channel over provider", func(t *testing.T) {
		input := map[string]interface{}{
			"kind":     "agentTurn",
			"message":  "hi",
			"channel":  "telegram",
			"provider": "whatsapp",
		}

		result := coercePayload(input)

		if result.Channel != "telegram" {
			t.Errorf("Channel = %q, want %q", result.Channel, "telegram")
		}
	})

	t.Run("handles systemEvent payload", func(t *testing.T) {
		input := map[string]interface{}{
			"kind": "systemEvent",
			"text": "System notification",
		}

		result := coercePayload(input)

		if result.Kind != PayloadSystemEvent {
			t.Errorf("Kind = %q, want %q", result.Kind, PayloadSystemEvent)
		}
		if result.Text != "System notification" {
			t.Errorf("Text = %q, want %q", result.Text, "System notification")
		}
	})

	t.Run("handles webhook payload", func(t *testing.T) {
		input := map[string]interface{}{
			"kind": "webhook",
			"url":  "  https://example.com/hook  ",
		}

		result := coercePayload(input)

		if result.Kind != PayloadWebhook {
			t.Errorf("Kind = %q, want %q", result.Kind, PayloadWebhook)
		}
		if result.URL != "https://example.com/hook" {
			t.Errorf("URL = %q, want %q", result.URL, "https://example.com/hook")
		}
	})
}

func TestNormalizeCronJobInput(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := NormalizeCronJobInput(nil, nil)
		if result != nil {
			t.Error("expected nil result for nil input")
		}
	})

	t.Run("nil options uses defaults", func(t *testing.T) {
		raw := map[string]interface{}{
			"name":    "test",
			"enabled": true,
			"schedule": map[string]interface{}{
				"kind": "cron",
				"expr": "* * * * *",
			},
			"payload": map[string]interface{}{
				"kind": "systemEvent",
				"text": "hi",
			},
		}

		result := NormalizeCronJobInput(raw, nil)

		// With nil options (ApplyDefaults=false), WakeMode should be empty
		if result.WakeMode != "" {
			t.Errorf("WakeMode should be empty without ApplyDefaults, got %q", result.WakeMode)
		}
	})
}
