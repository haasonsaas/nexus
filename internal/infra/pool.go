package infra

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrPoolClosed is returned when operations are attempted on a closed pool.
	ErrPoolClosed = errors.New("pool is closed")
	// ErrPoolExhausted is returned when the pool has no available resources.
	ErrPoolExhausted = errors.New("pool exhausted")
)

// Resource represents a poolable resource.
type Resource[T any] struct {
	Value     T
	CreatedAt time.Time
	LastUsed  time.Time
}

// PoolConfig configures a resource pool.
type PoolConfig[T any] struct {
	// MaxSize is the maximum number of resources in the pool.
	MaxSize int
	// MinIdle is the minimum number of idle resources to maintain.
	MinIdle int
	// MaxIdleTime is the maximum time a resource can be idle before being closed.
	MaxIdleTime time.Duration
	// MaxLifetime is the maximum lifetime of a resource.
	MaxLifetime time.Duration
	// Factory creates a new resource.
	Factory func(ctx context.Context) (T, error)
	// Close cleans up a resource when it's removed from the pool.
	Close func(T) error
	// Validate checks if a resource is still valid before returning it.
	Validate func(T) bool
}

// Pool manages a pool of reusable resources.
type Pool[T any] struct {
	config PoolConfig[T]
	mu     sync.Mutex
	cond   *sync.Cond
	idle   []*Resource[T]
	inUse  int
	closed atomic.Bool

	// Statistics
	created   atomic.Uint64
	reused    atomic.Uint64
	destroyed atomic.Uint64
	waitCount atomic.Int32
	waitTime  atomic.Int64
}

// NewPool creates a new resource pool.
func NewPool[T any](config PoolConfig[T]) *Pool[T] {
	if config.MaxSize <= 0 {
		config.MaxSize = 10
	}
	if config.Factory == nil {
		panic("pool: Factory is required")
	}

	p := &Pool[T]{
		config: config,
		idle:   make([]*Resource[T], 0, config.MaxSize),
	}
	p.cond = sync.NewCond(&p.mu)

	return p
}

// Get retrieves a resource from the pool, creating one if necessary.
func (p *Pool[T]) Get(ctx context.Context) (*Resource[T], error) {
	if p.closed.Load() {
		var zero T
		return &Resource[T]{Value: zero}, ErrPoolClosed
	}

	startWait := time.Now()
	p.mu.Lock()

	for {
		// Check if closed while waiting
		if p.closed.Load() {
			p.mu.Unlock()
			var zero T
			return &Resource[T]{Value: zero}, ErrPoolClosed
		}

		// Try to get an idle resource
		for len(p.idle) > 0 {
			res := p.idle[len(p.idle)-1]
			p.idle = p.idle[:len(p.idle)-1]

			// Check if resource is still valid
			if !p.isValid(res) {
				p.closeResource(res)
				continue
			}

			res.LastUsed = time.Now()
			p.inUse++
			p.mu.Unlock()
			p.reused.Add(1)
			return res, nil
		}

		// Check if we can create a new resource
		if p.inUse < p.config.MaxSize {
			p.inUse++
			p.mu.Unlock()

			res, err := p.createResource(ctx)
			if err != nil {
				p.mu.Lock()
				p.inUse--
				p.cond.Signal()
				p.mu.Unlock()
				return nil, err
			}
			return res, nil
		}

		// Need to wait for a resource to be returned
		p.waitCount.Add(1)

		// Wait with context support
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
			case <-done:
			}
		}()

		p.cond.Wait()
		close(done)

		if ctx.Err() != nil {
			p.waitCount.Add(-1)
			waited := time.Since(startWait)
			p.waitTime.Add(int64(waited))
			p.mu.Unlock()
			return nil, ctx.Err()
		}

		p.waitCount.Add(-1)
	}
}

// Put returns a resource to the pool.
func (p *Pool[T]) Put(res *Resource[T]) {
	if res == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.inUse--

	if p.closed.Load() {
		p.closeResource(res)
		return
	}

	// Validate before returning to pool
	if p.config.Validate != nil && !p.config.Validate(res.Value) {
		p.closeResource(res)
		p.cond.Signal()
		return
	}

	// Check lifetime
	if p.config.MaxLifetime > 0 && time.Since(res.CreatedAt) > p.config.MaxLifetime {
		p.closeResource(res)
		p.cond.Signal()
		return
	}

	res.LastUsed = time.Now()
	p.idle = append(p.idle, res)
	p.cond.Signal()
}

// Discard removes a resource from the pool without returning it.
// Use this when the resource is known to be invalid.
func (p *Pool[T]) Discard(res *Resource[T]) {
	if res == nil {
		return
	}

	p.mu.Lock()
	p.inUse--
	p.closeResource(res)
	p.cond.Signal()
	p.mu.Unlock()
}

// Close closes the pool and all resources.
func (p *Pool[T]) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Close all idle resources
	for _, res := range p.idle {
		p.closeResource(res)
	}
	p.idle = nil

	// Wake up any waiters
	p.cond.Broadcast()

	return nil
}

// Stats returns pool statistics.
func (p *Pool[T]) Stats() PoolStats {
	p.mu.Lock()
	idle := len(p.idle)
	inUse := p.inUse
	p.mu.Unlock()

	return PoolStats{
		MaxSize:   p.config.MaxSize,
		Idle:      idle,
		InUse:     inUse,
		Total:     idle + inUse,
		Created:   p.created.Load(),
		Reused:    p.reused.Load(),
		Destroyed: p.destroyed.Load(),
		Waiters:   int(p.waitCount.Load()),
		WaitTime:  time.Duration(p.waitTime.Load()),
	}
}

// PoolStats contains pool statistics.
type PoolStats struct {
	MaxSize   int
	Idle      int
	InUse     int
	Total     int
	Created   uint64
	Reused    uint64
	Destroyed uint64
	Waiters   int
	WaitTime  time.Duration
}

// ReuseRate returns the rate of resource reuse (0.0 to 1.0).
func (s PoolStats) ReuseRate() float64 {
	total := s.Created + s.Reused
	if total == 0 {
		return 0
	}
	return float64(s.Reused) / float64(total)
}

func (p *Pool[T]) createResource(ctx context.Context) (*Resource[T], error) {
	val, err := p.config.Factory(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	p.created.Add(1)

	return &Resource[T]{
		Value:     val,
		CreatedAt: now,
		LastUsed:  now,
	}, nil
}

func (p *Pool[T]) closeResource(res *Resource[T]) {
	if p.config.Close != nil {
		_ = p.config.Close(res.Value) //nolint:errcheck // close errors are logged by caller if needed
	}
	p.destroyed.Add(1)
}

func (p *Pool[T]) isValid(res *Resource[T]) bool {
	// Check idle time
	if p.config.MaxIdleTime > 0 && time.Since(res.LastUsed) > p.config.MaxIdleTime {
		return false
	}

	// Check lifetime
	if p.config.MaxLifetime > 0 && time.Since(res.CreatedAt) > p.config.MaxLifetime {
		return false
	}

	// Custom validation
	if p.config.Validate != nil && !p.config.Validate(res.Value) {
		return false
	}

	return true
}

// CleanupIdle removes idle resources that exceed the minimum idle count
// or have been idle too long.
func (p *Pool[T]) CleanupIdle() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return 0
	}

	removed := 0
	now := time.Now()

	// Keep resources that are valid and within limits
	valid := make([]*Resource[T], 0, len(p.idle))
	for _, res := range p.idle {
		// Check if we should keep this resource
		keep := true

		// Remove if we have more than MinIdle and it's been idle too long
		if len(valid) >= p.config.MinIdle {
			if p.config.MaxIdleTime > 0 && now.Sub(res.LastUsed) > p.config.MaxIdleTime {
				keep = false
			}
			if p.config.MaxLifetime > 0 && now.Sub(res.CreatedAt) > p.config.MaxLifetime {
				keep = false
			}
		}

		// Custom validation
		if keep && p.config.Validate != nil && !p.config.Validate(res.Value) {
			keep = false
		}

		if keep {
			valid = append(valid, res)
		} else {
			p.closeResource(res)
			removed++
		}
	}

	p.idle = valid
	return removed
}
