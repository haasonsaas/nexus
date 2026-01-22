//go:build !linux

// Package firecracker provides a Firecracker microVM-based sandbox backend for secure code execution.
// This stub file is used on non-Linux platforms where Firecracker is not supported.
package firecracker

import (
	"context"
	"errors"
	"time"

	"github.com/haasonsaas/nexus/internal/tools/sandbox"
)

// ErrNotSupported is returned when firecracker operations are attempted on non-Linux platforms.
var ErrNotSupported = errors.New("firecracker is only supported on Linux")

// Backend implements the sandbox.RuntimeExecutor interface using Firecracker microVMs.
// On non-Linux platforms, all operations return ErrNotSupported.
type Backend struct{}

// BackendConfig contains configuration for the Firecracker backend.
type BackendConfig struct {
	KernelPath      string
	RootFSImages    map[string]string
	PoolConfig      *PoolConfig
	OverlayDir      string
	SnapshotDir     string
	DefaultVCPUs    int64
	DefaultMemMB    int64
	NetworkEnabled  bool
	MaxExecTime     time.Duration
	EnableSnapshots bool
}

// PoolConfig contains configuration for the VM pool.
type PoolConfig struct {
	InitialSize    int
	MaxSize        int
	MinIdle        int
	MaxIdleTime    time.Duration
	MaxExecCount   int
	MaxUptime      time.Duration
	WarmupInterval time.Duration
	DefaultVCPUs   int64
	DefaultMemMB   int64
	OverlayEnabled bool
	KernelPath     string
	RootFSImages   map[string]string
	NetworkEnabled bool
	OverlayDir     string
}

// PoolStats contains VM pool statistics.
type PoolStats struct {
	TotalVMs      int `json:"total_vms"`
	IdleVMs       int `json:"idle_vms"`
	BusyVMs       int `json:"busy_vms"`
	TotalExecs    int `json:"total_execs"`
	FailedExecs   int `json:"failed_execs"`
	VMsCreated    int `json:"vms_created"`
	VMsRecycled   int `json:"vms_recycled"`
	VMsTerminated int `json:"vms_terminated"`
}

// OverlayStats contains overlay manager statistics.
type OverlayStats struct {
	TotalOverlays  int `json:"total_overlays"`
	ActiveOverlays int `json:"active_overlays"`
	CachedOverlays int `json:"cached_overlays"`
}

// BackendStats contains backend statistics.
type BackendStats struct {
	Pool    PoolStats    `json:"pool"`
	Overlay OverlayStats `json:"overlay"`
}

// DefaultBackendConfig returns a BackendConfig with sensible defaults.
func DefaultBackendConfig() *BackendConfig {
	return &BackendConfig{
		KernelPath: "/var/lib/firecracker/vmlinux",
		RootFSImages: map[string]string{
			"python": "/var/lib/firecracker/rootfs-python.ext4",
			"nodejs": "/var/lib/firecracker/rootfs-nodejs.ext4",
			"go":     "/var/lib/firecracker/rootfs-go.ext4",
			"bash":   "/var/lib/firecracker/rootfs-bash.ext4",
		},
		PoolConfig: &PoolConfig{
			InitialSize:    3,
			MaxSize:        10,
			MinIdle:        2,
			MaxIdleTime:    5 * time.Minute,
			MaxExecCount:   100,
			MaxUptime:      30 * time.Minute,
			WarmupInterval: 30 * time.Second,
			DefaultVCPUs:   1,
			DefaultMemMB:   512,
			OverlayEnabled: true,
		},
		OverlayDir:      "/var/lib/firecracker/overlays",
		SnapshotDir:     "/var/lib/firecracker/snapshots",
		DefaultVCPUs:    1,
		DefaultMemMB:    512,
		NetworkEnabled:  false,
		MaxExecTime:     5 * time.Minute,
		EnableSnapshots: false,
	}
}

// DefaultPoolConfig returns a PoolConfig with sensible defaults.
func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		InitialSize:    3,
		MaxSize:        10,
		MinIdle:        2,
		MaxIdleTime:    5 * time.Minute,
		MaxExecCount:   100,
		MaxUptime:      30 * time.Minute,
		WarmupInterval: 30 * time.Second,
		DefaultVCPUs:   1,
		DefaultMemMB:   512,
		OverlayEnabled: true,
	}
}

// NewBackend creates a new Firecracker sandbox backend.
// On non-Linux platforms, this always returns ErrNotSupported.
func NewBackend(config *BackendConfig) (*Backend, error) {
	return nil, ErrNotSupported
}

// Start initializes the backend and starts the VM pool.
func (b *Backend) Start(ctx context.Context) error {
	return ErrNotSupported
}

// Run executes code in a Firecracker microVM.
func (b *Backend) Run(ctx context.Context, params *sandbox.ExecuteParams, workspace string) (*sandbox.ExecuteResult, error) {
	return nil, ErrNotSupported
}

// Language returns the configured language.
func (b *Backend) Language() string {
	return ""
}

// Close shuts down the backend and releases resources.
func (b *Backend) Close() error {
	return nil
}

// Stats returns backend statistics.
func (b *Backend) Stats() BackendStats {
	return BackendStats{}
}

// FirecrackerExecutor wraps Backend to implement RuntimeExecutor interface.
type FirecrackerExecutor struct {
	backend  *Backend
	language string
}

// NewFirecrackerExecutor creates a new Firecracker-based executor for a specific language.
func NewFirecrackerExecutor(backend *Backend, language string) *FirecrackerExecutor {
	return &FirecrackerExecutor{
		backend:  backend,
		language: language,
	}
}

// Execute runs code using the Firecracker backend.
func (e *FirecrackerExecutor) Execute(ctx context.Context, params *sandbox.ExecuteParams) (*sandbox.ExecuteResult, error) {
	return nil, ErrNotSupported
}
