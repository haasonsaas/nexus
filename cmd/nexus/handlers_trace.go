package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/spf13/cobra"
)

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
