package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestImportFromReader(t *testing.T) {
	store := NewMemoryStore()
	importer := NewImporter(store)
	ctx := context.Background()

	// Create test JSONL content
	now := time.Now().Truncate(time.Millisecond)
	jsonlContent := []string{
		mustJSON(ImportRecord{
			Type: FormatSession,
			Session: &SessionRecord{
				ID:        "session-1",
				AgentID:   "agent-1",
				Channel:   "telegram",
				ChannelID: "user-123",
				Title:     "Test Session",
				CreatedAt: now,
			},
		}),
		mustJSON(ImportRecord{
			Type: FormatMessage,
			Message: &MessageRecord{
				ID:        "msg-1",
				SessionID: "session-1",
				Channel:   "telegram",
				ChannelID: "user-123",
				Direction: "inbound",
				Role:      "user",
				Content:   "Hello, world!",
				CreatedAt: now,
			},
		}),
		mustJSON(ImportRecord{
			Type: FormatMessage,
			Message: &MessageRecord{
				ID:        "msg-2",
				SessionID: "session-1",
				Channel:   "telegram",
				ChannelID: "user-123",
				Direction: "outbound",
				Role:      "assistant",
				Content:   "Hello! How can I help you?",
				CreatedAt: now.Add(time.Second),
			},
		}),
	}

	reader := strings.NewReader(strings.Join(jsonlContent, "\n"))

	result, err := importer.ImportFromReader(ctx, reader, ImportOptions{})
	if err != nil {
		t.Fatalf("ImportFromReader failed: %v", err)
	}

	if result.SessionsImported != 1 {
		t.Errorf("expected 1 session imported, got %d", result.SessionsImported)
	}
	if result.MessagesImported != 2 {
		t.Errorf("expected 2 messages imported, got %d", result.MessagesImported)
	}
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestImportDryRun(t *testing.T) {
	store := NewMemoryStore()
	importer := NewImporter(store)
	ctx := context.Background()

	now := time.Now()
	jsonlContent := mustJSON(ImportRecord{
		Type: FormatSession,
		Session: &SessionRecord{
			ID:        "session-dry",
			AgentID:   "agent-1",
			Channel:   "telegram",
			ChannelID: "user-dry",
			CreatedAt: now,
		},
	})

	reader := strings.NewReader(jsonlContent)

	result, err := importer.ImportFromReader(ctx, reader, ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromReader failed: %v", err)
	}

	if result.SessionsImported != 1 {
		t.Errorf("expected 1 session imported in dry run, got %d", result.SessionsImported)
	}

	// Verify nothing was actually stored
	sessions, err := store.List(ctx, "agent-1", ListOptions{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions in store after dry run, got %d", len(sessions))
	}
}

func TestImportSkipDuplicates(t *testing.T) {
	store := NewMemoryStore()
	importer := NewImporter(store)
	ctx := context.Background()

	now := time.Now()
	jsonlContent := mustJSON(ImportRecord{
		Type: FormatSession,
		Session: &SessionRecord{
			ID:        "session-dup",
			AgentID:   "agent-dup",
			Channel:   "telegram",
			ChannelID: "user-dup",
			CreatedAt: now,
		},
	})

	// Import once
	reader := strings.NewReader(jsonlContent)
	_, err := importer.ImportFromReader(ctx, reader, ImportOptions{})
	if err != nil {
		t.Fatalf("first import failed: %v", err)
	}

	// Import again with skip duplicates
	reader = strings.NewReader(jsonlContent)
	result, err := importer.ImportFromReader(ctx, reader, ImportOptions{SkipDuplicates: true})
	if err != nil {
		t.Fatalf("second import failed: %v", err)
	}

	if result.SessionsSkipped != 1 {
		t.Errorf("expected 1 session skipped, got %d", result.SessionsSkipped)
	}
	if result.SessionsImported != 0 {
		t.Errorf("expected 0 sessions imported, got %d", result.SessionsImported)
	}
}

func TestImportInvalidJSON(t *testing.T) {
	store := NewMemoryStore()
	importer := NewImporter(store)
	ctx := context.Background()

	reader := strings.NewReader("not valid json\n{}")

	result, err := importer.ImportFromReader(ctx, reader, ImportOptions{})
	if err != nil {
		t.Fatalf("ImportFromReader failed: %v", err)
	}

	if len(result.Errors) == 0 {
		t.Error("expected errors for invalid JSON")
	}
}

func TestExportToJSONL(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create a session with messages
	session, err := store.GetOrCreate(ctx, "agent-export:telegram:user-export", "agent-export", models.ChannelTelegram, "user-export")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	userMsg := &models.Message{
		ID:        "msg-user",
		SessionID: session.ID,
		Role:      models.RoleUser,
		Content:   "Hello!",
		Direction: models.DirectionInbound,
	}
	store.AppendMessage(ctx, session.ID, userMsg)

	asstMsg := &models.Message{
		ID:        "msg-asst",
		SessionID: session.ID,
		Role:      models.RoleAssistant,
		Content:   "Hi there!",
		Direction: models.DirectionOutbound,
	}
	store.AppendMessage(ctx, session.ID, asstMsg)

	// Export
	var buf bytes.Buffer
	if err := ExportToJSONL(ctx, store, &buf, "agent-export"); err != nil {
		t.Fatalf("ExportToJSONL failed: %v", err)
	}

	// Verify output
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 { // 1 session + 2 messages
		t.Errorf("expected at least 3 lines, got %d", len(lines))
	}

	// Verify first line is a session
	var firstRecord ImportRecord
	if err := json.Unmarshal([]byte(lines[0]), &firstRecord); err != nil {
		t.Fatalf("failed to parse first line: %v", err)
	}
	if firstRecord.Type != FormatSession {
		t.Errorf("expected first record to be session, got %s", firstRecord.Type)
	}
	if firstRecord.Session == nil {
		t.Error("expected session data in first record")
	}
}

func TestImportPreserveIDs(t *testing.T) {
	store := NewMemoryStore()
	importer := NewImporter(store)
	ctx := context.Background()

	now := time.Now()
	originalID := "my-custom-session-id"
	jsonlContent := mustJSON(ImportRecord{
		Type: FormatSession,
		Session: &SessionRecord{
			ID:        originalID,
			AgentID:   "agent-preserve",
			Channel:   "telegram",
			ChannelID: "user-preserve",
			CreatedAt: now,
		},
	})

	reader := strings.NewReader(jsonlContent)
	result, err := importer.ImportFromReader(ctx, reader, ImportOptions{PreserveIDs: true})
	if err != nil {
		t.Fatalf("ImportFromReader failed: %v", err)
	}

	// Verify the ID was preserved
	newID := result.SessionIDMap[originalID]
	if newID != originalID {
		t.Errorf("expected ID %s to be preserved, got %s", originalID, newID)
	}

	// Verify session exists with original ID
	session, err := store.Get(ctx, originalID)
	if err != nil {
		t.Fatalf("failed to get session by original ID: %v", err)
	}
	if session.ID != originalID {
		t.Errorf("expected session ID %s, got %s", originalID, session.ID)
	}
}

func TestFormatImportResult(t *testing.T) {
	result := &ImportResult{
		SessionsImported: 5,
		SessionsSkipped:  2,
		MessagesImported: 100,
		MessagesSkipped:  10,
		Duration:         500 * time.Millisecond,
		Errors:           []string{"error 1", "error 2"},
		Warnings:         []string{"warning 1"},
	}

	output := FormatImportResult(result)

	if !strings.Contains(output, "5 imported") {
		t.Error("expected output to contain session count")
	}
	if !strings.Contains(output, "100 imported") {
		t.Error("expected output to contain message count")
	}
	if !strings.Contains(output, "error 1") {
		t.Error("expected output to contain errors")
	}
	if !strings.Contains(output, "warning 1") {
		t.Error("expected output to contain warnings")
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
