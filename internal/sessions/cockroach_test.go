package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/haasonsaas/nexus/pkg/models"
)

// setupMockDB creates a new mock database for testing.
func setupMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *CockroachStore) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}

	store := &CockroachStore{db: db}
	return db, mock, store
}

// TestCockroachStore_Create tests the Create method.
func TestCockroachStore_Create(t *testing.T) {
	tests := []struct {
		name        string
		session     *models.Session
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful create",
			session: &models.Session{
				ID:        "session-1",
				AgentID:   "agent-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-123",
				Key:       "agent-1:slack:user-123",
				Title:     "Test Session",
				Metadata:  map[string]any{"foo": "bar"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO sessions")
				mock.ExpectExec("INSERT INTO sessions").
					WithArgs(
						"session-1",
						"agent-1",
						models.ChannelSlack,
						"user-123",
						"agent-1:slack:user-123",
						"Test Session",
						sqlmock.AnyArg(), // metadata JSON
						sqlmock.AnyArg(), // created_at
						sqlmock.AnyArg(), // updated_at
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name: "missing session ID returns error",
			session: &models.Session{
				AgentID:   "agent-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-123",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO sessions")
			},
			wantErr:     true,
			errContains: "session ID is required",
		},
		{
			name: "database error",
			session: &models.Session{
				ID:        "session-1",
				AgentID:   "agent-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-123",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO sessions")
				mock.ExpectExec("INSERT INTO sessions").
					WillReturnError(errors.New("connection refused"))
			},
			wantErr:     true,
			errContains: "failed to create session",
		},
		{
			name: "session with nil metadata",
			session: &models.Session{
				ID:        "session-2",
				AgentID:   "agent-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-456",
				Key:       "key-2",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO sessions")
				mock.ExpectExec("INSERT INTO sessions").
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, _ := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			// Need to manually create store with prepared statements for this test
			store := &CockroachStore{db: db}

			// Prepare the statement (this is what the real code does)
			stmt, err := db.Prepare(`
				INSERT INTO sessions (id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			`)
			if err != nil {
				t.Fatalf("failed to prepare statement: %v", err)
			}
			store.stmtCreateSession = stmt

			err = store.Create(context.Background(), tt.session)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

// TestCockroachStore_Get tests the Get method.
func TestCockroachStore_Get(t *testing.T) {
	now := time.Now()
	metadata := map[string]any{"key": "value"}
	metadataJSON, _ := json.Marshal(metadata)

	tests := []struct {
		name        string
		id          string
		setupMock   func(sqlmock.Sqlmock)
		wantSession *models.Session
		wantErr     bool
		errContains string
	}{
		{
			name: "successful get",
			id:   "session-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM sessions WHERE id")
				rows := sqlmock.NewRows([]string{
					"id", "agent_id", "channel", "channel_id", "key", "title", "metadata", "created_at", "updated_at",
				}).AddRow(
					"session-1", "agent-1", "slack", "user-123", "key-1", "Test Session", metadataJSON, now, now,
				)
				mock.ExpectQuery("SELECT .* FROM sessions WHERE id").
					WithArgs("session-1").
					WillReturnRows(rows)
			},
			wantSession: &models.Session{
				ID:        "session-1",
				AgentID:   "agent-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-123",
				Key:       "key-1",
				Title:     "Test Session",
			},
			wantErr: false,
		},
		{
			name: "session not found",
			id:   "non-existent",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM sessions WHERE id")
				mock.ExpectQuery("SELECT .* FROM sessions WHERE id").
					WithArgs("non-existent").
					WillReturnError(sql.ErrNoRows)
			},
			wantErr:     true,
			errContains: "session not found",
		},
		{
			name: "database error",
			id:   "session-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM sessions WHERE id")
				mock.ExpectQuery("SELECT .* FROM sessions WHERE id").
					WithArgs("session-1").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "failed to get session",
		},
		{
			name: "empty metadata",
			id:   "session-2",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM sessions WHERE id")
				rows := sqlmock.NewRows([]string{
					"id", "agent_id", "channel", "channel_id", "key", "title", "metadata", "created_at", "updated_at",
				}).AddRow(
					"session-2", "agent-1", "slack", "user-456", "key-2", "", nil, now, now,
				)
				mock.ExpectQuery("SELECT .* FROM sessions WHERE id").
					WithArgs("session-2").
					WillReturnRows(rows)
			},
			wantSession: &models.Session{
				ID: "session-2",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, _ := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			store := &CockroachStore{db: db}
			stmt, err := db.Prepare(`
				SELECT id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at
				FROM sessions WHERE id = $1
			`)
			if err != nil {
				t.Fatalf("failed to prepare statement: %v", err)
			}
			store.stmtGetSession = stmt

			got, err := store.Get(context.Background(), tt.id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.ID != tt.wantSession.ID {
				t.Errorf("ID mismatch: got %q, want %q", got.ID, tt.wantSession.ID)
			}
		})
	}
}

// TestCockroachStore_Update tests the Update method.
func TestCockroachStore_Update(t *testing.T) {
	tests := []struct {
		name        string
		session     *models.Session
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful update",
			session: &models.Session{
				ID:       "session-1",
				Title:    "Updated Title",
				Metadata: map[string]any{"updated": true},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("UPDATE sessions")
				mock.ExpectExec("UPDATE sessions").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: false,
		},
		{
			name: "session not found",
			session: &models.Session{
				ID:    "non-existent",
				Title: "Title",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("UPDATE sessions")
				mock.ExpectExec("UPDATE sessions").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantErr:     true,
			errContains: "session not found",
		},
		{
			name: "database error",
			session: &models.Session{
				ID:    "session-1",
				Title: "Title",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("UPDATE sessions")
				mock.ExpectExec("UPDATE sessions").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "failed to update session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, _ := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			store := &CockroachStore{db: db}
			stmt, err := db.Prepare("UPDATE sessions SET title = $1, metadata = $2, updated_at = $3 WHERE id = $4")
			if err != nil {
				t.Fatalf("failed to prepare statement: %v", err)
			}
			store.stmtUpdateSession = stmt

			err = store.Update(context.Background(), tt.session)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCockroachStore_Delete tests the Delete method.
func TestCockroachStore_Delete(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful delete",
			id:   "session-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("DELETE FROM sessions")
				mock.ExpectExec("DELETE FROM sessions").
					WithArgs("session-1").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: false,
		},
		{
			name: "session not found",
			id:   "non-existent",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("DELETE FROM sessions")
				mock.ExpectExec("DELETE FROM sessions").
					WithArgs("non-existent").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantErr:     true,
			errContains: "session not found",
		},
		{
			name: "database error",
			id:   "session-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("DELETE FROM sessions")
				mock.ExpectExec("DELETE FROM sessions").
					WithArgs("session-1").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "failed to delete session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, _ := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			store := &CockroachStore{db: db}
			stmt, err := db.Prepare("DELETE FROM sessions WHERE id = $1")
			if err != nil {
				t.Fatalf("failed to prepare statement: %v", err)
			}
			store.stmtDeleteSession = stmt

			err = store.Delete(context.Background(), tt.id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCockroachStore_GetByKey tests the GetByKey method.
func TestCockroachStore_GetByKey(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		key         string
		setupMock   func(sqlmock.Sqlmock)
		wantID      string
		wantErr     bool
		errContains string
	}{
		{
			name: "successful get by key",
			key:  "agent-1:slack:user-123",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM sessions WHERE key")
				rows := sqlmock.NewRows([]string{
					"id", "agent_id", "channel", "channel_id", "key", "title", "metadata", "created_at", "updated_at",
				}).AddRow(
					"session-1", "agent-1", "slack", "user-123", "agent-1:slack:user-123", "Title", nil, now, now,
				)
				mock.ExpectQuery("SELECT .* FROM sessions WHERE key").
					WithArgs("agent-1:slack:user-123").
					WillReturnRows(rows)
			},
			wantID:  "session-1",
			wantErr: false,
		},
		{
			name: "key not found",
			key:  "non-existent-key",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM sessions WHERE key")
				mock.ExpectQuery("SELECT .* FROM sessions WHERE key").
					WithArgs("non-existent-key").
					WillReturnError(sql.ErrNoRows)
			},
			wantErr:     true,
			errContains: "session not found with key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, _ := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			store := &CockroachStore{db: db}
			stmt, err := db.Prepare(`
				SELECT id, agent_id, channel, channel_id, key, title, metadata, created_at, updated_at
				FROM sessions WHERE key = $1
			`)
			if err != nil {
				t.Fatalf("failed to prepare statement: %v", err)
			}
			store.stmtGetByKey = stmt

			got, err := store.GetByKey(context.Background(), tt.key)

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
				t.Errorf("ID mismatch: got %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

// TestCockroachStore_List tests the List method.
func TestCockroachStore_List(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		agentID     string
		opts        ListOptions
		setupMock   func(sqlmock.Sqlmock)
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:    "list all for agent",
			agentID: "agent-1",
			opts:    ListOptions{},
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "agent_id", "channel", "channel_id", "key", "title", "metadata", "created_at", "updated_at",
				}).
					AddRow("s1", "agent-1", "slack", "u1", "k1", "", nil, now, now).
					AddRow("s2", "agent-1", "slack", "u2", "k2", "", nil, now, now)
				mock.ExpectQuery("SELECT .* FROM sessions").
					WithArgs("agent-1").
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:    "list with channel filter",
			agentID: "agent-1",
			opts:    ListOptions{Channel: models.ChannelSlack},
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "agent_id", "channel", "channel_id", "key", "title", "metadata", "created_at", "updated_at",
				}).
					AddRow("s1", "agent-1", "slack", "u1", "k1", "", nil, now, now)
				mock.ExpectQuery("SELECT .* FROM sessions").
					WithArgs("agent-1", models.ChannelSlack).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:    "list with limit",
			agentID: "agent-1",
			opts:    ListOptions{Limit: 5},
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "agent_id", "channel", "channel_id", "key", "title", "metadata", "created_at", "updated_at",
				})
				mock.ExpectQuery("SELECT .* FROM sessions").
					WithArgs("agent-1", 5).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:    "list with limit and offset",
			agentID: "agent-1",
			opts:    ListOptions{Limit: 10, Offset: 5},
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "agent_id", "channel", "channel_id", "key", "title", "metadata", "created_at", "updated_at",
				})
				mock.ExpectQuery("SELECT .* FROM sessions").
					WithArgs("agent-1", 10, 5).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:    "database error",
			agentID: "agent-1",
			opts:    ListOptions{},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM sessions").
					WithArgs("agent-1").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "failed to list sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			got, err := store.List(context.Background(), tt.agentID, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("count mismatch: got %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// TestCockroachStore_AppendMessage tests the AppendMessage method.
func TestCockroachStore_AppendMessage(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		sessionID   string
		message     *models.Message
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful append",
			sessionID: "session-1",
			message: &models.Message{
				ID:        "msg-1",
				Channel:   models.ChannelSlack,
				ChannelID: "user-123",
				Direction: models.DirectionInbound,
				Role:      models.RoleUser,
				Content:   "Hello",
				CreatedAt: now,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO messages")
				mock.ExpectBegin()
				mock.ExpectExec("INSERT INTO messages").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE sessions").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			wantErr: false,
		},
		{
			name:      "missing message ID returns error",
			sessionID: "session-1",
			message: &models.Message{
				Role:    models.RoleUser,
				Content: "Hello",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO messages")
			},
			wantErr:     true,
			errContains: "message ID is required",
		},
		{
			name:      "database error on insert",
			sessionID: "session-1",
			message: &models.Message{
				ID:        "msg-1",
				Role:      models.RoleUser,
				Content:   "Hello",
				CreatedAt: now,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO messages")
				mock.ExpectBegin()
				mock.ExpectExec("INSERT INTO messages").
					WillReturnError(errors.New("database error"))
				mock.ExpectRollback()
			},
			wantErr:     true,
			errContains: "failed to append message",
		},
		{
			name:      "message with attachments",
			sessionID: "session-1",
			message: &models.Message{
				ID:      "msg-2",
				Role:    models.RoleUser,
				Content: "See attached",
				Attachments: []models.Attachment{
					{ID: "att-1", Type: "image", URL: "http://example.com/img.png"},
				},
				CreatedAt: now,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO messages")
				mock.ExpectBegin()
				mock.ExpectExec("INSERT INTO messages").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE sessions").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			wantErr: false,
		},
		{
			name:      "message with tool calls",
			sessionID: "session-1",
			message: &models.Message{
				ID:   "msg-3",
				Role: models.RoleAssistant,
				ToolCalls: []models.ToolCall{
					{ID: "tc-1", Name: "weather", Input: []byte(`{}`)},
				},
				CreatedAt: now,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("INSERT INTO messages")
				mock.ExpectBegin()
				mock.ExpectExec("INSERT INTO messages").
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE sessions").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			stmt, err := db.Prepare(`
				INSERT INTO messages (id, session_id, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			`)
			if err != nil {
				t.Fatalf("failed to prepare statement: %v", err)
			}
			store.stmtAppendMessage = stmt

			err = store.AppendMessage(context.Background(), tt.sessionID, tt.message)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCockroachStore_GetHistory tests the GetHistory method.
func TestCockroachStore_GetHistory(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		sessionID   string
		limit       int
		setupMock   func(sqlmock.Sqlmock)
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful get history",
			sessionID: "session-1",
			limit:     10,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM messages")
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "channel", "channel_id", "direction", "role", "content",
					"attachments", "tool_calls", "tool_results", "metadata", "created_at",
				}).
					AddRow("m2", "session-1", "slack", "u1", "inbound", "user", "World", nil, nil, nil, nil, now).
					AddRow("m1", "session-1", "slack", "u1", "inbound", "user", "Hello", nil, nil, nil, nil, now.Add(-time.Minute))
				mock.ExpectQuery("SELECT .* FROM messages").
					WithArgs("session-1", 10).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "zero limit uses default",
			sessionID: "session-1",
			limit:     0,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM messages")
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "channel", "channel_id", "direction", "role", "content",
					"attachments", "tool_calls", "tool_results", "metadata", "created_at",
				})
				mock.ExpectQuery("SELECT .* FROM messages").
					WithArgs("session-1", 100). // Default limit
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "database error",
			sessionID: "session-1",
			limit:     10,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM messages")
				mock.ExpectQuery("SELECT .* FROM messages").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "failed to get history",
		},
		{
			name:      "messages with JSON fields",
			sessionID: "session-1",
			limit:     10,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPrepare("SELECT .* FROM messages")
				attachmentsJSON, _ := json.Marshal([]models.Attachment{{ID: "a1", Type: "image"}})
				toolCallsJSON, _ := json.Marshal([]models.ToolCall{{ID: "tc1", Name: "test"}})
				toolResultsJSON, _ := json.Marshal([]models.ToolResult{{ToolCallID: "tc1", Content: "result"}})
				metadataJSON, _ := json.Marshal(map[string]any{"key": "value"})

				rows := sqlmock.NewRows([]string{
					"id", "session_id", "channel", "channel_id", "direction", "role", "content",
					"attachments", "tool_calls", "tool_results", "metadata", "created_at",
				}).
					AddRow("m1", "session-1", "slack", "u1", "inbound", "user", "Hello",
						attachmentsJSON, toolCallsJSON, toolResultsJSON, metadataJSON, now)
				mock.ExpectQuery("SELECT .* FROM messages").
					WithArgs("session-1", 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			stmt, err := db.Prepare(`
				SELECT id, session_id, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at
				FROM messages WHERE session_id = $1
				ORDER BY created_at DESC
				LIMIT $2
			`)
			if err != nil {
				t.Fatalf("failed to prepare statement: %v", err)
			}
			store.stmtGetHistory = stmt

			got, err := store.GetHistory(context.Background(), tt.sessionID, tt.limit)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("count mismatch: got %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// TestCockroachStore_Close tests the Close method.
func TestCockroachStore_Close(t *testing.T) {
	db, mock, store := setupMockDB(t)

	// Prepare some statements
	mock.ExpectPrepare("SELECT 1")
	mock.ExpectPrepare("SELECT 2")

	stmt1, _ := db.Prepare("SELECT 1")
	stmt2, _ := db.Prepare("SELECT 2")

	store.stmtGetSession = stmt1
	store.stmtCreateSession = stmt2

	mock.ExpectClose()

	err := store.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

// TestCockroachConfig tests configuration handling.
func TestCockroachConfig(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := DefaultCockroachConfig()

		if cfg.Host != "localhost" {
			t.Errorf("expected host localhost, got %s", cfg.Host)
		}
		if cfg.Port != 26257 {
			t.Errorf("expected port 26257, got %d", cfg.Port)
		}
		if cfg.User != "root" {
			t.Errorf("expected user root, got %s", cfg.User)
		}
		if cfg.Database != "nexus" {
			t.Errorf("expected database nexus, got %s", cfg.Database)
		}
		if cfg.SSLMode != "disable" {
			t.Errorf("expected sslmode disable, got %s", cfg.SSLMode)
		}
		if cfg.MaxOpenConns != 25 {
			t.Errorf("expected max open conns 25, got %d", cfg.MaxOpenConns)
		}
		if cfg.MaxIdleConns != 5 {
			t.Errorf("expected max idle conns 5, got %d", cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetime != 5*time.Minute {
			t.Errorf("expected conn max lifetime 5m, got %v", cfg.ConnMaxLifetime)
		}
	})
}

// TestNewCockroachStoreFromDSN_EmptyDSN tests error handling for empty DSN.
func TestNewCockroachStoreFromDSN_EmptyDSN(t *testing.T) {
	_, err := NewCockroachStoreFromDSN("", nil)
	if err == nil {
		t.Error("expected error for empty DSN")
	}
	if !contains(err.Error(), "dsn is required") {
		t.Errorf("expected error about dsn, got %v", err)
	}
}

// contains is a helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
