package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FailoverConfig configures the failover orchestrator.
type FailoverConfig struct {
	// MaxRetries is the maximum number of retry attempts per provider
	MaxRetries int

	// RetryBackoff is the initial backoff between retries
	RetryBackoff time.Duration

	// MaxRetryBackoff is the maximum backoff duration
	MaxRetryBackoff time.Duration

	// FailoverOnRateLimit enables failover on rate limit errors
	FailoverOnRateLimit bool

	// FailoverOnServerError enables failover on server errors
	FailoverOnServerError bool

	// CircuitBreakerThreshold is the number of failures before opening circuit
	CircuitBreakerThreshold int

	// CircuitBreakerTimeout is how long to wait before trying a failed provider
	CircuitBreakerTimeout time.Duration
}

// DefaultFailoverConfig returns sensible defaults for failover.
func DefaultFailoverConfig() *FailoverConfig {
	return &FailoverConfig{
		MaxRetries:              2,
		RetryBackoff:            100 * time.Millisecond,
		MaxRetryBackoff:         5 * time.Second,
		FailoverOnRateLimit:     true,
		FailoverOnServerError:   true,
		CircuitBreakerThreshold: 3,
		CircuitBreakerTimeout:   30 * time.Second,
	}
}

// ProviderState tracks the health of a provider.
type ProviderState struct {
	Name          string
	Failures      int
	LastFailure   time.Time
	CircuitOpen   bool
	CircuitOpenAt time.Time
}

// IsAvailable returns true if the provider can accept requests.
func (s *ProviderState) IsAvailable(cfg *FailoverConfig) bool {
	if !s.CircuitOpen {
		return true
	}
	// Check if circuit timeout has passed
	if time.Since(s.CircuitOpenAt) > cfg.CircuitBreakerTimeout {
		return true
	}
	return false
}

// FailoverOrchestrator manages multiple LLM providers with automatic failover.
type FailoverOrchestrator struct {
	providers []LLMProvider
	config    *FailoverConfig
	states    map[string]*ProviderState
	mu        sync.RWMutex
	metrics   *FailoverMetrics
}

// FailoverMetrics tracks failover statistics.
type FailoverMetrics struct {
	mu               sync.Mutex
	TotalRequests    int64
	TotalFailovers   int64
	TotalRetries     int64
	ProviderFailures map[string]int64
	CircuitBreaks    int64
}

// NewFailoverOrchestrator creates a new failover orchestrator.
func NewFailoverOrchestrator(primary LLMProvider, config *FailoverConfig) *FailoverOrchestrator {
	if config == nil {
		config = DefaultFailoverConfig()
	}

	return &FailoverOrchestrator{
		providers: []LLMProvider{primary},
		config:    config,
		states:    make(map[string]*ProviderState),
		metrics: &FailoverMetrics{
			ProviderFailures: make(map[string]int64),
		},
	}
}

// AddProvider adds a fallback provider.
func (o *FailoverOrchestrator) AddProvider(p LLMProvider) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.providers = append(o.providers, p)
}

// Complete implements LLMProvider with failover support.
func (o *FailoverOrchestrator) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	o.metrics.mu.Lock()
	o.metrics.TotalRequests++
	o.metrics.mu.Unlock()

	o.mu.RLock()
	providersCopy := make([]LLMProvider, len(o.providers))
	copy(providersCopy, o.providers)
	o.mu.RUnlock()

	var lastErr error

	for i, provider := range providersCopy {
		state := o.getOrCreateState(provider.Name())

		// Check if provider is available (circuit breaker)
		if !state.IsAvailable(o.config) {
			continue
		}

		// Try this provider with retries
		ch, err := o.tryProvider(ctx, provider, req)
		if err == nil {
			// Success - reset failures
			o.recordSuccess(provider.Name())
			return ch, nil
		}

		lastErr = err

		// Record failure
		o.recordFailure(provider.Name(), err)

		// Check if we should failover
		if !o.shouldFailover(err) {
			// Non-retriable error, don't try other providers
			return nil, err
		}

		// Record failover
		if i < len(providersCopy)-1 {
			o.metrics.mu.Lock()
			o.metrics.TotalFailovers++
			o.metrics.mu.Unlock()
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no available providers")
	}

	return nil, lastErr
}

// tryProvider attempts to complete with retries.
func (o *FailoverOrchestrator) tryProvider(ctx context.Context, provider LLMProvider, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	var lastErr error
	backoff := o.config.RetryBackoff

	for attempt := 0; attempt <= o.config.MaxRetries; attempt++ {
		ch, err := provider.Complete(ctx, req)
		if err == nil {
			return ch, nil
		}

		lastErr = err

		// Check if retryable
		if !isProviderRetryable(err) {
			return nil, err
		}

		// Check context
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Don't retry on last attempt
		if attempt >= o.config.MaxRetries {
			break
		}

		o.metrics.mu.Lock()
		o.metrics.TotalRetries++
		o.metrics.mu.Unlock()

		// Exponential backoff
		select {
		case <-time.After(backoff):
			backoff *= 2
			if backoff > o.config.MaxRetryBackoff {
				backoff = o.config.MaxRetryBackoff
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
}

// shouldFailover determines if an error warrants trying another provider.
func (o *FailoverOrchestrator) shouldFailover(err error) bool {
	if shouldProviderFailover(err) {
		return true
	}

	// Check configured failover conditions
	reason := classifyProviderError(err)

	if o.config.FailoverOnRateLimit && reason == "rate_limit" {
		return true
	}

	if o.config.FailoverOnServerError && reason == "server_error" {
		return true
	}

	return false
}

// isProviderRetryable checks if an error is worth retrying.
func isProviderRetryable(err error) bool {
	reason := classifyProviderError(err)
	switch reason {
	case "rate_limit", "timeout", "server_error":
		return true
	default:
		return false
	}
}

// shouldProviderFailover checks if an error warrants trying a different provider.
func shouldProviderFailover(err error) bool {
	reason := classifyProviderError(err)
	switch reason {
	case "billing", "auth", "model_unavailable":
		return true
	default:
		return false
	}
}

// classifyProviderError determines the error type from the error content.
func classifyProviderError(err error) string {
	if err == nil {
		return "unknown"
	}

	errStr := strings.ToLower(err.Error())

	// Check for timeout patterns
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context deadline") {
		return "timeout"
	}

	// Check for rate limit patterns
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "429") {
		return "rate_limit"
	}

	// Check for authentication patterns
	if strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "invalid api key") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") {
		return "auth"
	}

	// Check for billing patterns
	if strings.Contains(errStr, "billing") ||
		strings.Contains(errStr, "payment") ||
		strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "402") {
		return "billing"
	}

	// Check for model availability patterns
	if strings.Contains(errStr, "model not found") ||
		strings.Contains(errStr, "does not exist") ||
		strings.Contains(errStr, "unavailable") {
		return "model_unavailable"
	}

	// Check for server error patterns
	if strings.Contains(errStr, "internal server") ||
		strings.Contains(errStr, "server error") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "504") {
		return "server_error"
	}

	// Check for invalid request patterns
	if strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "bad request") ||
		strings.Contains(errStr, "400") {
		return "invalid_request"
	}

	return "unknown"
}

// getOrCreateState returns the state for a provider.
func (o *FailoverOrchestrator) getOrCreateState(name string) *ProviderState {
	o.mu.Lock()
	defer o.mu.Unlock()

	if state, ok := o.states[name]; ok {
		return state
	}

	state := &ProviderState{Name: name}
	o.states[name] = state
	return state
}

// recordSuccess records a successful request.
func (o *FailoverOrchestrator) recordSuccess(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	state := o.states[name]
	if state == nil {
		return
	}

	state.Failures = 0
	state.CircuitOpen = false
}

// recordFailure records a failed request.
func (o *FailoverOrchestrator) recordFailure(name string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	state := o.states[name]
	if state == nil {
		state = &ProviderState{Name: name}
		o.states[name] = state
	}

	state.Failures++
	state.LastFailure = time.Now()

	// Check circuit breaker
	if state.Failures >= o.config.CircuitBreakerThreshold {
		if !state.CircuitOpen {
			state.CircuitOpen = true
			state.CircuitOpenAt = time.Now()
			o.metrics.mu.Lock()
			o.metrics.CircuitBreaks++
			o.metrics.mu.Unlock()
		}
	}

	o.metrics.mu.Lock()
	o.metrics.ProviderFailures[name]++
	o.metrics.mu.Unlock()
}

// Name implements LLMProvider.
func (o *FailoverOrchestrator) Name() string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if len(o.providers) == 0 {
		return "failover"
	}
	return "failover:" + o.providers[0].Name()
}

// Models implements LLMProvider.
func (o *FailoverOrchestrator) Models() []Model {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var all []Model
	seen := make(map[string]bool)

	for _, p := range o.providers {
		for _, m := range p.Models() {
			if !seen[m.ID] {
				seen[m.ID] = true
				all = append(all, m)
			}
		}
	}

	return all
}

// SupportsTools implements LLMProvider.
func (o *FailoverOrchestrator) SupportsTools() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for _, p := range o.providers {
		if p.SupportsTools() {
			return true
		}
	}
	return false
}

// Metrics returns a snapshot of failover metrics.
func (o *FailoverOrchestrator) Metrics() FailoverMetrics {
	o.metrics.mu.Lock()
	defer o.metrics.mu.Unlock()

	// Copy the map
	failures := make(map[string]int64)
	for k, v := range o.metrics.ProviderFailures {
		failures[k] = v
	}

	return FailoverMetrics{
		TotalRequests:    o.metrics.TotalRequests,
		TotalFailovers:   o.metrics.TotalFailovers,
		TotalRetries:     o.metrics.TotalRetries,
		ProviderFailures: failures,
		CircuitBreaks:    o.metrics.CircuitBreaks,
	}
}

// ProviderStates returns the current state of all providers.
func (o *FailoverOrchestrator) ProviderStates() []ProviderState {
	o.mu.RLock()
	defer o.mu.RUnlock()

	states := make([]ProviderState, 0, len(o.states))
	for _, s := range o.states {
		states = append(states, *s)
	}
	return states
}

// ResetCircuitBreaker resets the circuit breaker for a provider.
func (o *FailoverOrchestrator) ResetCircuitBreaker(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if state, ok := o.states[name]; ok {
		state.Failures = 0
		state.CircuitOpen = false
	}
}

// ResetAllCircuitBreakers resets all circuit breakers.
func (o *FailoverOrchestrator) ResetAllCircuitBreakers() {
	o.mu.Lock()
	defer o.mu.Unlock()

	for _, state := range o.states {
		state.Failures = 0
		state.CircuitOpen = false
	}
}
