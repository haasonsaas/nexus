package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestMemoryStoreCRUD(t *testing.T) {
	store := NewMemoryStore()
	job := &Job{
		ID:         "job-1",
		ToolName:   "tool",
		ToolCallID: "call-1",
		Status:     StatusQueued,
		CreatedAt:  time.Now(),
		Result:     &models.ToolResult{ToolCallID: "call-1", Content: "ok"},
	}

	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.Get(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != "job-1" {
		t.Fatalf("expected job, got %+v", got)
	}
	if got.Result == nil || got.Result.Content != "ok" {
		t.Fatalf("expected result content, got %+v", got.Result)
	}

	job.Status = StatusSucceeded
	if err := store.Update(context.Background(), job); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = store.Get(context.Background(), "job-1")
	if got.Status != StatusSucceeded {
		t.Fatalf("expected status %q, got %q", StatusSucceeded, got.Status)
	}
}

func TestMemoryStorePrune(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create old job
	oldJob := &Job{
		ID:        "old-job",
		ToolName:  "tool",
		Status:    StatusSucceeded,
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	if err := store.Create(ctx, oldJob); err != nil {
		t.Fatalf("create old job: %v", err)
	}

	// Create recent job
	newJob := &Job{
		ID:        "new-job",
		ToolName:  "tool",
		Status:    StatusSucceeded,
		CreatedAt: time.Now(),
	}
	if err := store.Create(ctx, newJob); err != nil {
		t.Fatalf("create new job: %v", err)
	}

	// Prune jobs older than 24h
	pruned, err := store.Prune(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	// Old job should be gone
	got, _ := store.Get(ctx, "old-job")
	if got != nil {
		t.Fatalf("expected old job to be pruned")
	}

	// New job should remain
	got, _ = store.Get(ctx, "new-job")
	if got == nil {
		t.Fatalf("expected new job to remain")
	}
}

func TestMemoryStoreCancel(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create running job
	job := &Job{
		ID:       "running-job",
		ToolName: "tool",
		Status:   StatusRunning,
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Cancel job
	if err := store.Cancel(ctx, "running-job"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Job should be failed
	got, _ := store.Get(ctx, "running-job")
	if got.Status != StatusFailed {
		t.Fatalf("expected status %q, got %q", StatusFailed, got.Status)
	}
	if got.Error != "job cancelled" {
		t.Fatalf("expected cancel error, got %q", got.Error)
	}

	// Cancel completed job should not change status
	completedJob := &Job{
		ID:       "completed-job",
		ToolName: "tool",
		Status:   StatusSucceeded,
	}
	if err := store.Create(ctx, completedJob); err != nil {
		t.Fatalf("create completed: %v", err)
	}
	if err := store.Cancel(ctx, "completed-job"); err != nil {
		t.Fatalf("cancel completed: %v", err)
	}
	got, _ = store.Get(ctx, "completed-job")
	if got.Status != StatusSucceeded {
		t.Fatalf("expected completed job status unchanged, got %q", got.Status)
	}
}
