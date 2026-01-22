package infra

import (
	"sync"
	"sync/atomic"
)

// Group represents a class of work and forms a namespace in which
// units of work can be executed with duplicate suppression.
// This is similar to golang.org/x/sync/singleflight but with generics.
type Group[K comparable, V any] struct {
	mu    sync.Mutex
	calls map[K]*call[V]

	// Statistics
	hits   atomic.Uint64 // Number of deduplicated calls
	misses atomic.Uint64 // Number of actual executions
}

type call[V any] struct {
	wg     sync.WaitGroup
	val    V
	err    error
	dups   int
	shared bool
}

// Result holds the result of a Do call.
type Result[V any] struct {
	Val    V
	Err    error
	Shared bool // True if result was shared with another caller
}

// Do executes and returns the results of the given function, making
// sure that only one execution is in-flight for a given key at a
// time. If a duplicate comes in, the duplicate caller waits for the
// original to complete and receives the same results.
func (g *Group[K, V]) Do(key K, fn func() (V, error)) (V, error, bool) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[K]*call[V])
	}

	if c, ok := g.calls[key]; ok {
		c.dups++
		c.shared = true
		g.mu.Unlock()
		g.hits.Add(1)
		c.wg.Wait()
		return c.val, c.err, true
	}

	c := new(call[V])
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()
	g.misses.Add(1)

	g.doCall(c, key, fn)
	return c.val, c.err, c.shared
}

// DoChan is like Do but returns a channel that will receive the
// results when they are ready.
func (g *Group[K, V]) DoChan(key K, fn func() (V, error)) <-chan Result[V] {
	ch := make(chan Result[V], 1)
	go func() {
		val, err, shared := g.Do(key, fn)
		ch <- Result[V]{Val: val, Err: err, Shared: shared}
	}()
	return ch
}

func (g *Group[K, V]) doCall(c *call[V], key K, fn func() (V, error)) {
	defer func() {
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		c.wg.Done()
	}()

	c.val, c.err = fn()
}

// Forget tells the singleflight to forget about a key. Future calls
// to Do for this key will call the function rather than waiting for
// an earlier call to complete.
func (g *Group[K, V]) Forget(key K) {
	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
}

// Stats returns statistics about the group.
func (g *Group[K, V]) Stats() GroupStats {
	return GroupStats{
		Hits:   g.hits.Load(),
		Misses: g.misses.Load(),
	}
}

// GroupStats contains statistics about a singleflight group.
type GroupStats struct {
	Hits   uint64 // Calls that shared results
	Misses uint64 // Calls that executed the function
}

// HitRate returns the hit rate (0.0 to 1.0).
func (s GroupStats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// Coalescer wraps a function with singleflight behavior.
// It's a convenience wrapper around Group for simple use cases.
type Coalescer[K comparable, V any] struct {
	group Group[K, V]
	fn    func(K) (V, error)
}

// NewCoalescer creates a new coalescer with the given function.
func NewCoalescer[K comparable, V any](fn func(K) (V, error)) *Coalescer[K, V] {
	return &Coalescer[K, V]{fn: fn}
}

// Get returns the result for the given key, coalescing concurrent requests.
func (c *Coalescer[K, V]) Get(key K) (V, error) {
	val, err, _ := c.group.Do(key, func() (V, error) {
		return c.fn(key)
	})
	return val, err
}

// Stats returns statistics about the coalescer.
func (c *Coalescer[K, V]) Stats() GroupStats {
	return c.group.Stats()
}
