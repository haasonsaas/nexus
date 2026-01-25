// Package main provides the CLI entry point for the Nexus multi-channel AI gateway.
//
// Nexus connects messaging platforms (Telegram, Discord, Slack) to LLM providers
// (Anthropic, OpenAI) with powerful tool execution capabilities including web search,
// sandboxed code execution, and browser automation.
//
// # Basic Usage
//
// Start the server:
//
//	nexus serve --config nexus.yaml
//
// Check system status:
//
//	nexus status
//
// Manage database migrations:
//
//	nexus migrate up
//	nexus migrate status
//
// # Environment Variables
//
// Configuration can be provided via environment variables:
//
//   - NEXUS_CONFIG: Path to configuration file (default: nexus.yaml)
//   - ANTHROPIC_API_KEY: Anthropic API key for Claude models
//   - OPENAI_API_KEY: OpenAI API key for GPT models
//   - TELEGRAM_BOT_TOKEN: Telegram bot token
//   - DISCORD_BOT_TOKEN: Discord bot token
//   - SLACK_BOT_TOKEN: Slack bot OAuth token
//   - SLACK_APP_TOKEN: Slack app-level token for Socket Mode
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Build information - populated by ldflags during build.
//
// Example build command:
//
//	go build -ldflags "-X main.version=v1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	version     = "dev"     // Semantic version (e.g., "v1.0.0")
	commit      = "none"    // Git commit SHA
	date        = "unknown" // Build timestamp
	profileName string      // Active profile name from flag
)

// main is the entry point for the Nexus CLI.
// It sets up the root command and all subcommands, then executes based on CLI args.
func main() {
	// Configure structured logging with JSON output for production parsing.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Build the command tree.
	rootCmd := buildRootCmd()

	// Execute the CLI - Cobra handles argument parsing and command routing.
	if err := rootCmd.Execute(); err != nil {
		slog.Error("command execution failed", "error", err)
		os.Exit(1)
	}
}

// buildRootCmd creates the root command with all subcommands attached.
// This is separated from main() to facilitate testing.
func buildRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "nexus",
		Short: "Nexus - Multi-channel AI agent gateway",
		Long: `Nexus connects messaging platforms to LLM providers with tool execution.

Supported channels: Telegram, Discord, Slack
Supported LLM providers: Anthropic (Claude), OpenAI (GPT)
Available tools: Web Search, Code Sandbox, Browser Automation

Documentation: https://github.com/haasonsaas/nexus`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		// SilenceUsage prevents printing usage on every error.
		SilenceUsage: true,
	}
	rootCmd.PersistentFlags().StringVar(&profileName, "profile", "", "Profile name (uses ~/.nexus/profiles/<name>.yaml; or set NEXUS_PROFILE)")

	// Attach all subcommands.
	rootCmd.AddCommand(
		buildServeCmd(),
		buildMigrateCmd(),
		buildChannelsCmd(),
		buildAgentsCmd(),
		buildStatusCmd(),
		buildDoctorCmd(),
		buildPromptCmd(),
		buildSetupCmd(),
		buildOnboardCmd(),
		buildAuthCmd(),
		buildProfileCmd(),
		buildPairingCmd(),
		buildArtifactsCmd(),
		buildSkillsCmd(),
		buildExtensionsCmd(),
		buildPluginsCmd(),
		buildServiceCmd(),
		buildMemoryCmd(),
		buildRagCmd(),
		buildMcpCmd(),
		buildTraceCmd(),
		buildEdgeCmd(),
		buildEventsCmd(),
	)

	return rootCmd
}
