package sessions

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestNewCockroachBranchStore(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	store := NewCockroachBranchStore(db)
	if store == nil {
		t.Error("expected non-nil store")
	}
	if store.db != db {
		t.Error("expected db to be set")
	}
}

func TestCockroachBranchStore_CreateBranch(t *testing.T) {
	tests := []struct {
		name      string
		branch    *models.Branch
		setupMock func(sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name: "successful create with all fields",
			branch: &models.Branch{
				ID:        "branch-1",
				SessionID: "session-1",
				Name:      "main",
				IsPrimary: true,
				Status:    models.BranchStatusActive,
				Metadata:  map[string]interface{}{"key": "value"},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO branches").
					WithArgs(
						"branch-1", "session-1", nil, "main", "",
						int64(0), models.BranchStatusActive, true,
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name: "create generates ID if empty",
			branch: &models.Branch{
				SessionID: "session-1",
				Name:      "feature",
				Status:    models.BranchStatusActive,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO branches").
					WithArgs(
						sqlmock.AnyArg(), "session-1", nil, "feature", "",
						int64(0), models.BranchStatusActive, false,
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name: "database error",
			branch: &models.Branch{
				ID:        "branch-1",
				SessionID: "session-1",
				Name:      "main",
				Status:    models.BranchStatusActive,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO branches").
					WillReturnError(errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			err = store.CreateBranch(context.Background(), tt.branch)

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateBranch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_GetBranch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		branchID  string
		setupMock func(sqlmock.Sqlmock)
		wantErr   error
	}{
		{
			name:     "successful get",
			branchID: "branch-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"branch-1", "session-1", nil, "main", "desc",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("branch-1").
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name:     "branch not found",
			branchID: "nonexistent",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("nonexistent").
					WillReturnError(sql.ErrNoRows)
			},
			wantErr: ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			_, err = store.GetBranch(context.Background(), tt.branchID)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("GetBranch() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("GetBranch() unexpected error = %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_UpdateBranch(t *testing.T) {
	tests := []struct {
		name      string
		branch    *models.Branch
		setupMock func(sqlmock.Sqlmock)
		wantErr   error
	}{
		{
			name: "successful update",
			branch: &models.Branch{
				ID:          "branch-1",
				Name:        "updated-name",
				Description: "updated desc",
				Status:      models.BranchStatusMerged,
				Metadata:    map[string]interface{}{},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE branches SET").
					WithArgs(
						"updated-name", "updated desc", models.BranchStatusMerged,
						sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "branch-1",
					).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: nil,
		},
		{
			name: "branch not found",
			branch: &models.Branch{
				ID:     "nonexistent",
				Name:   "name",
				Status: models.BranchStatusActive,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE branches SET").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantErr: ErrBranchNotFound,
		},
		{
			name: "database error",
			branch: &models.Branch{
				ID:     "branch-1",
				Name:   "name",
				Status: models.BranchStatusActive,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE branches SET").
					WillReturnError(errors.New("db error"))
			},
			wantErr: errors.New("db error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			err = store.UpdateBranch(context.Background(), tt.branch)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("UpdateBranch() expected error, got nil")
				}
			} else if err != nil {
				t.Errorf("UpdateBranch() unexpected error = %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_DeleteBranch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		branchID       string
		deleteMessages bool
		setupMock      func(sqlmock.Sqlmock)
		wantErr        error
	}{
		{
			name:           "delete without messages",
			branchID:       "branch-1",
			deleteMessages: false,
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetBranch
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"branch-1", "session-1", nil, "feature", "",
					int64(0), models.BranchStatusActive, false, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("branch-1").
					WillReturnRows(rows)

				// Transaction
				mock.ExpectBegin()
				mock.ExpectExec("DELETE FROM branches WHERE id").
					WithArgs("branch-1").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			wantErr: nil,
		},
		{
			name:           "delete with messages",
			branchID:       "branch-2",
			deleteMessages: true,
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetBranch
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"branch-2", "session-1", nil, "feature", "",
					int64(0), models.BranchStatusActive, false, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("branch-2").
					WillReturnRows(rows)

				// Transaction
				mock.ExpectBegin()
				mock.ExpectExec("DELETE FROM messages WHERE branch_id").
					WithArgs("branch-2").
					WillReturnResult(sqlmock.NewResult(0, 5))
				mock.ExpectExec("DELETE FROM branches WHERE id").
					WithArgs("branch-2").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			wantErr: nil,
		},
		{
			name:           "cannot delete primary branch",
			branchID:       "primary-branch",
			deleteMessages: false,
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetBranch
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"primary-branch", "session-1", nil, "main", "",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("primary-branch").
					WillReturnRows(rows)
			},
			wantErr: ErrCannotDeletePrimary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			err = store.DeleteBranch(context.Background(), tt.branchID, tt.deleteMessages)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("DeleteBranch() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("DeleteBranch() unexpected error = %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_GetPrimaryBranch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		sessionID string
		setupMock func(sqlmock.Sqlmock)
		wantErr   error
	}{
		{
			name:      "successful get",
			sessionID: "session-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"primary-branch", "session-1", nil, "main", "",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE session_id .* AND is_primary").
					WithArgs("session-1").
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name:      "no primary branch",
			sessionID: "session-2",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM branches WHERE session_id .* AND is_primary").
					WithArgs("session-2").
					WillReturnError(sql.ErrNoRows)
			},
			wantErr: ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			_, err = store.GetPrimaryBranch(context.Background(), tt.sessionID)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("GetPrimaryBranch() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("GetPrimaryBranch() unexpected error = %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_ListBranches(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		sessionID string
		opts      BranchListOptions
		setupMock func(sqlmock.Sqlmock)
		wantCount int
		wantErr   bool
	}{
		{
			name:      "list all branches",
			sessionID: "session-1",
			opts:      BranchListOptions{IncludeArchived: true},
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).
					AddRow("branch-1", "session-1", nil, "main", "", int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil).
					AddRow("branch-2", "session-1", nil, "feature", "", int64(0), models.BranchStatusActive, false, []byte("{}"), now, now, nil)
				mock.ExpectQuery("SELECT .* FROM branches WHERE session_id").
					WithArgs("session-1").
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "list with status filter",
			sessionID: "session-1",
			opts: BranchListOptions{
				Status:          ptrBranchStatus(models.BranchStatusActive),
				IncludeArchived: true,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow("branch-1", "session-1", nil, "main", "", int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil)
				mock.ExpectQuery("SELECT .* FROM branches WHERE session_id .* AND status").
					WithArgs("session-1", models.BranchStatusActive).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "list with limit and offset",
			sessionID: "session-1",
			opts: BranchListOptions{
				Limit:           10,
				Offset:          5,
				IncludeArchived: true,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				})
				mock.ExpectQuery("SELECT .* FROM branches WHERE session_id .* LIMIT .* OFFSET").
					WithArgs("session-1", 10, 5).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "database error",
			sessionID: "session-1",
			opts:      BranchListOptions{IncludeArchived: true},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM branches WHERE session_id").
					WillReturnError(errors.New("db error"))
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			branches, err := store.ListBranches(context.Background(), tt.sessionID, tt.opts)

			if (err != nil) != tt.wantErr {
				t.Errorf("ListBranches() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(branches) != tt.wantCount {
				t.Errorf("ListBranches() count = %d, want %d", len(branches), tt.wantCount)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_ForkBranch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		parentBranchID string
		branchPoint    int64
		branchName     string
		setupMock      func(sqlmock.Sqlmock)
		wantErr        bool
	}{
		{
			name:           "successful fork",
			parentBranchID: "parent-branch",
			branchPoint:    5,
			branchName:     "feature",
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetBranch for parent
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"parent-branch", "session-1", nil, "main", "",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("parent-branch").
					WillReturnRows(rows)

				// CreateBranch
				mock.ExpectExec("INSERT INTO branches").
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantErr: false,
		},
		{
			name:           "parent not found",
			parentBranchID: "nonexistent",
			branchPoint:    5,
			branchName:     "feature",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("nonexistent").
					WillReturnError(sql.ErrNoRows)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			_, err = store.ForkBranch(context.Background(), tt.parentBranchID, tt.branchPoint, tt.branchName)

			if (err != nil) != tt.wantErr {
				t.Errorf("ForkBranch() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_ArchiveBranch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		branchID  string
		setupMock func(sqlmock.Sqlmock)
		wantErr   error
	}{
		{
			name:     "successful archive",
			branchID: "feature-branch",
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetBranch
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"feature-branch", "session-1", nil, "feature", "",
					int64(0), models.BranchStatusActive, false, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("feature-branch").
					WillReturnRows(rows)

				// UpdateBranch
				mock.ExpectExec("UPDATE branches SET").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: nil,
		},
		{
			name:     "cannot archive primary",
			branchID: "primary-branch",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"primary-branch", "session-1", nil, "main", "",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("primary-branch").
					WillReturnRows(rows)
			},
			wantErr: ErrCannotDeletePrimary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			err = store.ArchiveBranch(context.Background(), tt.branchID)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ArchiveBranch() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("ArchiveBranch() unexpected error = %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_GetBranchStats(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		branchID  string
		setupMock func(sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name:     "successful get stats",
			branchID: "branch-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Own messages count
				mock.ExpectQuery("SELECT COUNT.*FROM messages WHERE branch_id").
					WithArgs("branch-1").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

				// Child branch count
				mock.ExpectQuery("SELECT COUNT.*FROM branches WHERE parent_branch_id").
					WithArgs("branch-1").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

				// Total messages (recursive CTE)
				mock.ExpectQuery("WITH RECURSIVE branch_path AS").
					WithArgs("branch-1", 5).
					WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(10))

				// Last message timestamp
				mock.ExpectQuery("SELECT MAX.*FROM messages WHERE branch_id").
					WithArgs("branch-1").
					WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(now))
			},
			wantErr: false,
		},
		{
			name:     "error counting messages",
			branchID: "branch-1",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*FROM messages WHERE branch_id").
					WithArgs("branch-1").
					WillReturnError(errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			_, err = store.GetBranchStats(context.Background(), tt.branchID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetBranchStats() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_MergeBranch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		sourceBranchID string
		targetBranchID string
		strategy       models.MergeStrategy
		setupMock      func(sqlmock.Sqlmock)
		wantErr        error
	}{
		{
			name:           "successful merge",
			sourceBranchID: "source-branch",
			targetBranchID: "target-branch",
			strategy:       models.MergeStrategyContinue,
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetBranch for source
				sourceRows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"source-branch", "session-1", nil, "feature", "",
					int64(0), models.BranchStatusActive, false, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("source-branch").
					WillReturnRows(sourceRows)

				// GetBranch for target
				targetRows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"target-branch", "session-1", nil, "main", "",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("target-branch").
					WillReturnRows(targetRows)

				// Transaction
				mock.ExpectBegin()

				// Get max sequence
				mock.ExpectQuery("SELECT COALESCE.*MAX.*FROM messages").
					WithArgs("target-branch").
					WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(int64(5)))

				// Copy messages
				mock.ExpectExec("INSERT INTO messages").
					WillReturnResult(sqlmock.NewResult(0, 3))

				// Update source branch status
				mock.ExpectExec("UPDATE branches SET status").
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Create merge record
				mock.ExpectExec("INSERT INTO branch_merges").
					WillReturnResult(sqlmock.NewResult(1, 1))

				mock.ExpectCommit()
			},
			wantErr: nil,
		},
		{
			name:           "cannot merge primary",
			sourceBranchID: "primary-branch",
			targetBranchID: "target-branch",
			strategy:       models.MergeStrategyContinue,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"primary-branch", "session-1", nil, "main", "",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("primary-branch").
					WillReturnRows(rows)
			},
			wantErr: ErrCannotMergePrimary,
		},
		{
			name:           "cannot merge already merged",
			sourceBranchID: "merged-branch",
			targetBranchID: "target-branch",
			strategy:       models.MergeStrategyContinue,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"merged-branch", "session-1", nil, "feature", "",
					int64(0), models.BranchStatusMerged, false, []byte("{}"), now, now, &now,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE id").
					WithArgs("merged-branch").
					WillReturnRows(rows)
			},
			wantErr: ErrBranchMerged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			_, err = store.MergeBranch(context.Background(), tt.sourceBranchID, tt.targetBranchID, tt.strategy)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("MergeBranch() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("MergeBranch() unexpected error = %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_AppendMessageToBranch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		sessionID string
		branchID  string
		message   *models.Message
		setupMock func(sqlmock.Sqlmock)
		wantErr   bool
	}{
		{
			name:      "append to specific branch",
			sessionID: "session-1",
			branchID:  "branch-1",
			message: &models.Message{
				Role:    models.RoleUser,
				Content: "hello",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// Get max sequence
				mock.ExpectQuery("SELECT COALESCE.*MAX.*FROM messages").
					WithArgs("branch-1").
					WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(int64(5)))

				// Insert message
				mock.ExpectExec("INSERT INTO messages").
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Update branch timestamp
				mock.ExpectExec("UPDATE branches SET updated_at").
					WithArgs(sqlmock.AnyArg(), "branch-1").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: false,
		},
		{
			name:      "append to primary branch using empty branchID",
			sessionID: "session-1",
			branchID:  "",
			message: &models.Message{
				Role:    models.RoleUser,
				Content: "hello",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetPrimaryBranch
				rows := sqlmock.NewRows([]string{
					"id", "session_id", "parent_branch_id", "name", "description",
					"branch_point", "status", "is_primary", "metadata", "created_at", "updated_at", "merged_at",
				}).AddRow(
					"primary-branch", "session-1", nil, "main", "",
					int64(0), models.BranchStatusActive, true, []byte("{}"), now, now, nil,
				)
				mock.ExpectQuery("SELECT .* FROM branches WHERE session_id .* AND is_primary").
					WithArgs("session-1").
					WillReturnRows(rows)

				// Get max sequence
				mock.ExpectQuery("SELECT COALESCE.*MAX.*FROM messages").
					WithArgs("primary-branch").
					WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(int64(0)))

				// Insert message
				mock.ExpectExec("INSERT INTO messages").
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Update branch timestamp
				mock.ExpectExec("UPDATE branches SET updated_at").
					WithArgs(sqlmock.AnyArg(), "primary-branch").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			err = store.AppendMessageToBranch(context.Background(), tt.sessionID, tt.branchID, tt.message)

			if (err != nil) != tt.wantErr {
				t.Errorf("AppendMessageToBranch() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestCockroachBranchStore_GetBranchHistory(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		branchID  string
		limit     int
		setupMock func(sqlmock.Sqlmock)
		wantCount int
		wantErr   bool
	}{
		{
			name:     "get history",
			branchID: "branch-1",
			limit:    100,
			setupMock: func(mock sqlmock.Sqlmock) {
				// GetBranchHistory uses a recursive CTE query
				msgRows := sqlmock.NewRows([]string{
					"id", "session_id", "branch_id", "sequence_num", "channel", "channel_id",
					"direction", "role", "content", "attachments", "tool_calls", "tool_results",
					"metadata", "created_at",
				}).
					AddRow("msg-1", "session-1", "branch-1", int64(1), "slack", "ch-1",
						models.DirectionInbound, models.RoleUser, "hello", []byte("[]"), []byte("[]"), []byte("[]"),
						[]byte("{}"), now).
					AddRow("msg-2", "session-1", "branch-1", int64(2), "slack", "ch-1",
						models.DirectionOutbound, models.RoleAssistant, "world", []byte("[]"), []byte("[]"), []byte("[]"),
						[]byte("{}"), now)

				mock.ExpectQuery("WITH RECURSIVE branch_path AS").
					WithArgs("branch-1", 100).
					WillReturnRows(msgRows)
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "get history with default limit",
			branchID: "branch-1",
			limit:    0,
			setupMock: func(mock sqlmock.Sqlmock) {
				msgRows := sqlmock.NewRows([]string{
					"id", "session_id", "branch_id", "sequence_num", "channel", "channel_id",
					"direction", "role", "content", "attachments", "tool_calls", "tool_results",
					"metadata", "created_at",
				})
				mock.ExpectQuery("WITH RECURSIVE branch_path AS").
					WithArgs("branch-1", 100). // default limit is 100
					WillReturnRows(msgRows)
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:     "database error",
			branchID: "branch-1",
			limit:    100,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("WITH RECURSIVE branch_path AS").
					WithArgs("branch-1", 100).
					WillReturnError(errors.New("db error"))
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			tt.setupMock(mock)
			store := NewCockroachBranchStore(db)
			msgs, err := store.GetBranchHistory(context.Background(), tt.branchID, tt.limit)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetBranchHistory() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(msgs) != tt.wantCount {
				t.Errorf("GetBranchHistory() count = %d, want %d", len(msgs), tt.wantCount)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

// Helper function to create pointer to BranchStatus
func ptrBranchStatus(s models.BranchStatus) *models.BranchStatus {
	return &s
}
