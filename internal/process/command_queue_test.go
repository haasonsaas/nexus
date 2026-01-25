package process

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewCommandQueue(t *testing.T) {
	cq := NewCommandQueue()
	if cq == nil {
		t.Fatal("expected non-nil CommandQueue")
	}
	if cq.lanes == nil {
		t.Fatal("expected lanes map to be initialized")
	}
}

func TestCommandLane_Constants(t *testing.T) {
	tests := []struct {
		lane     CommandLane
		expected string
	}{
		{LaneMain, "main"},
		{LaneCron, "cron"},
		{LaneSubagent, "subagent"},
		{LaneNested, "nested"},
	}

	for _, tt := range tests {
		if string(tt.lane) != tt.expected {
			t.Errorf("lane %v: expected %q, got %q", tt.lane, tt.expected, string(tt.lane))
		}
	}
}

func TestEnqueue_BasicExecution(t *testing.T) {
	cq := NewCommandQueue()

	result, err := Enqueue(cq, func(ctx context.Context) (int, error) {
		return 42, nil
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestEnqueue_ReturnsError(t *testing.T) {
	cq := NewCommandQueue()

	_, err := Enqueue(cq, func(ctx context.Context) (int, error) {
		return 0, context.DeadlineExceeded
	}, nil)

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded error, got %v", err)
	}
}

func TestEnqueueInLane_DifferentLanes(t *testing.T) {
	cq := NewCommandQueue()

	// Enqueue in different lanes
	lanes := []CommandLane{LaneMain, LaneCron, LaneSubagent, LaneNested}
	var wg sync.WaitGroup

	for _, lane := range lanes {
		wg.Add(1)
		go func(l CommandLane) {
			defer wg.Done()
			result, err := EnqueueInLane(cq, l, func(ctx context.Context) (string, error) {
				return string(l), nil
			}, nil)
			if err != nil {
				t.Errorf("lane %s: unexpected error: %v", l, err)
			}
			if result != string(l) {
				t.Errorf("lane %s: expected %q, got %q", l, string(l), result)
			}
		}(lane)
	}

	wg.Wait()
}

func TestLaneIsolation_TasksInDifferentLanesDontBlock(t *testing.T) {
	cq := NewCommandQueue()

	// Set main lane to concurrency 1
	cq.SetLaneConcurrency(LaneMain, 1)
	cq.SetLaneConcurrency(LaneCron, 1)

	mainStarted := make(chan struct{})
	mainCanFinish := make(chan struct{})
	cronFinished := make(chan struct{})

	// Start a blocking task in main lane
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			close(mainStarted)
			<-mainCanFinish
			return 1, nil
		}, nil)
	}()

	// Wait for main task to start
	<-mainStarted

	// Start a task in cron lane - should not be blocked by main
	go func() {
		_, _ = EnqueueInLane(cq, LaneCron, func(ctx context.Context) (int, error) {
			return 2, nil
		}, nil)
		close(cronFinished)
	}()

	// Cron task should complete even though main is blocked
	select {
	case <-cronFinished:
		// Success - cron wasn't blocked
	case <-time.After(500 * time.Millisecond):
		t.Error("cron task blocked by main task - lane isolation failed")
	}

	// Cleanup
	close(mainCanFinish)
}

func TestConcurrencyLimit_Respected(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 2)

	var activeCount int32
	var maxObserved int32
	var mu sync.Mutex

	taskCount := 10
	var wg sync.WaitGroup

	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				current := atomic.AddInt32(&activeCount, 1)

				mu.Lock()
				if current > maxObserved {
					maxObserved = current
				}
				mu.Unlock()

				time.Sleep(50 * time.Millisecond)
				atomic.AddInt32(&activeCount, -1)
				return 0, nil
			}, nil)
		}()
	}

	wg.Wait()

	if maxObserved > 2 {
		t.Errorf("concurrency limit exceeded: max observed %d, expected <= 2", maxObserved)
	}
}

func TestConcurrencyLimit_SetToOne(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	var activeCount int32
	var maxObserved int32
	var mu sync.Mutex

	taskCount := 5
	var wg sync.WaitGroup

	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				current := atomic.AddInt32(&activeCount, 1)

				mu.Lock()
				if current > maxObserved {
					maxObserved = current
				}
				mu.Unlock()

				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&activeCount, -1)
				return 0, nil
			}, nil)
		}()
	}

	wg.Wait()

	if maxObserved > 1 {
		t.Errorf("concurrency limit exceeded: max observed %d, expected <= 1", maxObserved)
	}
}

func TestWaitTimeWarning_Callback(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	blockingStarted := make(chan struct{})
	blockingCanFinish := make(chan struct{})
	warningCalled := make(chan struct{})

	// Start blocking task
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			close(blockingStarted)
			<-blockingCanFinish
			return 1, nil
		}, nil)
	}()

	<-blockingStarted

	// Start second task with short warn threshold
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			return 2, nil
		}, &EnqueueOptions{
			WarnAfterMs: 50, // Very short threshold
			OnWait: func(waitMs int, queuedAhead int) {
				close(warningCalled)
			},
		})
	}()

	// Wait enough time for warning to trigger
	time.Sleep(100 * time.Millisecond)
	close(blockingCanFinish)

	// Check if warning was called
	select {
	case <-warningCalled:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Error("OnWait callback was not called")
	}
}

func TestFIFO_OrderingWithinLane(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	var executionOrder []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	taskCount := 5
	allEnqueued := make(chan struct{})

	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Small delay to ensure ordering of enqueue
			time.Sleep(time.Duration(idx) * 10 * time.Millisecond)

			_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				// Wait for all tasks to be enqueued before starting execution
				<-allEnqueued
				mu.Lock()
				executionOrder = append(executionOrder, idx)
				mu.Unlock()
				return idx, nil
			}, nil)
		}(i)
	}

	// Give time for all tasks to enqueue
	time.Sleep(100 * time.Millisecond)
	close(allEnqueued)

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if len(executionOrder) != taskCount {
		t.Fatalf("expected %d tasks executed, got %d", taskCount, len(executionOrder))
	}

	// Verify FIFO order
	for i := 0; i < taskCount; i++ {
		if executionOrder[i] != i {
			t.Errorf("FIFO order violated: position %d has task %d, expected %d", i, executionOrder[i], i)
		}
	}
}

func TestConcurrentAccess_Safety(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 5)
	cq.SetLaneConcurrency(LaneCron, 3)

	var wg sync.WaitGroup
	goroutines := 50

	// Concurrent enqueues to multiple lanes
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			lane := LaneMain
			if idx%2 == 0 {
				lane = LaneCron
			}
			_, _ = EnqueueInLane(cq, lane, func(ctx context.Context) (int, error) {
				time.Sleep(5 * time.Millisecond)
				return idx, nil
			}, nil)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = cq.GetQueueSize(LaneMain)
				_ = cq.GetQueueSize(LaneCron)
				_ = cq.GetTotalQueueSize()
				_ = cq.GetLaneStats(LaneMain)
				_ = cq.GetAllLaneStats()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	// Wait for all goroutines
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no race conditions
	case <-time.After(10 * time.Second):
		t.Error("test timed out - possible deadlock")
	}
}

func TestGetQueueSize(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	// Initially empty
	if size := cq.GetQueueSize(LaneMain); size != 0 {
		t.Errorf("expected initial size 0, got %d", size)
	}

	blockingStarted := make(chan struct{})
	blockingCanFinish := make(chan struct{})

	// Start blocking task
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			close(blockingStarted)
			<-blockingCanFinish
			return 1, nil
		}, nil)
	}()

	<-blockingStarted

	// Add more tasks
	for i := 0; i < 3; i++ {
		go func() {
			_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				return 0, nil
			}, nil)
		}()
	}

	// Give time for tasks to enqueue
	time.Sleep(50 * time.Millisecond)

	size := cq.GetQueueSize(LaneMain)
	if size != 4 { // 1 active + 3 queued
		t.Errorf("expected size 4 (1 active + 3 queued), got %d", size)
	}

	close(blockingCanFinish)
}

func TestGetTotalQueueSize(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)
	cq.SetLaneConcurrency(LaneCron, 1)

	mainBlocking := make(chan struct{})
	cronBlocking := make(chan struct{})
	mainStarted := make(chan struct{})
	cronStarted := make(chan struct{})

	// Start blocking tasks in both lanes
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			close(mainStarted)
			<-mainBlocking
			return 1, nil
		}, nil)
	}()

	go func() {
		_, _ = EnqueueInLane(cq, LaneCron, func(ctx context.Context) (int, error) {
			close(cronStarted)
			<-cronBlocking
			return 1, nil
		}, nil)
	}()

	<-mainStarted
	<-cronStarted

	// Add queued tasks
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			return 0, nil
		}, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	total := cq.GetTotalQueueSize()
	if total != 3 { // 2 active + 1 queued
		t.Errorf("expected total size 3, got %d", total)
	}

	close(mainBlocking)
	close(cronBlocking)
}

func TestClearLane(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	blockingStarted := make(chan struct{})
	blockingCanFinish := make(chan struct{})

	// Start blocking task
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			close(blockingStarted)
			<-blockingCanFinish
			return 1, nil
		}, nil)
	}()

	<-blockingStarted

	// Add more tasks
	errChan := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_, err := EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				return 0, nil
			}, nil)
			errChan <- err
		}()
	}

	// Give time for tasks to enqueue
	time.Sleep(50 * time.Millisecond)

	// Clear the lane
	removed := cq.ClearLane(LaneMain)
	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}

	// Active task should still be counted
	if size := cq.GetQueueSize(LaneMain); size != 1 {
		t.Errorf("expected size 1 (active task), got %d", size)
	}

	close(blockingCanFinish)

	// Cleared tasks should receive errors
	for i := 0; i < 3; i++ {
		select {
		case err := <-errChan:
			if err != context.Canceled {
				t.Errorf("expected context.Canceled, got %v", err)
			}
		case <-time.After(time.Second):
			t.Error("timed out waiting for error")
		}
	}
}

func TestSetLaneConcurrency_Clamps(t *testing.T) {
	cq := NewCommandQueue()

	// Set to 0 should clamp to 1
	cq.SetLaneConcurrency(LaneMain, 0)
	stats := cq.GetLaneStats(LaneMain)
	if stats.MaxConcurrent != 1 {
		t.Errorf("expected maxConcurrent 1, got %d", stats.MaxConcurrent)
	}

	// Set to negative should clamp to 1
	cq.SetLaneConcurrency(LaneMain, -5)
	stats = cq.GetLaneStats(LaneMain)
	if stats.MaxConcurrent != 1 {
		t.Errorf("expected maxConcurrent 1, got %d", stats.MaxConcurrent)
	}
}

func TestEmptyLane_DefaultsToMain(t *testing.T) {
	cq := NewCommandQueue()

	result, err := EnqueueInLane(cq, "", func(ctx context.Context) (string, error) {
		return "test", nil
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "test" {
		t.Errorf("expected 'test', got %q", result)
	}

	// Should be in main lane
	stats := cq.GetLaneStats(LaneMain)
	if stats.Lane != LaneMain {
		t.Errorf("expected lane to be main")
	}
}

func TestGetLaneStats(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 3)

	stats := cq.GetLaneStats(LaneMain)
	if stats.Lane != LaneMain {
		t.Errorf("expected lane main, got %v", stats.Lane)
	}
	if stats.MaxConcurrent != 3 {
		t.Errorf("expected maxConcurrent 3, got %d", stats.MaxConcurrent)
	}
	if stats.Pending != 0 {
		t.Errorf("expected pending 0, got %d", stats.Pending)
	}
	if stats.Active != 0 {
		t.Errorf("expected active 0, got %d", stats.Active)
	}
}

func TestGetAllLaneStats(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)
	cq.SetLaneConcurrency(LaneCron, 2)
	cq.SetLaneConcurrency(LaneSubagent, 3)

	// Trigger lane creation by enqueueing tasks
	var wg sync.WaitGroup
	for _, lane := range []CommandLane{LaneMain, LaneCron, LaneSubagent} {
		wg.Add(1)
		go func(l CommandLane) {
			defer wg.Done()
			_, _ = EnqueueInLane(cq, l, func(ctx context.Context) (int, error) {
				return 0, nil
			}, nil)
		}(lane)
	}
	wg.Wait()

	stats := cq.GetAllLaneStats()
	if len(stats) != 3 {
		t.Errorf("expected 3 lanes, got %d", len(stats))
	}
}

func TestGetActiveTasks(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 2)

	started := make(chan struct{}, 2)
	canFinish := make(chan struct{})

	// Start 2 blocking tasks
	for i := 0; i < 2; i++ {
		go func() {
			_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				started <- struct{}{}
				<-canFinish
				return 0, nil
			}, nil)
		}()
	}

	// Wait for both to start
	<-started
	<-started

	active := cq.GetActiveTasks(LaneMain)
	if active != 2 {
		t.Errorf("expected 2 active tasks, got %d", active)
	}

	close(canFinish)
}

func TestGetPendingTasks(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	started := make(chan struct{})
	canFinish := make(chan struct{})

	// Start blocking task
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			close(started)
			<-canFinish
			return 0, nil
		}, nil)
	}()

	<-started

	// Add pending tasks
	for i := 0; i < 3; i++ {
		go func() {
			_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				return 0, nil
			}, nil)
		}()
	}

	time.Sleep(50 * time.Millisecond)

	pending := cq.GetPendingTasks(LaneMain)
	if pending != 3 {
		t.Errorf("expected 3 pending tasks, got %d", pending)
	}

	close(canFinish)
}

func TestContextCancellation(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	blockingStarted := make(chan struct{})
	blockingCanFinish := make(chan struct{})

	// Start blocking task
	go func() {
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			close(blockingStarted)
			<-blockingCanFinish
			return 1, nil
		}, nil)
	}()

	<-blockingStarted

	// Start task with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)
	go func() {
		_, err := EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			return 0, nil
		}, &EnqueueOptions{Context: ctx})
		errChan <- err
	}()

	// Give time to enqueue
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	select {
	case err := <-errChan:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("expected context cancellation to return error")
	}

	close(blockingCanFinish)
}

func TestHighConcurrency_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 10)
	cq.SetLaneConcurrency(LaneCron, 5)
	cq.SetLaneConcurrency(LaneSubagent, 3)

	var completed int32
	var wg sync.WaitGroup
	taskCount := 100

	lanes := []CommandLane{LaneMain, LaneCron, LaneSubagent}

	for i := 0; i < taskCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			lane := lanes[idx%len(lanes)]
			result, err := EnqueueInLane(cq, lane, func(ctx context.Context) (int, error) {
				time.Sleep(time.Duration(idx%10) * time.Millisecond)
				return idx, nil
			}, nil)
			if err != nil {
				t.Errorf("task %d: unexpected error: %v", idx, err)
				return
			}
			if result != idx {
				t.Errorf("task %d: expected result %d, got %d", idx, idx, result)
				return
			}
			atomic.AddInt32(&completed, 1)
		}(i)
	}

	wg.Wait()

	if completed != int32(taskCount) {
		t.Errorf("expected %d completed tasks, got %d", taskCount, completed)
	}

	// All queues should be empty
	if total := cq.GetTotalQueueSize(); total != 0 {
		t.Errorf("expected total queue size 0, got %d", total)
	}
}

func TestNilResult(t *testing.T) {
	cq := NewCommandQueue()

	result, err := EnqueueInLane(cq, LaneMain, func(ctx context.Context) (*string, error) {
		return nil, nil
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestEnqueue_StringResult(t *testing.T) {
	cq := NewCommandQueue()

	result, err := Enqueue(cq, func(ctx context.Context) (string, error) {
		return "hello world", nil
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestEnqueue_StructResult(t *testing.T) {
	type Response struct {
		ID   int
		Name string
	}

	cq := NewCommandQueue()

	result, err := Enqueue(cq, func(ctx context.Context) (Response, error) {
		return Response{ID: 123, Name: "test"}, nil
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 123 || result.Name != "test" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestDefaultWarnAfterMs(t *testing.T) {
	if DefaultWarnAfterMs != 2000 {
		t.Errorf("expected DefaultWarnAfterMs to be 2000, got %d", DefaultWarnAfterMs)
	}
}

func TestIncreaseConcurrency_DrainsTasks(t *testing.T) {
	cq := NewCommandQueue()
	cq.SetLaneConcurrency(LaneMain, 1)

	var wg sync.WaitGroup
	var activeCount int32
	started := make(chan struct{})
	canFinish := make(chan struct{})

	// Start blocking task
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
			atomic.AddInt32(&activeCount, 1)
			close(started)
			<-canFinish
			atomic.AddInt32(&activeCount, -1)
			return 0, nil
		}, nil)
	}()

	<-started

	// Add more tasks
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = EnqueueInLane(cq, LaneMain, func(ctx context.Context) (int, error) {
				atomic.AddInt32(&activeCount, 1)
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&activeCount, -1)
				return 0, nil
			}, nil)
		}()
	}

	time.Sleep(50 * time.Millisecond)

	// Only 1 should be active
	if active := atomic.LoadInt32(&activeCount); active != 1 {
		t.Errorf("expected 1 active task, got %d", active)
	}

	// Increase concurrency
	cq.SetLaneConcurrency(LaneMain, 4)
	close(canFinish)

	wg.Wait()
}
