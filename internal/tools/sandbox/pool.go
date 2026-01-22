package sandbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Pool manages a pool of sandbox executors for efficient reuse.
type Pool struct {
	config    *Config
	executors map[string]*languagePool
	mu        sync.RWMutex
	closed    bool
}

// languagePool manages executors for a specific language.
type languagePool struct {
	language  string
	available chan RuntimeExecutor
	active    int
	maxSize   int
	mu        sync.Mutex
	config    *Config
}

// NewPool creates a new executor pool.
func NewPool(config *Config) (*Pool, error) {
	if config == nil {
		return nil, errors.New("config cannot be nil")
	}

	pool := &Pool{
		config:    config,
		executors: make(map[string]*languagePool),
	}

	// Pre-warm pools for each language
	languages := []string{"python", "nodejs", "go", "bash"}
	for _, lang := range languages {
		langPool := &languagePool{
			language:  lang,
			available: make(chan RuntimeExecutor, config.MaxPoolSize),
			maxSize:   config.MaxPoolSize,
			config:    config,
		}
		pool.executors[lang] = langPool

		// Pre-create initial executors
		for i := 0; i < config.PoolSize && i < config.MaxPoolSize; i++ {
			executor, err := pool.createExecutor(lang)
			if err != nil {
				// Log error but continue - pool can grow on demand
				continue
			}
			langPool.available <- executor
		}
	}

	return pool, nil
}

// Get retrieves an executor from the pool for the specified language.
func (p *Pool) Get(ctx context.Context, language string) (RuntimeExecutor, error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, errors.New("pool is closed")
	}
	p.mu.RUnlock()

	langPool, ok := p.executors[language]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	// Try to get an available executor
	select {
	case executor := <-langPool.available:
		return executor, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// No executor available, try to create a new one
		langPool.mu.Lock()
		if langPool.active < langPool.maxSize {
			langPool.active++
			langPool.mu.Unlock()

			executor, err := p.createExecutor(language)
			if err != nil {
				langPool.mu.Lock()
				langPool.active--
				langPool.mu.Unlock()
				return nil, err
			}
			return executor, nil
		}
		langPool.mu.Unlock()

		// Wait for an executor to become available
		select {
		case executor := <-langPool.available:
			return executor, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return nil, errors.New("timeout waiting for executor")
		}
	}
}

// Put returns an executor to the pool.
func (p *Pool) Put(executor RuntimeExecutor) {
	if executor == nil {
		return
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		executor.Close()
		return
	}

	langPool, ok := p.executors[executor.Language()]
	if !ok {
		executor.Close()
		return
	}

	// Try to return to pool, otherwise close
	select {
	case langPool.available <- executor:
		// Successfully returned to pool
	default:
		// Pool is full, close the executor
		executor.Close()
		langPool.mu.Lock()
		langPool.active--
		langPool.mu.Unlock()
	}
}

// Close shuts down the pool and all executors.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	// Close all executors in all pools
	for _, langPool := range p.executors {
		close(langPool.available)
		for executor := range langPool.available {
			executor.Close()
		}
	}

	return nil
}

// createExecutor creates a new executor for the specified language.
func (p *Pool) createExecutor(language string) (RuntimeExecutor, error) {
	switch p.config.Backend {
	case BackendDocker:
		return newDockerExecutor(language, p.config.DefaultCPU, p.config.DefaultMemory, p.config.NetworkEnabled)
	case BackendFirecracker:
		return newFirecrackerExecutor(language, p.config.DefaultCPU, p.config.DefaultMemory, p.config.NetworkEnabled)
	default:
		return nil, fmt.Errorf("unsupported backend: %s", p.config.Backend)
	}
}

// firecrackerBackend holds the shared Firecracker backend instance.
var (
	firecrackerBackend     *firecrackerBackendWrapper
	firecrackerBackendOnce sync.Once
	firecrackerBackendErr  error
)

// firecrackerBackendWrapper wraps the Firecracker backend for lazy initialization.
type firecrackerBackendWrapper struct {
	backend interface {
		Run(ctx context.Context, params *ExecuteParams, workspace string) (*ExecuteResult, error)
		Close() error
	}
}

// newFirecrackerExecutor creates a new Firecracker-based executor.
func newFirecrackerExecutor(language string, cpuLimit, memLimit int, networkEnabled bool) (RuntimeExecutor, error) {
	// Lazy initialization of shared backend
	firecrackerBackendOnce.Do(func() {
		// Import the firecracker package at runtime to avoid circular imports
		// The actual backend is created in the firecracker package
		firecrackerBackendErr = fmt.Errorf("firecracker backend not initialized - call InitFirecrackerBackend first")
	})

	if firecrackerBackendErr != nil {
		// Fall back to Docker if Firecracker is not available
		return newDockerExecutor(language, cpuLimit, memLimit, networkEnabled)
	}

	return &firecrackerExecutorWrapper{
		language: language,
		cpuLimit: cpuLimit,
		memLimit: memLimit,
		backend:  firecrackerBackend,
	}, nil
}

// firecrackerExecutorWrapper wraps a Firecracker executor.
type firecrackerExecutorWrapper struct {
	language string
	cpuLimit int
	memLimit int
	backend  *firecrackerBackendWrapper
}

// Run executes code in a Firecracker microVM.
func (f *firecrackerExecutorWrapper) Run(ctx context.Context, params *ExecuteParams, workspace string) (*ExecuteResult, error) {
	if f.backend == nil || f.backend.backend == nil {
		return nil, fmt.Errorf("firecracker backend not initialized")
	}
	return f.backend.backend.Run(ctx, params, workspace)
}

// Language returns the language this executor handles.
func (f *firecrackerExecutorWrapper) Language() string {
	return f.language
}

// Close cleans up resources.
func (f *firecrackerExecutorWrapper) Close() error {
	// Individual executors don't close the shared backend
	return nil
}

// InitFirecrackerBackend initializes the Firecracker backend.
// This should be called during application startup if Firecracker support is desired.
func InitFirecrackerBackend(backend interface {
	Run(ctx context.Context, params *ExecuteParams, workspace string) (*ExecuteResult, error)
	Close() error
}) {
	firecrackerBackendOnce.Do(func() {
		firecrackerBackend = &firecrackerBackendWrapper{backend: backend}
		firecrackerBackendErr = nil
	})
}

// Stats returns statistics about the pool.
func (p *Pool) Stats() map[string]PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make(map[string]PoolStats)
	for lang, langPool := range p.executors {
		langPool.mu.Lock()
		stats[lang] = PoolStats{
			Language:  lang,
			Available: len(langPool.available),
			Active:    langPool.active,
			MaxSize:   langPool.maxSize,
		}
		langPool.mu.Unlock()
	}

	return stats
}

// PoolStats contains statistics about a language pool.
type PoolStats struct {
	Language  string `json:"language"`
	Available int    `json:"available"`
	Active    int    `json:"active"`
	MaxSize   int    `json:"max_size"`
}

// Warmup pre-warms the pool by creating executors.
func (p *Pool) Warmup(ctx context.Context, language string, count int) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return errors.New("pool is closed")
	}
	p.mu.RUnlock()

	langPool, ok := p.executors[language]
	if !ok {
		return fmt.Errorf("unsupported language: %s", language)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			langPool.mu.Lock()
			if langPool.active >= langPool.maxSize {
				langPool.mu.Unlock()
				return
			}
			langPool.active++
			langPool.mu.Unlock()

			executor, err := p.createExecutor(language)
			if err != nil {
				langPool.mu.Lock()
				langPool.active--
				langPool.mu.Unlock()
				errChan <- err
				return
			}

			select {
			case langPool.available <- executor:
				// Successfully added to pool
			default:
				// Pool is full
				executor.Close()
				langPool.mu.Lock()
				langPool.active--
				langPool.mu.Unlock()
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Return first error if any
	for err := range errChan {
		return err
	}

	return nil
}

// Shrink reduces the pool size by closing idle executors.
func (p *Pool) Shrink(language string, count int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return errors.New("pool is closed")
	}

	langPool, ok := p.executors[language]
	if !ok {
		return fmt.Errorf("unsupported language: %s", language)
	}

	for i := 0; i < count; i++ {
		select {
		case executor := <-langPool.available:
			executor.Close()
			langPool.mu.Lock()
			langPool.active--
			langPool.mu.Unlock()
		default:
			// No more available executors
			return nil
		}
	}

	return nil
}

// Health checks the health of the pool.
func (p *Pool) Health() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return errors.New("pool is closed")
	}

	// Check each language pool
	for lang, langPool := range p.executors {
		langPool.mu.Lock()
		available := len(langPool.available)
		active := langPool.active
		langPool.mu.Unlock()

		// Ensure we have at least one executor available or active
		if available == 0 && active == 0 {
			return fmt.Errorf("no executors available for %s", lang)
		}
	}

	return nil
}
