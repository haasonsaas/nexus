package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// failingProvider always fails with the given error
type failingProvider struct {
	name       string
	err        error
	callCount  atomic.Int32
}

func (p *failingProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
	p.callCount.Add(1)
	return nil, p.err
}

func (p *failingProvider) Name() string              { return p.name }
func (p *failingProvider) Models() []Model           { return nil }
func (p *failingProvider) SupportsTools() bool       { return true }

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

func (p *successProvider) Name() string              { return p.name }
func (p *successProvider) Models() []Model           { return nil }
func (p *successProvider) SupportsTools() bool       { return true }

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

func (p *mockProviderWithModels) Name() string              { return p.name }
func (p *mockProviderWithModels) Models() []Model           { return p.models }
func (p *mockProviderWithModels) SupportsTools() bool       { return true }
