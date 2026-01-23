// Package sessions provides session storage and management.
//
// import.go implements JSONL-based session/history import for migrations.
package sessions

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ImportFormat defines the JSONL record types.
type ImportFormat string

const (
	// FormatSession indicates a session record.
	FormatSession ImportFormat = "session"
	// FormatMessage indicates a message record.
	FormatMessage ImportFormat = "message"
)

// ImportRecord is a single line in the JSONL import file.
type ImportRecord struct {
	Type      ImportFormat   `json:"type"`
	Session   *SessionRecord `json:"session,omitempty"`
	Message   *MessageRecord `json:"message,omitempty"`
	SourceID  string         `json:"source_id,omitempty"`  // Original ID from source system
	Timestamp time.Time      `json:"timestamp,omitempty"` // Import timestamp
}

// SessionRecord represents a session to import.
type SessionRecord struct {
	ID        string         `json:"id"`
	AgentID   string         `json:"agent_id"`
	Channel   string         `json:"channel"`
	ChannelID string         `json:"channel_id"`
	Title     string         `json:"title,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
}

// MessageRecord represents a message to import.
type MessageRecord struct {
	ID          string              `json:"id"`
	SessionID   string              `json:"session_id"`
	Channel     string              `json:"channel"`
	ChannelID   string              `json:"channel_id,omitempty"`
	Direction   string              `json:"direction"` // inbound or outbound
	Role        string              `json:"role"`      // user, assistant, system, tool
	Content     string              `json:"content"`
	Attachments []AttachmentRecord  `json:"attachments,omitempty"`
	ToolCalls   []ToolCallRecord    `json:"tool_calls,omitempty"`
	ToolResults []ToolResultRecord  `json:"tool_results,omitempty"`
	Metadata    map[string]any      `json:"metadata,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
}

// AttachmentRecord represents an attachment to import.
type AttachmentRecord struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	URL      string `json:"url,omitempty"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// ToolCallRecord represents a tool call to import.
type ToolCallRecord struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolResultRecord represents a tool result to import.
type ToolResultRecord struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

// ImportResult tracks the outcome of an import operation.
type ImportResult struct {
	SessionsImported   int      `json:"sessions_imported"`
	SessionsSkipped    int      `json:"sessions_skipped"`
	MessagesImported   int      `json:"messages_imported"`
	MessagesSkipped    int      `json:"messages_skipped"`
	Errors             []string `json:"errors,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
	Duration           time.Duration `json:"duration"`

	// Mapping from source IDs to new IDs
	SessionIDMap map[string]string `json:"session_id_map,omitempty"`
}

// ImportOptions configures the import behavior.
type ImportOptions struct {
	// DryRun performs validation without writing
	DryRun bool

	// SkipDuplicates silently skips records that already exist
	SkipDuplicates bool

	// DefaultAgentID is used when session has no agent_id
	DefaultAgentID string

	// RemapChannelIDs maps old channel peer IDs to new ones
	RemapChannelIDs map[string]string

	// PreserveIDs keeps original IDs instead of generating new ones
	PreserveIDs bool
}

// Importer handles JSONL import operations.
type Importer struct {
	store Store
}

// NewImporter creates a new importer.
func NewImporter(store Store) *Importer {
	return &Importer{store: store}
}

// ImportFromFile imports sessions and messages from a JSONL file.
func (i *Importer) ImportFromFile(ctx context.Context, path string, opts ImportOptions) (*ImportResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	return i.ImportFromReader(ctx, file, opts)
}

// ImportFromReader imports sessions and messages from a JSONL reader.
func (i *Importer) ImportFromReader(ctx context.Context, r io.Reader, opts ImportOptions) (*ImportResult, error) {
	start := time.Now()
	result := &ImportResult{
		SessionIDMap: make(map[string]string),
	}

	scanner := bufio.NewScanner(r)
	lineNum := 0

	// First pass: import sessions
	var sessionRecords []ImportRecord
	var messageRecords []ImportRecord

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var record ImportRecord
		if err := json.Unmarshal(line, &record); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: parse error: %v", lineNum, err))
			continue
		}

		switch record.Type {
		case FormatSession:
			if record.Session == nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: session record missing session data", lineNum))
				continue
			}
			sessionRecords = append(sessionRecords, record)
		case FormatMessage:
			if record.Message == nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: message record missing message data", lineNum))
				continue
			}
			messageRecords = append(messageRecords, record)
		default:
			result.Warnings = append(result.Warnings, fmt.Sprintf("line %d: unknown record type %q", lineNum, record.Type))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Import sessions first
	for _, rec := range sessionRecords {
		if err := i.importSession(ctx, rec.Session, opts, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("session %s: %v", rec.Session.ID, err))
		}
	}

	// Then import messages (need session ID mapping)
	for _, rec := range messageRecords {
		if err := i.importMessage(ctx, rec.Message, opts, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("message %s: %v", rec.Message.ID, err))
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (i *Importer) importSession(ctx context.Context, rec *SessionRecord, opts ImportOptions, result *ImportResult) error {
	// Build session key
	agentID := rec.AgentID
	if agentID == "" {
		agentID = opts.DefaultAgentID
	}
	if agentID == "" {
		agentID = "default"
	}

	channelID := rec.ChannelID
	if mapped, ok := opts.RemapChannelIDs[channelID]; ok {
		channelID = mapped
	}

	key := fmt.Sprintf("%s:%s:%s", agentID, rec.Channel, channelID)

	// Check for existing session
	existing, err := i.store.GetByKey(ctx, key)
	if err == nil && existing != nil {
		if opts.SkipDuplicates {
			result.SessionsSkipped++
			result.SessionIDMap[rec.ID] = existing.ID
			return nil
		}
		return fmt.Errorf("session already exists with key %s", key)
	}

	if opts.DryRun {
		result.SessionsImported++
		result.SessionIDMap[rec.ID] = rec.ID
		return nil
	}

	// Create new session
	newID := rec.ID
	if !opts.PreserveIDs || newID == "" {
		newID = uuid.NewString()
	}

	session := &models.Session{
		ID:        newID,
		AgentID:   agentID,
		Channel:   models.ChannelType(rec.Channel),
		ChannelID: channelID,
		Key:       key,
		Title:     rec.Title,
		Metadata:  rec.Metadata,
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
	}

	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = session.CreatedAt
	}

	if err := i.store.Create(ctx, session); err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	result.SessionsImported++
	result.SessionIDMap[rec.ID] = newID
	return nil
}

func (i *Importer) importMessage(ctx context.Context, rec *MessageRecord, opts ImportOptions, result *ImportResult) error {
	// Map session ID
	sessionID, ok := result.SessionIDMap[rec.SessionID]
	if !ok {
		return fmt.Errorf("unknown session ID %s", rec.SessionID)
	}

	if opts.DryRun {
		result.MessagesImported++
		return nil
	}

	// Build message
	newID := rec.ID
	if !opts.PreserveIDs || newID == "" {
		newID = uuid.NewString()
	}

	msg := &models.Message{
		ID:        newID,
		SessionID: sessionID,
		Channel:   models.ChannelType(rec.Channel),
		ChannelID: rec.ChannelID,
		Direction: models.Direction(rec.Direction),
		Role:      models.Role(rec.Role),
		Content:   rec.Content,
		Metadata:  rec.Metadata,
		CreatedAt: rec.CreatedAt,
	}

	// Convert attachments
	for _, att := range rec.Attachments {
		msg.Attachments = append(msg.Attachments, models.Attachment{
			ID:       att.ID,
			Type:     att.Type,
			URL:      att.URL,
			Filename: att.Filename,
			MimeType: att.MimeType,
			Size:     att.Size,
		})
	}

	// Convert tool calls
	for _, tc := range rec.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, models.ToolCall{
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		})
	}

	// Convert tool results
	for _, tr := range rec.ToolResults {
		msg.ToolResults = append(msg.ToolResults, models.ToolResult{
			ToolCallID: tr.ToolCallID,
			Content:    tr.Content,
			IsError:    tr.IsError,
		})
	}

	if err := i.store.AppendMessage(ctx, sessionID, msg); err != nil {
		return fmt.Errorf("append message: %w", err)
	}

	result.MessagesImported++
	return nil
}

// FormatImportResult returns a human-readable summary.
func FormatImportResult(result *ImportResult) string {
	var sb fmt.Stringer = &importResultFormatter{result}
	return sb.String()
}

type importResultFormatter struct {
	*ImportResult
}

func (f *importResultFormatter) String() string {
	var s string
	s += "Import Results\n"
	s += "==============\n\n"
	s += fmt.Sprintf("Sessions: %d imported, %d skipped\n", f.SessionsImported, f.SessionsSkipped)
	s += fmt.Sprintf("Messages: %d imported, %d skipped\n", f.MessagesImported, f.MessagesSkipped)
	s += fmt.Sprintf("Duration: %v\n", f.Duration.Round(time.Millisecond))

	if len(f.Errors) > 0 {
		s += fmt.Sprintf("\nErrors (%d):\n", len(f.Errors))
		for _, err := range f.Errors {
			s += fmt.Sprintf("  - %s\n", err)
		}
	}

	if len(f.Warnings) > 0 {
		s += fmt.Sprintf("\nWarnings (%d):\n", len(f.Warnings))
		for _, w := range f.Warnings {
			s += fmt.Sprintf("  - %s\n", w)
		}
	}

	return s
}

// ExportToJSONL exports sessions and messages to JSONL format.
func ExportToJSONL(ctx context.Context, store Store, w io.Writer, agentID string) error {
	sessions, err := store.List(ctx, agentID, ListOptions{Limit: 10000})
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	encoder := json.NewEncoder(w)

	for _, session := range sessions {
		// Export session
		rec := ImportRecord{
			Type:      FormatSession,
			Timestamp: time.Now(),
			Session: &SessionRecord{
				ID:        session.ID,
				AgentID:   session.AgentID,
				Channel:   string(session.Channel),
				ChannelID: session.ChannelID,
				Title:     session.Title,
				Metadata:  session.Metadata,
				CreatedAt: session.CreatedAt,
				UpdatedAt: session.UpdatedAt,
			},
		}
		if err := encoder.Encode(rec); err != nil {
			return fmt.Errorf("encode session %s: %w", session.ID, err)
		}

		// Export messages
		messages, err := store.GetHistory(ctx, session.ID, 10000)
		if err != nil {
			return fmt.Errorf("get history for %s: %w", session.ID, err)
		}

		for _, msg := range messages {
			msgRec := ImportRecord{
				Type:      FormatMessage,
				Timestamp: time.Now(),
				Message: &MessageRecord{
					ID:        msg.ID,
					SessionID: session.ID,
					Channel:   string(msg.Channel),
					ChannelID: msg.ChannelID,
					Direction: string(msg.Direction),
					Role:      string(msg.Role),
					Content:   msg.Content,
					Metadata:  msg.Metadata,
					CreatedAt: msg.CreatedAt,
				},
			}

			// Convert attachments
			for _, att := range msg.Attachments {
				msgRec.Message.Attachments = append(msgRec.Message.Attachments, AttachmentRecord{
					ID:       att.ID,
					Type:     att.Type,
					URL:      att.URL,
					Filename: att.Filename,
					MimeType: att.MimeType,
					Size:     att.Size,
				})
			}

			// Convert tool calls
			for _, tc := range msg.ToolCalls {
				msgRec.Message.ToolCalls = append(msgRec.Message.ToolCalls, ToolCallRecord{
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}

			// Convert tool results
			for _, tr := range msg.ToolResults {
				msgRec.Message.ToolResults = append(msgRec.Message.ToolResults, ToolResultRecord{
					ToolCallID: tr.ToolCallID,
					Content:    tr.Content,
					IsError:    tr.IsError,
				})
			}

			if err := encoder.Encode(msgRec); err != nil {
				return fmt.Errorf("encode message %s: %w", msg.ID, err)
			}
		}
	}

	return nil
}
