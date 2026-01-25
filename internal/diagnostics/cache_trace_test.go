package diagnostics

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// memoryWriter implements CacheTraceWriter for testing
type memoryWriter struct {
	lines    []string
	filePath string
	mu       sync.Mutex
}

func newMemoryWriter(filePath string) *memoryWriter {
	return &memoryWriter{
		filePath: filePath,
		lines:    make([]string, 0),
	}
}

func (w *memoryWriter) Write(line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, line)
	return nil
}

func (w *memoryWriter) FilePath() string {
	return w.filePath
}

func (w *memoryWriter) Lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.lines...)
}

func TestNewCacheTrace_DisabledReturnsNil(t *testing.T) {
	trace := NewCacheTrace(CacheTraceConfig{
		Enabled: false,
	}, CacheTraceParams{})

	if trace != nil {
		t.Error("expected nil trace when disabled")
	}
}

func TestNewCacheTrace_EnabledReturnsTrace(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled: true,
	}, CacheTraceParams{
		RunID:     "run-123",
		SessionID: "sess-456",
	}, writer)

	if trace == nil {
		t.Fatal("expected non-nil trace when enabled")
	}

	if !trace.Enabled() {
		t.Error("expected trace.Enabled() to be true")
	}
}

func TestRecordStage_BasicEvent(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled:         true,
		IncludeMessages: true,
		IncludePrompt:   true,
		IncludeSystem:   true,
	}, CacheTraceParams{
		RunID:        "run-123",
		SessionID:    "sess-456",
		SessionKey:   "key-789",
		Provider:     "anthropic",
		ModelID:      "claude-3-opus",
		WorkspaceDir: "/home/user/workspace",
	}, writer)

	trace.RecordStage(StageSessionLoaded, &CacheTraceEventPayload{
		Note: "session loaded successfully",
	})

	lines := writer.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.Stage != StageSessionLoaded {
		t.Errorf("expected stage %s, got %s", StageSessionLoaded, event.Stage)
	}
	if event.RunID != "run-123" {
		t.Errorf("expected runId run-123, got %s", event.RunID)
	}
	if event.SessionID != "sess-456" {
		t.Errorf("expected sessionId sess-456, got %s", event.SessionID)
	}
	if event.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", event.Sequence)
	}
	if event.Note != "session loaded successfully" {
		t.Errorf("expected note, got %s", event.Note)
	}
}

func TestRecordStage_WithMessages(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled:         true,
		IncludeMessages: true,
	}, CacheTraceParams{}, writer)

	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "Hello"},
		map[string]interface{}{"role": "assistant", "content": "Hi there!"},
	}

	trace.RecordStage(StageStreamContext, &CacheTraceEventPayload{
		Messages: messages,
	})

	lines := writer.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.MessageCount != 2 {
		t.Errorf("expected messageCount 2, got %d", event.MessageCount)
	}
	if len(event.MessageRoles) != 2 {
		t.Errorf("expected 2 message roles, got %d", len(event.MessageRoles))
	}
	if event.MessageRoles[0] != "user" {
		t.Errorf("expected first role 'user', got %s", event.MessageRoles[0])
	}
	if event.MessageRoles[1] != "assistant" {
		t.Errorf("expected second role 'assistant', got %s", event.MessageRoles[1])
	}
	if len(event.MessageFingerprints) != 2 {
		t.Errorf("expected 2 fingerprints, got %d", len(event.MessageFingerprints))
	}
	if event.MessagesDigest == "" {
		t.Error("expected non-empty messagesDigest")
	}
	if event.Messages == nil {
		t.Error("expected messages to be included")
	}
}

func TestRecordStage_MessagesExcluded(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled:         true,
		IncludeMessages: false,
	}, CacheTraceParams{}, writer)

	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "Hello"},
	}

	trace.RecordStage(StageStreamContext, &CacheTraceEventPayload{
		Messages: messages,
	})

	lines := writer.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	// Should have metadata but not full messages
	if event.MessageCount != 1 {
		t.Errorf("expected messageCount 1, got %d", event.MessageCount)
	}
	if event.Messages != nil {
		t.Error("expected messages to be excluded")
	}
}

func TestRecordStage_PromptAndSystem(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled:       true,
		IncludePrompt: true,
		IncludeSystem: true,
	}, CacheTraceParams{}, writer)

	trace.RecordStage(StagePromptBefore, &CacheTraceEventPayload{
		Prompt: "What is the weather?",
		System: "You are a helpful assistant.",
	})

	lines := writer.Lines()
	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.Prompt != "What is the weather?" {
		t.Errorf("expected prompt, got %s", event.Prompt)
	}
	if event.System != "You are a helpful assistant." {
		t.Errorf("expected system, got %v", event.System)
	}
	if event.SystemDigest == "" {
		t.Error("expected non-empty systemDigest")
	}
}

func TestRecordStage_PromptAndSystemExcluded(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled:       true,
		IncludePrompt: false,
		IncludeSystem: false,
	}, CacheTraceParams{}, writer)

	trace.RecordStage(StagePromptBefore, &CacheTraceEventPayload{
		Prompt: "What is the weather?",
		System: "You are a helpful assistant.",
	})

	lines := writer.Lines()
	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.Prompt != "" {
		t.Error("expected prompt to be excluded")
	}
	if event.System != nil {
		t.Error("expected system to be excluded")
	}
}

func TestRecordStage_EmptyPromptAndSystem(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled:       true,
		IncludePrompt: true,
		IncludeSystem: true,
	}, CacheTraceParams{}, writer)

	trace.RecordStage(StagePromptBefore, &CacheTraceEventPayload{
		Prompt: "",
		System: "",
	})

	lines := writer.Lines()
	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	// Empty strings should not be included (omitempty)
	if event.Prompt != "" {
		t.Errorf("expected empty prompt, got %s", event.Prompt)
	}
}

func TestRecordStage_SequenceIncrement(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled: true,
	}, CacheTraceParams{}, writer)

	trace.RecordStage(StageSessionLoaded, nil)
	trace.RecordStage(StageSessionSanitized, nil)
	trace.RecordStage(StageSessionLimited, nil)

	lines := writer.Lines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var event CacheTraceEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &event); err != nil {
			t.Fatalf("failed to unmarshal event %d: %v", i, err)
		}
		if event.Sequence != i+1 {
			t.Errorf("expected sequence %d, got %d", i+1, event.Sequence)
		}
	}
}

func TestRecordStage_NilPayload(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled: true,
	}, CacheTraceParams{
		RunID: "test-run",
	}, writer)

	trace.RecordStage(StageSessionLoaded, nil)

	lines := writer.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.RunID != "test-run" {
		t.Errorf("expected runId test-run, got %s", event.RunID)
	}
}

func TestRecordStage_NilTrace(t *testing.T) {
	var trace *CacheTrace = nil
	// Should not panic
	trace.RecordStage(StageSessionLoaded, nil)
}

func TestDigest_Consistency(t *testing.T) {
	value := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	digest1 := Digest(value)
	digest2 := Digest(value)

	if digest1 != digest2 {
		t.Errorf("digest should be consistent: %s != %s", digest1, digest2)
	}

	if len(digest1) != 64 {
		t.Errorf("expected 64 character hex digest, got %d characters", len(digest1))
	}
}

func TestDigest_DifferentValues(t *testing.T) {
	value1 := map[string]interface{}{"key": "value1"}
	value2 := map[string]interface{}{"key": "value2"}

	digest1 := Digest(value1)
	digest2 := Digest(value2)

	if digest1 == digest2 {
		t.Error("different values should produce different digests")
	}
}

func TestDigest_KeyOrdering(t *testing.T) {
	// Different insertion order should produce same digest
	value1 := map[string]interface{}{
		"a": 1,
		"b": 2,
		"c": 3,
	}
	value2 := map[string]interface{}{
		"c": 3,
		"a": 1,
		"b": 2,
	}

	digest1 := Digest(value1)
	digest2 := Digest(value2)

	if digest1 != digest2 {
		t.Errorf("same values with different key order should produce same digest: %s != %s", digest1, digest2)
	}
}

func TestSummarizeMessages(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "You are helpful."},
		map[string]interface{}{"role": "user", "content": "Hello!"},
		map[string]interface{}{"role": "assistant", "content": "Hi there!"},
	}

	count, roles, fingerprints, digest := SummarizeMessages(messages)

	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
	if len(roles) != 3 {
		t.Errorf("expected 3 roles, got %d", len(roles))
	}
	if roles[0] != "system" || roles[1] != "user" || roles[2] != "assistant" {
		t.Errorf("unexpected roles: %v", roles)
	}
	if len(fingerprints) != 3 {
		t.Errorf("expected 3 fingerprints, got %d", len(fingerprints))
	}
	if digest == "" {
		t.Error("expected non-empty digest")
	}
}

func TestSummarizeMessages_Empty(t *testing.T) {
	count, roles, fingerprints, digest := SummarizeMessages([]interface{}{})

	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
	if len(roles) != 0 {
		t.Errorf("expected 0 roles, got %d", len(roles))
	}
	if len(fingerprints) != 0 {
		t.Errorf("expected 0 fingerprints, got %d", len(fingerprints))
	}
	if digest == "" {
		t.Error("expected non-empty digest (hash of empty string)")
	}
}

func TestResolveCacheTraceConfig_Defaults(t *testing.T) {
	cfg := ResolveCacheTraceConfig(map[string]string{}, false, "")

	if cfg.Enabled {
		t.Error("expected disabled by default")
	}
	if !cfg.IncludeMessages {
		t.Error("expected IncludeMessages true by default")
	}
	if !cfg.IncludePrompt {
		t.Error("expected IncludePrompt true by default")
	}
	if !cfg.IncludeSystem {
		t.Error("expected IncludeSystem true by default")
	}
}

func TestResolveCacheTraceConfig_EnvOverrides(t *testing.T) {
	env := map[string]string{
		"NEXUS_CACHE_TRACE":          "1",
		"NEXUS_CACHE_TRACE_FILE":     "/custom/path/trace.jsonl",
		"NEXUS_CACHE_TRACE_MESSAGES": "0",
		"NEXUS_CACHE_TRACE_PROMPT":   "false",
		"NEXUS_CACHE_TRACE_SYSTEM":   "no",
	}

	cfg := ResolveCacheTraceConfig(env, false, "")

	if !cfg.Enabled {
		t.Error("expected enabled from env")
	}
	if cfg.FilePath != "/custom/path/trace.jsonl" {
		t.Errorf("expected custom file path, got %s", cfg.FilePath)
	}
	if cfg.IncludeMessages {
		t.Error("expected IncludeMessages false from env")
	}
	if cfg.IncludePrompt {
		t.Error("expected IncludePrompt false from env")
	}
	if cfg.IncludeSystem {
		t.Error("expected IncludeSystem false from env")
	}
}

func TestResolveCacheTraceConfig_EnvDisablesConfig(t *testing.T) {
	env := map[string]string{
		"NEXUS_CACHE_TRACE": "0",
	}

	cfg := ResolveCacheTraceConfig(env, true, "/config/path.jsonl")

	if cfg.Enabled {
		t.Error("expected env to disable tracing")
	}
}

func TestResolveCacheTraceConfig_HomeExpansion(t *testing.T) {
	env := map[string]string{
		"NEXUS_CACHE_TRACE":      "1",
		"NEXUS_CACHE_TRACE_FILE": "~/logs/trace.jsonl",
	}

	cfg := ResolveCacheTraceConfig(env, false, "")

	if strings.HasPrefix(cfg.FilePath, "~/") {
		t.Errorf("expected ~ to be expanded, got %s", cfg.FilePath)
	}
	if !strings.HasSuffix(cfg.FilePath, "logs/trace.jsonl") {
		t.Errorf("expected path to end with logs/trace.jsonl, got %s", cfg.FilePath)
	}
}

func TestResolveCacheTraceConfig_DefaultPath(t *testing.T) {
	env := map[string]string{
		"NEXUS_CACHE_TRACE": "1",
	}

	cfg := ResolveCacheTraceConfig(env, false, "")

	if cfg.FilePath == "" {
		t.Error("expected default file path to be set")
	}
	if !strings.HasSuffix(cfg.FilePath, "cache-trace.jsonl") {
		t.Errorf("expected path to end with cache-trace.jsonl, got %s", cfg.FilePath)
	}
}

func TestParseBooleanValue(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"t", true},
		{"T", true},
		{"yes", true},
		{"YES", true},
		{"y", true},
		{"Y", true},
		{"on", true},
		{"ON", true},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"False", false},
		{"f", false},
		{"F", false},
		{"no", false},
		{"NO", false},
		{"n", false},
		{"N", false},
		{"off", false},
		{"OFF", false},
		{"", false},
		{"invalid", false},
		{"  1  ", true},
		{"  true  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseBooleanValue(tt.input)
			if result != tt.expected {
				t.Errorf("parseBooleanValue(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStableStringify(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		contains string
	}{
		{"nil", nil, "null"},
		{"string", "hello", `"hello"`},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool_true", true, "true"},
		{"bool_false", false, "false"},
		{"array", []interface{}{"a", "b"}, `["a","b"]`},
		{"object", map[string]interface{}{"key": "value"}, `{"key":"value"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stableStringify(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("stableStringify(%v) = %s, want contains %s", tt.input, result, tt.contains)
			}
		})
	}
}

func TestCacheTrace_ConcurrentWrites(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled: true,
	}, CacheTraceParams{
		RunID: "concurrent-test",
	}, writer)

	var wg sync.WaitGroup
	numWriters := 10
	numWrites := 100

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numWrites; j++ {
				trace.RecordStage(StageSessionLoaded, &CacheTraceEventPayload{
					Note: "concurrent write",
				})
			}
		}()
	}

	wg.Wait()

	lines := writer.Lines()
	expectedLines := numWriters * numWrites
	if len(lines) != expectedLines {
		t.Errorf("expected %d lines, got %d", expectedLines, len(lines))
	}

	// Verify all sequences are unique
	sequences := make(map[int]bool)
	for i, line := range lines {
		var event CacheTraceEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &event); err != nil {
			t.Fatalf("failed to unmarshal event %d: %v", i, err)
		}
		if sequences[event.Sequence] {
			t.Errorf("duplicate sequence number: %d", event.Sequence)
		}
		sequences[event.Sequence] = true
	}
}

func TestRecordStage_WithModelAndOptions(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled: true,
	}, CacheTraceParams{}, writer)

	trace.RecordStage(StageStreamContext, &CacheTraceEventPayload{
		Model: map[string]interface{}{
			"id":       "claude-3-opus",
			"provider": "anthropic",
		},
		Options: map[string]interface{}{
			"temperature": 0.7,
			"maxTokens":   1024,
		},
	})

	lines := writer.Lines()
	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.Model == nil {
		t.Error("expected model to be present")
	}
	if event.Model["id"] != "claude-3-opus" {
		t.Errorf("expected model id, got %v", event.Model["id"])
	}
	if event.Options == nil {
		t.Error("expected options to be present")
	}
	if event.Options["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", event.Options["temperature"])
	}
}

func TestRecordStage_WithError(t *testing.T) {
	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled: true,
	}, CacheTraceParams{}, writer)

	trace.RecordStage(StageStreamContext, &CacheTraceEventPayload{
		Error: "rate limit exceeded",
	})

	lines := writer.Lines()
	var event CacheTraceEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[0])), &event); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if event.Error != "rate limit exceeded" {
		t.Errorf("expected error message, got %s", event.Error)
	}
}

func TestCacheTrace_FilePath(t *testing.T) {
	writer := newMemoryWriter("/test/path/trace.jsonl")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled:  true,
		FilePath: "/test/path/trace.jsonl",
	}, CacheTraceParams{}, writer)

	if trace.FilePath() != "/test/path/trace.jsonl" {
		t.Errorf("expected file path /test/path/trace.jsonl, got %s", trace.FilePath())
	}
}

func TestCacheTrace_NilFilePath(t *testing.T) {
	var trace *CacheTrace = nil
	if trace.FilePath() != "" {
		t.Error("expected empty file path for nil trace")
	}
}

func TestAllStages(t *testing.T) {
	stages := []CacheTraceStage{
		StageSessionLoaded,
		StageSessionSanitized,
		StageSessionLimited,
		StagePromptBefore,
		StagePromptImages,
		StageStreamContext,
		StageSessionAfter,
	}

	writer := newMemoryWriter("memory")
	trace := NewCacheTraceWithWriter(CacheTraceConfig{
		Enabled: true,
	}, CacheTraceParams{}, writer)

	for _, stage := range stages {
		trace.RecordStage(stage, nil)
	}

	lines := writer.Lines()
	if len(lines) != len(stages) {
		t.Fatalf("expected %d lines, got %d", len(stages), len(lines))
	}

	for i, line := range lines {
		var event CacheTraceEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &event); err != nil {
			t.Fatalf("failed to unmarshal event %d: %v", i, err)
		}
		if event.Stage != stages[i] {
			t.Errorf("expected stage %s, got %s", stages[i], event.Stage)
		}
	}
}
