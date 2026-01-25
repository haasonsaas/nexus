// Package backoff provides exponential backoff utilities with jitter for retry logic.
package backoff

import (
	"math"
	"math/rand"
	"time"
)

// BackoffPolicy defines the parameters for exponential backoff calculation.
type BackoffPolicy struct {
	// InitialMs is the initial backoff duration in milliseconds.
	InitialMs float64
	// MaxMs is the maximum backoff duration in milliseconds.
	MaxMs float64
	// Factor is the exponential factor applied to each attempt.
	Factor float64
	// Jitter is the randomization factor (0.0 to 1.0) applied to the backoff.
	Jitter float64
}

// ComputeBackoff calculates the backoff duration for a given attempt number.
// The formula is: base = initialMs * factor^(attempt-1), jitter = base * jitter * random()
// Returns min(maxMs, base + jitter) as a time.Duration.
// Attempt numbers start at 1.
func ComputeBackoff(policy BackoffPolicy, attempt int) time.Duration {
	return ComputeBackoffWithRand(policy, attempt, rand.Float64()) // #nosec G404 -- jitter does not require cryptographic randomness
}

// ComputeBackoffWithRand calculates the backoff duration using a provided random value.
// This is useful for testing to provide deterministic results.
// The randomValue should be in the range [0.0, 1.0).
func ComputeBackoffWithRand(policy BackoffPolicy, attempt int, randomValue float64) time.Duration {
	// Ensure attempt is at least 1
	exp := math.Max(float64(attempt-1), 0)

	// Calculate base backoff: initialMs * factor^(attempt-1)
	base := policy.InitialMs * math.Pow(policy.Factor, exp)

	// Calculate jitter: base * jitter * random()
	jitterAmount := base * policy.Jitter * randomValue

	// Calculate total backoff and clamp to max
	total := math.Min(policy.MaxMs, base+jitterAmount)

	// Round to nearest millisecond and convert to duration
	return time.Duration(math.Round(total)) * time.Millisecond
}

// DefaultPolicy returns a sensible default backoff policy.
// Initial: 100ms, Max: 30s, Factor: 2, Jitter: 10%
func DefaultPolicy() BackoffPolicy {
	return BackoffPolicy{
		InitialMs: 100,
		MaxMs:     30000,
		Factor:    2,
		Jitter:    0.1,
	}
}

// AggressivePolicy returns a policy for quick retries with shorter delays.
// Initial: 50ms, Max: 5s, Factor: 1.5, Jitter: 5%
func AggressivePolicy() BackoffPolicy {
	return BackoffPolicy{
		InitialMs: 50,
		MaxMs:     5000,
		Factor:    1.5,
		Jitter:    0.05,
	}
}

// ConservativePolicy returns a policy for slow retries with longer delays.
// Initial: 500ms, Max: 60s, Factor: 2.5, Jitter: 20%
func ConservativePolicy() BackoffPolicy {
	return BackoffPolicy{
		InitialMs: 500,
		MaxMs:     60000,
		Factor:    2.5,
		Jitter:    0.2,
	}
}
