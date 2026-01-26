package cron

import (
	"context"
	"sync"
	"time"
)

// ExecutionStatus represents the status of a cron job execution.
type ExecutionStatus string

const (
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionSucceeded ExecutionStatus = "succeeded"
	ExecutionFailed    ExecutionStatus = "failed"
)

// JobExecution captures a single cron job execution attempt.
type JobExecution struct {
	ID          string          `json:"id"`
	JobID       string          `json:"job_id"`
	Status      ExecutionStatus `json:"status"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt time.Time       `json:"completed_at,omitempty"`
	Duration    time.Duration   `json:"duration,omitempty"`
	Output      string          `json:"output,omitempty"`
	Error       string          `json:"error,omitempty"`
	Retry       int             `json:"retry"`
}

// ExecutionStore persists cron job execution history.
type ExecutionStore interface {
	Create(ctx context.Context, exec *JobExecution) error
	Update(ctx context.Context, exec *JobExecution) error
	Get(ctx context.Context, id string) (*JobExecution, error)
	List(ctx context.Context, jobID string, limit, offset int) ([]*JobExecution, error)
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
}

// MemoryExecutionStore keeps execution history in memory.
type MemoryExecutionStore struct {
	mu         sync.RWMutex
	executions map[string]*JobExecution
	order      []string
}

// NewMemoryExecutionStore creates an in-memory execution store.
func NewMemoryExecutionStore() *MemoryExecutionStore {
	return &MemoryExecutionStore{
		executions: make(map[string]*JobExecution),
	}
}

// Create stores a new execution record.
func (s *MemoryExecutionStore) Create(ctx context.Context, exec *JobExecution) error {
	if exec == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.executions[exec.ID]; !exists {
		s.order = append(s.order, exec.ID)
	}
	s.executions[exec.ID] = cloneExecution(exec)
	return nil
}

// Update updates an execution record.
func (s *MemoryExecutionStore) Update(ctx context.Context, exec *JobExecution) error {
	if exec == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executions[exec.ID] = cloneExecution(exec)
	return nil
}

// Get returns an execution by id.
func (s *MemoryExecutionStore) Get(ctx context.Context, id string) (*JobExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exec, ok := s.executions[id]
	if !ok {
		return nil, nil
	}
	return cloneExecution(exec), nil
}

// List returns executions, optionally filtered by job id.
func (s *MemoryExecutionStore) List(ctx context.Context, jobID string, limit, offset int) ([]*JobExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > len(s.order) {
		limit = len(s.order)
	}
	if offset >= len(s.order) {
		return nil, nil
	}
	end := offset + limit
	if end > len(s.order) {
		end = len(s.order)
	}
	result := make([]*JobExecution, 0, end-offset)
	for _, id := range s.order[offset:end] {
		exec, ok := s.executions[id]
		if !ok {
			continue
		}
		if jobID != "" && exec.JobID != jobID {
			continue
		}
		result = append(result, cloneExecution(exec))
	}
	return result, nil
}

// Prune removes executions older than the provided duration.
func (s *MemoryExecutionStore) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	var pruned int64
	newOrder := make([]string, 0, len(s.order))
	for _, id := range s.order {
		exec, ok := s.executions[id]
		if !ok {
			continue
		}
		if exec.StartedAt.Before(cutoff) {
			delete(s.executions, id)
			pruned++
			continue
		}
		newOrder = append(newOrder, id)
	}
	s.order = newOrder
	return pruned, nil
}

func cloneExecution(exec *JobExecution) *JobExecution {
	if exec == nil {
		return nil
	}
	clone := *exec
	return &clone
}
