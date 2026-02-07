package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/gateway"
	"github.com/haasonsaas/nexus/internal/plugins"
	"github.com/haasonsaas/nexus/internal/service"
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
		migrations, err := doctor.ApplyConfigMigrations(raw)
		if err != nil {
			return fmt.Errorf("config migrations failed: %w", err)
		}
		if len(migrations.Applied) > 0 {
			backupPath, err := doctor.BackupConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to backup config before migration: %w", err)
			}
			if err := doctor.WriteRawConfig(configPath, raw); err != nil {
				return fmt.Errorf("failed to write migrated config: %w", err)
			}
			slog.Info("config migrations applied",
				"from_version", migrations.FromVersion,
				"to_version", migrations.ToVersion,
				"count", len(migrations.Applied),
				"backup", backupPath)
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

	server, err := gateway.NewManagedServer(gateway.ManagedServerConfig{
		Config:     cfg,
		Logger:     slog.Default(),
		ConfigPath: configPath,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize gateway: %w", err)
	}

	// Create a context that cancels on shutdown signals.
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	slog.Info("Nexus gateway started",
		"grpc_addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.GRPCPort),
		"http_addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.HTTPPort),
	)

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	slog.Info("shutdown signal received, initiating graceful shutdown")

	// Create a timeout context for graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Stop(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}

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
