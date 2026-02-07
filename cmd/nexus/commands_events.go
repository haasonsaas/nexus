package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

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
