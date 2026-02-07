package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/spf13/cobra"
)

// =============================================================================
// Events Command Handlers
// =============================================================================

// runEventsShow shows the event timeline for a specific run.
func runEventsShow(cmd *cobra.Command, configPath string, runID string, format string, traceDir string) error {
	if traceDir == "" {
		traceDir = os.Getenv("NEXUS_TRACE_DIR")
	}
	if traceDir != "" {
		timeline, err := loadTraceTimeline(traceDir, runID)
		if err == nil {
			return renderTraceTimeline(cmd.OutOrStdout(), timeline, format)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "No trace found for run: %s\n", runID)
		fmt.Fprintf(cmd.OutOrStdout(), "Searched: %s\n", agent.TraceFilePath(traceDir, runID))
		return nil
	}

	// Fallback to in-memory event store (previous default)
	store := observability.NewMemoryEventStore(10000)

	events, err := store.GetByRunID(runID)
	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}

	if len(events) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No events found for run: %s\n", runID)
		fmt.Fprintln(cmd.OutOrStdout(), "\nNote: Events are currently stored in memory and are lost when the server restarts.")
		fmt.Fprintln(cmd.OutOrStdout(), "To capture events, set NEXUS_TRACE_DIR and re-run.")
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

type traceTimeline struct {
	Header *agent.TraceHeader  `json:"header"`
	Stats  *models.RunStats    `json:"stats,omitempty"`
	Events []models.AgentEvent `json:"events"`
}

func loadTraceTimeline(traceDir, runID string) (*traceTimeline, error) {
	tracePath := agent.TraceFilePath(traceDir, runID)
	f, err := os.Open(tracePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	defer f.Close()

	reader, err := agent.NewTraceReader(f)
	if err != nil {
		return nil, fmt.Errorf("read trace header: %w", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read trace events: %w", err)
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Time.Before(events[j].Time)
	})

	statsCollector := agent.NewStatsCollector(reader.Header().RunID)
	for _, event := range events {
		statsCollector.OnEvent(context.Background(), event)
	}

	return &traceTimeline{
		Header: reader.Header(),
		Stats:  statsCollector.Stats(),
		Events: events,
	}, nil
}

func renderTraceTimeline(out io.Writer, timeline *traceTimeline, format string) error {
	if timeline == nil {
		return nil
	}

	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(timeline)
	default:
		return writeTraceTimelineText(out, timeline)
	}
}

func writeTraceTimelineText(out io.Writer, timeline *traceTimeline) error {
	header := timeline.Header
	if header != nil {
		fmt.Fprintf(out, "Run: %s\n", header.RunID)
		fmt.Fprintf(out, "Started: %s\n", header.StartedAt.Format(time.RFC3339))
		if header.AppVersion != "" {
			fmt.Fprintf(out, "App: %s\n", header.AppVersion)
		}
		if header.Environment != "" {
			fmt.Fprintf(out, "Env: %s\n", header.Environment)
		}
	}
	if timeline.Stats != nil {
		stats := timeline.Stats
		fmt.Fprintf(out, "Duration: %s\n", stats.WallTime)
		fmt.Fprintf(out, "Turns: %d, Iters: %d\n", stats.Turns, stats.Iters)
		fmt.Fprintf(out, "Tool calls: %d (timeouts: %d)\n", stats.ToolCalls, stats.ToolTimeouts)
		fmt.Fprintf(out, "Errors: %d\n", stats.Errors)
	}

	fmt.Fprintln(out, "\nEvents:")
	for _, event := range timeline.Events {
		timestamp := event.Time.Format("15:04:05.000")
		detail := formatAgentEventDetail(event)
		if detail != "" {
			fmt.Fprintf(out, "[%s] %s %s\n", timestamp, event.Type, detail)
		} else {
			fmt.Fprintf(out, "[%s] %s\n", timestamp, event.Type)
		}
	}
	return nil
}

func formatAgentEventDetail(event models.AgentEvent) string {
	switch event.Type {
	case models.AgentEventRunError:
		if event.Error != nil {
			return event.Error.Message
		}
	case models.AgentEventToolStarted:
		if event.Tool != nil {
			return fmt.Sprintf("%s (%s)", event.Tool.Name, event.Tool.CallID)
		}
	case models.AgentEventToolFinished, models.AgentEventToolTimedOut:
		if event.Tool != nil {
			return fmt.Sprintf("%s (%s) success=%t", event.Tool.Name, event.Tool.CallID, event.Tool.Success)
		}
	case models.AgentEventModelDelta:
		if event.Stream != nil && event.Stream.Delta != "" {
			return fmt.Sprintf("delta_len=%d", len(event.Stream.Delta))
		}
	case models.AgentEventModelCompleted:
		if event.Stream != nil {
			return fmt.Sprintf("tokens in=%d out=%d", event.Stream.InputTokens, event.Stream.OutputTokens)
		}
	case models.AgentEventContextPacked:
		if event.Context != nil {
			return fmt.Sprintf("used %d/%d chars, dropped %d", event.Context.UsedChars, event.Context.BudgetChars, event.Context.Dropped)
		}
	}

	if event.Text != nil {
		return event.Text.Text
	}
	return ""
}
