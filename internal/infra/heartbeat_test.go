package infra

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeartbeatRunner_BasicExecution(t *testing.T) {
	var count int32

	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval:     50 * time.Millisecond,
		InitialDelay: 10 * time.Millisecond,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			atomic.AddInt32(&count, 1)
			return "ok", true
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner.Start(ctx)
	time.Sleep(180 * time.Millisecond) // Should run ~3 times
	runner.Stop()

	c := atomic.LoadInt32(&count)
	if c < 2 || c > 5 {
		t.Errorf("expected 2-5 heartbeats, got %d", c)
	}
}

func TestHeartbeatRunner_ManualExecution(t *testing.T) {
	var count int32

	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval: time.Hour, // Long interval so it won't run automatically
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			atomic.AddInt32(&count, 1)
			return "result", true
		},
	})

	result := runner.RunOnce(context.Background())

	if result.Status != HeartbeatStatusRan {
		t.Errorf("expected status 'ran', got %s", result.Status)
	}
	if result.Result != "result" {
		t.Errorf("expected result 'result', got %s", result.Result)
	}
	if atomic.LoadInt32(&count) != 1 {
		t.Error("expected heartbeat to run once")
	}
}

func TestHeartbeatRunner_SkipIfBusy(t *testing.T) {
	queue := NewCommandQueue()

	// Block the queue
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_, _ = queue.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
			close(started)
			<-done
			return nil, nil
		}, nil)
	}()
	<-started

	var skipped bool
	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval:   time.Hour,
		SkipIfBusy: true,
		Queue:      queue,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			return "ok", true
		},
		OnSkip: func(reason string) {
			if reason == "queue-busy" {
				skipped = true
			}
		},
	})

	result := runner.RunOnce(context.Background())
	close(done)

	if result.Status != HeartbeatStatusSkipped {
		t.Errorf("expected skipped status, got %s", result.Status)
	}
	if result.Reason != "queue-busy" {
		t.Errorf("expected reason 'queue-busy', got %s", result.Reason)
	}
	if !skipped {
		t.Error("expected OnSkip to be called")
	}
}

func TestHeartbeatRunner_DuplicateSuppression(t *testing.T) {
	var count int32

	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval:     time.Hour,
		DedupeWindow: time.Hour, // Long window for test
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			atomic.AddInt32(&count, 1)
			return "same-result", true
		},
	})

	// First run should succeed
	result1 := runner.RunOnce(context.Background())
	if result1.Status != HeartbeatStatusRan {
		t.Errorf("first run: expected 'ran', got %s", result1.Status)
	}

	// Second run with same result should be marked as duplicate
	result2 := runner.RunOnce(context.Background())
	if result2.Status != HeartbeatStatusDuplicate {
		t.Errorf("second run: expected 'duplicate', got %s", result2.Status)
	}

	// OnHeartbeat should still be called both times
	if atomic.LoadInt32(&count) != 2 {
		t.Errorf("expected 2 heartbeat calls, got %d", count)
	}
}

func TestHeartbeatRunner_DifferentResultsNotDuplicate(t *testing.T) {
	callCount := 0

	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval:     time.Hour,
		DedupeWindow: time.Hour,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			callCount++
			if callCount == 1 {
				return "result-1", true
			}
			return "result-2", true
		},
	})

	result1 := runner.RunOnce(context.Background())
	result2 := runner.RunOnce(context.Background())

	if result1.Status != HeartbeatStatusRan {
		t.Errorf("first run: expected 'ran', got %s", result1.Status)
	}
	if result2.Status != HeartbeatStatusRan {
		t.Errorf("second run: expected 'ran' (different result), got %s", result2.Status)
	}
}

func TestHeartbeatRunner_HandlerSkip(t *testing.T) {
	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval: time.Hour,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			return "", false // Handler indicates skip
		},
	})

	result := runner.RunOnce(context.Background())

	if result.Status != HeartbeatStatusSkipped {
		t.Errorf("expected 'skipped', got %s", result.Status)
	}
	if result.Reason != "handler-skip" {
		t.Errorf("expected reason 'handler-skip', got %s", result.Reason)
	}
}

func TestHeartbeatRunner_NoHandler(t *testing.T) {
	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval: time.Hour,
		// No OnHeartbeat handler
	})

	result := runner.RunOnce(context.Background())

	if result.Status != HeartbeatStatusSkipped {
		t.Errorf("expected 'skipped', got %s", result.Status)
	}
	if result.Reason != "no-handler" {
		t.Errorf("expected reason 'no-handler', got %s", result.Reason)
	}
}

func TestHeartbeatRunner_StartStop(t *testing.T) {
	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval: time.Hour,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			return "ok", true
		},
	})

	if runner.IsRunning() {
		t.Error("expected not running initially")
	}

	ctx := context.Background()
	runner.Start(ctx)

	if !runner.IsRunning() {
		t.Error("expected running after Start")
	}

	// Starting again should be a no-op
	runner.Start(ctx)
	if !runner.IsRunning() {
		t.Error("expected still running after second Start")
	}

	runner.Stop()

	if runner.IsRunning() {
		t.Error("expected not running after Stop")
	}

	// Stopping again should be a no-op
	runner.Stop()
	if runner.IsRunning() {
		t.Error("expected still not running after second Stop")
	}
}

func TestHeartbeatRunner_ContextCancellation(t *testing.T) {
	var count int32

	runner := NewHeartbeatRunner(HeartbeatConfig{
		Interval:     20 * time.Millisecond,
		InitialDelay: 5 * time.Millisecond,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			atomic.AddInt32(&count, 1)
			return "ok", true
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	runner.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel() // Cancel context

	time.Sleep(50 * time.Millisecond)
	countBefore := atomic.LoadInt32(&count)

	time.Sleep(50 * time.Millisecond)
	countAfter := atomic.LoadInt32(&count)

	// Count should not increase after context cancellation
	if countAfter != countBefore {
		t.Errorf("heartbeat continued after context cancel: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestMultiHeartbeatRunner_BasicUsage(t *testing.T) {
	multi := NewMultiHeartbeatRunner()

	var count1, count2 int32

	multi.Add("fast", HeartbeatConfig{
		Interval:     30 * time.Millisecond,
		InitialDelay: 5 * time.Millisecond,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			atomic.AddInt32(&count1, 1)
			return "fast", true
		},
	})

	multi.Add("slow", HeartbeatConfig{
		Interval:     60 * time.Millisecond,
		InitialDelay: 5 * time.Millisecond,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			atomic.AddInt32(&count2, 1)
			return "slow", true
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	multi.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	multi.Stop()

	c1 := atomic.LoadInt32(&count1)
	c2 := atomic.LoadInt32(&count2)

	// Fast should run more times than slow
	if c1 <= c2 {
		t.Errorf("expected fast (%d) > slow (%d)", c1, c2)
	}
	if c1 < 2 {
		t.Errorf("expected fast to run at least 2 times, got %d", c1)
	}
}

func TestMultiHeartbeatRunner_AddRemove(t *testing.T) {
	multi := NewMultiHeartbeatRunner()

	multi.Add("test1", HeartbeatConfig{
		Interval: time.Hour,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			return "ok", true
		},
	})

	names := multi.Names()
	if len(names) != 1 {
		t.Errorf("expected 1 name, got %d", len(names))
	}

	multi.Add("test2", HeartbeatConfig{
		Interval: time.Hour,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			return "ok", true
		},
	})

	names = multi.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}

	multi.Remove("test1")

	names = multi.Names()
	if len(names) != 1 {
		t.Errorf("expected 1 name after remove, got %d", len(names))
	}
}

func TestMultiHeartbeatRunner_RunOnce(t *testing.T) {
	multi := NewMultiHeartbeatRunner()

	multi.Add("test", HeartbeatConfig{
		Interval: time.Hour,
		OnHeartbeat: func(ctx context.Context) (string, bool) {
			return "result", true
		},
	})

	result, ok := multi.RunOnce(context.Background(), "test")
	if !ok {
		t.Error("expected to find runner")
	}
	if result.Status != HeartbeatStatusRan {
		t.Errorf("expected 'ran', got %s", result.Status)
	}

	_, ok = multi.RunOnce(context.Background(), "nonexistent")
	if ok {
		t.Error("expected not to find nonexistent runner")
	}
}
