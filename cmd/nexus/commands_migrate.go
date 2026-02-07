package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

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
