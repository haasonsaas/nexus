package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// MemoryLogger writes a simple daily markdown log of messages for human review.
type MemoryLogger struct {
	dir string
	mu  sync.Mutex
}

// NewMemoryLogger creates a new logger that writes to dir (defaults to "memory").
func NewMemoryLogger(dir string) *MemoryLogger {
	if strings.TrimSpace(dir) == "" {
		dir = "memory"
	}
	return &MemoryLogger{dir: dir}
}

// Append writes a single message entry to today's log file.
func (l *MemoryLogger) Append(msg *models.Message) error {
	if msg == nil {
		return fmt.Errorf("message is nil")
	}

	ts := msg.CreatedAt
	if ts.IsZero() {
		ts = time.Now()
	}

	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	filename := filepath.Join(l.dir, ts.Format("2006-01-02")+".md")
	line := formatMemoryLine(msg, ts)

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open memory log: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write memory log: %w", err)
	}

	return nil
}

func formatMemoryLine(msg *models.Message, ts time.Time) string {
	content := strings.TrimSpace(msg.Content)
	content = strings.ReplaceAll(content, "\n", " ")
	role := string(msg.Role)
	channel := string(msg.Channel)
	session := msg.SessionID
	if session == "" {
		session = "unknown"
	}
	return fmt.Sprintf("- [%s] %s (%s/%s): %s\n", ts.Format("15:04:05"), role, channel, session, content)
}
