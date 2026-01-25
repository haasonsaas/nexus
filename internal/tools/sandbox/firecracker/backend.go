//go:build linux

package firecracker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/tools/sandbox"
)

// Backend implements the sandbox.RuntimeExecutor interface using Firecracker microVMs.
type Backend struct {
	pool            *VMPool
	overlayManager  *OverlayManager
	snapshotManager *SnapshotManager
	config          *BackendConfig
	language        string
	mu              sync.RWMutex
	closed          bool
}

// BackendConfig contains configuration for the Firecracker backend.
type BackendConfig struct {
	// KernelPath is the path to the Linux kernel image.
	KernelPath string

	// RootFSImages maps languages to their rootfs image paths.
	RootFSImages map[string]string

	// PoolConfig contains VM pool configuration.
	PoolConfig *PoolConfig

	// OverlayDir is the directory for overlay filesystems.
	OverlayDir string

	// SnapshotDir is the directory for VM snapshots.
	SnapshotDir string

	// DefaultVCPUs is the default number of vCPUs per VM.
	DefaultVCPUs int64

	// DefaultMemMB is the default memory in MB per VM.
	DefaultMemMB int64

	// NetworkEnabled determines if VMs have network access.
	NetworkEnabled bool

	// MaxExecTime is the maximum execution time.
	MaxExecTime time.Duration

	// EnableSnapshots enables snapshot-based fast boot.
	EnableSnapshots bool

	// SnapshotRefreshInterval controls snapshot refresh cadence.
	SnapshotRefreshInterval time.Duration

	// SnapshotMaxAge controls when snapshots are considered stale.
	SnapshotMaxAge time.Duration
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
		OverlayDir:              "/var/lib/firecracker/overlays",
		SnapshotDir:             "/var/lib/firecracker/snapshots",
		DefaultVCPUs:            1,
		DefaultMemMB:            512,
		NetworkEnabled:          false,
		MaxExecTime:             5 * time.Minute,
		EnableSnapshots:         false,
		SnapshotRefreshInterval: 30 * time.Minute,
		SnapshotMaxAge:          6 * time.Hour,
	}
}

// NewBackend creates a new Firecracker sandbox backend.
func NewBackend(config *BackendConfig) (*Backend, error) {
	if config == nil {
		config = DefaultBackendConfig()
	}

	// Validate configuration
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create overlay manager
	overlayManager, err := NewOverlayManager(config.OverlayDir, config.PoolConfig.MaxSize*2)
	if err != nil {
		return nil, fmt.Errorf("failed to create overlay manager: %w", err)
	}

	// Create snapshot manager if enabled
	var snapshotManager *SnapshotManager
	if config.EnableSnapshots {
		snapshotManager, err = NewSnapshotManager(config.SnapshotDir)
		if err != nil {
			overlayManager.Close()
			return nil, fmt.Errorf("failed to create snapshot manager: %w", err)
		}
	}

	// Create pool config
	poolConfig := config.PoolConfig
	if poolConfig == nil {
		poolConfig = DefaultPoolConfig()
	}
	poolConfig.KernelPath = config.KernelPath
	poolConfig.RootFSImages = config.RootFSImages
	poolConfig.DefaultVCPUs = config.DefaultVCPUs
	poolConfig.DefaultMemMB = config.DefaultMemMB
	poolConfig.NetworkEnabled = config.NetworkEnabled
	poolConfig.OverlayDir = config.OverlayDir
	poolConfig.SnapshotsEnabled = config.EnableSnapshots
	if config.SnapshotRefreshInterval > 0 {
		poolConfig.SnapshotRefreshInterval = config.SnapshotRefreshInterval
	}
	if config.SnapshotMaxAge > 0 {
		poolConfig.SnapshotMaxAge = config.SnapshotMaxAge
	}

	// Create VM pool
	pool, err := NewVMPool(poolConfig)
	if err != nil {
		overlayManager.Close()
		return nil, fmt.Errorf("failed to create VM pool: %w", err)
	}
	if snapshotManager != nil {
		pool.SetSnapshotManager(snapshotManager)
	}

	backend := &Backend{
		pool:            pool,
		overlayManager:  overlayManager,
		snapshotManager: snapshotManager,
		config:          config,
	}

	return backend, nil
}

// validateConfig validates the backend configuration.
func validateConfig(config *BackendConfig) error {
	// Check if firecracker binary exists
	if _, err := exec.LookPath("firecracker"); err != nil {
		return fmt.Errorf("firecracker binary not found: %w", err)
	}

	// Check kernel path
	if _, err := os.Stat(config.KernelPath); os.IsNotExist(err) {
		return fmt.Errorf("kernel not found at %s", config.KernelPath)
	}

	// Check at least one rootfs exists
	found := false
	for _, path := range config.RootFSImages {
		if _, err := os.Stat(path); err == nil {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no rootfs images found")
	}

	return nil
}

// Start initializes the backend and starts the VM pool.
func (b *Backend) Start(ctx context.Context) error {
	return b.pool.Start(ctx)
}

// Run executes code in a Firecracker microVM.
func (b *Backend) Run(ctx context.Context, params *sandbox.ExecuteParams, workspace string) (*sandbox.ExecuteResult, error) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return nil, fmt.Errorf("backend is closed")
	}
	b.mu.RUnlock()

	// Get a VM from the pool
	vm, err := b.pool.Get(ctx, params.Language)
	if err != nil {
		return nil, fmt.Errorf("failed to get VM: %w", err)
	}
	defer func() {
		vm.IncrementExecCount()
		b.pool.Put(vm)
		b.pool.IncrementExecCount()
	}()

	// Ensure vsock connection is established
	vsock := vm.Vsock()
	if vsock == nil {
		return nil, fmt.Errorf("VM has no vsock connection")
	}

	if err := vsock.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to guest: %w", err)
	}

	// Prepare files to sync
	files := make(map[string]string)
	files[getMainFilename(params.Language)] = params.Code
	for name, content := range params.Files {
		files[filepath.Base(name)] = content
	}

	// Sync files to guest
	if err := vsock.SyncFiles(ctx, files, "/workspace"); err != nil {
		return nil, fmt.Errorf("failed to sync files: %w", err)
	}

	// Execute the code
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
	defer cancel()

	response, err := vsock.Execute(execCtx, params.Code, params.Language, params.Stdin, params.Files, params.Timeout)
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return &sandbox.ExecuteResult{
				Error:   "Execution timeout",
				Timeout: true,
			}, nil
		}
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	result := &sandbox.ExecuteResult{
		Stdout:   response.Stdout,
		Stderr:   response.Stderr,
		ExitCode: response.ExitCode,
		Error:    response.Error,
		Timeout:  response.Timeout,
	}

	return result, nil
}

// Language returns the language this executor handles.
func (b *Backend) Language() string {
	return b.language
}

// Close shuts down the backend and releases resources.
func (b *Backend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	var errs []error

	if b.pool != nil {
		if err := b.pool.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close pool: %w", err))
		}
	}

	if b.overlayManager != nil {
		if err := b.overlayManager.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close overlay manager: %w", err))
		}
	}

	if b.snapshotManager != nil {
		if err := b.snapshotManager.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close snapshot manager: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// Stats returns backend statistics.
func (b *Backend) Stats() BackendStats {
	poolStats := b.pool.Stats()
	overlayStats := b.overlayManager.Stats()

	return BackendStats{
		Pool:    poolStats,
		Overlay: overlayStats,
	}
}

// BackendStats contains backend statistics.
type BackendStats struct {
	Pool    PoolStats    `json:"pool"`
	Overlay OverlayStats `json:"overlay"`
}

// getMainFilename returns the filename for the code based on language.
func getMainFilename(language string) string {
	switch language {
	case "python":
		return "main.py"
	case "nodejs":
		return "main.js"
	case "go":
		return "main.go"
	case "bash":
		return "main.sh"
	default:
		return "main.txt"
	}
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

// Run executes code in a Firecracker microVM.
func (fe *FirecrackerExecutor) Run(ctx context.Context, params *sandbox.ExecuteParams, workspace string) (*sandbox.ExecuteResult, error) {
	return fe.backend.Run(ctx, params, workspace)
}

// Language returns the language this executor handles.
func (fe *FirecrackerExecutor) Language() string {
	return fe.language
}

// Close cleans up resources.
func (fe *FirecrackerExecutor) Close() error {
	// Individual executors don't close the backend
	return nil
}

// IsAvailable checks if Firecracker is available on the system.
func IsAvailable() bool {
	_, err := exec.LookPath("firecracker")
	return err == nil
}

// CheckRequirements verifies all requirements for running Firecracker.
func CheckRequirements() error {
	// Check firecracker binary
	if _, err := exec.LookPath("firecracker"); err != nil {
		return fmt.Errorf("firecracker binary not found: %w", err)
	}

	// Check KVM access
	if _, err := os.Stat("/dev/kvm"); os.IsNotExist(err) {
		return fmt.Errorf("/dev/kvm not found - KVM is required")
	}

	// Check KVM permissions
	kvmFile, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("cannot access /dev/kvm: %w", err)
	}
	kvmFile.Close()

	return nil
}

// SetupEnvironment prepares the environment for Firecracker.
func SetupEnvironment(config *BackendConfig) error {
	// Create necessary directories
	dirs := []string{
		config.OverlayDir,
		config.SnapshotDir,
		filepath.Dir(config.KernelPath),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// BackendOption is a functional option for configuring the backend.
type BackendOption func(*BackendConfig)

// WithKernelPath sets the kernel path.
func WithKernelPath(path string) BackendOption {
	return func(c *BackendConfig) {
		c.KernelPath = path
	}
}

// WithRootFSImage adds a rootfs image for a language.
func WithRootFSImage(language, path string) BackendOption {
	return func(c *BackendConfig) {
		if c.RootFSImages == nil {
			c.RootFSImages = make(map[string]string)
		}
		c.RootFSImages[language] = path
	}
}

// WithPoolSize sets the pool size.
func WithPoolSize(initial, max int) BackendOption {
	return func(c *BackendConfig) {
		if c.PoolConfig == nil {
			c.PoolConfig = DefaultPoolConfig()
		}
		c.PoolConfig.InitialSize = initial
		c.PoolConfig.MaxSize = max
	}
}

// WithVCPUs sets the default vCPUs per VM.
func WithVCPUs(vcpus int64) BackendOption {
	return func(c *BackendConfig) {
		c.DefaultVCPUs = vcpus
	}
}

// WithMemory sets the default memory per VM.
func WithMemory(memMB int64) BackendOption {
	return func(c *BackendConfig) {
		c.DefaultMemMB = memMB
	}
}

// WithNetwork enables or disables network access.
func WithNetwork(enabled bool) BackendOption {
	return func(c *BackendConfig) {
		c.NetworkEnabled = enabled
	}
}

// WithSnapshots enables snapshot-based fast boot.
func WithSnapshots(enabled bool) BackendOption {
	return func(c *BackendConfig) {
		c.EnableSnapshots = enabled
	}
}

// NewBackendWithOptions creates a new backend with functional options.
func NewBackendWithOptions(opts ...BackendOption) (*Backend, error) {
	config := DefaultBackendConfig()
	for _, opt := range opts {
		opt(config)
	}
	return NewBackend(config)
}
