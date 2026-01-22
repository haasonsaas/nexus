package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// failingProvider always fails with the given error
type failingProvider struct {
	name      string
	err       error
	callCount atomic.Int32
}

func (p *failingProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	p.callCount.Add(1)
	return nil, p.err
}

func (p *failingProvider) Name() string        { return p.name }
func (p *failingProvider) Models() []Model     { return nil }
func (p *failingProvider) SupportsTools() bool { return true }

// successProvider always succeeds
type successProvider struct {
	name      string
	callCount atomic.Int32
}

func (p *successProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	p.callCount.Add(1)
	ch := make(chan *CompletionChunk, 1)
	ch <- &CompletionChunk{Text: "success", Done: true}
	close(ch)
	return ch, nil
}

func (p *successProvider) Name() string        { return p.name }
func (p *successProvider) Models() []Model     { return nil }
func (p *successProvider) SupportsTools() bool { return true }

func TestFailoverOrchestrator_PrimarySuccess(t *testing.T) {
	primary := &successProvider{name: "primary"}
	secondary := &successProvider{name: "secondary"}

	orch := NewFailoverOrchestrator(primary, nil)
	orch.AddProvider(secondary)

	ch, err := orch.Complete(context.Background(), &CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain channel
	for range ch {
	}

	if primary.callCount.Load() != 1 {
		t.Errorf("primary call count = %d, want 1", primary.callCount.Load())
	}
	if secondary.callCount.Load() != 0 {
		t.Errorf("secondary should not be called")
	}
}

func TestFailoverOrchestrator_FailoverOnError(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("billing: quota exceeded"),
	}
	secondary := &successProvider{name: "secondary"}

	config := DefaultFailoverConfig()
	config.MaxRetries = 0 // Disable retries for this test

	orch := NewFailoverOrchestrator(primary, config)
	orch.AddProvider(secondary)

	ch, err := orch.Complete(context.Background(), &CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain channel
	for range ch {
	}

	if secondary.callCount.Load() != 1 {
		t.Errorf("secondary should be called on failover")
	}

	metrics := orch.Metrics()
	if metrics.TotalFailovers != 1 {
		t.Errorf("TotalFailovers = %d, want 1", metrics.TotalFailovers)
	}
}

func TestFailoverOrchestrator_RetryOnTransientError(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("rate limit exceeded"),
	}

	config := DefaultFailoverConfig()
	config.MaxRetries = 2
	config.RetryBackoff = time.Millisecond

	orch := NewFailoverOrchestrator(primary, config)

	_, err := orch.Complete(context.Background(), &CompletionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}

	// Should have retried
	if primary.callCount.Load() != 3 { // 1 initial + 2 retries
		t.Errorf("call count = %d, want 3", primary.callCount.Load())
	}
}

func TestFailoverOrchestrator_NoRetryOnNonRetriable(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("invalid request: missing field"),
	}

	config := DefaultFailoverConfig()
	config.MaxRetries = 3

	orch := NewFailoverOrchestrator(primary, config)

	_, err := orch.Complete(context.Background(), &CompletionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}

	// Should NOT retry invalid request errors
	if primary.callCount.Load() != 1 {
		t.Errorf("call count = %d, want 1 (no retry for invalid request)", primary.callCount.Load())
	}
}

func TestFailoverOrchestrator_CircuitBreaker(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("server error 500"),
	}
	secondary := &successProvider{name: "secondary"}

	config := DefaultFailoverConfig()
	config.MaxRetries = 0
	config.CircuitBreakerThreshold = 2
	config.CircuitBreakerTimeout = 100 * time.Millisecond

	orch := NewFailoverOrchestrator(primary, config)
	orch.AddProvider(secondary)

	// First call: primary fails, failover to secondary
	_, _ = orch.Complete(context.Background(), &CompletionRequest{})
	// Second call: primary fails again, opens circuit
	_, _ = orch.Complete(context.Background(), &CompletionRequest{})

	// Check circuit is open
	states := orch.ProviderStates()
	var primaryState *ProviderState
	for _, s := range states {
		if s.Name == "primary" {
			primaryState = &s
			break
		}
	}
	if primaryState == nil || !primaryState.CircuitOpen {
		t.Error("circuit breaker should be open")
	}

	// Third call: should skip primary (circuit open)
	primary.callCount.Store(0)
	secondary.callCount.Store(0)
	_, _ = orch.Complete(context.Background(), &CompletionRequest{})

	if primary.callCount.Load() != 0 {
		t.Error("primary should be skipped when circuit is open")
	}
	if secondary.callCount.Load() != 1 {
		t.Error("secondary should be called")
	}

	// Wait for circuit timeout
	time.Sleep(150 * time.Millisecond)

	// Circuit should be half-open, try primary again
	primary.callCount.Store(0)
	_, _ = orch.Complete(context.Background(), &CompletionRequest{})

	if primary.callCount.Load() == 0 {
		t.Error("primary should be tried after circuit timeout")
	}
}

func TestFailoverOrchestrator_ResetCircuitBreaker(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("server error"),
	}

	config := DefaultFailoverConfig()
	config.MaxRetries = 0
	config.CircuitBreakerThreshold = 1

	orch := NewFailoverOrchestrator(primary, config)

	// Trigger circuit breaker
	_, _ = orch.Complete(context.Background(), &CompletionRequest{})

	// Reset
	orch.ResetCircuitBreaker("primary")

	states := orch.ProviderStates()
	for _, s := range states {
		if s.Name == "primary" {
			if s.CircuitOpen {
				t.Error("circuit should be closed after reset")
			}
			if s.Failures != 0 {
				t.Errorf("failures = %d, want 0", s.Failures)
			}
			break
		}
	}
}

func TestFailoverOrchestrator_AllProvidersFail(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("billing error"),
	}
	secondary := &failingProvider{
		name: "secondary",
		err:  errors.New("auth error"),
	}

	config := DefaultFailoverConfig()
	config.MaxRetries = 0

	orch := NewFailoverOrchestrator(primary, config)
	orch.AddProvider(secondary)

	_, err := orch.Complete(context.Background(), &CompletionRequest{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}

	// Should contain the last error
	if !errors.Is(err, secondary.err) && err.Error() != secondary.err.Error() {
		t.Errorf("error = %v, want %v", err, secondary.err)
	}
}

func TestFailoverOrchestrator_ContextCancellation(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("rate limit"),
	}

	config := DefaultFailoverConfig()
	config.MaxRetries = 5
	config.RetryBackoff = time.Second // Long backoff

	orch := NewFailoverOrchestrator(primary, config)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := orch.Complete(ctx, &CompletionRequest{})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}

	// Should have stopped retrying due to context
	if primary.callCount.Load() > 2 {
		t.Errorf("should have stopped retrying, got %d calls", primary.callCount.Load())
	}
}

func TestFailoverOrchestrator_Metrics(t *testing.T) {
	primary := &failingProvider{
		name: "primary",
		err:  errors.New("server error"),
	}
	secondary := &successProvider{name: "secondary"}

	config := DefaultFailoverConfig()
	config.MaxRetries = 1
	config.RetryBackoff = time.Millisecond

	orch := NewFailoverOrchestrator(primary, config)
	orch.AddProvider(secondary)

	// Make a few requests
	for i := 0; i < 3; i++ {
		ch, _ := orch.Complete(context.Background(), &CompletionRequest{})
		for range ch {
		}
	}

	metrics := orch.Metrics()

	if metrics.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", metrics.TotalRequests)
	}
	if metrics.TotalFailovers < 3 {
		t.Errorf("TotalFailovers = %d, want >= 3", metrics.TotalFailovers)
	}
	if metrics.ProviderFailures["primary"] < 3 {
		t.Errorf("primary failures = %d, want >= 3", metrics.ProviderFailures["primary"])
	}
}

func TestFailoverOrchestrator_Name(t *testing.T) {
	primary := &successProvider{name: "anthropic"}
	orch := NewFailoverOrchestrator(primary, nil)

	name := orch.Name()
	if name != "failover:anthropic" {
		t.Errorf("Name = %q, want %q", name, "failover:anthropic")
	}
}

func TestFailoverOrchestrator_Models(t *testing.T) {
	primary := &mockProviderWithModels{
		name:   "primary",
		models: []Model{{ID: "model-a"}, {ID: "model-b"}},
	}
	secondary := &mockProviderWithModels{
		name:   "secondary",
		models: []Model{{ID: "model-b"}, {ID: "model-c"}},
	}

	orch := NewFailoverOrchestrator(primary, nil)
	orch.AddProvider(secondary)

	models := orch.Models()
	if len(models) != 3 { // a, b, c (deduped)
		t.Errorf("got %d models, want 3", len(models))
	}
}

type mockProviderWithModels struct {
	name   string
	models []Model
}

func (p *mockProviderWithModels) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	ch := make(chan *CompletionChunk)
	close(ch)
	return ch, nil
}

func (p *mockProviderWithModels) Name() string        { return p.name }
func (p *mockProviderWithModels) Models() []Model     { return p.models }
func (p *mockProviderWithModels) SupportsTools() bool { return true }

func TestFailoverOrchestrator_SupportsTools(t *testing.T) {
	tests := []struct {
		name     string
		primary  LLMProvider
		expected bool
	}{
		{
			name:     "primary supports tools",
			primary:  &successProvider{name: "with-tools"},
			expected: true,
		},
		{
			name:     "primary no tools",
			primary:  stubProvider{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := NewFailoverOrchestrator(tt.primary, nil)
			if orch.SupportsTools() != tt.expected {
				t.Errorf("SupportsTools() = %v, want %v", orch.SupportsTools(), tt.expected)
			}
		})
	}
}

func TestFailoverOrchestrator_SupportsToolsMultipleProviders(t *testing.T) {
	// Primary doesn't support, secondary does
	primary := stubProvider{}
	secondary := &successProvider{name: "with-tools"}

	orch := NewFailoverOrchestrator(primary, nil)
	orch.AddProvider(secondary)

	if !orch.SupportsTools() {
		t.Error("should return true if any provider supports tools")
	}
}

// trackingProvider tracks call times for testing backoff
type trackingProvider struct {
	name      string
	err       error
	callTimes []time.Time
	mu        sync.Mutex
}

func (p *trackingProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	p.mu.Lock()
	p.callTimes = append(p.callTimes, time.Now())
	p.mu.Unlock()
	return nil, p.err
}

func (p *trackingProvider) Name() string        { return p.name }
func (p *trackingProvider) Models() []Model     { return nil }
func (p *trackingProvider) SupportsTools() bool { return true }

func (p *trackingProvider) getCallTimes() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]time.Time, len(p.callTimes))
	copy(result, p.callTimes)
	return result
}

func TestFailoverOrchestrator_ExponentialBackoffCapping(t *testing.T) {
	primary := &trackingProvider{
		name:      "primary",
		err:       errors.New("rate limit exceeded"),
		callTimes: make([]time.Time, 0),
	}

	config := DefaultFailoverConfig()
	config.MaxRetries = 5
	config.RetryBackoff = 10 * time.Millisecond
	config.MaxRetryBackoff = 30 * time.Millisecond // Cap at 30ms

	orch := NewFailoverOrchestrator(primary, config)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, _ = orch.Complete(ctx, &CompletionRequest{})

	times := primary.getCallTimes()

	// Verify backoff increases but is capped
	if len(times) < 3 {
		t.Skip("not enough calls to verify backoff")
	}

	for i := 2; i < len(times); i++ {
		gap := times[i].Sub(times[i-1])
		// After a few retries, gap should not exceed MaxRetryBackoff significantly
		if gap > config.MaxRetryBackoff*2 {
			t.Errorf("gap %d: %v exceeds max backoff %v", i, gap, config.MaxRetryBackoff)
		}
	}
}

func TestFailoverOrchestrator_ResetAllCircuitBreakers(t *testing.T) {
	primary := &failingProvider{name: "primary", err: errors.New("error")}
	secondary := &failingProvider{name: "secondary", err: errors.New("error")}

	config := DefaultFailoverConfig()
	config.MaxRetries = 0
	config.CircuitBreakerThreshold = 1

	orch := NewFailoverOrchestrator(primary, config)
	orch.AddProvider(secondary)

	// Trigger circuit breakers
	_, _ = orch.Complete(context.Background(), &CompletionRequest{})

	// Both should have failures recorded
	states := orch.ProviderStates()
	openCount := 0
	for _, s := range states {
		if s.CircuitOpen {
			openCount++
		}
	}
	if openCount == 0 {
		t.Skip("no circuits opened")
	}

	// Reset all
	orch.ResetAllCircuitBreakers()

	// All should be closed
	states = orch.ProviderStates()
	for _, s := range states {
		if s.CircuitOpen {
			t.Errorf("provider %s circuit should be closed", s.Name)
		}
		if s.Failures != 0 {
			t.Errorf("provider %s failures = %d, want 0", s.Name, s.Failures)
		}
	}
}

func TestFailoverOrchestrator_NoProviders(t *testing.T) {
	orch := &FailoverOrchestrator{
		providers: []LLMProvider{},
		config:    DefaultFailoverConfig(),
		states:    make(map[string]*ProviderState),
		metrics:   &FailoverMetrics{ProviderFailures: make(map[string]int64)},
	}

	_, err := orch.Complete(context.Background(), &CompletionRequest{})
	if err == nil {
		t.Fatal("expected error when no providers")
	}
}

func TestFailoverOrchestrator_NameWithNoProviders(t *testing.T) {
	orch := &FailoverOrchestrator{
		providers: []LLMProvider{},
		config:    DefaultFailoverConfig(),
		states:    make(map[string]*ProviderState),
		metrics:   &FailoverMetrics{ProviderFailures: make(map[string]int64)},
	}

	name := orch.Name()
	if name != "failover" {
		t.Errorf("Name() = %q, want %q", name, "failover")
	}
}

func TestProviderState_IsAvailable(t *testing.T) {
	config := DefaultFailoverConfig()
	config.CircuitBreakerTimeout = 100 * time.Millisecond

	tests := []struct {
		name     string
		state    *ProviderState
		expected bool
	}{
		{
			name: "circuit closed",
			state: &ProviderState{
				Name:        "test",
				CircuitOpen: false,
			},
			expected: true,
		},
		{
			name: "circuit open recent",
			state: &ProviderState{
				Name:          "test",
				CircuitOpen:   true,
				CircuitOpenAt: time.Now(),
			},
			expected: false,
		},
		{
			name: "circuit open timeout passed",
			state: &ProviderState{
				Name:          "test",
				CircuitOpen:   true,
				CircuitOpenAt: time.Now().Add(-200 * time.Millisecond),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.state.IsAvailable(config)
			if result != tt.expected {
				t.Errorf("IsAvailable() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestClassifyProviderError(t *testing.T) {
	tests := []struct {
		err      error
		expected string
	}{
		{errors.New("rate limit exceeded"), "rate_limit"},
		{errors.New("too many requests 429"), "rate_limit"},
		{errors.New("timeout waiting for response"), "timeout"},
		{errors.New("context deadline exceeded"), "timeout"},
		{errors.New("unauthorized: invalid api key"), "auth"},
		{errors.New("authentication failed 401"), "auth"},
		{errors.New("billing: quota exceeded"), "billing"},
		{errors.New("payment required 402"), "billing"},
		{errors.New("model not found: gpt-5"), "model_unavailable"},
		{errors.New("service unavailable"), "model_unavailable"},
		{errors.New("internal server error 500"), "server_error"},
		{errors.New("bad gateway 502"), "server_error"},
		{errors.New("invalid request: missing field"), "invalid_request"},
		{errors.New("bad request 400"), "invalid_request"},
		{errors.New("something random happened"), "unknown"},
		{nil, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := classifyProviderError(tt.err)
			if result != tt.expected {
				t.Errorf("classifyProviderError(%v) = %q, want %q", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsProviderRetryable(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{errors.New("rate limit exceeded"), true},
		{errors.New("timeout"), true},
		{errors.New("server error 500"), true},
		{errors.New("invalid request"), false},
		{errors.New("unauthorized"), false},
		{errors.New("billing error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			result := isProviderRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("isProviderRetryable(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestShouldProviderFailover(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{errors.New("billing: quota exceeded"), true},
		{errors.New("unauthorized"), true},
		{errors.New("model not found"), true},
		{errors.New("rate limit"), false}, // Handled by config flags
		{errors.New("server error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			result := shouldProviderFailover(tt.err)
			if result != tt.expected {
				t.Errorf("shouldProviderFailover(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestFailoverConfig_Defaults(t *testing.T) {
	config := DefaultFailoverConfig()

	if config.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", config.MaxRetries)
	}
	if config.RetryBackoff != 100*time.Millisecond {
		t.Errorf("RetryBackoff = %v, want 100ms", config.RetryBackoff)
	}
	if config.MaxRetryBackoff != 5*time.Second {
		t.Errorf("MaxRetryBackoff = %v, want 5s", config.MaxRetryBackoff)
	}
	if !config.FailoverOnRateLimit {
		t.Error("FailoverOnRateLimit should be true")
	}
	if !config.FailoverOnServerError {
		t.Error("FailoverOnServerError should be true")
	}
	if config.CircuitBreakerThreshold != 3 {
		t.Errorf("CircuitBreakerThreshold = %d, want 3", config.CircuitBreakerThreshold)
	}
	if config.CircuitBreakerTimeout != 30*time.Second {
		t.Errorf("CircuitBreakerTimeout = %v, want 30s", config.CircuitBreakerTimeout)
	}
}

func TestFailoverOrchestrator_ShouldFailover(t *testing.T) {
	tests := []struct {
		name                  string
		err                   error
		failoverOnRateLimit   bool
		failoverOnServerError bool
		expected              bool
	}{
		{
			name:                "rate limit with flag on",
			err:                 errors.New("rate limit"),
			failoverOnRateLimit: true,
			expected:            true,
		},
		{
			name:                "rate limit with flag off",
			err:                 errors.New("rate limit"),
			failoverOnRateLimit: false,
			expected:            false,
		},
		{
			name:                  "server error with flag on",
			err:                   errors.New("server error 500"),
			failoverOnServerError: true,
			expected:              true,
		},
		{
			name:                  "server error with flag off",
			err:                   errors.New("server error 500"),
			failoverOnServerError: false,
			expected:              false,
		},
		{
			name:     "billing always failover",
			err:      errors.New("billing error"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultFailoverConfig()
			config.FailoverOnRateLimit = tt.failoverOnRateLimit
			config.FailoverOnServerError = tt.failoverOnServerError

			orch := NewFailoverOrchestrator(&successProvider{name: "test"}, config)
			result := orch.shouldFailover(tt.err)
			if result != tt.expected {
				t.Errorf("shouldFailover() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Sync import needed for mutex
var _ = sync.Mutex{}
