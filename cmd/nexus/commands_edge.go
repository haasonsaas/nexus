package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Status Command
// =============================================================================

// buildStatusCmd creates the "status" command for system health overview.
func buildStatusCmd() *cobra.Command {
	var (
		configPath string
		serverAddr string
		jsonOutput bool
		token      string
		apiKey     string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show system status",
		Long: `Display comprehensive system health and status information.

Shows the status of all components including:
- Database connectivity
- Channel adapter connections
- LLM provider availability
- Tool executor status
- Resource utilization`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printSystemStatus(cmd.Context(), cmd.OutOrStdout(), jsonOutput, configPath, serverAddr, token, apiKey)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVar(&serverAddr, "server", "", "Nexus HTTP server address (default from config)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().StringVar(&token, "token", "", "JWT bearer token for server auth")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for server auth")

	return cmd
}

// =============================================================================
// Edge Commands
// =============================================================================

// buildEdgeCmd creates the "edge" command group for edge daemon management.
func buildEdgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edge daemons",
		Long: `Manage edge daemons that provide local capabilities.

Edge daemons connect from user machines to provide:
- Device access (camera, screen, location)
- Browser relay (attached Chrome sessions)
- Edge-only channels (iMessage, local Signal bridges)
- Local filesystem and command execution

Use "nexus edge status" to see connected edges.`,
	}
	cmd.AddCommand(
		buildEdgeStatusCmd(),
		buildEdgeListCmd(),
		buildEdgeToolsCmd(),
		buildEdgeApproveCmd(),
		buildEdgeRevokeCmd(),
	)
	return cmd
}

func buildEdgeStatusCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "status [edge-id]",
		Short: "Show edge daemon status",
		Long: `Show the status of a connected edge daemon.

If no edge-id is provided, shows a summary of all connected edges.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			edgeID := ""
			if len(args) > 0 {
				edgeID = args[0]
			}
			return runEdgeStatus(cmd, configPath, edgeID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildEdgeListCmd() *cobra.Command {
	var configPath string
	var showTools bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List connected edge daemons",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeList(cmd, configPath, showTools)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVar(&showTools, "tools", false, "Show tools for each edge")
	return cmd
}

func buildEdgeToolsCmd() *cobra.Command {
	var configPath string
	var edgeID string
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List tools from connected edges",
		Long: `List all tools available from connected edge daemons.

Use --edge to filter by a specific edge.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeTools(cmd, configPath, edgeID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&edgeID, "edge", "", "Filter by edge ID")
	return cmd
}

func buildEdgeApproveCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "approve [edge-id]",
		Short: "Approve a pending edge (TOFU)",
		Long: `Approve a pending edge connection request.

When using Trust-On-First-Use (TOFU) authentication, new edges
are held pending until manually approved.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeApprove(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildEdgeRevokeCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "revoke [edge-id]",
		Short: "Revoke an approved edge",
		Long:  `Revoke an approved edge, disconnecting it and preventing reconnection.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeRevoke(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}
