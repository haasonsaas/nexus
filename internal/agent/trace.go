package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// TracePlugin writes AgentEvents to a JSONL file for debugging and replay.
// Each event is written as a single JSON line, flushed immediately for crash safety.
type TracePlugin struct {
	mu       sync.Mutex
	writer   io.Writer
	file     *os.File // non-nil if we opened the file ourselves
	redactor Redactor
	header   *TraceHeader
	started  bool
}

// TraceHeader contains metadata written as the first line of a trace file for versioning and context.
type TraceHeader struct {
	Version     int       `json:"version"`     // Schema version (1)
	RunID       string    `json:"run_id"`      // Unique run identifier
	StartedAt   time.Time `json:"started_at"`  // When the trace started
	AppVersion  string    `json:"app_version"` // Application version (optional)
	Environment string    `json:"environment"` // Environment name (optional)
}

// Redactor is an optional function to redact sensitive data from events before writing to trace.
// It receives a pointer to the event and can modify it in place.
type Redactor func(e *models.AgentEvent)

// TraceOption configures a TracePlugin using the functional options pattern.
type TraceOption func(*TracePlugin)

// WithRedactor sets a custom redactor function for removing sensitive data from events.
func WithRedactor(r Redactor) TraceOption {
	return func(p *TracePlugin) {
		p.redactor = r
	}
}

// WithAppVersion sets the application version in the trace header for debugging.
func WithAppVersion(version string) TraceOption {
	return func(p *TracePlugin) {
		if p.header != nil {
			p.header.AppVersion = version
		}
	}
}

// WithEnvironment sets the environment name (e.g., production, staging) in the trace header.
func WithEnvironment(env string) TraceOption {
	return func(p *TracePlugin) {
		if p.header != nil {
			p.header.Environment = env
		}
	}
}

// NewTracePlugin creates a new trace plugin that writes JSONL events to the given writer.
func NewTracePlugin(w io.Writer, runID string, opts ...TraceOption) *TracePlugin {
	p := &TracePlugin{
		writer: w,
		header: &TraceHeader{
			Version:   1,
			RunID:     runID,
			StartedAt: time.Now(),
		},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// NewTracePluginFile creates a new trace plugin that writes to the given file path.
// The file is created or truncated. The caller must call Close() when done.
func NewTracePluginFile(path string, runID string, opts ...TraceOption) (*TracePlugin, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace file: %w", err)
	}

	p := NewTracePlugin(f, runID, opts...)
	p.file = f

	return p, nil
}

// OnEvent implements the Plugin interface by writing the event as JSONL.
func (p *TracePlugin) OnEvent(ctx context.Context, e models.AgentEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Write header on first event
	if !p.started {
		p.started = true
		p.writeHeader()
	}

	// Make a copy for redaction
	eventCopy := e
	if p.redactor != nil {
		p.redactor(&eventCopy)
	}

	// Serialize and write
	data, err := json.Marshal(eventCopy)
	if err != nil {
		// Best effort - don't block on trace errors
		return
	}

	// Write as a single line, flush immediately
	if _, err := p.writer.Write(data); err != nil {
		return
	}
	if _, err := p.writer.Write([]byte("\n")); err != nil {
		return
	}

	// Sync if we have a file handle
	if p.file != nil {
		if err := p.file.Sync(); err != nil {
			return
		}
	}
}

// writeHeader writes the trace header as the first line.
func (p *TracePlugin) writeHeader() {
	data, err := json.Marshal(p.header)
	if err != nil {
		return
	}

	if _, err := p.writer.Write(data); err != nil {
		return
	}
	if _, err := p.writer.Write([]byte("\n")); err != nil {
		return
	}

	if p.file != nil {
		if err := p.file.Sync(); err != nil {
			return
		}
	}
}

// Close closes the underlying trace file if one was opened by NewTracePluginFile.
func (p *TracePlugin) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.file != nil {
		return p.file.Close()
	}
	return nil
}

// TraceReader reads AgentEvents from a JSONL trace file for replay or analysis.
type TraceReader struct {
	decoder *json.Decoder
	header  *TraceHeader
}

// NewTraceReader creates a new trace reader from the given reader and validates the header.
// Returns an error if the header is missing or has an unsupported version.
func NewTraceReader(r io.Reader) (*TraceReader, error) {
	decoder := json.NewDecoder(r)

	// Read header
	var header TraceHeader
	if err := decoder.Decode(&header); err != nil {
		return nil, fmt.Errorf("failed to read trace header: %w", err)
	}

	if header.Version != 1 {
		return nil, fmt.Errorf("unsupported trace version: %d", header.Version)
	}

	return &TraceReader{
		decoder: decoder,
		header:  &header,
	}, nil
}

// Header returns the trace header containing run metadata.
func (r *TraceReader) Header() *TraceHeader {
	return r.header
}

// ReadEvent reads the next event from the trace file.
// Returns io.EOF when all events have been read.
func (r *TraceReader) ReadEvent() (*models.AgentEvent, error) {
	var event models.AgentEvent
	if err := r.decoder.Decode(&event); err != nil {
		return nil, err
	}
	return &event, nil
}

// ReadAll reads all remaining events from the trace into a slice.
func (r *TraceReader) ReadAll() ([]models.AgentEvent, error) {
	var events []models.AgentEvent
	for {
		event, err := r.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return events, err
		}
		events = append(events, *event)
	}
	return events, nil
}

// DefaultRedactor provides basic redaction for common sensitive fields in events.
// It redacts tool inputs and outputs by replacing them with "[REDACTED]" placeholders.
func DefaultRedactor(e *models.AgentEvent) {
	if e.Tool != nil {
		if len(e.Tool.ArgsJSON) > 0 {
			e.Tool.ArgsJSON = []byte(`"[REDACTED]"`)
		}
		if len(e.Tool.ResultJSON) > 0 {
			e.Tool.ResultJSON = []byte(`"[REDACTED]"`)
		}
	}
	if e.Stream != nil && e.Stream.Delta != "" {
		// Don't redact streaming text by default - it's the main output
		// Callers can provide a custom redactor if needed
		return
	}
}

// =============================================================================
// Replay Harness
// =============================================================================

// TraceReplayer replays events from a trace file to an EventSink for testing or analysis.
type TraceReplayer struct {
	reader  *TraceReader
	sink    EventSink
	speed   float64 // 1.0 = real-time, 0 = as fast as possible
	fromSeq uint64  // start from this sequence (0 = beginning)
	toSeq   uint64  // stop at this sequence (0 = end)
}

// ReplayOption configures the replayer using the functional options pattern.
type ReplayOption func(*TraceReplayer)

// WithSpeed sets the replay speed multiplier. 1.0 is real-time, 0 replays as fast as possible.
func WithSpeed(speed float64) ReplayOption {
	return func(r *TraceReplayer) {
		r.speed = speed
	}
}

// WithSequenceRange limits replay to events within the given sequence number range.
func WithSequenceRange(from, to uint64) ReplayOption {
	return func(r *TraceReplayer) {
		r.fromSeq = from
		r.toSeq = to
	}
}

// NewTraceReplayer creates a new replayer that reads from reader and emits to sink.
func NewTraceReplayer(reader *TraceReader, sink EventSink, opts ...ReplayOption) *TraceReplayer {
	r := &TraceReplayer{
		reader: reader,
		sink:   sink,
		speed:  0, // default: as fast as possible
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Replay plays all events from the trace to the sink and returns statistics about the replay.
func (r *TraceReplayer) Replay(ctx context.Context) (*ReplayStats, error) {
	stats := &ReplayStats{
		Header: r.reader.Header(),
	}

	var lastTime time.Time
	var events []models.AgentEvent

	for {
		event, err := r.reader.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return stats, err
		}

		// Apply sequence range filter
		if r.fromSeq > 0 && event.Sequence < r.fromSeq {
			continue
		}
		if r.toSeq > 0 && event.Sequence > r.toSeq {
			break
		}

		// Speed control
		if r.speed > 0 && !lastTime.IsZero() && !event.Time.IsZero() {
			delay := event.Time.Sub(lastTime)
			if delay > 0 {
				scaledDelay := time.Duration(float64(delay) / r.speed)
				select {
				case <-time.After(scaledDelay):
				case <-ctx.Done():
					return stats, ctx.Err()
				}
			}
		}
		lastTime = event.Time

		// Emit to sink
		r.sink.Emit(ctx, *event)
		events = append(events, *event)
		stats.EventCount++

		// Track sequence
		if event.Sequence > stats.LastSequence {
			stats.LastSequence = event.Sequence
		}
		if stats.FirstSequence == 0 || event.Sequence < stats.FirstSequence {
			stats.FirstSequence = event.Sequence
		}
	}

	// Validate trace structure
	stats.Errors = r.validateTrace(events)

	return stats, nil
}

// validateTrace checks for common trace issues.
func (r *TraceReplayer) validateTrace(events []models.AgentEvent) []string {
	var errors []string

	if len(events) == 0 {
		errors = append(errors, "trace has no events")
		return errors
	}

	// Check run.started is first
	if events[0].Type != models.AgentEventRunStarted {
		errors = append(errors, "first event should be run.started")
	}

	// Check run.finished is last (if present)
	lastEvent := events[len(events)-1]
	if lastEvent.Type == models.AgentEventRunError {
		// Error is acceptable as last event
	} else if lastEvent.Type != models.AgentEventRunFinished {
		errors = append(errors, "last event should be run.finished or run.error")
	}

	// Check sequences are strictly increasing
	var lastSeq uint64
	for i, e := range events {
		if i > 0 && e.Sequence <= lastSeq {
			errors = append(errors, fmt.Sprintf("sequence not strictly increasing at event %d: %d <= %d", i, e.Sequence, lastSeq))
		}
		lastSeq = e.Sequence
	}

	return errors
}

// ReplayStats contains statistics and validation results from a replay operation.
type ReplayStats struct {
	Header        *TraceHeader // Original trace header
	EventCount    int          // Number of events replayed
	FirstSequence uint64       // First sequence number
	LastSequence  uint64       // Last sequence number
	Errors        []string     // Validation errors
}

// Valid returns true if the trace passed all validation checks with no errors.
func (s *ReplayStats) Valid() bool {
	return len(s.Errors) == 0
}

// ReplayToStats replays a trace through a StatsCollector and returns the computed statistics.
func ReplayToStats(reader *TraceReader) (*models.RunStats, error) {
	collector := NewStatsCollector(reader.Header().RunID)
	replayer := NewTraceReplayer(reader, NewCallbackSink(collector.OnEvent))

	_, err := replayer.Replay(context.Background())
	if err != nil {
		return nil, err
	}

	return collector.Stats(), nil
}
