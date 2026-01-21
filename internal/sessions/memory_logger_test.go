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
