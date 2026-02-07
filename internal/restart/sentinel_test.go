package restart

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveSentinelPath(t *testing.T) {
	path := ResolveSentinelPath("/tmp/state")
	expected := filepath.Join("/tmp/state", SentinelFilename)
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestWriteAndReadRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()

	msg := "test message"
	hint := "run doctor"
	reason := "config changed"
	cwd := "/home/user"
	stdout := "output text"
	exitCode := 0
	var durationMs int64 = 1234

	payload := SentinelPayload{
		Kind:       KindConfigApply,
		Status:     StatusOK,
		Ts:         time.Now().UnixMilli(),
		SessionKey: "session-123",
		DeliveryContext: &DeliveryContext{
			Channel:   "slack",
			To:        "#general",
			AccountID: "acc-456",
		},
		ThreadID:   "thread-789",
		Message:    &msg,
		DoctorHint: &hint,
		Stats: &SentinelStats{
			Mode:       "full",
			Root:       "/srv/app",
			Before:     map[string]interface{}{"version": "1.0"},
			After:      map[string]interface{}{"version": "1.1"},
			Reason:     &reason,
			DurationMs: &durationMs,
			Steps: []SentinelStep{
				{
					Name:       "build",
					Command:    "make build",
					Cwd:        &cwd,
					DurationMs: &durationMs,
					Log: &SentinelLog{
						StdoutTail: &stdout,
						ExitCode:   &exitCode,
					},
				},
			},
		},
	}

	err := WriteSentinel(tmpDir, payload)
	if err != nil {
		t.Fatalf("WriteSentinel failed: %v", err)
	}

	// Verify file exists
	sentinelPath := ResolveSentinelPath(tmpDir)
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Fatal("sentinel file was not created")
	}

	// Read it back
	sentinel, err := ReadSentinel(tmpDir)
	if err != nil {
		t.Fatalf("ReadSentinel failed: %v", err)
	}
	if sentinel == nil {
		t.Fatal("ReadSentinel returned nil")
	}

	if sentinel.Version != 1 {
		t.Errorf("expected version 1, got %d", sentinel.Version)
	}
	if sentinel.Payload.Kind != KindConfigApply {
		t.Errorf("expected kind %s, got %s", KindConfigApply, sentinel.Payload.Kind)
	}
	if sentinel.Payload.Status != StatusOK {
		t.Errorf("expected status %s, got %s", StatusOK, sentinel.Payload.Status)
	}
	if sentinel.Payload.SessionKey != "session-123" {
		t.Errorf("expected sessionKey session-123, got %s", sentinel.Payload.SessionKey)
	}
	if sentinel.Payload.DeliveryContext == nil {
		t.Fatal("expected deliveryContext to be set")
	}
	if sentinel.Payload.DeliveryContext.Channel != "slack" {
		t.Errorf("expected channel slack, got %s", sentinel.Payload.DeliveryContext.Channel)
	}
	if sentinel.Payload.ThreadID != "thread-789" {
		t.Errorf("expected threadId thread-789, got %s", sentinel.Payload.ThreadID)
	}
	if sentinel.Payload.Message == nil || *sentinel.Payload.Message != "test message" {
		t.Error("expected message to match")
	}
	if sentinel.Payload.Stats == nil {
		t.Fatal("expected stats to be set")
	}
	if sentinel.Payload.Stats.Mode != "full" {
		t.Errorf("expected mode full, got %s", sentinel.Payload.Stats.Mode)
	}
	if len(sentinel.Payload.Stats.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(sentinel.Payload.Stats.Steps))
	}
	if sentinel.Payload.Stats.Steps[0].Name != "build" {
		t.Errorf("expected step name build, got %s", sentinel.Payload.Stats.Steps[0].Name)
	}
}

func TestConsumeSentinelDeletesFile(t *testing.T) {
	tmpDir := t.TempDir()

	payload := SentinelPayload{
		Kind:   KindRestart,
		Status: StatusOK,
		Ts:     time.Now().UnixMilli(),
	}

	err := WriteSentinel(tmpDir, payload)
	if err != nil {
		t.Fatalf("WriteSentinel failed: %v", err)
	}

	sentinelPath := ResolveSentinelPath(tmpDir)

	// Verify file exists before consume
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Fatal("sentinel file should exist before consume")
	}

	sentinel, err := ConsumeSentinel(tmpDir)
	if err != nil {
		t.Fatalf("ConsumeSentinel failed: %v", err)
	}
	if sentinel == nil {
		t.Fatal("ConsumeSentinel returned nil")
	}
	if sentinel.Payload.Kind != KindRestart {
		t.Errorf("expected kind %s, got %s", KindRestart, sentinel.Payload.Kind)
	}

	// Verify file is deleted after consume
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Fatal("sentinel file should be deleted after consume")
	}

	// Second consume should return nil
	sentinel2, err := ConsumeSentinel(tmpDir)
	if err != nil {
		t.Fatalf("second ConsumeSentinel failed: %v", err)
	}
	if sentinel2 != nil {
		t.Fatal("second ConsumeSentinel should return nil")
	}
}

func TestReadSentinelMissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	sentinel, err := ReadSentinel(tmpDir)
	if err != nil {
		t.Fatalf("ReadSentinel with missing file should not error: %v", err)
	}
	if sentinel != nil {
		t.Fatal("ReadSentinel with missing file should return nil")
	}
}

func TestReadSentinelInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sentinelPath := ResolveSentinelPath(tmpDir)

	// Write invalid JSON
	err := os.WriteFile(sentinelPath, []byte("not valid json {{{"), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sentinel, err := ReadSentinel(tmpDir)
	if err != nil {
		t.Fatalf("ReadSentinel with invalid JSON should not error: %v", err)
	}
	if sentinel != nil {
		t.Fatal("ReadSentinel with invalid JSON should return nil")
	}

	// Verify file was deleted
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Fatal("invalid sentinel file should be deleted")
	}
}

func TestReadSentinelInvalidVersion(t *testing.T) {
	tmpDir := t.TempDir()
	sentinelPath := ResolveSentinelPath(tmpDir)

	// Write JSON with wrong version
	badSentinel := map[string]interface{}{
		"version": 99,
		"payload": map[string]interface{}{
			"kind":   "restart",
			"status": "ok",
			"ts":     12345,
		},
	}
	data, _ := json.Marshal(badSentinel)
	err := os.WriteFile(sentinelPath, data, 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sentinel, err := ReadSentinel(tmpDir)
	if err != nil {
		t.Fatalf("ReadSentinel with invalid version should not error: %v", err)
	}
	if sentinel != nil {
		t.Fatal("ReadSentinel with invalid version should return nil")
	}

	// Verify file was deleted
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Fatal("invalid sentinel file should be deleted")
	}
}

func TestReadSentinelMissingPayload(t *testing.T) {
	tmpDir := t.TempDir()
	sentinelPath := ResolveSentinelPath(tmpDir)

	// Write JSON with version 1 but missing required payload fields
	badSentinel := map[string]interface{}{
		"version": 1,
	}
	data, _ := json.Marshal(badSentinel)
	err := os.WriteFile(sentinelPath, data, 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// This should still parse since Go will use zero values
	sentinel, err := ReadSentinel(tmpDir)
	if err != nil {
		t.Fatalf("ReadSentinel failed: %v", err)
	}
	// With empty payload, it should still return the sentinel (Go zero values)
	if sentinel == nil {
		t.Fatal("ReadSentinel should return sentinel with empty payload")
	}
}

func TestFormatMessage(t *testing.T) {
	payload := SentinelPayload{
		Kind:   KindUpdate,
		Status: StatusError,
		Ts:     1234567890,
	}

	msg := FormatMessage(payload)
	if !strings.HasPrefix(msg, "GatewayRestart:\n") {
		t.Error("FormatMessage should start with 'GatewayRestart:'")
	}
	if !strings.Contains(msg, `"kind": "update"`) {
		t.Error("FormatMessage should contain kind")
	}
	if !strings.Contains(msg, `"status": "error"`) {
		t.Error("FormatMessage should contain status")
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name     string
		payload  SentinelPayload
		expected string
	}{
		{
			name: "basic restart ok",
			payload: SentinelPayload{
				Kind:   KindRestart,
				Status: StatusOK,
			},
			expected: "Gateway restart restart ok",
		},
		{
			name: "config-apply error",
			payload: SentinelPayload{
				Kind:   KindConfigApply,
				Status: StatusError,
			},
			expected: "Gateway restart config-apply error",
		},
		{
			name: "update skipped with mode",
			payload: SentinelPayload{
				Kind:   KindUpdate,
				Status: StatusSkipped,
				Stats: &SentinelStats{
					Mode: "incremental",
				},
			},
			expected: "Gateway restart update skipped (incremental)",
		},
		{
			name: "with empty mode",
			payload: SentinelPayload{
				Kind:   KindRestart,
				Status: StatusOK,
				Stats: &SentinelStats{
					Mode: "",
				},
			},
			expected: "Gateway restart restart ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Summarize(tt.payload)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTrimLogTail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxChars int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			maxChars: 100,
			expected: "",
		},
		{
			name:     "string shorter than max",
			input:    "hello world",
			maxChars: 100,
			expected: "hello world",
		},
		{
			name:     "string equal to max",
			input:    "hello",
			maxChars: 5,
			expected: "hello",
		},
		{
			name:     "string longer than max",
			input:    "hello world",
			maxChars: 5,
			expected: "...world",
		},
		{
			name:     "trailing whitespace trimmed",
			input:    "hello world  \n\t",
			maxChars: 100,
			expected: "hello world",
		},
		{
			name:     "trailing whitespace trimmed then truncated",
			input:    "abcdefghij  \n",
			maxChars: 5,
			expected: "...fghij",
		},
		{
			name:     "max chars of 1",
			input:    "hello",
			maxChars: 1,
			expected: "...o",
		},
		{
			name:     "multiline content",
			input:    "line1\nline2\nline3",
			maxChars: 10,
			expected: "...ine2\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TrimLogTail(tt.input, tt.maxChars)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestWriteSentinelCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "state", "dir")

	payload := SentinelPayload{
		Kind:   KindRestart,
		Status: StatusOK,
		Ts:     time.Now().UnixMilli(),
	}

	err := WriteSentinel(nestedDir, payload)
	if err != nil {
		t.Fatalf("WriteSentinel failed: %v", err)
	}

	sentinelPath := ResolveSentinelPath(nestedDir)
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Fatal("sentinel file was not created in nested directory")
	}
}

func TestAllKindsAndStatuses(t *testing.T) {
	tmpDir := t.TempDir()

	kinds := []RestartKind{KindConfigApply, KindUpdate, KindRestart}
	statuses := []RestartStatus{StatusOK, StatusError, StatusSkipped}

	for _, kind := range kinds {
		for _, status := range statuses {
			t.Run(string(kind)+"_"+string(status), func(t *testing.T) {
				testDir := filepath.Join(tmpDir, string(kind), string(status))

				payload := SentinelPayload{
					Kind:   kind,
					Status: status,
					Ts:     time.Now().UnixMilli(),
				}

				err := WriteSentinel(testDir, payload)
				if err != nil {
					t.Fatalf("WriteSentinel failed: %v", err)
				}

				sentinel, err := ReadSentinel(testDir)
				if err != nil {
					t.Fatalf("ReadSentinel failed: %v", err)
				}
				if sentinel == nil {
					t.Fatal("ReadSentinel returned nil")
				}
				if sentinel.Payload.Kind != kind {
					t.Errorf("kind mismatch: expected %s, got %s", kind, sentinel.Payload.Kind)
				}
				if sentinel.Payload.Status != status {
					t.Errorf("status mismatch: expected %s, got %s", status, sentinel.Payload.Status)
				}
			})
		}
	}
}

func TestSentinelJSONFormat(t *testing.T) {
	tmpDir := t.TempDir()

	payload := SentinelPayload{
		Kind:   KindRestart,
		Status: StatusOK,
		Ts:     1234567890,
	}

	err := WriteSentinel(tmpDir, payload)
	if err != nil {
		t.Fatalf("WriteSentinel failed: %v", err)
	}

	sentinelPath := ResolveSentinelPath(tmpDir)
	data, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatalf("failed to read sentinel file: %v", err)
	}

	// Verify it ends with newline (matches TypeScript behavior)
	if data[len(data)-1] != '\n' {
		t.Error("sentinel file should end with newline")
	}

	// Verify it's pretty-printed (has indentation)
	if !strings.Contains(string(data), "  ") {
		t.Error("sentinel file should be pretty-printed with indentation")
	}
}

func TestConsumeSentinel_ReadAndDeleteAtomicity(t *testing.T) {
	tmpDir := t.TempDir()

	msg := "atomicity test message"
	hint := "check logs"
	payload := SentinelPayload{
		Kind:       KindConfigApply,
		Status:     StatusError,
		Ts:         time.Now().UnixMilli(),
		SessionKey: "session-atomic",
		DeliveryContext: &DeliveryContext{
			Channel:   "telegram",
			To:        "@user",
			AccountID: "acc-789",
		},
		ThreadID:   "thread-atomic",
		Message:    &msg,
		DoctorHint: &hint,
	}

	err := WriteSentinel(tmpDir, payload)
	if err != nil {
		t.Fatalf("WriteSentinel failed: %v", err)
	}

	sentinelPath := ResolveSentinelPath(tmpDir)

	// Verify file exists before consume
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Fatal("sentinel file should exist before consume")
	}

	// Consume: should return correct data AND delete the file
	sentinel, err := ConsumeSentinel(tmpDir)
	if err != nil {
		t.Fatalf("ConsumeSentinel failed: %v", err)
	}
	if sentinel == nil {
		t.Fatal("ConsumeSentinel returned nil for valid file")
	}

	// Verify all data fields are correct
	if sentinel.Version != 1 {
		t.Errorf("expected version 1, got %d", sentinel.Version)
	}
	if sentinel.Payload.Kind != KindConfigApply {
		t.Errorf("expected kind %s, got %s", KindConfigApply, sentinel.Payload.Kind)
	}
	if sentinel.Payload.Status != StatusError {
		t.Errorf("expected status %s, got %s", StatusError, sentinel.Payload.Status)
	}
	if sentinel.Payload.SessionKey != "session-atomic" {
		t.Errorf("expected sessionKey 'session-atomic', got %s", sentinel.Payload.SessionKey)
	}
	if sentinel.Payload.DeliveryContext == nil {
		t.Fatal("expected deliveryContext to be set")
	}
	if sentinel.Payload.DeliveryContext.Channel != "telegram" {
		t.Errorf("expected channel 'telegram', got %s", sentinel.Payload.DeliveryContext.Channel)
	}
	if sentinel.Payload.DeliveryContext.To != "@user" {
		t.Errorf("expected to '@user', got %s", sentinel.Payload.DeliveryContext.To)
	}
	if sentinel.Payload.ThreadID != "thread-atomic" {
		t.Errorf("expected threadId 'thread-atomic', got %s", sentinel.Payload.ThreadID)
	}
	if sentinel.Payload.Message == nil || *sentinel.Payload.Message != "atomicity test message" {
		t.Error("expected message to be 'atomicity test message'")
	}
	if sentinel.Payload.DoctorHint == nil || *sentinel.Payload.DoctorHint != "check logs" {
		t.Error("expected doctorHint to be 'check logs'")
	}

	// Verify file is deleted after consume
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Fatal("sentinel file should be deleted after ConsumeSentinel")
	}

	// A second consume should return nil without error
	sentinel2, err := ConsumeSentinel(tmpDir)
	if err != nil {
		t.Fatalf("second ConsumeSentinel returned error: %v", err)
	}
	if sentinel2 != nil {
		t.Fatal("second ConsumeSentinel should return nil")
	}
}

func TestTrimLogTail_ExactBoundary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxChars int
		expected string
	}{
		{
			name:     "exact length match - no truncation",
			input:    "abcde",
			maxChars: 5,
			expected: "abcde",
		},
		{
			name:     "one char over boundary",
			input:    "abcdef",
			maxChars: 5,
			expected: "...bcdef",
		},
		{
			name:     "one char under boundary",
			input:    "abcd",
			maxChars: 5,
			expected: "abcd",
		},
		{
			name:     "exact length after whitespace trim",
			input:    "abcde   ",
			maxChars: 5,
			expected: "abcde",
		},
		{
			name:     "one over after whitespace trim",
			input:    "abcdef  ",
			maxChars: 5,
			expected: "...bcdef",
		},
		{
			name:     "maxChars equals zero",
			input:    "abc",
			maxChars: 0,
			expected: "...",
		},
		{
			name:     "single char input exact boundary",
			input:    "x",
			maxChars: 1,
			expected: "x",
		},
		{
			name:     "two char input with maxChars 1",
			input:    "xy",
			maxChars: 1,
			expected: "...y",
		},
		{
			name:     "input is only whitespace trimmed to empty",
			input:    "   \n\t",
			maxChars: 5,
			expected: "",
		},
		{
			name:     "multiline exact boundary after trim",
			input:    "ab\ncd",
			maxChars: 5,
			expected: "ab\ncd",
		},
		{
			name:     "multiline one over boundary after trim",
			input:    "ab\ncde",
			maxChars: 5,
			expected: "...b\ncde",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TrimLogTail(tt.input, tt.maxChars)
			if result != tt.expected {
				t.Errorf("TrimLogTail(%q, %d) = %q, want %q", tt.input, tt.maxChars, result, tt.expected)
			}
		})
	}
}
