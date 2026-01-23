package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/jobs"
)

// mockStore is a test mock implementing jobs.Store.
type mockStore struct {
	jobs     map[string]*jobs.Job
	getErr   error
	listErr  error
	cancelFn func(id string) error
}

func newMockStore() *mockStore {
	return &mockStore{
		jobs: make(map[string]*jobs.Job),
	}
}

func (m *mockStore) Create(ctx context.Context, job *jobs.Job) error {
	m.jobs[job.ID] = job
	return nil
}

func (m *mockStore) Update(ctx context.Context, job *jobs.Job) error {
	m.jobs[job.ID] = job
	return nil
}

func (m *mockStore) Get(ctx context.Context, id string) (*jobs.Job, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.jobs[id], nil
}

func (m *mockStore) List(ctx context.Context, limit, offset int) ([]*jobs.Job, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]*jobs.Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		result = append(result, j)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (m *mockStore) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

func (m *mockStore) Cancel(ctx context.Context, id string) error {
	if m.cancelFn != nil {
		return m.cancelFn(id)
	}
	if j, ok := m.jobs[id]; ok {
		j.Status = jobs.StatusFailed
	}
	return nil
}

func TestStatusTool(t *testing.T) {
	t.Run("Name and Description", func(t *testing.T) {
		tool := NewStatusTool(nil)
		if tool.Name() != "job_status" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "job_status")
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("Schema returns valid JSON", func(t *testing.T) {
		tool := NewStatusTool(nil)
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Errorf("Schema() invalid JSON: %v", err)
		}
	})

	t.Run("returns error when store unavailable", func(t *testing.T) {
		tool := NewStatusTool(nil)
		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"123"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError to be true")
		}
		if result.Content != "job store unavailable" {
			t.Errorf("Content = %q, want %q", result.Content, "job store unavailable")
		}
	})

	t.Run("returns error for missing job_id", func(t *testing.T) {
		store := newMockStore()
		tool := NewStatusTool(store)
		_, err := tool.Execute(context.Background(), []byte(`{}`))
		if err == nil {
			t.Error("expected error for missing job_id")
		}
	})

	t.Run("returns job not found", func(t *testing.T) {
		store := newMockStore()
		tool := NewStatusTool(store)
		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"nonexistent"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError || result.Content != "job not found" {
			t.Errorf("expected job not found error, got: %+v", result)
		}
	})

	t.Run("returns job status successfully", func(t *testing.T) {
		store := newMockStore()
		store.jobs["job-1"] = &jobs.Job{
			ID:       "job-1",
			ToolName: "test",
			Status:   jobs.StatusRunning,
		}
		tool := NewStatusTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"job-1"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("unexpected error result: %s", result.Content)
		}

		var job jobs.Job
		if err := json.Unmarshal([]byte(result.Content), &job); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}
		if job.ID != "job-1" {
			t.Errorf("job ID = %q, want %q", job.ID, "job-1")
		}
	})
}

func TestCancelTool(t *testing.T) {
	t.Run("Name and Description", func(t *testing.T) {
		tool := NewCancelTool(nil)
		if tool.Name() != "job_cancel" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "job_cancel")
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("returns error when store unavailable", func(t *testing.T) {
		tool := NewCancelTool(nil)
		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"123"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError to be true")
		}
	})

	t.Run("returns error for missing job_id", func(t *testing.T) {
		store := newMockStore()
		tool := NewCancelTool(store)
		_, err := tool.Execute(context.Background(), []byte(`{}`))
		if err == nil {
			t.Error("expected error for missing job_id")
		}
	})

	t.Run("returns job not found", func(t *testing.T) {
		store := newMockStore()
		tool := NewCancelTool(store)
		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"nonexistent"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError || result.Content != "job not found" {
			t.Errorf("expected job not found error, got: %+v", result)
		}
	})

	t.Run("cannot cancel completed job", func(t *testing.T) {
		store := newMockStore()
		store.jobs["job-1"] = &jobs.Job{
			ID:     "job-1",
			Status: jobs.StatusSucceeded,
		}
		tool := NewCancelTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"job-1"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error for completed job")
		}
	})

	t.Run("cancels running job successfully", func(t *testing.T) {
		store := newMockStore()
		store.jobs["job-1"] = &jobs.Job{
			ID:     "job-1",
			Status: jobs.StatusRunning,
		}
		tool := NewCancelTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"job-1"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("unexpected error result: %s", result.Content)
		}
		if result.Content != "Job job-1 cancelled successfully" {
			t.Errorf("Content = %q", result.Content)
		}
	})

	t.Run("cancels queued job successfully", func(t *testing.T) {
		store := newMockStore()
		store.jobs["job-1"] = &jobs.Job{
			ID:     "job-1",
			Status: jobs.StatusQueued,
		}
		tool := NewCancelTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{"job_id":"job-1"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("unexpected error result: %s", result.Content)
		}
	})
}

func TestListTool(t *testing.T) {
	t.Run("Name and Description", func(t *testing.T) {
		tool := NewListTool(nil)
		if tool.Name() != "job_list" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "job_list")
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("Schema returns valid JSON", func(t *testing.T) {
		tool := NewListTool(nil)
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Errorf("Schema() invalid JSON: %v", err)
		}
	})

	t.Run("returns error when store unavailable", func(t *testing.T) {
		tool := NewListTool(nil)
		result, err := tool.Execute(context.Background(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError to be true")
		}
	})

	t.Run("returns no jobs found", func(t *testing.T) {
		store := newMockStore()
		tool := NewListTool(store)
		result, err := tool.Execute(context.Background(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Content != "no jobs found" {
			t.Errorf("Content = %q, want %q", result.Content, "no jobs found")
		}
	})

	t.Run("lists jobs successfully", func(t *testing.T) {
		store := newMockStore()
		store.jobs["job-1"] = &jobs.Job{ID: "job-1", Status: jobs.StatusRunning}
		store.jobs["job-2"] = &jobs.Job{ID: "job-2", Status: jobs.StatusQueued}
		tool := NewListTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("unexpected error result: %s", result.Content)
		}

		var jobList []*jobs.Job
		if err := json.Unmarshal([]byte(result.Content), &jobList); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}
		if len(jobList) != 2 {
			t.Errorf("expected 2 jobs, got %d", len(jobList))
		}
	})

	t.Run("filters by status", func(t *testing.T) {
		store := newMockStore()
		store.jobs["job-1"] = &jobs.Job{ID: "job-1", Status: jobs.StatusRunning}
		store.jobs["job-2"] = &jobs.Job{ID: "job-2", Status: jobs.StatusQueued}
		store.jobs["job-3"] = &jobs.Job{ID: "job-3", Status: jobs.StatusRunning}
		tool := NewListTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{"status":"running"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var jobList []*jobs.Job
		json.Unmarshal([]byte(result.Content), &jobList)
		for _, j := range jobList {
			if j.Status != jobs.StatusRunning {
				t.Errorf("expected running status, got %s", j.Status)
			}
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		store := newMockStore()
		for i := 0; i < 20; i++ {
			store.jobs["job-"+string(rune('A'+i))] = &jobs.Job{
				ID:     "job-" + string(rune('A'+i)),
				Status: jobs.StatusQueued,
			}
		}
		tool := NewListTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{"limit":5}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var jobList []*jobs.Job
		json.Unmarshal([]byte(result.Content), &jobList)
		if len(jobList) > 5 {
			t.Errorf("expected max 5 jobs, got %d", len(jobList))
		}
	})

	t.Run("uses default limit when zero or negative", func(t *testing.T) {
		store := newMockStore()
		for i := 0; i < 5; i++ {
			store.jobs["job-"+string(rune('A'+i))] = &jobs.Job{
				ID:     "job-" + string(rune('A'+i)),
				Status: jobs.StatusQueued,
			}
		}
		tool := NewListTool(store)

		result, err := tool.Execute(context.Background(), []byte(`{"limit":0}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("unexpected error: %s", result.Content)
		}
	})
}
