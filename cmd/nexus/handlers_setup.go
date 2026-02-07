package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/onboard"
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/haasonsaas/nexus/internal/workspace"
	"github.com/spf13/cobra"
)

// =============================================================================
// Setup and Onboard Command Handlers
// =============================================================================

// runSetup handles the setup command.
func runSetup(cmd *cobra.Command, configPath, workspaceDir string, overwrite bool) error {
	configPath = resolveConfigPath(configPath)
	cfg := &config.Config{
		Workspace: config.DefaultWorkspaceConfig(),
	}

	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			slog.Warn("failed to load config, using defaults", "error", err)
		} else {
			cfg = loaded
		}
	}

	if strings.TrimSpace(workspaceDir) != "" {
		cfg.Workspace.Path = workspaceDir
	}

	files := workspace.BootstrapFilesForConfig(cfg)
	result, err := workspace.EnsureWorkspaceFiles(cfg.Workspace.Path, files, overwrite)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Workspace ready: %s\n", cfg.Workspace.Path)
	if len(result.Created) > 0 {
		fmt.Fprintln(out, "Created:")
		for _, path := range result.Created {
			fmt.Fprintf(out, "  - %s\n", path)
		}
	}
	if len(result.Skipped) > 0 {
		fmt.Fprintln(out, "Skipped (already exists):")
		for _, path := range result.Skipped {
			fmt.Fprintf(out, "  - %s\n", path)
		}
	}

	return nil
}

// runOnboard handles the onboard command.
func runOnboard(cmd *cobra.Command, opts *onboard.Options, nonInteractive, setupWorkspace bool) error {
	if strings.TrimSpace(profileName) != "" {
		opts.ConfigPath = profile.ProfileConfigPath(profileName)
		if strings.TrimSpace(opts.WorkspacePath) == "" {
			opts.WorkspacePath = workspacePathFromProfile(profileName)
		}
	}
	if !nonInteractive {
		reader := bufio.NewReader(os.Stdin)
		if strings.TrimSpace(opts.DatabaseURL) == "" {
			opts.DatabaseURL = promptString(reader, "Database URL", "postgres://root@localhost:26257/nexus?sslmode=disable")
		}
		if strings.TrimSpace(opts.Provider) == "" {
			opts.Provider = promptString(reader, "LLM provider (anthropic/openai/google/openrouter)", "anthropic")
		}
		if strings.TrimSpace(opts.ProviderKey) == "" {
			opts.ProviderKey = promptString(reader, "Provider API key", "")
		}
		if strings.TrimSpace(opts.WorkspacePath) == "" {
			opts.WorkspacePath = promptString(reader, "Workspace path (optional)", "")
		}
		opts.EnableTelegram = promptBool(reader, "Enable Telegram?", opts.EnableTelegram)
		if opts.EnableTelegram && strings.TrimSpace(opts.TelegramToken) == "" {
			opts.TelegramToken = promptString(reader, "Telegram bot token", "")
		}
		opts.EnableDiscord = promptBool(reader, "Enable Discord?", opts.EnableDiscord)
		if opts.EnableDiscord {
			if strings.TrimSpace(opts.DiscordToken) == "" {
				opts.DiscordToken = promptString(reader, "Discord bot token", "")
			}
			if strings.TrimSpace(opts.DiscordAppID) == "" {
				opts.DiscordAppID = promptString(reader, "Discord app ID", "")
			}
		}
		opts.EnableSlack = promptBool(reader, "Enable Slack?", opts.EnableSlack)
		if opts.EnableSlack {
			if strings.TrimSpace(opts.SlackBotToken) == "" {
				opts.SlackBotToken = promptString(reader, "Slack bot token", "")
			}
			if strings.TrimSpace(opts.SlackAppToken) == "" {
				opts.SlackAppToken = promptString(reader, "Slack app token", "")
			}
			if strings.TrimSpace(opts.SlackSecret) == "" {
				opts.SlackSecret = promptString(reader, "Slack signing secret", "")
			}
		}
	}

	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = resolveConfigPath(opts.ConfigPath)
	}

	raw := onboard.BuildConfig(*opts)
	if err := onboard.WriteConfig(opts.ConfigPath, raw); err != nil {
		return err
	}

	if setupWorkspace && strings.TrimSpace(opts.WorkspacePath) != "" {
		files := workspace.BootstrapFilesForConfig(&config.Config{Workspace: config.WorkspaceConfig{Enabled: true, Path: opts.WorkspacePath}})
		if _, err := workspace.EnsureWorkspaceFiles(opts.WorkspacePath, files, false); err != nil {
			return err
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Config written: %s\n", opts.ConfigPath)
	return nil
}

// runAuthSet handles the auth set command.
func runAuthSet(cmd *cobra.Command, configPath, provider, apiKey string, setDefault bool) error {
	configPath = resolveConfigPath(configPath)
	raw := map[string]any{}
	if configPath != "" {
		existing, err := doctor.LoadRawConfig(configPath)
		if err == nil {
			raw = existing
		}
	}
	onboard.ApplyAuthConfig(raw, provider, apiKey, setDefault)
	if err := onboard.WriteConfig(configPath, raw); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated auth for %s in %s\n", provider, configPath)
	return nil
}
