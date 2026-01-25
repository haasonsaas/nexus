package backoff

import (
	"testing"
	"time"
)

func TestComputeBackoffWithRand(t *testing.T) {
	tests := []struct {
		name        string
		policy      BackoffPolicy
		attempt     int
		randomValue float64
		expected    time.Duration
	}{
		{
			name: "first attempt with no jitter",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0,
			},
			attempt:     1,
			randomValue: 0.5,
			expected:    100 * time.Millisecond,
		},
		{
			name: "second attempt doubles",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0,
			},
			attempt:     2,
			randomValue: 0.5,
			expected:    200 * time.Millisecond,
		},
		{
			name: "third attempt quadruples",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0,
			},
			attempt:     3,
			randomValue: 0.5,
			expected:    400 * time.Millisecond,
		},
		{
			name: "fifth attempt with factor 2",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0,
			},
			attempt:     5,
			randomValue: 0.5,
			expected:    1600 * time.Millisecond,
		},
		{
			name: "clamped to max",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     500,
				Factor:    2,
				Jitter:    0,
			},
			attempt:     10,
			randomValue: 0.5,
			expected:    500 * time.Millisecond,
		},
		{
			name: "with 10% jitter at max random",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0.1,
			},
			attempt:     1,
			randomValue: 1.0,
			// base = 100, jitter = 100 * 0.1 * 1.0 = 10, total = 110
			expected: 110 * time.Millisecond,
		},
		{
			name: "with 10% jitter at zero random",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0.1,
			},
			attempt:     1,
			randomValue: 0.0,
			// base = 100, jitter = 100 * 0.1 * 0.0 = 0, total = 100
			expected: 100 * time.Millisecond,
		},
		{
			name: "with 50% jitter at mid random",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0.5,
			},
			attempt:     2,
			randomValue: 0.5,
			// base = 200, jitter = 200 * 0.5 * 0.5 = 50, total = 250
			expected: 250 * time.Millisecond,
		},
		{
			name: "attempt 0 treated as 1",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0,
			},
			attempt:     0,
			randomValue: 0.5,
			expected:    100 * time.Millisecond,
		},
		{
			name: "negative attempt treated as 1",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    2,
				Jitter:    0,
			},
			attempt:     -5,
			randomValue: 0.5,
			expected:    100 * time.Millisecond,
		},
		{
			name: "factor 1.5",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     10000,
				Factor:    1.5,
				Jitter:    0,
			},
			attempt:     3,
			randomValue: 0.5,
			// base = 100 * 1.5^2 = 225
			expected: 225 * time.Millisecond,
		},
		{
			name: "jitter causes max clamping",
			policy: BackoffPolicy{
				InitialMs: 100,
				MaxMs:     105,
				Factor:    1,
				Jitter:    0.5,
			},
			attempt:     1,
			randomValue: 1.0,
			// base = 100, jitter = 100 * 0.5 * 1.0 = 50, total would be 150, clamped to 105
			expected: 105 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeBackoffWithRand(tt.policy, tt.attempt, tt.randomValue)
			if got != tt.expected {
				t.Errorf("ComputeBackoffWithRand() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestComputeBackoff_JitterRange(t *testing.T) {
	// Test that jitter produces values within expected range
	policy := BackoffPolicy{
		InitialMs: 100,
		MaxMs:     10000,
		Factor:    2,
		Jitter:    0.2,
	}

	// For attempt 1: base = 100, max jitter = 100 * 0.2 = 20
	// Expected range: [100, 120]
	minExpected := 100 * time.Millisecond
	maxExpected := 120 * time.Millisecond

	// Run multiple times to check jitter randomization
	for i := 0; i < 100; i++ {
		got := ComputeBackoff(policy, 1)
		if got < minExpected || got > maxExpected {
			t.Errorf("ComputeBackoff() = %v, want in range [%v, %v]", got, minExpected, maxExpected)
		}
	}
}

func TestDefaultPolicy(t *testing.T) {
	policy := DefaultPolicy()

	if policy.InitialMs != 100 {
		t.Errorf("InitialMs = %v, want 100", policy.InitialMs)
	}
	if policy.MaxMs != 30000 {
		t.Errorf("MaxMs = %v, want 30000", policy.MaxMs)
	}
	if policy.Factor != 2 {
		t.Errorf("Factor = %v, want 2", policy.Factor)
	}
	if policy.Jitter != 0.1 {
		t.Errorf("Jitter = %v, want 0.1", policy.Jitter)
	}
}

func TestAggressivePolicy(t *testing.T) {
	policy := AggressivePolicy()

	if policy.InitialMs != 50 {
		t.Errorf("InitialMs = %v, want 50", policy.InitialMs)
	}
	if policy.MaxMs != 5000 {
		t.Errorf("MaxMs = %v, want 5000", policy.MaxMs)
	}
	if policy.Factor != 1.5 {
		t.Errorf("Factor = %v, want 1.5", policy.Factor)
	}
	if policy.Jitter != 0.05 {
		t.Errorf("Jitter = %v, want 0.05", policy.Jitter)
	}
}

func TestConservativePolicy(t *testing.T) {
	policy := ConservativePolicy()

	if policy.InitialMs != 500 {
		t.Errorf("InitialMs = %v, want 500", policy.InitialMs)
	}
	if policy.MaxMs != 60000 {
		t.Errorf("MaxMs = %v, want 60000", policy.MaxMs)
	}
	if policy.Factor != 2.5 {
		t.Errorf("Factor = %v, want 2.5", policy.Factor)
	}
	if policy.Jitter != 0.2 {
		t.Errorf("Jitter = %v, want 0.2", policy.Jitter)
	}
}

func TestPolicyComparison(t *testing.T) {
	// Verify that aggressive < default < conservative at same attempt
	aggressive := AggressivePolicy()
	defaultP := DefaultPolicy()
	conservative := ConservativePolicy()

	// Use zero jitter random for deterministic comparison
	aggBackoff := ComputeBackoffWithRand(aggressive, 1, 0)
	defBackoff := ComputeBackoffWithRand(defaultP, 1, 0)
	consBackoff := ComputeBackoffWithRand(conservative, 1, 0)

	if aggBackoff >= defBackoff {
		t.Errorf("aggressive backoff %v should be < default backoff %v", aggBackoff, defBackoff)
	}
	if defBackoff >= consBackoff {
		t.Errorf("default backoff %v should be < conservative backoff %v", defBackoff, consBackoff)
	}
}
