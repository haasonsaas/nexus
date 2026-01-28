// Package imessage provides an iMessage channel adapter for macOS.
//go:build darwin
// +build darwin

package imessage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
	"github.com/haasonsaas/nexus/pkg/models"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Adapter implements the iMessage channel adapter for macOS.
type Adapter struct {
	*personal.BaseAdapter

	config *Config
	db     *sql.DB

	lastMessageID atomic.Int64
	pollInterval  time.Duration

	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// New creates a new iMessage adapter.
func New(cfg *Config, logger *slog.Logger) (*Adapter, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Parse poll interval
	pollInterval, err := time.ParseDuration(cfg.PollInterval)
	if err != nil {
		pollInterval = time.Second
	}

	adapter := &Adapter{
		BaseAdapter:  personal.NewBaseAdapter(models.ChannelIMessage, &cfg.Personal, logger),
		config:       cfg,
		pollInterval: pollInterval,
	}

	return adapter, nil
}

// Start connects to the iMessage database and begins polling for messages.
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancelFunc = cancel

	// Expand database path
	dbPath := expandPath(a.config.DatabasePath)

	// Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return channels.ErrNotFound(fmt.Sprintf("iMessage database not found at %q", dbPath), nil)
	}

	// Open database in read-only mode
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return channels.ErrConnection("failed to open database", err)
	}
	a.db = db

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		return channels.ErrConnection("failed to ping database", err)
	}

	// Get the last message ID to avoid processing old messages
	lastID, err := a.getLastMessageID(ctx)
	if err != nil {
		a.Logger().Warn("failed to get last message ID", "error", err)
		lastID = 0
	}
	a.lastMessageID.Store(lastID)

	a.SetStatus(true, "")
	a.Logger().Info("started iMessage adapter",
		"database", dbPath,
		"poll_interval", a.pollInterval)

	// Start polling loop
	a.wg.Add(1)
	go a.pollLoop(ctx)

	return nil
}

// Stop disconnects from the iMessage database.
func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancelFunc != nil {
		a.cancelFunc()
	}

	a.wg.Wait()

	if a.db != nil {
		a.db.Close()
	}

	a.SetStatus(false, "")
	a.BaseAdapter.Close()
	return nil
}

// Send sends a message through iMessage using AppleScript.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	peerID, ok := msg.Metadata["peer_id"].(string)
	if !ok || peerID == "" {
		msgID := ""
		if msg != nil {
			msgID = msg.ID
		}
		return channels.ErrInvalidInput(channels.MissingMetadata("peer_id", msgID), nil)
	}

	// Build AppleScript
	script := fmt.Sprintf(`
		tell application "Messages"
			set targetService to 1st account whose service type = iMessage
			set targetBuddy to participant %q of targetService
			send %q to targetBuddy
		end tell
	`, peerID, escapeAppleScript(msg.Content))

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		a.IncrementErrors()
		return channels.ErrConnection(fmt.Sprintf("failed to send message via AppleScript (output: %s)", output), err)
	}

	a.IncrementSent()
	return nil
}

// HealthCheck returns the adapter's health status.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	start := time.Now()

	if a.db == nil {
		return channels.HealthStatus{
			Healthy:   false,
			Message:   "database not connected",
			Latency:   time.Since(start),
			LastCheck: time.Now(),
		}
	}

	if err := a.db.PingContext(ctx); err != nil {
		return channels.HealthStatus{
			Healthy:   false,
			Message:   fmt.Sprintf("database ping failed: %v", err),
			Latency:   time.Since(start),
			LastCheck: time.Now(),
		}
	}

	return channels.HealthStatus{
		Healthy:   true,
		Message:   "connected",
		Latency:   time.Since(start),
		LastCheck: time.Now(),
	}
}

// Contacts returns the contact manager.
func (a *Adapter) Contacts() personal.ContactManager {
	return &contactManager{adapter: a}
}

// Media returns the media handler.
func (a *Adapter) Media() personal.MediaHandler {
	return &personal.BaseMediaHandler{}
}

// Presence returns the presence manager.
func (a *Adapter) Presence() personal.PresenceManager {
	return &personal.BasePresenceManager{}
}

// GetConversation returns a conversation by peer ID.
func (a *Adapter) GetConversation(ctx context.Context, peerID string) (*personal.Conversation, error) {
	// Query chat info
	query := `
		SELECT c.ROWID, c.chat_identifier, c.display_name, c.style
		FROM chat c
		WHERE c.chat_identifier = ?
	`

	var rowID int64
	var chatID, displayName string
	var style int

	err := a.db.QueryRowContext(ctx, query, peerID).Scan(&rowID, &chatID, &displayName, &style)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, channels.ErrInternal("failed to query conversation", err)
	}

	convType := personal.ConversationDM
	if style == 43 { // Group chat style
		convType = personal.ConversationGroup
	}

	return &personal.Conversation{
		ID:   chatID,
		Type: convType,
		Name: displayName,
	}, nil
}

// ListConversations lists conversations.
func (a *Adapter) ListConversations(ctx context.Context, opts personal.ListOptions) ([]*personal.Conversation, error) {
	query := `
		SELECT c.chat_identifier, c.display_name, c.style
		FROM chat c
		ORDER BY c.ROWID DESC
		LIMIT ?
	`

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	rows, err := a.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, channels.ErrInternal("failed to query conversations", err)
	}
	defer rows.Close()

	var conversations []*personal.Conversation
	for rows.Next() {
		var chatID, displayName string
		var style int

		if err := rows.Scan(&chatID, &displayName, &style); err != nil {
			continue
		}

		convType := personal.ConversationDM
		if style == 43 {
			convType = personal.ConversationGroup
		}

		conversations = append(conversations, &personal.Conversation{
			ID:   chatID,
			Type: convType,
			Name: displayName,
		})
	}

	return conversations, nil
}

// pollLoop continuously polls for new messages.
func (a *Adapter) pollLoop(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollNewMessages(ctx)
		}
	}
}

// pollNewMessages queries for messages newer than lastMessageID.
func (a *Adapter) pollNewMessages(ctx context.Context) {
	query := `
		SELECT
			m.ROWID,
			m.guid,
			m.text,
			m.date,
			m.is_from_me,
			h.id as handle_id,
			c.chat_identifier,
			c.display_name,
			c.style
		FROM message m
		LEFT JOIN handle h ON m.handle_id = h.ROWID
		LEFT JOIN chat_message_join cmj ON m.ROWID = cmj.message_id
		LEFT JOIN chat c ON cmj.chat_id = c.ROWID
		WHERE m.ROWID > ?
			AND m.is_from_me = 0
		ORDER BY m.ROWID ASC
		LIMIT 100
	`

	rows, err := a.db.QueryContext(ctx, query, a.lastMessageID.Load())
	if err != nil {
		a.Logger().Error("failed to poll messages", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var rowID int64
		var guid, text, handleID string
		var dateNano int64
		var isFromMe int
		var chatID, displayName sql.NullString
		var style sql.NullInt64

		if err := rows.Scan(&rowID, &guid, &text, &dateNano, &isFromMe, &handleID, &chatID, &displayName, &style); err != nil {
			a.Logger().Error("failed to scan message", "error", err)
			continue
		}

		// Update last message ID atomically
		for {
			current := a.lastMessageID.Load()
			if rowID <= current {
				break
			}
			if a.lastMessageID.CompareAndSwap(current, rowID) {
				break
			}
		}

		// Skip outgoing messages
		if isFromMe == 1 {
			continue
		}

		// Convert Apple timestamp (nanoseconds since 2001-01-01)
		timestamp := appleTimestampToTime(dateNano)

		raw := personal.RawMessage{
			ID:        guid,
			Content:   text,
			PeerID:    handleID,
			PeerName:  handleID, // Could resolve via Contacts
			Timestamp: timestamp,
		}

		// Handle group chats
		if chatID.Valid && style.Valid && style.Int64 == 43 {
			raw.GroupID = chatID.String
			raw.GroupName = displayName.String
		}

		msg := a.NormalizeInbound(raw)
		a.Emit(msg)
	}
}

// getLastMessageID returns the maximum message ROWID.
func (a *Adapter) getLastMessageID(ctx context.Context) (int64, error) {
	var maxID sql.NullInt64
	err := a.db.QueryRowContext(ctx, "SELECT MAX(ROWID) FROM message").Scan(&maxID)
	if err != nil {
		return 0, err
	}
	if maxID.Valid {
		return maxID.Int64, nil
	}
	return 0, nil
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// escapeAppleScript escapes a string for use in AppleScript.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// appleTimestampToTime converts an Apple timestamp to time.Time.
// Apple timestamps are nanoseconds since 2001-01-01 00:00:00 UTC.
func appleTimestampToTime(nano int64) time.Time {
	appleEpoch := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	return appleEpoch.Add(time.Duration(nano) * time.Nanosecond)
}

// SendTypingIndicator is a no-op for iMessage as it doesn't support
// programmatic typing indicators.
// This is part of the StreamingAdapter interface.
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	// iMessage doesn't support programmatic typing indicators
	return nil
}

// StartStreamingResponse reports streaming as unsupported for iMessage.
// This is part of the StreamingAdapter interface.
func (a *Adapter) StartStreamingResponse(ctx context.Context, msg *models.Message) (string, error) {
	// iMessage doesn't support message editing, so we can't do true streaming.
	return "", channels.ErrStreamingNotSupported
}

// UpdateStreamingResponse reports streaming as unsupported for iMessage.
// This is part of the StreamingAdapter interface.
func (a *Adapter) UpdateStreamingResponse(ctx context.Context, msg *models.Message, messageID string, content string) error {
	// iMessage doesn't support editing sent messages
	return channels.ErrStreamingNotSupported
}
