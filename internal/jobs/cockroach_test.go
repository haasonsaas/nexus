package jobs

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
	now := time.Now()
	result := &models.ToolResult{ToolCallID: "call-1", Content: "result"}
	resultJSON, _ := json.Marshal(result)

	tests := []struct {
		name        string
		job         *Job
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful create",
			job: &Job{
				ID:         "job-1",
				ToolName:   "test-tool",
				ToolCallID: "call-1",
				Status:     StatusQueued,
				CreatedAt:  now,
				Result:     result,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO tool_jobs").
					WithArgs(
						"job-1",
						"test-tool",
						"call-1",
						"queued",
						sqlmock.AnyArg(), // created_at
						sqlmock.AnyArg(), // started_at
						sqlmock.AnyArg(), // finished_at
						resultJSON,
						sqlmock.AnyArg(), // error_message
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name: "nil job returns nil",
			job:  nil,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			wantErr: false,
		},
		{
			name: "database error",
			job: &Job{
				ID:        "job-1",
				ToolName:  "tool",
				Status:    StatusQueued,
				CreatedAt: now,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO tool_jobs").
					WillReturnError(errors.New("connection refused"))
			},
			wantErr:     true,
			errContains: "create job",
		},
		{
			name: "job with all fields",
			job: &Job{
				ID:         "job-2",
				ToolName:   "tool",
				ToolCallID: "call-2",
				Status:     StatusSucceeded,
				CreatedAt:  now,
				StartedAt:  now.Add(1 * time.Second),
				FinishedAt: now.Add(2 * time.Second),
				Result:     &models.ToolResult{ToolCallID: "call-2", Content: "done"},
				Error:      "",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO tool_jobs").
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name: "job with error",
			job: &Job{
				ID:         "job-3",
				ToolName:   "tool",
				ToolCallID: "call-3",
				Status:     StatusFailed,
				CreatedAt:  now,
				FinishedAt: now,
				Error:      "execution failed",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO tool_jobs").
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			err := store.Create(context.Background(), tt.job)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errContains != "" && err != nil {
					if !containsSubstring(err.Error(), tt.errContains) {
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

// TestCockroachStore_Update tests the Update method.
func TestCockroachStore_Update(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		job         *Job
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful update",
			job: &Job{
				ID:         "job-1",
				ToolName:   "test-tool",
				ToolCallID: "call-1",
				Status:     StatusRunning,
				CreatedAt:  now,
				StartedAt:  now,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE tool_jobs").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: false,
		},
		{
			name: "nil job returns nil",
			job:  nil,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			wantErr: false,
		},
		{
			name: "database error",
			job: &Job{
				ID:       "job-1",
				ToolName: "tool",
				Status:   StatusRunning,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE tool_jobs").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "update job",
		},
		{
			name: "update with result",
			job: &Job{
				ID:         "job-2",
				ToolName:   "tool",
				Status:     StatusSucceeded,
				FinishedAt: now,
				Result:     &models.ToolResult{ToolCallID: "call", Content: "done"},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE tool_jobs").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			err := store.Update(context.Background(), tt.job)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
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
	result := &models.ToolResult{ToolCallID: "call-1", Content: "result"}
	resultJSON, _ := json.Marshal(result)

	tests := []struct {
		name        string
		id          string
		setupMock   func(sqlmock.Sqlmock)
		wantJob     *Job
		wantErr     bool
		errContains string
	}{
		{
			name: "successful get",
			id:   "job-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tool_name", "tool_call_id", "status", "created_at",
					"started_at", "finished_at", "result", "error_message",
				}).AddRow(
					"job-1", "test-tool", "call-1", "succeeded", now,
					sql.NullTime{Time: now, Valid: true},
					sql.NullTime{Time: now, Valid: true},
					resultJSON,
					sql.NullString{String: "", Valid: false},
				)
				mock.ExpectQuery("SELECT .* FROM tool_jobs WHERE id").
					WithArgs("job-1").
					WillReturnRows(rows)
			},
			wantJob: &Job{
				ID:         "job-1",
				ToolName:   "test-tool",
				ToolCallID: "call-1",
				Status:     StatusSucceeded,
			},
			wantErr: false,
		},
		{
			name: "empty id returns nil",
			id:   "",
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			wantJob: nil,
			wantErr: false,
		},
		{
			name: "job not found",
			id:   "non-existent",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM tool_jobs WHERE id").
					WithArgs("non-existent").
					WillReturnError(sql.ErrNoRows)
			},
			wantJob: nil,
			wantErr: false,
		},
		{
			name: "database error",
			id:   "job-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM tool_jobs WHERE id").
					WithArgs("job-1").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "get job",
		},
		{
			name: "job with error field",
			id:   "job-2",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tool_name", "tool_call_id", "status", "created_at",
					"started_at", "finished_at", "result", "error_message",
				}).AddRow(
					"job-2", "tool", "call-2", "failed", now,
					sql.NullTime{Time: now, Valid: true},
					sql.NullTime{Time: now, Valid: true},
					nil,
					sql.NullString{String: "execution failed", Valid: true},
				)
				mock.ExpectQuery("SELECT .* FROM tool_jobs WHERE id").
					WithArgs("job-2").
					WillReturnRows(rows)
			},
			wantJob: &Job{
				ID:     "job-2",
				Status: StatusFailed,
				Error:  "execution failed",
			},
			wantErr: false,
		},
		{
			name: "job with null timestamps",
			id:   "job-3",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tool_name", "tool_call_id", "status", "created_at",
					"started_at", "finished_at", "result", "error_message",
				}).AddRow(
					"job-3", "tool", "call-3", "queued", now,
					sql.NullTime{Valid: false},
					sql.NullTime{Valid: false},
					nil,
					sql.NullString{Valid: false},
				)
				mock.ExpectQuery("SELECT .* FROM tool_jobs WHERE id").
					WithArgs("job-3").
					WillReturnRows(rows)
			},
			wantJob: &Job{
				ID:     "job-3",
				Status: StatusQueued,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			got, err := store.Get(context.Background(), tt.id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantJob == nil {
				if got != nil {
					t.Errorf("expected nil job, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected job, got nil")
			}
			if got.ID != tt.wantJob.ID {
				t.Errorf("ID mismatch: got %q, want %q", got.ID, tt.wantJob.ID)
			}
			if got.Status != tt.wantJob.Status {
				t.Errorf("Status mismatch: got %q, want %q", got.Status, tt.wantJob.Status)
			}
		})
	}
}

// TestCockroachStore_List tests the List method.
func TestCockroachStore_List(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		limit       int
		offset      int
		setupMock   func(sqlmock.Sqlmock)
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name:   "list with limit",
			limit:  5,
			offset: 0,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tool_name", "tool_call_id", "status", "created_at",
					"started_at", "finished_at", "result", "error_message",
				}).
					AddRow("job-1", "tool", "call-1", "succeeded", now, sql.NullTime{}, sql.NullTime{}, nil, sql.NullString{}).
					AddRow("job-2", "tool", "call-2", "running", now, sql.NullTime{}, sql.NullTime{}, nil, sql.NullString{})
				mock.ExpectQuery("SELECT .* FROM tool_jobs").
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:   "list with limit and offset",
			limit:  10,
			offset: 5,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tool_name", "tool_call_id", "status", "created_at",
					"started_at", "finished_at", "result", "error_message",
				})
				mock.ExpectQuery("SELECT .* FROM tool_jobs").
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:   "list all (no limit)",
			limit:  0,
			offset: 0,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tool_name", "tool_call_id", "status", "created_at",
					"started_at", "finished_at", "result", "error_message",
				}).
					AddRow("job-1", "tool", "call-1", "queued", now, sql.NullTime{}, sql.NullTime{}, nil, sql.NullString{})
				mock.ExpectQuery("SELECT .* FROM tool_jobs").
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:   "database error",
			limit:  10,
			offset: 0,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM tool_jobs").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "list jobs",
		},
		{
			name:   "scan error",
			limit:  10,
			offset: 0,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tool_name", "tool_call_id", "status", "created_at",
					"started_at", "finished_at", "result", "error_message",
				}).AddRow(
					"job-1", "tool", "call-1", "running", "invalid-time", // Invalid time format
					sql.NullTime{}, sql.NullTime{}, nil, sql.NullString{},
				)
				mock.ExpectQuery("SELECT .* FROM tool_jobs").
					WillReturnRows(rows)
			},
			wantErr:     true,
			errContains: "scan job",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			got, err := store.List(context.Background(), tt.limit, tt.offset)

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

// TestCockroachStore_Prune tests the Prune method.
func TestCockroachStore_Prune(t *testing.T) {
	tests := []struct {
		name        string
		olderThan   time.Duration
		setupMock   func(sqlmock.Sqlmock)
		wantPruned  int64
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful prune",
			olderThan: 24 * time.Hour,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("DELETE FROM tool_jobs WHERE created_at").
					WillReturnResult(sqlmock.NewResult(0, 5))
			},
			wantPruned: 5,
			wantErr:    false,
		},
		{
			name:      "no jobs to prune",
			olderThan: 24 * time.Hour,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("DELETE FROM tool_jobs WHERE created_at").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantPruned: 0,
			wantErr:    false,
		},
		{
			name:      "database error",
			olderThan: 24 * time.Hour,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("DELETE FROM tool_jobs WHERE created_at").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "prune jobs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			pruned, err := store.Prune(context.Background(), tt.olderThan)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if pruned != tt.wantPruned {
				t.Errorf("pruned count mismatch: got %d, want %d", pruned, tt.wantPruned)
			}
		})
	}
}

// TestCockroachStore_Cancel tests the Cancel method.
func TestCockroachStore_Cancel(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		setupMock   func(sqlmock.Sqlmock)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful cancel",
			id:   "job-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE tool_jobs").
					WithArgs("job-1", "failed", "job cancelled", sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: false,
		},
		{
			name: "empty id returns nil",
			id:   "",
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			wantErr: false,
		},
		{
			name: "job not found (no rows affected)",
			id:   "non-existent",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE tool_jobs").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantErr: false, // Cancel doesn't error for non-existent jobs
		},
		{
			name: "database error",
			id:   "job-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE tool_jobs").
					WillReturnError(errors.New("database error"))
			},
			wantErr:     true,
			errContains: "cancel job",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, store := setupMockDB(t)
			defer db.Close()

			tt.setupMock(mock)

			err := store.Cancel(context.Background(), tt.id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestCockroachStore_Close tests the Close method.
func TestCockroachStore_Close(t *testing.T) {
	t.Run("successful close", func(t *testing.T) {
		db, mock, store := setupMockDB(t)
		mock.ExpectClose()

		err := store.Close()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Verify db.Close was called
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}

		_ = db // suppress unused warning
	})

	t.Run("nil store", func(t *testing.T) {
		var store *CockroachStore
		err := store.Close()
		if err != nil {
			t.Errorf("expected nil error for nil store, got %v", err)
		}
	})

	t.Run("nil db", func(t *testing.T) {
		store := &CockroachStore{db: nil}
		err := store.Close()
		if err != nil {
			t.Errorf("expected nil error for nil db, got %v", err)
		}
	})
}

// TestDefaultCockroachConfig tests the default configuration.
func TestDefaultCockroachConfig(t *testing.T) {
	cfg := DefaultCockroachConfig()

	if cfg.MaxOpenConns != 10 {
		t.Errorf("MaxOpenConns = %d, want 10", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Errorf("MaxIdleConns = %d, want 5", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 5*time.Minute {
		t.Errorf("ConnMaxLifetime = %v, want 5m", cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != 2*time.Minute {
		t.Errorf("ConnMaxIdleTime = %v, want 2m", cfg.ConnMaxIdleTime)
	}
	if cfg.ConnectTimeout != 10*time.Second {
		t.Errorf("ConnectTimeout = %v, want 10s", cfg.ConnectTimeout)
	}
}

// TestNewCockroachStoreFromDSN_EmptyDSN tests error handling for empty DSN.
func TestNewCockroachStoreFromDSN_EmptyDSN(t *testing.T) {
	_, err := NewCockroachStoreFromDSN("", nil)
	if err == nil {
		t.Error("expected error for empty DSN")
	}
	if !containsSubstring(err.Error(), "dsn is required") {
		t.Errorf("expected error about dsn, got %v", err)
	}
}

// TestMarshalResult tests the marshalResult helper.
func TestMarshalResult(t *testing.T) {
	t.Run("nil result", func(t *testing.T) {
		data, err := marshalResult(nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if data != nil {
			t.Errorf("expected nil data, got %v", data)
		}
	})

	t.Run("valid result", func(t *testing.T) {
		result := &models.ToolResult{
			ToolCallID: "call-1",
			Content:    "result",
			IsError:    false,
		}
		data, err := marshalResult(result)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if data == nil {
			t.Error("expected non-nil data")
		}

		// Verify we can unmarshal it back
		var unmarshaled models.ToolResult
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Errorf("unmarshal error: %v", err)
		}
		if unmarshaled.ToolCallID != result.ToolCallID {
			t.Errorf("ToolCallID mismatch: got %q, want %q", unmarshaled.ToolCallID, result.ToolCallID)
		}
	})
}

// TestNullableString tests the nullableString helper.
func TestNullableString(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		ns := nullableString("")
		if ns.Valid {
			t.Error("expected Valid to be false for empty string")
		}
	})

	t.Run("non-empty string", func(t *testing.T) {
		ns := nullableString("hello")
		if !ns.Valid {
			t.Error("expected Valid to be true for non-empty string")
		}
		if ns.String != "hello" {
			t.Errorf("String = %q, want %q", ns.String, "hello")
		}
	})
}

// TestNullTime tests the nullTime helper.
func TestNullTime(t *testing.T) {
	t.Run("zero time", func(t *testing.T) {
		nt := nullTime(time.Time{})
		if nt.Valid {
			t.Error("expected Valid to be false for zero time")
		}
	})

	t.Run("non-zero time", func(t *testing.T) {
		now := time.Now()
		nt := nullTime(now)
		if !nt.Valid {
			t.Error("expected Valid to be true for non-zero time")
		}
		if !nt.Time.Equal(now) {
			t.Errorf("Time = %v, want %v", nt.Time, now)
		}
	})
}

// TestScanJob tests the scanJob function with different scenarios.
func TestScanJob(t *testing.T) {
	now := time.Now()
	result := &models.ToolResult{ToolCallID: "call-1", Content: "result"}
	resultJSON, _ := json.Marshal(result)

	t.Run("full job with all fields", func(t *testing.T) {
		db, mock, _ := setupMockDB(t)
		defer db.Close()

		rows := sqlmock.NewRows([]string{
			"id", "tool_name", "tool_call_id", "status", "created_at",
			"started_at", "finished_at", "result", "error_message",
		}).AddRow(
			"job-1", "tool", "call-1", "succeeded", now,
			sql.NullTime{Time: now, Valid: true},
			sql.NullTime{Time: now, Valid: true},
			resultJSON,
			sql.NullString{String: "", Valid: false},
		)
		mock.ExpectQuery("SELECT").WillReturnRows(rows)

		row := db.QueryRow("SELECT")
		job, err := scanJob(row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if job.ID != "job-1" {
			t.Errorf("ID = %q, want %q", job.ID, "job-1")
		}
		if job.Status != StatusSucceeded {
			t.Errorf("Status = %q, want %q", job.Status, StatusSucceeded)
		}
		if job.Result == nil {
			t.Error("expected non-nil Result")
		}
	})

	t.Run("job with invalid result JSON", func(t *testing.T) {
		db, mock, _ := setupMockDB(t)
		defer db.Close()

		rows := sqlmock.NewRows([]string{
			"id", "tool_name", "tool_call_id", "status", "created_at",
			"started_at", "finished_at", "result", "error_message",
		}).AddRow(
			"job-1", "tool", "call-1", "succeeded", now,
			sql.NullTime{Valid: false},
			sql.NullTime{Valid: false},
			[]byte("invalid json"),
			sql.NullString{Valid: false},
		)
		mock.ExpectQuery("SELECT").WillReturnRows(rows)

		row := db.QueryRow("SELECT")
		_, err := scanJob(row)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
		if !containsSubstring(err.Error(), "unmarshal job result") {
			t.Errorf("expected unmarshal error, got %v", err)
		}
	})
}

// TestCockroachStore_Store_Interface tests that CockroachStore implements Store interface.
func TestCockroachStore_Store_Interface(t *testing.T) {
	var _ Store = (*CockroachStore)(nil)
}

// containsSubstring is a helper function to check if a string contains a substring.
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
