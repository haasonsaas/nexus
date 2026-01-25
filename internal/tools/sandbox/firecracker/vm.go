//go:build linux

// Package firecracker provides a Firecracker microVM-based sandbox backend for secure code execution.
package firecracker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/google/uuid"
)

// VMState represents the current state of a microVM.
type VMState int

const (
	VMStateUnknown VMState = iota
	VMStateCreating
	VMStateRunning
	VMStatePaused
	VMStateStopped
	VMStateFailed
)

func (s VMState) String() string {
	switch s {
	case VMStateCreating:
		return "creating"
	case VMStateRunning:
		return "running"
	case VMStatePaused:
		return "paused"
	case VMStateStopped:
		return "stopped"
	case VMStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// VMConfig contains configuration for creating a microVM.
type VMConfig struct {
	// VMID is the unique identifier for this VM.
	VMID string

	// KernelPath is the path to the kernel image.
	KernelPath string

	// RootFSPath is the path to the root filesystem image.
	RootFSPath string

	// OverlayPath is the path for the copy-on-write overlay.
	OverlayPath string

	// VCPUs is the number of virtual CPUs.
	VCPUs int64

	// MemSizeMB is the memory size in megabytes.
	MemSizeMB int64

	// NetworkEnabled determines if network access is allowed.
	NetworkEnabled bool

	// VsockCID is the context ID for vsock communication.
	VsockCID uint32

	// SocketPath is the path to the Firecracker API socket.
	SocketPath string

	// LogPath is the path for VM logs.
	LogPath string

	// MetricsPath is the path for VM metrics.
	MetricsPath string

	// Language is the programming language this VM supports.
	Language string

	// BootArgs are additional kernel boot arguments.
	BootArgs string
}

// DefaultVMConfig returns a VMConfig with sensible defaults.
func DefaultVMConfig() *VMConfig {
	return &VMConfig{
		VMID:           uuid.New().String(),
		VCPUs:          1,
		MemSizeMB:      512,
		NetworkEnabled: false,
		VsockCID:       3, // CID 0, 1, 2 are reserved
		BootArgs:       "console=ttyS0 reboot=k panic=1 pci=off",
	}
}

// MicroVM represents a Firecracker microVM instance.
type MicroVM struct {
	config  *VMConfig
	machine *firecracker.Machine
	state   VMState
	mu      sync.RWMutex

	// vsock handles host-guest communication.
	vsock *VsockConnection

	// startedAt is when the VM started.
	startedAt time.Time

	// execCount tracks how many executions have been run.
	execCount int

	// lastUsed tracks the last time the VM executed work.
	lastUsed time.Time

	// workDir is the working directory for this VM.
	workDir string

	// cmd is the firecracker process.
	cmd *exec.Cmd

	// cleanupFuncs are called when the VM is stopped.
	cleanupFuncs []func() error
}

// NewMicroVM creates a new microVM with the given configuration.
func NewMicroVM(ctx context.Context, config *VMConfig) (*MicroVM, error) {
	if config == nil {
		config = DefaultVMConfig()
	}

	// Validate required fields
	if config.KernelPath == "" {
		return nil, fmt.Errorf("kernel path is required")
	}
	if config.RootFSPath == "" {
		return nil, fmt.Errorf("rootfs path is required")
	}

	// Generate VMID if not set
	if config.VMID == "" {
		config.VMID = uuid.New().String()
	}

	// Create work directory
	workDir := filepath.Join(os.TempDir(), "firecracker-vm", config.VMID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	// Set socket path if not specified
	if config.SocketPath == "" {
		config.SocketPath = filepath.Join(workDir, "api.sock")
	}

	// Set log path if not specified
	if config.LogPath == "" {
		config.LogPath = filepath.Join(workDir, "vm.log")
	}

	vm := &MicroVM{
		config:  config,
		state:   VMStateCreating,
		workDir: workDir,
		cleanupFuncs: []func() error{
			func() error { return os.RemoveAll(workDir) },
		},
	}

	return vm, nil
}

// Start boots the microVM.
func (vm *MicroVM) Start(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.state == VMStateRunning {
		return nil
	}

	// Create overlay filesystem if specified
	if vm.config.OverlayPath != "" {
		if err := vm.createOverlay(); err != nil {
			vm.state = VMStateFailed
			return fmt.Errorf("failed to create overlay: %w", err)
		}
	}

	// Build firecracker configuration
	fcConfig, err := vm.buildFirecrackerConfig()
	if err != nil {
		vm.state = VMStateFailed
		return fmt.Errorf("failed to build firecracker config: %w", err)
	}

	// Find firecracker binary
	firecrackerBin, err := exec.LookPath("firecracker")
	if err != nil {
		vm.state = VMStateFailed
		return fmt.Errorf("firecracker binary not found: %w", err)
	}

	// Create the machine
	cmd := firecracker.VMCommandBuilder{}.
		WithBin(firecrackerBin).
		WithSocketPath(vm.config.SocketPath).
		Build(ctx)

	vm.cmd = cmd

	machineOpts := []firecracker.Opt{
		firecracker.WithProcessRunner(cmd),
	}

	machine, err := firecracker.NewMachine(ctx, fcConfig, machineOpts...)
	if err != nil {
		vm.state = VMStateFailed
		return fmt.Errorf("failed to create machine: %w", err)
	}
	vm.machine = machine

	// Start the machine
	if err := machine.Start(ctx); err != nil {
		vm.state = VMStateFailed
		return fmt.Errorf("failed to start machine: %w", err)
	}

	vm.state = VMStateRunning
	vm.startedAt = time.Now()
	vm.lastUsed = vm.startedAt

	// Establish vsock connection
	vsock, err := NewVsockConnection(vm.config.SocketPath, vm.config.VsockCID, GuestAgentPort)
	if err != nil {
		// Log but don't fail - vsock might not be ready yet
		// We'll retry on first communication attempt
	} else {
		vm.vsock = vsock
	}

	return nil
}

// Stop shuts down the microVM.
func (vm *MicroVM) Stop(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.state == VMStateStopped {
		return nil
	}

	var errs []error

	// Close vsock connection
	if vm.vsock != nil {
		if err := vm.vsock.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close vsock: %w", err))
		}
		vm.vsock = nil
	}

	// Stop the machine
	if vm.machine != nil {
		if err := vm.machine.StopVMM(); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop VMM: %w", err))
		}
		vm.machine = nil
	}

	// Kill the firecracker process if still running
	if vm.cmd != nil && vm.cmd.Process != nil {
		if err := vm.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			// Try SIGKILL if SIGTERM fails
			if err := vm.cmd.Process.Kill(); err != nil {
				errs = append(errs, fmt.Errorf("kill firecracker process: %w", err))
			}
		}
	}

	// Run cleanup functions
	for _, cleanup := range vm.cleanupFuncs {
		if err := cleanup(); err != nil {
			errs = append(errs, err)
		}
	}

	vm.state = VMStateStopped

	if len(errs) > 0 {
		return fmt.Errorf("stop encountered errors: %v", errs)
	}
	return nil
}

// Pause pauses the microVM.
func (vm *MicroVM) Pause(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.state != VMStateRunning {
		return fmt.Errorf("VM is not running (state: %s)", vm.state)
	}

	if err := vm.machine.PauseVM(ctx); err != nil {
		return fmt.Errorf("failed to pause VM: %w", err)
	}

	vm.state = VMStatePaused
	return nil
}

// Resume resumes a paused microVM.
func (vm *MicroVM) Resume(ctx context.Context) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.state != VMStatePaused {
		return fmt.Errorf("VM is not paused (state: %s)", vm.state)
	}

	if err := vm.machine.ResumeVM(ctx); err != nil {
		return fmt.Errorf("failed to resume VM: %w", err)
	}

	vm.state = VMStateRunning
	return nil
}

// State returns the current VM state.
func (vm *MicroVM) State() VMState {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.state
}

// VMID returns the VM identifier.
func (vm *MicroVM) VMID() string {
	return vm.config.VMID
}

// Language returns the language this VM supports.
func (vm *MicroVM) Language() string {
	return vm.config.Language
}

// Uptime returns how long the VM has been running.
func (vm *MicroVM) Uptime() time.Duration {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	if vm.startedAt.IsZero() {
		return 0
	}
	return time.Since(vm.startedAt)
}

// ExecCount returns the number of executions performed.
func (vm *MicroVM) ExecCount() int {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.execCount
}

// IncrementExecCount increments the execution counter.
func (vm *MicroVM) IncrementExecCount() {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.execCount++
	vm.lastUsed = time.Now()
}

// LastUsed returns the last time the VM executed work.
func (vm *MicroVM) LastUsed() time.Time {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.lastUsed
}

// Vsock returns the vsock connection for this VM.
func (vm *MicroVM) Vsock() *VsockConnection {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.vsock
}

// WorkDir returns the VM's working directory.
func (vm *MicroVM) WorkDir() string {
	return vm.workDir
}

// buildFirecrackerConfig constructs the firecracker SDK configuration.
func (vm *MicroVM) buildFirecrackerConfig() (firecracker.Config, error) {
	// Determine rootfs path (overlay or original)
	rootfsPath := vm.config.RootFSPath
	if vm.config.OverlayPath != "" {
		rootfsPath = vm.config.OverlayPath
	}

	// Build boot source
	bootSource := models.BootSource{
		KernelImagePath: firecracker.String(vm.config.KernelPath),
		BootArgs:        vm.config.BootArgs,
	}

	// Build drives
	drives := []models.Drive{
		{
			DriveID:      firecracker.String("rootfs"),
			PathOnHost:   firecracker.String(rootfsPath),
			IsRootDevice: firecracker.Bool(true),
			IsReadOnly:   firecracker.Bool(false),
		},
	}

	// Build machine config
	machineConfig := models.MachineConfiguration{
		VcpuCount:  firecracker.Int64(vm.config.VCPUs),
		MemSizeMib: firecracker.Int64(vm.config.MemSizeMB),
		Smt:        firecracker.Bool(false),
	}

	// Build vsock device
	vsockDevices := []firecracker.VsockDevice{
		{
			Path: filepath.Join(vm.workDir, "vsock.sock"),
			CID:  vm.config.VsockCID,
		},
	}

	// Build network config if enabled
	var networkInterfaces firecracker.NetworkInterfaces
	if vm.config.NetworkEnabled {
		networkInterfaces = firecracker.NetworkInterfaces{
			firecracker.NetworkInterface{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					MacAddress:  "AA:FC:00:00:00:01",
					HostDevName: "tap0",
				},
			},
		}
	}

	config := firecracker.Config{
		SocketPath:        vm.config.SocketPath,
		LogPath:           vm.config.LogPath,
		LogLevel:          "Warning",
		MetricsPath:       vm.config.MetricsPath,
		KernelImagePath:   vm.config.KernelPath,
		KernelArgs:        vm.config.BootArgs,
		Drives:            drives,
		MachineCfg:        machineConfig,
		VsockDevices:      vsockDevices,
		NetworkInterfaces: networkInterfaces,
	}

	// Set boot source separately
	_ = bootSource

	return config, nil
}

// createOverlay creates a copy-on-write overlay filesystem.
func (vm *MicroVM) createOverlay() error {
	// Use device-mapper or a simple copy for overlay
	// For simplicity, we'll use a file copy approach here
	// In production, you'd want to use device-mapper snapshots

	src, err := os.Open(vm.config.RootFSPath)
	if err != nil {
		return fmt.Errorf("failed to open source rootfs: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(vm.config.OverlayPath)
	if err != nil {
		return fmt.Errorf("failed to create overlay file: %w", err)
	}
	defer dst.Close()

	// Use copy_file_range for efficient copy-on-write on supported filesystems
	srcInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	// Create a sparse file of the same size
	if err := dst.Truncate(srcInfo.Size()); err != nil {
		return fmt.Errorf("failed to truncate overlay: %w", err)
	}

	// Use reflink copy if available (btrfs, xfs with reflink)
	// Fall back to regular copy if not supported
	if err := vm.reflinkCopy(src, dst, srcInfo.Size()); err != nil {
		// Fall back to regular sparse copy
		if err := vm.sparseCopy(src, dst); err != nil {
			return fmt.Errorf("failed to copy rootfs: %w", err)
		}
	}

	vm.cleanupFuncs = append(vm.cleanupFuncs, func() error {
		return os.Remove(vm.config.OverlayPath)
	})

	return nil
}

// reflinkCopy attempts to create a reflink copy (copy-on-write).
func (vm *MicroVM) reflinkCopy(src, dst *os.File, size int64) error {
	// Use FICLONE ioctl for reflink copy
	// This creates a copy-on-write clone on supported filesystems
	const FICLONE = 0x40049409 // ioctl number for FICLONE

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, dst.Fd(), FICLONE, src.Fd())
	if errno != 0 {
		return fmt.Errorf("FICLONE failed: %w", errno)
	}
	return nil
}

// sparseCopy copies a file while preserving sparseness.
func (vm *MicroVM) sparseCopy(src, dst *os.File) error {
	buf := make([]byte, 1024*1024) // 1MB buffer
	zeros := make([]byte, len(buf))

	for {
		n, err := src.Read(buf)
		if n > 0 {
			// Check if block is all zeros
			isZero := true
			for i := 0; i < n; i++ {
				if buf[i] != 0 {
					isZero = false
					break
				}
			}

			if !isZero {
				// Write non-zero block
				if _, err := dst.Write(buf[:n]); err != nil {
					return err
				}
			} else {
				// Seek over zero block (sparse)
				if _, err := dst.Seek(int64(n), 1); err != nil {
					return err
				}
			}
		}

		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
	}

	// Avoid unused variable warning
	_ = zeros

	return nil
}

// VMInfo contains runtime information about a microVM.
type VMInfo struct {
	VMID      string        `json:"vmid"`
	State     string        `json:"state"`
	Language  string        `json:"language"`
	Uptime    time.Duration `json:"uptime"`
	LastUsed  time.Time     `json:"last_used"`
	ExecCount int           `json:"exec_count"`
	VCPUs     int64         `json:"vcpus"`
	MemSizeMB int64         `json:"mem_size_mb"`
}

// Info returns runtime information about the VM.
func (vm *MicroVM) Info() VMInfo {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	return VMInfo{
		VMID:      vm.config.VMID,
		State:     vm.state.String(),
		Language:  vm.config.Language,
		Uptime:    vm.Uptime(),
		LastUsed:  vm.lastUsed,
		ExecCount: vm.execCount,
		VCPUs:     vm.config.VCPUs,
		MemSizeMB: vm.config.MemSizeMB,
	}
}
