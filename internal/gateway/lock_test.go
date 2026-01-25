package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAcquireEnhancedGatewayLock_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	lock, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   configPath,
		AllowInTests: true,
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if lock == nil {
		t.Fatal("expected lock to be non-nil")
	}

	// Verify lock file exists
	if _, err := os.Stat(lock.LockPath); os.IsNotExist(err) {
		t.Error("expected lock file to exist")
	}

	// Verify lock payload
	payload, err := readEnhancedLockPayload(lock.LockPath)
	if err != nil {
		t.Fatalf("failed to read lock payload: %v", err)
	}
	if payload.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), payload.PID)
	}
	if payload.ConfigPath != configPath {
		t.Errorf("expected config path %s, got %s", configPath, payload.ConfigPath)
	}
	if runtime.GOOS == "linux" && payload.StartTime == 0 {
		t.Error("expected StartTime to be set on Linux")
	}

	// Release the lock
	if err := lock.Release(); err != nil {
		t.Errorf("failed to release lock: %v", err)
	}

	// Verify lock file is removed
	if _, err := os.Stat(lock.LockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after release")
	}
}

func TestAcquireEnhancedGatewayLock_BlocksSecondInstance(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Manually create a lock file to simulate another process
	lockPath := ResolveLockPath(tmpDir, configPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}

	// Write a lock file with current PID (so it looks alive)
	payload := LockPayload{
		PID:        os.Getpid(),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		ConfigPath: configPath,
	}
	if runtime.GOOS == "linux" {
		if startTime, ok := readLinuxStartTime(os.Getpid()); ok {
			payload.StartTime = startTime
		}
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("failed to write lock: %v", err)
	}
	defer os.Remove(lockPath)

	// Try to acquire lock with short timeout - should fail because file exists with alive PID
	_, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:       tmpDir,
		ConfigPath:     configPath,
		TimeoutMs:      200,
		PollIntervalMs: 50,
		AllowInTests:   true,
	})

	if err == nil {
		t.Fatal("expected error when acquiring second lock")
	}

	lockErr, ok := err.(*LockError)
	if !ok {
		t.Fatalf("expected LockError, got %T", err)
	}
	if lockErr.Message == "" {
		t.Error("expected error message")
	}
}

func TestAcquireEnhancedGatewayLock_SkippedInTests(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Set GO_TEST env var to simulate test environment
	oldVal := os.Getenv("GO_TEST")
	os.Setenv("GO_TEST", "1")
	defer func() {
		if oldVal == "" {
			os.Unsetenv("GO_TEST")
		} else {
			os.Setenv("GO_TEST", oldVal)
		}
	}()

	// With AllowInTests=false (default), lock should return nil in test environment
	lock, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   configPath,
		AllowInTests: false,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lock != nil {
		lock.Release()
		t.Error("expected nil lock when AllowInTests is false in test env")
	}
}

func TestAcquireEnhancedGatewayLock_DifferentConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	// First config
	lock1, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   "/path/to/config1.yaml",
		AllowInTests: true,
	})
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	if lock1 == nil {
		t.Fatal("first lock should be non-nil")
	}
	defer lock1.Release()

	// Second config (different path = different lock file)
	lock2, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   "/path/to/config2.yaml",
		AllowInTests: true,
	})
	if err != nil {
		t.Fatalf("second lock failed: %v", err)
	}
	if lock2 == nil {
		t.Fatal("second lock should be non-nil")
	}
	defer lock2.Release()

	// Both locks should have different paths
	if lock1.LockPath == lock2.LockPath {
		t.Error("expected different lock paths for different configs")
	}
}

func TestAcquireEnhancedGatewayLock_StaleLockRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Manually create a stale lock file with an invalid PID
	lockPath := ResolveLockPath(tmpDir, configPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}

	// Write a lock file with PID that doesn't exist
	payload := LockPayload{
		PID:        999999999,
		CreatedAt:  "2020-01-01T00:00:00Z",
		ConfigPath: configPath,
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("failed to write stale lock: %v", err)
	}

	// Should acquire lock because the PID doesn't exist
	lock, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   configPath,
		AllowInTests: true,
	})
	if err != nil {
		t.Fatalf("expected stale lock to be removed, got error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected lock to be non-nil")
	}
	defer lock.Release()
}

func TestAcquireEnhancedGatewayLock_PIDReuseDetection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PID reuse detection only works on Linux")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a lock file with current PID but wrong start time
	lockPath := ResolveLockPath(tmpDir, configPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}

	// Get current process start time
	actualStartTime, ok := readLinuxStartTime(os.Getpid())
	if !ok {
		t.Skip("cannot read process start time")
	}

	// Write lock with wrong start time (simulating PID reuse)
	payload := LockPayload{
		PID:        os.Getpid(),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		ConfigPath: configPath,
		StartTime:  actualStartTime + 1000, // Wrong start time
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("failed to write lock: %v", err)
	}

	// Should acquire lock because start time doesn't match (PID was reused)
	lock, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   configPath,
		AllowInTests: true,
	})
	if err != nil {
		t.Fatalf("expected lock to be acquired (PID reused), got error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected lock to be non-nil")
	}
	defer lock.Release()
}

func TestLockHandle_Release(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	lock, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   configPath,
		AllowInTests: true,
	})
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	lockPath := lock.LockPath

	// Release should succeed
	if err := lock.Release(); err != nil {
		t.Errorf("first release failed: %v", err)
	}

	// File should be gone
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed")
	}
}

func TestResolveLockPath_Enhanced(t *testing.T) {
	tests := []struct {
		name       string
		stateDir   string
		configPath string
	}{
		{
			name:       "normal paths",
			stateDir:   "/tmp/nexus",
			configPath: "/etc/nexus/config.yaml",
		},
		{
			name:       "empty state dir uses temp",
			stateDir:   "",
			configPath: "/etc/config.yaml",
		},
		{
			name:       "different configs produce different locks",
			stateDir:   "/tmp",
			configPath: "/home/user/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := ResolveLockPath(tt.stateDir, tt.configPath)
			if path == "" {
				t.Error("expected non-empty lock path")
			}
			// Should contain "gateway" and ".lock"
			base := filepath.Base(path)
			if len(base) < 7 || base[:7] != "gateway" {
				t.Errorf("expected lock path to start with 'gateway', got: %s", base)
			}
			if filepath.Ext(path) != ".lock" {
				t.Errorf("expected .lock extension, got: %s", filepath.Ext(path))
			}
		})
	}

	// Different configs should produce different lock files
	path1 := ResolveLockPath("/tmp", "/config1.yaml")
	path2 := ResolveLockPath("/tmp", "/config2.yaml")
	if path1 == path2 {
		t.Error("expected different lock paths for different configs")
	}
}

func TestIsLockProcessAlive(t *testing.T) {
	// Current process should be alive
	if !isLockProcessAlive(os.Getpid()) {
		t.Error("expected current process to be alive")
	}

	// Invalid PIDs should not be alive
	if isLockProcessAlive(0) {
		t.Error("expected PID 0 to not be alive")
	}
	if isLockProcessAlive(-1) {
		t.Error("expected PID -1 to not be alive")
	}

	// Non-existent PID should not be alive
	if isLockProcessAlive(999999999) {
		t.Error("expected non-existent PID to not be alive")
	}
}

func TestReadLinuxStartTime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	startTime, ok := readLinuxStartTime(os.Getpid())
	if !ok {
		t.Fatal("expected to read start time for current process")
	}
	if startTime <= 0 {
		t.Errorf("expected positive start time, got %d", startTime)
	}

	// Non-existent PID should fail
	_, ok = readLinuxStartTime(999999999)
	if ok {
		t.Error("expected failure for non-existent PID")
	}
}

func TestReadLinuxCmdline(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	args := readLinuxCmdline(os.Getpid())
	if args == nil {
		t.Fatal("expected non-nil cmdline for current process")
	}
	if len(args) == 0 {
		t.Error("expected at least one cmdline argument")
	}

	// Non-existent PID should return nil
	args = readLinuxCmdline(999999999)
	if args != nil {
		t.Error("expected nil for non-existent PID")
	}
}

func TestIsGatewayProcess(t *testing.T) {
	tests := []struct {
		args     []string
		expected bool
	}{
		{[]string{"nexus", "gateway"}, true},
		{[]string{"/usr/bin/nexus", "serve"}, true},
		{[]string{"gateway"}, true},
		{[]string{"serve"}, true},
		{[]string{"GATEWAY"}, true}, // Case insensitive
		{[]string{"bash", "-c", "echo hello"}, false},
		{[]string{"python", "script.py"}, false},
		{[]string{}, false},
		{nil, false},
	}

	for _, tt := range tests {
		result := isGatewayProcess(tt.args)
		if result != tt.expected {
			t.Errorf("isGatewayProcess(%v) = %v, expected %v", tt.args, result, tt.expected)
		}
	}
}

func TestResolveGatewayOwnerStatus(t *testing.T) {
	// Current process should be alive
	payload := &LockPayload{
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if runtime.GOOS == "linux" {
		if st, ok := readLinuxStartTime(os.Getpid()); ok {
			payload.StartTime = st
		}
	}

	status := resolveGatewayOwnerStatus(os.Getpid(), payload)
	if status != OwnerAlive {
		t.Errorf("expected OwnerAlive for current process, got %s", status)
	}

	// Non-existent PID should be dead
	status = resolveGatewayOwnerStatus(999999999, nil)
	if status != OwnerDead {
		t.Errorf("expected OwnerDead for non-existent PID, got %s", status)
	}
}

func TestReadEnhancedLockPayload(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid payload", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "valid.lock")
		payload := LockPayload{
			PID:        12345,
			CreatedAt:  "2024-01-01T00:00:00Z",
			ConfigPath: "/test/config.yaml",
			StartTime:  1000,
		}
		data, _ := json.Marshal(payload)
		if err := os.WriteFile(lockPath, data, 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		p, err := readEnhancedLockPayload(lockPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.PID != 12345 {
			t.Errorf("expected PID 12345, got %d", p.PID)
		}
		if p.StartTime != 1000 {
			t.Errorf("expected StartTime 1000, got %d", p.StartTime)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "invalid.lock")
		if err := os.WriteFile(lockPath, []byte("not json"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := readEnhancedLockPayload(lockPath)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := readEnhancedLockPayload(filepath.Join(tmpDir, "nonexistent.lock"))
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "incomplete.lock")
		// Missing CreatedAt and ConfigPath
		if err := os.WriteFile(lockPath, []byte(`{"pid": 12345}`), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := readEnhancedLockPayload(lockPath)
		if err == nil {
			t.Error("expected error for incomplete payload")
		}
	})
}

func TestLockError(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := fmt.Errorf("underlying error")
		err := &LockError{Message: "lock failed", Cause: cause}

		if err.Error() != "lock failed: underlying error" {
			t.Errorf("unexpected error string: %s", err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("without cause", func(t *testing.T) {
		err := &LockError{Message: "lock failed"}

		if err.Error() != "lock failed" {
			t.Errorf("unexpected error string: %s", err.Error())
		}
		if err.Unwrap() != nil {
			t.Error("Unwrap should return nil when no cause")
		}
	})
}

func TestLockOwnerStatus(t *testing.T) {
	// Verify constants are defined correctly
	if OwnerAlive != "alive" {
		t.Error("OwnerAlive should be 'alive'")
	}
	if OwnerDead != "dead" {
		t.Error("OwnerDead should be 'dead'")
	}
	if OwnerUnknown != "unknown" {
		t.Error("OwnerUnknown should be 'unknown'")
	}
}

func TestAcquireEnhancedGatewayLock_EnvSkip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Set env to skip locking
	oldVal := os.Getenv("NEXUS_ALLOW_MULTI_GATEWAY")
	os.Setenv("NEXUS_ALLOW_MULTI_GATEWAY", "1")
	defer func() {
		if oldVal == "" {
			os.Unsetenv("NEXUS_ALLOW_MULTI_GATEWAY")
		} else {
			os.Setenv("NEXUS_ALLOW_MULTI_GATEWAY", oldVal)
		}
	}()

	lock, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:     tmpDir,
		ConfigPath:   configPath,
		AllowInTests: true, // Even with AllowInTests, env var should take precedence
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lock != nil {
		lock.Release()
		t.Error("expected nil lock when NEXUS_ALLOW_MULTI_GATEWAY=1")
	}
}

func TestAcquireEnhancedGatewayLock_TimeoutOnContendedLock(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a valid lock held by current process
	lockPath := ResolveLockPath(tmpDir, configPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}

	payload := LockPayload{
		PID:        os.Getpid(),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		ConfigPath: configPath,
	}
	if runtime.GOOS == "linux" {
		if st, ok := readLinuxStartTime(os.Getpid()); ok {
			payload.StartTime = st
		}
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("failed to write lock: %v", err)
	}
	defer os.Remove(lockPath)

	start := time.Now()
	_, err := AcquireEnhancedGatewayLock(LockOptions{
		StateDir:       tmpDir,
		ConfigPath:     configPath,
		TimeoutMs:      300,
		PollIntervalMs: 50,
		AllowInTests:   true,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Should have taken approximately the timeout duration
	if elapsed < 250*time.Millisecond {
		t.Errorf("expected to wait for timeout, only waited %v", elapsed)
	}

	// Error message should mention the PID
	if lockErr, ok := err.(*LockError); ok {
		if lockErr.Message == "" {
			t.Error("expected non-empty error message")
		}
	} else {
		t.Errorf("expected LockError, got %T", err)
	}
}
