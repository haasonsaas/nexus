package infra

import (
	"context"
	"sync"
	"time"
)

// Semaphore is a weighted semaphore for limiting concurrent access to resources.
// Unlike a simple mutex, it allows multiple concurrent acquisitions up to a limit,
// and each acquisition can request a different number of permits (weight).
type Semaphore struct {
	mu       sync.Mutex
	cond     *sync.Cond
	max      int64
	current  int64
	waiters  int
	acquired int64 // Total successful acquisitions
	released int64 // Total releases
	timedOut int64 // Total timeouts
}

// NewSemaphore creates a new semaphore with the given maximum permits.
// For example, NewSemaphore(10) allows up to 10 concurrent permits.
func NewSemaphore(max int64) *Semaphore {
	if max <= 0 {
		max = 1
	}
	s := &Semaphore{max: max}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire blocks until n permits are available or the context is cancelled.
// Returns nil on success, or context error if cancelled/timed out.
func (s *Semaphore) Acquire(ctx context.Context, n int64) error {
	if n <= 0 {
		return nil
	}
	if n > s.max {
		n = s.max // Cap at maximum
	}

	// Fast path: try to acquire without waiting
	s.mu.Lock()
	if s.current+n <= s.max && s.waiters == 0 {
		s.current += n
		s.acquired++
		s.mu.Unlock()
		return nil
	}

	// Slow path: need to wait
	s.waiters++

	// Start a goroutine to handle context cancellation
	done := make(chan struct{})
	cancelled := false

	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			cancelled = true
			s.timedOut++
			s.cond.Broadcast()
			s.mu.Unlock()
		case <-done:
		}
	}()

	for {
		if cancelled {
			s.waiters--
			s.mu.Unlock()
			close(done)
			return ctx.Err()
		}

		if s.current+n <= s.max {
			s.current += n
			s.acquired++
			s.waiters--
			s.mu.Unlock()
			close(done)
			return nil
		}

		s.cond.Wait()
	}
}

// TryAcquire attempts to acquire n permits without blocking.
// Returns true if successful, false otherwise.
func (s *Semaphore) TryAcquire(n int64) bool {
	if n <= 0 {
		return true
	}
	if n > s.max {
		n = s.max
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current+n <= s.max {
		s.current += n
		s.acquired++
		return true
	}
	return false
}

// AcquireWithTimeout attempts to acquire n permits with a timeout.
// Returns nil on success, context.DeadlineExceeded on timeout.
func (s *Semaphore) AcquireWithTimeout(n int64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Acquire(ctx, n)
}

// Release releases n permits back to the semaphore.
// It is safe to call Release more times than Acquire (the semaphore will cap at max).
func (s *Semaphore) Release(n int64) {
	if n <= 0 {
		return
	}

	s.mu.Lock()
	s.current -= n
	if s.current < 0 {
		s.current = 0
	}
	s.released++
	s.cond.Broadcast()
	s.mu.Unlock()
}

// Available returns the number of permits currently available.
func (s *Semaphore) Available() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.max - s.current
}

// InUse returns the number of permits currently in use.
func (s *Semaphore) InUse() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Waiters returns the number of goroutines currently waiting to acquire.
func (s *Semaphore) Waiters() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiters
}

// Stats returns statistics about the semaphore.
func (s *Semaphore) Stats() SemaphoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SemaphoreStats{
		Max:       s.max,
		InUse:     s.current,
		Available: s.max - s.current,
		Waiters:   s.waiters,
		Acquired:  s.acquired,
		Released:  s.released,
		TimedOut:  s.timedOut,
	}
}

// SemaphoreStats contains statistics about a semaphore.
type SemaphoreStats struct {
	Max       int64
	InUse     int64
	Available int64
	Waiters   int
	Acquired  int64
	Released  int64
	TimedOut  int64
}

// SemaphorePool manages named semaphores for different resources.
type SemaphorePool struct {
	mu         sync.RWMutex
	semaphores map[string]*Semaphore
	defaultMax int64
}

// NewSemaphorePool creates a new semaphore pool with a default max permits.
func NewSemaphorePool(defaultMax int64) *SemaphorePool {
	if defaultMax <= 0 {
		defaultMax = 10
	}
	return &SemaphorePool{
		semaphores: make(map[string]*Semaphore),
		defaultMax: defaultMax,
	}
}

// Get returns the semaphore for the given name, creating it if necessary.
func (p *SemaphorePool) Get(name string) *Semaphore {
	p.mu.RLock()
	sem, ok := p.semaphores[name]
	p.mu.RUnlock()

	if ok {
		return sem
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if sem, ok := p.semaphores[name]; ok {
		return sem
	}

	sem = NewSemaphore(p.defaultMax)
	p.semaphores[name] = sem
	return sem
}

// GetOrCreate returns the semaphore for the given name with a specific max.
func (p *SemaphorePool) GetOrCreate(name string, max int64) *Semaphore {
	p.mu.Lock()
	defer p.mu.Unlock()

	if sem, ok := p.semaphores[name]; ok {
		return sem
	}

	sem := NewSemaphore(max)
	p.semaphores[name] = sem
	return sem
}

// Acquire acquires n permits from the named semaphore.
func (p *SemaphorePool) Acquire(ctx context.Context, name string, n int64) error {
	return p.Get(name).Acquire(ctx, n)
}

// Release releases n permits to the named semaphore.
func (p *SemaphorePool) Release(name string, n int64) {
	p.mu.RLock()
	sem, ok := p.semaphores[name]
	p.mu.RUnlock()

	if ok {
		sem.Release(n)
	}
}

// Stats returns statistics for all semaphores in the pool.
func (p *SemaphorePool) Stats() map[string]SemaphoreStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make(map[string]SemaphoreStats, len(p.semaphores))
	for name, sem := range p.semaphores {
		stats[name] = sem.Stats()
	}
	return stats
}
