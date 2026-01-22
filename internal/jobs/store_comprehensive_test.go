package jobs

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
		name    string
		job     *Job
		wantErr bool
	}{
		{
			name:    "nil job",
			job:     nil,
			wantErr: false, // Current implementation returns nil for nil job
		},
		{
			name: "valid job with all fields",
			job: &Job{
				ID:         "job-1",
				ToolName:   "test-tool",
				ToolCallID: "call-1",
				Status:     StatusQueued,
				CreatedAt:  time.Now(),
				Result: &models.ToolResult{
					ToolCallID: "call-1",
					Content:    "result",
				},
			},
			wantErr: false,
		},
		{
			name: "job with minimal fields",
			job: &Job{
				ID:       "job-2",
				ToolName: "tool",
				Status:   StatusQueued,
			},
			wantErr: false,
		},
		{
			name: "job with error field",
			job: &Job{
				ID:       "job-3",
				ToolName: "tool",
				Status:   StatusFailed,
				Error:    "something went wrong",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			err := store.Create(ctx, tt.job)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.job == nil {
				return
			}

			// Verify job was stored
			got, err := store.Get(ctx, tt.job.ID)
			if err != nil {
				t.Fatalf("failed to retrieve created job: %v", err)
			}
			if got == nil {
				t.Fatal("expected job, got nil")
			}
			if got.ID != tt.job.ID {
				t.Errorf("ID mismatch: got %q, want %q", got.ID, tt.job.ID)
			}
		})
	}
}

// TestMemoryStore_Create_Duplicate tests creating with duplicate ID.
func TestMemoryStore_Create_Duplicate(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	job := &Job{
		ID:       "job-1",
		ToolName: "tool",
		Status:   StatusQueued,
	}

	// First create
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Second create with same ID should overwrite
	job2 := &Job{
		ID:       "job-1",
		ToolName: "updated-tool",
		Status:   StatusRunning,
	}
	if err := store.Create(ctx, job2); err != nil {
		t.Fatalf("second create failed: %v", err)
	}

	// Verify the job was overwritten
	got, _ := store.Get(ctx, "job-1")
	if got.ToolName != "updated-tool" {
		t.Errorf("expected tool name to be updated, got %q", got.ToolName)
	}
}

// TestMemoryStore_Get tests the Get method.
func TestMemoryStore_Get(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create a job
	job := &Job{
		ID:         "job-1",
		ToolName:   "test-tool",
		ToolCallID: "call-1",
		Status:     StatusSucceeded,
		Result:     &models.ToolResult{ToolCallID: "call-1", Content: "success"},
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name       string
		id         string
		wantNil    bool
		wantStatus Status
	}{
		{
			name:       "existing job",
			id:         "job-1",
			wantNil:    false,
			wantStatus: StatusSucceeded,
		},
		{
			name:    "non-existent job",
			id:      "non-existent",
			wantNil: true,
		},
		{
			name:    "empty id",
			id:      "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.Get(ctx, tt.id)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected job, got nil")
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status mismatch: got %q, want %q", got.Status, tt.wantStatus)
			}
		})
	}
}

// TestMemoryStore_Get_ReturnsClone tests that Get returns a copy.
func TestMemoryStore_Get_ReturnsClone(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	job := &Job{
		ID:       "job-1",
		ToolName: "original",
		Status:   StatusQueued,
		Result:   &models.ToolResult{ToolCallID: "call", Content: "original"},
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Modify retrieved job
	retrieved, _ := store.Get(ctx, "job-1")
	retrieved.ToolName = "modified"
	retrieved.Result.Content = "modified"

	// Original should be unchanged
	original, _ := store.Get(ctx, "job-1")
	if original.ToolName != "original" {
		t.Error("modifying retrieved job affected stored job")
	}
	if original.Result.Content != "original" {
		t.Error("modifying retrieved result affected stored result")
	}
}

// TestMemoryStore_Update tests the Update method.
func TestMemoryStore_Update(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	job := &Job{
		ID:       "job-1",
		ToolName: "tool",
		Status:   StatusQueued,
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	tests := []struct {
		name     string
		job      *Job
		wantErr  bool
		validate func(*testing.T, *MemoryStore)
	}{
		{
			name:    "nil job",
			job:     nil,
			wantErr: false, // Current implementation returns nil for nil job
		},
		{
			name: "update status",
			job: &Job{
				ID:       "job-1",
				ToolName: "tool",
				Status:   StatusRunning,
			},
			wantErr: false,
			validate: func(t *testing.T, store *MemoryStore) {
				got, _ := store.Get(ctx, "job-1")
				if got.Status != StatusRunning {
					t.Errorf("status not updated: got %q, want %q", got.Status, StatusRunning)
				}
			},
		},
		{
			name: "update with result",
			job: &Job{
				ID:       "job-1",
				ToolName: "tool",
				Status:   StatusSucceeded,
				Result:   &models.ToolResult{ToolCallID: "call", Content: "done"},
			},
			wantErr: false,
			validate: func(t *testing.T, store *MemoryStore) {
				got, _ := store.Get(ctx, "job-1")
				if got.Result == nil || got.Result.Content != "done" {
					t.Error("result not updated correctly")
				}
			},
		},
		{
			name: "update with error",
			job: &Job{
				ID:       "job-1",
				ToolName: "tool",
				Status:   StatusFailed,
				Error:    "execution failed",
			},
			wantErr: false,
			validate: func(t *testing.T, store *MemoryStore) {
				got, _ := store.Get(ctx, "job-1")
				if got.Error != "execution failed" {
					t.Errorf("error not updated: got %q", got.Error)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Update(ctx, tt.job)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, store)
			}
		})
	}
}

// TestMemoryStore_List tests the List method.
func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create jobs in order
	for i := 0; i < 10; i++ {
		job := &Job{
			ID:        "job-" + string(rune('0'+i)),
			ToolName:  "tool",
			Status:    StatusSucceeded,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := store.Create(ctx, job); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	tests := []struct {
		name      string
		limit     int
		offset    int
		wantCount int
		wantFirst string
		wantLast  string
	}{
		{
			name:      "all jobs with zero limit",
			limit:     0,
			offset:    0,
			wantCount: 10,
			wantFirst: "job-0",
			wantLast:  "job-9",
		},
		{
			name:      "first 5 jobs",
			limit:     5,
			offset:    0,
			wantCount: 5,
			wantFirst: "job-0",
			wantLast:  "job-4",
		},
		{
			name:      "offset 3, limit 3",
			limit:     3,
			offset:    3,
			wantCount: 3,
			wantFirst: "job-3",
			wantLast:  "job-5",
		},
		{
			name:      "offset beyond count",
			limit:     10,
			offset:    100,
			wantCount: 0,
		},
		{
			name:      "negative offset treated as zero",
			limit:     3,
			offset:    -5,
			wantCount: 3,
			wantFirst: "job-0",
		},
		{
			name:      "limit larger than remaining",
			limit:     5,
			offset:    8,
			wantCount: 2,
			wantFirst: "job-8",
			wantLast:  "job-9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.List(ctx, tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("count mismatch: got %d, want %d", len(got), tt.wantCount)
			}

			if tt.wantFirst != "" && len(got) > 0 {
				if got[0].ID != tt.wantFirst {
					t.Errorf("first job mismatch: got %q, want %q", got[0].ID, tt.wantFirst)
				}
			}

			if tt.wantLast != "" && len(got) > 0 {
				if got[len(got)-1].ID != tt.wantLast {
					t.Errorf("last job mismatch: got %q, want %q", got[len(got)-1].ID, tt.wantLast)
				}
			}
		})
	}
}

// TestMemoryStore_List_ReturnsClones tests that List returns copies.
func TestMemoryStore_List_ReturnsClones(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	job := &Job{
		ID:       "job-1",
		ToolName: "original",
		Status:   StatusQueued,
	}
	store.Create(ctx, job)

	list1, _ := store.List(ctx, 10, 0)
	list1[0].ToolName = "modified"

	list2, _ := store.List(ctx, 10, 0)
	if list2[0].ToolName != "original" {
		t.Error("modifying list affected stored jobs")
	}
}

// TestMemoryStore_List_PreservesInsertionOrder tests that List returns jobs in insertion order.
func TestMemoryStore_List_PreservesInsertionOrder(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	ids := []string{"first", "second", "third", "fourth", "fifth"}
	for _, id := range ids {
		job := &Job{ID: id, ToolName: "tool", Status: StatusQueued}
		store.Create(ctx, job)
	}

	got, _ := store.List(ctx, 10, 0)

	if len(got) != len(ids) {
		t.Fatalf("count mismatch: got %d, want %d", len(got), len(ids))
	}

	for i, id := range ids {
		if got[i].ID != id {
			t.Errorf("order mismatch at index %d: got %q, want %q", i, got[i].ID, id)
		}
	}
}

// TestMemoryStore_Prune tests the Prune method.
func TestMemoryStore_Prune(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	now := time.Now()

	// Create old jobs
	for i := 0; i < 5; i++ {
		job := &Job{
			ID:        "old-job-" + string(rune('0'+i)),
			ToolName:  "tool",
			Status:    StatusSucceeded,
			CreatedAt: now.Add(-48 * time.Hour),
		}
		store.Create(ctx, job)
	}

	// Create recent jobs
	for i := 0; i < 3; i++ {
		job := &Job{
			ID:        "new-job-" + string(rune('0'+i)),
			ToolName:  "tool",
			Status:    StatusSucceeded,
			CreatedAt: now,
		}
		store.Create(ctx, job)
	}

	tests := []struct {
		name          string
		olderThan     time.Duration
		wantPruned    int64
		wantRemaining int
	}{
		{
			name:          "prune jobs older than 24 hours",
			olderThan:     24 * time.Hour,
			wantPruned:    5,
			wantRemaining: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pruned, err := store.Prune(ctx, tt.olderThan)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if pruned != tt.wantPruned {
				t.Errorf("pruned count mismatch: got %d, want %d", pruned, tt.wantPruned)
			}

			remaining, _ := store.List(ctx, 100, 0)
			if len(remaining) != tt.wantRemaining {
				t.Errorf("remaining count mismatch: got %d, want %d", len(remaining), tt.wantRemaining)
			}
		})
	}
}

// TestMemoryStore_Prune_EmptyStore tests pruning empty store.
func TestMemoryStore_Prune_EmptyStore(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	pruned, err := store.Prune(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}
}

// TestMemoryStore_Prune_AllJobsRecent tests pruning when all jobs are recent.
func TestMemoryStore_Prune_AllJobsRecent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		job := &Job{
			ID:        "job-" + string(rune('0'+i)),
			ToolName:  "tool",
			Status:    StatusSucceeded,
			CreatedAt: time.Now(),
		}
		store.Create(ctx, job)
	}

	pruned, _ := store.Prune(ctx, 24*time.Hour)
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}

	remaining, _ := store.List(ctx, 100, 0)
	if len(remaining) != 5 {
		t.Errorf("expected 5 remaining, got %d", len(remaining))
	}
}

// TestMemoryStore_Cancel tests the Cancel method.
func TestMemoryStore_Cancel(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  Status
		wantCancelled  bool
		wantFinalError string
	}{
		{
			name:           "cancel running job",
			initialStatus:  StatusRunning,
			wantCancelled:  true,
			wantFinalError: "job cancelled",
		},
		{
			name:           "cancel queued job",
			initialStatus:  StatusQueued,
			wantCancelled:  true,
			wantFinalError: "job cancelled",
		},
		{
			name:          "cannot cancel succeeded job",
			initialStatus: StatusSucceeded,
			wantCancelled: false,
		},
		{
			name:          "cannot cancel failed job",
			initialStatus: StatusFailed,
			wantCancelled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			job := &Job{
				ID:       "job-1",
				ToolName: "tool",
				Status:   tt.initialStatus,
			}
			store.Create(ctx, job)

			err := store.Cancel(ctx, "job-1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, _ := store.Get(ctx, "job-1")

			if tt.wantCancelled {
				if got.Status != StatusFailed {
					t.Errorf("status mismatch: got %q, want %q", got.Status, StatusFailed)
				}
				if got.Error != tt.wantFinalError {
					t.Errorf("error mismatch: got %q, want %q", got.Error, tt.wantFinalError)
				}
				if got.FinishedAt.IsZero() {
					t.Error("FinishedAt should be set after cancel")
				}
			} else {
				if got.Status != tt.initialStatus {
					t.Errorf("status should be unchanged: got %q, want %q", got.Status, tt.initialStatus)
				}
			}
		})
	}
}

// TestMemoryStore_Cancel_NonExistent tests cancelling non-existent job.
func TestMemoryStore_Cancel_NonExistent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	err := store.Cancel(ctx, "non-existent")
	if err != nil {
		t.Errorf("expected nil error for non-existent job, got %v", err)
	}
}

// TestMemoryStore_Cancel_WithCancelFunc tests that cancel function is called.
func TestMemoryStore_Cancel_WithCancelFunc(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	cancelled := false
	cancelFunc := func() {
		cancelled = true
	}

	job := &Job{
		ID:         "job-1",
		ToolName:   "tool",
		Status:     StatusRunning,
		cancelFunc: cancelFunc,
	}
	store.Create(ctx, job)

	// Set the cancel func on the stored job
	store.SetCancelFunc("job-1", cancelFunc)

	err := store.Cancel(ctx, "job-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cancelled {
		t.Error("cancel function was not called")
	}
}

// TestMemoryStore_SetCancelFunc tests setting cancel function.
func TestMemoryStore_SetCancelFunc(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	job := &Job{
		ID:       "job-1",
		ToolName: "tool",
		Status:   StatusRunning,
	}
	store.Create(ctx, job)

	called := false
	store.SetCancelFunc("job-1", func() {
		called = true
	})

	// Cancel should trigger the function
	store.Cancel(ctx, "job-1")

	if !called {
		t.Error("cancel function was not set/called")
	}
}

// TestMemoryStore_SetCancelFunc_NonExistent tests setting cancel func for non-existent job.
func TestMemoryStore_SetCancelFunc_NonExistent(t *testing.T) {
	store := NewMemoryStore()

	// Should not panic
	store.SetCancelFunc("non-existent", func() {})
}

// TestMemoryStore_Concurrency tests thread safety.
func TestMemoryStore_Concurrency(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Concurrent creates
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job := &Job{
				ID:        "job-" + string(rune('A'+i)),
				ToolName:  "tool",
				Status:    StatusQueued,
				CreatedAt: time.Now(),
			}
			if err := store.Create(ctx, job); err != nil {
				errChan <- err
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.List(ctx, 10, 0)
			if err != nil {
				errChan <- err
			}
		}()
	}

	// Concurrent updates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job := &Job{
				ID:       "job-" + string(rune('A'+i)),
				ToolName: "updated",
				Status:   StatusRunning,
			}
			if err := store.Update(ctx, job); err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent operation failed: %v", err)
	}
}

// TestCloneJob tests the cloneJob helper function.
func TestCloneJob(t *testing.T) {
	t.Run("nil job", func(t *testing.T) {
		got := cloneJob(nil)
		if got != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("job with result", func(t *testing.T) {
		original := &Job{
			ID:       "job-1",
			ToolName: "tool",
			Status:   StatusSucceeded,
			Result: &models.ToolResult{
				ToolCallID: "call-1",
				Content:    "result",
			},
		}

		clone := cloneJob(original)

		// Modify clone
		clone.ToolName = "modified"
		clone.Result.Content = "modified"

		// Original should be unchanged
		if original.ToolName != "tool" {
			t.Error("modifying clone affected original")
		}
		if original.Result.Content != "result" {
			t.Error("modifying clone result affected original result")
		}
	})

	t.Run("job without result", func(t *testing.T) {
		original := &Job{
			ID:       "job-1",
			ToolName: "tool",
			Status:   StatusQueued,
		}

		clone := cloneJob(original)

		if clone.ID != original.ID {
			t.Errorf("ID mismatch: got %q, want %q", clone.ID, original.ID)
		}
		if clone.Result != nil {
			t.Error("expected nil result")
		}
	})
}

// TestJobStatus tests status constants.
func TestJobStatus(t *testing.T) {
	if StatusQueued != "queued" {
		t.Errorf("StatusQueued = %q, want %q", StatusQueued, "queued")
	}
	if StatusRunning != "running" {
		t.Errorf("StatusRunning = %q, want %q", StatusRunning, "running")
	}
	if StatusSucceeded != "succeeded" {
		t.Errorf("StatusSucceeded = %q, want %q", StatusSucceeded, "succeeded")
	}
	if StatusFailed != "failed" {
		t.Errorf("StatusFailed = %q, want %q", StatusFailed, "failed")
	}
}

// TestNewMemoryStore tests store initialization.
func TestNewMemoryStore(t *testing.T) {
	store := NewMemoryStore()

	if store == nil {
		t.Fatal("NewMemoryStore returned nil")
	}
	if store.jobs == nil {
		t.Error("jobs map not initialized")
	}
	if store.keys != nil && len(store.keys) != 0 {
		t.Error("keys should be empty initially")
	}
}

// TestMemoryStore_JobLifecycle tests a complete job lifecycle.
func TestMemoryStore_JobLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// 1. Create a queued job
	job := &Job{
		ID:         "lifecycle-job",
		ToolName:   "test-tool",
		ToolCallID: "call-123",
		Status:     StatusQueued,
		CreatedAt:  time.Now(),
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify queued
	got, _ := store.Get(ctx, "lifecycle-job")
	if got.Status != StatusQueued {
		t.Errorf("expected queued status, got %q", got.Status)
	}

	// 2. Start the job
	job.Status = StatusRunning
	job.StartedAt = time.Now()
	if err := store.Update(ctx, job); err != nil {
		t.Fatalf("update to running failed: %v", err)
	}

	got, _ = store.Get(ctx, "lifecycle-job")
	if got.Status != StatusRunning {
		t.Errorf("expected running status, got %q", got.Status)
	}

	// 3. Complete the job
	job.Status = StatusSucceeded
	job.FinishedAt = time.Now()
	job.Result = &models.ToolResult{
		ToolCallID: "call-123",
		Content:    "Task completed successfully",
	}
	if err := store.Update(ctx, job); err != nil {
		t.Fatalf("update to succeeded failed: %v", err)
	}

	got, _ = store.Get(ctx, "lifecycle-job")
	if got.Status != StatusSucceeded {
		t.Errorf("expected succeeded status, got %q", got.Status)
	}
	if got.Result == nil || got.Result.Content != "Task completed successfully" {
		t.Error("result not saved correctly")
	}

	// 4. Verify job appears in list
	list, _ := store.List(ctx, 10, 0)
	if len(list) != 1 {
		t.Errorf("expected 1 job in list, got %d", len(list))
	}

	// 5. Prune (job is recent so should not be pruned)
	pruned, _ := store.Prune(ctx, 24*time.Hour)
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}

	list, _ = store.List(ctx, 10, 0)
	if len(list) != 1 {
		t.Error("job should still exist after prune")
	}
}

// TestMemoryStore_FailedJobLifecycle tests a failed job scenario.
func TestMemoryStore_FailedJobLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	job := &Job{
		ID:        "failed-job",
		ToolName:  "failing-tool",
		Status:    StatusQueued,
		CreatedAt: time.Now(),
	}
	store.Create(ctx, job)

	// Start job
	job.Status = StatusRunning
	job.StartedAt = time.Now()
	store.Update(ctx, job)

	// Job fails
	job.Status = StatusFailed
	job.FinishedAt = time.Now()
	job.Error = "execution timeout"
	store.Update(ctx, job)

	got, _ := store.Get(ctx, "failed-job")
	if got.Status != StatusFailed {
		t.Errorf("expected failed status, got %q", got.Status)
	}
	if got.Error != "execution timeout" {
		t.Errorf("expected error message, got %q", got.Error)
	}
}

// TestMemoryStore_CancelledJobLifecycle tests a cancelled job scenario.
func TestMemoryStore_CancelledJobLifecycle(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	cancelCalled := false
	cancelCtx, cancel := context.WithCancel(ctx)

	job := &Job{
		ID:        "cancelled-job",
		ToolName:  "long-running-tool",
		Status:    StatusRunning,
		StartedAt: time.Now(),
		cancelFunc: func() {
			cancelCalled = true
			cancel()
		},
	}
	store.Create(ctx, job)
	store.SetCancelFunc("cancelled-job", job.cancelFunc)

	// Cancel the job
	if err := store.Cancel(cancelCtx, "cancelled-job"); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}

	if !cancelCalled {
		t.Error("cancel function was not called")
	}

	got, _ := store.Get(ctx, "cancelled-job")
	if got.Status != StatusFailed {
		t.Errorf("expected failed status after cancel, got %q", got.Status)
	}
	if got.Error != "job cancelled" {
		t.Errorf("expected 'job cancelled' error, got %q", got.Error)
	}
}
