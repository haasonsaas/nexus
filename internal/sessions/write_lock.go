package sessions

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

var (
	// ErrLockTimeout is returned when acquiring a lock times out.
	ErrLockTimeout = errors.New("session: lock acquisition timeout")

	// ErrLockHeld is returned when a lock is already held by another goroutine.
	ErrLockHeld = errors.New("session: lock held by another writer")
)

// DefaultLockTimeout is the default timeout for lock acquisition (5 seconds).
const DefaultLockTimeout = 5 * time.Second

// lockPollInterval is how often we check if a lock has been released.
const lockPollInterval = 10 * time.Millisecond

// sessionMutex wraps a mutex for per-session locking.
type sessionMutex struct {
	mu     sync.Mutex
	locked bool
}

// SessionLocker provides per-session write locks using sync.Map.
// It ensures that only one goroutine can hold a lock for a given session at a time.
//
// Thread Safety:
// SessionLocker is safe for concurrent use from multiple goroutines.
type SessionLocker struct {
	locks   sync.Map // map[string]*sessionMutex
	timeout time.Duration
}

// NewSessionLocker creates a new SessionLocker with the specified default timeout.
// If timeout is <= 0, DefaultLockTimeout (5 seconds) is used.
func NewSessionLocker(timeout time.Duration) *SessionLocker {
	if timeout <= 0 {
		timeout = DefaultLockTimeout
	}
	return &SessionLocker{
		timeout: timeout,
	}
}

// getOrCreateMutex gets or creates a mutex for the given session ID.
func (s *SessionLocker) getOrCreateMutex(sessionID string) *sessionMutex {
	if m, ok := s.locks.Load(sessionID); ok {
		if mu, ok := m.(*sessionMutex); ok {
			return mu
		}
	}
	newMu := &sessionMutex{}
	actual, _ := s.locks.LoadOrStore(sessionID, newMu)
	if mu, ok := actual.(*sessionMutex); ok {
		return mu
	}
	return newMu
}

// Lock acquires a lock for the given session ID, blocking until the lock is available
// or the default timeout expires. Returns an error if the lock cannot be acquired.
func (s *SessionLocker) Lock(sessionID string) error {
	return s.LockWithTimeout(sessionID, s.timeout)
}

// LockWithTimeout acquires a lock for the given session ID with a custom timeout.
// Returns ErrLockTimeout if the lock cannot be acquired within the timeout.
func (s *SessionLocker) LockWithTimeout(sessionID string, timeout time.Duration) error {
	m := s.getOrCreateMutex(sessionID)
	deadline := time.Now().Add(timeout)

	for {
		m.mu.Lock()
		if !m.locked {
			m.locked = true
			m.mu.Unlock()
			return nil
		}
		m.mu.Unlock()

		if time.Now().After(deadline) {
			return ErrLockTimeout
		}

		// Poll with a small interval
		time.Sleep(lockPollInterval)
	}
}

// Unlock releases the lock for the given session ID.
// It is safe to call Unlock even if the lock is not held.
func (s *SessionLocker) Unlock(sessionID string) {
	if m, ok := s.locks.Load(sessionID); ok {
		mu, ok := m.(*sessionMutex)
		if !ok {
			return
		}
		mu.mu.Lock()
		mu.locked = false
		mu.mu.Unlock()
	}
}

// TryLock attempts to acquire a lock for the given session ID without blocking.
// Returns true if the lock was acquired, false otherwise.
func (s *SessionLocker) TryLock(sessionID string) bool {
	m := s.getOrCreateMutex(sessionID)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.locked {
		return false
	}

	m.locked = true
	return true
}

// IsLocked returns whether the given session ID is currently locked.
func (s *SessionLocker) IsLocked(sessionID string) bool {
	if m, ok := s.locks.Load(sessionID); ok {
		mu, ok := m.(*sessionMutex)
		if !ok {
			return false
		}
		mu.mu.Lock()
		defer mu.mu.Unlock()
		return mu.locked
	}
	return false
}

// LockWithContext acquires a lock for the given session ID, respecting context cancellation.
// Returns an error if the context is cancelled or the default timeout expires.
func (s *SessionLocker) LockWithContext(ctx context.Context, sessionID string) error {
	m := s.getOrCreateMutex(sessionID)
	deadline := time.Now().Add(s.timeout)

	for {
		// Check context first
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		m.mu.Lock()
		if !m.locked {
			m.locked = true
			m.mu.Unlock()
			return nil
		}
		m.mu.Unlock()

		if time.Now().After(deadline) {
			return ErrLockTimeout
		}

		// Poll with a small interval, checking context
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}
}

// SessionLock represents a lock for a specific session.
type SessionLock struct {
	sessionID string
	holder    string
	acquired  time.Time
	mu        sync.Mutex
	cond      *sync.Cond
	locked    bool
}

// SessionLockManager manages write locks for sessions.
// It ensures that only one writer can modify a session at a time,
// preventing race conditions and data corruption.
//
// Thread Safety:
// SessionLockManager is safe for concurrent use.
type SessionLockManager struct {
	locks      map[string]*SessionLock
	mu         sync.RWMutex
	defaultTTL time.Duration
}

// NewSessionLockManager creates a new session lock manager.
func NewSessionLockManager(defaultTTL time.Duration) *SessionLockManager {
	if defaultTTL <= 0 {
		defaultTTL = 30 * time.Second
	}

	mgr := &SessionLockManager{
		locks:      make(map[string]*SessionLock),
		defaultTTL: defaultTTL,
	}

	// Start background cleanup of expired locks
	go mgr.cleanupLoop()

	return mgr
}

// Acquire attempts to acquire a write lock for the session.
// If the lock is already held, it will wait up to timeout duration.
// Returns a release function that must be called when done.
func (m *SessionLockManager) Acquire(ctx context.Context, sessionID, holder string, timeout time.Duration) (func(), error) {
	if timeout <= 0 {
		timeout = m.defaultTTL
	}

	m.mu.Lock()
	lock, ok := m.locks[sessionID]
	if !ok {
		lock = &SessionLock{sessionID: sessionID}
		lock.cond = sync.NewCond(&lock.mu)
		m.locks[sessionID] = lock
	}
	m.mu.Unlock()

	// Try to acquire the lock
	lock.mu.Lock()
	defer lock.mu.Unlock()

	deadline := time.Now().Add(timeout)

	for lock.locked {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Check timeout
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ErrLockTimeout
		}

		// Wait for unlock with timeout
		done := make(chan struct{})
		go func() {
			lock.cond.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Lock was released, try again
		case <-time.After(remaining):
			return nil, ErrLockTimeout
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Acquired the lock
	lock.locked = true
	lock.holder = holder
	lock.acquired = time.Now()

	release := func() {
		lock.mu.Lock()
		defer lock.mu.Unlock()
		lock.locked = false
		lock.holder = ""
		lock.cond.Broadcast()
	}

	return release, nil
}

// TryAcquire attempts to acquire a write lock without waiting.
// Returns false if the lock is already held.
func (m *SessionLockManager) TryAcquire(sessionID, holder string) (func(), bool) {
	m.mu.Lock()
	lock, ok := m.locks[sessionID]
	if !ok {
		lock = &SessionLock{sessionID: sessionID}
		lock.cond = sync.NewCond(&lock.mu)
		m.locks[sessionID] = lock
	}
	m.mu.Unlock()

	lock.mu.Lock()
	defer lock.mu.Unlock()

	if lock.locked {
		return nil, false
	}

	lock.locked = true
	lock.holder = holder
	lock.acquired = time.Now()

	release := func() {
		lock.mu.Lock()
		defer lock.mu.Unlock()
		lock.locked = false
		lock.holder = ""
		lock.cond.Broadcast()
	}

	return release, true
}

// IsLocked returns whether the session is currently locked.
func (m *SessionLockManager) IsLocked(sessionID string) bool {
	m.mu.RLock()
	lock, ok := m.locks[sessionID]
	m.mu.RUnlock()

	if !ok {
		return false
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()
	return lock.locked
}

// GetLockInfo returns information about the current lock holder.
func (m *SessionLockManager) GetLockInfo(sessionID string) (holder string, since time.Time, locked bool) {
	m.mu.RLock()
	lock, ok := m.locks[sessionID]
	m.mu.RUnlock()

	if !ok {
		return "", time.Time{}, false
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()
	return lock.holder, lock.acquired, lock.locked
}

// cleanupLoop periodically removes stale lock entries.
func (m *SessionLockManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanup()
	}
}

// cleanup removes unlocked session entries that haven't been used recently.
func (m *SessionLockManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)

	for id, lock := range m.locks {
		lock.mu.Lock()
		if !lock.locked && lock.acquired.Before(cutoff) {
			delete(m.locks, id)
		}
		lock.mu.Unlock()
	}
}

// LockingStore wraps a Store with automatic write locking.
// All write operations acquire a lock before proceeding.
//
// Thread Safety:
// LockingStore is safe for concurrent use.
type LockingStore struct {
	Store
	locks  *SessionLockManager
	holder string
}

// NewLockingStore creates a new store wrapper with write locking.
// The holder string identifies this writer (e.g., "agent-worker-1").
func NewLockingStore(store Store, locks *SessionLockManager, holder string) *LockingStore {
	return &LockingStore{
		Store:  store,
		locks:  locks,
		holder: holder,
	}
}

// Create creates a session with a write lock.
func (s *LockingStore) Create(ctx context.Context, session *models.Session) error {
	release, err := s.locks.Acquire(ctx, session.ID, s.holder, 0)
	if err != nil {
		return err
	}
	defer release()

	return s.Store.Create(ctx, session)
}

// Update updates a session with a write lock.
func (s *LockingStore) Update(ctx context.Context, session *models.Session) error {
	release, err := s.locks.Acquire(ctx, session.ID, s.holder, 0)
	if err != nil {
		return err
	}
	defer release()

	return s.Store.Update(ctx, session)
}

// Delete deletes a session with a write lock.
func (s *LockingStore) Delete(ctx context.Context, id string) error {
	release, err := s.locks.Acquire(ctx, id, s.holder, 0)
	if err != nil {
		return err
	}
	defer release()

	return s.Store.Delete(ctx, id)
}

// AppendMessage appends a message with a write lock.
func (s *LockingStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	release, err := s.locks.Acquire(ctx, sessionID, s.holder, 0)
	if err != nil {
		return err
	}
	defer release()

	return s.Store.AppendMessage(ctx, sessionID, msg)
}

// WithLock executes a function while holding the write lock.
// Useful for compound operations that need atomic guarantees.
func (s *LockingStore) WithLock(ctx context.Context, sessionID string, fn func(Store) error) error {
	release, err := s.locks.Acquire(ctx, sessionID, s.holder, 0)
	if err != nil {
		return err
	}
	defer release()

	return fn(s.Store)
}
