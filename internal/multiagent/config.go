package multiagent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads a multi-agent configuration from a YAML file.
func LoadConfig(path string) (*MultiAgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return ParseConfigYAML(data)
}

// ParseConfigYAML parses multi-agent configuration from YAML data.
func ParseConfigYAML(data []byte) (*MultiAgentConfig, error) {
	var config MultiAgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Apply defaults
	if config.MaxHandoffDepth <= 0 {
		config.MaxHandoffDepth = 10
	}
	if config.HandoffTimeout <= 0 {
		config.HandoffTimeout = 5 * time.Minute
	}
	if config.DefaultContextMode == "" {
		config.DefaultContextMode = ContextFull
	}

	// Validate agents
	for i := range config.Agents {
		if config.Agents[i].ID == "" {
			return nil, fmt.Errorf("agent at index %d has no ID", i)
		}
		if config.Agents[i].Name == "" {
			config.Agents[i].Name = config.Agents[i].ID
		}
	}

	return &config, nil
}

// SaveConfig saves a multi-agent configuration to a YAML file.
func SaveConfig(config *MultiAgentConfig, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadAgentsManifest loads agent definitions from an AGENTS.md file.
// AGENTS.md is a markdown format for defining agents in a human-readable way.
//
// Format:
//
//	# Agent: agent-id
//	Name: My Agent
//	Description: What this agent does
//
//	## System Prompt
//	Your system prompt here...
//
//	## Tools
//	- tool1
//	- tool2
//
//	## Handoffs
//	- To: other-agent
//	  Triggers: keyword:help, pattern:.*error.*
//	  Context: summary
func LoadAgentsManifest(path string) (*AgentManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read AGENTS.md: %w", err)
	}

	return ParseAgentsMarkdown(string(data), path)
}

// ParseAgentsMarkdown parses agent definitions from markdown content.
func ParseAgentsMarkdown(content string, source string) (*AgentManifest, error) {
	manifest := &AgentManifest{
		Source: source,
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var currentAgent *AgentDefinition
	var currentSection string
	var sectionContent strings.Builder

	// Regex patterns
	agentHeaderRe := regexp.MustCompile(`^#\s+Agent:\s*(.+)$`)
	sectionHeaderRe := regexp.MustCompile(`^##\s+(.+)$`)
	propertyRe := regexp.MustCompile(`^([A-Za-z_]+):\s*(.*)$`)
	listItemRe := regexp.MustCompile(`^[-*]\s+(.+)$`)

	flushSection := func() {
		if currentAgent == nil || currentSection == "" {
			return
		}

		content := strings.TrimSpace(sectionContent.String())
		switch strings.ToLower(currentSection) {
		case "system prompt", "systemprompt", "prompt":
			currentAgent.SystemPrompt = content
		case "description":
			if currentAgent.Description == "" {
				currentAgent.Description = content
			}
		}
		sectionContent.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()

		// Check for new agent definition
		if matches := agentHeaderRe.FindStringSubmatch(line); len(matches) > 1 {
			// Save previous agent
			if currentAgent != nil {
				flushSection()
				manifest.Agents = append(manifest.Agents, *currentAgent)
			}

			// Start new agent
			agentID := strings.TrimSpace(matches[1])
			currentAgent = &AgentDefinition{
				ID:                 agentID,
				Name:               agentID,
				CanReceiveHandoffs: true,
			}
			currentSection = ""
			continue
		}

		if currentAgent == nil {
			// Skip content before first agent definition
			// Could be global config in the future
			continue
		}

		// Check for section header
		if matches := sectionHeaderRe.FindStringSubmatch(line); len(matches) > 1 {
			flushSection()
			currentSection = strings.TrimSpace(matches[1])
			continue
		}

		// Check for property line (outside of sections)
		if currentSection == "" {
			if matches := propertyRe.FindStringSubmatch(line); len(matches) > 2 {
				key := strings.ToLower(matches[1])
				value := strings.TrimSpace(matches[2])

				switch key {
				case "name":
					currentAgent.Name = value
				case "description":
					currentAgent.Description = value
				case "model":
					currentAgent.Model = value
				case "provider":
					currentAgent.Provider = value
				case "can_receive_handoffs", "canreceivehandoffs":
					currentAgent.CanReceiveHandoffs = strings.ToLower(value) == "true" || value == "yes"
				case "max_iterations", "maxiterations":
					if parsed, err := strconv.Atoi(value); err == nil {
						currentAgent.MaxIterations = parsed
					}
				}
				continue
			}
		}

		// Handle section content
		switch strings.ToLower(currentSection) {
		case "tools":
			if matches := listItemRe.FindStringSubmatch(line); len(matches) > 1 {
				tool := strings.TrimSpace(matches[1])
				currentAgent.Tools = append(currentAgent.Tools, tool)
			}

		case "handoffs", "handoff rules":
			// Parse handoff rules
			rule := parseHandoffLine(line, listItemRe, propertyRe)
			if rule != nil {
				currentAgent.HandoffRules = append(currentAgent.HandoffRules, *rule)
			}

		case "system prompt", "systemprompt", "prompt", "description":
			sectionContent.WriteString(line)
			sectionContent.WriteString("\n")
		}
	}

	// Don't forget the last agent
	if currentAgent != nil {
		flushSection()
		manifest.Agents = append(manifest.Agents, *currentAgent)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading markdown: %w", err)
	}

	return manifest, nil
}

// parseHandoffLine parses a handoff rule from a markdown line.
func parseHandoffLine(line string, listItemRe, propertyRe *regexp.Regexp) *HandoffRule {
	// Check for list item format: - To: agent-id, Triggers: keyword:help
	if matches := listItemRe.FindStringSubmatch(line); len(matches) > 1 {
		content := matches[1]
		rule := &HandoffRule{}

		// Parse comma-separated properties
		parts := strings.Split(content, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if idx := strings.Index(part, ":"); idx > 0 {
				key := strings.ToLower(strings.TrimSpace(part[:idx]))
				value := strings.TrimSpace(part[idx+1:])

				switch key {
				case "to", "target":
					rule.TargetAgentID = value
				case "trigger", "triggers":
					rule.Triggers = parseTriggers(value)
				case "context":
					rule.ContextMode = ContextSharingMode(value)
				case "priority":
					if _, err := fmt.Sscanf(value, "%d", &rule.Priority); err != nil {
						rule.Priority = 0
					}
				case "return":
					rule.ReturnToSender = strings.ToLower(value) == "true" || value == "yes"
				case "message":
					rule.Message = value
				}
			}
		}

		if rule.TargetAgentID != "" {
			return rule
		}
	}

	return nil
}

// parseTriggers parses trigger specifications from a string.
// Format: "type:value type:value" or "type:value,type:value"
func parseTriggers(spec string) []RoutingTrigger {
	var triggers []RoutingTrigger

	// Split by space or comma
	parts := regexp.MustCompile(`[\s,]+`).Split(spec, -1)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		trigger := RoutingTrigger{}

		// Check for type:value format
		if idx := strings.Index(part, ":"); idx > 0 {
			triggerType := strings.ToLower(part[:idx])
			value := part[idx+1:]

			switch triggerType {
			case "keyword", "kw":
				trigger.Type = TriggerKeyword
				trigger.Value = value
			case "pattern", "regex":
				trigger.Type = TriggerPattern
				trigger.Value = value
			case "intent":
				trigger.Type = TriggerIntent
				trigger.Value = value
			case "tool":
				trigger.Type = TriggerToolUse
				trigger.Value = value
			case "explicit":
				trigger.Type = TriggerExplicit
				trigger.Value = value
			case "fallback":
				trigger.Type = TriggerFallback
			case "always":
				trigger.Type = TriggerAlways
			case "complete", "task_complete":
				trigger.Type = TriggerTaskComplete
			case "error":
				trigger.Type = TriggerError
			default:
				// Treat as keyword by default
				trigger.Type = TriggerKeyword
				trigger.Value = part
			}
		} else {
			// No type specified, treat as keyword
			trigger.Type = TriggerKeyword
			trigger.Value = part
		}

		triggers = append(triggers, trigger)
	}

	return triggers
}

// DiscoverAgentsFiles finds AGENTS.md files in a directory tree.
func DiscoverAgentsFiles(root string) ([]string, error) {
	var files []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		name := strings.ToLower(info.Name())
		if name == "agents.md" || strings.HasSuffix(name, ".agents.md") {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// LoadAllAgentsFiles loads and merges agent definitions from multiple AGENTS.md files.
func LoadAllAgentsFiles(paths []string) (*AgentManifest, error) {
	merged := &AgentManifest{}

	for _, path := range paths {
		manifest, err := LoadAgentsManifest(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", path, err)
		}

		merged.Agents = append(merged.Agents, manifest.Agents...)
	}

	return merged, nil
}

// ConfigFromManifest creates a MultiAgentConfig from an AgentManifest.
func ConfigFromManifest(manifest *AgentManifest) *MultiAgentConfig {
	config := &MultiAgentConfig{
		Agents:             manifest.Agents,
		DefaultContextMode: ContextFull,
		MaxHandoffDepth:    10,
		HandoffTimeout:     5 * time.Minute,
		EnablePeerHandoffs: true,
	}

	// Use first agent as default if not specified
	if len(manifest.Agents) > 0 {
		config.DefaultAgentID = manifest.Agents[0].ID
	}

	return config
}

// ValidateConfig validates a multi-agent configuration.
func ValidateConfig(config *MultiAgentConfig) []error {
	var errors []error

	if config == nil {
		return []error{fmt.Errorf("config is nil")}
	}

	// Check for duplicate agent IDs
	agentIDs := make(map[string]bool)
	for _, agent := range config.Agents {
		if agent.ID == "" {
			errors = append(errors, fmt.Errorf("agent has empty ID"))
			continue
		}
		if agentIDs[agent.ID] {
			errors = append(errors, fmt.Errorf("duplicate agent ID: %s", agent.ID))
		}
		agentIDs[agent.ID] = true
	}

	// Validate default agent exists
	if config.DefaultAgentID != "" && !agentIDs[config.DefaultAgentID] {
		errors = append(errors, fmt.Errorf("default agent not found: %s", config.DefaultAgentID))
	}

	// Validate supervisor agent exists
	if config.SupervisorAgentID != "" && !agentIDs[config.SupervisorAgentID] {
		errors = append(errors, fmt.Errorf("supervisor agent not found: %s", config.SupervisorAgentID))
	}

	// Validate handoff targets exist
	for _, agent := range config.Agents {
		for _, rule := range agent.HandoffRules {
			if rule.TargetAgentID != "" && !agentIDs[rule.TargetAgentID] {
				errors = append(errors, fmt.Errorf("agent %s: handoff target not found: %s",
					agent.ID, rule.TargetAgentID))
			}
		}
	}

	// Validate global handoff rules
	for i, rule := range config.GlobalHandoffRules {
		if rule.TargetAgentID != "" && !agentIDs[rule.TargetAgentID] {
			errors = append(errors, fmt.Errorf("global rule %d: handoff target not found: %s",
				i, rule.TargetAgentID))
		}
	}

	return errors
}

// ExampleAgentsMD returns an example AGENTS.md content.
func ExampleAgentsMD() string {
	return `# Multi-Agent Configuration
#
# This file defines agents using markdown format.
# Each agent is defined by a level-1 header starting with "Agent:".

# Agent: coordinator
Name: Coordinator
Description: Routes requests to appropriate specialists

## System Prompt
You are a coordinator agent. Your job is to understand user requests
and route them to the most appropriate specialist agent.

Use the handoff tool to transfer control when needed.

## Tools
- handoff
- list_agents

## Handoffs
- To: code-expert, Triggers: keyword:code keyword:programming, Context: summary
- To: research-expert, Triggers: keyword:research keyword:find, Context: full

---

# Agent: code-expert
Name: Code Expert
Description: Specializes in writing and reviewing code

## System Prompt
You are a code expert. Help users with programming tasks, code review,
and software development questions.

## Tools
- exec
- read
- write
- edit

## Handoffs
- To: coordinator, Triggers: task_complete error, Return: true

---

# Agent: research-expert
Name: Research Expert
Description: Specializes in web research and information gathering

## System Prompt
You are a research expert. Help users find information, summarize content,
and answer questions requiring web research.

## Tools
- websearch
- webfetch

## Handoffs
- To: coordinator, Triggers: task_complete error, Return: true
`
}
