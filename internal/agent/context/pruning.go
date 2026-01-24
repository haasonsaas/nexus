package context

import (
	"strconv"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ContextPruningMode controls when pruning runs.
type ContextPruningMode string

const (
	// ContextPruningOff disables pruning.
	ContextPruningOff ContextPruningMode = "off"
	// ContextPruningCacheTTL prunes when cached tool results are stale.
	ContextPruningCacheTTL ContextPruningMode = "cache-ttl"
)

// ContextPruningToolMatch controls which tool results are prunable.
type ContextPruningToolMatch struct {
	Allow []string
	Deny  []string
}

// ContextPruningSoftTrim configures soft trimming.
type ContextPruningSoftTrim struct {
	MaxChars  int
	HeadChars int
	TailChars int
}

// ContextPruningHardClear configures hard clearing.
type ContextPruningHardClear struct {
	Enabled     bool
	Placeholder string
}

// ContextPruningSettings controls in-memory tool result pruning.
type ContextPruningSettings struct {
	Mode                 ContextPruningMode
	TTL                  time.Duration
	KeepLastAssistants   int
	SoftTrimRatio        float64
	HardClearRatio       float64
	MinPrunableToolChars int
	Tools                ContextPruningToolMatch
	SoftTrim             ContextPruningSoftTrim
	HardClear            ContextPruningHardClear
}

// DefaultContextPruningSettings returns defaults aligned with Clawdbot.
func DefaultContextPruningSettings() ContextPruningSettings {
	return ContextPruningSettings{
		Mode:                 ContextPruningCacheTTL,
		TTL:                  5 * time.Minute,
		KeepLastAssistants:   3,
		SoftTrimRatio:        0.3,
		HardClearRatio:       0.5,
		MinPrunableToolChars: 50000,
		Tools:                ContextPruningToolMatch{},
		SoftTrim: ContextPruningSoftTrim{
			MaxChars:  4000,
			HeadChars: 1500,
			TailChars: 1500,
		},
		HardClear: ContextPruningHardClear{
			Enabled:     true,
			Placeholder: "[Old tool result content cleared]",
		},
	}
}

// PruneContextMessages trims or clears old tool results from history.
// Returns the original slice if no changes are required.
func PruneContextMessages(messages []*models.Message, settings ContextPruningSettings, charWindow int) []*models.Message {
	if len(messages) == 0 || charWindow <= 0 {
		return messages
	}

	cutoffIndex, ok := findAssistantCutoffIndex(messages, settings.KeepLastAssistants)
	if !ok {
		return messages
	}

	firstUser := findFirstUserIndex(messages)
	pruneStart := len(messages)
	if firstUser >= 0 {
		pruneStart = firstUser
	}
	if pruneStart >= cutoffIndex {
		return messages
	}

	totalChars := estimateContextChars(messages)
	if float64(totalChars)/float64(charWindow) < settings.SoftTrimRatio {
		return messages
	}

	toolNames := buildToolCallNameMap(messages)
	isToolPrunable := makeToolPrunablePredicate(settings.Tools)

	type prunableRef struct {
		msgIndex    int
		resultIndex int
	}

	var prunable []prunableRef
	var next []*models.Message

	for i := pruneStart; i < cutoffIndex; i++ {
		msg := currentMessage(messages, next, i)
		if msg == nil || len(msg.ToolResults) == 0 {
			continue
		}
		for j := range msg.ToolResults {
			tr := msg.ToolResults[j]
			toolName := toolNames[tr.ToolCallID]
			if !isToolPrunable(toolName) {
				continue
			}
			prunable = append(prunable, prunableRef{msgIndex: i, resultIndex: j})

			trimmed, changed := softTrimToolResult(tr.Content, settings)
			if !changed {
				continue
			}

			before := estimateMessageChars(msg)
			updated := copyMessageWithToolResults(msg)
			if j < len(updated.ToolResults) {
				updated.ToolResults[j].Content = trimmed
			}
			after := estimateMessageChars(updated)
			totalChars += after - before
			next = ensureMessage(next, messages, i, updated)
			msg = updated
		}
	}

	output := messages
	if next != nil {
		output = next
	}

	if float64(totalChars)/float64(charWindow) < settings.HardClearRatio || !settings.HardClear.Enabled {
		return output
	}

	prunableChars := 0
	for _, ref := range prunable {
		msg := currentMessage(messages, next, ref.msgIndex)
		if msg == nil || ref.resultIndex >= len(msg.ToolResults) {
			continue
		}
		prunableChars += len(msg.ToolResults[ref.resultIndex].Content)
	}
	if prunableChars < settings.MinPrunableToolChars {
		return output
	}

	ratio := float64(totalChars) / float64(charWindow)
	for _, ref := range prunable {
		if ratio < settings.HardClearRatio {
			break
		}
		msg := currentMessage(messages, next, ref.msgIndex)
		if msg == nil || ref.resultIndex >= len(msg.ToolResults) {
			continue
		}

		before := estimateMessageChars(msg)
		updated := copyMessageWithToolResults(msg)
		updated.ToolResults[ref.resultIndex].Content = settings.HardClear.Placeholder
		after := estimateMessageChars(updated)
		totalChars += after - before
		ratio = float64(totalChars) / float64(charWindow)
		next = ensureMessage(next, messages, ref.msgIndex, updated)
	}

	if next != nil {
		return next
	}
	return messages
}

func findAssistantCutoffIndex(messages []*models.Message, keepLastAssistants int) (int, bool) {
	if keepLastAssistants <= 0 {
		return len(messages), true
	}
	remaining := keepLastAssistants
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].Role == models.RoleAssistant {
			remaining--
			if remaining == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func findFirstUserIndex(messages []*models.Message) int {
	for i, msg := range messages {
		if msg != nil && msg.Role == models.RoleUser {
			return i
		}
	}
	return -1
}

func softTrimToolResult(content string, settings ContextPruningSettings) (string, bool) {
	rawLen := len(content)
	if rawLen <= settings.SoftTrim.MaxChars {
		return content, false
	}
	headChars := maxInt(settings.SoftTrim.HeadChars, 0)
	tailChars := maxInt(settings.SoftTrim.TailChars, 0)
	if headChars+tailChars >= rawLen {
		return content, false
	}
	head := content
	if headChars < len(head) {
		head = head[:headChars]
	}
	tail := content
	if tailChars < len(tail) {
		tail = tail[len(tail)-tailChars:]
	}

	trimmed := head + "\n...\n" + tail
	note := "\n\n[Tool result trimmed: kept first " + strconv.Itoa(headChars) + " chars and last " + strconv.Itoa(tailChars) + " chars of " + strconv.Itoa(rawLen) + " chars.]"
	return trimmed + note, true
}

func makeToolPrunablePredicate(match ContextPruningToolMatch) func(string) bool {
	deny := normalizePatterns(match.Deny)
	allow := normalizePatterns(match.Allow)
	return func(toolName string) bool {
		normalized := strings.ToLower(strings.TrimSpace(toolName))
		if normalized == "" {
			return false
		}
		if matchesAny(normalized, deny) {
			return false
		}
		if len(allow) == 0 {
			return true
		}
		return matchesAny(normalized, allow)
	}
}

func normalizePatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		value := strings.ToLower(strings.TrimSpace(p))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if wildcardMatch(p, name) {
			return true
		}
	}
	return false
}

func wildcardMatch(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}
	parts := strings.Split(pattern, "*")
	idx := 0
	if len(parts) == 0 {
		return false
	}
	if parts[0] != "" {
		if !strings.HasPrefix(value, parts[0]) {
			return false
		}
		idx = len(parts[0])
	}
	for i := 1; i < len(parts)-1; i++ {
		part := parts[i]
		if part == "" {
			continue
		}
		pos := strings.Index(value[idx:], part)
		if pos < 0 {
			return false
		}
		idx += pos + len(part)
	}
	last := parts[len(parts)-1]
	if last != "" && !strings.HasSuffix(value, last) {
		return false
	}
	return true
}

func buildToolCallNameMap(messages []*models.Message) map[string]string {
	names := make(map[string]string)
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID == "" || tc.Name == "" {
				continue
			}
			names[tc.ID] = tc.Name
		}
	}
	return names
}

func estimateContextChars(messages []*models.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateMessageChars(msg)
	}
	return total
}

func estimateMessageChars(msg *models.Message) int {
	if msg == nil {
		return 0
	}
	chars := len(msg.Content)
	for _, tc := range msg.ToolCalls {
		chars += len(tc.Name) + len(tc.Input)
	}
	for _, tr := range msg.ToolResults {
		chars += len(tr.Content)
	}
	return chars
}

func currentMessage(messages []*models.Message, next []*models.Message, index int) *models.Message {
	if next != nil {
		return next[index]
	}
	return messages[index]
}

func ensureMessage(next []*models.Message, messages []*models.Message, index int, updated *models.Message) []*models.Message {
	if next == nil {
		next = make([]*models.Message, len(messages))
		copy(next, messages)
	}
	next[index] = updated
	return next
}

func copyMessageWithToolResults(msg *models.Message) *models.Message {
	if msg == nil {
		return nil
	}
	clone := *msg
	if len(msg.ToolResults) > 0 {
		clone.ToolResults = append([]models.ToolResult(nil), msg.ToolResults...)
	}
	return &clone
}

func maxInt(value, min int) int {
	if value < min {
		return min
	}
	return value
}
