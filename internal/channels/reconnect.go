package channels

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/haasonsaas/nexus/internal/retry"
)

// ReconnectConfig controls reconnection behavior.
type ReconnectConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Factor       float64
	Jitter       bool
}

// DefaultReconnectConfig returns a baseline reconnection config.
func DefaultReconnectConfig() ReconnectConfig {
	return ReconnectConfig{
		MaxAttempts:  5,
		InitialDelay: 2 * time.Second,
		MaxDelay:     30 * time.Second,
		Factor:       2,
		Jitter:       true,
	}
}

// Reconnector runs an operation with automatic reconnection attempts.
type Reconnector struct {
	Config ReconnectConfig
	Logger *slog.Logger
	Health *BaseHealthAdapter
}

// Run executes the provided function until it succeeds, the context is canceled,
// or max attempts are reached. It returns the last error.
func (r *Reconnector) Run(ctx context.Context, run func(context.Context) error) error {
	if run == nil {
		return errors.New("reconnector: run func is nil")
	}
	cfg := r.Config
	if cfg.MaxAttempts == 0 {
		cfg = DefaultReconnectConfig()
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = DefaultReconnectConfig().InitialDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = DefaultReconnectConfig().MaxDelay
	}
	if cfg.Factor <= 0 {
		cfg.Factor = DefaultReconnectConfig().Factor
	}

	attempt := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := run(ctx); err == nil {
			return nil
		} else {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if retry.IsPermanent(err) {
				return err
			}
			attempt++
			if r.Health != nil {
				r.Health.RecordReconnectAttempt()
				r.Health.SetStatus(false, err.Error())
			}
			if r.Logger != nil {
				r.Logger.Warn("reconnect attempt failed", "attempt", attempt, "error", err)
			}
			if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
				return err
			}
			delay := retry.Backoff(attempt, cfg.InitialDelay, cfg.MaxDelay, cfg.Factor)
			if cfg.Jitter {
				delay = retry.BackoffWithJitter(attempt, cfg.InitialDelay, cfg.MaxDelay, cfg.Factor)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
}
