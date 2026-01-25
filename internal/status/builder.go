// Package status provides rich status message building following the clawdbot pattern.
package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/commands"
	"github.com/haasonsaas/nexus/internal/config"
)

// Version information - these should be set at build time.
var (
	Version   = "dev"
	GitCommit = ""
)

// StatusArgs contains all inputs for building a status message.
type StatusArgs struct {
	Config            *config.Config
	SessionKey        string
	SessionScope      string // "per-sender", "shared", etc.
	GroupActivation   string // "mention" or "always"
	ResolvedThink     string // "off", "low", "medium", "high"
	ResolvedVerbose   string // "off", "on", "full"
	ResolvedReasoning string
	ResolvedElevated  string // "off", "on", "ask", "full"
	ModelAuth         string // "api-key", "oauth", "mixed"

	// Model info
	Provider      string
	Model         string
	ContextTokens int

	// Usage info
	InputTokens     int
	OutputTokens    int
	TotalTokens     int
	CompactionCount int
	ResponseTimeMs  int64

	// Optional usage lines
	UsageLine string
	TimeLine  string

	// Queue status
	Queue *QueueStatus

	// Media understanding
	MediaDecisions []MediaDecision

	// Subagents
	SubagentsLine string

	// Runtime info
	RuntimeMode string // "docker", "direct"
	SandboxMode string // "off", "all", "non-main"

	// Voice info
	VoiceEnabled      bool
	VoiceProvider     string
	VoiceSummaryLimit int
	VoiceSummaryOn    bool

	// Session timing
	UpdatedAt *time.Time
	Now       time.Time

	// Whether to include transcript usage
	IncludeTranscriptUsage bool
}

// QueueStatus contains queue information.
type QueueStatus struct {
	Mode        string
	Depth       int
	DebounceMs  int
	Cap         int
	DropPolicy  string
	ShowDetails bool
}

// MediaDecision represents media understanding status for a capability.
type MediaDecision struct {
	Capability  string // "vision", "audio", "video"
	Outcome     string // "success", "disabled", "scope-deny", "skipped", "no-attachment"
	Attachments []MediaAttachment
}

// MediaAttachment contains media processing info.
type MediaAttachment struct {
	Chosen   *MediaChoice
	Attempts []MediaAttempt
}

// MediaChoice represents the selected provider/model for media processing.
type MediaChoice struct {
	Provider string
	Model    string
}

// MediaAttempt records a processing attempt.
type MediaAttempt struct {
	Reason string
}

// SkillCommand represents a skill command for help display.
type SkillCommand struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
}

// FormatTokenCount formats token count with K/M suffixes.
func FormatTokenCount(tokens int) string {
	if tokens <= 0 {
		return "0"
	}
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
	}
	if tokens >= 10_000 {
		return fmt.Sprintf("%dk", tokens/1_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}

// FormatContextUsageShort formats context usage as "Context X/Y (Z%)".
func FormatContextUsageShort(total, contextTokens int) string {
	totalLabel := FormatTokenCount(total)
	ctxLabel := "?"
	if contextTokens > 0 {
		ctxLabel = FormatTokenCount(contextTokens)
	}

	if total == 0 {
		return fmt.Sprintf("Context ?/%s", ctxLabel)
	}

	pctStr := ""
	if contextTokens > 0 {
		pct := min(999, (total*100)/contextTokens)
		pctStr = fmt.Sprintf(" (%d%%)", pct)
	}

	return fmt.Sprintf("Context %s/%s%s", totalLabel, ctxLabel, pctStr)
}

// FormatAge formats a duration as "just now", "5m ago", "2h ago", "3d ago".
func FormatAge(d time.Duration) string {
	if d < 0 {
		return "unknown"
	}
	minutes := int(d.Minutes())
	if minutes < 1 {
		return "just now"
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm ago", minutes)
	}
	hours := int(d.Hours())
	if hours < 48 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("%dd ago", days)
}

// FormatQueueDetails formats queue status details.
func FormatQueueDetails(queue *QueueStatus) string {
	if queue == nil {
		return ""
	}

	depth := ""
	if queue.Depth >= 0 {
		depth = fmt.Sprintf("depth %d", queue.Depth)
	}

	if !queue.ShowDetails {
		if depth != "" {
			return fmt.Sprintf(" (%s)", depth)
		}
		return ""
	}

	var parts []string
	if depth != "" {
		parts = append(parts, depth)
	}
	if queue.DebounceMs > 0 {
		ms := queue.DebounceMs
		var label string
		if ms >= 1000 {
			if ms%1000 == 0 {
				label = fmt.Sprintf("%ds", ms/1000)
			} else {
				label = fmt.Sprintf("%.1fs", float64(ms)/1000)
			}
		} else {
			label = fmt.Sprintf("%dms", ms)
		}
		parts = append(parts, fmt.Sprintf("debounce %s", label))
	}
	if queue.Cap > 0 {
		parts = append(parts, fmt.Sprintf("cap %d", queue.Cap))
	}
	if queue.DropPolicy != "" {
		parts = append(parts, fmt.Sprintf("drop %s", queue.DropPolicy))
	}

	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s)", strings.Join(parts, " \u00b7 "))
}

// FormatUsagePair formats "Tokens: X in / Y out".
func FormatUsagePair(input, output int) string {
	if input == 0 && output == 0 {
		return ""
	}
	inputLabel := FormatTokenCount(input)
	outputLabel := FormatTokenCount(output)
	return fmt.Sprintf("\U0001F9EE Tokens: %s in / %s out", inputLabel, outputLabel)
}

// FormatMediaUnderstandingLine formats media processing status.
func FormatMediaUnderstandingLine(decisions []MediaDecision) string {
	if len(decisions) == 0 {
		return ""
	}

	var parts []string
	for _, decision := range decisions {
		count := len(decision.Attachments)
		countLabel := ""
		if count > 1 {
			countLabel = fmt.Sprintf(" x%d", count)
		}

		switch decision.Outcome {
		case "success":
			var modelLabel string
			for _, att := range decision.Attachments {
				if att.Chosen != nil {
					provider := strings.TrimSpace(att.Chosen.Provider)
					model := strings.TrimSpace(att.Chosen.Model)
					if provider != "" {
						if model != "" {
							modelLabel = fmt.Sprintf("%s/%s", provider, model)
						} else {
							modelLabel = provider
						}
						break
					}
				}
			}
			part := fmt.Sprintf("%s%s ok", decision.Capability, countLabel)
			if modelLabel != "" {
				part += fmt.Sprintf(" (%s)", modelLabel)
			}
			parts = append(parts, part)

		case "no-attachment":
			parts = append(parts, fmt.Sprintf("%s none", decision.Capability))

		case "disabled":
			parts = append(parts, fmt.Sprintf("%s off", decision.Capability))

		case "scope-deny":
			parts = append(parts, fmt.Sprintf("%s denied", decision.Capability))

		case "skipped":
			var reason string
			for _, att := range decision.Attachments {
				for _, attempt := range att.Attempts {
					if attempt.Reason != "" {
						reason = strings.Split(attempt.Reason, ":")[0]
						break
					}
				}
				if reason != "" {
					break
				}
			}
			part := fmt.Sprintf("%s skipped", decision.Capability)
			if reason != "" {
				part += fmt.Sprintf(" (%s)", strings.TrimSpace(reason))
			}
			parts = append(parts, part)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	// If all parts end with " none", return empty
	allNone := true
	for _, part := range parts {
		if !strings.HasSuffix(part, " none") {
			allNone = false
			break
		}
	}
	if allNone {
		return ""
	}

	return fmt.Sprintf("\U0001F4CE Media: %s", strings.Join(parts, " \u00b7 "))
}

// FormatVoiceModeLine formats TTS status.
func FormatVoiceModeLine(cfg *config.Config, args *StatusArgs) string {
	if args == nil || !args.VoiceEnabled {
		return ""
	}

	parts := []string{"\U0001F50A Voice: on"}

	if args.VoiceProvider != "" {
		parts = append(parts, fmt.Sprintf("provider=%s", args.VoiceProvider))
	}
	if args.VoiceSummaryLimit > 0 {
		parts = append(parts, fmt.Sprintf("limit=%d", args.VoiceSummaryLimit))
	}
	if args.VoiceSummaryOn {
		parts = append(parts, "summary=on")
	}

	return strings.Join(parts, " \u00b7 ")
}

// FormatResponseTime formats response time as "1.2s" or "150ms".
func FormatResponseTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dms", ms)
}

// resolveRuntimeLabel returns the runtime label (e.g., "docker/isolated", "direct").
func resolveRuntimeLabel(args *StatusArgs) string {
	if args.SandboxMode == "" || args.SandboxMode == "off" {
		return "direct"
	}

	runtime := "direct"
	if args.RuntimeMode != "" {
		runtime = args.RuntimeMode
	}

	return fmt.Sprintf("%s/%s", runtime, args.SandboxMode)
}

// BuildStatusMessage builds the full status message.
func BuildStatusMessage(args StatusArgs) string {
	if args.Now.IsZero() {
		args.Now = time.Now()
	}

	// Version line
	versionLine := fmt.Sprintf("\U0001F99E Nexus %s", Version)
	if GitCommit != "" {
		versionLine += fmt.Sprintf(" (%s)", GitCommit)
	}

	// Response time line
	var responseLine string
	if args.ResponseTimeMs > 0 {
		responseLine = fmt.Sprintf("\u23F1\uFE0F Response time: %s", FormatResponseTime(args.ResponseTimeMs))
	}

	// Model line
	provider := args.Provider
	if provider == "" {
		provider = "anthropic"
	}
	model := args.Model
	if model == "" {
		model = "unknown"
	}
	modelLabel := fmt.Sprintf("%s/%s", provider, model)
	authLabel := ""
	if args.ModelAuth != "" && args.ModelAuth != "unknown" {
		authLabel = fmt.Sprintf(" \u00b7 \U0001F511 %s", args.ModelAuth)
	}
	modelLine := fmt.Sprintf("\U0001F9E0 Model: %s%s", modelLabel, authLabel)

	// Usage and cost line
	usagePair := FormatUsagePair(args.InputTokens, args.OutputTokens)
	var usageCostLine string
	if usagePair != "" {
		usageCostLine = usagePair
		// Cost estimation will be added by the cost module
		if args.ModelAuth == "api-key" || args.ModelAuth == "mixed" {
			costConfig := ResolveModelCostConfig(provider, model, args.Config)
			if costConfig != nil {
				cost := EstimateUsageCost(args.InputTokens, args.OutputTokens, costConfig)
				if cost > 0 {
					usageCostLine += fmt.Sprintf(" \u00b7 \U0001F4B5 Cost: %s", FormatUSD(cost))
				}
			}
		}
	}

	// Context line
	contextLine := fmt.Sprintf("\U0001F4DA %s \u00b7 \U0001F9F9 Compactions: %d",
		FormatContextUsageShort(args.TotalTokens, args.ContextTokens),
		args.CompactionCount)

	// Media line
	mediaLine := FormatMediaUnderstandingLine(args.MediaDecisions)

	// Session line
	sessionKey := args.SessionKey
	if sessionKey == "" {
		sessionKey = "unknown"
	}
	var updatedLabel string
	if args.UpdatedAt != nil {
		updatedLabel = fmt.Sprintf("updated %s", FormatAge(args.Now.Sub(*args.UpdatedAt)))
	} else {
		updatedLabel = "no activity"
	}
	sessionLine := fmt.Sprintf("\U0001F9F5 Session: %s \u2022 %s", sessionKey, updatedLabel)

	// Queue and activation line
	isGroupSession := strings.Contains(sessionKey, ":group:") || strings.Contains(sessionKey, ":channel:")
	var activationLine string
	queueMode := "unknown"
	if args.Queue != nil && args.Queue.Mode != "" {
		queueMode = args.Queue.Mode
	}
	queueDetails := FormatQueueDetails(args.Queue)

	if isGroupSession {
		activation := args.GroupActivation
		if activation == "" {
			activation = "mention"
		}
		activationLine = fmt.Sprintf("\U0001F465 Activation: %s \u00b7 \U0001FAA2 Queue: %s%s",
			activation, queueMode, queueDetails)
	} else {
		activationLine = fmt.Sprintf("\U0001FAA2 Queue: %s%s", queueMode, queueDetails)
	}

	// Options line
	thinkLevel := args.ResolvedThink
	if thinkLevel == "" {
		thinkLevel = "off"
	}

	var verboseLabel string
	switch args.ResolvedVerbose {
	case "full":
		verboseLabel = "verbose:full"
	case "on":
		verboseLabel = "verbose"
	}

	var elevatedLabel string
	if args.ResolvedElevated != "" && args.ResolvedElevated != "off" {
		if args.ResolvedElevated == "on" {
			elevatedLabel = "elevated"
		} else {
			elevatedLabel = fmt.Sprintf("elevated:%s", args.ResolvedElevated)
		}
	}

	var optionParts []string
	optionParts = append(optionParts, fmt.Sprintf("Runtime: %s", resolveRuntimeLabel(&args)))
	optionParts = append(optionParts, fmt.Sprintf("Think: %s", thinkLevel))
	if verboseLabel != "" {
		optionParts = append(optionParts, verboseLabel)
	}
	if args.ResolvedReasoning != "" && args.ResolvedReasoning != "off" {
		optionParts = append(optionParts, fmt.Sprintf("Reasoning: %s", args.ResolvedReasoning))
	}
	if elevatedLabel != "" {
		optionParts = append(optionParts, elevatedLabel)
	}
	optionsLine := "\u2699\uFE0F " + strings.Join(optionParts, " \u00b7 ")

	// Voice line
	voiceLine := FormatVoiceModeLine(args.Config, &args)

	// Build final message
	var lines []string
	lines = append(lines, versionLine)
	if responseLine != "" {
		lines = append(lines, responseLine)
	}
	lines = append(lines, modelLine)
	if usageCostLine != "" {
		lines = append(lines, usageCostLine)
	}
	lines = append(lines, contextLine)
	if mediaLine != "" {
		lines = append(lines, mediaLine)
	}
	if args.UsageLine != "" {
		lines = append(lines, args.UsageLine)
	}
	lines = append(lines, sessionLine)
	if args.SubagentsLine != "" {
		lines = append(lines, args.SubagentsLine)
	}
	lines = append(lines, activationLine)
	lines = append(lines, optionsLine)
	if voiceLine != "" {
		lines = append(lines, voiceLine)
	}

	return strings.Join(lines, "\n")
}

// BuildHelpMessage builds the help message.
func BuildHelpMessage(cfg *config.Config) string {
	options := []string{
		"/think <level>",
		"/verbose on|full|off",
		"/reasoning on|off",
		"/elevated on|off|ask|full",
		"/model <id>",
		"/usage off|tokens|full",
	}

	lines := []string{
		"\u2139\uFE0F Help",
		"Shortcuts: /new reset | /compact [instructions] | /restart relink (if enabled)",
		fmt.Sprintf("Options: %s", strings.Join(options, " | ")),
		"Skills: /skill <name> [input]",
		"More: /commands for all slash commands",
	}

	return strings.Join(lines, "\n")
}

// BuildCommandsMessage builds the commands list message.
func BuildCommandsMessage(cfg *config.Config, skillCommands []SkillCommand) string {
	lines := []string{"\u2139\uFE0F Slash commands"}

	// Get registered commands from the command registry
	cmds := listCommands(cfg, skillCommands)
	for _, cmd := range cmds {
		primary := "/" + cmd.Name
		if len(cmd.Aliases) > 0 && cmd.Aliases[0] != "" {
			// Use first alias as primary if different
			alias := strings.TrimPrefix(cmd.Aliases[0], "/")
			if !strings.EqualFold(alias, cmd.Name) {
				primary = "/" + cmd.Name
			}
		}

		seen := make(map[string]struct{})
		seen[strings.ToLower(primary)] = struct{}{}

		var aliases []string
		for _, alias := range cmd.Aliases {
			aliasLower := strings.ToLower(strings.TrimPrefix(alias, "/"))
			if _, exists := seen[aliasLower]; exists {
				continue
			}
			seen[aliasLower] = struct{}{}
			aliases = append(aliases, alias)
		}

		aliasLabel := ""
		if len(aliases) > 0 {
			aliasLabel = fmt.Sprintf(" (aliases: %s)", strings.Join(aliases, ", "))
		}

		lines = append(lines, fmt.Sprintf("%s%s - %s", primary, aliasLabel, cmd.Description))
	}

	return strings.Join(lines, "\n")
}

// commandInfo holds command display info.
type commandInfo struct {
	Name        string
	Aliases     []string
	Description string
	Scope       string
}

// listCommands builds the list of available commands.
func listCommands(cfg *config.Config, skillCommands []SkillCommand) []commandInfo {
	var cmds []commandInfo

	// Built-in commands
	builtins := []commandInfo{
		{Name: "status", Description: "Show current session status"},
		{Name: "help", Description: "Show help message"},
		{Name: "commands", Description: "List all available commands"},
		{Name: "new", Aliases: []string{"/reset"}, Description: "Start a new session"},
		{Name: "compact", Description: "Compact conversation history"},
		{Name: "think", Description: "Set thinking level (off, low, medium, high)"},
		{Name: "verbose", Description: "Set verbose mode (off, on, full)"},
		{Name: "reasoning", Description: "Toggle reasoning mode"},
		{Name: "elevated", Description: "Set elevated permissions (off, on, ask, full)"},
		{Name: "model", Description: "Change the AI model"},
		{Name: "usage", Description: "Show token usage"},
		{Name: "whoami", Description: "Show your identity"},
		{Name: "id", Description: "Show session identifier"},
	}
	cmds = append(cmds, builtins...)

	// Add skill commands
	for _, skill := range skillCommands {
		cmds = append(cmds, commandInfo{
			Name:        skill.Name,
			Aliases:     skill.Aliases,
			Description: skill.Description,
		})
	}

	return cmds
}

// CommandSpec returns a Command struct for the status command.
func CommandSpec() *commands.Command {
	return &commands.Command{
		Name:        "status",
		Description: "Show current session status",
		Usage:       "/status",
		Category:    "info",
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
