package sessions

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// TestMemoryStore_Create tests the Create method thoroughly.
func TestMemoryStore_Create(t *testing.T) {
	tests := []struct {
		name        string
		session     *models.Session
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil session returns error",
			session:     nil,
			wantErr:     true,
			errContains: "session is required",
		},
		{
			name: "valid session without ID gets ID assigned",
			session: &models.Session{
				AgentID:   "agent-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-123",
				Key:       "agent-1:slack:user-123",
			},
			wantErr: false,
		},
		{
			name: "valid session with ID keeps ID",
			session: &models.Session{
				ID:        "custom-id",
				AgentID:   "agent-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-123",
				Key:       "agent-1:slack:user-123",
			},
			wantErr: false,
		},
		{
			name: "session with metadata",
			session: &models.Session{
				AgentID:   "agent-1",
				Channel:   models.ChannelDiscord,
				ChannelID: "user-456",
				Key:       "agent-1:discord:user-456",
				Metadata: map[string]any{
					"custom_field": "value",
					"count":        42,
				},
			},
			wantErr: false,
		},
		{
			name: "session with existing CreatedAt keeps it",
			session: &models.Session{
				AgentID:   "agent-1",
				Channel:   models.ChannelTelegram,
				ChannelID: "user-789",
				Key:       "agent-1:telegram:user-789",
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			err := store.Create(ctx, tt.session)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && err.Error() != tt.errContains {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify session was created
			if tt.session.ID == "" {
				t.Error("expected ID to be assigned")
			}
			if tt.session.CreatedAt.IsZero() {
				t.Error("expected CreatedAt to be set")
			}
			if tt.session.UpdatedAt.IsZero() {
				t.Error("expected UpdatedAt to be set")
			}

			// Verify we can retrieve it
			retrieved, err := store.Get(ctx, tt.session.ID)
			if err != nil {
				t.Fatalf("failed to retrieve created session: %v", err)
			}
			if retrieved.Key != tt.session.Key {
				t.Errorf("key mismatch: got %q, want %q", retrieved.Key, tt.session.Key)
			}
		})
	}
}

// TestMemoryStore_Get tests the Get method.
func TestMemoryStore_Get(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create a session first
	session := &models.Session{
		ID:        "test-session",
		AgentID:   "agent-1",
		Channel:   models.ChannelSlack,
		ChannelID: "user-123",
		Key:       "agent-1:slack:user-123",
		Title:     "Test Session",
		Metadata:  map[string]any{"foo": "bar"},
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name      string
		id        string
		wantErr   bool
		wantTitle string
	}{
		{
			name:      "existing session",
			id:        "test-session",
			wantErr:   false,
			wantTitle: "Test Session",
		},
		{
			name:    "non-existent session",
			id:      "non-existent",
			wantErr: true,
		},
		{
			name:    "empty id",
			id:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.Get(ctx, tt.id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Title != tt.wantTitle {
				t.Errorf("title mismatch: got %q, want %q", got.Title, tt.wantTitle)
			}
		})
	}
}

// TestMemoryStore_Get_ReturnsClone verifies that Get returns a copy, not the original.
func TestMemoryStore_Get_ReturnsClone(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{
		ID:       "test-session",
		AgentID:  "agent-1",
		Title:    "Original Title",
		Metadata: map[string]any{"key": "original"},
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	retrieved, _ := store.Get(ctx, "test-session")
	retrieved.Title = "Modified Title"
	retrieved.Metadata["key"] = "modified"

	// Original should be unchanged
	original, _ := store.Get(ctx, "test-session")
	if original.Title != "Original Title" {
		t.Error("modifying retrieved session affected the stored session")
	}
	if original.Metadata["key"] != "original" {
		t.Error("modifying retrieved metadata affected the stored metadata")
	}
}

// TestMemoryStore_Update tests the Update method.
func TestMemoryStore_Update(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{
		ID:      "test-session",
		AgentID: "agent-1",
		Title:   "Original Title",
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	originalCreatedAt := session.CreatedAt

	tests := []struct {
		name        string
		updateFn    func(*models.Session)
		wantErr     bool
		errContains string
	}{
		{
			name: "update title",
			updateFn: func(s *models.Session) {
				s.Title = "New Title"
			},
			wantErr: false,
		},
		{
			name: "update metadata",
			updateFn: func(s *models.Session) {
				s.Metadata = map[string]any{"new": "data"}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retrieved, _ := store.Get(ctx, "test-session")
			tt.updateFn(retrieved)

			err := store.Update(ctx, retrieved)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify changes persisted
			updated, _ := store.Get(ctx, "test-session")
			if updated.Title != retrieved.Title {
				t.Errorf("title not updated: got %q, want %q", updated.Title, retrieved.Title)
			}
			// CreatedAt should be preserved
			if !updated.CreatedAt.Equal(originalCreatedAt) {
				t.Error("CreatedAt was modified during update")
			}
			// UpdatedAt should be changed
			if updated.UpdatedAt.Equal(originalCreatedAt) {
				t.Error("UpdatedAt was not modified during update")
			}
		})
	}
}

// TestMemoryStore_Update_NilSession tests updating with nil.
func TestMemoryStore_Update_NilSession(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	err := store.Update(ctx, nil)
	if err == nil {
		t.Error("expected error for nil session")
	}
}

// TestMemoryStore_Update_NonExistent tests updating non-existent session.
func TestMemoryStore_Update_NonExistent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{
		ID:      "non-existent",
		AgentID: "agent-1",
	}
	err := store.Update(ctx, session)
	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

// TestMemoryStore_Delete tests the Delete method.
func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{
		ID:      "test-session",
		AgentID: "agent-1",
		Key:     "test-key",
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add a message
	msg := &models.Message{
		SessionID: "test-session",
		Role:      models.RoleUser,
		Content:   "hello",
	}
	if err := store.AppendMessage(ctx, "test-session", msg); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "delete existing session",
			id:      "test-session",
			wantErr: false,
		},
		{
			name:    "delete already deleted session",
			id:      "test-session",
			wantErr: true,
		},
		{
			name:    "delete non-existent session",
			id:      "non-existent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Delete(ctx, tt.id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify session is gone
			_, err = store.Get(ctx, tt.id)
			if err == nil {
				t.Error("session should not exist after delete")
			}

			// Verify key lookup is gone
			_, err = store.GetByKey(ctx, "test-key")
			if err == nil {
				t.Error("key lookup should fail after delete")
			}

			// Verify messages are gone
			history, err := store.GetHistory(ctx, tt.id, 10)
			if err != nil {
				t.Fatalf("GetHistory should not error: %v", err)
			}
			if len(history) != 0 {
				t.Error("messages should be deleted with session")
			}
		})
	}
}

// TestMemoryStore_GetByKey tests the GetByKey method.
func TestMemoryStore_GetByKey(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{
		ID:      "test-session",
		AgentID: "agent-1",
		Key:     "agent-1:slack:user-123",
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name    string
		key     string
		wantID  string
		wantErr bool
	}{
		{
			name:    "existing key",
			key:     "agent-1:slack:user-123",
			wantID:  "test-session",
			wantErr: false,
		},
		{
			name:    "non-existent key",
			key:     "non-existent-key",
			wantErr: true,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetByKey(ctx, tt.key)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("id mismatch: got %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

// TestMemoryStore_GetOrCreate tests the GetOrCreate method.
func TestMemoryStore_GetOrCreate(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// First call should create
	session1, err := store.GetOrCreate(ctx, "test-key", "agent-1", models.ChannelSlack, "user-123")
	if err != nil {
		t.Fatalf("first GetOrCreate failed: %v", err)
	}
	if session1.ID == "" {
		t.Error("expected ID to be assigned")
	}

	// Second call with same key should return existing
	session2, err := store.GetOrCreate(ctx, "test-key", "agent-1", models.ChannelSlack, "user-123")
	if err != nil {
		t.Fatalf("second GetOrCreate failed: %v", err)
	}
	if session2.ID != session1.ID {
		t.Error("expected same session to be returned")
	}

	// Different key should create new session
	session3, err := store.GetOrCreate(ctx, "different-key", "agent-1", models.ChannelSlack, "user-456")
	if err != nil {
		t.Fatalf("third GetOrCreate failed: %v", err)
	}
	if session3.ID == session1.ID {
		t.Error("expected different session for different key")
	}
}

// TestMemoryStore_List tests the List method.
func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create sessions for different agents and channels
	sessions := []*models.Session{
		{ID: "s1", AgentID: "agent-1", Channel: models.ChannelSlack, Key: "k1"},
		{ID: "s2", AgentID: "agent-1", Channel: models.ChannelSlack, Key: "k2"},
		{ID: "s3", AgentID: "agent-1", Channel: models.ChannelDiscord, Key: "k3"},
		{ID: "s4", AgentID: "agent-2", Channel: models.ChannelSlack, Key: "k4"},
		{ID: "s5", AgentID: "agent-2", Channel: models.ChannelTelegram, Key: "k5"},
	}
	for _, s := range sessions {
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	tests := []struct {
		name      string
		agentID   string
		opts      ListOptions
		wantCount int
	}{
		{
			name:      "all sessions for agent-1",
			agentID:   "agent-1",
			opts:      ListOptions{},
			wantCount: 3,
		},
		{
			name:      "all sessions for agent-2",
			agentID:   "agent-2",
			opts:      ListOptions{},
			wantCount: 2,
		},
		{
			name:    "filter by channel",
			agentID: "agent-1",
			opts: ListOptions{
				Channel: models.ChannelSlack,
			},
			wantCount: 2,
		},
		{
			name:    "with limit",
			agentID: "agent-1",
			opts: ListOptions{
				Limit: 2,
			},
			wantCount: 2,
		},
		{
			name:    "with offset",
			agentID: "agent-1",
			opts: ListOptions{
				Offset: 1,
			},
			wantCount: 2,
		},
		{
			name:    "with limit and offset",
			agentID: "agent-1",
			opts: ListOptions{
				Limit:  1,
				Offset: 1,
			},
			wantCount: 1,
		},
		{
			name:    "offset beyond count",
			agentID: "agent-1",
			opts: ListOptions{
				Offset: 100,
			},
			wantCount: 0,
		},
		{
			name:    "negative offset treated as zero",
			agentID: "agent-1",
			opts: ListOptions{
				Offset: -5,
			},
			wantCount: 3,
		},
		{
			name:      "empty agent ID returns all",
			agentID:   "",
			opts:      ListOptions{},
			wantCount: 5,
		},
		{
			name:      "non-existent agent",
			agentID:   "non-existent",
			opts:      ListOptions{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.List(ctx, tt.agentID, tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Errorf("count mismatch: got %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// TestMemoryStore_AppendMessage tests the AppendMessage method.
func TestMemoryStore_AppendMessage(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create a session
	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name        string
		sessionID   string
		message     *models.Message
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid message without ID",
			sessionID: "test-session",
			message: &models.Message{
				Role:    models.RoleUser,
				Content: "Hello",
			},
			wantErr: false,
		},
		{
			name:      "valid message with ID",
			sessionID: "test-session",
			message: &models.Message{
				ID:      "custom-msg-id",
				Role:    models.RoleAssistant,
				Content: "Hi there",
			},
			wantErr: false,
		},
		{
			name:      "message with metadata",
			sessionID: "test-session",
			message: &models.Message{
				Role:     models.RoleUser,
				Content:  "Test",
				Metadata: map[string]any{"source": "test"},
			},
			wantErr: false,
		},
		{
			name:      "message with attachments",
			sessionID: "test-session",
			message: &models.Message{
				Role:    models.RoleUser,
				Content: "See attached",
				Attachments: []models.Attachment{
					{ID: "att-1", Type: "image", URL: "http://example.com/img.png"},
				},
			},
			wantErr: false,
		},
		{
			name:      "message with tool calls",
			sessionID: "test-session",
			message: &models.Message{
				Role:    models.RoleAssistant,
				Content: "",
				ToolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "get_weather", Input: []byte(`{"city":"NYC"}`)},
				},
			},
			wantErr: false,
		},
		{
			name:      "message with tool results",
			sessionID: "test-session",
			message: &models.Message{
				Role: models.RoleTool,
				ToolResults: []models.ToolResult{
					{ToolCallID: "tc-1", Content: "Sunny, 72F"},
				},
			},
			wantErr: false,
		},
		{
			name:        "nil message",
			sessionID:   "test-session",
			message:     nil,
			wantErr:     true,
			errContains: "message is required",
		},
		{
			name:      "non-existent session",
			sessionID: "non-existent",
			message: &models.Message{
				Role:    models.RoleUser,
				Content: "Hello",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.AppendMessage(ctx, tt.sessionID, tt.message)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify message was stored (the implementation clones, so we check history)
			history, _ := store.GetHistory(ctx, tt.sessionID, 100)
			found := false
			for _, msg := range history {
				if msg.ID != "" && msg.Content == tt.message.Content {
					found = true
					break
				}
			}
			if !found && tt.message != nil {
				t.Error("expected message to be stored")
			}
		})
	}
}

// TestMemoryStore_GetHistory tests the GetHistory method.
func TestMemoryStore_GetHistory(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add multiple messages
	for i := 0; i < 10; i++ {
		msg := &models.Message{
			Role:    models.RoleUser,
			Content: "Message " + string(rune('0'+i)),
		}
		if err := store.AppendMessage(ctx, "test-session", msg); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	tests := []struct {
		name      string
		sessionID string
		limit     int
		wantCount int
		wantFirst string // Content of first message
		wantLast  string // Content of last message
	}{
		{
			name:      "get all with limit 0",
			sessionID: "test-session",
			limit:     0,
			wantCount: 10,
			wantFirst: "Message 0",
			wantLast:  "Message 9",
		},
		{
			name:      "get last 5",
			sessionID: "test-session",
			limit:     5,
			wantCount: 5,
			wantFirst: "Message 5",
			wantLast:  "Message 9",
		},
		{
			name:      "limit larger than count",
			sessionID: "test-session",
			limit:     100,
			wantCount: 10,
		},
		{
			name:      "non-existent session returns empty",
			sessionID: "non-existent",
			limit:     10,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history, err := store.GetHistory(ctx, tt.sessionID, tt.limit)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(history) != tt.wantCount {
				t.Errorf("count mismatch: got %d, want %d", len(history), tt.wantCount)
			}
			if tt.wantFirst != "" && len(history) > 0 {
				if history[0].Content != tt.wantFirst {
					t.Errorf("first message mismatch: got %q, want %q", history[0].Content, tt.wantFirst)
				}
			}
			if tt.wantLast != "" && len(history) > 0 {
				if history[len(history)-1].Content != tt.wantLast {
					t.Errorf("last message mismatch: got %q, want %q", history[len(history)-1].Content, tt.wantLast)
				}
			}
		})
	}
}

// TestMemoryStore_GetHistory_ReturnsClones verifies that GetHistory returns copies.
func TestMemoryStore_GetHistory_ReturnsClones(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	msg := &models.Message{
		Role:     models.RoleUser,
		Content:  "Original",
		Metadata: map[string]any{"key": "original"},
	}
	if err := store.AppendMessage(ctx, "test-session", msg); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	history1, _ := store.GetHistory(ctx, "test-session", 10)
	history1[0].Content = "Modified"
	history1[0].Metadata["key"] = "modified"

	history2, _ := store.GetHistory(ctx, "test-session", 10)
	if history2[0].Content != "Original" {
		t.Error("modifying returned history affected stored messages")
	}
	if history2[0].Metadata["key"] != "original" {
		t.Error("modifying returned metadata affected stored messages")
	}
}

// TestMemoryStore_Concurrency tests thread safety.
func TestMemoryStore_Concurrency(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create initial session
	session := &models.Session{ID: "concurrent-session", AgentID: "agent-1", Key: "key"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Get(ctx, "concurrent-session")
			if err != nil {
				errChan <- err
			}
		}()
	}

	// Concurrent writes
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := &models.Message{
				Role:    models.RoleUser,
				Content: "Message",
			}
			err := store.AppendMessage(ctx, "concurrent-session", msg)
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	// Concurrent GetOrCreate
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.GetOrCreate(ctx, "key", "agent-1", models.ChannelSlack, "user")
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// Verify final state
	history, _ := store.GetHistory(ctx, "concurrent-session", 0)
	if len(history) != 20 {
		t.Errorf("expected 20 messages after concurrent writes, got %d", len(history))
	}
}

// TestSessionKey tests the SessionKey function.
func TestSessionKey(t *testing.T) {
	tests := []struct {
		name      string
		agentID   string
		channel   models.ChannelType
		channelID string
		want      string
	}{
		{
			name:      "slack session",
			agentID:   "agent-1",
			channel:   models.ChannelSlack,
			channelID: "U12345",
			want:      "agent-1:slack:U12345",
		},
		{
			name:      "telegram session",
			agentID:   "my-agent",
			channel:   models.ChannelTelegram,
			channelID: "123456789",
			want:      "my-agent:telegram:123456789",
		},
		{
			name:      "discord session",
			agentID:   "bot",
			channel:   models.ChannelDiscord,
			channelID: "guild:channel",
			want:      "bot:discord:guild:channel",
		},
		{
			name:      "empty values",
			agentID:   "",
			channel:   "",
			channelID: "",
			want:      "::",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionKey(tt.agentID, tt.channel, tt.channelID)
			if got != tt.want {
				t.Errorf("SessionKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
