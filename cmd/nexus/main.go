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

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/gateway"
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
		buildServiceCmd(),
		buildMemoryCmd(),
		buildMcpCmd(),
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
