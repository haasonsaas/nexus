package infra

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// ChannelRetryPolicy defines retry behavior for a specific channel type.
type ChannelRetryPolicy struct {
	// Name identifies this policy.
	Name string

	// MaxAttempts is the total number of attempts (1 = no retries).
	MaxAttempts int

	// MinDelay is the minimum delay between retries.
	MinDelay time.Duration

	// MaxDelay caps the delay between retries.
	MaxDelay time.Duration

	// JitterFraction adds randomness to delays (0.0-1.0).
	JitterFraction float64

	// ShouldRetry determines if an error should trigger a retry.
	// If nil, defaults to retrying all non-permanent errors.
	ShouldRetry func(err error) bool

	// RetryAfter extracts a server-specified retry delay from an error.
	// Returns 0 if no specific delay is specified.
	RetryAfter func(err error) time.Duration

	// OnRetry is called before each retry attempt for logging/observability.
	OnRetry func(info RetryInfo)
}

// RetryInfo provides context about a retry attempt.
type RetryInfo struct {
	Attempt     int
	MaxAttempts int
	Delay       time.Duration
	Error       error
	Label       string
}

// Discord retry defaults based on rate limiting patterns.
var DiscordRetryPolicy = ChannelRetryPolicy{
	Name:           "discord",
	MaxAttempts:    4, // 1 initial + 3 retries
	MinDelay:       500 * time.Millisecond,
	MaxDelay:       30 * time.Second,
	JitterFraction: 0.1,
	ShouldRetry:    IsDiscordRetryable,
	RetryAfter:     ExtractDiscordRetryAfter,
}

// Telegram retry defaults with pattern-based error detection.
var TelegramRetryPolicy = ChannelRetryPolicy{
	Name:           "telegram",
	MaxAttempts:    4,
	MinDelay:       400 * time.Millisecond,
	MaxDelay:       30 * time.Second,
	JitterFraction: 0.1,
	ShouldRetry:    IsTelegramRetryable,
	RetryAfter:     ExtractTelegramRetryAfter,
}

// Slack retry defaults.
var SlackRetryPolicy = ChannelRetryPolicy{
	Name:           "slack",
	MaxAttempts:    4,
	MinDelay:       1 * time.Second,
	MaxDelay:       60 * time.Second,
	JitterFraction: 0.1,
	ShouldRetry:    IsSlackRetryable,
	RetryAfter:     ExtractSlackRetryAfter,
}

// Email retry defaults with longer delays for transient SMTP errors.
var EmailRetryPolicy = ChannelRetryPolicy{
	Name:           "email",
	MaxAttempts:    3,
	MinDelay:       5 * time.Second,
	MaxDelay:       5 * time.Minute,
	JitterFraction: 0.2,
	ShouldRetry:    IsEmailRetryable,
}

// DefaultChannelRetryPolicy for unknown channels.
var DefaultChannelRetryPolicy = ChannelRetryPolicy{
	Name:           "default",
	MaxAttempts:    3,
	MinDelay:       1 * time.Second,
	MaxDelay:       30 * time.Second,
	JitterFraction: 0.1,
	ShouldRetry: func(err error) bool {
		return !IsPermanent(err)
	},
}

// channelPolicies maps channel names to their retry policies.
var channelPolicies = map[string]*ChannelRetryPolicy{
	"discord":  &DiscordRetryPolicy,
	"telegram": &TelegramRetryPolicy,
	"slack":    &SlackRetryPolicy,
	"email":    &EmailRetryPolicy,
}

// GetChannelRetryPolicy returns the retry policy for a channel.
func GetChannelRetryPolicy(channel string) *ChannelRetryPolicy {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if policy, ok := channelPolicies[channel]; ok {
		return policy
	}
	return &DefaultChannelRetryPolicy
}

// RegisterChannelRetryPolicy registers a custom retry policy for a channel.
func RegisterChannelRetryPolicy(channel string, policy *ChannelRetryPolicy) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	channelPolicies[channel] = policy
}

// Patterns for detecting retryable errors.
var (
	telegramRetryPattern = regexp.MustCompile(`(?i)429|timeout|connect|reset|closed|unavailable|temporarily`)
	slackRetryPattern    = regexp.MustCompile(`(?i)rate.?limit|429|timeout|unavailable|retry`)
	emailRetryPattern    = regexp.MustCompile(`(?i)timeout|connection|temporary|try.?again|unavailable|421|450|451|452`)
)

// IsDiscordRetryable checks if an error is retryable for Discord.
func IsDiscordRetryable(err error) bool {
	if err == nil {
		return false
	}
	if IsPermanent(err) {
		return false
	}

	msg := err.Error()
	// Discord rate limits typically include "rate limit" or HTTP 429
	return strings.Contains(strings.ToLower(msg), "rate limit") ||
		strings.Contains(msg, "429")
}

// IsTelegramRetryable checks if an error is retryable for Telegram.
func IsTelegramRetryable(err error) bool {
	if err == nil {
		return false
	}
	if IsPermanent(err) {
		return false
	}

	return telegramRetryPattern.MatchString(err.Error())
}

// IsSlackRetryable checks if an error is retryable for Slack.
func IsSlackRetryable(err error) bool {
	if err == nil {
		return false
	}
	if IsPermanent(err) {
		return false
	}

	return slackRetryPattern.MatchString(err.Error())
}

// IsEmailRetryable checks if an error is retryable for email.
func IsEmailRetryable(err error) bool {
	if err == nil {
		return false
	}
	if IsPermanent(err) {
		return false
	}

	return emailRetryPattern.MatchString(err.Error())
}

// ExtractDiscordRetryAfter extracts the retry-after duration from a Discord error.
func ExtractDiscordRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}

	// Look for common patterns in error messages
	// Discord rate limit errors often include retry_after in JSON
	msg := err.Error()

	// Try to find "retry_after": X pattern
	if idx := strings.Index(msg, "retry_after"); idx >= 0 {
		// Simple extraction - look for a number after retry_after
		sub := msg[idx:]
		if ms := parseRetryAfterFromString(sub); ms != 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}

	return 0
}

// ExtractTelegramRetryAfter extracts retry-after from Telegram errors.
func ExtractTelegramRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}

	msg := err.Error()

	// Telegram often includes retry_after in error parameters
	if idx := strings.Index(msg, "retry_after"); idx >= 0 {
		if ms := parseRetryAfterFromString(msg[idx:]); ms != 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}

	return 0
}

// ExtractSlackRetryAfter extracts retry-after from Slack errors.
func ExtractSlackRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}

	msg := err.Error()

	// Slack includes Retry-After header value in error messages
	if idx := strings.Index(strings.ToLower(msg), "retry-after"); idx >= 0 {
		if ms := parseRetryAfterFromString(msg[idx:]); ms != 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}

	return 0
}

// parseRetryAfterFromString tries to extract a retry delay from a string.
// Returns milliseconds or 0 if not found.
func parseRetryAfterFromString(s string) int64 {
	// Look for patterns like: retry_after: 5, retry_after":5, retry-after: 5
	var num int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			// Found start of number
			for j := i; j < len(s); j++ {
				d := s[j]
				if d >= '0' && d <= '9' {
					num = num*10 + int64(d-'0')
				} else if d == '.' {
					// Handle decimal - multiply by 1000 for ms
					// Skip decimal parsing, just use integer part
					break
				} else {
					break
				}
			}
			// Assume seconds, convert to ms
			if num > 0 && num < 1000 {
				return num * 1000
			}
			return num
		}
	}
	return 0
}

// RetryRunner wraps a function with channel-specific retry logic.
type RetryRunner struct {
	policy  *ChannelRetryPolicy
	verbose bool
}

// NewRetryRunner creates a new retry runner for a channel.
func NewRetryRunner(channel string, verbose bool) *RetryRunner {
	return &RetryRunner{
		policy:  GetChannelRetryPolicy(channel),
		verbose: verbose,
	}
}

// Run executes a function with the configured retry policy.
func (r *RetryRunner) Run(ctx context.Context, label string, fn func(context.Context) error) error {
	cfg := &RetryConfig{
		MaxAttempts:    r.policy.MaxAttempts - 1, // Convert to retry count
		InitialDelay:   r.policy.MinDelay,
		MaxDelay:       r.policy.MaxDelay,
		Strategy:       BackoffExponential,
		JitterFraction: r.policy.JitterFraction,
		RetryIf:        r.policy.ShouldRetry,
	}

	result := RetryVoid(ctx, cfg, fn)
	return result.LastError
}

// RunWithResult executes a function that returns a value with retry.
func (r *RetryRunner) RunWithResult(ctx context.Context, label string, fn func(context.Context) (any, error)) (any, error) {
	cfg := &RetryConfig{
		MaxAttempts:    r.policy.MaxAttempts - 1,
		InitialDelay:   r.policy.MinDelay,
		MaxDelay:       r.policy.MaxDelay,
		Strategy:       BackoffExponential,
		JitterFraction: r.policy.JitterFraction,
		RetryIf:        r.policy.ShouldRetry,
	}

	val, result := Retry(ctx, cfg, fn)
	return val, result.LastError
}
