package main

import "github.com/spf13/cobra"

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
