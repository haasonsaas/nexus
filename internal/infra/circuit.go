package infra

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Circuit breaker states
const (
	CircuitClosed   = "closed"
	CircuitOpen     = "open"
	CircuitHalfOpen = "half-open"
)

// CircuitBreaker errors
var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// CircuitBreakerConfig configures a circuit breaker.
type CircuitBreakerConfig struct {
	// Name identifies this circuit breaker.
	Name string

	// FailureThreshold is the number of failures before opening.
	FailureThreshold int

	// SuccessThreshold is the number of successes in half-open to close.
	SuccessThreshold int

	// Timeout is how long the circuit stays open before trying half-open.
	Timeout time.Duration

	// OnStateChange is called when the circuit state changes.
	OnStateChange func(from, to string)
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	config CircuitBreakerConfig

	mu             sync.RWMutex
	state          string
	failures       int
	successes      int
	lastFailure    time.Time
	lastStateChange time.Time
}

// NewCircuitBreaker creates a new circuit breaker with the given config.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 2
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}

	return &CircuitBreaker{
		config:          config,
		state:           CircuitClosed,
		lastStateChange: time.Now(),
	}
}

// Execute runs the given function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	if err := cb.canExecute(); err != nil {
		return err
	}

	err := fn(ctx)
	cb.recordResult(err)
	return err
}

// ExecuteWithResult runs a function that returns a value with circuit breaker protection.
func ExecuteWithResult[T any](cb *CircuitBreaker, ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := cb.canExecute(); err != nil {
		return zero, err
	}

	result, err := fn(ctx)
	cb.recordResult(err)
	return result, err
}

// canExecute checks if execution is allowed and transitions state if needed.
func (cb *CircuitBreaker) canExecute() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return nil

	case CircuitOpen:
		// Check if timeout has elapsed
		if time.Since(cb.lastStateChange) >= cb.config.Timeout {
			cb.transitionTo(CircuitHalfOpen)
			return nil
		}
		return ErrCircuitOpen

	case CircuitHalfOpen:
		return nil

	default:
		return nil
	}
}

// recordResult records the result of an execution.
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}
}

// recordFailure records a failed execution.
func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.successes = 0
	cb.lastFailure = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.failures >= cb.config.FailureThreshold {
			cb.transitionTo(CircuitOpen)
		}

	case CircuitHalfOpen:
		cb.transitionTo(CircuitOpen)
	}
}

// recordSuccess records a successful execution.
func (cb *CircuitBreaker) recordSuccess() {
	switch cb.state {
	case CircuitClosed:
		cb.failures = 0

	case CircuitHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transitionTo(CircuitClosed)
		}
	}
}

// transitionTo changes the circuit breaker state.
func (cb *CircuitBreaker) transitionTo(newState string) {
	oldState := cb.state
	cb.state = newState
	cb.lastStateChange = time.Now()
	cb.failures = 0
	cb.successes = 0

	if cb.config.OnStateChange != nil {
		// Call asynchronously to avoid blocking
		go cb.config.OnStateChange(oldState, newState)
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats returns current circuit breaker statistics.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		Name:            cb.config.Name,
		State:           cb.state,
		Failures:        cb.failures,
		Successes:       cb.successes,
		LastFailure:     cb.lastFailure,
		LastStateChange: cb.lastStateChange,
	}
}

// Reset manually resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastStateChange = time.Now()
}

// CircuitBreakerStats contains statistics about a circuit breaker.
type CircuitBreakerStats struct {
	Name            string
	State           string
	Failures        int
	Successes       int
	LastFailure     time.Time
	LastStateChange time.Time
}

// CircuitBreakerRegistry manages multiple circuit breakers.
type CircuitBreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	defaults CircuitBreakerConfig
}

// NewCircuitBreakerRegistry creates a new registry with default config.
func NewCircuitBreakerRegistry(defaults CircuitBreakerConfig) *CircuitBreakerRegistry {
	if defaults.FailureThreshold <= 0 {
		defaults.FailureThreshold = 5
	}
	if defaults.SuccessThreshold <= 0 {
		defaults.SuccessThreshold = 2
	}
	if defaults.Timeout <= 0 {
		defaults.Timeout = 30 * time.Second
	}

	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		defaults: defaults,
	}
}

// Get returns or creates a circuit breaker with the given name.
func (r *CircuitBreakerRegistry) Get(name string) *CircuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[name]
	r.mu.RUnlock()

	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, ok := r.breakers[name]; ok {
		return cb
	}

	config := r.defaults
	config.Name = name
	cb = NewCircuitBreaker(config)
	r.breakers[name] = cb
	return cb
}

// GetWithConfig returns or creates a circuit breaker with custom config.
func (r *CircuitBreakerRegistry) GetWithConfig(name string, config CircuitBreakerConfig) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, ok := r.breakers[name]; ok {
		return cb
	}

	config.Name = name
	cb := NewCircuitBreaker(config)
	r.breakers[name] = cb
	return cb
}

// Stats returns statistics for all circuit breakers.
func (r *CircuitBreakerRegistry) Stats() []CircuitBreakerStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make([]CircuitBreakerStats, 0, len(r.breakers))
	for _, cb := range r.breakers {
		stats = append(stats, cb.Stats())
	}
	return stats
}

// OpenCircuits returns names of all open circuit breakers.
func (r *CircuitBreakerRegistry) OpenCircuits() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var open []string
	for name, cb := range r.breakers {
		if cb.State() == CircuitOpen {
			open = append(open, name)
		}
	}
	return open
}

// ResetAll resets all circuit breakers to closed state.
func (r *CircuitBreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}

// DefaultCircuitBreakerRegistry is the global circuit breaker registry.
var DefaultCircuitBreakerRegistry = NewCircuitBreakerRegistry(CircuitBreakerConfig{})

// GetCircuitBreaker returns a circuit breaker from the default registry.
func GetCircuitBreaker(name string) *CircuitBreaker {
	return DefaultCircuitBreakerRegistry.Get(name)
}
