package sessions

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionLocker_Lock(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)

	// Test basic lock/unlock
	err := locker.Lock("session-1")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Should be locked now
	if !locker.IsLocked("session-1") {
		t.Error("expected session to be locked")
	}

	locker.Unlock("session-1")

	// Should be unlocked now
	if locker.IsLocked("session-1") {
		t.Error("expected session to be unlocked")
	}
}

func TestSessionLocker_TryLock(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)

	// First TryLock should succeed
	if !locker.TryLock("session-1") {
		t.Error("first TryLock should succeed")
	}

	// Second TryLock on same session should fail
	if locker.TryLock("session-1") {
		t.Error("second TryLock should fail")
	}

	// TryLock on different session should succeed
	if !locker.TryLock("session-2") {
		t.Error("TryLock on different session should succeed")
	}

	locker.Unlock("session-1")
	locker.Unlock("session-2")
}

func TestSessionLocker_LockWithTimeout(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)

	// Acquire lock
	if err := locker.Lock("session-1"); err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Try to acquire with short timeout - should fail
	err := locker.LockWithTimeout("session-1", 50*time.Millisecond)
	if err != ErrLockTimeout {
		t.Errorf("expected ErrLockTimeout, got: %v", err)
	}

	locker.Unlock("session-1")

	// Now lock should succeed
	if err := locker.LockWithTimeout("session-1", 50*time.Millisecond); err != nil {
		t.Errorf("expected lock to succeed after unlock, got: %v", err)
	}
	locker.Unlock("session-1")
}

func TestSessionLocker_LockWithContext(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)

	// Acquire lock
	if err := locker.Lock("session-1"); err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Try to acquire with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := locker.LockWithContext(ctx, "session-1")
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	locker.Unlock("session-1")
}

func TestSessionLocker_ConcurrentAccess(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)
	const numGoroutines = 10
	const sessionID = "session-concurrent"

	var counter int64
	var wg sync.WaitGroup

	// Launch multiple goroutines that increment a counter while holding the lock
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := locker.Lock(sessionID); err != nil {
				t.Errorf("failed to acquire lock: %v", err)
				return
			}
			defer locker.Unlock(sessionID)

			// Read, increment, write - this would race without proper locking
			val := atomic.LoadInt64(&counter)
			time.Sleep(1 * time.Millisecond) // Small delay to increase chance of race
			atomic.StoreInt64(&counter, val+1)
		}()
	}

	wg.Wait()

	if counter != numGoroutines {
		t.Errorf("expected counter to be %d, got %d", numGoroutines, counter)
	}
}

func TestSessionLocker_MultipleSessions(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)
	const numSessions = 5

	var wg sync.WaitGroup

	// Lock multiple sessions concurrently - all should succeed
	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(sessionNum int) {
			defer wg.Done()

			sessionID := "session-" + string(rune('A'+sessionNum))
			if err := locker.Lock(sessionID); err != nil {
				t.Errorf("failed to acquire lock for %s: %v", sessionID, err)
				return
			}

			// Hold lock briefly
			time.Sleep(10 * time.Millisecond)

			locker.Unlock(sessionID)
		}(i)
	}

	wg.Wait()
}

func TestSessionLocker_UnlockNonexistent(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)

	// Unlocking a session that was never locked should not panic
	locker.Unlock("nonexistent-session")
}

func TestSessionLocker_DefaultTimeout(t *testing.T) {
	// Test with zero timeout - should use default
	locker := NewSessionLocker(0)
	if locker.timeout != DefaultLockTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultLockTimeout, locker.timeout)
	}

	// Test with negative timeout - should use default
	locker = NewSessionLocker(-1 * time.Second)
	if locker.timeout != DefaultLockTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultLockTimeout, locker.timeout)
	}
}

func TestSessionLocker_IsLocked(t *testing.T) {
	locker := NewSessionLocker(DefaultLockTimeout)

	// Non-existent session should not be locked
	if locker.IsLocked("nonexistent") {
		t.Error("non-existent session should not be locked")
	}

	// Lock and check
	if err := locker.Lock("session-1"); err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	if !locker.IsLocked("session-1") {
		t.Error("locked session should report as locked")
	}

	// Unlock and check
	locker.Unlock("session-1")
	if locker.IsLocked("session-1") {
		t.Error("unlocked session should not report as locked")
	}
}
