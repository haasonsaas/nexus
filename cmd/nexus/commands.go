// Package main provides the CLI entry point for the Nexus multi-channel AI gateway.
//
// commands.go contains all cobra command definitions and their flag configurations.
// Each command builder function creates a command and wires it to its handler.
package main

import (
	"github.com/haasonsaas/nexus/internal/onboard"
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Serve Command
// =============================================================================

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

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(),
		"Path to YAML configuration file")
	cmd.Flags().BoolVarP(&debug, "debug", "d", false,
		"Enable debug logging (verbose output)")

	return cmd
}

// =============================================================================
// Service Commands
// =============================================================================

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
			return runServiceInstall(cmd, configPath, restart)
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
			return runServiceRepair(cmd, configPath, restart)
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
			return runServiceStatus(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

// =============================================================================
// Profile Commands
// =============================================================================

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
			return runProfileList(cmd)
		},
	}
}

func buildProfileUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use [name]",
		Short: "Set the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileUse(cmd, args[0])
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
			return runProfilePath(cmd, name)
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
			return runProfileInit(cmd, args[0], provider, setActive)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "anthropic", "Default LLM provider")
	cmd.Flags().BoolVar(&setActive, "use", false, "Set as active profile after creation")
	return cmd
}

// =============================================================================
// Pairing Commands
// =============================================================================

// buildPairingCmd creates the "pairing" command group.
func buildPairingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pairing",
		Short: "Manage pairing requests for messaging channels",
	}
	cmd.AddCommand(buildPairingListCmd(), buildPairingApproveCmd(), buildPairingDenyCmd())
	return cmd
}

func buildPairingListCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pending pairing requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingList(cmd, provider)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Filter by provider (telegram, discord, slack, whatsapp, signal, imessage, matrix, teams)")
	return cmd
}

func buildPairingApproveCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "approve [code]",
		Short: "Approve a pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingApprove(cmd, args[0], provider)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Provider for the pairing code")
	return cmd
}

func buildPairingDenyCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "deny [code]",
		Short: "Deny a pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingDeny(cmd, args[0], provider)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Provider for the pairing code")
	return cmd
}

// =============================================================================
// Skills Commands
// =============================================================================

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
			return runSkillsList(cmd, configPath, all)
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
			return runSkillsShow(cmd, configPath, args[0], showContent)
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
			return runSkillsCheck(cmd, configPath, args[0])
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
			return runSkillsEnable(cmd, configPath, args[0])
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
			return runSkillsDisable(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

// =============================================================================
// Extensions Commands
// =============================================================================

func buildExtensionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extensions",
		Short: "List configured extensions (skills, plugins, MCP)",
	}
	cmd.AddCommand(buildExtensionsListCmd())
	return cmd
}

func buildExtensionsListCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured extensions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExtensionsList(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

// =============================================================================
// Memory Commands
// =============================================================================

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
			return runMemorySearch(cmd, configPath, args[0], scope, scopeID, limit, threshold)
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
			return runMemoryIndex(cmd, configPath, args[0], scope, scopeID, source)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&scope, "scope", "global", "Memory scope (session, channel, agent, global)")
	cmd.Flags().StringVar(&scopeID, "scope-id", "", "Scope ID")
	cmd.Flags().StringVar(&source, "source", "document", "Source label for indexed content")
	return cmd
}

func buildMemoryStatsCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show memory statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemoryStats(cmd, configPath)
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
			return runMemoryCompact(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

// =============================================================================
// RAG Commands
// =============================================================================

// buildRagCmd creates the "rag" command group.
func buildRagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rag",
		Short: "Evaluate and inspect RAG retrieval quality",
	}
	cmd.AddCommand(buildRagEvalCmd(), buildRagPackCmd())
	return cmd
}

func buildRagEvalCmd() *cobra.Command {
	var (
		configPath  string
		testSet     string
		output      string
		limit       int
		threshold   float32
		judge       bool
		judgeModel  string
		judgeProv   string
		judgeTokens int
	)
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run RAG evaluation against a test set",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRagEval(cmd, configPath, testSet, output, limit, threshold, judge, judgeModel, judgeProv, judgeTokens)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&testSet, "test-set", "", "Path to RAG evaluation test set (YAML)")
	cmd.Flags().StringVar(&output, "output", "", "Write JSON report to file (optional)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of retrieval results per case")
	cmd.Flags().Float32Var(&threshold, "threshold", 0.7, "Minimum similarity threshold (0-1)")
	cmd.Flags().BoolVar(&judge, "judge", false, "Enable LLM-as-judge scoring")
	cmd.Flags().StringVar(&judgeProv, "judge-provider", "", "Provider ID for LLM judge (defaults to llm.default_provider)")
	cmd.Flags().StringVar(&judgeModel, "judge-model", "", "Model ID for LLM judge (defaults to provider default)")
	cmd.Flags().IntVar(&judgeTokens, "judge-max-tokens", 1024, "Max tokens for answer generation when judging")
	if err := cmd.MarkFlagRequired("test-set"); err != nil {
		panic(err)
	}
	return cmd
}

func buildRagPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Manage RAG knowledge packs",
	}
	cmd.AddCommand(buildRagPackInstallCmd())
	return cmd
}

func buildRagPackInstallCmd() *cobra.Command {
	var (
		configPath string
		packDir    string
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a knowledge pack into RAG storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRagPackInstall(cmd, configPath, packDir)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&packDir, "path", "", "Path to knowledge pack directory")
	if err := cmd.MarkFlagRequired("path"); err != nil {
		panic(err)
	}
	return cmd
}

// =============================================================================
// MCP Commands
// =============================================================================

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
			return runMcpServers(cmd, configPath)
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
			return runMcpConnect(cmd, configPath, args[0])
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
			return runMcpTools(cmd, configPath, serverID)
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
			return runMcpCall(cmd, configPath, args[0], rawArgs)
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
			return runMcpResources(cmd, configPath, serverID)
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
			return runMcpRead(cmd, configPath, args[0], args[1])
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
			return runMcpPrompts(cmd, configPath, serverID)
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
			return runMcpPrompt(cmd, configPath, args[0], rawArgs)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringArrayVar(&rawArgs, "arg", nil, "Prompt argument (key=value)")
	return cmd
}

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

// =============================================================================
// Migration Commands
// =============================================================================

// buildMigrateCmd creates the "migrate" command group for migrations.
func buildMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migration commands",
		Long: `Manage database migrations and workspace imports.

Database migrations ensure your schema matches the version of Nexus you're running.
Workspace migrations import data from other systems (e.g., Clawdbot).`,
	}

	cmd.AddCommand(buildMigrateUpCmd())
	cmd.AddCommand(buildMigrateDownCmd())
	cmd.AddCommand(buildMigrateStatusCmd())
	cmd.AddCommand(buildMigrateClawdbotWorkspaceCmd())
	cmd.AddCommand(buildMigrateSessionsImportCmd())
	cmd.AddCommand(buildMigrateSessionsExportCmd())

	return cmd
}

func buildMigrateClawdbotWorkspaceCmd() *cobra.Command {
	var (
		targetWorkspace string
		targetConfig    string
		overwrite       bool
		dryRun          bool
	)

	cmd := &cobra.Command{
		Use:   "clawdbot-workspace <source-path>",
		Short: "Import Clawdbot workspace files",
		Long: `Migrate workspace files from a Clawdbot installation to Nexus.

This command copies workspace files (SOUL.md, IDENTITY.md, USER.md, MEMORY.md, AGENTS.md)
from a Clawdbot workspace to a Nexus workspace. It also creates new files that are
specific to Nexus (TOOLS.md, HEARTBEAT.md).

The doctor command can validate the migrated workspace afterwards.`,
		Example: `  # Migrate workspace files
  nexus migrate clawdbot-workspace /path/to/clawdbot/workspace

  # Specify target workspace
  nexus migrate clawdbot-workspace /path/to/clawdbot --target ~/nexus-workspace

  # Preview what would be migrated
  nexus migrate clawdbot-workspace /path/to/clawdbot --dry-run

  # Overwrite existing files
  nexus migrate clawdbot-workspace /path/to/clawdbot --overwrite`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourcePath := args[0]
			return runMigrateClawdbotWorkspace(cmd, sourcePath, targetWorkspace, targetConfig, overwrite, dryRun)
		},
	}

	cmd.Flags().StringVar(&targetWorkspace, "target", "", "Target workspace directory (default: current config workspace.path)")
	cmd.Flags().StringVar(&targetConfig, "target-config", "", "Target config file to update (optional)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview migration without making changes")

	return cmd
}

func buildMigrateSessionsImportCmd() *cobra.Command {
	var (
		configPath     string
		dryRun         bool
		skipDuplicates bool
		defaultAgent   string
		preserveIDs    bool
	)

	cmd := &cobra.Command{
		Use:   "sessions-import <file.jsonl>",
		Short: "Import session history from JSONL",
		Long: `Import conversation history and messages from a JSONL file.

The JSONL format contains one record per line, with types:
- "session": Session/conversation metadata
- "message": Individual messages within sessions

This enables migrating history from Clawdbot or other systems.
Use --dry-run to validate without writing to the database.`,
		Example: `  # Preview import
  nexus migrate sessions-import history.jsonl --dry-run

  # Import with duplicate skipping
  nexus migrate sessions-import history.jsonl --skip-duplicates

  # Import preserving original IDs
  nexus migrate sessions-import history.jsonl --preserve-ids`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return runMigrateSessionsImport(cmd, configPath, args[0], dryRun, skipDuplicates, defaultAgent, preserveIDs)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate without writing")
	cmd.Flags().BoolVar(&skipDuplicates, "skip-duplicates", true, "Skip records that already exist")
	cmd.Flags().StringVar(&defaultAgent, "default-agent", "default", "Default agent ID for sessions without one")
	cmd.Flags().BoolVar(&preserveIDs, "preserve-ids", false, "Keep original IDs instead of generating new ones")

	return cmd
}

func buildMigrateSessionsExportCmd() *cobra.Command {
	var (
		configPath string
		agentID    string
		output     string
	)

	cmd := &cobra.Command{
		Use:   "sessions-export",
		Short: "Export session history to JSONL",
		Long: `Export conversation history and messages to a JSONL file.

The export can be used for backup or migration to another Nexus instance.`,
		Example: `  # Export all sessions
  nexus migrate sessions-export -o backup.jsonl

  # Export sessions for a specific agent
  nexus migrate sessions-export --agent default -o agent-history.jsonl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return runMigrateSessionsExport(cmd, configPath, agentID, output)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVar(&agentID, "agent", "", "Export only sessions for this agent ID")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default: stdout)")

	return cmd
}

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
			return runMigrateUp(cmd, configPath, steps)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().IntVarP(&steps, "steps", "n", 0, "Number of migrations to apply (0 = all)")

	return cmd
}

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
			return runMigrateDown(cmd, configPath, steps)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().IntVarP(&steps, "steps", "n", 1, "Number of migrations to rollback")

	return cmd
}

func buildMigrateStatusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Long: `Display the current state of database migrations.

Shows which migrations have been applied and which are pending.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateStatus(cmd, configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")

	return cmd
}

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

// =============================================================================
// Agent Commands
// =============================================================================

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

func buildAgentsListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured agents",
		Long:  "Display all AI agents defined in the system.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printAgentsList(cmd.OutOrStdout(), configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

func buildAgentsCreateCmd() *cobra.Command {
	var (
		configPath string
		name       string
		provider   string
		model      string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new agent",
		Long: `Create a new AI agent with specified configuration.

The agent definition will be appended to AGENTS.md and loaded by the server.`,
		Example: `  # Create agent with Claude
  nexus agents create --name "coder" --provider anthropic --model claude-sonnet-4-20250514

  # Create agent with GPT-4
  nexus agents create --name "researcher" --provider openai --model gpt-4o`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printAgentCreate(cmd.OutOrStdout(), configPath, name, provider, model)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Agent name (required)")
	cmd.Flags().StringVarP(&provider, "provider", "p", "anthropic", "LLM provider")
	cmd.Flags().StringVarP(&model, "model", "m", "", "Model identifier")
	if err := cmd.MarkFlagRequired("name"); err != nil {
		panic(err)
	}

	return cmd
}

func buildAgentsShowCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "show [agent-id]",
		Short: "Show agent details",
		Long:  "Display detailed configuration for a specific agent.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printAgentShow(cmd.OutOrStdout(), configPath, args[0])
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

// =============================================================================
// Status Command
// =============================================================================

// buildStatusCmd creates the "status" command for system health overview.
func buildStatusCmd() *cobra.Command {
	var (
		configPath string
		serverAddr string
		jsonOutput bool
		token      string
		apiKey     string
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
			return printSystemStatus(cmd.Context(), cmd.OutOrStdout(), jsonOutput, configPath, serverAddr, token, apiKey)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVar(&serverAddr, "server", "", "Nexus HTTP server address (default from config)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().StringVar(&token, "token", "", "JWT bearer token for server auth")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for server auth")

	return cmd
}

// =============================================================================
// Doctor Command
// =============================================================================

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
			return runDoctor(cmd, configPath, repair, probe, audit)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(),
		"Path to YAML configuration file")
	cmd.Flags().BoolVar(&repair, "repair", false, "Apply migrations and common repairs")
	cmd.Flags().BoolVar(&probe, "probe", false, "Run channel health probes")
	cmd.Flags().BoolVar(&audit, "audit", false, "Audit service files and port availability")

	return cmd
}

// =============================================================================
// Prompt Command
// =============================================================================

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
			return runPrompt(cmd, configPath, sessionID, channel, message, heartbeat)
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
			return runTraceValidate(cmd, args[0])
		},
	}
	return cmd
}

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
			return runTraceStats(cmd, args[0], jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output statistics as JSON")
	return cmd
}

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
			return runTraceReplay(cmd, args[0], speed, fromSeq, toSeq, filter, showTime, view)
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

// =============================================================================
// Edge Commands
// =============================================================================

// buildEdgeCmd creates the "edge" command group for edge daemon management.
func buildEdgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edge daemons",
		Long: `Manage edge daemons that provide local capabilities.

Edge daemons connect from user machines to provide:
- Device access (camera, screen, location)
- Browser relay (attached Chrome sessions)
- Edge-only channels (iMessage, local Signal bridges)
- Local filesystem and command execution

Use "nexus edge status" to see connected edges.`,
	}
	cmd.AddCommand(
		buildEdgeStatusCmd(),
		buildEdgeListCmd(),
		buildEdgeToolsCmd(),
		buildEdgeApproveCmd(),
		buildEdgeRevokeCmd(),
	)
	return cmd
}

func buildEdgeStatusCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "status [edge-id]",
		Short: "Show edge daemon status",
		Long: `Show the status of a connected edge daemon.

If no edge-id is provided, shows a summary of all connected edges.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			edgeID := ""
			if len(args) > 0 {
				edgeID = args[0]
			}
			return runEdgeStatus(cmd, configPath, edgeID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildEdgeListCmd() *cobra.Command {
	var configPath string
	var showTools bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List connected edge daemons",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeList(cmd, configPath, showTools)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVar(&showTools, "tools", false, "Show tools for each edge")
	return cmd
}

func buildEdgeToolsCmd() *cobra.Command {
	var configPath string
	var edgeID string
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List tools from connected edges",
		Long: `List all tools available from connected edge daemons.

Use --edge to filter by a specific edge.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeTools(cmd, configPath, edgeID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&edgeID, "edge", "", "Filter by edge ID")
	return cmd
}

func buildEdgeApproveCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "approve [edge-id]",
		Short: "Approve a pending edge (TOFU)",
		Long: `Approve a pending edge connection request.

When using Trust-On-First-Use (TOFU) authentication, new edges
are held pending until manually approved.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeApprove(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildEdgeRevokeCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "revoke [edge-id]",
		Short: "Revoke an approved edge",
		Long:  `Revoke an approved edge, disconnecting it and preventing reconnection.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdgeRevoke(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

// =============================================================================
// Events Commands
// =============================================================================

// buildEventsCmd creates the "events" command for viewing event timelines.
func buildEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "View and debug event timelines",
		Long: `View event timelines for debugging agent runs, tool executions, and edge operations.

Events are captured with correlation IDs (run_id, session_id, tool_call_id, edge_id)
allowing you to trace exactly what happened during a run.`,
	}
	cmd.AddCommand(
		buildEventsShowCmd(),
		buildEventsListCmd(),
	)
	return cmd
}

func buildEventsShowCmd() *cobra.Command {
	var configPath string
	var format string
	var traceDir string
	cmd := &cobra.Command{
		Use:   "show <run-id>",
		Short: "Show event timeline for a run",
		Long: `Display the event timeline for a specific run.

The timeline shows all events in chronological order including:
- Run start/end
- Tool executions and their results
- Edge daemon interactions
- LLM requests/responses
- Errors and their context`,
		Example: `  # Show events for a specific run
  nexus events show run_123456

  # Show events in JSON format
  nexus events show run_123456 --format json

  # Show events from trace files
  nexus events show run_123456 --trace-dir ./traces`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEventsShow(cmd, configPath, args[0], format, traceDir)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format (text, json)")
	cmd.Flags().StringVar(&traceDir, "trace-dir", "", "Directory containing trace files (defaults to NEXUS_TRACE_DIR)")
	return cmd
}

func buildEventsListCmd() *cobra.Command {
	var configPath string
	var limit int
	var eventType string
	var sessionID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent events",
		Long:  `List recent events, optionally filtered by type or session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEventsList(cmd, configPath, limit, eventType, sessionID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of events to show")
	cmd.Flags().StringVarP(&eventType, "type", "t", "", "Filter by event type (e.g., tool.start, run.error)")
	cmd.Flags().StringVarP(&sessionID, "session", "s", "", "Filter by session ID")
	return cmd
}

// ============================================================================
// Artifacts Commands
// ============================================================================

func buildArtifactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Manage artifacts produced by tool execution",
		Long:  `List, view, and manage artifacts (screenshots, recordings, files) produced by edge tools.`,
	}
	cmd.AddCommand(
		buildArtifactsListCmd(),
		buildArtifactsGetCmd(),
		buildArtifactsDeleteCmd(),
	)
	return cmd
}

func buildArtifactsListCmd() *cobra.Command {
	var configPath string
	var limit int
	var artifactType string
	var sessionID string
	var edgeID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts",
		Long:  `List artifacts, optionally filtered by type, session, or edge.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactsList(cmd, configPath, limit, artifactType, sessionID, edgeID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of artifacts to show")
	cmd.Flags().StringVarP(&artifactType, "type", "t", "", "Filter by artifact type (screenshot, recording, file)")
	cmd.Flags().StringVarP(&sessionID, "session", "s", "", "Filter by session ID")
	cmd.Flags().StringVarP(&edgeID, "edge", "e", "", "Filter by edge ID")
	return cmd
}

func buildArtifactsGetCmd() *cobra.Command {
	var configPath string
	var outputPath string
	cmd := &cobra.Command{
		Use:   "get <artifact-id>",
		Short: "Get an artifact",
		Long:  `Download an artifact by its ID.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactsGet(cmd, configPath, args[0], outputPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (default: stdout for text, or ./<filename>)")
	return cmd
}

func buildArtifactsDeleteCmd() *cobra.Command {
	var configPath string
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <artifact-id>",
		Short: "Delete an artifact",
		Long:  `Delete an artifact by its ID.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactsDelete(cmd, configPath, args[0], force)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}
