package main

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
)

// =============================================================================
// MCP Command Handlers
// =============================================================================

type mcpStopper interface {
	Stop() error
}

func stopMCPManager(mgr mcpStopper) {
	if mgr == nil {
		return
	}
	if err := mgr.Stop(); err != nil {
		slog.Warn("failed to stop MCP manager", "error", err)
	}
}

// runMcpServers handles the mcp servers command.
func runMcpServers(cmd *cobra.Command, configPath string) error {
	cfg, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	if cfg.MCP.Enabled {
		if err := mgr.Start(cmd.Context()); err != nil {
			return err
		}
	}
	defer stopMCPManager(mgr)

	out := cmd.OutOrStdout()
	statuses := mgr.Status()
	if len(statuses) == 0 {
		fmt.Fprintln(out, "No MCP servers configured.")
		return nil
	}
	fmt.Fprintln(out, "MCP Servers:")
	for _, status := range statuses {
		state := "disconnected"
		if status.Connected {
			state = "connected"
		}
		fmt.Fprintf(out, "  %s (%s) - %s\n", status.ID, status.Name, state)
		if status.Connected {
			fmt.Fprintf(out, "    Tools: %d | Resources: %d | Prompts: %d\n", status.Tools, status.Resources, status.Prompts)
		}
	}
	return nil
}

// runMcpConnect handles the mcp connect command.
func runMcpConnect(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if err := mgr.Connect(cmd.Context(), serverID); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Connected to %s\n", serverID)
	return nil
}

// runMcpTools handles the mcp tools command.
func runMcpTools(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if serverID != "" {
		if err := mgr.Connect(cmd.Context(), serverID); err != nil {
			return err
		}
	} else {
		if err := mgr.Start(cmd.Context()); err != nil {
			return err
		}
	}

	tools := mgr.AllTools()
	out := cmd.OutOrStdout()
	if serverID != "" {
		list := tools[serverID]
		if len(list) == 0 {
			fmt.Fprintf(out, "No tools for %s\n", serverID)
			return nil
		}
		fmt.Fprintf(out, "Tools for %s:\n", serverID)
		for _, tool := range list {
			fmt.Fprintf(out, "  - %s: %s\n", tool.Name, tool.Description)
		}
		return nil
	}
	if len(tools) == 0 {
		fmt.Fprintln(out, "No tools available.")
		return nil
	}
	for id, list := range tools {
		fmt.Fprintf(out, "Tools for %s:\n", id)
		for _, tool := range list {
			fmt.Fprintf(out, "  - %s: %s\n", tool.Name, tool.Description)
		}
	}
	return nil
}

// runMcpCall handles the mcp call command.
func runMcpCall(cmd *cobra.Command, configPath, qualifiedName string, rawArgs []string) error {
	serverID, toolName, err := parseMCPQualifiedName(qualifiedName)
	if err != nil {
		return err
	}
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if err := mgr.Connect(cmd.Context(), serverID); err != nil {
		return err
	}
	toolArgs, err := parseAnyArgs(rawArgs)
	if err != nil {
		return err
	}
	result, err := mgr.CallTool(cmd.Context(), serverID, toolName, toolArgs)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if result == nil || len(result.Content) == 0 {
		fmt.Fprintln(out, "No result.")
		return nil
	}
	for _, item := range result.Content {
		if item.Type == "text" {
			fmt.Fprintln(out, item.Text)
			continue
		}
		payload, err := json.Marshal(item)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(payload))
	}
	return nil
}

// runMcpResources handles the mcp resources command.
func runMcpResources(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if serverID != "" {
		if err := mgr.Connect(cmd.Context(), serverID); err != nil {
			return err
		}
	} else {
		if err := mgr.Start(cmd.Context()); err != nil {
			return err
		}
	}

	resources := mgr.AllResources()
	out := cmd.OutOrStdout()
	if serverID != "" {
		list := resources[serverID]
		if len(list) == 0 {
			fmt.Fprintf(out, "No resources for %s\n", serverID)
			return nil
		}
		fmt.Fprintf(out, "Resources for %s:\n", serverID)
		for _, res := range list {
			fmt.Fprintf(out, "  - %s (%s)\n", res.URI, res.Name)
		}
		return nil
	}
	if len(resources) == 0 {
		fmt.Fprintln(out, "No resources available.")
		return nil
	}
	for id, list := range resources {
		fmt.Fprintf(out, "Resources for %s:\n", id)
		for _, res := range list {
			fmt.Fprintf(out, "  - %s (%s)\n", res.URI, res.Name)
		}
	}
	return nil
}

// runMcpRead handles the mcp read command.
func runMcpRead(cmd *cobra.Command, configPath, serverID, uri string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if err := mgr.Connect(cmd.Context(), serverID); err != nil {
		return err
	}
	contents, err := mgr.ReadResource(cmd.Context(), serverID, uri)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(contents) == 0 {
		fmt.Fprintln(out, "No content.")
		return nil
	}
	payload, err := json.Marshal(contents)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(payload))
	return nil
}

// runMcpPrompts handles the mcp prompts command.
func runMcpPrompts(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if serverID != "" {
		if err := mgr.Connect(cmd.Context(), serverID); err != nil {
			return err
		}
	} else {
		if err := mgr.Start(cmd.Context()); err != nil {
			return err
		}
	}

	prompts := mgr.AllPrompts()
	out := cmd.OutOrStdout()
	if serverID != "" {
		list := prompts[serverID]
		if len(list) == 0 {
			fmt.Fprintf(out, "No prompts for %s\n", serverID)
			return nil
		}
		fmt.Fprintf(out, "Prompts for %s:\n", serverID)
		for _, prompt := range list {
			fmt.Fprintf(out, "  - %s: %s\n", prompt.Name, prompt.Description)
		}
		return nil
	}
	if len(prompts) == 0 {
		fmt.Fprintln(out, "No prompts available.")
		return nil
	}
	for id, list := range prompts {
		fmt.Fprintf(out, "Prompts for %s:\n", id)
		for _, prompt := range list {
			fmt.Fprintf(out, "  - %s: %s\n", prompt.Name, prompt.Description)
		}
	}
	return nil
}

// runMcpPrompt handles the mcp prompt command.
func runMcpPrompt(cmd *cobra.Command, configPath, qualifiedName string, rawArgs []string) error {
	serverID, promptName, err := parseMCPQualifiedName(qualifiedName)
	if err != nil {
		return err
	}
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if err := mgr.Connect(cmd.Context(), serverID); err != nil {
		return err
	}
	promptArgs, err := parseStringArgs(rawArgs)
	if err != nil {
		return err
	}
	result, err := mgr.GetPrompt(cmd.Context(), serverID, promptName, promptArgs)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(payload))
	return nil
}
