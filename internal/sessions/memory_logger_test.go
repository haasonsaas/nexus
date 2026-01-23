package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestMemoryLoggerAppend(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	ts := time.Date(2026, 1, 21, 12, 0, 1, 0, time.UTC)
	msg := &models.Message{
		SessionID: "session-1",
		Channel:   models.ChannelSlack,
		Role:      models.RoleUser,
		Content:   "hello\nworld",
		CreatedAt: ts,
	}

	if err := logger.Append(msg); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	path := filepath.Join(dir, "2026-01-21.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "user") || !strings.Contains(text, "slack") {
		t.Fatalf("expected log to contain role and channel, got %q", text)
	}
	if !strings.Contains(text, "session-1") {
		t.Fatalf("expected session id in log, got %q", text)
	}
	if !strings.Contains(text, "hello world") {
		t.Fatalf("expected flattened content, got %q", text)
	}
}

func TestMemoryLoggerReadRecentAtFiltersSession(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	base := time.Date(2026, 1, 21, 12, 0, 0, 0, time.UTC)
	msgs := []*models.Message{
		{
			SessionID: "session-1",
			Channel:   models.ChannelSlack,
			Role:      models.RoleUser,
			Content:   "yesterday note",
			CreatedAt: base.AddDate(0, 0, -1),
		},
		{
			SessionID: "session-2",
			Channel:   models.ChannelSlack,
			Role:      models.RoleUser,
			Content:   "other session",
			CreatedAt: base.AddDate(0, 0, -1),
		},
		{
			SessionID: "session-1",
			Channel:   models.ChannelSlack,
			Role:      models.RoleAssistant,
			Content:   "today update",
			CreatedAt: base,
		},
	}

	for _, msg := range msgs {
		if err := logger.Append(msg); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	lines, err := logger.ReadRecentAt(base, models.ChannelSlack, "session-1", 2, 10)
	if err != nil {
		t.Fatalf("ReadRecentAt() error = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if strings.Contains(lines[0], "session-2") || strings.Contains(lines[1], "session-2") {
		t.Fatalf("expected filtered session lines, got %q", lines)
	}

	lines, err = logger.ReadRecentAt(base, models.ChannelSlack, "session-1", 2, 1)
	if err != nil {
		t.Fatalf("ReadRecentAt() error = %v", err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "today update") {
		t.Fatalf("expected most recent line, got %q", lines)
	}
}

func TestMemoryLoggerRotate(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	// Create log files for multiple dates
	dates := []time.Time{
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), // Old
		time.Date(2026, 1, 18, 12, 0, 0, 0, time.UTC), // Old
		time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), // Recent
		time.Date(2026, 1, 21, 12, 0, 0, 0, time.UTC), // Today
	}

	for _, d := range dates {
		msg := &models.Message{
			SessionID: "session-1",
			Channel:   models.ChannelSlack,
			Role:      models.RoleUser,
			Content:   "message for " + d.Format("2006-01-02"),
			CreatedAt: d,
		}
		if err := logger.Append(msg); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	// Rotate with cutoff on Jan 19
	cutoff := time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC)
	removed, err := logger.RotateAt(cutoff)
	if err != nil {
		t.Fatalf("RotateAt() error = %v", err)
	}
	if removed != 2 {
		t.Errorf("expected 2 files removed, got %d", removed)
	}

	// Check remaining files
	remaining, err := logger.ListDates()
	if err != nil {
		t.Fatalf("ListDates() error = %v", err)
	}
	if len(remaining) != 2 {
		t.Errorf("expected 2 files remaining, got %d", len(remaining))
	}
}

func TestMemoryLoggerRotate_ZeroRetention(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	removed, err := logger.Rotate(0)
	if err != nil {
		t.Fatalf("Rotate(0) error = %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

func TestMemoryLoggerRotate_NonExistentDir(t *testing.T) {
	logger := NewMemoryLogger("/nonexistent/path/memory")

	removed, err := logger.RotateAt(time.Now())
	if err != nil {
		t.Fatalf("RotateAt() error = %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed for nonexistent dir, got %d", removed)
	}
}

func TestMemoryLoggerListDates(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	// Create log files in non-sorted order
	dates := []time.Time{
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 21, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 18, 12, 0, 0, 0, time.UTC),
	}

	for _, d := range dates {
		msg := &models.Message{
			SessionID: "session-1",
			Channel:   models.ChannelSlack,
			Role:      models.RoleUser,
			Content:   "test",
			CreatedAt: d,
		}
		if err := logger.Append(msg); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	result, err := logger.ListDates()
	if err != nil {
		t.Fatalf("ListDates() error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 dates, got %d", len(result))
	}

	// Should be sorted descending (most recent first)
	expected := []string{"2026-01-21", "2026-01-18", "2026-01-15"}
	for i, d := range result {
		if d.Format("2006-01-02") != expected[i] {
			t.Errorf("result[%d] = %s, want %s", i, d.Format("2006-01-02"), expected[i])
		}
	}
}

func TestMemoryLoggerListDates_Empty(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	result, err := logger.ListDates()
	if err != nil {
		t.Fatalf("ListDates() error = %v", err)
	}
	if result != nil && len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestMemoryLoggerStats(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	// Create log files
	dates := []time.Time{
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 21, 12, 0, 0, 0, time.UTC),
	}

	for _, d := range dates {
		for i := 0; i < 3; i++ {
			msg := &models.Message{
				SessionID: "session-1",
				Channel:   models.ChannelSlack,
				Role:      models.RoleUser,
				Content:   "test message",
				CreatedAt: d,
			}
			if err := logger.Append(msg); err != nil {
				t.Fatalf("Append() error = %v", err)
			}
		}
	}

	stats, err := logger.Stats()
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}

	if stats.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", stats.TotalFiles)
	}
	if stats.TotalLines != 6 {
		t.Errorf("TotalLines = %d, want 6", stats.TotalLines)
	}
	if stats.TotalBytes == 0 {
		t.Error("TotalBytes should be > 0")
	}
	if stats.OldestDate.Format("2006-01-02") != "2026-01-15" {
		t.Errorf("OldestDate = %s, want 2026-01-15", stats.OldestDate.Format("2006-01-02"))
	}
	if stats.NewestDate.Format("2006-01-02") != "2026-01-21" {
		t.Errorf("NewestDate = %s, want 2026-01-21", stats.NewestDate.Format("2006-01-02"))
	}
}

func TestMemoryLoggerStats_Empty(t *testing.T) {
	dir := t.TempDir()
	logger := NewMemoryLogger(dir)

	stats, err := logger.Stats()
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}

	if stats.TotalFiles != 0 {
		t.Errorf("TotalFiles = %d, want 0", stats.TotalFiles)
	}
	if stats.TotalLines != 0 {
		t.Errorf("TotalLines = %d, want 0", stats.TotalLines)
	}
}

func TestMemoryLoggerDir(t *testing.T) {
	logger := NewMemoryLogger("/custom/path")
	if logger.Dir() != "/custom/path" {
		t.Errorf("Dir() = %q, want %q", logger.Dir(), "/custom/path")
	}
}
