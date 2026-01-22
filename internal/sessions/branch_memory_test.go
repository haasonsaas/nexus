package sessions

import (
	"context"
	"sync"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

// TestMemoryBranchStore_CreateBranch tests branch creation.
func TestMemoryBranchStore_CreateBranch(t *testing.T) {
	tests := []struct {
		name        string
		branch      *models.Branch
		wantErr     bool
		errContains string
	}{
		{
			name: "create branch without ID",
			branch: &models.Branch{
				SessionID: "session-1",
				Name:      "feature-branch",
				Status:    models.BranchStatusActive,
			},
			wantErr: false,
		},
		{
			name: "create branch with ID",
			branch: &models.Branch{
				ID:        "custom-id",
				SessionID: "session-1",
				Name:      "custom-branch",
				Status:    models.BranchStatusActive,
			},
			wantErr: false,
		},
		{
			name: "create primary branch",
			branch: &models.Branch{
				SessionID: "session-1",
				Name:      "main",
				IsPrimary: true,
				Status:    models.BranchStatusActive,
			},
			wantErr: false,
		},
		{
			name: "create branch with parent",
			branch: &models.Branch{
				SessionID:      "session-1",
				Name:           "child-branch",
				ParentBranchID: strPtr("parent-id"),
				BranchPoint:    5,
				Status:         models.BranchStatusActive,
			},
			wantErr: false,
		},
		{
			name: "create branch with metadata",
			branch: &models.Branch{
				SessionID: "session-1",
				Name:      "metadata-branch",
				Status:    models.BranchStatusActive,
				Metadata:  map[string]any{"key": "value"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryBranchStore()
			ctx := context.Background()

			err := store.CreateBranch(ctx, tt.branch)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify ID was assigned
			if tt.branch.ID == "" {
				t.Error("expected ID to be assigned")
			}

			// Verify timestamps
			if tt.branch.CreatedAt.IsZero() {
				t.Error("expected CreatedAt to be set")
			}
			if tt.branch.UpdatedAt.IsZero() {
				t.Error("expected UpdatedAt to be set")
			}

			// Verify we can retrieve it
			retrieved, err := store.GetBranch(ctx, tt.branch.ID)
			if err != nil {
				t.Fatalf("failed to retrieve created branch: %v", err)
			}
			if retrieved.Name != tt.branch.Name {
				t.Errorf("name mismatch: got %q, want %q", retrieved.Name, tt.branch.Name)
			}
		})
	}
}

// TestMemoryBranchStore_GetBranch tests branch retrieval.
func TestMemoryBranchStore_GetBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create a branch
	branch := &models.Branch{
		ID:          "test-branch",
		SessionID:   "session-1",
		Name:        "test",
		Description: "Test branch",
		Status:      models.BranchStatusActive,
	}
	if err := store.CreateBranch(ctx, branch); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{
			name:    "existing branch",
			id:      "test-branch",
			wantErr: nil,
		},
		{
			name:    "non-existent branch",
			id:      "non-existent",
			wantErr: ErrBranchNotFound,
		},
		{
			name:    "empty id",
			id:      "",
			wantErr: ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetBranch(ctx, tt.id)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tt.id {
				t.Errorf("ID mismatch: got %q, want %q", got.ID, tt.id)
			}
		})
	}
}

// TestMemoryBranchStore_GetBranch_ReturnsClone verifies that GetBranch returns a copy.
func TestMemoryBranchStore_GetBranch_ReturnsClone(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	branch := &models.Branch{
		ID:        "test-branch",
		SessionID: "session-1",
		Name:      "original",
		Metadata:  map[string]any{"key": "original"},
	}
	if err := store.CreateBranch(ctx, branch); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	retrieved, _ := store.GetBranch(ctx, "test-branch")
	retrieved.Name = "modified"
	retrieved.Metadata["key"] = "modified"

	original, _ := store.GetBranch(ctx, "test-branch")
	if original.Name != "original" {
		t.Error("modifying retrieved branch affected stored branch")
	}
	if original.Metadata["key"] != "original" {
		t.Error("modifying retrieved metadata affected stored metadata")
	}
}

// TestMemoryBranchStore_UpdateBranch tests branch updates.
func TestMemoryBranchStore_UpdateBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	branch := &models.Branch{
		ID:        "test-branch",
		SessionID: "session-1",
		Name:      "original",
		Status:    models.BranchStatusActive,
	}
	if err := store.CreateBranch(ctx, branch); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name     string
		updateFn func(*models.Branch)
		wantErr  error
	}{
		{
			name: "update name",
			updateFn: func(b *models.Branch) {
				b.Name = "updated"
			},
			wantErr: nil,
		},
		{
			name: "update description",
			updateFn: func(b *models.Branch) {
				b.Description = "New description"
			},
			wantErr: nil,
		},
		{
			name: "update status",
			updateFn: func(b *models.Branch) {
				b.Status = models.BranchStatusArchived
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retrieved, _ := store.GetBranch(ctx, "test-branch")
			tt.updateFn(retrieved)

			err := store.UpdateBranch(ctx, retrieved)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify changes persisted
			updated, _ := store.GetBranch(ctx, "test-branch")
			if updated.Name != retrieved.Name {
				t.Errorf("name not updated: got %q, want %q", updated.Name, retrieved.Name)
			}
		})
	}
}

// TestMemoryBranchStore_UpdateBranch_NonExistent tests updating non-existent branch.
func TestMemoryBranchStore_UpdateBranch_NonExistent(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	branch := &models.Branch{
		ID:        "non-existent",
		SessionID: "session-1",
		Name:      "test",
	}
	err := store.UpdateBranch(ctx, branch)
	if err != ErrBranchNotFound {
		t.Errorf("expected ErrBranchNotFound, got %v", err)
	}
}

// TestMemoryBranchStore_DeleteBranch tests branch deletion.
func TestMemoryBranchStore_DeleteBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create branches
	primary := &models.Branch{
		ID:        "primary-branch",
		SessionID: "session-1",
		Name:      "main",
		IsPrimary: true,
		Status:    models.BranchStatusActive,
	}
	secondary := &models.Branch{
		ID:        "secondary-branch",
		SessionID: "session-1",
		Name:      "feature",
		Status:    models.BranchStatusActive,
	}
	if err := store.CreateBranch(ctx, primary); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := store.CreateBranch(ctx, secondary); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add message to secondary branch
	msg := &models.Message{Role: models.RoleUser, Content: "test"}
	if err := store.AppendMessageToBranch(ctx, "session-1", "secondary-branch", msg); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name           string
		branchID       string
		deleteMessages bool
		wantErr        error
	}{
		{
			name:           "delete non-primary branch with messages",
			branchID:       "secondary-branch",
			deleteMessages: true,
			wantErr:        nil,
		},
		{
			name:           "cannot delete primary branch",
			branchID:       "primary-branch",
			deleteMessages: false,
			wantErr:        ErrCannotDeletePrimary,
		},
		{
			name:           "delete non-existent branch",
			branchID:       "non-existent",
			deleteMessages: false,
			wantErr:        ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.DeleteBranch(ctx, tt.branchID, tt.deleteMessages)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify branch is gone
			_, err = store.GetBranch(ctx, tt.branchID)
			if err != ErrBranchNotFound {
				t.Error("branch should not exist after delete")
			}
		})
	}
}

// TestMemoryBranchStore_GetPrimaryBranch tests getting the primary branch.
func TestMemoryBranchStore_GetPrimaryBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create a primary branch
	primary := &models.Branch{
		ID:        "primary-branch",
		SessionID: "session-1",
		Name:      "main",
		IsPrimary: true,
		Status:    models.BranchStatusActive,
	}
	if err := store.CreateBranch(ctx, primary); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Create a non-primary branch
	secondary := &models.Branch{
		ID:        "secondary-branch",
		SessionID: "session-1",
		Name:      "feature",
		Status:    models.BranchStatusActive,
	}
	if err := store.CreateBranch(ctx, secondary); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name      string
		sessionID string
		wantID    string
		wantErr   error
	}{
		{
			name:      "session with primary branch",
			sessionID: "session-1",
			wantID:    "primary-branch",
			wantErr:   nil,
		},
		{
			name:      "session without primary branch",
			sessionID: "session-2",
			wantErr:   ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetPrimaryBranch(ctx, tt.sessionID)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
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

// TestMemoryBranchStore_ListBranches tests branch listing.
func TestMemoryBranchStore_ListBranches(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create branches for session-1
	branches := []*models.Branch{
		{ID: "b1", SessionID: "session-1", Name: "main", IsPrimary: true, Status: models.BranchStatusActive},
		{ID: "b2", SessionID: "session-1", Name: "feature", Status: models.BranchStatusActive},
		{ID: "b3", SessionID: "session-1", Name: "archived", Status: models.BranchStatusArchived},
		{ID: "b4", SessionID: "session-2", Name: "other", Status: models.BranchStatusActive},
	}
	for _, b := range branches {
		if err := store.CreateBranch(ctx, b); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	tests := []struct {
		name      string
		sessionID string
		opts      BranchListOptions
		wantCount int
	}{
		{
			name:      "all active branches for session-1",
			sessionID: "session-1",
			opts:      BranchListOptions{},
			wantCount: 2, // archived excluded by default
		},
		{
			name:      "include archived",
			sessionID: "session-1",
			opts:      BranchListOptions{IncludeArchived: true},
			wantCount: 3,
		},
		{
			name:      "filter by status",
			sessionID: "session-1",
			opts:      BranchListOptions{Status: statusPtr(models.BranchStatusActive)},
			wantCount: 2,
		},
		{
			name:      "with limit",
			sessionID: "session-1",
			opts:      BranchListOptions{Limit: 1, IncludeArchived: true},
			wantCount: 1,
		},
		{
			name:      "with offset",
			sessionID: "session-1",
			opts:      BranchListOptions{Offset: 1, IncludeArchived: true},
			wantCount: 2,
		},
		{
			name:      "offset beyond count",
			sessionID: "session-1",
			opts:      BranchListOptions{Offset: 100},
			wantCount: 0,
		},
		{
			name:      "session-2 branches",
			sessionID: "session-2",
			opts:      BranchListOptions{},
			wantCount: 1,
		},
		{
			name:      "non-existent session",
			sessionID: "non-existent",
			opts:      BranchListOptions{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.ListBranches(ctx, tt.sessionID, tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Errorf("count mismatch: got %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// TestMemoryBranchStore_ForkBranch tests forking a branch.
func TestMemoryBranchStore_ForkBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create parent branch
	parent := &models.Branch{
		ID:        "parent-branch",
		SessionID: "session-1",
		Name:      "main",
		Status:    models.BranchStatusActive,
	}
	if err := store.CreateBranch(ctx, parent); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add some messages
	for i := 0; i < 5; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "message"}
		if err := store.AppendMessageToBranch(ctx, "session-1", "parent-branch", msg); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	tests := []struct {
		name           string
		parentBranchID string
		branchPoint    int64
		branchName     string
		wantErr        error
	}{
		{
			name:           "successful fork",
			parentBranchID: "parent-branch",
			branchPoint:    3,
			branchName:     "feature",
			wantErr:        nil,
		},
		{
			name:           "fork from non-existent branch",
			parentBranchID: "non-existent",
			branchPoint:    1,
			branchName:     "test",
			wantErr:        ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.ForkBranch(ctx, tt.parentBranchID, tt.branchPoint, tt.branchName)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Name != tt.branchName {
				t.Errorf("name mismatch: got %q, want %q", got.Name, tt.branchName)
			}
			if got.ParentBranchID == nil || *got.ParentBranchID != tt.parentBranchID {
				t.Error("parent branch ID not set correctly")
			}
			if got.BranchPoint != tt.branchPoint {
				t.Errorf("branch point mismatch: got %d, want %d", got.BranchPoint, tt.branchPoint)
			}
			if got.SessionID != "session-1" {
				t.Errorf("session ID mismatch: got %q, want %q", got.SessionID, "session-1")
			}
		})
	}
}

// TestMemoryBranchStore_MergeBranch tests merging branches.
func TestMemoryBranchStore_MergeBranch(t *testing.T) {
	ctx := context.Background()

	t.Run("successful merge", func(t *testing.T) {
		store := NewMemoryBranchStore()

		// Create target (primary) branch
		target := &models.Branch{
			ID:        "target-branch",
			SessionID: "session-1",
			Name:      "main",
			IsPrimary: true,
			Status:    models.BranchStatusActive,
		}
		if err := store.CreateBranch(ctx, target); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Add messages to target
		for i := 0; i < 3; i++ {
			msg := &models.Message{Role: models.RoleUser, Content: "target message"}
			if err := store.AppendMessageToBranch(ctx, "session-1", "target-branch", msg); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
		}

		// Create source branch with branch point at 0 (inherits no messages)
		// Using branch point 0 so that messages added to the fork (starting at seq 1)
		// will satisfy the condition: msg.SequenceNum > source.BranchPoint (1 > 0, 2 > 0)
		source, err := store.ForkBranch(ctx, "target-branch", 0, "feature")
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		// Add messages to source - these will be the messages that get merged
		for i := 0; i < 2; i++ {
			msg := &models.Message{Role: models.RoleUser, Content: "source message"}
			if err := store.AppendMessageToBranch(ctx, "session-1", source.ID, msg); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
		}

		// Merge
		merge, err := store.MergeBranch(ctx, source.ID, "target-branch", models.MergeStrategyContinue)
		if err != nil {
			t.Fatalf("merge failed: %v", err)
		}

		if merge.SourceBranchID != source.ID {
			t.Errorf("source branch ID mismatch")
		}
		if merge.TargetBranchID != "target-branch" {
			t.Errorf("target branch ID mismatch")
		}
		// The merge copies messages from source where msg.SequenceNum > source.BranchPoint
		// Since source has BranchPoint 0 and messages with SequenceNum 1 and 2, both get copied
		if merge.MessageCount != 2 {
			t.Errorf("message count mismatch: got %d, want 2", merge.MessageCount)
		}

		// Verify source branch status changed
		updatedSource, _ := store.GetBranch(ctx, source.ID)
		if updatedSource.Status != models.BranchStatusMerged {
			t.Errorf("source status should be merged, got %v", updatedSource.Status)
		}
	})

	t.Run("cannot merge primary branch", func(t *testing.T) {
		store := NewMemoryBranchStore()

		primary := &models.Branch{
			ID:        "primary-branch",
			SessionID: "session-1",
			Name:      "main",
			IsPrimary: true,
			Status:    models.BranchStatusActive,
		}
		target := &models.Branch{
			ID:        "target-branch",
			SessionID: "session-1",
			Name:      "target",
			Status:    models.BranchStatusActive,
		}
		store.CreateBranch(ctx, primary)
		store.CreateBranch(ctx, target)

		_, err := store.MergeBranch(ctx, "primary-branch", "target-branch", models.MergeStrategyContinue)
		if err != ErrCannotMergePrimary {
			t.Errorf("expected ErrCannotMergePrimary, got %v", err)
		}
	})

	t.Run("cannot merge already merged branch", func(t *testing.T) {
		store := NewMemoryBranchStore()

		merged := &models.Branch{
			ID:        "merged-branch",
			SessionID: "session-1",
			Name:      "merged",
			Status:    models.BranchStatusMerged,
		}
		target := &models.Branch{
			ID:        "target-branch",
			SessionID: "session-1",
			Name:      "target",
			Status:    models.BranchStatusActive,
		}
		store.CreateBranch(ctx, merged)
		store.CreateBranch(ctx, target)

		_, err := store.MergeBranch(ctx, "merged-branch", "target-branch", models.MergeStrategyContinue)
		if err != ErrBranchMerged {
			t.Errorf("expected ErrBranchMerged, got %v", err)
		}
	})
}

// TestMemoryBranchStore_ArchiveBranch tests archiving branches.
func TestMemoryBranchStore_ArchiveBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create branches
	primary := &models.Branch{
		ID:        "primary-branch",
		SessionID: "session-1",
		Name:      "main",
		IsPrimary: true,
		Status:    models.BranchStatusActive,
	}
	secondary := &models.Branch{
		ID:        "secondary-branch",
		SessionID: "session-1",
		Name:      "feature",
		Status:    models.BranchStatusActive,
	}
	store.CreateBranch(ctx, primary)
	store.CreateBranch(ctx, secondary)

	tests := []struct {
		name     string
		branchID string
		wantErr  error
	}{
		{
			name:     "archive non-primary branch",
			branchID: "secondary-branch",
			wantErr:  nil,
		},
		{
			name:     "cannot archive primary branch",
			branchID: "primary-branch",
			wantErr:  ErrCannotDeletePrimary,
		},
		{
			name:     "archive non-existent branch",
			branchID: "non-existent",
			wantErr:  ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.ArchiveBranch(ctx, tt.branchID)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify status changed
			branch, _ := store.GetBranch(ctx, tt.branchID)
			if branch.Status != models.BranchStatusArchived {
				t.Errorf("branch should be archived, got %v", branch.Status)
			}
		})
	}
}

// TestMemoryBranchStore_GetBranchTree tests building branch trees.
func TestMemoryBranchStore_GetBranchTree(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create a branch hierarchy
	root := &models.Branch{ID: "root", SessionID: "session-1", Name: "main", IsPrimary: true, Status: models.BranchStatusActive}
	store.CreateBranch(ctx, root)

	child1 := &models.Branch{
		ID: "child1", SessionID: "session-1", Name: "feature1",
		ParentBranchID: strPtr("root"), Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, child1)

	child2 := &models.Branch{
		ID: "child2", SessionID: "session-1", Name: "feature2",
		ParentBranchID: strPtr("root"), Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, child2)

	grandchild := &models.Branch{
		ID: "grandchild", SessionID: "session-1", Name: "sub-feature",
		ParentBranchID: strPtr("child1"), Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, grandchild)

	t.Run("successful tree retrieval", func(t *testing.T) {
		tree, err := store.GetBranchTree(ctx, "session-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tree.Branch.ID != "root" {
			t.Errorf("root ID mismatch: got %q, want %q", tree.Branch.ID, "root")
		}
		if tree.Depth != 0 {
			t.Errorf("root depth should be 0, got %d", tree.Depth)
		}
		if len(tree.Children) != 2 {
			t.Errorf("root should have 2 children, got %d", len(tree.Children))
		}
	})

	t.Run("non-existent session", func(t *testing.T) {
		_, err := store.GetBranchTree(ctx, "non-existent")
		if err != ErrBranchNotFound {
			t.Errorf("expected ErrBranchNotFound, got %v", err)
		}
	})
}

// TestMemoryBranchStore_GetFullBranchPath tests getting branch ancestry paths.
func TestMemoryBranchStore_GetFullBranchPath(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create a branch hierarchy
	root := &models.Branch{ID: "root", SessionID: "session-1", Name: "main", Status: models.BranchStatusActive}
	store.CreateBranch(ctx, root)

	child := &models.Branch{
		ID: "child", SessionID: "session-1", Name: "feature",
		ParentBranchID: strPtr("root"), Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, child)

	grandchild := &models.Branch{
		ID: "grandchild", SessionID: "session-1", Name: "sub-feature",
		ParentBranchID: strPtr("child"), Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, grandchild)

	t.Run("path to grandchild", func(t *testing.T) {
		path, err := store.GetFullBranchPath(ctx, "grandchild")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if path.BranchID != "grandchild" {
			t.Errorf("branch ID mismatch")
		}
		if len(path.Path) != 3 {
			t.Errorf("path length should be 3, got %d", len(path.Path))
		}
		if path.Path[0] != "root" || path.Path[1] != "child" || path.Path[2] != "grandchild" {
			t.Errorf("unexpected path: %v", path.Path)
		}
	})

	t.Run("path to root", func(t *testing.T) {
		path, err := store.GetFullBranchPath(ctx, "root")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(path.Path) != 1 {
			t.Errorf("root path length should be 1, got %d", len(path.Path))
		}
	})

	t.Run("non-existent branch", func(t *testing.T) {
		_, err := store.GetFullBranchPath(ctx, "non-existent")
		if err != ErrBranchNotFound {
			t.Errorf("expected ErrBranchNotFound, got %v", err)
		}
	})
}

// TestMemoryBranchStore_GetBranchStats tests branch statistics.
func TestMemoryBranchStore_GetBranchStats(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create branches
	parent := &models.Branch{ID: "parent", SessionID: "session-1", Name: "main", Status: models.BranchStatusActive}
	store.CreateBranch(ctx, parent)

	child := &models.Branch{
		ID: "child", SessionID: "session-1", Name: "feature",
		ParentBranchID: strPtr("parent"), BranchPoint: 2, Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, child)

	// Add messages to parent
	for i := 0; i < 3; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "parent message"}
		store.AppendMessageToBranch(ctx, "session-1", "parent", msg)
	}

	// Add messages to child
	for i := 0; i < 2; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "child message"}
		store.AppendMessageToBranch(ctx, "session-1", "child", msg)
	}

	t.Run("parent stats", func(t *testing.T) {
		stats, err := store.GetBranchStats(ctx, "parent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if stats.OwnMessages != 3 {
			t.Errorf("own messages should be 3, got %d", stats.OwnMessages)
		}
		if stats.ChildBranchCount != 1 {
			t.Errorf("child branch count should be 1, got %d", stats.ChildBranchCount)
		}
	})

	t.Run("child stats", func(t *testing.T) {
		stats, err := store.GetBranchStats(ctx, "child")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if stats.OwnMessages != 2 {
			t.Errorf("own messages should be 2, got %d", stats.OwnMessages)
		}
		// Total includes inherited messages (up to branch point)
		if stats.TotalMessages != 4 { // 2 inherited + 2 own
			t.Errorf("total messages should be 4, got %d", stats.TotalMessages)
		}
	})

	t.Run("non-existent branch", func(t *testing.T) {
		_, err := store.GetBranchStats(ctx, "non-existent")
		if err != ErrBranchNotFound {
			t.Errorf("expected ErrBranchNotFound, got %v", err)
		}
	})
}

// TestMemoryBranchStore_AppendMessageToBranch tests message appending.
func TestMemoryBranchStore_AppendMessageToBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create branches
	primary := &models.Branch{
		ID:        "primary-branch",
		SessionID: "session-1",
		Name:      "main",
		IsPrimary: true,
		Status:    models.BranchStatusActive,
	}
	store.CreateBranch(ctx, primary)

	tests := []struct {
		name      string
		sessionID string
		branchID  string
		message   *models.Message
		wantErr   error
	}{
		{
			name:      "append to specific branch",
			sessionID: "session-1",
			branchID:  "primary-branch",
			message:   &models.Message{Role: models.RoleUser, Content: "hello"},
			wantErr:   nil,
		},
		{
			name:      "append to primary branch using empty branchID",
			sessionID: "session-1",
			branchID:  "",
			message:   &models.Message{Role: models.RoleUser, Content: "hello"},
			wantErr:   nil,
		},
		{
			name:      "append to non-existent branch",
			sessionID: "session-1",
			branchID:  "non-existent",
			message:   &models.Message{Role: models.RoleUser, Content: "hello"},
			wantErr:   ErrBranchNotFound,
		},
		{
			name:      "append to non-existent session with empty branchID",
			sessionID: "non-existent",
			branchID:  "",
			message:   &models.Message{Role: models.RoleUser, Content: "hello"},
			wantErr:   ErrBranchNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.AppendMessageToBranch(ctx, tt.sessionID, tt.branchID, tt.message)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify message was stored by checking the branch history
			branchID := tt.branchID
			if branchID == "" {
				branchID = "primary-branch"
			}
			history, histErr := store.GetBranchHistory(ctx, branchID, 100)
			if histErr != nil {
				t.Fatalf("failed to get history: %v", histErr)
			}
			if len(history) == 0 {
				t.Error("expected message to be stored in branch history")
			}
		})
	}
}

// TestMemoryBranchStore_GetBranchHistory tests message history retrieval.
func TestMemoryBranchStore_GetBranchHistory(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create parent branch with messages
	parent := &models.Branch{ID: "parent", SessionID: "session-1", Name: "main", Status: models.BranchStatusActive}
	store.CreateBranch(ctx, parent)

	for i := 0; i < 5; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "parent message"}
		store.AppendMessageToBranch(ctx, "session-1", "parent", msg)
	}

	// Create child branch with messages
	child := &models.Branch{
		ID: "child", SessionID: "session-1", Name: "feature",
		ParentBranchID: strPtr("parent"), BranchPoint: 3, Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, child)

	for i := 0; i < 2; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "child message"}
		store.AppendMessageToBranch(ctx, "session-1", "child", msg)
	}

	t.Run("get parent history", func(t *testing.T) {
		history, err := store.GetBranchHistory(ctx, "parent", 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(history) != 5 {
			t.Errorf("expected 5 messages, got %d", len(history))
		}
	})

	t.Run("get child history with inheritance", func(t *testing.T) {
		history, err := store.GetBranchHistory(ctx, "child", 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Child should have 3 inherited + 2 own = 5 messages
		if len(history) != 5 {
			t.Errorf("expected 5 messages (3 inherited + 2 own), got %d", len(history))
		}
	})

	t.Run("with limit", func(t *testing.T) {
		history, err := store.GetBranchHistory(ctx, "parent", 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(history) != 3 {
			t.Errorf("expected 3 messages, got %d", len(history))
		}
	})

	t.Run("non-existent branch", func(t *testing.T) {
		_, err := store.GetBranchHistory(ctx, "non-existent", 100)
		if err != ErrBranchNotFound {
			t.Errorf("expected ErrBranchNotFound, got %v", err)
		}
	})
}

// TestMemoryBranchStore_GetBranchOwnMessages tests getting only own messages.
func TestMemoryBranchStore_GetBranchOwnMessages(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create parent branch with messages
	parent := &models.Branch{ID: "parent", SessionID: "session-1", Name: "main", Status: models.BranchStatusActive}
	store.CreateBranch(ctx, parent)

	for i := 0; i < 5; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "parent message"}
		store.AppendMessageToBranch(ctx, "session-1", "parent", msg)
	}

	// Create child branch with messages
	child := &models.Branch{
		ID: "child", SessionID: "session-1", Name: "feature",
		ParentBranchID: strPtr("parent"), BranchPoint: 3, Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, child)

	for i := 0; i < 2; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "child message"}
		store.AppendMessageToBranch(ctx, "session-1", "child", msg)
	}

	t.Run("get own messages only", func(t *testing.T) {
		messages, err := store.GetBranchOwnMessages(ctx, "child", 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(messages) != 2 {
			t.Errorf("expected 2 own messages, got %d", len(messages))
		}
	})

	t.Run("with limit", func(t *testing.T) {
		messages, err := store.GetBranchOwnMessages(ctx, "parent", 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(messages) != 3 {
			t.Errorf("expected 3 messages, got %d", len(messages))
		}
	})
}

// TestMemoryBranchStore_EnsurePrimaryBranch tests primary branch creation.
func TestMemoryBranchStore_EnsurePrimaryBranch(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	t.Run("create primary branch if not exists", func(t *testing.T) {
		branch, err := store.EnsurePrimaryBranch(ctx, "session-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !branch.IsPrimary {
			t.Error("branch should be primary")
		}
		if branch.SessionID != "session-1" {
			t.Errorf("session ID mismatch")
		}
	})

	t.Run("return existing primary branch", func(t *testing.T) {
		branch1, _ := store.EnsurePrimaryBranch(ctx, "session-1")
		branch2, err := store.EnsurePrimaryBranch(ctx, "session-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if branch1.ID != branch2.ID {
			t.Error("should return the same primary branch")
		}
	})
}

// TestMemoryBranchStore_CompareBranches tests branch comparison.
func TestMemoryBranchStore_CompareBranches(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create branch hierarchy
	root := &models.Branch{ID: "root", SessionID: "session-1", Name: "main", Status: models.BranchStatusActive}
	store.CreateBranch(ctx, root)

	branch1 := &models.Branch{
		ID: "branch1", SessionID: "session-1", Name: "feature1",
		ParentBranchID: strPtr("root"), Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, branch1)

	branch2 := &models.Branch{
		ID: "branch2", SessionID: "session-1", Name: "feature2",
		ParentBranchID: strPtr("root"), Status: models.BranchStatusActive,
	}
	store.CreateBranch(ctx, branch2)

	// Add messages
	for i := 0; i < 3; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "msg"}
		store.AppendMessageToBranch(ctx, "session-1", "branch1", msg)
	}
	for i := 0; i < 2; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "msg"}
		store.AppendMessageToBranch(ctx, "session-1", "branch2", msg)
	}

	t.Run("compare sibling branches", func(t *testing.T) {
		compare, err := store.CompareBranches(ctx, "branch1", "branch2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if compare.SourceBranch.ID != "branch1" {
			t.Errorf("source branch ID mismatch")
		}
		if compare.TargetBranch.ID != "branch2" {
			t.Errorf("target branch ID mismatch")
		}
		if compare.SourceAhead != 3 {
			t.Errorf("source ahead should be 3, got %d", compare.SourceAhead)
		}
		if compare.TargetAhead != 2 {
			t.Errorf("target ahead should be 2, got %d", compare.TargetAhead)
		}
	})

	t.Run("non-existent source branch", func(t *testing.T) {
		_, err := store.CompareBranches(ctx, "non-existent", "branch2")
		if err != ErrBranchNotFound {
			t.Errorf("expected ErrBranchNotFound, got %v", err)
		}
	})

	t.Run("non-existent target branch", func(t *testing.T) {
		_, err := store.CompareBranches(ctx, "branch1", "non-existent")
		if err != ErrBranchNotFound {
			t.Errorf("expected ErrBranchNotFound, got %v", err)
		}
	})
}

// TestMemoryBranchStore_Concurrency tests thread safety.
func TestMemoryBranchStore_Concurrency(t *testing.T) {
	store := NewMemoryBranchStore()
	ctx := context.Background()

	// Create primary branch
	primary := &models.Branch{
		ID:        "primary",
		SessionID: "session-1",
		Name:      "main",
		IsPrimary: true,
		Status:    models.BranchStatusActive,
	}
	if err := store.CreateBranch(ctx, primary); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.GetBranch(ctx, "primary")
			if err != nil {
				errChan <- err
			}
		}()
	}

	// Concurrent message appends
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := &models.Message{Role: models.RoleUser, Content: "test"}
			err := store.AppendMessageToBranch(ctx, "session-1", "primary", msg)
			if err != nil {
				errChan <- err
			}
		}()
	}

	// Concurrent branch creates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			branch := &models.Branch{
				SessionID:      "session-1",
				Name:           "feature",
				ParentBranchID: strPtr("primary"),
				Status:         models.BranchStatusActive,
			}
			err := store.CreateBranch(ctx, branch)
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

	// Verify state
	branches, _ := store.ListBranches(ctx, "session-1", BranchListOptions{})
	if len(branches) != 11 { // 1 primary + 10 feature branches
		t.Errorf("expected 11 branches, got %d", len(branches))
	}

	history, _ := store.GetBranchHistory(ctx, "primary", 0)
	if len(history) != 20 {
		t.Errorf("expected 20 messages, got %d", len(history))
	}
}

// TestDefaultBranchListOptions tests default options.
func TestDefaultBranchListOptions(t *testing.T) {
	opts := DefaultBranchListOptions()

	if opts.IncludeArchived {
		t.Error("IncludeArchived should default to false")
	}
	if opts.Limit != 50 {
		t.Errorf("Limit should default to 50, got %d", opts.Limit)
	}
	if opts.OrderBy != "created_at" {
		t.Errorf("OrderBy should default to created_at, got %s", opts.OrderBy)
	}
	if !opts.OrderDesc {
		t.Error("OrderDesc should default to true")
	}
}

// TestDefaultBranchHistoryOptions tests default history options.
func TestDefaultBranchHistoryOptions(t *testing.T) {
	opts := DefaultBranchHistoryOptions()

	if opts.Limit != 100 {
		t.Errorf("Limit should default to 100, got %d", opts.Limit)
	}
	if !opts.IncludeInherited {
		t.Error("IncludeInherited should default to true")
	}
	if opts.ReverseOrder {
		t.Error("ReverseOrder should default to false")
	}
}

// Helper functions

func strPtr(s string) *string {
	return &s
}

func statusPtr(s models.BranchStatus) *models.BranchStatus {
	return &s
}
