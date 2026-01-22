package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

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

// nopWriteCloser wraps an io.Writer to implement io.WriteCloser
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }
