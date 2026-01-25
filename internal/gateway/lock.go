// Package gateway provides the main Nexus gateway server.
//
// lock.go implements an enhanced file-based lock to prevent multiple gateway
// instances from running simultaneously with the same configuration.
// It includes Linux-specific process validation using /proc.
package gateway

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Default timeouts and intervals
const (
	DefaultLockTimeoutMs      = 5000
	DefaultLockPollIntervalMs = 100
	DefaultLockStaleMs        = 30000
)

// LockPayload contains lock file contents
type LockPayload struct {
	PID        int    `json:"pid"`
	CreatedAt  string `json:"createdAt"`
	ConfigPath string `json:"configPath"`
	StartTime  int64  `json:"startTime,omitempty"` // Linux process start time
}

// LockHandle represents an acquired lock
type LockHandle struct {
	LockPath   string
	ConfigPath string
	file       *os.File
}

// Release releases the lock
func (h *LockHandle) Release() error {
	if h.file != nil {
		h.file.Close()
		h.file = nil
	}
	return os.Remove(h.LockPath)
}

// LockOptions configures lock acquisition
type LockOptions struct {
	TimeoutMs      int
	PollIntervalMs int
	StaleMs        int
	AllowInTests   bool
	StateDir       string
	ConfigPath     string
}

// LockError represents a lock acquisition error
type LockError struct {
	Message string
	Cause   error
}

func (e *LockError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *LockError) Unwrap() error {
	return e.Cause
}

// LockOwnerStatus indicates the status of the lock owner
type LockOwnerStatus string

const (
	OwnerAlive   LockOwnerStatus = "alive"
	OwnerDead    LockOwnerStatus = "dead"
	OwnerUnknown LockOwnerStatus = "unknown"
)

// isLockProcessAlive checks if a process is running
func isLockProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 doesn't send a signal but checks if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// readLinuxStartTime reads process start time from /proc/<pid>/stat
func readLinuxStartTime(pid int) (int64, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, false
	}

	content := string(data)
	// Find closing paren of comm field
	closeParen := strings.LastIndex(content, ")")
	if closeParen < 0 {
		return 0, false
	}

	rest := strings.TrimSpace(content[closeParen+1:])
	fields := strings.Fields(rest)
	if len(fields) < 20 {
		return 0, false
	}

	startTime, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, false
	}
	return startTime, true
}

// readLinuxCmdline reads process command line from /proc/<pid>/cmdline
func readLinuxCmdline(pid int) []string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil
	}

	parts := strings.Split(string(data), "\x00")
	var result []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// isGatewayProcess checks if command line indicates a gateway process
func isGatewayProcess(args []string) bool {
	for _, arg := range args {
		normalized := strings.ToLower(strings.ReplaceAll(arg, "\\", "/"))
		if normalized == "gateway" || normalized == "serve" {
			return true
		}
	}
	return false
}

// resolveGatewayOwnerStatus determines if the lock owner is still running
func resolveGatewayOwnerStatus(pid int, payload *LockPayload) LockOwnerStatus {
	if !isLockProcessAlive(pid) {
		return OwnerDead
	}

	// On Linux, verify it's the same process using start time
	if runtime.GOOS == "linux" && payload != nil && payload.StartTime > 0 {
		currentStartTime, ok := readLinuxStartTime(pid)
		if !ok {
			return OwnerUnknown
		}
		if currentStartTime != payload.StartTime {
			return OwnerDead // PID was reused
		}
		return OwnerAlive
	}

	// Fallback: check cmdline on Linux
	if runtime.GOOS == "linux" {
		args := readLinuxCmdline(pid)
		if args == nil {
			return OwnerUnknown
		}
		if isGatewayProcess(args) {
			return OwnerAlive
		}
		return OwnerDead
	}

	return OwnerAlive
}

// readEnhancedLockPayload reads and parses the lock file
func readEnhancedLockPayload(lockPath string) (*LockPayload, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}

	var payload LockPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	if payload.PID == 0 || payload.CreatedAt == "" || payload.ConfigPath == "" {
		return nil, errors.New("invalid lock payload")
	}

	return &payload, nil
}

// ResolveLockPath generates the lock file path based on config path
func ResolveLockPath(stateDir, configPath string) string {
	if stateDir == "" {
		stateDir = os.TempDir()
	}
	hash := sha1.Sum([]byte(configPath))
	hashStr := hex.EncodeToString(hash[:])[:8]
	return filepath.Join(stateDir, fmt.Sprintf("gateway.%s.lock", hashStr))
}

// AcquireEnhancedGatewayLock acquires a lock preventing multiple gateway instances
// with enhanced Linux-specific process validation
func AcquireEnhancedGatewayLock(opts LockOptions) (*LockHandle, error) {
	// Check environment for skip flag
	if os.Getenv("NEXUS_ALLOW_MULTI_GATEWAY") == "1" {
		return nil, nil
	}

	// Check if running in tests
	if !opts.AllowInTests && (os.Getenv("GO_TEST") != "" || os.Getenv("NEXUS_ENV") == "test") {
		return nil, nil
	}

	timeoutMs := opts.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = DefaultLockTimeoutMs
	}

	pollIntervalMs := opts.PollIntervalMs
	if pollIntervalMs <= 0 {
		pollIntervalMs = DefaultLockPollIntervalMs
	}

	staleMs := opts.StaleMs
	if staleMs <= 0 {
		staleMs = DefaultLockStaleMs
	}

	lockPath := ResolveLockPath(opts.StateDir, opts.ConfigPath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, &LockError{Message: "failed to create lock directory", Cause: err}
	}

	startedAt := time.Now()
	var lastPayload *LockPayload

	for time.Since(startedAt) < time.Duration(timeoutMs)*time.Millisecond {
		// Try to create lock file exclusively
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			// Successfully created lock file
			payload := LockPayload{
				PID:        os.Getpid(),
				CreatedAt:  time.Now().UTC().Format(time.RFC3339),
				ConfigPath: opts.ConfigPath,
			}

			// On Linux, record start time for PID reuse detection
			if runtime.GOOS == "linux" {
				if startTime, ok := readLinuxStartTime(os.Getpid()); ok {
					payload.StartTime = startTime
				}
			}

			data, err := json.Marshal(payload)
			if err != nil {
				file.Close()
				os.Remove(lockPath)
				return nil, &LockError{Message: "failed to marshal lock payload", Cause: err}
			}
			if _, err := file.Write(data); err != nil {
				file.Close()
				os.Remove(lockPath)
				return nil, &LockError{Message: "failed to write lock payload", Cause: err}
			}

			return &LockHandle{
				LockPath:   lockPath,
				ConfigPath: opts.ConfigPath,
				file:       file,
			}, nil
		}

		if !os.IsExist(err) {
			return nil, &LockError{Message: "failed to acquire lock", Cause: err}
		}

		// Lock file exists, check if owner is still alive
		lastPayload, _ = readEnhancedLockPayload(lockPath)
		ownerPID := 0
		if lastPayload != nil {
			ownerPID = lastPayload.PID
		}

		ownerStatus := resolveGatewayOwnerStatus(ownerPID, lastPayload)

		if ownerStatus == OwnerDead {
			// Owner is dead, remove stale lock
			os.Remove(lockPath)
			continue
		}

		if ownerStatus != OwnerAlive {
			// Check if lock is stale by time
			isStale := false
			if lastPayload != nil && lastPayload.CreatedAt != "" {
				createdAt, err := time.Parse(time.RFC3339, lastPayload.CreatedAt)
				if err == nil {
					isStale = time.Since(createdAt) > time.Duration(staleMs)*time.Millisecond
				}
			}

			if !isStale {
				// Check mtime
				if info, err := os.Stat(lockPath); err == nil {
					isStale = time.Since(info.ModTime()) > time.Duration(staleMs)*time.Millisecond
				} else {
					isStale = true
				}
			}

			if isStale {
				os.Remove(lockPath)
				continue
			}
		}

		// Wait and retry
		time.Sleep(time.Duration(pollIntervalMs) * time.Millisecond)
	}

	owner := ""
	if lastPayload != nil && lastPayload.PID > 0 {
		owner = fmt.Sprintf(" (pid %d)", lastPayload.PID)
	}
	return nil, &LockError{
		Message: fmt.Sprintf("gateway already running%s; lock timeout after %dms", owner, timeoutMs),
	}
}
