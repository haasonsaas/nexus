package links

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Default timeout
const DefaultLinkTimeoutSeconds = 30

// LinkUnderstandingResult is the output of link processing.
type LinkUnderstandingResult struct {
	URLs    []string
	Outputs []string
}

// LinkModelConfig defines a link processing model.
type LinkModelConfig struct {
	Type           string   `yaml:"type"` // "cli"
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
}

// ScopeConfig defines scope configuration for link understanding.
type ScopeConfig struct {
	Mode      string   `yaml:"mode"` // "allowlist", "denylist", "all"
	Allowlist []string `yaml:"allowlist"`
	Denylist  []string `yaml:"denylist"`
}

// LinkToolsConfig is the configuration for link understanding.
type LinkToolsConfig struct {
	Enabled        bool              `yaml:"enabled"`
	Models         []LinkModelConfig `yaml:"models"`
	MaxLinks       int               `yaml:"max_links"`
	TimeoutSeconds int               `yaml:"timeout_seconds"`
	Scope          *ScopeConfig      `yaml:"scope"`
}

// MsgContext provides context about the message being processed.
type MsgContext struct {
	Channel   string
	SessionID string
	PeerID    string
	AgentID   string
}

// RunnerParams for link understanding.
type RunnerParams struct {
	Config  *LinkToolsConfig
	Context *MsgContext
	Message string
}

// RunLinkUnderstanding processes links in a message.
func RunLinkUnderstanding(ctx context.Context, params RunnerParams) (*LinkUnderstandingResult, error) {
	config := params.Config
	if config == nil || !config.Enabled {
		return &LinkUnderstandingResult{}, nil
	}

	// Check scope (channel/session allowed)
	if decision := resolveScopeDecision(config.Scope, params.Context); decision == "deny" {
		return &LinkUnderstandingResult{}, nil
	}

	// Extract links
	maxLinks := config.MaxLinks
	if maxLinks <= 0 {
		maxLinks = DefaultMaxLinks
	}
	links := ExtractLinksFromMessage(params.Message, maxLinks)
	if len(links) == 0 {
		return &LinkUnderstandingResult{}, nil
	}

	entries := config.Models
	if len(entries) == 0 {
		return &LinkUnderstandingResult{URLs: links}, nil
	}

	// Process each link
	var outputs []string
	for _, url := range links {
		output, err := runLinkEntries(ctx, entries, url, params.Context, config)
		if err == nil && output != "" {
			outputs = append(outputs, output)
		}
	}

	return &LinkUnderstandingResult{
		URLs:    links,
		Outputs: outputs,
	}, nil
}

// runLinkEntries tries each model config until one succeeds.
func runLinkEntries(ctx context.Context, entries []LinkModelConfig, url string, msgCtx *MsgContext, config *LinkToolsConfig) (string, error) {
	var lastErr error

	for _, entry := range entries {
		output, err := runCliEntry(ctx, entry, url, msgCtx, config)
		if err != nil {
			lastErr = err
			continue
		}
		if output != "" {
			return output, nil
		}
	}

	return "", lastErr
}

// runCliEntry executes a CLI command for link processing.
func runCliEntry(ctx context.Context, entry LinkModelConfig, url string, msgCtx *MsgContext, config *LinkToolsConfig) (string, error) {
	if entry.Type != "" && entry.Type != "cli" {
		return "", nil
	}

	command := strings.TrimSpace(entry.Command)
	if command == "" {
		return "", nil
	}

	// Apply template to args (replace {{LinkUrl}}, etc.)
	args := applyTemplateToArgs(entry.Args, msgCtx, url)

	// Execute with timeout
	timeout := resolveTimeout(entry.TimeoutSeconds, config.TimeoutSeconds)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// applyTemplateToArgs replaces template variables in args.
func applyTemplateToArgs(args []string, msgCtx *MsgContext, url string) []string {
	result := make([]string, len(args))
	for i, arg := range args {
		result[i] = applyTemplate(arg, msgCtx, url)
	}
	return result
}

// applyTemplate replaces template variables in a string.
func applyTemplate(s string, msgCtx *MsgContext, url string) string {
	s = strings.ReplaceAll(s, "{{LinkUrl}}", url)
	s = strings.ReplaceAll(s, "{{URL}}", url)
	s = strings.ReplaceAll(s, "{{url}}", url)

	if msgCtx != nil {
		s = strings.ReplaceAll(s, "{{Channel}}", msgCtx.Channel)
		s = strings.ReplaceAll(s, "{{SessionID}}", msgCtx.SessionID)
		s = strings.ReplaceAll(s, "{{PeerID}}", msgCtx.PeerID)
		s = strings.ReplaceAll(s, "{{AgentID}}", msgCtx.AgentID)
	}

	return s
}

// resolveTimeout resolves the timeout from entry and config defaults.
func resolveTimeout(entryTimeout, configTimeout int) time.Duration {
	if entryTimeout > 0 {
		return time.Duration(entryTimeout) * time.Second
	}
	if configTimeout > 0 {
		return time.Duration(configTimeout) * time.Second
	}
	return time.Duration(DefaultLinkTimeoutSeconds) * time.Second
}
