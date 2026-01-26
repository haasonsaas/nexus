package gateway

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

// SteeringRuleTrace captures why a steering rule matched.
type SteeringRuleTrace struct {
	ID       string   `json:"id,omitempty"`
	Name     string   `json:"name,omitempty"`
	Priority int      `json:"priority,omitempty"`
	Matched  bool     `json:"matched"`
	Reasons  []string `json:"reasons,omitempty"`
}

func (s *Server) steeringForMessage(session *models.Session, msg *models.Message) (string, []SteeringRuleTrace) {
	if s == nil || s.config == nil || !s.config.Steering.Enabled {
		return "", nil
	}
	if msg == nil {
		return "", nil
	}

	now := time.Now()
	tags := mergeTagsFromMetadata(msg, session)

	type match struct {
		index    int
		prompt   string
		trace    SteeringRuleTrace
		priority int
	}

	var matches []match
	for i, rule := range s.config.Steering.Rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		prompt := strings.TrimSpace(rule.Prompt)
		if prompt == "" {
			continue
		}

		ok, reasons := matchSteeringRule(rule, session, msg, tags, now)
		if !ok {
			continue
		}

		trace := SteeringRuleTrace{
			ID:       steeringRuleID(rule, i),
			Name:     strings.TrimSpace(rule.Name),
			Priority: rule.Priority,
			Matched:  true,
			Reasons:  reasons,
		}

		matches = append(matches, match{
			index:    i,
			prompt:   prompt,
			trace:    trace,
			priority: rule.Priority,
		})
	}

	if len(matches) == 0 {
		return "", nil
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].priority == matches[j].priority {
			return matches[i].index < matches[j].index
		}
		return matches[i].priority > matches[j].priority
	})

	prompts := make([]string, 0, len(matches))
	traces := make([]SteeringRuleTrace, 0, len(matches))
	for _, m := range matches {
		prompts = append(prompts, m.prompt)
		traces = append(traces, m.trace)
	}

	return strings.Join(prompts, "\n"), traces
}

func steeringRuleID(rule config.SteeringRule, index int) string {
	id := strings.TrimSpace(rule.ID)
	if id != "" {
		return id
	}
	if name := strings.TrimSpace(rule.Name); name != "" {
		return name
	}
	return fmt.Sprintf("rule-%d", index+1)
}

func matchSteeringRule(rule config.SteeringRule, session *models.Session, msg *models.Message, tags map[string]struct{}, now time.Time) (bool, []string) {
	var reasons []string

	if len(rule.Roles) > 0 {
		role := strings.ToLower(strings.TrimSpace(string(msg.Role)))
		if !matchesAny(rule.Roles, role) {
			return false, []string{"role mismatch"}
		}
		reasons = append(reasons, "role="+role)
	}

	if len(rule.Channels) > 0 {
		channel := strings.ToLower(strings.TrimSpace(string(msg.Channel)))
		if !matchesAny(rule.Channels, channel) {
			return false, []string{"channel mismatch"}
		}
		reasons = append(reasons, "channel="+channel)
	}

	if len(rule.Agents) > 0 && session != nil {
		agent := strings.ToLower(strings.TrimSpace(session.AgentID))
		if !matchesAny(rule.Agents, agent) {
			return false, []string{"agent mismatch"}
		}
		reasons = append(reasons, "agent="+agent)
	}

	if len(rule.Tags) > 0 {
		if len(tags) == 0 {
			return false, []string{"tags missing"}
		}
		matched := []string{}
		for _, tag := range rule.Tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			if _, ok := tags[tag]; ok {
				matched = append(matched, tag)
			}
		}
		if len(matched) == 0 {
			return false, []string{"tag mismatch"}
		}
		reasons = append(reasons, "tags="+strings.Join(matched, ","))
	}

	if len(rule.Contains) > 0 {
		content := strings.ToLower(msg.Content)
		if content == "" {
			return false, []string{"content missing"}
		}
		var matched string
		for _, value := range rule.Contains {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" {
				continue
			}
			if strings.Contains(content, value) {
				matched = value
				break
			}
		}
		if matched == "" {
			return false, []string{"contains mismatch"}
		}
		reasons = append(reasons, fmt.Sprintf("contains=%s", matched))
	}

	if len(rule.Metadata) > 0 {
		if msg == nil || (msg.Metadata == nil && (session == nil || session.Metadata == nil)) {
			return false, []string{"metadata missing"}
		}
		for key, expected := range rule.Metadata {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			actual, ok := findMetadataValue(msg, session, key)
			if !ok {
				return false, []string{fmt.Sprintf("metadata missing: %s", key)}
			}
			actual = strings.TrimSpace(actual)
			expected = strings.TrimSpace(expected)
			if expected == "" {
				continue
			}
			if !strings.EqualFold(actual, expected) {
				return false, []string{fmt.Sprintf("metadata mismatch: %s", key)}
			}
			reasons = append(reasons, fmt.Sprintf("metadata[%s]=%s", key, expected))
		}
	}

	if rule.TimeWindow.After != "" || rule.TimeWindow.Before != "" {
		after := strings.TrimSpace(rule.TimeWindow.After)
		before := strings.TrimSpace(rule.TimeWindow.Before)
		if after != "" {
			parsed, err := time.Parse(time.RFC3339, after)
			if err != nil || now.Before(parsed) {
				return false, []string{"time before window"}
			}
			reasons = append(reasons, "after="+parsed.Format(time.RFC3339))
		}
		if before != "" {
			parsed, err := time.Parse(time.RFC3339, before)
			if err != nil || now.After(parsed) {
				return false, []string{"time after window"}
			}
			reasons = append(reasons, "before="+parsed.Format(time.RFC3339))
		}
	}

	return true, reasons
}

func matchesAny(list []string, value string) bool {
	if value == "" {
		return false
	}
	for _, item := range list {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if item == value {
			return true
		}
	}
	return false
}

func mergeTagsFromMetadata(msg *models.Message, session *models.Session) map[string]struct{} {
	tags := make(map[string]struct{})
	for _, meta := range []map[string]any{metadataForMessage(msg), metadataForSession(session)} {
		for _, tag := range tagsFromMetadata(meta) {
			if tag == "" {
				continue
			}
			tags[tag] = struct{}{}
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func metadataForMessage(msg *models.Message) map[string]any {
	if msg == nil {
		return nil
	}
	return msg.Metadata
}

func metadataForSession(session *models.Session) map[string]any {
	if session == nil {
		return nil
	}
	return session.Metadata
}

func tagsFromMetadata(meta map[string]any) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta["tags"]
	if !ok {
		return nil
	}
	switch val := raw.(type) {
	case []string:
		return normalizeTags(val)
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, fmt.Sprint(item))
		}
		return normalizeTags(out)
	case string:
		parts := strings.Split(val, ",")
		return normalizeTags(parts)
	default:
		return normalizeTags([]string{fmt.Sprint(val)})
	}
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		out = append(out, tag)
	}
	return out
}

func findMetadataValue(msg *models.Message, session *models.Session, key string) (string, bool) {
	if msg != nil && msg.Metadata != nil {
		if val, ok := msg.Metadata[key]; ok {
			return fmt.Sprint(val), true
		}
	}
	if session != nil && session.Metadata != nil {
		if val, ok := session.Metadata[key]; ok {
			return fmt.Sprint(val), true
		}
	}
	return "", false
}
