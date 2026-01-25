package shell

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewProcessRegistry(t *testing.T) {
	r := NewProcessRegistry(nil)
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.runningSessions == nil {
		t.Error("expected initialized running sessions map")
	}
	if r.finishedSessions == nil {
		t.Error("expected initialized finished sessions map")
	}
	if r.jobTTL != DefaultJobTTL {
		t.Errorf("expected default job TTL %v, got %v", DefaultJobTTL, r.jobTTL)
	}
}

func TestProcessRegistry_AddSession(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{
		ID:        "test-1",
		Command:   "echo hello",
		StartedAt: time.Now(),
		PID:       12345,
	}

	r.AddSession(session)

	if r.RunningCount() != 1 {
		t.Errorf("expected 1 running session, got %d", r.RunningCount())
	}

	got, exists := r.GetSession("test-1")
	if !exists {
		t.Fatal("expected session to exist")
	}
	if got.Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", got.Command)
	}
}

func TestProcessRegistry_GetSession(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	// Non-existent session
	_, exists := r.GetSession("non-existent")
	if exists {
		t.Error("expected non-existent session to return false")
	}

	// Add and retrieve
	session := &ProcessSession{ID: "test-1", Command: "ls"}
	r.AddSession(session)

	got, exists := r.GetSession("test-1")
	if !exists {
		t.Error("expected session to exist")
	}
	if got.ID != "test-1" {
		t.Errorf("expected ID 'test-1', got %q", got.ID)
	}
}

func TestProcessRegistry_DeleteSession(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{ID: "test-1", Command: "ls"}
	r.AddSession(session)

	r.DeleteSession("test-1")

	if r.RunningCount() != 0 {
		t.Errorf("expected 0 running sessions, got %d", r.RunningCount())
	}

	_, exists := r.GetSession("test-1")
	if exists {
		t.Error("expected session to be deleted")
	}
}

func TestProcessRegistry_IsSessionIDTaken(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	if r.IsSessionIDTaken("test-1") {
		t.Error("expected ID not to be taken initially")
	}

	session := &ProcessSession{ID: "test-1", Command: "ls"}
	r.AddSession(session)

	if !r.IsSessionIDTaken("test-1") {
		t.Error("expected ID to be taken after adding session")
	}
}

func TestProcessRegistry_AppendOutput(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{
		ID:             "test-1",
		MaxOutputChars: 1000,
	}
	r.AddSession(session)

	r.AppendOutput(session, "stdout", "hello ")
	r.AppendOutput(session, "stdout", "world")
	r.AppendOutput(session, "stderr", "error!")

	if session.TotalOutputChars != 17 {
		t.Errorf("expected 17 total chars, got %d", session.TotalOutputChars)
	}

	if session.Aggregated != "hello worlderror!" {
		t.Errorf("expected aggregated 'hello worlderror!', got %q", session.Aggregated)
	}
}

func TestProcessRegistry_AppendOutput_Truncation(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{
		ID:             "test-1",
		MaxOutputChars: 20,
	}
	r.AddSession(session)

	// Add more than max output chars
	r.AppendOutput(session, "stdout", strings.Repeat("a", 15))
	r.AppendOutput(session, "stdout", strings.Repeat("b", 15))

	if !session.Truncated {
		t.Error("expected session to be marked as truncated")
	}

	if len(session.Aggregated) > 20 {
		t.Errorf("expected aggregated to be <= 20 chars, got %d", len(session.Aggregated))
	}

	// Should keep the tail (end of output)
	if !strings.HasSuffix(session.Aggregated, "bbbbb") {
		t.Errorf("expected aggregated to end with 'bbbbb', got %q", session.Aggregated)
	}
}

func TestProcessRegistry_DrainSession(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{
		ID:             "test-1",
		MaxOutputChars: 1000,
	}
	r.AddSession(session)

	r.AppendOutput(session, "stdout", "out1")
	r.AppendOutput(session, "stdout", "out2")
	r.AppendOutput(session, "stderr", "err1")

	stdout, stderr := r.DrainSession(session)

	if stdout != "out1out2" {
		t.Errorf("expected stdout 'out1out2', got %q", stdout)
	}
	if stderr != "err1" {
		t.Errorf("expected stderr 'err1', got %q", stderr)
	}

	// Drain again should return empty
	stdout2, stderr2 := r.DrainSession(session)
	if stdout2 != "" || stderr2 != "" {
		t.Errorf("expected empty after drain, got stdout=%q stderr=%q", stdout2, stderr2)
	}
}

func TestProcessRegistry_MarkExited(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{
		ID:           "test-1",
		Command:      "ls",
		Backgrounded: true,
		Aggregated:   "output",
	}
	r.AddSession(session)

	exitCode := 0
	r.MarkExited(session, &exitCode, "", ProcessStatusCompleted)

	if !session.Exited {
		t.Error("expected session to be marked as exited")
	}
	if session.ExitCode == nil || *session.ExitCode != 0 {
		t.Error("expected exit code 0")
	}

	// Should be moved to finished
	if r.RunningCount() != 0 {
		t.Errorf("expected 0 running, got %d", r.RunningCount())
	}
	if r.FinishedCount() != 1 {
		t.Errorf("expected 1 finished, got %d", r.FinishedCount())
	}

	finished, exists := r.GetFinishedSession("test-1")
	if !exists {
		t.Fatal("expected finished session to exist")
	}
	if finished.Status != ProcessStatusCompleted {
		t.Errorf("expected status completed, got %s", finished.Status)
	}
}

func TestProcessRegistry_MarkExited_NotBackgrounded(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{
		ID:           "test-1",
		Backgrounded: false,
	}
	r.AddSession(session)

	exitCode := 0
	r.MarkExited(session, &exitCode, "", ProcessStatusCompleted)

	// Should be removed but NOT added to finished
	if r.RunningCount() != 0 {
		t.Errorf("expected 0 running, got %d", r.RunningCount())
	}
	if r.FinishedCount() != 0 {
		t.Errorf("expected 0 finished (not backgrounded), got %d", r.FinishedCount())
	}
}

func TestProcessRegistry_MarkBackgrounded(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{ID: "test-1"}
	r.AddSession(session)

	if session.Backgrounded {
		t.Error("expected session not to be backgrounded initially")
	}

	r.MarkBackgrounded(session)

	if !session.Backgrounded {
		t.Error("expected session to be backgrounded after MarkBackgrounded")
	}
}

func TestProcessRegistry_ListRunningSessions(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	// Add some sessions, only some backgrounded
	s1 := &ProcessSession{ID: "test-1", Backgrounded: true}
	s2 := &ProcessSession{ID: "test-2", Backgrounded: false}
	s3 := &ProcessSession{ID: "test-3", Backgrounded: true}

	r.AddSession(s1)
	r.AddSession(s2)
	r.AddSession(s3)

	running := r.ListRunningSessions()

	if len(running) != 2 {
		t.Errorf("expected 2 backgrounded sessions, got %d", len(running))
	}

	// Check IDs
	ids := make(map[string]bool)
	for _, s := range running {
		ids[s.ID] = true
	}
	if !ids["test-1"] || !ids["test-3"] {
		t.Error("expected test-1 and test-3 in running list")
	}
}

func TestProcessRegistry_ListFinishedSessions(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	s1 := &ProcessSession{ID: "test-1", Backgrounded: true}
	s2 := &ProcessSession{ID: "test-2", Backgrounded: true}
	r.AddSession(s1)
	r.AddSession(s2)

	exitCode := 0
	r.MarkExited(s1, &exitCode, "", ProcessStatusCompleted)

	exitCode2 := 1
	r.MarkExited(s2, &exitCode2, "", ProcessStatusFailed)

	finished := r.ListFinishedSessions()
	if len(finished) != 2 {
		t.Errorf("expected 2 finished sessions, got %d", len(finished))
	}
}

func TestProcessRegistry_ClearFinished(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	s := &ProcessSession{ID: "test-1", Backgrounded: true}
	r.AddSession(s)

	exitCode := 0
	r.MarkExited(s, &exitCode, "", ProcessStatusCompleted)

	if r.FinishedCount() != 1 {
		t.Fatalf("expected 1 finished, got %d", r.FinishedCount())
	}

	r.ClearFinished()

	if r.FinishedCount() != 0 {
		t.Errorf("expected 0 finished after clear, got %d", r.FinishedCount())
	}
}

func TestProcessRegistry_Reset(t *testing.T) {
	r := NewProcessRegistry(nil)

	s1 := &ProcessSession{ID: "test-1", Backgrounded: true}
	r.AddSession(s1)

	exitCode := 0
	r.MarkExited(s1, &exitCode, "", ProcessStatusCompleted)

	s2 := &ProcessSession{ID: "test-2"}
	r.AddSession(s2)

	r.Reset()

	if r.RunningCount() != 0 {
		t.Errorf("expected 0 running after reset, got %d", r.RunningCount())
	}
	if r.FinishedCount() != 0 {
		t.Errorf("expected 0 finished after reset, got %d", r.FinishedCount())
	}
}

func TestProcessRegistry_ConcurrentAccess(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent adds
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%26))
			s := &ProcessSession{ID: id, Backgrounded: true}
			r.AddSession(s)
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = r.ListRunningSessions()
			_ = r.ListFinishedSessions()
			_ = r.RunningCount()
		}()
	}
	wg.Wait()

	// Concurrent deletes
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%26))
			r.DeleteSession(id)
		}(i)
	}
	wg.Wait()
}

func TestProcessRegistry_TTLSweeper(t *testing.T) {
	r := NewProcessRegistry(nil)
	// Use very short TTL for testing
	r.jobTTL = 100 * time.Millisecond

	// Add and finish a session
	s := &ProcessSession{ID: "test-1", Backgrounded: true}
	r.AddSession(s)

	exitCode := 0
	r.MarkExited(s, &exitCode, "", ProcessStatusCompleted)

	if r.FinishedCount() != 1 {
		t.Fatalf("expected 1 finished session")
	}

	// Manually call prune (simulating what sweeper does)
	time.Sleep(150 * time.Millisecond)
	r.pruneFinishedSessions()

	if r.FinishedCount() != 0 {
		t.Errorf("expected session to be pruned, got %d finished", r.FinishedCount())
	}

	r.StopSweeper()
}

func TestProcessRegistry_SetJobTTL(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	// Test clamping
	r.SetJobTTL(30 * time.Second) // Below min
	if r.GetJobTTL() != MinJobTTL {
		t.Errorf("expected TTL to be clamped to %v, got %v", MinJobTTL, r.GetJobTTL())
	}

	r.SetJobTTL(5 * time.Hour) // Above max
	if r.GetJobTTL() != MaxJobTTL {
		t.Errorf("expected TTL to be clamped to %v, got %v", MaxJobTTL, r.GetJobTTL())
	}

	r.SetJobTTL(1 * time.Hour) // Within bounds
	if r.GetJobTTL() != 1*time.Hour {
		t.Errorf("expected TTL 1h, got %v", r.GetJobTTL())
	}
}

func TestClampTTL(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected time.Duration
	}{
		{30 * time.Second, MinJobTTL},
		{5 * time.Hour, MaxJobTTL},
		{1 * time.Hour, 1 * time.Hour},
		{MinJobTTL, MinJobTTL},
		{MaxJobTTL, MaxJobTTL},
	}

	for _, tt := range tests {
		got := ClampTTL(tt.input)
		if got != tt.expected {
			t.Errorf("ClampTTL(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestTail(t *testing.T) {
	tests := []struct {
		text     string
		n        int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "world"},
		{"", 5, ""},
		{"abcdefghij", 3, "hij"},
	}

	for _, tt := range tests {
		got := Tail(tt.text, tt.n)
		if got != tt.expected {
			t.Errorf("Tail(%q, %d) = %q, want %q", tt.text, tt.n, got, tt.expected)
		}
	}
}

func TestTrimWithCap(t *testing.T) {
	tests := []struct {
		text     string
		max      int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "world"},
		{"", 5, ""},
		{"abcdefghij", 3, "hij"},
	}

	for _, tt := range tests {
		got := TrimWithCap(tt.text, tt.max)
		if got != tt.expected {
			t.Errorf("TrimWithCap(%q, %d) = %q, want %q", tt.text, tt.max, got, tt.expected)
		}
	}
}

func TestCapPendingBuffer(t *testing.T) {
	tests := []struct {
		name          string
		buffer        []string
		pendingChars  int
		cap           int
		expectedChars int
		expectedLen   int
	}{
		{
			name:          "under cap",
			buffer:        []string{"hello"},
			pendingChars:  5,
			cap:           10,
			expectedChars: 5,
			expectedLen:   1,
		},
		{
			name:          "at cap",
			buffer:        []string{"hello"},
			pendingChars:  5,
			cap:           5,
			expectedChars: 5,
			expectedLen:   1,
		},
		{
			name:          "over cap single chunk",
			buffer:        []string{"hello world"},
			pendingChars:  11,
			cap:           5,
			expectedChars: 5,
			expectedLen:   1,
		},
		{
			name:          "over cap multiple chunks",
			buffer:        []string{"hello", " ", "world"},
			pendingChars:  11,
			cap:           6,
			expectedChars: 6,
			expectedLen:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buffer := make([]string, len(tt.buffer))
			copy(buffer, tt.buffer)

			got := capPendingBuffer(&buffer, tt.pendingChars, tt.cap)

			if got != tt.expectedChars {
				t.Errorf("capPendingBuffer returned %d, want %d", got, tt.expectedChars)
			}
			if len(buffer) != tt.expectedLen {
				t.Errorf("buffer length = %d, want %d", len(buffer), tt.expectedLen)
			}
		})
	}
}

func TestProcessStatus_Values(t *testing.T) {
	if ProcessStatusRunning != "running" {
		t.Errorf("expected 'running', got %s", ProcessStatusRunning)
	}
	if ProcessStatusCompleted != "completed" {
		t.Errorf("expected 'completed', got %s", ProcessStatusCompleted)
	}
	if ProcessStatusFailed != "failed" {
		t.Errorf("expected 'failed', got %s", ProcessStatusFailed)
	}
	if ProcessStatusKilled != "killed" {
		t.Errorf("expected 'killed', got %s", ProcessStatusKilled)
	}
}

func TestProcessRegistry_AppendOutput_NilSession(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	// Should not panic
	r.AppendOutput(nil, "stdout", "test")
}

func TestProcessRegistry_DrainSession_NilSession(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	stdout, stderr := r.DrainSession(nil)
	if stdout != "" || stderr != "" {
		t.Errorf("expected empty output for nil session")
	}
}

func TestProcessRegistry_MarkExited_WithExitSignal(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	session := &ProcessSession{
		ID:           "test-1",
		Command:      "sleep 100",
		Backgrounded: true,
	}
	r.AddSession(session)

	r.MarkExited(session, nil, "SIGTERM", ProcessStatusKilled)

	finished, exists := r.GetFinishedSession("test-1")
	if !exists {
		t.Fatal("expected finished session")
	}

	if finished.ExitSignal != "SIGTERM" {
		t.Errorf("expected exit signal 'SIGTERM', got %q", finished.ExitSignal)
	}
	if finished.Status != ProcessStatusKilled {
		t.Errorf("expected status 'killed', got %s", finished.Status)
	}
}

func TestProcessRegistry_SweeperStartStop(t *testing.T) {
	r := NewProcessRegistry(nil)

	// Start sweeper
	r.StartSweeper()

	// Start again (should be idempotent)
	r.StartSweeper()

	// Stop sweeper
	r.StopSweeper()

	// Stop again (should be idempotent)
	r.StopSweeper()
}

func TestProcessRegistry_DeleteFinishedSession(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	s := &ProcessSession{ID: "test-1", Backgrounded: true}
	r.AddSession(s)

	exitCode := 0
	r.MarkExited(s, &exitCode, "", ProcessStatusCompleted)

	// Delete should remove from finished
	r.DeleteSession("test-1")

	_, exists := r.GetFinishedSession("test-1")
	if exists {
		t.Error("expected finished session to be deleted")
	}
}

func TestFinishedSession_Fields(t *testing.T) {
	r := NewProcessRegistry(nil)
	defer r.Reset()

	now := time.Now()
	session := &ProcessSession{
		ID:               "test-1",
		Command:          "echo hello",
		ScopeKey:         "scope-123",
		StartedAt:        now,
		CWD:              "/home/user",
		Backgrounded:     true,
		Aggregated:       "hello\n",
		Truncated:        true,
		TotalOutputChars: 6,
	}
	r.AddSession(session)

	exitCode := 0
	r.MarkExited(session, &exitCode, "", ProcessStatusCompleted)

	finished, exists := r.GetFinishedSession("test-1")
	if !exists {
		t.Fatal("expected finished session")
	}

	if finished.ID != "test-1" {
		t.Errorf("expected ID 'test-1', got %q", finished.ID)
	}
	if finished.Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", finished.Command)
	}
	if finished.ScopeKey != "scope-123" {
		t.Errorf("expected scope key 'scope-123', got %q", finished.ScopeKey)
	}
	if !finished.StartedAt.Equal(now) {
		t.Errorf("expected start time %v, got %v", now, finished.StartedAt)
	}
	if finished.EndedAt.Before(now) {
		t.Error("expected end time after start time")
	}
	if finished.CWD != "/home/user" {
		t.Errorf("expected CWD '/home/user', got %q", finished.CWD)
	}
	if !finished.Truncated {
		t.Error("expected truncated to be true")
	}
	if finished.TotalOutputChars != 6 {
		t.Errorf("expected total output chars 6, got %d", finished.TotalOutputChars)
	}
}
