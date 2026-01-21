package gateway

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// SystemPromptOptions holds dynamic prompt sections that vary per request.
type SystemPromptOptions struct {
	ToolNotes         string
	MemoryLines       []string
	Heartbeat         string
	WorkspaceSections []PromptSection
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

	if memoryLines := normalizePromptLines(opts.MemoryLines); len(memoryLines) > 0 {
		lines = append(lines, fmt.Sprintf("Recent memory:\n%s", strings.Join(memoryLines, "\n")))
	}

	if notes := strings.TrimSpace(opts.ToolNotes); notes != "" {
		lines = append(lines, fmt.Sprintf("Tool notes:\n%s", notes))
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
