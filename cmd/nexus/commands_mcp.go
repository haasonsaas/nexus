package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// MCP Commands
// =============================================================================

// buildMcpCmd creates the "mcp" command group for MCP servers/tools.
func buildMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP servers and tools",
		Long: `Manage MCP servers and interact with MCP tools/resources/prompts.

Use "nexus mcp servers" to list configured servers.`,
	}
	cmd.AddCommand(
		buildMcpServersCmd(),
		buildMcpConnectCmd(),
		buildMcpToolsCmd(),
		buildMcpCallCmd(),
		buildMcpResourcesCmd(),
		buildMcpReadCmd(),
		buildMcpPromptsCmd(),
		buildMcpPromptCmd(),
	)
	return cmd
}

func buildMcpServersCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "servers",
		Short: "List configured MCP servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpServers(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildMcpConnectCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "connect <server-id>",
		Short: "Connect to an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpConnect(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildMcpToolsCmd() *cobra.Command {
	var (
		configPath string
		serverID   string
	)
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List MCP tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpTools(cmd, configPath, serverID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&serverID, "server", "", "Server ID (optional)")
	return cmd
}

func buildMcpCallCmd() *cobra.Command {
	var (
		configPath string
		rawArgs    []string
	)
	cmd := &cobra.Command{
		Use:   "call <server.tool>",
		Short: "Call an MCP tool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpCall(cmd, configPath, args[0], rawArgs)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringArrayVar(&rawArgs, "arg", nil, "Tool argument (key=value)")
	return cmd
}

func buildMcpResourcesCmd() *cobra.Command {
	var (
		configPath string
		serverID   string
	)
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "List MCP resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpResources(cmd, configPath, serverID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&serverID, "server", "", "Server ID (optional)")
	return cmd
}

func buildMcpReadCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "read <server-id> <uri>",
		Short: "Read an MCP resource",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpRead(cmd, configPath, args[0], args[1])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildMcpPromptsCmd() *cobra.Command {
	var (
		configPath string
		serverID   string
	)
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "List MCP prompts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpPrompts(cmd, configPath, serverID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&serverID, "server", "", "Server ID (optional)")
	return cmd
}

func buildMcpPromptCmd() *cobra.Command {
	var (
		configPath string
		rawArgs    []string
	)
	cmd := &cobra.Command{
		Use:   "prompt <server.prompt>",
		Short: "Fetch an MCP prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpPrompt(cmd, configPath, args[0], rawArgs)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringArrayVar(&rawArgs, "arg", nil, "Prompt argument (key=value)")
	return cmd
}
