package gateway

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// SystemPromptOptions holds dynamic prompt sections that vary per request.
type SystemPromptOptions struct {
	ExperimentPrompt    string
	ToolNotes           string
	MemoryLines         []string
	VectorMemoryResults []VectorMemoryResult // Results from semantic memory search
	Heartbeat           string
	AttentionSummary    string
	WorkspaceSections   []PromptSection
	MemoryFlush         string
	SkillContent        []SkillSection
}

// VectorMemoryResult represents a result from vector memory search.
type VectorMemoryResult struct {
	Content string
	Score   float32
	Source  string
}

// SkillSection represents skill content to inject into the prompt.
type SkillSection struct {
	Name        string
	Description string
	Content     string
}

type PromptSection struct {
	Label   string
	Content string
}

func buildSystemPrompt(cfg *config.Config, opts SystemPromptOptions) string {
	if cfg == nil {
		return ""
	}

	lines := make([]string, 0, 10)

	if experimentPrompt := strings.TrimSpace(opts.ExperimentPrompt); experimentPrompt != "" {
		lines = append(lines, experimentPrompt)
	}

	missingIdentity := cfg.Identity.Name == "" && cfg.Identity.Creature == "" && cfg.Identity.Vibe == "" && cfg.Identity.Emoji == ""
	missingUser := cfg.User.Name == "" && cfg.User.PreferredAddress == "" && cfg.User.Pronouns == "" && cfg.User.Timezone == "" && cfg.User.Notes == ""

	if !missingIdentity {
		parts := []string{}
		if cfg.Identity.Name != "" {
			parts = append(parts, cfg.Identity.Name)
		}
		if cfg.Identity.Creature != "" {
			parts = append(parts, cfg.Identity.Creature)
		}
		if cfg.Identity.Vibe != "" {
			parts = append(parts, cfg.Identity.Vibe)
		}
		if cfg.Identity.Emoji != "" {
			parts = append(parts, cfg.Identity.Emoji)
		}
		lines = append(lines, fmt.Sprintf("Identity: %s.", strings.Join(parts, ", ")))
	}

	if !missingUser {
		label := cfg.User.PreferredAddress
		if label == "" {
			label = cfg.User.Name
		}
		if label == "" {
			label = "User"
		}
		meta := []string{}
		if cfg.User.Pronouns != "" {
			meta = append(meta, "pronouns: "+cfg.User.Pronouns)
		}
		if cfg.User.Timezone != "" {
			meta = append(meta, "timezone: "+cfg.User.Timezone)
		}
		if cfg.User.Notes != "" {
			meta = append(meta, "notes: "+cfg.User.Notes)
		}
		if len(meta) > 0 {
			lines = append(lines, fmt.Sprintf("%s (%s).", label, strings.Join(meta, ", ")))
		} else {
			lines = append(lines, fmt.Sprintf("%s.", label))
		}
	}

	if missingIdentity || missingUser {
		lines = append(lines, "If identity or user profile details are missing, ask the user for them and offer a few suggestions.")
	}

	if sections := normalizePromptSections(opts.WorkspaceSections); len(sections) > 0 {
		for _, section := range sections {
			lines = append(lines, fmt.Sprintf("%s:\n%s", section.Label, section.Content))
		}
	}

	if heartbeat := strings.TrimSpace(opts.Heartbeat); heartbeat != "" {
		lines = append(lines, fmt.Sprintf("Heartbeat checklist (only report new/changed items; reply HEARTBEAT_OK if nothing needs attention):\n%s", heartbeat))
	}

	if attention := strings.TrimSpace(opts.AttentionSummary); attention != "" {
		lines = append(lines, fmt.Sprintf("Attention feed (active items):\n%s", attention))
	}

	if flush := strings.TrimSpace(opts.MemoryFlush); flush != "" {
		lines = append(lines, fmt.Sprintf("Memory flush reminder:\n%s", flush))
	}

	if memoryLines := normalizePromptLines(opts.MemoryLines); len(memoryLines) > 0 {
		lines = append(lines, fmt.Sprintf("Recent memory:\n%s", strings.Join(memoryLines, "\n")))
	}

	// Add vector memory results if available
	if vectorResults := normalizeVectorResults(opts.VectorMemoryResults); len(vectorResults) > 0 {
		var resultLines []string
		for _, r := range vectorResults {
			resultLines = append(resultLines, fmt.Sprintf("- [%.2f] %s", r.Score, truncateContent(r.Content, 200)))
		}
		lines = append(lines, fmt.Sprintf("Relevant context (from memory search):\n%s", strings.Join(resultLines, "\n")))
	}

	if notes := strings.TrimSpace(opts.ToolNotes); notes != "" {
		lines = append(lines, fmt.Sprintf("Tool notes:\n%s", notes))
	}

	// Add skill content
	if skillSections := normalizeSkillSections(opts.SkillContent); len(skillSections) > 0 {
		lines = append(lines, "\n# Skills\n")
		for _, skill := range skillSections {
			skillHeader := fmt.Sprintf("## %s", skill.Name)
			if skill.Description != "" {
				skillHeader += fmt.Sprintf("\n%s", skill.Description)
			}
			lines = append(lines, fmt.Sprintf("%s\n\n%s", skillHeader, skill.Content))
		}
	}

	lines = append(lines, "Do not exfiltrate secrets. Avoid destructive actions unless explicitly requested. Never stream partial replies to external messaging surfaces.")
	lines = append(lines, "Be concise, direct, and ask clarifying questions when requirements are ambiguous.")

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizePromptLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func normalizePromptSections(sections []PromptSection) []PromptSection {
	if len(sections) == 0 {
		return nil
	}
	out := make([]PromptSection, 0, len(sections))
	for _, section := range sections {
		label := strings.TrimSpace(section.Label)
		content := strings.TrimSpace(section.Content)
		if label == "" || content == "" {
			continue
		}
		out = append(out, PromptSection{Label: label, Content: content})
	}
	return out
}

func normalizeSkillSections(sections []SkillSection) []SkillSection {
	if len(sections) == 0 {
		return nil
	}
	out := make([]SkillSection, 0, len(sections))
	for _, section := range sections {
		name := strings.TrimSpace(section.Name)
		content := strings.TrimSpace(section.Content)
		if name == "" || content == "" {
			continue
		}
		out = append(out, SkillSection{
			Name:        name,
			Description: strings.TrimSpace(section.Description),
			Content:     content,
		})
	}
	return out
}

func normalizeVectorResults(results []VectorMemoryResult) []VectorMemoryResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]VectorMemoryResult, 0, len(results))
	for _, r := range results {
		content := strings.TrimSpace(r.Content)
		if content == "" {
			continue
		}
		out = append(out, VectorMemoryResult{
			Content: content,
			Score:   r.Score,
			Source:  strings.TrimSpace(r.Source),
		})
	}
	return out
}

func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
