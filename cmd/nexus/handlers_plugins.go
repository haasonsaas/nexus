package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/marketplace"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
	"github.com/spf13/cobra"
)

// =============================================================================
// Plugin Command Handlers
// =============================================================================

// runPluginsSearch handles the plugins search command.
func runPluginsSearch(cmd *cobra.Command, configPath, query, category string, limit int) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	opts := marketplace.DefaultSearchOptions()
	opts.Category = category
	if limit > 0 {
		opts.Limit = limit
	}

	results, err := mgr.Search(cmd.Context(), query, opts)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(results) == 0 {
		fmt.Fprintln(out, "No plugins found.")
		return nil
	}

	fmt.Fprintf(out, "Found %d plugins:\n\n", len(results))
	for _, result := range results {
		plugin := result.Plugin
		status := ""
		if result.Installed {
			if result.UpdateAvailable {
				status = fmt.Sprintf(" [installed: %s, update available: %s]", result.InstalledVersion, plugin.Version)
			} else {
				status = fmt.Sprintf(" [installed: %s]", result.InstalledVersion)
			}
		}

		fmt.Fprintf(out, "  %s (%s)%s\n", plugin.ID, plugin.Version, status)
		if plugin.Description != "" {
			desc := plugin.Description
			if len(desc) > 70 {
				desc = desc[:67] + "..."
			}
			fmt.Fprintf(out, "    %s\n", desc)
		}
		if len(plugin.Categories) > 0 {
			fmt.Fprintf(out, "    Categories: %s\n", strings.Join(plugin.Categories, ", "))
		}
		fmt.Fprintln(out)
	}

	return nil
}

// runPluginsInstall handles the plugins install command.
func runPluginsInstall(cmd *cobra.Command, configPath, pluginID, version string, force, skipVerify, autoUpdate bool) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	if err := marketplace.ValidatePluginID(pluginID); err != nil {
		return err
	}

	opts := pluginsdk.InstallOptions{
		Version:    version,
		Force:      force,
		SkipVerify: skipVerify || cfg.Marketplace.SkipVerify,
		AutoUpdate: autoUpdate || cfg.Marketplace.AutoUpdate,
	}

	result, err := mgr.Install(cmd.Context(), pluginID, opts)
	if err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	out := cmd.OutOrStdout()
	if result.Updated {
		fmt.Fprintf(out, "Updated plugin: %s (%s -> %s)\n", pluginID, result.PreviousVersion, result.Plugin.Version)
	} else {
		fmt.Fprintf(out, "Installed plugin: %s (%s)\n", pluginID, result.Plugin.Version)
	}
	fmt.Fprintf(out, "  Path: %s\n", result.Plugin.Path)
	if result.Plugin.Verified {
		fmt.Fprintln(out, "  Verified: yes")
	}

	return nil
}

// runPluginsList handles the plugins list command.
func runPluginsList(cmd *cobra.Command, configPath string, showAll bool) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	pluginsList := mgr.List()
	out := cmd.OutOrStdout()

	if len(pluginsList) == 0 {
		fmt.Fprintln(out, "No plugins installed.")
		fmt.Fprintln(out, "\nUse 'nexus plugins search' to find plugins.")
		return nil
	}

	fmt.Fprintf(out, "Installed plugins (%d):\n\n", len(pluginsList))
	for _, plugin := range pluginsList {
		status := "enabled"
		if !plugin.Enabled {
			status = "disabled"
		}

		autoUpdateStr := ""
		if plugin.AutoUpdate {
			autoUpdateStr = ", auto-update"
		}

		verified := ""
		if plugin.Verified {
			verified = ", verified"
		}

		fmt.Fprintf(out, "  %s (%s) [%s%s%s]\n", plugin.ID, plugin.Version, status, autoUpdateStr, verified)
		if showAll {
			fmt.Fprintf(out, "    Path: %s\n", plugin.Path)
			fmt.Fprintf(out, "    Installed: %s\n", plugin.InstalledAt.Format(time.RFC3339))
			if plugin.Manifest != nil && plugin.Manifest.Description != "" {
				fmt.Fprintf(out, "    %s\n", plugin.Manifest.Description)
			}
		}
	}

	return nil
}

// runPluginsUpdate handles the plugins update command.
func runPluginsUpdate(cmd *cobra.Command, configPath, pluginID string, all, force, skipVerify bool) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	out := cmd.OutOrStdout()

	if all || pluginID == "" {
		// Check for updates first
		updates, err := mgr.CheckUpdates(cmd.Context())
		if err != nil {
			return fmt.Errorf("failed to check updates: %w", err)
		}

		if len(updates) == 0 {
			fmt.Fprintln(out, "All plugins are up to date.")
			return nil
		}

		fmt.Fprintf(out, "Updates available for %d plugins:\n", len(updates))
		for id, newVersion := range updates {
			installed, _ := mgr.Get(id)
			fmt.Fprintf(out, "  %s: %s -> %s\n", id, installed.Version, newVersion)
		}
		fmt.Fprintln(out)

		results, err := mgr.UpdateAll(cmd.Context())
		if err != nil {
			return fmt.Errorf("update failed: %w", err)
		}

		if len(results) == 0 {
			fmt.Fprintln(out, "No plugins were updated.")
		} else {
			fmt.Fprintf(out, "Updated %d plugins.\n", len(results))
		}
		return nil
	}

	// Update specific plugin
	opts := pluginsdk.UpdateOptions{
		Force:      force,
		SkipVerify: skipVerify || cfg.Marketplace.SkipVerify,
	}

	result, err := mgr.Update(cmd.Context(), pluginID, opts)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Fprintf(out, "Updated plugin: %s (%s -> %s)\n", pluginID, result.PreviousVersion, result.Plugin.Version)
	return nil
}

// runPluginsUninstall handles the plugins uninstall command.
func runPluginsUninstall(cmd *cobra.Command, configPath, pluginID string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	if err := mgr.Uninstall(cmd.Context(), pluginID); err != nil {
		return fmt.Errorf("uninstall failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled plugin: %s\n", pluginID)
	return nil
}

// runPluginsVerify handles the plugins verify command.
func runPluginsVerify(cmd *cobra.Command, configPath, pluginID string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	result, err := mgr.Verify(cmd.Context(), pluginID)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	out := cmd.OutOrStdout()
	if result.Valid {
		fmt.Fprintf(out, "Plugin '%s' verification PASSED\n", pluginID)
		fmt.Fprintf(out, "  Checksum: %s\n", result.ComputedChecksum)
		if result.SignedBy != "" {
			fmt.Fprintf(out, "  Signed by: %s\n", result.SignedBy)
		}
	} else {
		fmt.Fprintf(out, "Plugin '%s' verification FAILED\n", pluginID)
		if result.Error != nil {
			fmt.Fprintf(out, "  Error: %s\n", result.Error)
		}
		return fmt.Errorf("verification failed")
	}

	return nil
}

// runPluginsInfo handles the plugins info command.
func runPluginsInfo(cmd *cobra.Command, configPath, pluginID string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	out := cmd.OutOrStdout()

	// If no plugin ID, show marketplace info
	if pluginID == "" {
		info := mgr.Info()
		fmt.Fprintln(out, "Marketplace Information")
		fmt.Fprintln(out, "=======================")
		fmt.Fprintf(out, "Store Path:      %s\n", info.StorePath)
		fmt.Fprintf(out, "Platform:        %s\n", info.Platform)
		fmt.Fprintf(out, "Installed:       %d plugins\n", info.InstalledCount)
		fmt.Fprintf(out, "Enabled:         %d plugins\n", info.EnabledCount)
		fmt.Fprintf(out, "Auto-update:     %d plugins\n", info.AutoUpdateCount)
		fmt.Fprintf(out, "Trusted Keys:    %v\n", info.HasTrustedKeys)
		fmt.Fprintln(out, "\nRegistries:")
		for _, reg := range info.Registries {
			fmt.Fprintf(out, "  - %s\n", reg)
		}
		return nil
	}

	// Show specific plugin info
	result, err := mgr.PluginInfo(cmd.Context(), pluginID)
	if err != nil {
		return fmt.Errorf("failed to get plugin info: %w", err)
	}

	fmt.Fprintf(out, "Plugin: %s\n", pluginID)
	fmt.Fprintln(out, strings.Repeat("=", len(pluginID)+8))
	fmt.Fprintln(out)

	if result.Manifest != nil {
		m := result.Manifest
		fmt.Fprintf(out, "Name:        %s\n", m.Name)
		fmt.Fprintf(out, "Version:     %s\n", m.Version)
		if m.Description != "" {
			fmt.Fprintf(out, "Description: %s\n", m.Description)
		}
		if m.Author != "" {
			fmt.Fprintf(out, "Author:      %s\n", m.Author)
		}
		if m.License != "" {
			fmt.Fprintf(out, "License:     %s\n", m.License)
		}
		if m.Homepage != "" {
			fmt.Fprintf(out, "Homepage:    %s\n", m.Homepage)
		}
		if len(m.Categories) > 0 {
			fmt.Fprintf(out, "Categories:  %s\n", strings.Join(m.Categories, ", "))
		}
		if len(m.Keywords) > 0 {
			fmt.Fprintf(out, "Keywords:    %s\n", strings.Join(m.Keywords, ", "))
		}
		fmt.Fprintf(out, "Compatible:  %v\n", result.Compatible)
		fmt.Fprintln(out)
	}

	if result.Installed != nil {
		i := result.Installed
		fmt.Fprintln(out, "Installation:")
		fmt.Fprintf(out, "  Version:     %s\n", i.Version)
		fmt.Fprintf(out, "  Path:        %s\n", i.Path)
		fmt.Fprintf(out, "  Enabled:     %v\n", i.Enabled)
		fmt.Fprintf(out, "  Auto-update: %v\n", i.AutoUpdate)
		fmt.Fprintf(out, "  Verified:    %v\n", i.Verified)
		fmt.Fprintf(out, "  Installed:   %s\n", i.InstalledAt.Format(time.RFC3339))
		if result.UpdateAvailable {
			fmt.Fprintf(out, "\n  UPDATE AVAILABLE: %s\n", result.Manifest.Version)
		}
	} else {
		fmt.Fprintln(out, "Status: Not installed")
	}

	return nil
}

// runPluginsEnable handles the plugins enable command.
func runPluginsEnable(cmd *cobra.Command, configPath, pluginID string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	if err := mgr.Enable(pluginID); err != nil {
		return fmt.Errorf("failed to enable plugin: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Enabled plugin: %s\n", pluginID)
	return nil
}

// runPluginsDisable handles the plugins disable command.
func runPluginsDisable(cmd *cobra.Command, configPath, pluginID string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := createMarketplaceManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create marketplace manager: %w", err)
	}

	if err := mgr.Disable(pluginID); err != nil {
		return fmt.Errorf("failed to disable plugin: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Disabled plugin: %s\n", pluginID)
	return nil
}
