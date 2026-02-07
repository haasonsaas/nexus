package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Channel Commands
// =============================================================================

// buildChannelsCmd creates the "channels" command group for managing messaging channels.
func buildChannelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage messaging channels",
		Long: `View and manage messaging channel integrations.

Nexus supports multiple messaging platforms:
- Telegram: Full bot API with inline keyboards and media
- Discord: Slash commands, threads, and rich embeds
- Slack: Socket Mode with Block Kit formatting`,
	}

	cmd.AddCommand(buildChannelsListCmd())
	cmd.AddCommand(buildChannelsStatusCmd())
	cmd.AddCommand(buildChannelsTestCmd())
	cmd.AddCommand(buildChannelsLoginCmd())
	cmd.AddCommand(buildChannelsEnableCmd())
	cmd.AddCommand(buildChannelsDisableCmd())
	cmd.AddCommand(buildChannelsValidateCmd())
	cmd.AddCommand(buildChannelsSetupCmd())

	return cmd
}

func buildChannelsListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured channels",
		Long:  "Display all messaging channels defined in the configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			// Inline handler for this simple command
			cfg, err := loadConfigForChannels(configPath)
			if err != nil {
				return err
			}
			printChannelsList(cmd.OutOrStdout(), cfg)
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

func buildChannelsLoginCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Validate channel credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := loadConfigForChannels(configPath)
			if err != nil {
				return err
			}
			printChannelsLogin(cmd.OutOrStdout(), cfg)
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

func buildChannelsStatusCmd() *cobra.Command {
	var (
		configPath string
		serverAddr string
		token      string
		apiKey     string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show channel connection status",
		Long:  "Display the current connection status of all enabled channels.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printChannelsStatus(cmd.Context(), cmd.OutOrStdout(), configPath, serverAddr, token, apiKey)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVar(&serverAddr, "server", "", "Nexus HTTP server address (default from config)")
	cmd.Flags().StringVar(&token, "token", "", "JWT bearer token for server auth")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for server auth")

	return cmd
}

func buildChannelsTestCmd() *cobra.Command {
	var (
		configPath string
		serverAddr string
		token      string
		apiKey     string
		channelID  string
		message    string
	)

	cmd := &cobra.Command{
		Use:   "test [channel]",
		Short: "Test channel connectivity",
		Long: `Validate channel connectivity using the running server.

This command queries the live channel status from the Nexus HTTP API
and reports connection and health details.`,
		Example: `  # Test Telegram connection
  nexus channels test telegram

  # Test Discord connection
  nexus channels test discord`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printChannelTest(cmd.Context(), cmd.OutOrStdout(), configPath, serverAddr, token, apiKey, args[0], channelID, message)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVar(&serverAddr, "server", "", "Nexus HTTP server address (default from config)")
	cmd.Flags().StringVar(&token, "token", "", "JWT bearer token for server auth")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for server auth")
	cmd.Flags().StringVar(&channelID, "channel-id", "", "Channel identifier to send test message to")
	cmd.Flags().StringVar(&message, "message", "Nexus test message", "Test message content")

	return cmd
}

func buildChannelsEnableCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "enable <channel>",
		Short: "Enable a channel",
		Long:  "Enable a messaging channel in the configuration.",
		Example: `  # Enable Telegram
  nexus channels enable telegram

  # Enable Discord
  nexus channels enable discord`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return runChannelsEnable(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

func buildChannelsDisableCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "disable <channel>",
		Short: "Disable a channel",
		Long:  "Disable a messaging channel in the configuration.",
		Example: `  # Disable Telegram
  nexus channels disable telegram`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return runChannelsDisable(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

func buildChannelsValidateCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate [channel]",
		Short: "Validate channel configuration",
		Long:  "Validate that a channel has all required credentials configured.",
		Example: `  # Validate all channels
  nexus channels validate

  # Validate specific channel
  nexus channels validate telegram`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			channel := ""
			if len(args) > 0 {
				channel = args[0]
			}
			return runChannelsValidate(cmd, configPath, channel)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

func buildChannelsSetupCmd() *cobra.Command {
	var (
		configPath string
		serverAddr string
		edgeID     string
		saveConfig bool
	)

	cmd := &cobra.Command{
		Use:   "setup [channel]",
		Short: "Interactive channel setup wizard",
		Long: `Guide through the setup process for a messaging channel.

This command provides an interactive wizard that walks you through:
- Entering required credentials (API tokens, app IDs)
- OAuth flows (for platforms that support it)
- QR code scanning (for WhatsApp, requires edge daemon)
- Validation and testing of the connection

If no channel is specified, displays available channels to set up.`,
		Example: `  # List available channels
  nexus channels setup

  # Set up Telegram
  nexus channels setup telegram

  # Set up WhatsApp (requires edge daemon)
  nexus channels setup whatsapp --edge-id my-laptop

  # Set up and save to config
  nexus channels setup telegram --save`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			channel := ""
			if len(args) > 0 {
				channel = args[0]
			}
			return runChannelsSetup(cmd, configPath, serverAddr, channel, edgeID, saveConfig)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVar(&serverAddr, "server", "localhost:50051", "Nexus server address for provisioning")
	cmd.Flags().StringVar(&edgeID, "edge-id", "", "Edge daemon ID for QR code flows")
	cmd.Flags().BoolVar(&saveConfig, "save", true, "Save credentials to config file on success")
	return cmd
}
