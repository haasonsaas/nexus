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
