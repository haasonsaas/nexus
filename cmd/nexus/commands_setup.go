package main

import (
	"github.com/haasonsaas/nexus/internal/onboard"
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Setup and Onboard Commands
// =============================================================================

// buildSetupCmd creates the "setup" command for initializing a workspace.
func buildSetupCmd() *cobra.Command {
	var (
		configPath   string
		workspaceDir string
		overwrite    bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize a workspace with bootstrap files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd, configPath, workspaceDir, overwrite)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(),
		"Path to YAML configuration file (optional)")
	cmd.Flags().StringVar(&workspaceDir, "workspace", "",
		"Workspace directory to initialize (overrides config)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false,
		"Overwrite existing bootstrap files")

	return cmd
}

// buildOnboardCmd creates the "onboard" command for guided config creation.
func buildOnboardCmd() *cobra.Command {
	var opts onboard.Options
	var nonInteractive bool
	var setupWorkspace bool

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Create a Nexus config file with guided prompts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOnboard(cmd, &opts, nonInteractive, setupWorkspace)
		},
	}

	cmd.Flags().StringVarP(&opts.ConfigPath, "config", "c", profile.DefaultConfigPath(), "Path to write the config file")
	cmd.Flags().StringVar(&opts.DatabaseURL, "database-url", "", "Database URL")
	cmd.Flags().StringVar(&opts.JWTSecret, "jwt-secret", "", "JWT secret (generated if empty)")
	cmd.Flags().StringVar(&opts.Provider, "provider", "anthropic", "Default LLM provider")
	cmd.Flags().StringVar(&opts.ProviderKey, "provider-key", "", "Provider API key")
	cmd.Flags().BoolVar(&opts.EnableTelegram, "enable-telegram", false, "Enable Telegram channel")
	cmd.Flags().StringVar(&opts.TelegramToken, "telegram-token", "", "Telegram bot token")
	cmd.Flags().BoolVar(&opts.EnableDiscord, "enable-discord", false, "Enable Discord channel")
	cmd.Flags().StringVar(&opts.DiscordToken, "discord-token", "", "Discord bot token")
	cmd.Flags().StringVar(&opts.DiscordAppID, "discord-app-id", "", "Discord app ID")
	cmd.Flags().BoolVar(&opts.EnableSlack, "enable-slack", false, "Enable Slack channel")
	cmd.Flags().StringVar(&opts.SlackBotToken, "slack-bot-token", "", "Slack bot token")
	cmd.Flags().StringVar(&opts.SlackAppToken, "slack-app-token", "", "Slack app token")
	cmd.Flags().StringVar(&opts.SlackSecret, "slack-signing-secret", "", "Slack signing secret")
	cmd.Flags().StringVar(&opts.WorkspacePath, "workspace", "", "Workspace path to set in config")
	cmd.Flags().BoolVar(&setupWorkspace, "setup-workspace", false, "Create workspace bootstrap files")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable prompts and use flags only")

	return cmd
}

// buildAuthCmd creates the "auth" command group.
func buildAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage provider credentials",
	}
	cmd.AddCommand(buildAuthSetCmd())
	return cmd
}

func buildAuthSetCmd() *cobra.Command {
	var (
		configPath string
		provider   string
		apiKey     string
		setDefault bool
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set provider credentials in the config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthSet(cmd, configPath, provider, apiKey, setDefault)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&provider, "provider", "anthropic", "Provider to update")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Provider API key")
	cmd.Flags().BoolVar(&setDefault, "default", false, "Set as default provider")

	return cmd
}
