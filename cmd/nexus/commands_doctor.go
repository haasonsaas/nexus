package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

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
