package sessions

import (
	"bufio"
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

// ReadRecentAt returns up to maxLines memory log lines for the given session and channel.
// It scans back the requested number of days (including today) and keeps the most recent lines.
func (l *MemoryLogger) ReadRecentAt(now time.Time, channel models.ChannelType, sessionID string, days int, maxLines int) ([]string, error) {
	if days <= 0 {
		return nil, nil
	}
	if maxLines <= 0 {
		maxLines = 20
	}

	needle := ""
	if sessionID != "" && channel != "" {
		needle = fmt.Sprintf("(%s/%s):", channel, sessionID)
	}

	var lines []string
	for offset := days - 1; offset >= 0; offset-- {
		date := now.AddDate(0, 0, -offset).Format("2006-01-02")
		path := filepath.Join(l.dir, date+".md")
		file, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("open memory log: %w", err)
		}

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if needle != "" && !strings.Contains(line, needle) {
				continue
			}
			lines = append(lines, line)
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("read memory log: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close memory log: %w", err)
		}
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
}

// ReadRecent reads recent memory lines using the current time.
func (l *MemoryLogger) ReadRecent(channel models.ChannelType, sessionID string, days int, maxLines int) ([]string, error) {
	return l.ReadRecentAt(time.Now(), channel, sessionID, days, maxLines)
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
