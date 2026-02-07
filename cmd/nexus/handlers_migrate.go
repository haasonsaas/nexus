package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/spf13/cobra"
)

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

// runMigrateClawdbotWorkspace handles the migrate clawdbot-workspace command.
func runMigrateClawdbotWorkspace(cmd *cobra.Command, sourcePath, targetWorkspace, targetConfig string, overwrite, dryRun bool) error {
	out := cmd.OutOrStdout()

	// Validate source workspace
	valid, missing := doctor.ValidateClawdbotWorkspace(sourcePath)
	if !valid {
		return fmt.Errorf("source path does not appear to be a Clawdbot workspace (missing SOUL.md and IDENTITY.md)")
	}

	// Determine target workspace
	if targetWorkspace == "" {
		// Try to get from current config
		configPath := resolveConfigPath("")
		if cfg, err := config.Load(configPath); err == nil && cfg.Workspace.Path != "" {
			targetWorkspace = cfg.Workspace.Path
		} else {
			// Default to ./workspace
			targetWorkspace = "./workspace"
		}
	}

	// Expand paths
	var err error
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}
	targetWorkspace, err = filepath.Abs(targetWorkspace)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}

	// Show what we're doing
	fmt.Fprintln(out, "Clawdbot Workspace Migration")
	fmt.Fprintln(out, "============================")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Source:  %s\n", sourcePath)
	fmt.Fprintf(out, "Target:  %s\n", targetWorkspace)
	fmt.Fprintln(out)

	if len(missing) > 0 {
		fmt.Fprintln(out, "Warning: Some standard workspace files not found in source:")
		for _, f := range missing {
			fmt.Fprintf(out, "  - %s\n", f)
		}
		fmt.Fprintln(out)
	}

	if dryRun {
		fmt.Fprintln(out, "[DRY RUN] Would migrate the following:")
		for _, f := range doctor.ClawdbotWorkspaceFiles {
			srcFile := filepath.Join(sourcePath, f)
			if _, err := os.Stat(srcFile); err == nil {
				dstFile := filepath.Join(targetWorkspace, f)
				if _, err := os.Stat(dstFile); err == nil && !overwrite {
					fmt.Fprintf(out, "  - %s (skip - exists)\n", f)
				} else {
					fmt.Fprintf(out, "  - %s (copy)\n", f)
				}
			}
		}
		fmt.Fprintln(out, "  + TOOLS.md (create if missing)")
		fmt.Fprintln(out, "  + HEARTBEAT.md (create if missing)")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Run without --dry-run to perform the migration.")
		return nil
	}

	// Perform migration
	result, err := doctor.MigrateClawdbotWorkspace(sourcePath, targetWorkspace, overwrite)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Show results
	fmt.Fprint(out, doctor.FormatMigrationResult(result))

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  1. Review migrated files in", targetWorkspace)
	fmt.Fprintln(out, "  2. Run `nexus doctor --repair` to validate")
	fmt.Fprintln(out, "  3. Update nexus config: workspace.path =", targetWorkspace)

	return nil
}

// runMigrateSessionsImport handles the migrate sessions-import command.
func runMigrateSessionsImport(cmd *cobra.Command, configPath, inputFile string, dryRun, skipDuplicates bool, defaultAgent string, preserveIDs bool) error {
	out := cmd.OutOrStdout()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Create session store
	store, err := sessions.NewCockroachStoreFromDSN(cfg.Database.URL, nil)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}
	defer store.Close()

	importer := sessions.NewImporter(store)

	opts := sessions.ImportOptions{
		DryRun:         dryRun,
		SkipDuplicates: skipDuplicates,
		DefaultAgentID: defaultAgent,
		PreserveIDs:    preserveIDs,
	}

	fmt.Fprintln(out, "Session History Import")
	fmt.Fprintln(out, "======================")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Input file: %s\n", inputFile)
	if dryRun {
		fmt.Fprintln(out, "Mode: DRY RUN (no changes will be made)")
	}
	fmt.Fprintln(out)

	result, err := importer.ImportFromFile(cmd.Context(), inputFile, opts)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	fmt.Fprint(out, sessions.FormatImportResult(result))

	if len(result.Errors) > 0 && !dryRun {
		return fmt.Errorf("import completed with %d errors", len(result.Errors))
	}

	return nil
}

// runMigrateSessionsExport handles the migrate sessions-export command.
func runMigrateSessionsExport(cmd *cobra.Command, configPath, agentID, outputFile string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Create session store
	store, err := sessions.NewCockroachStoreFromDSN(cfg.Database.URL, nil)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}
	defer store.Close()

	var w io.Writer = cmd.OutOrStdout()
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	if err := sessions.ExportToJSONL(cmd.Context(), store, w, agentID); err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if outputFile != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Exported to %s\n", outputFile)
	}

	return nil
}
