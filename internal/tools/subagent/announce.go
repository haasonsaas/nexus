package subagent

import (
	"fmt"
	"strings"
	"time"
)

// SubagentRunOutcome represents the result of a subagent run
type SubagentRunOutcome struct {
	Status string // "ok", "error", "timeout", "unknown"
	Error  string
}

// DeliveryContext for message delivery
type DeliveryContext struct {
	Channel   string
	AccountID string
	To        string
	ThreadID  string
}

// AnnounceParams for running the announce flow
type AnnounceParams struct {
	ChildSessionKey     string
	ChildRunID          string
	RequesterSessionKey string
	RequesterOrigin     *DeliveryContext
	RequesterDisplayKey string
	Task                string
	TimeoutMs           int
	Cleanup             string // "delete" or "keep"
	RoundOneReply       string
	WaitForCompletion   bool
	StartedAt           time.Time
	EndedAt             time.Time
	Label               string
	Outcome             *SubagentRunOutcome
}

// AnnounceResult from the announce flow
type AnnounceResult struct {
	Announced bool
	Outcome   *SubagentRunOutcome
}

// StatsLine contains formatted subagent stats
type StatsLine struct {
	Runtime        string
	InputTokens    int
	OutputTokens   int
	TotalTokens    int
	Cost           float64
	SessionKey     string
	SessionID      string
	TranscriptPath string
}

// FormatDurationShort formats duration in human-readable form
func FormatDurationShort(d time.Duration) string {
	if d <= 0 {
		return "n/a"
	}

	totalSeconds := int(d.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// FormatTokenCount formats token counts with k/m suffixes
func FormatTokenCount(count int) string {
	if count <= 0 {
		return "0"
	}
	if count >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(count)/1_000_000)
	}
	if count >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(count)/1_000)
	}
	return fmt.Sprintf("%d", count)
}

// FormatUSD formats cost as USD
func FormatUSD(amount float64) string {
	if amount <= 0 {
		return ""
	}
	if amount >= 1 {
		return fmt.Sprintf("$%.2f", amount)
	}
	if amount >= 0.01 {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("$%.4f", amount)
}

// BuildStatsLine builds a formatted stats line for subagent results
func BuildStatsLine(stats *StatsLine) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("runtime %s", stats.Runtime))

	if stats.TotalTokens > 0 {
		inputText := FormatTokenCount(stats.InputTokens)
		outputText := FormatTokenCount(stats.OutputTokens)
		totalText := FormatTokenCount(stats.TotalTokens)
		parts = append(parts, fmt.Sprintf("tokens %s (in %s / out %s)", totalText, inputText, outputText))
	} else {
		parts = append(parts, "tokens n/a")
	}

	if costText := FormatUSD(stats.Cost); costText != "" {
		parts = append(parts, fmt.Sprintf("est %s", costText))
	}

	parts = append(parts, fmt.Sprintf("sessionKey %s", stats.SessionKey))

	if stats.SessionID != "" {
		parts = append(parts, fmt.Sprintf("sessionId %s", stats.SessionID))
	}

	if stats.TranscriptPath != "" {
		parts = append(parts, fmt.Sprintf("transcript %s", stats.TranscriptPath))
	}

	return "Stats: " + strings.Join(parts, " \u2022 ")
}

// SubagentSystemPromptParams for building system prompt
type SubagentSystemPromptParams struct {
	RequesterSessionKey string
	RequesterOrigin     *DeliveryContext
	ChildSessionKey     string
	Label               string
	Task                string
}

// BuildSubagentSystemPrompt builds the system prompt for a subagent
func BuildSubagentSystemPrompt(params SubagentSystemPromptParams) string {
	taskText := params.Task
	if taskText == "" {
		taskText = "{{TASK_DESCRIPTION}}"
	}

	var lines []string
	lines = append(lines, "# Subagent Context")
	lines = append(lines, "")
	lines = append(lines, "You are a **subagent** spawned by the main agent for a specific task.")
	lines = append(lines, "")
	lines = append(lines, "## Your Role")
	lines = append(lines, fmt.Sprintf("- You were created to handle: %s", taskText))
	lines = append(lines, "- Complete this task. That's your entire purpose.")
	lines = append(lines, "- You are NOT the main agent. Don't try to be.")
	lines = append(lines, "")
	lines = append(lines, "## Rules")
	lines = append(lines, "1. **Stay focused** - Do your assigned task, nothing else")
	lines = append(lines, "2. **Complete the task** - Your final message will be automatically reported to the main agent")
	lines = append(lines, "3. **Don't initiate** - No heartbeats, no proactive actions, no side quests")
	lines = append(lines, "4. **Be ephemeral** - You may be terminated after task completion. That's fine.")
	lines = append(lines, "")
	lines = append(lines, "## Output Format")
	lines = append(lines, "When complete, your final response should include:")
	lines = append(lines, "- What you accomplished or found")
	lines = append(lines, "- Any relevant details the main agent should know")
	lines = append(lines, "- Keep it concise but informative")
	lines = append(lines, "")
	lines = append(lines, "## What You DON'T Do")
	lines = append(lines, "- NO user conversations (that's main agent's job)")
	lines = append(lines, "- NO external messages (email, tweets, etc.) unless explicitly tasked")
	lines = append(lines, "- NO cron jobs or persistent state")
	lines = append(lines, "- NO pretending to be the main agent")
	lines = append(lines, "- NO using the `message` tool directly")
	lines = append(lines, "")
	lines = append(lines, "## Session Context")

	if params.Label != "" {
		lines = append(lines, fmt.Sprintf("- Label: %s", params.Label))
	}
	if params.RequesterSessionKey != "" {
		lines = append(lines, fmt.Sprintf("- Requester session: %s.", params.RequesterSessionKey))
	}
	if params.RequesterOrigin != nil && params.RequesterOrigin.Channel != "" {
		lines = append(lines, fmt.Sprintf("- Requester channel: %s.", params.RequesterOrigin.Channel))
	}
	lines = append(lines, fmt.Sprintf("- Your session: %s.", params.ChildSessionKey))
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

// TriggerMessageParams for building trigger message
type TriggerMessageParams struct {
	Label     string
	Task      string
	Outcome   *SubagentRunOutcome
	Reply     string
	StatsLine string
}

// BuildTriggerMessage builds the message to send to main agent
func BuildTriggerMessage(params TriggerMessageParams) string {
	taskLabel := params.Label
	if taskLabel == "" {
		taskLabel = params.Task
	}
	if taskLabel == "" {
		taskLabel = "background task"
	}

	statusLabel := "finished with unknown status"
	switch params.Outcome.Status {
	case "ok":
		statusLabel = "completed successfully"
	case "timeout":
		statusLabel = "timed out"
	case "error":
		if params.Outcome.Error != "" {
			statusLabel = fmt.Sprintf("failed: %s", params.Outcome.Error)
		} else {
			statusLabel = "failed: unknown error"
		}
	}

	reply := params.Reply
	if reply == "" {
		reply = "(no output)"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf(`A background task "%s" just %s.`, taskLabel, statusLabel))
	lines = append(lines, "")
	lines = append(lines, "Findings:")
	lines = append(lines, reply)
	lines = append(lines, "")
	lines = append(lines, params.StatsLine)
	lines = append(lines, "")
	lines = append(lines, "Summarize this naturally for the user. Keep it brief (1-2 sentences). Flow it into the conversation naturally.")
	lines = append(lines, "Do not mention technical details like tokens, stats, or that this was a background task.")
	lines = append(lines, "You can respond with NO_REPLY if no announcement is needed (e.g., internal task with no user-facing result).")

	return strings.Join(lines, "\n")
}
