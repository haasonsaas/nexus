package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/gateway"
	"github.com/haasonsaas/nexus/internal/plugins"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/spf13/cobra"
)

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
	migrations, err := doctor.ApplyConfigMigrations(raw)
	if err != nil {
		return fmt.Errorf("config migrations failed: %w", err)
	}
	if len(migrations.Applied) > 0 {
		if repair {
			backupPath, err := doctor.BackupConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to backup config before migration: %w", err)
			}
			if err := doctor.WriteRawConfig(configPath, raw); err != nil {
				return fmt.Errorf("failed to write migrated config: %w", err)
			}
			fmt.Fprintln(out, "Applied config migrations:")
			for _, note := range migrations.Applied {
				fmt.Fprintf(out, "  - %s\n", note)
			}
			fmt.Fprintf(out, "Backup created: %s\n", backupPath)
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
			return fmt.Errorf("config validation failed (migrations available). run `nexus doctor --repair`: %w", err)
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

		// Check reminder status
		if server.TaskStore() != nil {
			reminderStatus := doctor.ProbeReminderStatus(cmd.Context(), server.TaskStore())
			fmt.Fprintf(out, "Reminders: %s\n", doctor.FormatReminderStatus(reminderStatus))
			if len(reminderStatus.Errors) > 0 {
				for _, errMsg := range reminderStatus.Errors {
					fmt.Fprintf(out, "  - error: %s\n", errMsg)
				}
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
