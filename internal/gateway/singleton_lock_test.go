package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireGatewayLock_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	lock, err := AcquireGatewayLock(GatewayLockOptions{
		StateDir:   tmpDir,
		ConfigPath: configPath,
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

	// Release the lock
	if err := lock.Release(); err != nil {
		t.Errorf("failed to release lock: %v", err)
	}

	// Verify lock file is removed
	if _, err := os.Stat(lock.LockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after release")
	}
}

func TestAcquireGatewayLock_BlocksSecondInstance(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Manually create a lock file to simulate another process
	lockPath := resolveLockPath(tmpDir, configPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}

	// Write a lock file with current PID (so it looks alive)
	payload := `{"pid": ` + fmt.Sprintf("%d", os.Getpid()) + `, "created_at": "2024-01-01T00:00:00Z", "config_path": "test"}`
	if err := os.WriteFile(lockPath, []byte(payload), 0644); err != nil {
		t.Fatalf("failed to write lock: %v", err)
	}
	defer os.Remove(lockPath)

	// Clear test env vars temporarily to enable locking
	origGoTest := os.Getenv("GO_TEST")
	origNexusTest := os.Getenv("NEXUS_TEST")
	os.Unsetenv("GO_TEST")
	os.Unsetenv("NEXUS_TEST")
	defer func() {
		if origGoTest != "" {
			os.Setenv("GO_TEST", origGoTest)
		}
		if origNexusTest != "" {
			os.Setenv("NEXUS_TEST", origNexusTest)
		}
	}()

	// Try to acquire lock with short timeout - should fail because file exists with alive PID
	_, err := AcquireGatewayLock(GatewayLockOptions{
		StateDir:     tmpDir,
		ConfigPath:   configPath,
		Timeout:      200 * time.Millisecond,
		PollInterval: 50 * time.Millisecond,
	})

	if err == nil {
		t.Fatal("expected error when acquiring second lock")
	}

	lockErr, ok := err.(*GatewayLockError)
	if !ok {
		t.Fatalf("expected GatewayLockError, got %T", err)
	}
	if lockErr.Message == "" {
		t.Error("expected error message")
	}
}

func TestAcquireGatewayLock_AllowMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// With AllowMultiple, lock should return nil
	lock, err := AcquireGatewayLock(GatewayLockOptions{
		StateDir:      tmpDir,
		ConfigPath:    configPath,
		AllowMultiple: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lock != nil {
		t.Error("expected nil lock when AllowMultiple is true")
	}
}

func TestAcquireGatewayLock_DifferentConfigs(t *testing.T) {
	tmpDir := t.TempDir()

	// First config
	lock1, err := AcquireGatewayLock(GatewayLockOptions{
		StateDir:   tmpDir,
		ConfigPath: "/path/to/config1.yaml",
	})
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	if lock1 == nil {
		t.Fatal("first lock should be non-nil")
	}
	defer lock1.Release()

	// Second config (different path = different lock file)
	lock2, err := AcquireGatewayLock(GatewayLockOptions{
		StateDir:   tmpDir,
		ConfigPath: "/path/to/config2.yaml",
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

func TestAcquireGatewayLock_StaleLockRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Manually create a stale lock file with an invalid PID
	lockPath := resolveLockPath(tmpDir, configPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}

	// Write a lock file with PID 1 (init process, we can't signal it but it's "alive")
	// Use PID 999999999 which is unlikely to exist
	stalePayload := `{"pid": 999999999, "created_at": "2020-01-01T00:00:00Z", "config_path": "test"}`
	if err := os.WriteFile(lockPath, []byte(stalePayload), 0644); err != nil {
		t.Fatalf("failed to write stale lock: %v", err)
	}

	// Should acquire lock because the PID doesn't exist
	lock, err := AcquireGatewayLock(GatewayLockOptions{
		StateDir:   tmpDir,
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatalf("expected stale lock to be removed, got error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected lock to be non-nil")
	}
	defer lock.Release()
}

func TestGatewayLockHandle_ReleaseIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	lock, err := AcquireGatewayLock(GatewayLockOptions{
		StateDir:   tmpDir,
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Release multiple times should not error
	if err := lock.Release(); err != nil {
		t.Errorf("first release failed: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Errorf("second release failed: %v", err)
	}
}

func TestGatewayLockHandle_ReleaseNil(t *testing.T) {
	var lock *GatewayLockHandle
	// Should not panic
	if err := lock.Release(); err != nil {
		t.Errorf("nil release failed: %v", err)
	}
}

func TestResolveLockPath(t *testing.T) {
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
			path := resolveLockPath(tt.stateDir, tt.configPath)
			if path == "" {
				t.Error("expected non-empty lock path")
			}
			// Should contain "gateway" and ".lock"
			if filepath.Base(path)[:7] != "gateway" {
				t.Errorf("expected lock path to start with 'gateway', got: %s", filepath.Base(path))
			}
			if filepath.Ext(path) != ".lock" {
				t.Errorf("expected .lock extension, got: %s", filepath.Ext(path))
			}
		})
	}

	// Different configs should produce different lock files
	path1 := resolveLockPath("/tmp", "/config1.yaml")
	path2 := resolveLockPath("/tmp", "/config2.yaml")
	if path1 == path2 {
		t.Error("expected different lock paths for different configs")
	}
}

func TestIsProcessAlive(t *testing.T) {
	// Current process should be alive
	if !isProcessAlive(os.Getpid()) {
		t.Error("expected current process to be alive")
	}

	// Invalid PIDs should not be alive
	if isProcessAlive(0) {
		t.Error("expected PID 0 to not be alive")
	}
	if isProcessAlive(-1) {
		t.Error("expected PID -1 to not be alive")
	}

	// Non-existent PID should not be alive
	if isProcessAlive(999999999) {
		t.Error("expected non-existent PID to not be alive")
	}
}

func TestReadLockPayload(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid payload", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "valid.lock")
		payload := `{"pid": 12345, "created_at": "2024-01-01T00:00:00Z", "config_path": "/test/config.yaml"}`
		if err := os.WriteFile(lockPath, []byte(payload), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		p := readLockPayload(lockPath)
		if p == nil {
			t.Fatal("expected non-nil payload")
		}
		if p.PID != 12345 {
			t.Errorf("expected PID 12345, got %d", p.PID)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "invalid.lock")
		if err := os.WriteFile(lockPath, []byte("not json"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		p := readLockPayload(lockPath)
		if p != nil {
			t.Error("expected nil payload for invalid JSON")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		p := readLockPayload(filepath.Join(tmpDir, "nonexistent.lock"))
		if p != nil {
			t.Error("expected nil payload for missing file")
		}
	})

	t.Run("missing PID", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "nopid.lock")
		payload := `{"created_at": "2024-01-01T00:00:00Z", "config_path": "/test/config.yaml"}`
		if err := os.WriteFile(lockPath, []byte(payload), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		p := readLockPayload(lockPath)
		if p != nil {
			t.Error("expected nil payload when PID is missing")
		}
	})
}
