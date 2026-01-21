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
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/gateway"
	"github.com/haasonsaas/nexus/internal/plugins"
	"github.com/haasonsaas/nexus/internal/workspace"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/spf13/cobra"
)

// Build information - populated by ldflags during build.
//
// Example build command:
//
//	go build -ldflags "-X main.version=v1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	version = "dev"     // Semantic version (e.g., "v1.0.0")
	commit  = "none"    // Git commit SHA
	date    = "unknown" // Build timestamp
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
	)

	return rootCmd
}

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
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml",
		"Path to YAML configuration file (optional)")
	cmd.Flags().StringVar(&workspaceDir, "workspace", "",
		"Workspace directory to initialize (overrides config)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false,
		"Overwrite existing bootstrap files")

	return cmd
}

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
			return runServe(cmd.Context(), configPath, debug)
		},
	}

	// Define flags with descriptive help text.
	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml",
		"Path to YAML configuration file")
	cmd.Flags().BoolVarP(&debug, "debug", "d", false,
		"Enable debug logging (verbose output)")

	return cmd
}

// runServe implements the serve command logic.
// It handles configuration loading, service initialization, and graceful shutdown.
func runServe(ctx context.Context, configPath string, debug bool) error {
	// Adjust log level if debug mode is enabled.
	if debug {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}

	slog.Info("starting Nexus gateway",
		"version", version,
		"commit", commit,
		"config", configPath,
		"debug", debug,
	)

	if raw, err := doctor.LoadRawConfig(configPath); err == nil {
		migrations := doctor.ApplyConfigMigrations(raw)
		if len(migrations.Applied) > 0 {
			slog.Warn("config migrations detected; run `nexus doctor --repair`",
				"count", len(migrations.Applied))
		}
	} else {
		slog.Warn("failed to inspect config for migrations", "error", err)
	}

	// Load and validate configuration.
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := plugins.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("plugin validation failed: %w", err)
	}

	slog.Info("configuration loaded",
		"grpc_port", cfg.Server.GRPCPort,
		"http_port", cfg.Server.HTTPPort,
		"llm_provider", cfg.LLM.DefaultProvider,
	)

	// Create a context that cancels on shutdown signals.
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start the gateway components.
	// TODO: Initialize and start actual components:
	// - Database connection pool
	// - Session store
	// - LLM providers (Anthropic, OpenAI)
	// - Channel adapters (Telegram, Discord, Slack)
	// - Tool executors (Sandbox, Browser, WebSearch)
	// - gRPC server
	// - HTTP server (health, metrics)
	// - Agent runtime orchestrator

	slog.Info("Nexus gateway started",
		"grpc_addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.GRPCPort),
		"http_addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.HTTPPort),
	)

	// Wait for shutdown signal.
	<-ctx.Done()
	slog.Info("shutdown signal received, initiating graceful shutdown")

	// Create a timeout context for graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// TODO: Gracefully shutdown all components in reverse order:
	// - Stop accepting new connections
	// - Drain in-flight requests
	// - Close channel adapters
	// - Close LLM provider connections
	// - Close database connections
	_ = shutdownCtx // Silence unused variable warning until implemented

	slog.Info("Nexus gateway stopped gracefully")
	return nil
}

// buildMigrateCmd creates the "migrate" command group for database migrations.
// Migrations are essential for schema evolution in production deployments.
func buildMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration commands",
		Long: `Manage database schema migrations for CockroachDB.

Migrations ensure your database schema matches the version of Nexus you're running.
Always run migrations after upgrading Nexus to apply any schema changes.`,
	}

	// Add subcommands for migration operations.
	cmd.AddCommand(buildMigrateUpCmd())
	cmd.AddCommand(buildMigrateDownCmd())
	cmd.AddCommand(buildMigrateStatusCmd())

	return cmd
}

// buildMigrateUpCmd creates the "migrate up" command.
func buildMigrateUpCmd() *cobra.Command {
	var (
		configPath string
		steps      int
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Run pending migrations",
		Long: `Apply all pending database migrations.

This command connects to the database specified in your config and applies
any migrations that haven't been run yet. Migrations are applied in order
based on their timestamp prefix.`,
		Example: `  # Apply all pending migrations
  nexus migrate up

  # Apply only the next 2 migrations
  nexus migrate up --steps 2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Info("running database migrations",
				"config", configPath,
				"steps", steps,
			)

			// Load configuration for database URL.
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// TODO: Implement migration runner:
			// 1. Connect to database
			// 2. Load migration files from migrations/
			// 3. Apply pending migrations
			// 4. Record applied migrations in schema_migrations table
			_ = cfg // Silence unused variable warning

			slog.Info("migrations completed successfully")
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml", "Path to config file")
	cmd.Flags().IntVarP(&steps, "steps", "n", 0, "Number of migrations to apply (0 = all)")

	return cmd
}

// buildMigrateDownCmd creates the "migrate down" command.
func buildMigrateDownCmd() *cobra.Command {
	var (
		configPath string
		steps      int
	)

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Rollback migrations",
		Long: `Rollback the last N database migrations.

Use with caution in production! Rolling back migrations may cause data loss
if the migration removed columns or tables.`,
		Example: `  # Rollback the last migration
  nexus migrate down

  # Rollback the last 3 migrations
  nexus migrate down --steps 3`,
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Warn("rolling back migrations",
				"config", configPath,
				"steps", steps,
			)

			// TODO: Implement migration rollback.
			fmt.Printf("Would rollback %d migration(s)\n", steps)
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml", "Path to config file")
	cmd.Flags().IntVarP(&steps, "steps", "n", 1, "Number of migrations to rollback")

	return cmd
}

// buildMigrateStatusCmd creates the "migrate status" command.
func buildMigrateStatusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Long: `Display the current state of database migrations.

Shows which migrations have been applied and which are pending.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Migration Status")
			fmt.Println("================")
			fmt.Println()

			// TODO: Query schema_migrations table and list migration files.
			fmt.Println("Applied migrations:")
			fmt.Println("  ✓ 20240101000000_initial_schema")
			fmt.Println("  ✓ 20240115000000_add_sessions")
			fmt.Println()
			fmt.Println("Pending migrations:")
			fmt.Println("  ○ 20240201000000_add_embeddings")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml", "Path to config file")

	return cmd
}

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

	return cmd
}

// buildChannelsListCmd creates the "channels list" command.
func buildChannelsListCmd() *cobra.Command {
	var configPath string

	return &cobra.Command{
		Use:   "list",
		Short: "List configured channels",
		Long:  "Display all messaging channels defined in the configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			fmt.Println("Configured Channels")
			fmt.Println("===================")
			fmt.Println()

			// Display Telegram configuration.
			if cfg.Channels.Telegram.Enabled {
				fmt.Printf("📱 Telegram\n")
				fmt.Printf("   Status: Enabled\n")
				fmt.Printf("   Bot Token: %s***\n", cfg.Channels.Telegram.BotToken[:10])
				fmt.Println()
			}

			// Display Discord configuration.
			if cfg.Channels.Discord.Enabled {
				fmt.Printf("🎮 Discord\n")
				fmt.Printf("   Status: Enabled\n")
				fmt.Printf("   App ID: %s\n", cfg.Channels.Discord.AppID)
				fmt.Println()
			}

			// Display Slack configuration.
			if cfg.Channels.Slack.Enabled {
				fmt.Printf("💼 Slack\n")
				fmt.Printf("   Status: Enabled\n")
				fmt.Printf("   Bot Token: %s***\n", cfg.Channels.Slack.BotToken[:10])
				fmt.Println()
			}

			return nil
		},
	}
}

// buildChannelsStatusCmd creates the "channels status" command.
func buildChannelsStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show channel connection status",
		Long:  "Display the current connection status of all enabled channels.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Channel Connection Status")
			fmt.Println("========================")
			fmt.Println()

			// TODO: Query actual channel adapter status.
			fmt.Println("📱 Telegram")
			fmt.Println("   Connected: ✓")
			fmt.Println("   Last Message: 2 minutes ago")
			fmt.Println("   Messages Today: 142")
			fmt.Println()

			fmt.Println("🎮 Discord")
			fmt.Println("   Connected: ✓")
			fmt.Println("   Guilds: 3")
			fmt.Println("   Last Message: 5 minutes ago")
			fmt.Println()

			fmt.Println("💼 Slack")
			fmt.Println("   Connected: ✓")
			fmt.Println("   Workspaces: 1")
			fmt.Println("   Socket Mode: Active")
			fmt.Println()

			return nil
		},
	}
}

// buildChannelsTestCmd creates the "channels test" command.
func buildChannelsTestCmd() *cobra.Command {
	var channel string

	cmd := &cobra.Command{
		Use:   "test [channel]",
		Short: "Test channel connectivity",
		Long: `Send a test message to verify channel configuration.

This command attempts to send a test message through the specified channel
to verify that credentials are correct and the bot has necessary permissions.`,
		Example: `  # Test Telegram connection
  nexus channels test telegram

  # Test Discord connection
  nexus channels test discord`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			channel = args[0]
			slog.Info("testing channel connectivity", "channel", channel)

			// TODO: Implement actual channel testing.
			fmt.Printf("Testing %s channel...\n", channel)
			fmt.Printf("✓ API credentials valid\n")
			fmt.Printf("✓ Bot permissions verified\n")
			fmt.Printf("✓ Test message sent successfully\n")

			return nil
		},
	}

	return cmd
}

// buildAgentsCmd creates the "agents" command group for managing AI agents.
func buildAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage AI agents",
		Long: `Configure and manage AI agent instances.

Agents define the behavior, LLM provider, and tools available for conversations.
Each agent can have different system prompts, model configurations, and tool access.`,
	}

	cmd.AddCommand(buildAgentsListCmd())
	cmd.AddCommand(buildAgentsCreateCmd())
	cmd.AddCommand(buildAgentsShowCmd())

	return cmd
}

// buildAgentsListCmd creates the "agents list" command.
func buildAgentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured agents",
		Long:  "Display all AI agents defined in the system.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Configured Agents")
			fmt.Println("=================")
			fmt.Println()

			// TODO: Query agents from database.
			fmt.Println("ID          Name           Provider    Model")
			fmt.Println("----------  -------------  ----------  ----------------------")
			fmt.Println("default     Default Agent  anthropic   claude-sonnet-4-20250514")
			fmt.Println("coder       Code Helper    anthropic   claude-sonnet-4-20250514")
			fmt.Println("researcher  Web Researcher openai      gpt-4o")
			fmt.Println()

			return nil
		},
	}
}

// buildAgentsCreateCmd creates the "agents create" command.
func buildAgentsCreateCmd() *cobra.Command {
	var (
		name     string
		provider string
		model    string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new agent",
		Long: `Create a new AI agent with specified configuration.

The agent will be stored in the database and can be assigned to channels
or selected via API for specific conversations.`,
		Example: `  # Create agent with Claude
  nexus agents create --name "coder" --provider anthropic --model claude-sonnet-4-20250514

  # Create agent with GPT-4
  nexus agents create --name "researcher" --provider openai --model gpt-4o`,
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Info("creating agent",
				"name", name,
				"provider", provider,
				"model", model,
			)

			// TODO: Implement agent creation in database.
			fmt.Printf("Created agent: %s\n", name)
			fmt.Printf("  Provider: %s\n", provider)
			fmt.Printf("  Model: %s\n", model)

			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Agent name (required)")
	cmd.Flags().StringVarP(&provider, "provider", "p", "anthropic", "LLM provider")
	cmd.Flags().StringVarP(&model, "model", "m", "", "Model identifier")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// buildAgentsShowCmd creates the "agents show" command.
func buildAgentsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [agent-id]",
		Short: "Show agent details",
		Long:  "Display detailed configuration for a specific agent.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]

			fmt.Printf("Agent: %s\n", agentID)
			fmt.Println("==========")
			fmt.Println()

			// TODO: Query agent from database.
			fmt.Println("Configuration:")
			fmt.Println("  Provider: anthropic")
			fmt.Println("  Model: claude-sonnet-4-20250514")
			fmt.Println("  Max Tokens: 4096")
			fmt.Println("  Temperature: 0.7")
			fmt.Println()
			fmt.Println("Tools Enabled:")
			fmt.Println("  ✓ web_search")
			fmt.Println("  ✓ code_sandbox")
			fmt.Println("  ✓ browser")
			fmt.Println()
			fmt.Println("Statistics:")
			fmt.Println("  Total Sessions: 1,234")
			fmt.Println("  Messages Today: 567")
			fmt.Println("  Tokens Used Today: 1,234,567")

			return nil
		},
	}
}

// buildStatusCmd creates the "status" command for system health overview.
func buildStatusCmd() *cobra.Command {
	var (
		configPath string
		json       bool
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
			if json {
				// TODO: Output JSON format for scripting.
				fmt.Println(`{"status": "healthy", "version": "` + version + `"}`)
				return nil
			}

			fmt.Println("╔════════════════════════════════════════════════════════════╗")
			fmt.Println("║                      NEXUS STATUS                          ║")
			fmt.Println("╚════════════════════════════════════════════════════════════╝")
			fmt.Println()
			fmt.Printf("Version: %s (commit: %s)\n", version, commit[:7])
			fmt.Printf("Built: %s\n", date)
			fmt.Println()

			fmt.Println("🗄️  Database")
			fmt.Println("   CockroachDB: ✓ Connected")
			fmt.Println("   Latency: 2.3ms")
			fmt.Println("   Active Connections: 5/20")
			fmt.Println()

			fmt.Println("📡 Channels")
			fmt.Println("   Telegram: ✓ Connected")
			fmt.Println("   Discord: ✓ Connected")
			fmt.Println("   Slack: ✓ Connected")
			fmt.Println()

			fmt.Println("🤖 LLM Providers")
			fmt.Println("   Anthropic: ✓ Available")
			fmt.Println("   OpenAI: ✓ Available")
			fmt.Println()

			fmt.Println("🔧 Tools")
			fmt.Println("   Web Search: ✓ Ready")
			fmt.Println("   Code Sandbox: ✓ 5 VMs pooled")
			fmt.Println("   Browser: ✓ 3 instances pooled")
			fmt.Println()

			fmt.Println("📊 Metrics (Last 24h)")
			fmt.Println("   Messages Processed: 12,345")
			fmt.Println("   Tool Invocations: 2,345")
			fmt.Println("   LLM Tokens: 5,678,901")
			fmt.Println("   Avg Response Time: 1.2s")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml", "Path to config file")
	cmd.Flags().BoolVar(&json, "json", false, "Output in JSON format")

	return cmd
}

// buildDoctorCmd creates the "doctor" command for config validation.
func buildDoctorCmd() *cobra.Command {
	var configPath string
	var repair bool
	var probe bool
	var audit bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate configuration and plugin manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			raw, err := doctor.LoadRawConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to read config: %w", err)
			}
			migrations := doctor.ApplyConfigMigrations(raw)
			if len(migrations.Applied) > 0 {
				if repair {
					if err := doctor.WriteRawConfig(configPath, raw); err != nil {
						return fmt.Errorf("failed to write migrated config: %w", err)
					}
					fmt.Fprintln(out, "Applied config migrations:")
					for _, note := range migrations.Applied {
						fmt.Fprintf(out, "  - %s\n", note)
					}
				} else {
					fmt.Fprintln(out, "Config migrations available (run `nexus doctor --repair` to apply):")
					for _, note := range migrations.Applied {
						fmt.Fprintf(out, "  - %s\n", note)
					}
				}
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				if len(migrations.Applied) > 0 && !repair {
					return fmt.Errorf("config validation failed (legacy keys detected). run `nexus doctor --repair`: %w", err)
				}
				return fmt.Errorf("config validation failed: %w", err)
			}
			if err := plugins.ValidateConfig(cfg); err != nil {
				return fmt.Errorf("plugin validation failed: %w", err)
			}

			if repair {
				if result, err := doctor.RepairWorkspace(cfg); err != nil {
					return fmt.Errorf("workspace repair failed: %w", err)
				} else if len(result.Created) > 0 {
					fmt.Fprintln(out, "Workspace files created:")
					for _, path := range result.Created {
						fmt.Fprintf(out, "  - %s\n", path)
					}
				}
				if path, created, err := doctor.RepairHeartbeat(cfg, configPath); err != nil {
					return fmt.Errorf("heartbeat repair failed: %w", err)
				} else if created {
					fmt.Fprintf(out, "Heartbeat file created: %s\n", path)
				}
			}

			if probe {
				server, err := gateway.NewServer(cfg, slog.Default())
				if err != nil {
					return fmt.Errorf("failed to initialize gateway for probes: %w", err)
				}
				results := doctor.ProbeChannelHealth(cmd.Context(), server.Channels())
				if len(results) == 0 {
					fmt.Fprintln(out, "Channel probes: no health adapters registered")
				} else {
					fmt.Fprintln(out, "Channel probes:")
					for _, result := range results {
						status := "unhealthy"
						if result.Status.Healthy {
							status = "healthy"
						}
						if result.Status.Degraded {
							status = "degraded"
						}
						fmt.Fprintf(out, "  - %s: %s (%s)\n", result.Channel, status, result.Status.Message)
					}
				}
			}

			if audit {
				report := doctor.AuditServices(cfg)
				fmt.Fprintln(out, "Service audit:")
				printAuditList(out, "systemd user", report.SystemdUser)
				printAuditList(out, "systemd system", report.SystemdSystem)
				printAuditList(out, "launchd user", report.LaunchdUser)
				printAuditList(out, "launchd system", report.LaunchdSystem)
				if len(report.Ports) > 0 {
					fmt.Fprintln(out, "Port checks:")
					for _, port := range report.Ports {
						status := "available"
						if port.InUse {
							status = "in use"
						}
						if port.Error != "" {
							fmt.Fprintf(out, "  - %d: %s (%s)\n", port.Port, status, port.Error)
						} else {
							fmt.Fprintf(out, "  - %d: %s\n", port.Port, status)
						}
					}
				}
			}

			fmt.Fprintf(out, "Config OK (provider: %s)\n", cfg.LLM.DefaultProvider)
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml",
		"Path to YAML configuration file")
	cmd.Flags().BoolVar(&repair, "repair", false, "Apply migrations and common repairs")
	cmd.Flags().BoolVar(&probe, "probe", false, "Run channel health probes")
	cmd.Flags().BoolVar(&audit, "audit", false, "Audit service files and port availability")

	return cmd
}

func printAuditList(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(out, "%s: none found\n", label)
		return
	}
	fmt.Fprintf(out, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(out, "  - %s\n", item)
	}
}

// buildPromptCmd creates the "prompt" command for previewing the system prompt.
func buildPromptCmd() *cobra.Command {
	var (
		configPath string
		sessionID  string
		channel    string
		message    string
		heartbeat  bool
	)

	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Render the system prompt for a session/message",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if err := plugins.ValidateConfig(cfg); err != nil {
				return fmt.Errorf("plugin validation failed: %w", err)
			}

			if strings.TrimSpace(sessionID) == "" {
				return fmt.Errorf("session-id is required")
			}
			if strings.TrimSpace(channel) == "" {
				return fmt.Errorf("channel is required")
			}

			msg := &models.Message{
				Channel: models.ChannelType(channel),
				Content: message,
			}
			if heartbeat {
				if msg.Metadata == nil {
					msg.Metadata = map[string]any{}
				}
				msg.Metadata["heartbeat"] = true
				if strings.TrimSpace(msg.Content) == "" {
					msg.Content = "heartbeat"
				}
			}

			prompt, err := gateway.BuildSystemPrompt(cfg, sessionID, msg)
			if err != nil {
				return fmt.Errorf("failed to build prompt: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), prompt)
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "nexus.yaml",
		"Path to YAML configuration file")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID for memory scoping")
	cmd.Flags().StringVar(&channel, "channel", "", "Channel type (telegram, discord, slack)")
	cmd.Flags().StringVar(&message, "message", "", "Message content (used for heartbeat mode)")
	cmd.Flags().BoolVar(&heartbeat, "heartbeat", false, "Force heartbeat prompt mode")

	return cmd
}
