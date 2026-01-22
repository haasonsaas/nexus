package agent

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestTracePlugin_WritesHeader(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "test-run-123")

	// Emit an event to trigger header write
	plugin.OnEvent(context.Background(), models.AgentEvent{
		Type: models.AgentEventRunStarted,
	})

	// Read back
	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	header := reader.Header()
	if header.Version != 1 {
		t.Errorf("Version = %d, want 1", header.Version)
	}
	if header.RunID != "test-run-123" {
		t.Errorf("RunID = %q, want %q", header.RunID, "test-run-123")
	}
}

func TestTracePlugin_WritesEvents(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "test-run")

	// Emit events
	events := []models.AgentEvent{
		{Type: models.AgentEventRunStarted, Sequence: 1},
		{Type: models.AgentEventIterStarted, Sequence: 2},
		{Type: models.AgentEventModelDelta, Sequence: 3, Stream: &models.StreamEventPayload{Delta: "hello"}},
		{Type: models.AgentEventIterFinished, Sequence: 4},
		{Type: models.AgentEventRunFinished, Sequence: 5},
	}

	for _, e := range events {
		plugin.OnEvent(context.Background(), e)
	}

	// Read back
	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	readEvents, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read events: %v", err)
	}

	if len(readEvents) != len(events) {
		t.Fatalf("got %d events, want %d", len(readEvents), len(events))
	}

	for i, re := range readEvents {
		if re.Type != events[i].Type {
			t.Errorf("event[%d].Type = %s, want %s", i, re.Type, events[i].Type)
		}
		if re.Sequence != events[i].Sequence {
			t.Errorf("event[%d].Sequence = %d, want %d", i, re.Sequence, events[i].Sequence)
		}
	}
}

func TestTracePlugin_WithOptions(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "test-run",
		WithAppVersion("1.2.3"),
		WithEnvironment("test"),
	)

	plugin.OnEvent(context.Background(), models.AgentEvent{Type: models.AgentEventRunStarted})

	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	header := reader.Header()
	if header.AppVersion != "1.2.3" {
		t.Errorf("AppVersion = %q, want %q", header.AppVersion, "1.2.3")
	}
	if header.Environment != "test" {
		t.Errorf("Environment = %q, want %q", header.Environment, "test")
	}
}

func TestTracePlugin_Redaction(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "test-run",
		WithRedactor(DefaultRedactor),
	)

	// Emit event with sensitive tool data
	plugin.OnEvent(context.Background(), models.AgentEvent{
		Type: models.AgentEventToolFinished,
		Tool: &models.ToolEventPayload{
			CallID:     "tc-1",
			Name:       "secret_tool",
			ArgsJSON:   []byte(`{"secret":"password123"}`),
			ResultJSON: []byte(`{"data":"sensitive"}`),
		},
	})

	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read events: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	// Tool data should be redacted
	if events[0].Tool == nil {
		t.Fatal("expected Tool payload")
	}
	if string(events[0].Tool.ArgsJSON) != `"[REDACTED]"` {
		t.Errorf("ArgsJSON = %s, want [REDACTED]", events[0].Tool.ArgsJSON)
	}
	if string(events[0].Tool.ResultJSON) != `"[REDACTED]"` {
		t.Errorf("ResultJSON = %s, want [REDACTED]", events[0].Tool.ResultJSON)
	}
}

func TestTracePlugin_FileIO(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "trace.jsonl")

	// Write trace
	plugin, err := NewTracePluginFile(path, "file-test")
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	plugin.OnEvent(context.Background(), models.AgentEvent{Type: models.AgentEventRunStarted, Sequence: 1})
	plugin.OnEvent(context.Background(), models.AgentEvent{Type: models.AgentEventRunFinished, Sequence: 2})

	if err := plugin.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// Read trace
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open trace: %v", err)
	}
	defer f.Close()

	reader, err := NewTraceReader(f)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	if reader.Header().RunID != "file-test" {
		t.Errorf("RunID = %q, want %q", reader.Header().RunID, "file-test")
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read events: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
}

func TestTraceReader_InvalidVersion(t *testing.T) {
	buf := bytes.NewBufferString(`{"version":99,"run_id":"test"}` + "\n")
	_, err := NewTraceReader(buf)
	if err == nil {
		t.Error("expected error for unsupported version")
	}
}

func TestTraceReader_InvalidHeader(t *testing.T) {
	buf := bytes.NewBufferString("not json\n")
	_, err := NewTraceReader(buf)
	if err == nil {
		t.Error("expected error for invalid header")
	}
}

func TestTraceReader_ReadEvent_EOF(t *testing.T) {
	buf := bytes.NewBufferString(`{"version":1,"run_id":"test"}` + "\n")
	reader, err := NewTraceReader(buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	_, err = reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestTracePlugin_ConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "concurrent-test")

	// Write events concurrently
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(seq uint64) {
			plugin.OnEvent(context.Background(), models.AgentEvent{
				Type:     models.AgentEventModelDelta,
				Sequence: seq,
			})
			done <- struct{}{}
		}(uint64(i))
	}

	// Wait for all writes
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and should have valid JSONL
	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read events: %v", err)
	}

	if len(events) != 10 {
		t.Errorf("got %d events, want 10", len(events))
	}
}

// =============================================================================
// Replay Harness Tests
// =============================================================================

func TestTraceReplayer_Basic(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "replay-test")

	// Write a complete trace
	events := []models.AgentEvent{
		{Type: models.AgentEventRunStarted, Sequence: 1},
		{Type: models.AgentEventIterStarted, Sequence: 2},
		{Type: models.AgentEventModelDelta, Sequence: 3, Stream: &models.StreamEventPayload{Delta: "hello"}},
		{Type: models.AgentEventIterFinished, Sequence: 4},
		{Type: models.AgentEventRunFinished, Sequence: 5},
	}
	for _, e := range events {
		plugin.OnEvent(context.Background(), e)
	}

	// Replay
	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	var received []models.AgentEvent
	sink := NewCallbackSink(func(ctx context.Context, e models.AgentEvent) {
		received = append(received, e)
	})

	replayer := NewTraceReplayer(reader, sink)
	stats, err := replayer.Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}

	if stats.EventCount != len(events) {
		t.Errorf("EventCount = %d, want %d", stats.EventCount, len(events))
	}
	if len(received) != len(events) {
		t.Errorf("received %d events, want %d", len(received), len(events))
	}
	if !stats.Valid() {
		t.Errorf("unexpected validation errors: %v", stats.Errors)
	}
}

func TestTraceReplayer_SequenceRange(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "range-test")

	for i := uint64(1); i <= 10; i++ {
		plugin.OnEvent(context.Background(), models.AgentEvent{
			Type:     models.AgentEventModelDelta,
			Sequence: i,
		})
	}

	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	var received []models.AgentEvent
	sink := NewCallbackSink(func(ctx context.Context, e models.AgentEvent) {
		received = append(received, e)
	})

	replayer := NewTraceReplayer(reader, sink, WithSequenceRange(3, 7))
	_, err = replayer.Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}

	if len(received) != 5 { // sequences 3, 4, 5, 6, 7
		t.Errorf("received %d events, want 5", len(received))
	}
}

func TestTraceReplayer_Validation(t *testing.T) {
	tests := []struct {
		name        string
		events      []models.AgentEvent
		wantValid   bool
		wantErrors  int
	}{
		{
			name: "valid trace",
			events: []models.AgentEvent{
				{Type: models.AgentEventRunStarted, Sequence: 1},
				{Type: models.AgentEventRunFinished, Sequence: 2},
			},
			wantValid:  true,
			wantErrors: 0,
		},
		{
			name: "missing run.started",
			events: []models.AgentEvent{
				{Type: models.AgentEventModelDelta, Sequence: 1},
				{Type: models.AgentEventRunFinished, Sequence: 2},
			},
			wantValid:  false,
			wantErrors: 1,
		},
		{
			name: "missing run.finished",
			events: []models.AgentEvent{
				{Type: models.AgentEventRunStarted, Sequence: 1},
				{Type: models.AgentEventModelDelta, Sequence: 2},
			},
			wantValid:  false,
			wantErrors: 1,
		},
		{
			name: "non-monotonic sequences",
			events: []models.AgentEvent{
				{Type: models.AgentEventRunStarted, Sequence: 1},
				{Type: models.AgentEventModelDelta, Sequence: 3},
				{Type: models.AgentEventModelDelta, Sequence: 2}, // out of order
				{Type: models.AgentEventRunFinished, Sequence: 4},
			},
			wantValid:  false,
			wantErrors: 1,
		},
		{
			name: "ends with error (valid)",
			events: []models.AgentEvent{
				{Type: models.AgentEventRunStarted, Sequence: 1},
				{Type: models.AgentEventRunError, Sequence: 2},
			},
			wantValid:  true,
			wantErrors: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			plugin := NewTracePlugin(&buf, "validation-test")
			for _, e := range tc.events {
				plugin.OnEvent(context.Background(), e)
			}

			reader, err := NewTraceReader(&buf)
			if err != nil {
				t.Fatalf("failed to create reader: %v", err)
			}

			replayer := NewTraceReplayer(reader, NopSink{})
			stats, err := replayer.Replay(context.Background())
			if err != nil {
				t.Fatalf("Replay() error = %v", err)
			}

			if stats.Valid() != tc.wantValid {
				t.Errorf("Valid() = %v, want %v; errors: %v", stats.Valid(), tc.wantValid, stats.Errors)
			}
			if len(stats.Errors) != tc.wantErrors {
				t.Errorf("got %d errors, want %d: %v", len(stats.Errors), tc.wantErrors, stats.Errors)
			}
		})
	}
}

func TestReplayToStats(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "stats-test")

	// Write a trace with known stats
	events := []models.AgentEvent{
		{Type: models.AgentEventRunStarted, Sequence: 1, Time: time.Now()},
		{Type: models.AgentEventIterStarted, Sequence: 2, Time: time.Now()},
		{Type: models.AgentEventModelCompleted, Sequence: 3, Time: time.Now(),
			Stream: &models.StreamEventPayload{InputTokens: 100, OutputTokens: 50}},
		{Type: models.AgentEventToolStarted, Sequence: 4, Time: time.Now(),
			Tool: &models.ToolEventPayload{CallID: "tc-1"}},
		{Type: models.AgentEventToolFinished, Sequence: 5, Time: time.Now(),
			Tool: &models.ToolEventPayload{CallID: "tc-1", Success: true}},
		{Type: models.AgentEventIterFinished, Sequence: 6, Time: time.Now()},
		{Type: models.AgentEventRunFinished, Sequence: 7, Time: time.Now()},
	}
	for _, e := range events {
		plugin.OnEvent(context.Background(), e)
	}

	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	stats, err := ReplayToStats(reader)
	if err != nil {
		t.Fatalf("ReplayToStats() error = %v", err)
	}

	if stats.Iters != 1 {
		t.Errorf("Iters = %d, want 1", stats.Iters)
	}
	if stats.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", stats.ToolCalls)
	}
	if stats.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", stats.InputTokens)
	}
	if stats.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", stats.OutputTokens)
	}
}

func TestTraceRoundTrip_EventTypes(t *testing.T) {
	var buf bytes.Buffer
	plugin := NewTracePlugin(&buf, "roundtrip-test")

	now := time.Now().Truncate(time.Millisecond) // JSON truncates to milliseconds

	// Create events of each type with full payloads
	events := []models.AgentEvent{
		{
			Type: models.AgentEventRunStarted, Version: 1, Sequence: 1, RunID: "test", Time: now,
		},
		{
			Type: models.AgentEventIterStarted, Version: 1, Sequence: 2, RunID: "test", Time: now, IterIndex: 0,
		},
		{
			Type: models.AgentEventModelDelta, Version: 1, Sequence: 3, RunID: "test", Time: now,
			Stream: &models.StreamEventPayload{Delta: "hello"},
		},
		{
			Type: models.AgentEventToolStarted, Version: 1, Sequence: 4, RunID: "test", Time: now,
			Tool: &models.ToolEventPayload{CallID: "tc-1", Name: "search", ArgsJSON: []byte(`{"q":"test"}`)},
		},
		{
			Type: models.AgentEventToolFinished, Version: 1, Sequence: 5, RunID: "test", Time: now,
			Tool: &models.ToolEventPayload{CallID: "tc-1", Name: "search", Success: true, ResultJSON: []byte(`"result"`)},
		},
		{
			Type: models.AgentEventIterFinished, Version: 1, Sequence: 6, RunID: "test", Time: now, IterIndex: 0,
		},
		{
			Type: models.AgentEventRunFinished, Version: 1, Sequence: 7, RunID: "test", Time: now,
			Stats: &models.StatsEventPayload{Run: &models.RunStats{Iters: 1, ToolCalls: 1}},
		},
	}

	for _, e := range events {
		plugin.OnEvent(context.Background(), e)
	}

	// Read back
	reader, err := NewTraceReader(&buf)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}

	readEvents, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read events: %v", err)
	}

	if len(readEvents) != len(events) {
		t.Fatalf("got %d events, want %d", len(readEvents), len(events))
	}

	// Verify each event round-trips correctly
	for i, re := range readEvents {
		orig := events[i]

		if re.Type != orig.Type {
			t.Errorf("event[%d].Type = %s, want %s", i, re.Type, orig.Type)
		}
		if re.Sequence != orig.Sequence {
			t.Errorf("event[%d].Sequence = %d, want %d", i, re.Sequence, orig.Sequence)
		}
		if re.RunID != orig.RunID {
			t.Errorf("event[%d].RunID = %q, want %q", i, re.RunID, orig.RunID)
		}

		// Check payloads
		if orig.Stream != nil {
			if re.Stream == nil || re.Stream.Delta != orig.Stream.Delta {
				t.Errorf("event[%d].Stream.Delta mismatch", i)
			}
		}
		if orig.Tool != nil {
			if re.Tool == nil || re.Tool.CallID != orig.Tool.CallID {
				t.Errorf("event[%d].Tool.CallID mismatch", i)
			}
		}
		if orig.Stats != nil && orig.Stats.Run != nil {
			if re.Stats == nil || re.Stats.Run == nil {
				t.Errorf("event[%d].Stats mismatch", i)
			}
		}
	}
}
