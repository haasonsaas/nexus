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

// Rotate removes log files older than the given retention period.
// Returns the number of files removed and any error encountered.
func (l *MemoryLogger) Rotate(retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	return l.RotateAt(cutoff)
}

// RotateAt removes log files with dates before the cutoff time.
func (l *MemoryLogger) RotateAt(cutoff time.Time) (int, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read memory dir: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		// Parse date from filename (YYYY-MM-DD.md)
		dateStr := strings.TrimSuffix(name, ".md")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue // Skip non-date files
		}

		if fileDate.Before(cutoff) {
			path := filepath.Join(l.dir, name)
			if err := os.Remove(path); err != nil {
				return removed, fmt.Errorf("remove old log %s: %w", name, err)
			}
			removed++
		}
	}

	return removed, nil
}

// ListDates returns a sorted list of available log dates (most recent first).
func (l *MemoryLogger) ListDates() ([]time.Time, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory dir: %w", err)
	}

	var dates []time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		dateStr := strings.TrimSuffix(name, ".md")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		dates = append(dates, fileDate)
	}

	// Sort descending (most recent first)
	for i := 0; i < len(dates)-1; i++ {
		for j := i + 1; j < len(dates); j++ {
			if dates[j].After(dates[i]) {
				dates[i], dates[j] = dates[j], dates[i]
			}
		}
	}

	return dates, nil
}

// Stats returns statistics about the memory logs.
type MemoryLogStats struct {
	TotalFiles int
	TotalLines int
	TotalBytes int64
	OldestDate time.Time
	NewestDate time.Time
}

// Stats returns statistics about the memory logs.
func (l *MemoryLogger) Stats() (*MemoryLogStats, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &MemoryLogStats{}, nil
		}
		return nil, fmt.Errorf("read memory dir: %w", err)
	}

	stats := &MemoryLogStats{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		dateStr := strings.TrimSuffix(name, ".md")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		stats.TotalFiles++

		if stats.OldestDate.IsZero() || fileDate.Before(stats.OldestDate) {
			stats.OldestDate = fileDate
		}
		if stats.NewestDate.IsZero() || fileDate.After(stats.NewestDate) {
			stats.NewestDate = fileDate
		}

		info, err := entry.Info()
		if err == nil {
			stats.TotalBytes += info.Size()
		}

		// Count lines
		path := filepath.Join(l.dir, name)
		if lines, err := countLines(path); err == nil {
			stats.TotalLines += lines
		}
	}

	return stats, nil
}

func countLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, scanner.Err()
}

// Dir returns the directory path for the memory logs.
func (l *MemoryLogger) Dir() string {
	return l.dir
}
