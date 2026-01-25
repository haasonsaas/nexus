package sessions

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
)

// Locker provides a process-safe session lock interface.
type Locker interface {
	Lock(ctx context.Context, sessionID string) error
	Unlock(sessionID string)
}

// LocalLocker wraps the in-memory SessionLocker with a context-aware interface.
type LocalLocker struct {
	inner *SessionLocker
}

// NewLocalLocker creates a LocalLocker using the default timeout.
func NewLocalLocker(timeout time.Duration) *LocalLocker {
	return &LocalLocker{inner: NewSessionLocker(timeout)}
}

// Lock acquires a local lock using the provided context.
func (l *LocalLocker) Lock(ctx context.Context, sessionID string) error {
	if l == nil || l.inner == nil {
		return errors.New("session locker unavailable")
	}
	return l.inner.LockWithContext(ctx, sessionID)
}

// Unlock releases the local lock.
func (l *LocalLocker) Unlock(sessionID string) {
	if l == nil || l.inner == nil {
		return
	}
	l.inner.Unlock(sessionID)
}

// DBLockerConfig configures the DB-backed session lock.
type DBLockerConfig struct {
	OwnerID         string
	TTL             time.Duration
	RefreshInterval time.Duration
	AcquireTimeout  time.Duration
	PollInterval    time.Duration
}

// DefaultDBLockerConfig returns default settings for DBLocker.
func DefaultDBLockerConfig() DBLockerConfig {
	return DBLockerConfig{
		TTL:             2 * time.Minute,
		RefreshInterval: 30 * time.Second,
		AcquireTimeout:  10 * time.Second,
		PollInterval:    200 * time.Millisecond,
	}
}

// DBLocker implements a DB-backed lease lock for sessions.
type DBLocker struct {
	db     *sql.DB
	config DBLockerConfig

	mu     sync.Mutex
	renew  map[string]context.CancelFunc
	closed bool
}

// NewDBLocker creates a new DB-backed session locker.
func NewDBLocker(db *sql.DB, cfg DBLockerConfig) (*DBLocker, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}
	if cfg.OwnerID == "" {
		return nil, errors.New("owner id is required")
	}
	defaults := DefaultDBLockerConfig()
	if cfg.TTL <= 0 {
		cfg.TTL = defaults.TTL
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = defaults.RefreshInterval
	}
	if cfg.AcquireTimeout <= 0 {
		cfg.AcquireTimeout = defaults.AcquireTimeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaults.PollInterval
	}

	return &DBLocker{
		db:     db,
		config: cfg,
		renew:  make(map[string]context.CancelFunc),
	}, nil
}

// Lock attempts to acquire a DB-backed lock with lease renewal.
func (l *DBLocker) Lock(ctx context.Context, sessionID string) error {
	if l == nil {
		return errors.New("session locker unavailable")
	}
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("session_id is required")
	}

	deadline := time.Now().Add(l.config.AcquireTimeout)
	for {
		ok, err := l.tryAcquire(ctx, sessionID)
		if err != nil {
			return err
		}
		if ok {
			l.startRenew(sessionID)
			return nil
		}

		if time.Now().After(deadline) {
			return ErrLockTimeout
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(l.config.PollInterval):
		}
	}
}

// Unlock releases a DB-backed lock.
func (l *DBLocker) Unlock(sessionID string) {
	if l == nil {
		return
	}
	l.stopRenew(sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := l.db.ExecContext(ctx, `
		DELETE FROM session_locks
		WHERE session_id = $1 AND owner_id = $2
	`, sessionID, l.config.OwnerID); err != nil {
		// Best-effort unlock; if this fails, the lock will expire via TTL.
		_ = err
	}
}

// Close stops all renew loops.
func (l *DBLocker) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	for _, cancel := range l.renew {
		cancel()
	}
	l.renew = make(map[string]context.CancelFunc)
	l.mu.Unlock()
	return nil
}

func (l *DBLocker) tryAcquire(ctx context.Context, sessionID string) (bool, error) {
	now := time.Now()
	expiresAt := now.Add(l.config.TTL)
	var owner string
	err := l.db.QueryRowContext(ctx, `
		INSERT INTO session_locks (session_id, owner_id, acquired_at, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (session_id) DO UPDATE
		SET owner_id = EXCLUDED.owner_id,
			acquired_at = EXCLUDED.acquired_at,
			expires_at = EXCLUDED.expires_at
		WHERE session_locks.expires_at < $3 OR session_locks.owner_id = EXCLUDED.owner_id
		RETURNING owner_id
	`, sessionID, l.config.OwnerID, now, expiresAt).Scan(&owner)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return owner == l.config.OwnerID, nil
}

func (l *DBLocker) startRenew(sessionID string) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	if _, ok := l.renew[sessionID]; ok {
		l.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.renew[sessionID] = cancel
	l.mu.Unlock()

	go l.renewLoop(ctx, sessionID)
}

func (l *DBLocker) stopRenew(sessionID string) {
	l.mu.Lock()
	cancel, ok := l.renew[sessionID]
	if ok {
		delete(l.renew, sessionID)
	}
	l.mu.Unlock()
	if ok {
		cancel()
	}
}

func (l *DBLocker) renewLoop(ctx context.Context, sessionID string) {
	ticker := time.NewTicker(l.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !l.extendLease(ctx, sessionID) {
				l.stopRenew(sessionID)
				return
			}
		}
	}
}

func (l *DBLocker) extendLease(ctx context.Context, sessionID string) bool {
	expiresAt := time.Now().Add(l.config.TTL)
	result, err := l.db.ExecContext(ctx, `
		UPDATE session_locks
		SET expires_at = $1
		WHERE session_id = $2 AND owner_id = $3
	`, expiresAt, sessionID, l.config.OwnerID)
	if err != nil {
		return false
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false
	}
	return rows > 0
}
