package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Status represents the state of a job.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Job represents an async tool execution.
type Job struct {
	ID         string             `json:"id"`
	ToolName   string             `json:"tool_name"`
	ToolCallID string             `json:"tool_call_id"`
	Status     Status             `json:"status"`
	CreatedAt  time.Time          `json:"created_at"`
	StartedAt  time.Time          `json:"started_at,omitempty"`
	FinishedAt time.Time          `json:"finished_at,omitempty"`
	Result     *models.ToolResult `json:"result,omitempty"`
	Error      string             `json:"error,omitempty"`

	// cancelFunc is set when the job starts and can be called to cancel execution.
	cancelFunc context.CancelFunc `json:"-"`
}

// Store persists job records.
type Store interface {
	Create(ctx context.Context, job *Job) error
	Update(ctx context.Context, job *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, limit, offset int) ([]*Job, error)
	// Prune removes jobs older than the given duration. Returns count of pruned jobs.
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
	// Cancel marks a running job as failed with a cancellation error.
	Cancel(ctx context.Context, id string) error
}

// MemoryStore keeps jobs in memory.
type MemoryStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	keys []string
}

// NewMemoryStore returns a new in-memory job store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs: make(map[string]*Job),
	}
}

// Create stores a job.
func (s *MemoryStore) Create(ctx context.Context, job *Job) error {
	if job == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; !exists {
		s.keys = append(s.keys, job.ID)
	}
	s.jobs[job.ID] = cloneJob(job)
	return nil
}

// Update updates a job record.
func (s *MemoryStore) Update(ctx context.Context, job *Job) error {
	if job == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = cloneJob(job)
	return nil
}

// Get returns a job by id.
func (s *MemoryStore) Get(ctx context.Context, id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, nil
	}
	return cloneJob(job), nil
}

// List returns jobs in insertion order.
func (s *MemoryStore) List(ctx context.Context, limit, offset int) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > len(s.keys) {
		limit = len(s.keys)
	}
	if offset >= len(s.keys) {
		return nil, nil
	}
	end := offset + limit
	if end > len(s.keys) {
		end = len(s.keys)
	}
	result := make([]*Job, 0, end-offset)
	for _, id := range s.keys[offset:end] {
		if job, ok := s.jobs[id]; ok {
			result = append(result, cloneJob(job))
		}
	}
	return result, nil
}

// Prune removes jobs older than the given duration.
func (s *MemoryStore) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	var pruned int64
	var newKeys []string

	for _, id := range s.keys {
		job, ok := s.jobs[id]
		if !ok {
			continue
		}
		if job.CreatedAt.Before(cutoff) {
			delete(s.jobs, id)
			pruned++
		} else {
			newKeys = append(newKeys, id)
		}
	}
	s.keys = newKeys
	return pruned, nil
}

// Cancel marks a running job as failed with a cancellation error.
func (s *MemoryStore) Cancel(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return nil
	}
	if job.Status == StatusRunning || job.Status == StatusQueued {
		// Call the cancel function if set
		if job.cancelFunc != nil {
			job.cancelFunc()
		}
		job.Status = StatusFailed
		job.Error = "job cancelled"
		job.FinishedAt = time.Now()
	}
	return nil
}

// SetCancelFunc sets the cancel function for a running job.
func (s *MemoryStore) SetCancelFunc(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job, ok := s.jobs[id]; ok {
		job.cancelFunc = cancel
	}
}

func cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	clone := *job
	if job.Result != nil {
		result := *job.Result
		clone.Result = &result
	}
	return &clone
}
