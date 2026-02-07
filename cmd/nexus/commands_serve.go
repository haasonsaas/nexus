package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Serve Command
// =============================================================================

// buildServeCmd creates the "serve" command that starts the gateway server.
// This is the primary command for running Nexus in production.
func buildServeCmd() *cobra.Command {
	var (
		configPath string
		debug      bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Nexus gateway server",
		Long: `Start the Nexus gateway server with all configured channels and providers.

The server will:
1. Load configuration from the specified file (or nexus.yaml)
2. Initialize database connections
3. Start all enabled channel adapters (Telegram, Discord, Slack)
4. Initialize LLM providers (Anthropic, OpenAI)
5. Start the gRPC server for API access
6. Start the HTTP server for health checks and metrics

Graceful shutdown is handled on SIGINT/SIGTERM signals.`,
		Example: `  # Start with default config
  nexus serve

  # Start with custom config
  nexus serve --config /etc/nexus/production.yaml

  # Start with debug logging
  nexus serve --debug`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return runServe(cmd.Context(), configPath, debug)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(),
		"Path to YAML configuration file")
	cmd.Flags().BoolVarP(&debug, "debug", "d", false,
		"Enable debug logging (verbose output)")

	return cmd
}

// =============================================================================
// Service Commands
// =============================================================================

// buildServiceCmd creates the "service" command group.
func buildServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage service installation files",
	}
	cmd.AddCommand(buildServiceInstallCmd(), buildServiceRepairCmd(), buildServiceStatusCmd())
	return cmd
}

func buildServiceInstallCmd() *cobra.Command {
	var configPath string
	var restart bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a user-level service file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceInstall(cmd, configPath, restart)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVar(&restart, "restart", true, "Restart the service after writing the file")
	return cmd
}

func buildServiceRepairCmd() *cobra.Command {
	var configPath string
	var restart bool
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Rewrite the user-level service file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceRepair(cmd, configPath, restart)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVar(&restart, "restart", true, "Restart the service after writing the file")
	return cmd
}

func buildServiceStatusCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show service audit details",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceStatus(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}
