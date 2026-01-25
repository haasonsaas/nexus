package cron

import (
	"strconv"
	"strings"
	"time"
)

// ScheduleKind represents the type of schedule
type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"    // One-time at specific time
	ScheduleEvery ScheduleKind = "every" // Recurring interval
	ScheduleCron  ScheduleKind = "cron"  // Cron expression
)

// NormalizedSchedule represents a normalized job schedule
type NormalizedSchedule struct {
	Kind    ScheduleKind `json:"kind"`
	AtMs    int64        `json:"atMs,omitempty"`    // For kind=at
	EveryMs int64        `json:"everyMs,omitempty"` // For kind=every
	Expr    string       `json:"expr,omitempty"`    // For kind=cron
	Tz      string       `json:"tz,omitempty"`      // Timezone for cron
}

// PayloadKind represents the type of job payload
type PayloadKind string

const (
	PayloadSystemEvent PayloadKind = "systemEvent"
	PayloadAgentTurn   PayloadKind = "agentTurn"
	PayloadWebhook     PayloadKind = "webhook"
)

// Payload represents the job payload
type Payload struct {
	Kind    PayloadKind `json:"kind"`
	Channel string      `json:"channel,omitempty"`
	To      string      `json:"to,omitempty"`
	Text    string      `json:"text,omitempty"`
	Message string      `json:"message,omitempty"`
	URL     string      `json:"url,omitempty"`
}

// WakeMode determines how the agent is woken
type WakeMode string

const (
	WakeNextHeartbeat WakeMode = "next-heartbeat"
	WakeImmediate     WakeMode = "immediate"
)

// SessionTarget determines which session to use
type SessionTarget string

const (
	SessionMain     SessionTarget = "main"
	SessionIsolated SessionTarget = "isolated"
)

// CronJobCreate for creating new jobs
type CronJobCreate struct {
	ID            string              `json:"id,omitempty"`
	AgentID       *string             `json:"agentId,omitempty"`
	Name          string              `json:"name,omitempty"`
	Enabled       bool                `json:"enabled"`
	Schedule      *NormalizedSchedule `json:"schedule"`
	Payload       *Payload            `json:"payload"`
	WakeMode      WakeMode            `json:"wakeMode,omitempty"`
	SessionTarget SessionTarget       `json:"sessionTarget,omitempty"`
	Label         string              `json:"label,omitempty"`
}

// CronJobPatch for updating existing jobs
type CronJobPatch struct {
	Enabled       *bool               `json:"enabled,omitempty"`
	Schedule      *NormalizedSchedule `json:"schedule,omitempty"`
	Payload       *Payload            `json:"payload,omitempty"`
	WakeMode      WakeMode            `json:"wakeMode,omitempty"`
	SessionTarget SessionTarget       `json:"sessionTarget,omitempty"`
	Label         string              `json:"label,omitempty"`
}

// NormalizeOptions for controlling normalization behavior
type NormalizeOptions struct {
	ApplyDefaults bool
}

// hasTzSuffix checks if a string has a timezone suffix (Z or +/-HH:MM)
func hasTzSuffix(s string) bool {
	if strings.HasSuffix(s, "Z") {
		return true
	}
	// Check for offset like +01:00 or -05:00
	if len(s) >= 6 {
		suffix := s[len(s)-6:]
		if (suffix[0] == '+' || suffix[0] == '-') &&
			suffix[1] >= '0' && suffix[1] <= '9' &&
			suffix[2] >= '0' && suffix[2] <= '9' &&
			suffix[3] == ':' &&
			suffix[4] >= '0' && suffix[4] <= '9' &&
			suffix[5] >= '0' && suffix[5] <= '9' {
			return true
		}
	}
	// Check for offset without colon like +0100 or -0500
	if len(s) >= 5 {
		suffix := s[len(s)-5:]
		if (suffix[0] == '+' || suffix[0] == '-') &&
			suffix[1] >= '0' && suffix[1] <= '9' &&
			suffix[2] >= '0' && suffix[2] <= '9' &&
			suffix[3] >= '0' && suffix[3] <= '9' &&
			suffix[4] >= '0' && suffix[4] <= '9' {
			return true
		}
	}
	return false
}

// isISODate checks if a string is a date-only format (YYYY-MM-DD)
func isISODate(s string) bool {
	return len(s) == 10 && s[4] == '-' && s[7] == '-'
}

// isISODateTime checks if a string starts with a datetime format (YYYY-MM-DDTHH...)
func isISODateTime(s string) bool {
	return len(s) > 10 && s[4] == '-' && s[7] == '-' && s[10] == 'T'
}

// normalizeUtcIso ensures an ISO string has a timezone suffix
func normalizeUtcIso(raw string) string {
	if hasTzSuffix(raw) {
		return raw
	}
	if isISODate(raw) {
		return raw + "T00:00:00Z"
	}
	if isISODateTime(raw) {
		return raw + "Z"
	}
	return raw
}

// ParseAbsoluteTimeMs parses time strings to milliseconds
// Supports: ISO8601, Unix timestamps (seconds or milliseconds)
func ParseAbsoluteTimeMs(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	// Try numeric (Unix timestamp)
	if isNumeric(raw) {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return 0, false
		}
		// If it looks like milliseconds (> 1 trillion), use as-is
		// Otherwise assume seconds and convert
		if n > 1e12 {
			return n, true
		}
		return n * 1000, true
	}

	// Try ISO parsing
	normalized := normalizeUtcIso(raw)
	parsed, err := time.Parse(time.RFC3339, normalized)
	if err != nil {
		// Try RFC3339Nano
		parsed, err = time.Parse(time.RFC3339Nano, normalized)
		if err != nil {
			return 0, false
		}
	}
	return parsed.UnixMilli(), true
}

// isNumeric checks if a string contains only digits
func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// coerceSchedule normalizes schedule input
func coerceSchedule(input map[string]interface{}) *NormalizedSchedule {
	schedule := &NormalizedSchedule{}

	// Determine kind
	if kind, ok := input["kind"].(string); ok {
		schedule.Kind = ScheduleKind(kind)
	}

	// Handle atMs / at
	if atMs, ok := input["atMs"].(float64); ok {
		schedule.AtMs = int64(atMs)
		if schedule.Kind == "" {
			schedule.Kind = ScheduleAt
		}
	} else if atMs, ok := input["atMs"].(int64); ok {
		schedule.AtMs = atMs
		if schedule.Kind == "" {
			schedule.Kind = ScheduleAt
		}
	} else if at, ok := input["at"].(string); ok {
		if ms, parsed := ParseAbsoluteTimeMs(at); parsed {
			schedule.AtMs = ms
			if schedule.Kind == "" {
				schedule.Kind = ScheduleAt
			}
		}
	} else if atMsStr, ok := input["atMs"].(string); ok {
		if ms, parsed := ParseAbsoluteTimeMs(atMsStr); parsed {
			schedule.AtMs = ms
			if schedule.Kind == "" {
				schedule.Kind = ScheduleAt
			}
		}
	}

	// Handle everyMs
	if everyMs, ok := input["everyMs"].(float64); ok {
		schedule.EveryMs = int64(everyMs)
		if schedule.Kind == "" {
			schedule.Kind = ScheduleEvery
		}
	} else if everyMs, ok := input["everyMs"].(int64); ok {
		schedule.EveryMs = everyMs
		if schedule.Kind == "" {
			schedule.Kind = ScheduleEvery
		}
	}

	// Handle expr
	if expr, ok := input["expr"].(string); ok {
		schedule.Expr = expr
		if schedule.Kind == "" {
			schedule.Kind = ScheduleCron
		}
	}

	// Handle tz
	if tz, ok := input["tz"].(string); ok {
		schedule.Tz = strings.TrimSpace(tz)
	}

	return schedule
}

// normalizeChannel converts a channel string to lowercase and trims whitespace
func normalizeChannel(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// coercePayload normalizes payload input
func coercePayload(input map[string]interface{}) *Payload {
	payload := &Payload{}

	// Determine kind
	if kind, ok := input["kind"].(string); ok {
		payload.Kind = PayloadKind(kind)
	}

	// Handle channel with legacy provider migration
	if channel, ok := input["channel"].(string); ok && strings.TrimSpace(channel) != "" {
		payload.Channel = normalizeChannel(channel)
	} else if provider, ok := input["provider"].(string); ok && strings.TrimSpace(provider) != "" {
		// Back-compat: older configs used `provider` for delivery channel
		payload.Channel = normalizeChannel(provider)
	}

	// Handle to
	if to, ok := input["to"].(string); ok {
		payload.To = strings.TrimSpace(to)
	}

	// Handle text (for systemEvent)
	if text, ok := input["text"].(string); ok {
		payload.Text = text
	}

	// Handle message (for agentTurn)
	if message, ok := input["message"].(string); ok {
		payload.Message = message
	}

	// Handle URL (for webhook)
	if url, ok := input["url"].(string); ok {
		payload.URL = strings.TrimSpace(url)
	}

	return payload
}

// unwrapJob extracts the job data from a potentially wrapped input
func unwrapJob(raw map[string]interface{}) map[string]interface{} {
	if data, ok := raw["data"].(map[string]interface{}); ok {
		return data
	}
	if job, ok := raw["job"].(map[string]interface{}); ok {
		return job
	}
	return raw
}

// normalizeAgentID normalizes an agent ID string
func normalizeAgentID(agentID string) string {
	// Preserve case but trim whitespace (like clawdbot's normalizeAgentId)
	return strings.TrimSpace(agentID)
}

// NormalizeCronJobInput normalizes raw job input
func NormalizeCronJobInput(raw map[string]interface{}, opts *NormalizeOptions) *CronJobCreate {
	if raw == nil {
		return nil
	}
	if opts == nil {
		opts = &NormalizeOptions{ApplyDefaults: false}
	}

	base := unwrapJob(raw)
	result := &CronJobCreate{
		Enabled: true, // Default to enabled
	}

	// Handle ID
	if id, ok := base["id"].(string); ok {
		result.ID = strings.TrimSpace(id)
	}

	// Handle name
	if name, ok := base["name"].(string); ok {
		result.Name = strings.TrimSpace(name)
	}

	// Handle label
	if label, ok := base["label"].(string); ok {
		result.Label = strings.TrimSpace(label)
	}

	// Handle agentId with null support
	if agentIDRaw, exists := base["agentId"]; exists {
		if agentIDRaw == nil {
			// Explicitly null - preserve as nil pointer to signal clearing
			result.AgentID = nil
		} else if agentID, ok := agentIDRaw.(string); ok {
			trimmed := strings.TrimSpace(agentID)
			if trimmed != "" {
				normalized := normalizeAgentID(trimmed)
				result.AgentID = &normalized
			}
		}
	}

	// Handle enabled
	if enabled, ok := base["enabled"].(bool); ok {
		result.Enabled = enabled
	} else if enabledStr, ok := base["enabled"].(string); ok {
		trimmed := strings.ToLower(strings.TrimSpace(enabledStr))
		if trimmed == "true" {
			result.Enabled = true
		} else if trimmed == "false" {
			result.Enabled = false
		}
	}

	// Handle schedule
	if scheduleRaw, ok := base["schedule"].(map[string]interface{}); ok {
		result.Schedule = coerceSchedule(scheduleRaw)
	}

	// Handle payload
	if payloadRaw, ok := base["payload"].(map[string]interface{}); ok {
		result.Payload = coercePayload(payloadRaw)
	}

	// Handle wakeMode
	if wakeMode, ok := base["wakeMode"].(string); ok {
		result.WakeMode = WakeMode(strings.TrimSpace(wakeMode))
	}

	// Handle sessionTarget
	if sessionTarget, ok := base["sessionTarget"].(string); ok {
		result.SessionTarget = SessionTarget(strings.TrimSpace(sessionTarget))
	}

	// Apply defaults if requested
	if opts.ApplyDefaults {
		if result.WakeMode == "" {
			result.WakeMode = WakeNextHeartbeat
		}
		if result.SessionTarget == "" && result.Payload != nil {
			switch result.Payload.Kind {
			case PayloadSystemEvent:
				result.SessionTarget = SessionMain
			case PayloadAgentTurn:
				result.SessionTarget = SessionIsolated
			}
		}
	}

	return result
}

// NormalizeCronJobCreate normalizes for creation (with defaults)
func NormalizeCronJobCreate(raw map[string]interface{}) *CronJobCreate {
	return NormalizeCronJobInput(raw, &NormalizeOptions{ApplyDefaults: true})
}

// NormalizeCronJobPatch normalizes for patching (without defaults)
func NormalizeCronJobPatch(raw map[string]interface{}) *CronJobPatch {
	if raw == nil {
		return nil
	}

	base := unwrapJob(raw)
	result := &CronJobPatch{}

	// Handle enabled
	if enabled, ok := base["enabled"].(bool); ok {
		result.Enabled = &enabled
	} else if enabledStr, ok := base["enabled"].(string); ok {
		trimmed := strings.ToLower(strings.TrimSpace(enabledStr))
		if trimmed == "true" {
			enabled := true
			result.Enabled = &enabled
		} else if trimmed == "false" {
			enabled := false
			result.Enabled = &enabled
		}
	}

	// Handle schedule
	if scheduleRaw, ok := base["schedule"].(map[string]interface{}); ok {
		result.Schedule = coerceSchedule(scheduleRaw)
	}

	// Handle payload
	if payloadRaw, ok := base["payload"].(map[string]interface{}); ok {
		result.Payload = coercePayload(payloadRaw)
	}

	// Handle wakeMode
	if wakeMode, ok := base["wakeMode"].(string); ok {
		result.WakeMode = WakeMode(strings.TrimSpace(wakeMode))
	}

	// Handle sessionTarget
	if sessionTarget, ok := base["sessionTarget"].(string); ok {
		result.SessionTarget = SessionTarget(strings.TrimSpace(sessionTarget))
	}

	// Handle label
	if label, ok := base["label"].(string); ok {
		result.Label = strings.TrimSpace(label)
	}

	return result
}
