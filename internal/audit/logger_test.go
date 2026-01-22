package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Helper types and functions
// =============================================================================

// nopWriteCloser wraps an io.Writer to implement io.WriteCloser
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// threadSafeBuffer is a thread-safe bytes.Buffer for concurrent write testing
type threadSafeBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *threadSafeBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *threadSafeBuffer) Close() error { return nil }

// createTestLogger creates a logger with a buffer for testing
func createTestLogger(t *testing.T, cfg Config) (*Logger, *threadSafeBuffer) {
	t.Helper()
	buf := &threadSafeBuffer{}

	// Override config for testing
	cfg.Output = "stdout" // Will be replaced
	cfg.Enabled = true
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 100
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 50 * time.Millisecond
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Replace output with our buffer
	logger.output = buf

	return logger, buf
}

// =============================================================================
// 1. Logger Configuration Tests
// =============================================================================

func TestNewLogger_Disabled(t *testing.T) {
	logger, err := NewLogger(Config{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	// Should not panic on disabled logger
	logger.Log(context.Background(), &Event{Type: EventToolInvocation})
	if err := logger.Close(); err != nil {
		t.Errorf("unexpected error closing: %v", err)
	}
}

func TestNewLogger_InvalidOutput(t *testing.T) {
	_, err := NewLogger(Config{
		Enabled: true,
		Output:  "invalid://path",
	})
	if err == nil {
		t.Error("expected error for invalid output")
	}
}

func TestNewLogger_OutputDestinations(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{
			name:    "stdout",
			output:  "stdout",
			wantErr: false,
		},
		{
			name:    "empty defaults to stdout",
			output:  "",
			wantErr: false,
		},
		{
			name:    "stderr",
			output:  "stderr",
			wantErr: false,
		},
		{
			name:    "invalid output",
			output:  "ftp://invalid",
			wantErr: true,
		},
		{
			name:    "file with invalid path",
			output:  "file:/nonexistent/path/that/should/not/exist/audit.log",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewLogger(Config{
				Enabled: true,
				Output:  tt.output,
			})

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if logger == nil {
				t.Fatal("expected non-nil logger")
			}
			defer logger.Close()
		})
	}
}

func TestNewLogger_FileOutput(t *testing.T) {
	// Create temp directory for test file
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewLogger(Config{
		Enabled: true,
		Output:  "file:" + logPath,
		Format:  FormatJSON,
		Level:   LevelInfo,
	})
	if err != nil {
		t.Fatalf("failed to create logger with file output: %v", err)
	}

	// Log an event
	logger.Log(context.Background(), &Event{
		Type:   EventAgentStartup,
		Level:  LevelInfo,
		Action: "test_startup",
	})

	// Wait for buffer flush
	time.Sleep(100 * time.Millisecond)

	// Close to flush (only call once)
	if err := logger.Close(); err != nil {
		t.Errorf("error closing logger: %v", err)
	}

	// Check file was created
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("log file was not created")
	}
}

func TestNewLogger_OutputFormats(t *testing.T) {
	tests := []struct {
		name   string
		format OutputFormat
	}{
		{
			name:   "JSON format",
			format: FormatJSON,
		},
		{
			name:   "Text format",
			format: FormatText,
		},
		{
			name:   "Logfmt format (defaults to JSON)",
			format: FormatLogfmt,
		},
		{
			name:   "Empty format (defaults to JSON)",
			format: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewLogger(Config{
				Enabled: true,
				Format:  tt.format,
				Output:  "stdout",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if logger == nil {
				t.Fatal("expected non-nil logger")
			}
			defer logger.Close()
		})
	}
}

func TestConfig_PrivacyControls(t *testing.T) {
	tests := []struct {
		name                  string
		includeToolInput      bool
		includeToolOutput     bool
		includeMessageContent bool
		input                 string
		expectInputInDetails  bool
		expectHash            bool
	}{
		{
			name:                 "all privacy enabled - include input",
			includeToolInput:     true,
			input:                `{"query":"test"}`,
			expectInputInDetails: true,
			expectHash:           false,
		},
		{
			name:                 "privacy disabled - input hashed",
			includeToolInput:     false,
			input:                `{"query":"test"}`,
			expectInputInDetails: false,
			expectHash:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := &Logger{
				config: Config{
					Enabled:               true,
					Level:                 LevelInfo,
					SampleRate:            1.0,
					IncludeToolInput:      tt.includeToolInput,
					IncludeToolOutput:     tt.includeToolOutput,
					IncludeMessageContent: tt.includeMessageContent,
					MaxFieldSize:          1024,
				},
				eventTypes: make(map[EventType]bool),
				output:     &nopWriteCloser{buf},
				buffer:     make(chan *Event, 10),
				done:       make(chan struct{}),
			}

			logger.LogToolInvocation(context.Background(), "test_tool", "call-123", []byte(tt.input), "session-key")

			// Check event in buffer
			select {
			case event := <-logger.buffer:
				details := event.Details
				if tt.expectInputInDetails {
					if _, ok := details["input"]; !ok {
						t.Error("expected input in details")
					}
				}
				if tt.expectHash {
					if _, ok := details["input_hash"]; !ok {
						t.Error("expected input_hash in details")
					}
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("expected event in buffer")
			}
		})
	}
}

func TestConfig_SamplingRates(t *testing.T) {
	tests := []struct {
		name        string
		sampleRate  float64
		eventCount  int
		expectRange [2]int // min, max expected events (accounting for randomness)
	}{
		{
			name:        "100% sampling",
			sampleRate:  1.0,
			eventCount:  100,
			expectRange: [2]int{100, 100},
		},
		{
			name:        "0% sampling",
			sampleRate:  0.0,
			eventCount:  100,
			expectRange: [2]int{0, 0},
		},
		{
			name:        "50% sampling (approximate)",
			sampleRate:  0.5,
			eventCount:  1000,
			expectRange: [2]int{300, 700}, // Wide range for statistical variance
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &Logger{
				config: Config{
					Enabled:    true,
					Level:      LevelInfo,
					SampleRate: tt.sampleRate,
				},
				eventTypes: make(map[EventType]bool),
				buffer:     make(chan *Event, tt.eventCount+100),
				done:       make(chan struct{}),
			}

			for i := 0; i < tt.eventCount; i++ {
				logger.Log(context.Background(), &Event{
					Type:   EventToolInvocation,
					Level:  LevelInfo,
					Action: "test",
				})
			}

			// Count events in buffer
			count := len(logger.buffer)

			if count < tt.expectRange[0] || count > tt.expectRange[1] {
				t.Errorf("expected events in range [%d, %d], got %d",
					tt.expectRange[0], tt.expectRange[1], count)
			}
		})
	}
}

func TestConfig_MaxFieldSizeTruncation(t *testing.T) {
	tests := []struct {
		name         string
		maxFieldSize int
		inputSize    int
		expectTrunc  bool
	}{
		{
			name:         "input within limit",
			maxFieldSize: 100,
			inputSize:    50,
			expectTrunc:  false,
		},
		{
			name:         "input exceeds limit",
			maxFieldSize: 50,
			inputSize:    100,
			expectTrunc:  true,
		},
		{
			name:         "input exactly at limit",
			maxFieldSize: 50,
			inputSize:    50,
			expectTrunc:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &Logger{
				config: Config{
					Enabled:          true,
					Level:            LevelInfo,
					SampleRate:       1.0,
					IncludeToolInput: true,
					MaxFieldSize:     tt.maxFieldSize,
				},
				eventTypes: make(map[EventType]bool),
				buffer:     make(chan *Event, 10),
				done:       make(chan struct{}),
			}

			// Create input of specific size
			input := strings.Repeat("a", tt.inputSize)
			logger.LogToolInvocation(context.Background(), "test_tool", "call-123", []byte(input), "session-key")

			select {
			case event := <-logger.buffer:
				inputVal, ok := event.Details["input"].(string)
				if !ok {
					t.Fatal("expected input in details")
				}
				if tt.expectTrunc {
					if !strings.HasSuffix(inputVal, "...(truncated)") {
						t.Error("expected truncation suffix")
					}
					if len(inputVal) > tt.maxFieldSize+20 { // +20 for truncation suffix
						t.Errorf("truncated input too long: %d", len(inputVal))
					}
				} else {
					if strings.HasSuffix(inputVal, "...(truncated)") {
						t.Error("unexpected truncation")
					}
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("expected event in buffer")
			}
		})
	}
}

func TestConfig_MaxFieldSizeTruncation_ToolOutput(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:           true,
			Level:             LevelInfo,
			SampleRate:        1.0,
			IncludeToolOutput: true,
			MaxFieldSize:      50,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	// Create output that exceeds limit
	output := strings.Repeat("x", 100)
	logger.LogToolCompletion(context.Background(), "test_tool", "call-123", true, output, time.Second, "session-key")

	select {
	case event := <-logger.buffer:
		outputVal, ok := event.Details["output"].(string)
		if !ok {
			t.Fatal("expected output in details")
		}
		if !strings.HasSuffix(outputVal, "...(truncated)") {
			t.Error("expected truncation suffix")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestConfig_ToolOutputSize(t *testing.T) {
	// When IncludeToolOutput is false, only size should be recorded
	logger := &Logger{
		config: Config{
			Enabled:           true,
			Level:             LevelInfo,
			SampleRate:        1.0,
			IncludeToolOutput: false,
			MaxFieldSize:      1024,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	output := "test output data"
	logger.LogToolCompletion(context.Background(), "test_tool", "call-123", true, output, time.Second, "session-key")

	select {
	case event := <-logger.buffer:
		if _, ok := event.Details["output"]; ok {
			t.Error("should not include output when IncludeToolOutput is false")
		}
		outputSize, ok := event.Details["output_size"].(int)
		if !ok {
			t.Fatal("expected output_size in details")
		}
		if outputSize != len(output) {
			t.Errorf("expected output_size %d, got %d", len(output), outputSize)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

// =============================================================================
// 2. Event Logging Tests
// =============================================================================

func TestLogger_LogLevels(t *testing.T) {
	tests := []struct {
		configLevel Level
		eventLevel  Level
		shouldLog   bool
	}{
		{LevelDebug, LevelDebug, true},
		{LevelDebug, LevelInfo, true},
		{LevelDebug, LevelWarn, true},
		{LevelDebug, LevelError, true},
		{LevelInfo, LevelDebug, false},
		{LevelInfo, LevelInfo, true},
		{LevelInfo, LevelWarn, true},
		{LevelInfo, LevelError, true},
		{LevelWarn, LevelDebug, false},
		{LevelWarn, LevelInfo, false},
		{LevelWarn, LevelWarn, true},
		{LevelWarn, LevelError, true},
		{LevelError, LevelDebug, false},
		{LevelError, LevelInfo, false},
		{LevelError, LevelWarn, false},
		{LevelError, LevelError, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.configLevel)+"_"+string(tt.eventLevel), func(t *testing.T) {
			logger := &Logger{
				config: Config{
					Enabled: true,
					Level:   tt.configLevel,
				},
			}
			result := logger.shouldLog(tt.eventLevel)
			if result != tt.shouldLog {
				t.Errorf("shouldLog(%s) with config level %s = %v, want %v",
					tt.eventLevel, tt.configLevel, result, tt.shouldLog)
			}
		})
	}
}

func TestLogger_EventTypeFilter(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: map[EventType]bool{
			EventToolInvocation: true,
		},
		output: &nopWriteCloser{buf},
		buffer: make(chan *Event, 10),
		done:   make(chan struct{}),
	}

	// Should be filtered out
	logger.Log(context.Background(), &Event{
		Type:  EventToolCompletion,
		Level: LevelInfo,
	})

	// Should be logged (if we had slogger initialized)
	logger.Log(context.Background(), &Event{
		Type:  EventToolInvocation,
		Level: LevelInfo,
	})

	// Check buffer
	select {
	case event := <-logger.buffer:
		if event.Type != EventToolInvocation {
			t.Errorf("expected EventToolInvocation, got %v", event.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestLogger_AllEventTypes(t *testing.T) {
	eventTypes := []struct {
		eventType EventType
		level     Level
	}{
		// Tool events
		{EventToolInvocation, LevelInfo},
		{EventToolCompletion, LevelInfo},
		{EventToolDenied, LevelWarn},
		{EventToolRetry, LevelWarn},
		// Agent events
		{EventAgentAction, LevelInfo},
		{EventAgentHandoff, LevelInfo},
		{EventAgentError, LevelError},
		{EventAgentStartup, LevelInfo},
		{EventAgentShutdown, LevelInfo},
		// Permission events
		{EventPermissionGranted, LevelInfo},
		{EventPermissionDenied, LevelWarn},
		{EventPermissionRequest, LevelInfo},
		// Session events
		{EventSessionCreate, LevelInfo},
		{EventSessionUpdate, LevelInfo},
		{EventSessionDelete, LevelWarn},
		{EventSessionCompact, LevelInfo},
		// Message events
		{EventMessageReceived, LevelInfo},
		{EventMessageProcessed, LevelInfo},
		{EventMessageSent, LevelInfo},
		// Gateway events
		{EventGatewayStartup, LevelInfo},
		{EventGatewayShutdown, LevelInfo},
		{EventGatewayError, LevelError},
	}

	for _, tt := range eventTypes {
		t.Run(string(tt.eventType), func(t *testing.T) {
			logger := &Logger{
				config: Config{
					Enabled:    true,
					Level:      LevelDebug, // Allow all levels
					SampleRate: 1.0,
				},
				eventTypes: make(map[EventType]bool), // Empty = allow all
				buffer:     make(chan *Event, 10),
				done:       make(chan struct{}),
			}

			event := &Event{
				Type:   tt.eventType,
				Level:  tt.level,
				Action: "test_" + string(tt.eventType),
			}

			logger.Log(context.Background(), event)

			select {
			case receivedEvent := <-logger.buffer:
				if receivedEvent.Type != tt.eventType {
					t.Errorf("expected event type %s, got %s", tt.eventType, receivedEvent.Type)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("expected event in buffer")
			}
		})
	}
}

func TestLogger_EventMetadataPreservation(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	originalEvent := &Event{
		Type:          EventToolInvocation,
		Level:         LevelInfo,
		SessionID:     "sess-123",
		SessionKey:    "agent:main:telegram:123",
		AgentID:       "agent-456",
		ToolName:      "web_search",
		ToolCallID:    "call-789",
		Action:        "tool_invoked",
		UserID:        "user-111",
		Channel:       "telegram",
		ParentEventID: "parent-222",
		Details: map[string]any{
			"custom_field": "custom_value",
		},
	}

	logger.Log(context.Background(), originalEvent)

	select {
	case event := <-logger.buffer:
		// ID should be auto-generated if empty
		if event.ID == "" {
			t.Error("expected ID to be set")
		}
		// Timestamp should be set
		if event.Timestamp.IsZero() {
			t.Error("expected Timestamp to be set")
		}
		// Check preserved fields
		if event.SessionID != originalEvent.SessionID {
			t.Errorf("SessionID mismatch: got %s, want %s", event.SessionID, originalEvent.SessionID)
		}
		if event.SessionKey != originalEvent.SessionKey {
			t.Errorf("SessionKey mismatch: got %s, want %s", event.SessionKey, originalEvent.SessionKey)
		}
		if event.AgentID != originalEvent.AgentID {
			t.Errorf("AgentID mismatch: got %s, want %s", event.AgentID, originalEvent.AgentID)
		}
		if event.ToolName != originalEvent.ToolName {
			t.Errorf("ToolName mismatch: got %s, want %s", event.ToolName, originalEvent.ToolName)
		}
		if event.ToolCallID != originalEvent.ToolCallID {
			t.Errorf("ToolCallID mismatch: got %s, want %s", event.ToolCallID, originalEvent.ToolCallID)
		}
		if event.UserID != originalEvent.UserID {
			t.Errorf("UserID mismatch: got %s, want %s", event.UserID, originalEvent.UserID)
		}
		if event.Channel != originalEvent.Channel {
			t.Errorf("Channel mismatch: got %s, want %s", event.Channel, originalEvent.Channel)
		}
		if event.ParentEventID != originalEvent.ParentEventID {
			t.Errorf("ParentEventID mismatch: got %s, want %s", event.ParentEventID, originalEvent.ParentEventID)
		}
		// Check details
		if event.Details["custom_field"] != "custom_value" {
			t.Error("Details not preserved correctly")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestLogger_LogToolInvocation(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:          true,
			Level:            LevelInfo,
			SampleRate:       1.0,
			IncludeToolInput: true,
			MaxFieldSize:     1024,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	input := json.RawMessage(`{"query":"test search"}`)
	logger.LogToolInvocation(context.Background(), "web_search", "call-123", input, "session-key")

	select {
	case event := <-logger.buffer:
		if event.Type != EventToolInvocation {
			t.Errorf("expected EventToolInvocation, got %s", event.Type)
		}
		if event.ToolName != "web_search" {
			t.Errorf("expected ToolName 'web_search', got %s", event.ToolName)
		}
		if event.ToolCallID != "call-123" {
			t.Errorf("expected ToolCallID 'call-123', got %s", event.ToolCallID)
		}
		if event.SessionKey != "session-key" {
			t.Errorf("expected SessionKey 'session-key', got %s", event.SessionKey)
		}
		if event.Action != "tool_invoked" {
			t.Errorf("expected Action 'tool_invoked', got %s", event.Action)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestLogger_LogToolCompletion(t *testing.T) {
	tests := []struct {
		name    string
		success bool
		level   Level
	}{
		{
			name:    "successful completion",
			success: true,
			level:   LevelInfo,
		},
		{
			name:    "failed completion",
			success: false,
			level:   LevelWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &Logger{
				config: Config{
					Enabled:           true,
					Level:             LevelDebug,
					SampleRate:        1.0,
					IncludeToolOutput: true,
					MaxFieldSize:      1024,
				},
				eventTypes: make(map[EventType]bool),
				buffer:     make(chan *Event, 10),
				done:       make(chan struct{}),
			}

			duration := 500 * time.Millisecond
			logger.LogToolCompletion(context.Background(), "web_search", "call-123", tt.success, "output data", duration, "session-key")

			select {
			case event := <-logger.buffer:
				if event.Type != EventToolCompletion {
					t.Errorf("expected EventToolCompletion, got %s", event.Type)
				}
				if event.Level != tt.level {
					t.Errorf("expected Level %s, got %s", tt.level, event.Level)
				}
				if event.Duration != duration {
					t.Errorf("expected Duration %v, got %v", duration, event.Duration)
				}
				if event.Details["success"] != tt.success {
					t.Errorf("expected success=%v in details", tt.success)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("expected event in buffer")
			}
		})
	}
}

func TestLogger_LogToolDenied(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	logger.LogToolDenied(context.Background(), "dangerous_tool", "call-123", "policy violation", "deny_all_policy", "session-key")

	select {
	case event := <-logger.buffer:
		if event.Type != EventToolDenied {
			t.Errorf("expected EventToolDenied, got %s", event.Type)
		}
		if event.Level != LevelWarn {
			t.Errorf("expected LevelWarn, got %s", event.Level)
		}
		if event.Details["reason"] != "policy violation" {
			t.Error("expected reason in details")
		}
		if event.Details["policy_matched"] != "deny_all_policy" {
			t.Error("expected policy_matched in details")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestLogger_LogPermissionDecision(t *testing.T) {
	tests := []struct {
		name      string
		granted   bool
		eventType EventType
		level     Level
	}{
		{
			name:      "permission granted",
			granted:   true,
			eventType: EventPermissionGranted,
			level:     LevelInfo,
		},
		{
			name:      "permission denied",
			granted:   false,
			eventType: EventPermissionDenied,
			level:     LevelWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &Logger{
				config: Config{
					Enabled:    true,
					Level:      LevelDebug,
					SampleRate: 1.0,
				},
				eventTypes: make(map[EventType]bool),
				buffer:     make(chan *Event, 10),
				done:       make(chan struct{}),
			}

			logger.LogPermissionDecision(context.Background(), tt.granted, "file_read", "/tmp/test", "read", "test reason", "session-key")

			select {
			case event := <-logger.buffer:
				if event.Type != tt.eventType {
					t.Errorf("expected %s, got %s", tt.eventType, event.Type)
				}
				if event.Level != tt.level {
					t.Errorf("expected %s, got %s", tt.level, event.Level)
				}
				if event.Details["granted"] != tt.granted {
					t.Errorf("expected granted=%v in details", tt.granted)
				}
				if event.Details["permission"] != "file_read" {
					t.Error("expected permission in details")
				}
				if event.Details["resource"] != "/tmp/test" {
					t.Error("expected resource in details")
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("expected event in buffer")
			}
		})
	}
}

func TestLogger_LogAgentHandoff(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	logger.LogAgentHandoff(context.Background(), "agent-1", "agent-2", "task delegation", "full", 2, "session-key")

	select {
	case event := <-logger.buffer:
		if event.Type != EventAgentHandoff {
			t.Errorf("expected EventAgentHandoff, got %s", event.Type)
		}
		if event.AgentID != "agent-2" {
			t.Errorf("expected AgentID 'agent-2', got %s", event.AgentID)
		}
		if event.Details["from_agent_id"] != "agent-1" {
			t.Error("expected from_agent_id in details")
		}
		if event.Details["to_agent_id"] != "agent-2" {
			t.Error("expected to_agent_id in details")
		}
		if event.Details["reason"] != "task delegation" {
			t.Error("expected reason in details")
		}
		if event.Details["context_mode"] != "full" {
			t.Error("expected context_mode in details")
		}
		if event.Details["handoff_depth"] != 2 {
			t.Error("expected handoff_depth in details")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestLogger_LogSessionCompact(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	logger.LogSessionCompact(context.Background(), "sess-123", "session-key", 100, 50, 5000, "sliding_window")

	select {
	case event := <-logger.buffer:
		if event.Type != EventSessionCompact {
			t.Errorf("expected EventSessionCompact, got %s", event.Type)
		}
		if event.SessionID != "sess-123" {
			t.Errorf("expected SessionID 'sess-123', got %s", event.SessionID)
		}
		if event.Details["messages_before_compact"] != 100 {
			t.Error("expected messages_before_compact in details")
		}
		if event.Details["messages_after_compact"] != 50 {
			t.Error("expected messages_after_compact in details")
		}
		if event.Details["tokens_saved"] != 5000 {
			t.Error("expected tokens_saved in details")
		}
		if event.Details["compaction_strategy"] != "sliding_window" {
			t.Error("expected compaction_strategy in details")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestLogger_LogAgentAction(t *testing.T) {
	tests := []struct {
		name        string
		details     map[string]any
		description string
	}{
		{
			name:        "with existing details",
			details:     map[string]any{"key": "value"},
			description: "test action",
		},
		{
			name:        "with nil details",
			details:     nil,
			description: "test action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &Logger{
				config: Config{
					Enabled:    true,
					Level:      LevelInfo,
					SampleRate: 1.0,
				},
				eventTypes: make(map[EventType]bool),
				buffer:     make(chan *Event, 10),
				done:       make(chan struct{}),
			}

			logger.LogAgentAction(context.Background(), "agent-123", "process_message", tt.description, tt.details, "session-key")

			select {
			case event := <-logger.buffer:
				if event.Type != EventAgentAction {
					t.Errorf("expected EventAgentAction, got %s", event.Type)
				}
				if event.AgentID != "agent-123" {
					t.Errorf("expected AgentID 'agent-123', got %s", event.AgentID)
				}
				if event.Action != "process_message" {
					t.Errorf("expected Action 'process_message', got %s", event.Action)
				}
				if event.Details["description"] != tt.description {
					t.Error("expected description in details")
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("expected event in buffer")
			}
		})
	}
}

func TestLogger_LogError(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	details := map[string]any{"context": "test context"}
	logger.LogError(context.Background(), EventAgentError, "error_action", "something went wrong", details, "session-key")

	select {
	case event := <-logger.buffer:
		if event.Type != EventAgentError {
			t.Errorf("expected EventAgentError, got %s", event.Type)
		}
		if event.Level != LevelError {
			t.Errorf("expected LevelError, got %s", event.Level)
		}
		if event.Error != "something went wrong" {
			t.Errorf("expected Error 'something went wrong', got %s", event.Error)
		}
		if event.Details["context"] != "test context" {
			t.Error("expected context in details")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

// =============================================================================
// 3. Async/Buffered Writing Tests
// =============================================================================

func TestLogger_AsyncBufferedWrite(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "async_test.log")

	logger, err := NewLogger(Config{
		Enabled:       true,
		Output:        "file:" + logPath,
		Format:        FormatJSON,
		Level:         LevelInfo,
		BufferSize:    100,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Log multiple events
	for i := 0; i < 10; i++ {
		logger.Log(context.Background(), &Event{
			Type:   EventAgentAction,
			Level:  LevelInfo,
			Action: "test_action",
		})
	}

	// Wait for flush interval
	time.Sleep(100 * time.Millisecond)

	// Close logger
	if err := logger.Close(); err != nil {
		t.Errorf("error closing logger: %v", err)
	}

	// Check file content
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	// Should have logged events
	if len(data) == 0 {
		t.Error("expected log file to have content")
	}
}

func TestLogger_BufferFlushOnClose(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "flush_on_close.log")

	logger, err := NewLogger(Config{
		Enabled:       true,
		Output:        "file:" + logPath,
		Format:        FormatJSON,
		Level:         LevelInfo,
		BufferSize:    1000,
		FlushInterval: 10 * time.Second, // Long interval - won't auto flush
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Log events
	for i := 0; i < 5; i++ {
		logger.Log(context.Background(), &Event{
			Type:   EventAgentAction,
			Level:  LevelInfo,
			Action: "test_action",
		})
	}

	// Close immediately - should flush
	if err := logger.Close(); err != nil {
		t.Errorf("error closing logger: %v", err)
	}

	// Check file content
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	// Should have logged events
	if len(data) == 0 {
		t.Error("expected log file to have content after close")
	}
}

func TestLogger_ConcurrentWriteSafety(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "concurrent_test.log")

	logger, err := NewLogger(Config{
		Enabled:       true,
		Output:        "file:" + logPath,
		Format:        FormatJSON,
		Level:         LevelInfo,
		BufferSize:    1000,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Concurrent writes from multiple goroutines
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				logger.Log(context.Background(), &Event{
					Type:   EventAgentAction,
					Level:  LevelInfo,
					Action: "concurrent_test",
					Details: map[string]any{
						"goroutine": id,
						"event":     j,
					},
				})
			}
		}(i)
	}

	wg.Wait()

	// Close and flush
	if err := logger.Close(); err != nil {
		t.Errorf("error closing logger: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	// Count lines (each JSON log is one line with slog)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	expectedMin := numGoroutines * eventsPerGoroutine * 80 / 100 // Allow some tolerance

	if len(lines) < expectedMin {
		t.Errorf("expected at least %d log entries, got %d", expectedMin, len(lines))
	}
}

func TestLogger_BufferFullBehavior(t *testing.T) {
	// Create a real logger with small buffer for this test
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "buffer_full_test.log")

	logger, err := NewLogger(Config{
		Enabled:       true,
		Output:        "file:" + logPath,
		Level:         LevelInfo,
		BufferSize:    1, // Very small buffer
		FlushInterval: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Log rapidly to potentially fill buffer - the Log method should not block
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			logger.Log(context.Background(), &Event{
				Type:   EventAgentAction,
				Level:  LevelInfo,
				Action: "overflow_test",
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// Good - didn't block
	case <-time.After(500 * time.Millisecond):
		t.Error("Log() blocked when buffer was full")
	}
}

// =============================================================================
// 4. Session-Bound Logger Tests
// =============================================================================

func TestSessionLogger(t *testing.T) {
	// Create a session logger
	mainLogger := &Logger{
		config: Config{
			Enabled:          true,
			Level:            LevelInfo,
			SampleRate:       1.0,
			IncludeToolInput: true,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	sessionLogger := mainLogger.WithSessionKey("agent:main:telegram:123")
	if sessionLogger.sessionKey != "agent:main:telegram:123" {
		t.Errorf("expected session key to be set, got %s", sessionLogger.sessionKey)
	}
}

func TestSessionLogger_FieldInheritance(t *testing.T) {
	mainLogger := &Logger{
		config: Config{
			Enabled:          true,
			Level:            LevelInfo,
			SampleRate:       1.0,
			IncludeToolInput: true,
			MaxFieldSize:     1024,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	sessionKey := "agent:main:telegram:user123"
	sessionLogger := mainLogger.WithSessionKey(sessionKey)

	// Test LogToolInvocation
	sessionLogger.LogToolInvocation(context.Background(), "test_tool", "call-123", []byte(`{"query":"test"}`))

	select {
	case event := <-mainLogger.buffer:
		if event.SessionKey != sessionKey {
			t.Errorf("expected SessionKey %s, got %s", sessionKey, event.SessionKey)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}

	// Test LogToolCompletion
	sessionLogger.LogToolCompletion(context.Background(), "test_tool", "call-123", true, "output", time.Second)

	select {
	case event := <-mainLogger.buffer:
		if event.SessionKey != sessionKey {
			t.Errorf("expected SessionKey %s, got %s", sessionKey, event.SessionKey)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestSessionLogger_AllMethods(t *testing.T) {
	mainLogger := &Logger{
		config: Config{
			Enabled:           true,
			Level:             LevelDebug,
			SampleRate:        1.0,
			IncludeToolInput:  true,
			IncludeToolOutput: true,
			MaxFieldSize:      1024,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 20),
		done:       make(chan struct{}),
	}

	sessionKey := "agent:main:slack:channel123"
	sessionLogger := mainLogger.WithSessionKey(sessionKey)

	// Test all SessionLogger methods
	ctx := context.Background()

	// LogToolInvocation
	sessionLogger.LogToolInvocation(ctx, "tool1", "call-1", []byte(`{}`))

	// LogToolCompletion
	sessionLogger.LogToolCompletion(ctx, "tool1", "call-1", true, "done", time.Second)

	// LogToolDenied
	sessionLogger.LogToolDenied(ctx, "tool2", "call-2", "policy", "deny_policy")

	// LogPermissionDecision
	sessionLogger.LogPermissionDecision(ctx, true, "read", "/file", "access", "allowed")

	// LogAgentHandoff
	sessionLogger.LogAgentHandoff(ctx, "agent1", "agent2", "task", "full", 1)

	// LogAgentAction
	sessionLogger.LogAgentAction(ctx, "agent1", "action", "desc", nil)

	// LogError
	sessionLogger.LogError(ctx, EventAgentError, "error_action", "error message", nil)

	// Verify all events have the session key
	eventCount := 0
	timeout := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case event := <-mainLogger.buffer:
			if event.SessionKey != sessionKey {
				t.Errorf("event %d: expected SessionKey %s, got %s", eventCount, sessionKey, event.SessionKey)
			}
			eventCount++
			if eventCount >= 7 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if eventCount != 7 {
		t.Errorf("expected 7 events, got %d", eventCount)
	}
}

// =============================================================================
// 5. Distributed Tracing Tests
// =============================================================================

func TestLogger_TraceIDAndSpanIDInclusion(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	// Event with pre-set trace IDs
	event := &Event{
		Type:    EventAgentAction,
		Level:   LevelInfo,
		Action:  "test",
		TraceID: "trace-123",
		SpanID:  "span-456",
	}

	logger.Log(context.Background(), event)

	select {
	case receivedEvent := <-logger.buffer:
		if receivedEvent.TraceID != "trace-123" {
			t.Errorf("expected TraceID 'trace-123', got %s", receivedEvent.TraceID)
		}
		if receivedEvent.SpanID != "span-456" {
			t.Errorf("expected SpanID 'span-456', got %s", receivedEvent.SpanID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestLogger_DurationTracking(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	duration := 2500 * time.Millisecond
	event := &Event{
		Type:     EventToolCompletion,
		Level:    LevelInfo,
		Action:   "test",
		Duration: duration,
	}

	logger.Log(context.Background(), event)

	select {
	case receivedEvent := <-logger.buffer:
		if receivedEvent.Duration != duration {
			t.Errorf("expected Duration %v, got %v", duration, receivedEvent.Duration)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

// =============================================================================
// 6. Utility Function Tests
// =============================================================================

func TestHashString(t *testing.T) {
	// Same input should produce same hash
	hash1 := hashString("test input")
	hash2 := hashString("test input")
	if hash1 != hash2 {
		t.Errorf("expected same hash for same input, got %s and %s", hash1, hash2)
	}

	// Different input should produce different hash
	hash3 := hashString("different input")
	if hash1 == hash3 {
		t.Error("expected different hash for different input")
	}

	// Hash should be 16 characters
	if len(hash1) != 16 {
		t.Errorf("expected hash length 16, got %d", len(hash1))
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("expected Enabled to be false")
	}
	if cfg.Level != LevelInfo {
		t.Errorf("expected Level to be LevelInfo, got %v", cfg.Level)
	}
	if cfg.Format != FormatJSON {
		t.Errorf("expected Format to be FormatJSON, got %v", cfg.Format)
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("expected SampleRate to be 1.0, got %v", cfg.SampleRate)
	}
	if cfg.Output != "stdout" {
		t.Errorf("expected Output to be 'stdout', got %v", cfg.Output)
	}
	if cfg.IncludeToolInput {
		t.Error("expected IncludeToolInput to be false")
	}
	if cfg.IncludeToolOutput {
		t.Error("expected IncludeToolOutput to be false")
	}
	if cfg.IncludeMessageContent {
		t.Error("expected IncludeMessageContent to be false")
	}
	if cfg.MaxFieldSize != 1024 {
		t.Errorf("expected MaxFieldSize to be 1024, got %d", cfg.MaxFieldSize)
	}
	if cfg.BufferSize != 1000 {
		t.Errorf("expected BufferSize to be 1000, got %d", cfg.BufferSize)
	}
	if cfg.FlushInterval != 5*time.Second {
		t.Errorf("expected FlushInterval to be 5s, got %v", cfg.FlushInterval)
	}
}

func TestEvent_Marshaling(t *testing.T) {
	event := &Event{
		ID:         "test-id",
		Type:       EventToolInvocation,
		Level:      LevelInfo,
		Timestamp:  time.Now(),
		SessionKey: "agent:main:telegram:123",
		ToolName:   "web_search",
		ToolCallID: "call-123",
		Action:     "tool_invoked",
		Details: map[string]any{
			"query": "test query",
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if decoded.ID != event.ID {
		t.Errorf("expected ID %s, got %s", event.ID, decoded.ID)
	}
	if decoded.Type != event.Type {
		t.Errorf("expected Type %s, got %s", event.Type, decoded.Type)
	}
	if decoded.ToolName != event.ToolName {
		t.Errorf("expected ToolName %s, got %s", event.ToolName, decoded.ToolName)
	}
}

func TestEvent_MarshalingWithAllFields(t *testing.T) {
	now := time.Now()
	event := &Event{
		ID:            "test-id",
		Type:          EventToolCompletion,
		Level:         LevelWarn,
		Timestamp:     now,
		SessionID:     "session-123",
		SessionKey:    "agent:main:telegram:user",
		AgentID:       "agent-456",
		ToolName:      "web_search",
		ToolCallID:    "call-789",
		Action:        "tool_completed",
		Duration:      time.Second,
		Error:         "error message",
		UserID:        "user-111",
		Channel:       "telegram",
		TraceID:       "trace-222",
		SpanID:        "span-333",
		ParentEventID: "parent-444",
		Details: map[string]any{
			"key1": "value1",
			"key2": 123,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	// Verify all fields
	if decoded.SessionID != event.SessionID {
		t.Errorf("SessionID mismatch")
	}
	if decoded.AgentID != event.AgentID {
		t.Errorf("AgentID mismatch")
	}
	if decoded.Error != event.Error {
		t.Errorf("Error mismatch")
	}
	if decoded.UserID != event.UserID {
		t.Errorf("UserID mismatch")
	}
	if decoded.Channel != event.Channel {
		t.Errorf("Channel mismatch")
	}
	if decoded.TraceID != event.TraceID {
		t.Errorf("TraceID mismatch")
	}
	if decoded.SpanID != event.SpanID {
		t.Errorf("SpanID mismatch")
	}
	if decoded.ParentEventID != event.ParentEventID {
		t.Errorf("ParentEventID mismatch")
	}
}

// =============================================================================
// 7. Global Logger Tests
// =============================================================================

func TestGlobalLogger(t *testing.T) {
	// Save original
	originalLogger := GetGlobalLogger()
	defer SetGlobalLogger(originalLogger)

	// Set new logger
	testLogger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool),
		buffer:     make(chan *Event, 10),
		done:       make(chan struct{}),
	}

	SetGlobalLogger(testLogger)

	if GetGlobalLogger() != testLogger {
		t.Error("expected global logger to be set")
	}

	// Test global Log function
	Log(context.Background(), &Event{
		Type:   EventAgentAction,
		Level:  LevelInfo,
		Action: "global_test",
	})

	select {
	case event := <-testLogger.buffer:
		if event.Action != "global_test" {
			t.Errorf("expected Action 'global_test', got %s", event.Action)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in buffer")
	}
}

func TestGlobalLogger_NilSafe(t *testing.T) {
	// Save original
	originalLogger := GetGlobalLogger()
	defer SetGlobalLogger(originalLogger)

	// Set nil logger
	SetGlobalLogger(nil)

	// Should not panic
	Log(context.Background(), &Event{
		Type:   EventAgentAction,
		Level:  LevelInfo,
		Action: "nil_test",
	})
}

// =============================================================================
// 8. slogLevel Tests
// =============================================================================

func TestLogger_SlogLevel(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{"unknown", "INFO"}, // Default
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			logger := &Logger{
				config: Config{Level: tt.level},
			}
			slogLvl := logger.slogLevel()
			if slogLvl.String() != tt.expected {
				t.Errorf("expected slog level %s, got %s", tt.expected, slogLvl.String())
			}
		})
	}
}

// =============================================================================
// 9. Event Filtering Tests
// =============================================================================

func TestLogger_MultipleEventTypeFilters(t *testing.T) {
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: map[EventType]bool{
			EventToolInvocation: true,
			EventToolCompletion: true,
			EventAgentAction:    true,
		},
		buffer: make(chan *Event, 20),
		done:   make(chan struct{}),
	}

	// These should pass
	allowedTypes := []EventType{EventToolInvocation, EventToolCompletion, EventAgentAction}
	for _, et := range allowedTypes {
		logger.Log(context.Background(), &Event{Type: et, Level: LevelInfo})
	}

	// These should be filtered
	filteredTypes := []EventType{EventToolDenied, EventAgentHandoff, EventSessionCompact}
	for _, et := range filteredTypes {
		logger.Log(context.Background(), &Event{Type: et, Level: LevelInfo})
	}

	// Check buffer contains only allowed types
	count := 0
	timeout := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case event := <-logger.buffer:
			found := false
			for _, allowed := range allowedTypes {
				if event.Type == allowed {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("unexpected event type in buffer: %s", event.Type)
			}
			count++
		case <-timeout:
			break loop
		}
	}

	if count != len(allowedTypes) {
		t.Errorf("expected %d events, got %d", len(allowedTypes), count)
	}
}

func TestLogger_EmptyEventTypeFilter(t *testing.T) {
	// Empty filter should allow all events
	logger := &Logger{
		config: Config{
			Enabled:    true,
			Level:      LevelInfo,
			SampleRate: 1.0,
		},
		eventTypes: make(map[EventType]bool), // Empty
		buffer:     make(chan *Event, 20),
		done:       make(chan struct{}),
	}

	eventTypes := []EventType{
		EventToolInvocation, EventToolCompletion, EventAgentAction,
		EventPermissionGranted, EventSessionCompact,
	}

	for _, et := range eventTypes {
		logger.Log(context.Background(), &Event{Type: et, Level: LevelInfo})
	}

	count := 0
	timeout := time.After(100 * time.Millisecond)
loop:
	for {
		select {
		case <-logger.buffer:
			count++
		case <-timeout:
			break loop
		}
	}

	if count != len(eventTypes) {
		t.Errorf("expected all %d events to pass through, got %d", len(eventTypes), count)
	}
}

// =============================================================================
// 10. WriteEvent Tests
// =============================================================================

func TestLogger_WriteEventAllFields(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "write_event_test.log")

	logger, err := NewLogger(Config{
		Enabled:       true,
		Output:        "file:" + logPath,
		Format:        FormatJSON,
		Level:         LevelDebug,
		BufferSize:    10,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Log event with all fields populated
	event := &Event{
		ID:            "test-id",
		Type:          EventToolCompletion,
		Level:         LevelInfo,
		Timestamp:     time.Now(),
		SessionID:     "sess-123",
		SessionKey:    "agent:main:telegram:user",
		AgentID:       "agent-456",
		ToolName:      "web_search",
		ToolCallID:    "call-789",
		Action:        "tool_completed",
		Duration:      time.Second,
		Error:         "some error",
		UserID:        "user-111",
		Channel:       "telegram",
		TraceID:       "trace-222",
		SpanID:        "span-333",
		ParentEventID: "parent-444",
		Details: map[string]any{
			"custom_key": "custom_value",
		},
	}

	logger.Log(context.Background(), event)

	// Wait and close
	time.Sleep(100 * time.Millisecond)
	logger.Close()

	// Read and verify log content
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	content := string(data)

	// Verify key fields are present in output
	expectedFields := []string{
		"audit_id", "audit_type", "action", "session_id", "session_key",
		"agent_id", "tool_name", "tool_call_id", "user_id", "channel",
		"trace_id", "span_id", "parent_event_id", "duration_ms", "error",
	}

	for _, field := range expectedFields {
		if !strings.Contains(content, field) {
			t.Errorf("expected field %s in log output", field)
		}
	}
}

// =============================================================================
// 11. New Logger Configuration Defaults Tests
// =============================================================================

func TestNewLogger_ConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "defaults_test.log")

	// Config with zeros for values that should get defaults
	logger, err := NewLogger(Config{
		Enabled:       true,
		Output:        "file:" + logPath,
		SampleRate:    0, // Should default to 1.0
		BufferSize:    0, // Should default to 1000
		FlushInterval: 0, // Should default to 5s
		MaxFieldSize:  0, // Should default to 1024
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Close()

	// Verify defaults were applied
	if logger.config.SampleRate != 1.0 {
		t.Errorf("expected SampleRate 1.0, got %v", logger.config.SampleRate)
	}
	if logger.config.BufferSize != 1000 {
		t.Errorf("expected BufferSize 1000, got %d", logger.config.BufferSize)
	}
	if logger.config.FlushInterval != 5*time.Second {
		t.Errorf("expected FlushInterval 5s, got %v", logger.config.FlushInterval)
	}
	if logger.config.MaxFieldSize != 1024 {
		t.Errorf("expected MaxFieldSize 1024, got %d", logger.config.MaxFieldSize)
	}
}

// =============================================================================
// 12. Detail Types Tests
// =============================================================================

func TestToolInvocationDetails_Marshaling(t *testing.T) {
	details := ToolInvocationDetails{
		ToolName:    "web_search",
		ToolCallID:  "call-123",
		Input:       json.RawMessage(`{"query":"test"}`),
		InputHash:   "abc123",
		Attempt:     1,
		MaxAttempts: 3,
	}

	data, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ToolInvocationDetails
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ToolName != details.ToolName {
		t.Errorf("ToolName mismatch")
	}
	if decoded.Attempt != details.Attempt {
		t.Errorf("Attempt mismatch")
	}
}

func TestToolCompletionDetails_Marshaling(t *testing.T) {
	details := ToolCompletionDetails{
		ToolName:   "web_search",
		ToolCallID: "call-123",
		Success:    true,
		OutputSize: 1024,
		Duration:   250,
	}

	data, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ToolCompletionDetails
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Success != details.Success {
		t.Errorf("Success mismatch")
	}
	if decoded.OutputSize != details.OutputSize {
		t.Errorf("OutputSize mismatch")
	}
}

func TestPermissionDetails_Marshaling(t *testing.T) {
	details := PermissionDetails{
		Permission:    "file_read",
		Resource:      "/tmp/test",
		Action:        "read",
		GrantedBy:     "admin",
		DeniedReason:  "",
		PolicyMatched: "allow_tmp",
		Scopes:        []string{"read", "list"},
	}

	data, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded PermissionDetails
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Permission != details.Permission {
		t.Errorf("Permission mismatch")
	}
	if len(decoded.Scopes) != len(details.Scopes) {
		t.Errorf("Scopes mismatch")
	}
}

func TestAgentHandoffDetails_Marshaling(t *testing.T) {
	details := AgentHandoffDetails{
		FromAgentID:  "agent-1",
		ToAgentID:    "agent-2",
		Reason:       "specialization",
		ContextMode:  "full",
		HandoffDepth: 2,
	}

	data, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded AgentHandoffDetails
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.FromAgentID != details.FromAgentID {
		t.Errorf("FromAgentID mismatch")
	}
	if decoded.HandoffDepth != details.HandoffDepth {
		t.Errorf("HandoffDepth mismatch")
	}
}

func TestSessionCompactDetails_Marshaling(t *testing.T) {
	details := SessionCompactDetails{
		MessagesBeforeCompact: 100,
		MessagesAfterCompact:  50,
		TokensSaved:           5000,
		CompactionStrategy:    "sliding_window",
	}

	data, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SessionCompactDetails
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.MessagesBeforeCompact != details.MessagesBeforeCompact {
		t.Errorf("MessagesBeforeCompact mismatch")
	}
	if decoded.TokensSaved != details.TokensSaved {
		t.Errorf("TokensSaved mismatch")
	}
}

// =============================================================================
// 13. Config Marshaling Tests
// =============================================================================

func TestConfig_Marshaling(t *testing.T) {
	cfg := Config{
		Enabled:               true,
		Level:                 LevelWarn,
		Format:                FormatText,
		Output:                "file:/var/log/audit.log",
		IncludeToolInput:      true,
		IncludeToolOutput:     true,
		IncludeMessageContent: false,
		MaxFieldSize:          2048,
		EventTypes:            []EventType{EventToolInvocation, EventToolCompletion},
		SampleRate:            0.5,
		BufferSize:            500,
		FlushInterval:         10 * time.Second,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if decoded.Enabled != cfg.Enabled {
		t.Errorf("Enabled mismatch")
	}
	if decoded.Level != cfg.Level {
		t.Errorf("Level mismatch")
	}
	if decoded.Format != cfg.Format {
		t.Errorf("Format mismatch")
	}
	if decoded.MaxFieldSize != cfg.MaxFieldSize {
		t.Errorf("MaxFieldSize mismatch")
	}
	if decoded.SampleRate != cfg.SampleRate {
		t.Errorf("SampleRate mismatch")
	}
	if len(decoded.EventTypes) != len(cfg.EventTypes) {
		t.Errorf("EventTypes length mismatch")
	}
}
