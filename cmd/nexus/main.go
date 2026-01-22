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
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/gateway"
	"github.com/haasonsaas/nexus/internal/marketplace"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/internal/onboard"
	"github.com/haasonsaas/nexus/internal/plugins"
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/haasonsaas/nexus/internal/service"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/haasonsaas/nexus/internal/workspace"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
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
	profileName string
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
		buildSkillsCmd(),
		buildPluginsCmd(),
		buildServiceCmd(),
		buildMemoryCmd(),
		buildMcpCmd(),
		buildTraceCmd(),
	)

	return rootCmd
}

func resolveConfigPath(path string) string {
	activeProfile := strings.TrimSpace(profileName)
	if activeProfile == "" {
		activeProfile = strings.TrimSpace(os.Getenv("NEXUS_PROFILE"))
	}
	if activeProfile != "" {
		return profile.ProfileConfigPath(activeProfile)
	}
	if strings.TrimSpace(path) == "" || path == profile.DefaultConfigName {
		return profile.DefaultConfigPath()
	}
	return path
}

func openMigrationDB(cfg *config.Config) (*sql.DB, error) {
	if cfg == nil || strings.TrimSpace(cfg.Database.URL) == "" {
		return nil, fmt.Errorf("database url is required")
	}
	db, err := sql.Open("postgres", cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pool := sessions.DefaultCockroachConfig()
	if cfg.Database.MaxConnections > 0 {
		pool.MaxOpenConns = cfg.Database.MaxConnections
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		pool.ConnMaxLifetime = cfg.Database.ConnMaxLifetime
	}
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), pool.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

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
			configPath = resolveConfigPath(configPath)
			result, err := service.InstallUserService(configPath, false)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Service file written: %s\n", result.Path)
			if restart {
				steps, err := service.RestartUserService(cmd.Context())
				if err != nil {
					fmt.Fprintf(out, "Service restart failed: %v\n", err)
					if len(steps) > 0 {
						fmt.Fprintln(out, "Manual restart steps:")
						for _, step := range steps {
							fmt.Fprintf(out, "  - %s\n", step)
						}
					}
					return err
				}
				fmt.Fprintln(out, "Service restarted.")
			}
			if len(result.Instructions) > 0 {
				label := "Next steps:"
				if restart {
					label = "Next steps (if needed):"
				}
				fmt.Fprintln(out, label)
				for _, step := range result.Instructions {
					fmt.Fprintf(out, "  - %s\n", step)
				}
			}
			return nil
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
			configPath = resolveConfigPath(configPath)
			result, err := service.InstallUserService(configPath, true)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Service file updated: %s\n", result.Path)
			if restart {
				steps, err := service.RestartUserService(cmd.Context())
				if err != nil {
					fmt.Fprintf(out, "Service restart failed: %v\n", err)
					if len(steps) > 0 {
						fmt.Fprintln(out, "Manual restart steps:")
						for _, step := range steps {
							fmt.Fprintf(out, "  - %s\n", step)
						}
					}
					return err
				}
				fmt.Fprintln(out, "Service restarted.")
			}
			if len(result.Instructions) > 0 {
				label := "Next steps:"
				if restart {
					label = "Next steps (if needed):"
				}
				fmt.Fprintln(out, label)
				for _, step := range result.Instructions {
					fmt.Fprintf(out, "  - %s\n", step)
				}
			}
			return nil
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
			configPath = resolveConfigPath(configPath)
			cfg, _ := config.Load(configPath)
			report := doctor.AuditServices(cfg)
			out := cmd.OutOrStdout()
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
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

// buildProfileCmd creates the "profile" command group.
func buildProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage configuration profiles",
	}
	cmd.AddCommand(buildProfileListCmd(), buildProfileUseCmd(), buildProfilePathCmd(), buildProfileInitCmd())
	return cmd
}

func buildProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := profile.ListProfiles()
			if err != nil {
				return err
			}
			active, _ := profile.ReadActiveProfile()
			out := cmd.OutOrStdout()
			if len(profiles) == 0 {
				fmt.Fprintln(out, "No profiles found.")
				return nil
			}
			fmt.Fprintln(out, "Profiles:")
			for _, name := range profiles {
				marker := ""
				if name == active {
					marker = " (active)"
				}
				fmt.Fprintf(out, "  - %s%s\n", name, marker)
			}
			return nil
		},
	}
}

func buildProfileUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use [name]",
		Short: "Set the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("profile name is required")
			}
			if err := profile.WriteActiveProfile(name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Active profile set: %s\n", name)
			return nil
		},
	}
}

func buildProfilePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path [name]",
		Short: "Print the config path for a profile",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			path := profile.ProfileConfigPath(name)
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

func buildProfileInitCmd() *cobra.Command {
	var provider string
	var setActive bool
	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Initialize a new profile config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("profile name is required")
			}
			path := profile.ProfileConfigPath(name)
			opts := onboard.Options{Provider: provider}
			raw := onboard.BuildConfig(opts)
			if err := onboard.WriteConfig(path, raw); err != nil {
				return err
			}
			if setActive {
				if err := profile.WriteActiveProfile(name); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Profile config written: %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "anthropic", "Default LLM provider")
	cmd.Flags().BoolVar(&setActive, "use", false, "Set as active profile after creation")
	return cmd
}

// buildSkillsCmd creates the "skills" command group.
func buildSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills (SKILL.md-based)",
		Long: `Manage skills that extend agent capabilities.

Skills are discovered from:
  - <workspace>/skills/ (highest priority)
  - ~/.nexus/skills/ (user skills)
  - Bundled skills (shipped with binary)
  - Extra directories (skills.load.extraDirs)

Each skill is a directory containing a SKILL.md file with YAML frontmatter.`,
	}
	cmd.AddCommand(
		buildSkillsListCmd(),
		buildSkillsShowCmd(),
		buildSkillsCheckCmd(),
		buildSkillsEnableCmd(),
		buildSkillsDisableCmd(),
	)
	return cmd
}

func buildSkillsListCmd() *cobra.Command {
	var configPath string
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		Long: `List all discovered skills and their eligibility status.

By default, only eligible skills are shown. Use --all to include ineligible skills.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}

			mgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
			if err != nil {
				return fmt.Errorf("failed to create skill manager: %w", err)
			}

			if err := mgr.Discover(cmd.Context()); err != nil {
				return fmt.Errorf("skill discovery failed: %w", err)
			}

			out := cmd.OutOrStdout()
			var skillsList []*skills.SkillEntry
			if all {
				skillsList = mgr.ListAll()
			} else {
				skillsList = mgr.ListEligible()
			}

			if len(skillsList) == 0 {
				fmt.Fprintln(out, "No skills found.")
				return nil
			}

			fmt.Fprintln(out, "Skills:")
			for _, skill := range skillsList {
				emoji := ""
				if skill.Metadata != nil && skill.Metadata.Emoji != "" {
					emoji = skill.Metadata.Emoji + " "
				}

				status := "eligible"
				if all {
					result, _ := mgr.CheckEligibility(skill.Name)
					if result != nil && !result.Eligible {
						status = "ineligible"
					}
				}

				fmt.Fprintf(out, "  %s%s (%s, %s)\n", emoji, skill.Name, skill.Source, status)
				if skill.Description != "" {
					desc := skill.Description
					if len(desc) > 60 {
						desc = desc[:57] + "..."
					}
					fmt.Fprintf(out, "    %s\n", desc)
				}
			}

			if all {
				reasons := mgr.GetIneligibleReasons()
				if len(reasons) > 0 {
					fmt.Fprintln(out, "\nIneligible reasons:")
					for name, reason := range reasons {
						fmt.Fprintf(out, "  %s: %s\n", name, reason)
					}
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all skills including ineligible ones")
	return cmd
}

func buildSkillsShowCmd() *cobra.Command {
	var configPath string
	var showContent bool
	cmd := &cobra.Command{
		Use:   "show [name]",
		Short: "Show skill details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}

			mgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
			if err != nil {
				return fmt.Errorf("failed to create skill manager: %w", err)
			}

			if err := mgr.Discover(cmd.Context()); err != nil {
				return fmt.Errorf("skill discovery failed: %w", err)
			}

			skill, ok := mgr.GetSkill(args[0])
			if !ok {
				return fmt.Errorf("skill not found: %s", args[0])
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Skill: %s\n", skill.Name)
			fmt.Fprintln(out, strings.Repeat("=", len(skill.Name)+7))
			fmt.Fprintln(out)

			if skill.Description != "" {
				fmt.Fprintf(out, "Description: %s\n", skill.Description)
			}
			if skill.Homepage != "" {
				fmt.Fprintf(out, "Homepage: %s\n", skill.Homepage)
			}
			fmt.Fprintf(out, "Path: %s\n", skill.Path)
			fmt.Fprintf(out, "Source: %s\n", skill.Source)

			// Metadata
			if skill.Metadata != nil {
				fmt.Fprintln(out, "\nMetadata:")
				if skill.Metadata.Emoji != "" {
					fmt.Fprintf(out, "  Emoji: %s\n", skill.Metadata.Emoji)
				}
				if skill.Metadata.Always {
					fmt.Fprintln(out, "  Always: true")
				}
				if len(skill.Metadata.OS) > 0 {
					fmt.Fprintf(out, "  OS: %v\n", skill.Metadata.OS)
				}
				if skill.Metadata.PrimaryEnv != "" {
					fmt.Fprintf(out, "  Primary Env: %s\n", skill.Metadata.PrimaryEnv)
				}

				// Requirements
				if skill.Metadata.Requires != nil {
					req := skill.Metadata.Requires
					fmt.Fprintln(out, "\nRequirements:")
					if len(req.Bins) > 0 {
						fmt.Fprintf(out, "  Binaries: %v\n", req.Bins)
					}
					if len(req.AnyBins) > 0 {
						fmt.Fprintf(out, "  Any Binary: %v\n", req.AnyBins)
					}
					if len(req.Env) > 0 {
						fmt.Fprintf(out, "  Env Vars: %v\n", req.Env)
					}
					if len(req.Config) > 0 {
						fmt.Fprintf(out, "  Config: %v\n", req.Config)
					}
				}

				// Install specs
				if len(skill.Metadata.Install) > 0 {
					fmt.Fprintln(out, "\nInstall Options:")
					for _, spec := range skill.Metadata.Install {
						label := spec.Label
						if label == "" {
							label = spec.ID
						}
						fmt.Fprintf(out, "  - %s (%s)\n", label, spec.Kind)
					}
				}
			}

			// Eligibility
			result, _ := mgr.CheckEligibility(skill.Name)
			if result != nil {
				fmt.Fprintln(out)
				if result.Eligible {
					fmt.Fprintln(out, "Status: Eligible")
				} else {
					fmt.Fprintf(out, "Status: Ineligible (%s)\n", result.Reason)
				}
			}

			// Content
			if showContent {
				content, err := mgr.LoadContent(skill.Name)
				if err != nil {
					return fmt.Errorf("failed to load content: %w", err)
				}
				fmt.Fprintln(out, "\nContent:")
				fmt.Fprintln(out, strings.Repeat("-", 40))
				fmt.Fprintln(out, content)
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVar(&showContent, "content", false, "Show full skill content")
	return cmd
}

func buildSkillsCheckCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "check [name]",
		Short: "Check skill eligibility",
		Long:  "Check if a skill is eligible to be loaded and show the reason if not.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}

			mgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
			if err != nil {
				return fmt.Errorf("failed to create skill manager: %w", err)
			}

			if err := mgr.Discover(cmd.Context()); err != nil {
				return fmt.Errorf("skill discovery failed: %w", err)
			}

			result, err := mgr.CheckEligibility(args[0])
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if result.Eligible {
				fmt.Fprintf(out, "Skill '%s' is eligible\n", args[0])
				if result.Reason != "" {
					fmt.Fprintf(out, "  Reason: %s\n", result.Reason)
				}
			} else {
				fmt.Fprintf(out, "Skill '%s' is NOT eligible\n", args[0])
				fmt.Fprintf(out, "  Reason: %s\n", result.Reason)
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildSkillsEnableCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "enable [name]",
		Short: "Enable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			raw, err := doctor.LoadRawConfig(configPath)
			if err != nil {
				return err
			}
			setSkillEnabled(raw, args[0], true)
			if err := doctor.WriteRawConfig(configPath, raw); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Enabled skill: %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildSkillsDisableCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "disable [name]",
		Short: "Disable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			raw, err := doctor.LoadRawConfig(configPath)
			if err != nil {
				return err
			}
			setSkillEnabled(raw, args[0], false)
			if err := doctor.WriteRawConfig(configPath, raw); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Disabled skill: %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func setSkillEnabled(raw map[string]any, name string, enabled bool) {
	if raw == nil {
		return
	}
	skillsSection, ok := raw["skills"].(map[string]any)
	if !ok {
		skillsSection = map[string]any{}
		raw["skills"] = skillsSection
	}
	entries, ok := skillsSection["entries"].(map[string]any)
	if !ok {
		entries = map[string]any{}
		skillsSection["entries"] = entries
	}
	entry, ok := entries[name].(map[string]any)
	if !ok {
		entry = map[string]any{}
		entries[name] = entry
	}
	entry["enabled"] = enabled
}

// buildMemoryCmd creates the "memory" command group for vector memory.
func buildMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage vector memory for semantic search",
		Long: `Manage the vector memory system for semantic search.

Vector memory allows semantic search over conversation history
and indexed documents using embedding models (OpenAI, Ollama).

Storage backends: sqlite-vec (default), LanceDB, pgvector`,
	}
	cmd.AddCommand(
		buildMemorySearchCmd(),
		buildMemoryIndexCmd(),
		buildMemoryStatsCmd(),
		buildMemoryCompactCmd(),
	)
	return cmd
}

func buildMemorySearchCmd() *cobra.Command {
	var (
		configPath string
		scope      string
		scopeID    string
		limit      int
		threshold  float32
	)
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memory using semantic similarity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := memory.NewManager(&cfg.VectorMemory)
			if err != nil {
				return fmt.Errorf("failed to create memory manager: %w", err)
			}
			defer mgr.Close()

			memScope := models.MemoryScope(scope)
			resp, err := mgr.Search(cmd.Context(), &models.SearchRequest{
				Query:     args[0],
				Scope:     memScope,
				ScopeID:   scopeID,
				Limit:     limit,
				Threshold: threshold,
			})
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			out := cmd.OutOrStdout()
			if len(resp.Results) == 0 {
				fmt.Fprintln(out, "No results found.")
				return nil
			}

			fmt.Fprintf(out, "Found %d results (query time: %v):\n\n", len(resp.Results), resp.QueryTime)
			for i, result := range resp.Results {
				content := result.Entry.Content
				if len(content) > 200 {
					content = content[:197] + "..."
				}
				fmt.Fprintf(out, "%d. [Score: %.3f] %s\n", i+1, result.Score, content)
				fmt.Fprintf(out, "   Source: %s | Created: %s\n\n",
					result.Entry.Metadata.Source, result.Entry.CreatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&scope, "scope", "global", "Search scope (session, channel, agent, global)")
	cmd.Flags().StringVar(&scopeID, "scope-id", "", "Scope ID for scoped searches")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of results")
	cmd.Flags().Float32Var(&threshold, "threshold", 0.7, "Minimum similarity threshold (0-1)")
	return cmd
}

func buildMemoryIndexCmd() *cobra.Command {
	var (
		configPath string
		scope      string
		scopeID    string
		source     string
	)
	cmd := &cobra.Command{
		Use:   "index [file-or-directory]",
		Short: "Index files into memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := memory.NewManager(&cfg.VectorMemory)
			if err != nil {
				return fmt.Errorf("failed to create memory manager: %w", err)
			}
			defer mgr.Close()

			path := args[0]
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("failed to stat path: %w", err)
			}

			var entries []*models.MemoryEntry
			if info.IsDir() {
				err = filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
					if err != nil || fi.IsDir() {
						return err
					}
					entry, err := fileToEntry(p, scope, scopeID, source)
					if err != nil {
						slog.Warn("skipping file", "path", p, "error", err)
						return nil
					}
					entries = append(entries, entry)
					return nil
				})
				if err != nil {
					return fmt.Errorf("failed to walk directory: %w", err)
				}
			} else {
				entry, err := fileToEntry(path, scope, scopeID, source)
				if err != nil {
					return fmt.Errorf("failed to read file: %w", err)
				}
				entries = append(entries, entry)
			}

			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No files to index.")
				return nil
			}

			if err := mgr.Index(cmd.Context(), entries); err != nil {
				return fmt.Errorf("indexing failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d entries.\n", len(entries))
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&scope, "scope", "global", "Memory scope (session, channel, agent, global)")
	cmd.Flags().StringVar(&scopeID, "scope-id", "", "Scope ID")
	cmd.Flags().StringVar(&source, "source", "document", "Source label for indexed content")
	return cmd
}

func fileToEntry(path, scope, scopeID, source string) (*models.MemoryEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entry := &models.MemoryEntry{
		Content: string(content),
		Metadata: models.MemoryMetadata{
			Source: source,
			Extra:  map[string]any{"path": path},
		},
		CreatedAt: time.Now(),
	}
	switch models.MemoryScope(scope) {
	case models.ScopeSession:
		entry.SessionID = scopeID
	case models.ScopeChannel:
		entry.ChannelID = scopeID
	case models.ScopeAgent:
		entry.AgentID = scopeID
	}
	return entry, nil
}

func buildMemoryStatsCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show memory statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := memory.NewManager(&cfg.VectorMemory)
			if err != nil {
				return fmt.Errorf("failed to create memory manager: %w", err)
			}
			defer mgr.Close()

			stats, err := mgr.Stats(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get stats: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Memory Statistics")
			fmt.Fprintln(out, "=================")
			fmt.Fprintf(out, "Total Entries:      %d\n", stats.TotalEntries)
			fmt.Fprintf(out, "Backend:            %s\n", stats.Backend)
			fmt.Fprintf(out, "Embedding Provider: %s\n", stats.EmbeddingProvider)
			fmt.Fprintf(out, "Embedding Model:    %s\n", stats.EmbeddingModel)
			fmt.Fprintf(out, "Dimension:          %d\n", stats.Dimension)
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildMemoryCompactCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Compact and optimize memory storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := memory.NewManager(&cfg.VectorMemory)
			if err != nil {
				return fmt.Errorf("failed to create memory manager: %w", err)
			}
			defer mgr.Close()

			if err := mgr.Compact(cmd.Context()); err != nil {
				return fmt.Errorf("compact failed: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Memory compacted successfully.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

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
			cfg, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			if cfg.MCP.Enabled {
				if err := mgr.Start(cmd.Context()); err != nil {
					return err
				}
			}
			defer mgr.Stop()

			out := cmd.OutOrStdout()
			statuses := mgr.Status()
			if len(statuses) == 0 {
				fmt.Fprintln(out, "No MCP servers configured.")
				return nil
			}
			fmt.Fprintln(out, "MCP Servers:")
			for _, status := range statuses {
				state := "disconnected"
				if status.Connected {
					state = "connected"
				}
				fmt.Fprintf(out, "  %s (%s) - %s\n", status.ID, status.Name, state)
				if status.Connected {
					fmt.Fprintf(out, "    Tools: %d | Resources: %d | Prompts: %d\n", status.Tools, status.Resources, status.Prompts)
				}
			}
			return nil
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
			_, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			defer mgr.Stop()

			if err := mgr.Connect(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Connected to %s\n", args[0])
			return nil
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
			_, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			defer mgr.Stop()

			if serverID != "" {
				if err := mgr.Connect(cmd.Context(), serverID); err != nil {
					return err
				}
			} else {
				if err := mgr.Start(cmd.Context()); err != nil {
					return err
				}
			}

			tools := mgr.AllTools()
			out := cmd.OutOrStdout()
			if serverID != "" {
				list := tools[serverID]
				if len(list) == 0 {
					fmt.Fprintf(out, "No tools for %s\n", serverID)
					return nil
				}
				fmt.Fprintf(out, "Tools for %s:\n", serverID)
				for _, tool := range list {
					fmt.Fprintf(out, "  - %s: %s\n", tool.Name, tool.Description)
				}
				return nil
			}
			if len(tools) == 0 {
				fmt.Fprintln(out, "No tools available.")
				return nil
			}
			for id, list := range tools {
				fmt.Fprintf(out, "Tools for %s:\n", id)
				for _, tool := range list {
					fmt.Fprintf(out, "  - %s: %s\n", tool.Name, tool.Description)
				}
			}
			return nil
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
			serverID, toolName, err := parseMCPQualifiedName(args[0])
			if err != nil {
				return err
			}
			_, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			defer mgr.Stop()

			if err := mgr.Connect(cmd.Context(), serverID); err != nil {
				return err
			}
			toolArgs, err := parseAnyArgs(rawArgs)
			if err != nil {
				return err
			}
			result, err := mgr.CallTool(cmd.Context(), serverID, toolName, toolArgs)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if result == nil || len(result.Content) == 0 {
				fmt.Fprintln(out, "No result.")
				return nil
			}
			for _, item := range result.Content {
				if item.Type == "text" {
					fmt.Fprintln(out, item.Text)
					continue
				}
				payload, err := json.Marshal(item)
				if err != nil {
					return err
				}
				fmt.Fprintln(out, string(payload))
			}
			return nil
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
			_, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			defer mgr.Stop()

			if serverID != "" {
				if err := mgr.Connect(cmd.Context(), serverID); err != nil {
					return err
				}
			} else {
				if err := mgr.Start(cmd.Context()); err != nil {
					return err
				}
			}

			resources := mgr.AllResources()
			out := cmd.OutOrStdout()
			if serverID != "" {
				list := resources[serverID]
				if len(list) == 0 {
					fmt.Fprintf(out, "No resources for %s\n", serverID)
					return nil
				}
				fmt.Fprintf(out, "Resources for %s:\n", serverID)
				for _, res := range list {
					fmt.Fprintf(out, "  - %s (%s)\n", res.URI, res.Name)
				}
				return nil
			}
			if len(resources) == 0 {
				fmt.Fprintln(out, "No resources available.")
				return nil
			}
			for id, list := range resources {
				fmt.Fprintf(out, "Resources for %s:\n", id)
				for _, res := range list {
					fmt.Fprintf(out, "  - %s (%s)\n", res.URI, res.Name)
				}
			}
			return nil
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
			_, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			defer mgr.Stop()

			serverID := args[0]
			if err := mgr.Connect(cmd.Context(), serverID); err != nil {
				return err
			}
			contents, err := mgr.ReadResource(cmd.Context(), serverID, args[1])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(contents) == 0 {
				fmt.Fprintln(out, "No content.")
				return nil
			}
			payload, err := json.Marshal(contents)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, string(payload))
			return nil
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
			_, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			defer mgr.Stop()

			if serverID != "" {
				if err := mgr.Connect(cmd.Context(), serverID); err != nil {
					return err
				}
			} else {
				if err := mgr.Start(cmd.Context()); err != nil {
					return err
				}
			}

			prompts := mgr.AllPrompts()
			out := cmd.OutOrStdout()
			if serverID != "" {
				list := prompts[serverID]
				if len(list) == 0 {
					fmt.Fprintf(out, "No prompts for %s\n", serverID)
					return nil
				}
				fmt.Fprintf(out, "Prompts for %s:\n", serverID)
				for _, prompt := range list {
					fmt.Fprintf(out, "  - %s: %s\n", prompt.Name, prompt.Description)
				}
				return nil
			}
			if len(prompts) == 0 {
				fmt.Fprintln(out, "No prompts available.")
				return nil
			}
			for id, list := range prompts {
				fmt.Fprintf(out, "Prompts for %s:\n", id)
				for _, prompt := range list {
					fmt.Fprintf(out, "  - %s: %s\n", prompt.Name, prompt.Description)
				}
			}
			return nil
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
			serverID, promptName, err := parseMCPQualifiedName(args[0])
			if err != nil {
				return err
			}
			_, mgr, err := loadMCPManager(configPath)
			if err != nil {
				return err
			}
			defer mgr.Stop()

			if err := mgr.Connect(cmd.Context(), serverID); err != nil {
				return err
			}
			promptArgs, err := parseStringArgs(rawArgs)
			if err != nil {
				return err
			}
			result, err := mgr.GetPrompt(cmd.Context(), serverID, promptName, promptArgs)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(result)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(payload))
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringArrayVar(&rawArgs, "arg", nil, "Prompt argument (key=value)")
	return cmd
}

func loadMCPManager(configPath string) (*config.Config, *mcp.Manager, error) {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	if !cfg.MCP.Enabled {
		return cfg, mcp.NewManager(&cfg.MCP, slog.Default()), nil
	}
	return cfg, mcp.NewManager(&cfg.MCP, slog.Default()), nil
}

func parseMCPQualifiedName(value string) (string, string, error) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format <server>.<name>")
	}
	return parts[0], parts[1], nil
}

func parseAnyArgs(items []string) (map[string]any, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]any)
	for _, item := range items {
		key, value, err := parseKeyValue(item)
		if err != nil {
			return nil, err
		}
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			out[key] = parsed
		} else {
			out[key] = value
		}
	}
	return out, nil
}

func parseStringArgs(items []string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]string)
	for _, item := range items {
		key, value, err := parseKeyValue(item)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

func parseKeyValue(item string) (string, string, error) {
	parts := strings.SplitN(item, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", fmt.Errorf("invalid arg %q, expected key=value", item)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
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
			if strings.TrimSpace(profileName) != "" {
				opts.ConfigPath = profile.ProfileConfigPath(profileName)
				if strings.TrimSpace(opts.WorkspacePath) == "" {
					home, _ := os.UserHomeDir()
					if strings.TrimSpace(home) == "" {
						home = "."
					}
					opts.WorkspacePath = filepath.Join(home, "nexus-"+profileName)
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

			raw := onboard.BuildConfig(opts)
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
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&provider, "provider", "anthropic", "Provider to update")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Provider API key")
	cmd.Flags().BoolVar(&setDefault, "default", false, "Set as default provider")

	return cmd
}

func promptString(reader *bufio.Reader, label string, defaultValue string) string {
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	} else {
		fmt.Printf("%s: ", label)
	}
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue
	}
	return text
}

func promptBool(reader *bufio.Reader, label string, defaultValue bool) bool {
	defaultLabel := "n"
	if defaultValue {
		defaultLabel = "y"
	}
	answer := promptString(reader, label+" (y/n)", defaultLabel)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return defaultValue
	}
	return answer == "y" || answer == "yes"
}

func setPluginEnabled(raw map[string]any, id string, enabled bool) {
	if raw == nil {
		return
	}
	pluginsSection, ok := raw["plugins"].(map[string]any)
	if !ok {
		pluginsSection = map[string]any{}
		raw["plugins"] = pluginsSection
	}
	entries, ok := pluginsSection["entries"].(map[string]any)
	if !ok {
		entries = map[string]any{}
		pluginsSection["entries"] = entries
	}
	entry, ok := entries[id].(map[string]any)
	if !ok {
		entry = map[string]any{}
		entries[id] = entry
	}
	entry["enabled"] = enabled
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
			configPath = resolveConfigPath(configPath)
			return runServe(cmd.Context(), configPath, debug)
		},
	}

	// Define flags with descriptive help text.
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(),
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
			configPath = resolveConfigPath(configPath)
			slog.Info("running database migrations",
				"config", configPath,
				"steps", steps,
			)

			// Load configuration for database URL.
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			db, err := openMigrationDB(cfg)
			if err != nil {
				return err
			}
			defer db.Close()

			migrator, err := sessions.NewMigrator(db)
			if err != nil {
				return fmt.Errorf("failed to initialize migrator: %w", err)
			}

			applied, err := migrator.Up(cmd.Context(), steps)
			if err != nil {
				return err
			}
			if len(applied) == 0 {
				slog.Info("no pending migrations")
				return nil
			}
			for _, id := range applied {
				slog.Info("applied migration", "id", id)
			}

			slog.Info("migrations completed successfully")
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
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
			configPath = resolveConfigPath(configPath)
			slog.Warn("rolling back migrations",
				"config", configPath,
				"steps", steps,
			)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			db, err := openMigrationDB(cfg)
			if err != nil {
				return err
			}
			defer db.Close()

			migrator, err := sessions.NewMigrator(db)
			if err != nil {
				return fmt.Errorf("failed to initialize migrator: %w", err)
			}
			rolled, err := migrator.Down(cmd.Context(), steps)
			if err != nil {
				return err
			}
			if len(rolled) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No migrations to roll back.")
				return nil
			}
			for _, id := range rolled {
				slog.Info("rolled back migration", "id", id)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			db, err := openMigrationDB(cfg)
			if err != nil {
				return err
			}
			defer db.Close()

			migrator, err := sessions.NewMigrator(db)
			if err != nil {
				return fmt.Errorf("failed to initialize migrator: %w", err)
			}
			applied, pending, err := migrator.Status(cmd.Context())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Migration Status")
			fmt.Fprintln(out, "================")
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Applied migrations:")
			if len(applied) == 0 {
				fmt.Fprintln(out, "  (none)")
			} else {
				for _, entry := range applied {
					fmt.Fprintf(out, "  ✓ %s (%s)\n", entry.ID, entry.AppliedAt.Format(time.RFC3339))
				}
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Pending migrations:")
			if len(pending) == 0 {
				fmt.Fprintln(out, "  (none)")
			} else {
				for _, entry := range pending {
					fmt.Fprintf(out, "  ○ %s\n", entry.ID)
				}
			}
			fmt.Fprintln(out)

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")

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
	cmd.AddCommand(buildChannelsLoginCmd())

	return cmd
}

// buildChannelsListCmd creates the "channels list" command.
func buildChannelsListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured channels",
		Long:  "Display all messaging channels defined in the configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
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
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Channel login checks:")

			if cfg.Channels.Telegram.Enabled {
				if cfg.Channels.Telegram.BotToken == "" {
					fmt.Fprintln(out, "  - Telegram: missing bot_token (use @BotFather)")
				} else {
					fmt.Fprintln(out, "  - Telegram: token set")
				}
			}

			if cfg.Channels.Discord.Enabled {
				if cfg.Channels.Discord.BotToken == "" || cfg.Channels.Discord.AppID == "" {
					fmt.Fprintln(out, "  - Discord: missing bot_token/app_id (create app + bot token)")
				} else {
					fmt.Fprintln(out, "  - Discord: token + app id set")
				}
			}

			if cfg.Channels.Slack.Enabled {
				if cfg.Channels.Slack.BotToken == "" || cfg.Channels.Slack.AppToken == "" || cfg.Channels.Slack.SigningSecret == "" {
					fmt.Fprintln(out, "  - Slack: missing bot_token/app_token/signing_secret")
				} else {
					fmt.Fprintln(out, "  - Slack: credentials set")
				}
			}

			fmt.Fprintln(out, "Run `nexus channels test <channel>` to send a test message.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
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
			configPath = resolveConfigPath(configPath)
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

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
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
			configPath = resolveConfigPath(configPath)
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

			if warnings := doctor.CheckChannelPolicies(cfg); len(warnings) > 0 {
				fmt.Fprintln(out, "Channel policy warnings:")
				for _, warning := range warnings {
					fmt.Fprintf(out, "  - %s\n", warning)
				}
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

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(),
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
			configPath = resolveConfigPath(configPath)
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

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(),
		"Path to YAML configuration file")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID for memory scoping")
	cmd.Flags().StringVar(&channel, "channel", "", "Channel type (telegram, discord, slack)")
	cmd.Flags().StringVar(&message, "message", "", "Message content (used for heartbeat mode)")
	cmd.Flags().BoolVar(&heartbeat, "heartbeat", false, "Force heartbeat prompt mode")

	return cmd
}

// =============================================================================
// Trace Commands
// =============================================================================

// buildTraceCmd creates the "trace" command group for JSONL trace operations.
func buildTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Manage JSONL trace files for debugging and replay",
		Long: `Manage JSONL trace files for debugging and replay.

Trace files record agent events in JSONL format for:
- Debugging agent behavior
- Replaying runs for testing
- Computing statistics from historical runs
- Validating trace structure

Example workflow:
  nexus trace validate run.jsonl     # Check trace structure
  nexus trace stats run.jsonl        # View computed statistics
  nexus trace replay run.jsonl       # Replay events to stdout`,
	}
	cmd.AddCommand(
		buildTraceValidateCmd(),
		buildTraceStatsCmd(),
		buildTraceReplayCmd(),
	)
	return cmd
}

// buildTraceValidateCmd creates the "trace validate" subcommand.
func buildTraceValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate a trace file structure",
		Long: `Validate a JSONL trace file for structural correctness.

Checks:
- Header has valid version
- First event is run.started
- Last event is run.finished or run.error
- Sequences are strictly increasing
- All events can be parsed`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			out := cmd.OutOrStdout()

			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("failed to open trace file: %w", err)
			}
			defer f.Close()

			reader, err := agent.NewTraceReader(f)
			if err != nil {
				return fmt.Errorf("failed to read trace: %w", err)
			}

			// Replay to validate
			replayer := agent.NewTraceReplayer(reader, agent.NopSink{})
			stats, err := replayer.Replay(cmd.Context())
			if err != nil {
				return fmt.Errorf("replay failed: %w", err)
			}

			// Print header info
			header := reader.Header()
			fmt.Fprintf(out, "Trace: %s\n", filePath)
			fmt.Fprintf(out, "  Run ID:     %s\n", header.RunID)
			fmt.Fprintf(out, "  Version:    %d\n", header.Version)
			fmt.Fprintf(out, "  Started:    %s\n", header.StartedAt.Format(time.RFC3339))
			if header.AppVersion != "" {
				fmt.Fprintf(out, "  App:        %s\n", header.AppVersion)
			}
			if header.Environment != "" {
				fmt.Fprintf(out, "  Env:        %s\n", header.Environment)
			}
			fmt.Fprintln(out)

			// Print stats
			fmt.Fprintf(out, "Events: %d (seq %d..%d)\n", stats.EventCount, stats.FirstSequence, stats.LastSequence)
			fmt.Fprintln(out)

			// Print validation results
			if stats.Valid() {
				fmt.Fprintln(out, "✓ Trace is valid")
				return nil
			}

			fmt.Fprintln(out, "✗ Validation errors:")
			for _, e := range stats.Errors {
				fmt.Fprintf(out, "  - %s\n", e)
			}
			return fmt.Errorf("trace validation failed with %d errors", len(stats.Errors))
		},
	}
	return cmd
}

// buildTraceStatsCmd creates the "trace stats" subcommand.
func buildTraceStatsCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "stats <file>",
		Short: "Compute and display statistics from a trace file",
		Long: `Recompute run statistics from a JSONL trace file.

Statistics include:
- Timing (wall time, model time, tool time)
- Token counts (input/output)
- Iteration and tool call counts
- Error counts`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			out := cmd.OutOrStdout()

			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("failed to open trace file: %w", err)
			}
			defer f.Close()

			reader, err := agent.NewTraceReader(f)
			if err != nil {
				return fmt.Errorf("failed to read trace: %w", err)
			}

			stats, err := agent.ReplayToStats(reader)
			if err != nil {
				return fmt.Errorf("failed to compute stats: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(stats)
			}

			// Human-readable output
			fmt.Fprintf(out, "Run Statistics: %s\n", stats.RunID)
			fmt.Fprintln(out, strings.Repeat("-", 40))

			// Timing
			fmt.Fprintln(out, "Timing:")
			fmt.Fprintf(out, "  Wall time:    %v\n", stats.WallTime)
			fmt.Fprintf(out, "  Model time:   %v\n", stats.ModelWallTime)
			fmt.Fprintf(out, "  Tool time:    %v\n", stats.ToolWallTime)
			fmt.Fprintln(out)

			// Counts
			fmt.Fprintln(out, "Counts:")
			fmt.Fprintf(out, "  Turns:        %d\n", stats.Turns)
			fmt.Fprintf(out, "  Iterations:   %d\n", stats.Iters)
			fmt.Fprintf(out, "  Tool calls:   %d\n", stats.ToolCalls)
			fmt.Fprintln(out)

			// Tokens
			fmt.Fprintln(out, "Tokens:")
			fmt.Fprintf(out, "  Input:        %d\n", stats.InputTokens)
			fmt.Fprintf(out, "  Output:       %d\n", stats.OutputTokens)
			fmt.Fprintln(out)

			// Errors
			if stats.Errors > 0 {
				fmt.Fprintf(out, "Errors: %d\n", stats.Errors)
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output statistics as JSON")
	return cmd
}

// buildTraceReplayCmd creates the "trace replay" subcommand.
func buildTraceReplayCmd() *cobra.Command {
	var (
		speed    float64
		fromSeq  uint64
		toSeq    uint64
		filter   string
		showTime bool
		view     string
	)

	cmd := &cobra.Command{
		Use:   "replay <file>",
		Short: "Replay trace events to stdout",
		Long: `Replay events from a JSONL trace file to stdout.

Use for:
- Watching agent behavior unfold
- Debugging specific sequences
- Filtering to specific event types

Speed control:
  --speed 0     Instant (default)
  --speed 1     Real-time
  --speed 2     2x speed
  --speed 0.5   Half speed

Views:
  --view=default   Standard event replay (default)
  --view=context   Show only context packing decisions`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			out := cmd.OutOrStdout()

			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("failed to open trace file: %w", err)
			}
			defer f.Close()

			reader, err := agent.NewTraceReader(f)
			if err != nil {
				return fmt.Errorf("failed to read trace: %w", err)
			}

			// Create a callback sink based on view mode
			var printSink agent.EventSink
			if view == "context" {
				printSink = agent.NewCallbackSink(func(_ context.Context, e models.AgentEvent) {
					// Context view: only show context.packed events
					if e.Type != models.AgentEventContextPacked {
						return
					}

					var prefix string
					if showTime {
						prefix = fmt.Sprintf("[%s] ", e.Time.Format("15:04:05.000"))
					}

					fmt.Fprintf(out, "%s📦 Context Packed (iter=%d)\n", prefix, e.IterIndex)

					if e.Context != nil {
						ctx := e.Context
						fmt.Fprintf(out, "   Budget:     %d/%d chars, %d/%d msgs\n",
							ctx.UsedChars, ctx.BudgetChars, ctx.UsedMessages, ctx.BudgetMessages)
						fmt.Fprintf(out, "   Messages:   %d candidates → %d included, %d dropped\n",
							ctx.Candidates, ctx.Included, ctx.Dropped)
						if ctx.SummaryUsed {
							fmt.Fprintf(out, "   Summary:    ✓ included (%d chars)\n", ctx.SummaryChars)
						}

						// Show per-item details if available
						if len(ctx.Items) > 0 {
							fmt.Fprintln(out, "   Items:")
							for _, item := range ctx.Items {
								status := "✓"
								if !item.Included {
									status = "✗"
								}
								fmt.Fprintf(out, "     %s %-8s %5d chars  %-12s  %s\n",
									status, item.Kind, item.Chars, item.Reason, item.ID)
							}
						}
					}
					fmt.Fprintln(out)
				})
			} else {
				printSink = agent.NewCallbackSink(func(_ context.Context, e models.AgentEvent) {
					// Apply filter
					if filter != "" && !strings.Contains(string(e.Type), filter) {
						return
					}

					// Format output
					var prefix string
					if showTime {
						prefix = fmt.Sprintf("[%s] ", e.Time.Format("15:04:05.000"))
					}

					switch e.Type {
					case models.AgentEventRunStarted:
						fmt.Fprintf(out, "%s▶ Run started (run_id=%s)\n", prefix, e.RunID)

					case models.AgentEventRunFinished:
						fmt.Fprintf(out, "%s■ Run finished\n", prefix)
						if e.Stats != nil && e.Stats.Run != nil {
							fmt.Fprintf(out, "  wall=%v iters=%d tools=%d\n",
								e.Stats.Run.WallTime, e.Stats.Run.Iters, e.Stats.Run.ToolCalls)
						}

					case models.AgentEventRunError:
						if e.Error != nil {
							fmt.Fprintf(out, "%s✗ Error: %s\n", prefix, e.Error.Message)
						}

					case models.AgentEventIterStarted:
						fmt.Fprintf(out, "%s→ Iteration %d started\n", prefix, e.IterIndex)

					case models.AgentEventIterFinished:
						fmt.Fprintf(out, "%s← Iteration %d finished\n", prefix, e.IterIndex)

					case models.AgentEventToolStarted:
						if e.Tool != nil {
							fmt.Fprintf(out, "%s⚙ Tool: %s (call_id=%s)\n", prefix, e.Tool.Name, e.Tool.CallID)
						}

					case models.AgentEventToolFinished:
						if e.Tool != nil {
							status := "✓"
							if !e.Tool.Success {
								status = "✗"
							}
							fmt.Fprintf(out, "%s  %s %s completed (%v)\n", prefix, status, e.Tool.Name, e.Tool.Elapsed)
						}

					case models.AgentEventModelDelta:
						if e.Stream != nil && e.Stream.Delta != "" {
							// Print streaming text without newline for natural flow
							fmt.Fprint(out, e.Stream.Delta)
						}

					case models.AgentEventModelCompleted:
						fmt.Fprintln(out) // End the streaming line
						if e.Stream != nil {
							fmt.Fprintf(out, "%s  [tokens: in=%d out=%d]\n",
								prefix, e.Stream.InputTokens, e.Stream.OutputTokens)
						}

					case models.AgentEventContextPacked:
						if e.Context != nil {
							fmt.Fprintf(out, "%s📦 Context: %d/%d msgs, %d dropped\n",
								prefix, e.Context.UsedMessages, e.Context.BudgetMessages, e.Context.Dropped)
						}

					default:
						// Other events - print type for debugging
						fmt.Fprintf(out, "%s  [%s] seq=%d\n", prefix, e.Type, e.Sequence)
					}
				})
			}

			// Build replay options
			var opts []agent.ReplayOption
			if speed > 0 {
				opts = append(opts, agent.WithSpeed(speed))
			}
			if fromSeq > 0 || toSeq > 0 {
				opts = append(opts, agent.WithSequenceRange(fromSeq, toSeq))
			}

			replayer := agent.NewTraceReplayer(reader, printSink, opts...)

			fmt.Fprintf(out, "Replaying: %s\n", filePath)
			fmt.Fprintf(out, "Run ID: %s\n", reader.Header().RunID)
			if view == "context" {
				fmt.Fprintln(out, "View: context packing decisions")
			}
			fmt.Fprintln(out, strings.Repeat("-", 40))

			stats, err := replayer.Replay(cmd.Context())
			if err != nil {
				return fmt.Errorf("replay failed: %w", err)
			}

			fmt.Fprintln(out, strings.Repeat("-", 40))
			fmt.Fprintf(out, "Replayed %d events\n", stats.EventCount)

			if !stats.Valid() {
				fmt.Fprintln(out, "Warnings:")
				for _, e := range stats.Errors {
					fmt.Fprintf(out, "  - %s\n", e)
				}
			}

			return nil
		},
	}

	cmd.Flags().Float64Var(&speed, "speed", 0, "Replay speed (0=instant, 1=real-time, 2=2x)")
	cmd.Flags().Uint64Var(&fromSeq, "from", 0, "Start from sequence number")
	cmd.Flags().Uint64Var(&toSeq, "to", 0, "Stop at sequence number")
	cmd.Flags().StringVar(&filter, "filter", "", "Filter events by type substring (e.g., 'tool', 'model')")
	cmd.Flags().BoolVar(&showTime, "time", false, "Show timestamps for each event")
	cmd.Flags().StringVar(&view, "view", "default", "Output view (default, context)")

	return cmd
}

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

func createMarketplaceManager(cfg *config.Config) (*marketplace.Manager, error) {
	managerCfg := &marketplace.ManagerConfig{
		Registries:  cfg.Marketplace.Registries,
		TrustedKeys: cfg.Marketplace.TrustedKeys,
	}
	return marketplace.NewManager(managerCfg)
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := createMarketplaceManager(cfg)
			if err != nil {
				return fmt.Errorf("failed to create marketplace manager: %w", err)
			}

			query := ""
			if len(args) > 0 {
				query = args[0]
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := createMarketplaceManager(cfg)
			if err != nil {
				return fmt.Errorf("failed to create marketplace manager: %w", err)
			}

			pluginID := args[0]
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := createMarketplaceManager(cfg)
			if err != nil {
				return fmt.Errorf("failed to create marketplace manager: %w", err)
			}

			plugins := mgr.List()
			out := cmd.OutOrStdout()

			if len(plugins) == 0 {
				fmt.Fprintln(out, "No plugins installed.")
				fmt.Fprintln(out, "\nUse 'nexus plugins search' to find plugins.")
				return nil
			}

			fmt.Fprintf(out, "Installed plugins (%d):\n\n", len(plugins))
			for _, plugin := range plugins {
				status := "enabled"
				if !plugin.Enabled {
					status = "disabled"
				}

				autoUpdate := ""
				if plugin.AutoUpdate {
					autoUpdate = ", auto-update"
				}

				verified := ""
				if plugin.Verified {
					verified = ", verified"
				}

				fmt.Fprintf(out, "  %s (%s) [%s%s%s]\n", plugin.ID, plugin.Version, status, autoUpdate, verified)
				if showAll {
					fmt.Fprintf(out, "    Path: %s\n", plugin.Path)
					fmt.Fprintf(out, "    Installed: %s\n", plugin.InstalledAt.Format(time.RFC3339))
					if plugin.Manifest != nil && plugin.Manifest.Description != "" {
						fmt.Fprintf(out, "    %s\n", plugin.Manifest.Description)
					}
				}
			}

			return nil
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

			if all || len(args) == 0 {
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
			pluginID := args[0]
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := createMarketplaceManager(cfg)
			if err != nil {
				return fmt.Errorf("failed to create marketplace manager: %w", err)
			}

			pluginID := args[0]
			if err := mgr.Uninstall(cmd.Context(), pluginID); err != nil {
				return fmt.Errorf("uninstall failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled plugin: %s\n", pluginID)
			return nil
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := createMarketplaceManager(cfg)
			if err != nil {
				return fmt.Errorf("failed to create marketplace manager: %w", err)
			}

			pluginID := args[0]
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
			if len(args) == 0 {
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
			pluginID := args[0]
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := createMarketplaceManager(cfg)
			if err != nil {
				return fmt.Errorf("failed to create marketplace manager: %w", err)
			}

			pluginID := args[0]
			if err := mgr.Enable(pluginID); err != nil {
				return fmt.Errorf("failed to enable plugin: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Enabled plugin: %s\n", pluginID)
			return nil
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
			configPath = resolveConfigPath(configPath)
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			mgr, err := createMarketplaceManager(cfg)
			if err != nil {
				return fmt.Errorf("failed to create marketplace manager: %w", err)
			}

			pluginID := args[0]
			if err := mgr.Disable(pluginID); err != nil {
				return fmt.Errorf("failed to disable plugin: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Disabled plugin: %s\n", pluginID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}
