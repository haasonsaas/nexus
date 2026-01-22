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
