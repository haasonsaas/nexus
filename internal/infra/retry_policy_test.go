package infra

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetChannelRetryPolicy(t *testing.T) {
	tests := []struct {
		channel  string
		expected string
	}{
		{"discord", "discord"},
		{"Discord", "discord"},
		{"DISCORD", "discord"},
		{"telegram", "telegram"},
		{"slack", "slack"},
		{"email", "email"},
		{"unknown", "default"},
		{"", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			policy := GetChannelRetryPolicy(tt.channel)
			if policy.Name != tt.expected {
				t.Errorf("expected policy %s, got %s", tt.expected, policy.Name)
			}
		})
	}
}

func TestIsDiscordRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"rate limit error", errors.New("rate limit exceeded"), true},
		{"429 error", errors.New("HTTP 429 Too Many Requests"), true},
		{"Rate Limit case insensitive", errors.New("RATE LIMIT"), true},
		{"connection error", errors.New("connection refused"), false},
		{"permanent error", AsPermanent(errors.New("rate limit")), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDiscordRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsTelegramRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"429 error", errors.New("error code: 429"), true},
		{"timeout", errors.New("request timeout"), true},
		{"connection error", errors.New("connection reset"), true},
		{"unavailable", errors.New("service temporarily unavailable"), true},
		{"closed", errors.New("connection closed"), true},
		{"auth error", errors.New("unauthorized"), false},
		{"permanent error", AsPermanent(errors.New("timeout")), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTelegramRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsSlackRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"rate limit", errors.New("rate_limit_exceeded"), true},
		{"rate-limit", errors.New("rate-limit"), true},
		{"429 error", errors.New("429 Too Many Requests"), true},
		{"timeout", errors.New("request timeout"), true},
		{"retry", errors.New("please retry"), true},
		{"auth error", errors.New("invalid_auth"), false},
		{"permanent error", AsPermanent(errors.New("rate limit")), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSlackRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsEmailRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"timeout", errors.New("connection timeout"), true},
		{"temporary failure", errors.New("temporary failure"), true},
		{"try again", errors.New("try again later"), true},
		{"SMTP 421", errors.New("421 Service not available"), true},
		{"SMTP 450", errors.New("450 Mailbox unavailable"), true},
		{"SMTP 451", errors.New("451 Local error"), true},
		{"SMTP 452", errors.New("452 Insufficient storage"), true},
		{"SMTP 550", errors.New("550 User not found"), false},
		{"permanent error", AsPermanent(errors.New("timeout")), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEmailRetryable(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestParseRetryAfterFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"retry_after: 5", 5000},
		{"retry_after\":5", 5000},
		{"retry-after: 10", 10000},
		{"retry_after: 0", 0},
		{"no number here", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseRetryAfterFromString(tt.input)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestExtractDiscordRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected time.Duration
	}{
		{
			"with retry_after",
			errors.New(`{"retry_after": 5, "message": "rate limited"}`),
			5 * time.Second,
		},
		{
			"without retry_after",
			errors.New("generic error"),
			0,
		},
		{
			"nil error",
			nil,
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractDiscordRetryAfter(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRegisterChannelRetryPolicy(t *testing.T) {
	customPolicy := &ChannelRetryPolicy{
		Name:        "custom",
		MaxAttempts: 5,
		MinDelay:    100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
	}

	RegisterChannelRetryPolicy("custom", customPolicy)

	policy := GetChannelRetryPolicy("custom")
	if policy.Name != "custom" {
		t.Errorf("expected custom policy, got %s", policy.Name)
	}
	if policy.MaxAttempts != 5 {
		t.Errorf("expected 5 attempts, got %d", policy.MaxAttempts)
	}
}

func TestRetryRunner_Run(t *testing.T) {
	runner := NewRetryRunner("discord", false)

	var attempts int32
	err := runner.Run(context.Background(), "test", func(ctx context.Context) error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return errors.New("rate limit exceeded")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryRunner_NonRetryableError(t *testing.T) {
	runner := NewRetryRunner("discord", false)

	var attempts int32
	err := runner.Run(context.Background(), "test", func(ctx context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("connection refused") // Not retryable for Discord (no rate limit/429)
	})

	if err == nil {
		t.Error("expected error")
	}
	// Should only attempt once since error is not retryable for Discord
	if attempts != 1 {
		t.Errorf("expected 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestRetryRunner_PermanentError(t *testing.T) {
	runner := NewRetryRunner("telegram", false)

	var attempts int32
	err := runner.Run(context.Background(), "test", func(ctx context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return AsPermanent(errors.New("timeout")) // Would be retryable but marked permanent
	})

	if err == nil {
		t.Error("expected error")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt for permanent error, got %d", attempts)
	}
}

func TestChannelRetryPolicyDefaults(t *testing.T) {
	policies := []struct {
		name   string
		policy *ChannelRetryPolicy
	}{
		{"discord", &DiscordRetryPolicy},
		{"telegram", &TelegramRetryPolicy},
		{"slack", &SlackRetryPolicy},
		{"email", &EmailRetryPolicy},
		{"default", &DefaultChannelRetryPolicy},
	}

	for _, p := range policies {
		t.Run(p.name, func(t *testing.T) {
			if p.policy.MaxAttempts < 1 {
				t.Error("MaxAttempts should be at least 1")
			}
			if p.policy.MinDelay <= 0 {
				t.Error("MinDelay should be positive")
			}
			if p.policy.MaxDelay < p.policy.MinDelay {
				t.Error("MaxDelay should be >= MinDelay")
			}
			if p.policy.JitterFraction < 0 || p.policy.JitterFraction > 1 {
				t.Error("JitterFraction should be between 0 and 1")
			}
		})
	}
}
