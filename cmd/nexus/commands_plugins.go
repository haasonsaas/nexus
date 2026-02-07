package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Plugin Commands
// =============================================================================

// buildPluginsCmd creates the "plugins" command group for marketplace operations.
func buildPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugins",
		Short: "Manage marketplace plugins",
		Long: `Manage plugins from the Nexus plugin marketplace.

Commands for searching, installing, updating, and managing plugins.
Plugins extend Nexus with additional channels, tools, and integrations.

Plugin store: ~/.nexus/plugins/
Default registry: https://plugins.nexus.dev`,
	}
	cmd.AddCommand(
		buildPluginsSearchCmd(),
		buildPluginsInstallCmd(),
		buildPluginsListCmd(),
		buildPluginsUpdateCmd(),
		buildPluginsUninstallCmd(),
		buildPluginsVerifyCmd(),
		buildPluginsInfoCmd(),
		buildPluginsEnableCmd(),
		buildPluginsDisableCmd(),
	)
	return cmd
}

func buildPluginsSearchCmd() *cobra.Command {
	var (
		configPath string
		category   string
		limit      int
	)
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search for plugins in the marketplace",
		Long: `Search for plugins in the configured registries.

Examples:
  nexus plugins search slack
  nexus plugins search --category channels
  nexus plugins search discord --limit 10`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runPluginsSearch(cmd, configPath, query, category, limit)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&category, "category", "", "Filter by category (channels, tools, integrations)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")
	return cmd
}

func buildPluginsInstallCmd() *cobra.Command {
	var (
		configPath string
		version    string
		force      bool
		skipVerify bool
		autoUpdate bool
	)
	cmd := &cobra.Command{
		Use:   "install [plugin-id]",
		Short: "Install a plugin from the marketplace",
		Long: `Install a plugin from the configured registries.

Examples:
  nexus plugins install nexus/slack-enhanced
  nexus plugins install nexus/discord-voice --version 1.2.0
  nexus plugins install my-plugin --auto-update`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsInstall(cmd, configPath, args[0], version, force, skipVerify, autoUpdate)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&version, "version", "", "Specific version to install")
	cmd.Flags().BoolVar(&force, "force", false, "Force reinstall if already installed")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip signature verification (not recommended)")
	cmd.Flags().BoolVar(&autoUpdate, "auto-update", false, "Enable automatic updates")
	return cmd
}

func buildPluginsListCmd() *cobra.Command {
	var configPath string
	var showAll bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsList(cmd, configPath, showAll)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show detailed information")
	return cmd
}

func buildPluginsUpdateCmd() *cobra.Command {
	var (
		configPath string
		all        bool
		force      bool
		skipVerify bool
	)
	cmd := &cobra.Command{
		Use:   "update [plugin-id]",
		Short: "Update a plugin or all plugins",
		Long: `Update an installed plugin to the latest version.

Examples:
  nexus plugins update nexus/slack-enhanced
  nexus plugins update --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginID := ""
			if len(args) > 0 {
				pluginID = args[0]
			}
			return runPluginsUpdate(cmd, configPath, pluginID, all, force, skipVerify)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVar(&all, "all", false, "Update all plugins with updates available")
	cmd.Flags().BoolVar(&force, "force", false, "Force update even if already at latest version")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip signature verification")
	return cmd
}

func buildPluginsUninstallCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "uninstall [plugin-id]",
		Short: "Uninstall a plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsUninstall(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildPluginsVerifyCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "verify [plugin-id]",
		Short: "Verify an installed plugin's integrity",
		Long: `Verify an installed plugin's checksum and signature.

This checks that the plugin binary hasn't been modified since installation.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsVerify(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildPluginsInfoCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "info [plugin-id]",
		Short: "Show detailed plugin information",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pluginID := ""
			if len(args) > 0 {
				pluginID = args[0]
			}
			return runPluginsInfo(cmd, configPath, pluginID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildPluginsEnableCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "enable [plugin-id]",
		Short: "Enable a plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsEnable(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildPluginsDisableCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "disable [plugin-id]",
		Short: "Disable a plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsDisable(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}
