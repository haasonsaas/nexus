// Package gateway provides the main Nexus gateway server.
//
// singleton_lock.go implements a file-based lock to prevent multiple gateway
// instances from running simultaneously with the same configuration.
package gateway

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// DefaultLockTimeout is the maximum time to wait for the lock.
	DefaultLockTimeout = 5 * time.Second
	// DefaultPollInterval is how often to check if the lock is available.
	DefaultPollInterval = 100 * time.Millisecond
	// DefaultStaleTimeout is how long before a lock is considered stale.
	DefaultStaleTimeout = 30 * time.Second
)

// GatewayLockError is returned when the gateway lock cannot be acquired.
type GatewayLockError struct {
	Message string
	Cause   error
}

func (e *GatewayLockError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *GatewayLockError) Unwrap() error {
	return e.Cause
}

// GatewayLockHandle represents an acquired gateway lock.
type GatewayLockHandle struct {
	LockPath   string
	ConfigPath string
	file       *os.File
	released   bool
}

// Release releases the gateway lock.
func (h *GatewayLockHandle) Release() error {
	if h == nil || h.released {
		return nil
	}
	h.released = true

	if h.file != nil {
		h.file.Close()
	}
	return os.Remove(h.LockPath)
}

// GatewayLockOptions configures gateway lock acquisition.
type GatewayLockOptions struct {
	// StateDir is the directory where the lock file is created.
	StateDir string
	// ConfigPath is the path to the config file (used for lock file naming).
	ConfigPath string
	// Timeout is the maximum time to wait for the lock.
	Timeout time.Duration
	// PollInterval is how often to check if the lock is available.
	PollInterval time.Duration
	// StaleTimeout is how long before a lock is considered stale.
	StaleTimeout time.Duration
	// AllowMultiple disables singleton behavior (useful for tests).
	AllowMultiple bool
}

// lockPayload is the JSON structure stored in the lock file.
type lockPayload struct {
	PID        int    `json:"pid"`
	CreatedAt  string `json:"created_at"`
	ConfigPath string `json:"config_path"`
}

// AcquireGatewayLock attempts to acquire a gateway singleton lock.
// Returns nil handle if AllowMultiple is true or in test environments.
func AcquireGatewayLock(opts GatewayLockOptions) (*GatewayLockHandle, error) {
	// Skip locking in tests or when explicitly allowed
	if opts.AllowMultiple || os.Getenv("NEXUS_ALLOW_MULTI_GATEWAY") == "1" {
		return nil, nil
	}
	if os.Getenv("GO_TEST") != "" || os.Getenv("NEXUS_TEST") == "1" {
		return nil, nil
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultLockTimeout
	}

	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultPollInterval
	}

	staleTimeout := opts.StaleTimeout
	if staleTimeout == 0 {
		staleTimeout = DefaultStaleTimeout
	}

	// Resolve lock file path
	lockPath := resolveLockPath(opts.StateDir, opts.ConfigPath)

	// Ensure lock directory exists
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, &GatewayLockError{
			Message: fmt.Sprintf("failed to create lock directory: %s", filepath.Dir(lockPath)),
			Cause:   err,
		}
	}

	startTime := time.Now()
	var lastPayload *lockPayload

	for time.Since(startTime) < timeout {
		// Try to create the lock file exclusively
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			// Successfully created the lock file
			payload := lockPayload{
				PID:        os.Getpid(),
				CreatedAt:  time.Now().UTC().Format(time.RFC3339),
				ConfigPath: opts.ConfigPath,
			}

			data, err := json.Marshal(payload)
			if err != nil {
				_ = file.Close()
				return nil, &GatewayLockError{
					Message: "failed to serialize lock payload",
					Cause:   err,
				}
			}
			if _, err := file.Write(data); err != nil {
				_ = file.Close()
				return nil, &GatewayLockError{
					Message: "failed to write lock payload",
					Cause:   err,
				}
			}

			return &GatewayLockHandle{
				LockPath:   lockPath,
				ConfigPath: opts.ConfigPath,
				file:       file,
			}, nil
		}

		// Lock file already exists
		if !os.IsExist(err) {
			return nil, &GatewayLockError{
				Message: fmt.Sprintf("failed to acquire gateway lock at %s", lockPath),
				Cause:   err,
			}
		}

		// Check if the existing lock is stale
		lastPayload = readLockPayload(lockPath)
		if lastPayload != nil {
			// Check if the owning process is still alive
			if !isProcessAlive(lastPayload.PID) {
				// Process is dead, remove stale lock
				os.Remove(lockPath)
				continue
			}
		} else {
			// Can't read payload, check file age for staleness
			if isLockFileStale(lockPath, staleTimeout) {
				os.Remove(lockPath)
				continue
			}
		}

		time.Sleep(pollInterval)
	}

	// Timeout reached
	ownerInfo := ""
	if lastPayload != nil {
		ownerInfo = fmt.Sprintf(" (pid %d)", lastPayload.PID)
	}

	return nil, &GatewayLockError{
		Message: fmt.Sprintf("gateway already running%s; lock timeout after %v", ownerInfo, timeout),
	}
}

// resolveLockPath generates the lock file path based on config path hash.
func resolveLockPath(stateDir, configPath string) string {
	if stateDir == "" {
		stateDir = os.TempDir()
	}

	// Hash the config path to create a unique lock file per config
	hash := sha1.Sum([]byte(configPath))
	hashStr := hex.EncodeToString(hash[:])[:8]

	return filepath.Join(stateDir, fmt.Sprintf("gateway.%s.lock", hashStr))
}

// readLockPayload reads and parses the lock file payload.
func readLockPayload(lockPath string) *lockPayload {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil
	}

	var payload lockPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}

	if payload.PID <= 0 {
		return nil
	}

	return &payload
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// On Unix, we can check if a process exists by sending signal 0.
	// os.FindProcess always succeeds on Unix, so we need to actually check.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Try to send signal 0 - this doesn't actually send a signal but checks
	// if the process exists and we have permission to signal it.
	// On Unix, this returns nil if the process exists.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// isLockFileStale checks if the lock file is older than the stale timeout.
func isLockFileStale(lockPath string, staleTimeout time.Duration) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return true // Can't stat, consider it stale
	}

	return time.Since(info.ModTime()) > staleTimeout
}
