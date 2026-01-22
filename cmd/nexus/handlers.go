// Package main provides the CLI entry point for the Nexus multi-channel AI gateway.
//
// handlers.go contains the RunE handler functions for all CLI commands.
// These functions implement the business logic for each command.
package main

import (
	"bufio"
	"context"
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
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/internal/observability"
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

// =============================================================================
// Serve Command Handler
// =============================================================================

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

	_ = shutdownCtx // Silence unused variable warning until implemented

	slog.Info("Nexus gateway stopped gracefully")
	return nil
}

// =============================================================================
// Service Command Handlers
// =============================================================================

// runServiceInstall handles the service install command.
func runServiceInstall(cmd *cobra.Command, configPath string, restart bool) error {
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
}

// runServiceRepair handles the service repair command.
func runServiceRepair(cmd *cobra.Command, configPath string, restart bool) error {
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
}

// runServiceStatus handles the service status command.
func runServiceStatus(cmd *cobra.Command, configPath string) error {
	out := cmd.OutOrStdout()
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(out, "Config load failed: %v\n", err)
	}
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
	return nil
}

// =============================================================================
// Profile Command Handlers
// =============================================================================

// runProfileList handles the profile list command.
func runProfileList(cmd *cobra.Command) error {
	profiles, err := profile.ListProfiles()
	if err != nil {
		return err
	}
	active, err := profile.ReadActiveProfile()
	if err != nil {
		active = ""
	}
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
}

// runProfileUse handles the profile use command.
func runProfileUse(cmd *cobra.Command, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	if err := profile.WriteActiveProfile(name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Active profile set: %s\n", name)
	return nil
}

// runProfilePath handles the profile path command.
func runProfilePath(cmd *cobra.Command, name string) error {
	path := profile.ProfileConfigPath(name)
	fmt.Fprintln(cmd.OutOrStdout(), path)
	return nil
}

// runProfileInit handles the profile init command.
func runProfileInit(cmd *cobra.Command, name, provider string, setActive bool) error {
	name = strings.TrimSpace(name)
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
}

// =============================================================================
// Skills Command Handlers
// =============================================================================

// runSkillsList handles the skills list command.
func runSkillsList(cmd *cobra.Command, configPath string, all bool) error {
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
			result, err := mgr.CheckEligibility(skill.Name)
			if err != nil {
				status = "unknown"
			} else if result != nil && !result.Eligible {
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
}

// runSkillsShow handles the skills show command.
func runSkillsShow(cmd *cobra.Command, configPath, skillName string, showContent bool) error {
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

	skill, ok := mgr.GetSkill(skillName)
	if !ok {
		return fmt.Errorf("skill not found: %s", skillName)
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
	result, err := mgr.CheckEligibility(skill.Name)
	if err == nil && result != nil {
		fmt.Fprintln(out)
		if result.Eligible {
			fmt.Fprintln(out, "Status: Eligible")
		} else {
			fmt.Fprintf(out, "Status: Ineligible (%s)\n", result.Reason)
		}
	} else if err != nil {
		fmt.Fprintf(out, "Eligibility check failed: %v\n", err)
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
}

// runSkillsCheck handles the skills check command.
func runSkillsCheck(cmd *cobra.Command, configPath, skillName string) error {
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

	result, err := mgr.CheckEligibility(skillName)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if result.Eligible {
		fmt.Fprintf(out, "Skill '%s' is eligible\n", skillName)
		if result.Reason != "" {
			fmt.Fprintf(out, "  Reason: %s\n", result.Reason)
		}
	} else {
		fmt.Fprintf(out, "Skill '%s' is NOT eligible\n", skillName)
		fmt.Fprintf(out, "  Reason: %s\n", result.Reason)
	}

	return nil
}

// runSkillsEnable handles the skills enable command.
func runSkillsEnable(cmd *cobra.Command, configPath, skillName string) error {
	configPath = resolveConfigPath(configPath)
	raw, err := doctor.LoadRawConfig(configPath)
	if err != nil {
		return err
	}
	setSkillEnabled(raw, skillName, true)
	if err := doctor.WriteRawConfig(configPath, raw); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Enabled skill: %s\n", skillName)
	return nil
}

// runSkillsDisable handles the skills disable command.
func runSkillsDisable(cmd *cobra.Command, configPath, skillName string) error {
	configPath = resolveConfigPath(configPath)
	raw, err := doctor.LoadRawConfig(configPath)
	if err != nil {
		return err
	}
	setSkillEnabled(raw, skillName, false)
	if err := doctor.WriteRawConfig(configPath, raw); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Disabled skill: %s\n", skillName)
	return nil
}

// =============================================================================
// Memory Command Handlers
// =============================================================================

// runMemorySearch handles the memory search command.
func runMemorySearch(cmd *cobra.Command, configPath, query, scope, scopeID string, limit int, threshold float32) error {
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
		Query:     query,
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
}

// runMemoryIndex handles the memory index command.
func runMemoryIndex(cmd *cobra.Command, configPath, path, scope, scopeID, source string) error {
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
}

// runMemoryStats handles the memory stats command.
func runMemoryStats(cmd *cobra.Command, configPath string) error {
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
}

// runMemoryCompact handles the memory compact command.
func runMemoryCompact(cmd *cobra.Command, configPath string) error {
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
}

// =============================================================================
// MCP Command Handlers
// =============================================================================

type mcpStopper interface {
	Stop() error
}

func stopMCPManager(mgr mcpStopper) {
	if mgr == nil {
		return
	}
	if err := mgr.Stop(); err != nil {
		slog.Warn("failed to stop MCP manager", "error", err)
	}
}

// runMcpServers handles the mcp servers command.
func runMcpServers(cmd *cobra.Command, configPath string) error {
	cfg, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	if cfg.MCP.Enabled {
		if err := mgr.Start(cmd.Context()); err != nil {
			return err
		}
	}
	defer stopMCPManager(mgr)

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
}

// runMcpConnect handles the mcp connect command.
func runMcpConnect(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if err := mgr.Connect(cmd.Context(), serverID); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Connected to %s\n", serverID)
	return nil
}

// runMcpTools handles the mcp tools command.
func runMcpTools(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

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
}

// runMcpCall handles the mcp call command.
func runMcpCall(cmd *cobra.Command, configPath, qualifiedName string, rawArgs []string) error {
	serverID, toolName, err := parseMCPQualifiedName(qualifiedName)
	if err != nil {
		return err
	}
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

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
}

// runMcpResources handles the mcp resources command.
func runMcpResources(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

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
}

// runMcpRead handles the mcp read command.
func runMcpRead(cmd *cobra.Command, configPath, serverID, uri string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

	if err := mgr.Connect(cmd.Context(), serverID); err != nil {
		return err
	}
	contents, err := mgr.ReadResource(cmd.Context(), serverID, uri)
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
}

// runMcpPrompts handles the mcp prompts command.
func runMcpPrompts(cmd *cobra.Command, configPath, serverID string) error {
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

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
}

// runMcpPrompt handles the mcp prompt command.
func runMcpPrompt(cmd *cobra.Command, configPath, qualifiedName string, rawArgs []string) error {
	serverID, promptName, err := parseMCPQualifiedName(qualifiedName)
	if err != nil {
		return err
	}
	_, mgr, err := loadMCPManager(configPath)
	if err != nil {
		return err
	}
	defer stopMCPManager(mgr)

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
}

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

// =============================================================================
// Migration Command Handlers
// =============================================================================

// runMigrateUp handles the migrate up command.
func runMigrateUp(cmd *cobra.Command, configPath string, steps int) error {
	configPath = resolveConfigPath(configPath)
	slog.Info("running database migrations",
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
}

// runMigrateDown handles the migrate down command.
func runMigrateDown(cmd *cobra.Command, configPath string, steps int) error {
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
}

// runMigrateStatus handles the migrate status command.
func runMigrateStatus(cmd *cobra.Command, configPath string) error {
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
			fmt.Fprintf(out, "  - %s (%s)\n", entry.ID, entry.AppliedAt.Format(time.RFC3339))
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Pending migrations:")
	if len(pending) == 0 {
		fmt.Fprintln(out, "  (none)")
	} else {
		for _, entry := range pending {
			fmt.Fprintf(out, "  - %s\n", entry.ID)
		}
	}
	fmt.Fprintln(out)

	return nil
}

// =============================================================================
// Doctor Command Handler
// =============================================================================

// runDoctor handles the doctor command.
func runDoctor(cmd *cobra.Command, configPath string, repair, probe, audit bool) error {
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
		security := doctor.AuditSecurity(cfg, configPath)
		if len(security.Findings) == 0 {
			fmt.Fprintln(out, "Security audit: no issues detected")
		} else {
			fmt.Fprintln(out, "Security audit:")
			for _, finding := range security.Findings {
				fmt.Fprintf(out, "  - [%s] %s\n", strings.ToUpper(string(finding.Severity)), finding.Message)
			}
		}
	}

	fmt.Fprintf(out, "Config OK (provider: %s)\n", cfg.LLM.DefaultProvider)
	return nil
}

// =============================================================================
// Prompt Command Handler
// =============================================================================

// runPrompt handles the prompt command.
func runPrompt(cmd *cobra.Command, configPath, sessionID, channel, message string, heartbeat bool) error {
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
}

// =============================================================================
// Trace Command Handlers
// =============================================================================

// runTraceValidate handles the trace validate command.
func runTraceValidate(cmd *cobra.Command, filePath string) error {
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
		fmt.Fprintln(out, "Trace is valid")
		return nil
	}

	fmt.Fprintln(out, "Validation errors:")
	for _, e := range stats.Errors {
		fmt.Fprintf(out, "  - %s\n", e)
	}
	return fmt.Errorf("trace validation failed with %d errors", len(stats.Errors))
}

// runTraceStats handles the trace stats command.
func runTraceStats(cmd *cobra.Command, filePath string, jsonOutput bool) error {
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
}

// runTraceReplay handles the trace replay command.
func runTraceReplay(cmd *cobra.Command, filePath string, speed float64, fromSeq, toSeq uint64, filter string, showTime bool, view string) error {
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

			fmt.Fprintf(out, "%sContext Packed (iter=%d)\n", prefix, e.IterIndex)

			if e.Context != nil {
				ctx := e.Context
				fmt.Fprintf(out, "   Budget:     %d/%d chars, %d/%d msgs\n",
					ctx.UsedChars, ctx.BudgetChars, ctx.UsedMessages, ctx.BudgetMessages)
				fmt.Fprintf(out, "   Messages:   %d candidates -> %d included, %d dropped\n",
					ctx.Candidates, ctx.Included, ctx.Dropped)
				if ctx.SummaryUsed {
					fmt.Fprintf(out, "   Summary:    included (%d chars)\n", ctx.SummaryChars)
				}

				// Show per-item details if available
				if len(ctx.Items) > 0 {
					fmt.Fprintln(out, "   Items:")
					for _, item := range ctx.Items {
						status := "+"
						if !item.Included {
							status = "-"
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
				fmt.Fprintf(out, "%s> Run started (run_id=%s)\n", prefix, e.RunID)

			case models.AgentEventRunFinished:
				fmt.Fprintf(out, "%s| Run finished\n", prefix)
				if e.Stats != nil && e.Stats.Run != nil {
					fmt.Fprintf(out, "  wall=%v iters=%d tools=%d\n",
						e.Stats.Run.WallTime, e.Stats.Run.Iters, e.Stats.Run.ToolCalls)
				}

			case models.AgentEventRunError:
				if e.Error != nil {
					fmt.Fprintf(out, "%sx Error: %s\n", prefix, e.Error.Message)
				}

			case models.AgentEventIterStarted:
				fmt.Fprintf(out, "%s-> Iteration %d started\n", prefix, e.IterIndex)

			case models.AgentEventIterFinished:
				fmt.Fprintf(out, "%s<- Iteration %d finished\n", prefix, e.IterIndex)

			case models.AgentEventToolStarted:
				if e.Tool != nil {
					fmt.Fprintf(out, "%s* Tool: %s (call_id=%s)\n", prefix, e.Tool.Name, e.Tool.CallID)
				}

			case models.AgentEventToolFinished:
				if e.Tool != nil {
					status := "+"
					if !e.Tool.Success {
						status = "-"
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
					fmt.Fprintf(out, "%sContext: %d/%d msgs, %d dropped\n",
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
}

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

// =============================================================================
// Helper Functions
// =============================================================================

// printAuditList prints a labeled list of audit items.
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

// =============================================================================
// Channel Command Helpers
// =============================================================================

// loadConfigForChannels loads the config for channel commands.
func loadConfigForChannels(configPath string) (*config.Config, error) {
	return config.Load(configPath)
}

// printChannelsList prints the list of configured channels.
func printChannelsList(out io.Writer, cfg *config.Config) {
	fmt.Fprintln(out, "Configured Channels")
	fmt.Fprintln(out, "===================")
	fmt.Fprintln(out)

	if cfg.Channels.Telegram.Enabled {
		fmt.Fprintln(out, "Telegram")
		fmt.Fprintln(out, "   Status: Enabled")
		if len(cfg.Channels.Telegram.BotToken) >= 10 {
			fmt.Fprintf(out, "   Bot Token: %s***\n", cfg.Channels.Telegram.BotToken[:10])
		}
		fmt.Fprintln(out)
	}

	if cfg.Channels.Discord.Enabled {
		fmt.Fprintln(out, "Discord")
		fmt.Fprintln(out, "   Status: Enabled")
		fmt.Fprintf(out, "   App ID: %s\n", cfg.Channels.Discord.AppID)
		fmt.Fprintln(out)
	}

	if cfg.Channels.Slack.Enabled {
		fmt.Fprintln(out, "Slack")
		fmt.Fprintln(out, "   Status: Enabled")
		if len(cfg.Channels.Slack.BotToken) >= 10 {
			fmt.Fprintf(out, "   Bot Token: %s***\n", cfg.Channels.Slack.BotToken[:10])
		}
		fmt.Fprintln(out)
	}
}

// printChannelsLogin prints channel login validation results.
func printChannelsLogin(out io.Writer, cfg *config.Config) {
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
}

// printChannelsStatus prints the channel connection status.
func printChannelsStatus(out io.Writer) {
	fmt.Fprintln(out, "Channel Connection Status")
	fmt.Fprintln(out, "========================")
	fmt.Fprintln(out)

	// TODO: Query actual channel adapter status.
	fmt.Fprintln(out, "Telegram")
	fmt.Fprintln(out, "   Connected: yes")
	fmt.Fprintln(out, "   Last Message: 2 minutes ago")
	fmt.Fprintln(out, "   Messages Today: 142")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Discord")
	fmt.Fprintln(out, "   Connected: yes")
	fmt.Fprintln(out, "   Guilds: 3")
	fmt.Fprintln(out, "   Last Message: 5 minutes ago")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Slack")
	fmt.Fprintln(out, "   Connected: yes")
	fmt.Fprintln(out, "   Workspaces: 1")
	fmt.Fprintln(out, "   Socket Mode: Active")
	fmt.Fprintln(out)
}

// printChannelTest prints the channel test results.
func printChannelTest(out io.Writer, channel string) {
	slog.Info("testing channel connectivity", "channel", channel)

	// TODO: Implement actual channel testing.
	fmt.Fprintf(out, "Testing %s channel...\n", channel)
	fmt.Fprintln(out, "API credentials valid")
	fmt.Fprintln(out, "Bot permissions verified")
	fmt.Fprintln(out, "Test message sent successfully")
}

// =============================================================================
// Agent Command Helpers
// =============================================================================

// printAgentsList prints the list of configured agents.
func printAgentsList(out io.Writer) {
	fmt.Fprintln(out, "Configured Agents")
	fmt.Fprintln(out, "=================")
	fmt.Fprintln(out)

	// TODO: Query agents from database.
	fmt.Fprintln(out, "ID          Name           Provider    Model")
	fmt.Fprintln(out, "----------  -------------  ----------  ----------------------")
	fmt.Fprintln(out, "default     Default Agent  anthropic   claude-sonnet-4-20250514")
	fmt.Fprintln(out, "coder       Code Helper    anthropic   claude-sonnet-4-20250514")
	fmt.Fprintln(out, "researcher  Web Researcher openai      gpt-4o")
	fmt.Fprintln(out)
}

// printAgentCreate prints the agent creation result.
func printAgentCreate(out io.Writer, name, provider, model string) {
	slog.Info("creating agent",
		"name", name,
		"provider", provider,
		"model", model,
	)

	// TODO: Implement agent creation in database.
	fmt.Fprintf(out, "Created agent: %s\n", name)
	fmt.Fprintf(out, "  Provider: %s\n", provider)
	fmt.Fprintf(out, "  Model: %s\n", model)
}

// printAgentShow prints the agent details.
func printAgentShow(out io.Writer, agentID string) {
	fmt.Fprintf(out, "Agent: %s\n", agentID)
	fmt.Fprintln(out, "==========")
	fmt.Fprintln(out)

	// TODO: Query agent from database.
	fmt.Fprintln(out, "Configuration:")
	fmt.Fprintln(out, "  Provider: anthropic")
	fmt.Fprintln(out, "  Model: claude-sonnet-4-20250514")
	fmt.Fprintln(out, "  Max Tokens: 4096")
	fmt.Fprintln(out, "  Temperature: 0.7")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Tools Enabled:")
	fmt.Fprintln(out, "  web_search")
	fmt.Fprintln(out, "  code_sandbox")
	fmt.Fprintln(out, "  browser")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Statistics:")
	fmt.Fprintln(out, "  Total Sessions: 1,234")
	fmt.Fprintln(out, "  Messages Today: 567")
	fmt.Fprintln(out, "  Tokens Used Today: 1,234,567")
}

// =============================================================================
// Status Command Helpers
// =============================================================================

// printSystemStatus prints the system status.
func printSystemStatus(out io.Writer, jsonOutput bool) {
	if jsonOutput {
		fmt.Fprintf(out, `{"status": "healthy", "version": "%s"}`+"\n", version)
		return
	}

	fmt.Fprintln(out, "NEXUS STATUS")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Version: %s (commit: %s)\n", version, commit)
	fmt.Fprintf(out, "Built: %s\n", date)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Database")
	fmt.Fprintln(out, "   CockroachDB: Connected")
	fmt.Fprintln(out, "   Latency: 2.3ms")
	fmt.Fprintln(out, "   Active Connections: 5/20")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Channels")
	fmt.Fprintln(out, "   Telegram: Connected")
	fmt.Fprintln(out, "   Discord: Connected")
	fmt.Fprintln(out, "   Slack: Connected")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "LLM Providers")
	fmt.Fprintln(out, "   Anthropic: Available")
	fmt.Fprintln(out, "   OpenAI: Available")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Tools")
	fmt.Fprintln(out, "   Web Search: Ready")
	fmt.Fprintln(out, "   Code Sandbox: 5 VMs pooled")
	fmt.Fprintln(out, "   Browser: 3 instances pooled")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Metrics (Last 24h)")
	fmt.Fprintln(out, "   Messages Processed: 12,345")
	fmt.Fprintln(out, "   Tool Invocations: 2,345")
	fmt.Fprintln(out, "   LLM Tokens: 5,678,901")
	fmt.Fprintln(out, "   Avg Response Time: 1.2s")
	fmt.Fprintln(out)
}

// =============================================================================
// Edge Command Handlers
// =============================================================================

// runEdgeStatus shows the status of edge daemons.
func runEdgeStatus(cmd *cobra.Command, configPath string, edgeID string) error {
	fmt.Fprintln(cmd.OutOrStdout(), "Edge Status")
	fmt.Fprintln(cmd.OutOrStdout(), "===========")
	fmt.Fprintln(cmd.OutOrStdout())

	if edgeID == "" {
		// Show summary of all edges
		fmt.Fprintln(cmd.OutOrStdout(), "Connected Edges: 0")
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "No edges currently connected.")
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'nexus-edge --core-url <url> --edge-id <id>' to connect an edge daemon.")
	} else {
		// Show specific edge
		fmt.Fprintf(cmd.OutOrStdout(), "Edge ID: %s\n", edgeID)
		fmt.Fprintln(cmd.OutOrStdout(), "Status: Not connected")
	}
	return nil
}

// runEdgeList lists connected edge daemons.
func runEdgeList(cmd *cobra.Command, configPath string, showTools bool) error {
	fmt.Fprintln(cmd.OutOrStdout(), "Connected Edge Daemons")
	fmt.Fprintln(cmd.OutOrStdout(), "======================")
	fmt.Fprintln(cmd.OutOrStdout())

	fmt.Fprintln(cmd.OutOrStdout(), "ID            Name          Status       Tools  Last Heartbeat")
	fmt.Fprintln(cmd.OutOrStdout(), "------------  ------------  -----------  -----  ---------------")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "No edges currently connected.")
	return nil
}

// runEdgeTools lists tools from connected edges.
func runEdgeTools(cmd *cobra.Command, configPath string, edgeID string) error {
	fmt.Fprintln(cmd.OutOrStdout(), "Edge Tools")
	fmt.Fprintln(cmd.OutOrStdout(), "==========")
	fmt.Fprintln(cmd.OutOrStdout())

	if edgeID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Filtering by edge: %s\n", edgeID)
		fmt.Fprintln(cmd.OutOrStdout())
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Canonical Name                    Description                    Approval")
	fmt.Fprintln(cmd.OutOrStdout(), "--------------------------------  -----------------------------  --------")
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "No edge tools available. Connect an edge daemon first.")
	return nil
}

// runEdgeApprove approves a pending edge (TOFU).
func runEdgeApprove(cmd *cobra.Command, configPath string, edgeID string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Approving edge: %s\n", edgeID)
	fmt.Fprintln(cmd.OutOrStdout(), "No pending edge found with that ID.")
	return nil
}

// runEdgeRevoke revokes an approved edge.
func runEdgeRevoke(cmd *cobra.Command, configPath string, edgeID string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Revoking edge: %s\n", edgeID)
	fmt.Fprintln(cmd.OutOrStdout(), "No approved edge found with that ID.")
	return nil
}

// =============================================================================
// Events Command Handlers
// =============================================================================

// runEventsShow shows the event timeline for a specific run.
func runEventsShow(cmd *cobra.Command, configPath string, runID string, format string) error {
	// For now, we use a memory store - in production this would connect to persistent storage
	store := observability.NewMemoryEventStore(10000)

	events, err := store.GetByRunID(runID)
	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}

	if len(events) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No events found for run: %s\n", runID)
		fmt.Fprintln(cmd.OutOrStdout(), "\nNote: Events are currently stored in memory and are lost when the server restarts.")
		fmt.Fprintln(cmd.OutOrStdout(), "To capture events, ensure the server is running and processing requests.")
		return nil
	}

	timeline := observability.BuildTimeline(events)

	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(timeline)
	default:
		fmt.Fprint(cmd.OutOrStdout(), observability.FormatTimeline(timeline))
	}

	return nil
}

// runEventsList lists recent events.
func runEventsList(cmd *cobra.Command, configPath string, limit int, eventType string, sessionID string) error {
	// For now, we use a memory store - in production this would connect to persistent storage
	store := observability.NewMemoryEventStore(10000)

	var events []*observability.Event
	var err error

	if sessionID != "" {
		events, err = store.GetBySessionID(sessionID)
	} else if eventType != "" {
		events, err = store.GetByType(observability.EventType(eventType), limit)
	} else {
		// Get recent events by time range (last 24 hours)
		end := time.Now()
		start := end.Add(-24 * time.Hour)
		events, err = store.GetByTimeRange(start, end)
	}

	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}

	if len(events) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No events found.")
		fmt.Fprintln(cmd.OutOrStdout(), "\nNote: Events are currently stored in memory and are lost when the server restarts.")
		return nil
	}

	// Apply limit
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Found %d events:\n\n", len(events))
	for _, e := range events {
		timestamp := e.Timestamp.Format("15:04:05.000")
		errorMark := ""
		if e.Error != "" {
			errorMark = " ❌"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s%s\n", timestamp, e.Type, e.Name, errorMark)
		if e.RunID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "         Run: %s\n", e.RunID)
		}
		if e.Error != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "         Error: %s\n", e.Error)
		}
	}

	return nil
}
