package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/multiagent"
)

// =============================================================================
// Agent Command Helpers
// =============================================================================

// printAgentsList prints the list of configured agents.
func printAgentsList(out io.Writer, configPath string) error {
	manifest, agentsPath, err := loadAgentsManifest(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			manifest = &multiagent.AgentManifest{}
		} else {
			return err
		}
	}

	fmt.Fprintln(out, "Configured Agents")
	fmt.Fprintln(out, "=================")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Source: %s\n\n", agentsPath)

	if len(manifest.Agents) == 0 {
		fmt.Fprintln(out, "No agents defined.")
		return nil
	}

	fmt.Fprintln(out, "ID          Name           Provider    Model")
	fmt.Fprintln(out, "----------  -------------  ----------  ----------------------")
	for _, agent := range manifest.Agents {
		provider := agent.Provider
		if provider == "" {
			provider = "-"
		}
		model := agent.Model
		if model == "" {
			model = "-"
		}
		fmt.Fprintf(out, "%-10s  %-13s  %-10s  %s\n", agent.ID, truncate(agent.Name, 13), provider, model)
	}
	fmt.Fprintln(out)

	return nil
}

// printAgentCreate creates a new agent definition in AGENTS.md.
func printAgentCreate(out io.Writer, configPath, name, provider, model string) error {
	slog.Info("creating agent",
		"name", name,
		"provider", provider,
		"model", model,
	)

	manifest, agentsPath, err := loadAgentsManifest(configPath)
	if err != nil {
		return err
	}

	agentID := slugifyAgentID(name)
	if agentID == "" {
		return fmt.Errorf("invalid agent name: %q", name)
	}
	for _, agent := range manifest.Agents {
		if agent.ID == agentID {
			return fmt.Errorf("agent already exists: %s", agentID)
		}
	}

	section := buildAgentTemplate(agentID, name, provider, model)
	if err := appendAgentSection(agentsPath, section); err != nil {
		return err
	}

	fmt.Fprintf(out, "Created agent: %s\n", agentID)
	fmt.Fprintf(out, "  Name: %s\n", name)
	fmt.Fprintf(out, "  Provider: %s\n", provider)
	if model != "" {
		fmt.Fprintf(out, "  Model: %s\n", model)
	}
	fmt.Fprintf(out, "  File: %s\n", agentsPath)

	return nil
}

// printAgentShow prints the agent details.
func printAgentShow(out io.Writer, configPath, agentID string) error {
	manifest, agentsPath, err := loadAgentsManifest(configPath)
	if err != nil {
		return err
	}

	var target *multiagent.AgentDefinition
	for i := range manifest.Agents {
		if manifest.Agents[i].ID == agentID {
			target = &manifest.Agents[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("agent not found: %s (file: %s)", agentID, agentsPath)
	}

	fmt.Fprintf(out, "Agent: %s\n", target.ID)
	fmt.Fprintln(out, "==========")
	fmt.Fprintf(out, "Name: %s\n", target.Name)
	if target.Description != "" {
		fmt.Fprintf(out, "Description: %s\n", target.Description)
	}
	if target.Provider != "" {
		fmt.Fprintf(out, "Provider: %s\n", target.Provider)
	}
	if target.Model != "" {
		fmt.Fprintf(out, "Model: %s\n", target.Model)
	}
	if target.AgentDir != "" {
		fmt.Fprintf(out, "Agent Dir: %s\n", target.AgentDir)
	}
	if target.MaxIterations > 0 {
		fmt.Fprintf(out, "Max Iterations: %d\n", target.MaxIterations)
	}
	fmt.Fprintf(out, "Can Receive Handoffs: %t\n", target.CanReceiveHandoffs)
	fmt.Fprintf(out, "Source: %s\n", agentsPath)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "System Prompt:")
	if strings.TrimSpace(target.SystemPrompt) == "" {
		fmt.Fprintln(out, "  (empty)")
	} else {
		for _, line := range strings.Split(target.SystemPrompt, "\n") {
			if line == "" {
				fmt.Fprintln(out)
				continue
			}
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Tools:")
	if len(target.Tools) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, tool := range target.Tools {
			fmt.Fprintf(out, "  - %s\n", tool)
		}
	}

	if len(target.HandoffRules) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Handoff Rules:")
		for _, rule := range target.HandoffRules {
			fmt.Fprintf(out, "  - To: %s\n", rule.TargetAgentID)
			if rule.ContextMode != "" {
				fmt.Fprintf(out, "    Context: %s\n", rule.ContextMode)
			}
			if rule.SummaryPrompt != "" {
				fmt.Fprintf(out, "    Summary Prompt: %s\n", rule.SummaryPrompt)
			}
			if rule.Message != "" {
				fmt.Fprintf(out, "    Message: %s\n", rule.Message)
			}
			if rule.ReturnToSender {
				fmt.Fprintln(out, "    Return: true")
			}
			if len(rule.Triggers) > 0 {
				for _, trigger := range rule.Triggers {
					fmt.Fprintf(out, "    Trigger: %s %s\n", trigger.Type, trigger.Value)
				}
			}
		}
	}

	return nil
}

func loadAgentsManifest(configPath string) (*multiagent.AgentManifest, string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("load config: %w", err)
	}
	agentsPath := resolveAgentsPath(cfg)
	manifest, err := multiagent.LoadAgentsManifest(agentsPath)
	if err != nil {
		return nil, agentsPath, err
	}
	return manifest, agentsPath, nil
}

func resolveAgentsPath(cfg *config.Config) string {
	root := "."
	agentsFile := "AGENTS.md"
	if cfg != nil {
		if strings.TrimSpace(cfg.Workspace.Path) != "" {
			root = cfg.Workspace.Path
		}
		if strings.TrimSpace(cfg.Workspace.AgentsFile) != "" {
			agentsFile = cfg.Workspace.AgentsFile
		}
	}
	if filepath.IsAbs(agentsFile) {
		return agentsFile
	}
	return filepath.Join(root, agentsFile)
}

func slugifyAgentID(value string) string {
	s := strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func buildAgentTemplate(agentID, name, provider, model string) string {
	var b strings.Builder
	b.WriteString("# Agent: ")
	b.WriteString(agentID)
	b.WriteString("\n")
	if name != "" {
		b.WriteString("Name: ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	b.WriteString("Description: \n")
	if provider != "" {
		b.WriteString("Provider: ")
		b.WriteString(provider)
		b.WriteString("\n")
	}
	if model != "" {
		b.WriteString("Model: ")
		b.WriteString(model)
		b.WriteString("\n")
	}
	b.WriteString("\n## System Prompt\n")
	if name != "" {
		b.WriteString("You are ")
		b.WriteString(name)
		b.WriteString(".\n")
	} else {
		b.WriteString("You are a helpful assistant.\n")
	}
	b.WriteString("\n## Tools\n")
	b.WriteString("- web_search\n")
	return b.String()
}

func appendAgentSection(path, section string) error {
	if path == "" {
		return fmt.Errorf("agent file path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	needsNewline := false
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		needsNewline = true
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open agent file: %w", err)
	}
	defer f.Close()

	if needsNewline {
		if _, err := f.WriteString("\n\n"); err != nil {
			return fmt.Errorf("write agent file: %w", err)
		}
	}
	if _, err := f.WriteString(section); err != nil {
		return fmt.Errorf("write agent file: %w", err)
	}
	return nil
}
