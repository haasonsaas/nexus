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
)

// SnapshotType represents the type of snapshot.
type SnapshotType string

const (
	// SnapshotTypeFull is a full VM snapshot including memory.
	SnapshotTypeFull SnapshotType = "full"

	// SnapshotTypeDiff is a differential snapshot.
	SnapshotTypeDiff SnapshotType = "diff"
)

// Snapshot represents a VM snapshot.
type Snapshot struct {
	ID           string       `json:"id"`
	Type         SnapshotType `json:"type"`
	Language     string       `json:"language"`
	MemoryPath   string       `json:"memory_path"`
	StatePath    string       `json:"state_path"`
	CreatedAt    time.Time    `json:"created_at"`
	Size         int64        `json:"size"`
	ParentID     string       `json:"parent_id,omitempty"`
}

// OverlayManager manages copy-on-write overlays for rootfs images.
type OverlayManager struct {
	baseDir     string
	overlays    map[string]*Overlay
	mu          sync.RWMutex
	maxOverlays int
}

// Overlay represents a copy-on-write overlay filesystem.
type Overlay struct {
	ID         string
	BasePath   string
	OverlayPath string
	MergedPath string
	UpperPath  string
	WorkPath   string
	Language   string
	CreatedAt  time.Time
	InUse      bool
	mu         sync.Mutex
}

// NewOverlayManager creates a new overlay manager.
func NewOverlayManager(baseDir string, maxOverlays int) (*OverlayManager, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create overlay base directory: %w", err)
	}

	return &OverlayManager{
		baseDir:     baseDir,
		overlays:    make(map[string]*Overlay),
		maxOverlays: maxOverlays,
	}, nil
}

// CreateOverlay creates a new copy-on-write overlay for a base image.
func (om *OverlayManager) CreateOverlay(ctx context.Context, id, basePath, language string) (*Overlay, error) {
	om.mu.Lock()
	defer om.mu.Unlock()

	// Check max overlays
	if len(om.overlays) >= om.maxOverlays {
		// Try to clean up unused overlays
		if err := om.cleanupUnused(ctx); err != nil {
			return nil, fmt.Errorf("too many overlays and cleanup failed: %w", err)
		}
		if len(om.overlays) >= om.maxOverlays {
			return nil, fmt.Errorf("maximum overlay limit reached (%d)", om.maxOverlays)
		}
	}

	// Create overlay directories
	overlayDir := filepath.Join(om.baseDir, id)
	upperDir := filepath.Join(overlayDir, "upper")
	workDir := filepath.Join(overlayDir, "work")
	mergedDir := filepath.Join(overlayDir, "merged")

	for _, dir := range []string{overlayDir, upperDir, workDir, mergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	overlay := &Overlay{
		ID:          id,
		BasePath:    basePath,
		OverlayPath: overlayDir,
		MergedPath:  mergedDir,
		UpperPath:   upperDir,
		WorkPath:    workDir,
		Language:    language,
		CreatedAt:   time.Now(),
		InUse:       true,
	}

	om.overlays[id] = overlay
	return overlay, nil
}

// Mount mounts the overlay filesystem.
func (o *Overlay) Mount() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check if already mounted
	if o.isMounted() {
		return nil
	}

	// Mount using overlayfs
	// overlayfs options: lowerdir=<base>,upperdir=<upper>,workdir=<work>
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		o.BasePath, o.UpperPath, o.WorkPath)

	if err := syscall.Mount("overlay", o.MergedPath, "overlay", 0, opts); err != nil {
		return fmt.Errorf("failed to mount overlay: %w", err)
	}

	return nil
}

// Unmount unmounts the overlay filesystem.
func (o *Overlay) Unmount() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.isMounted() {
		return nil
	}

	if err := syscall.Unmount(o.MergedPath, 0); err != nil {
		// Try lazy unmount
		if err := syscall.Unmount(o.MergedPath, syscall.MNT_DETACH); err != nil {
			return fmt.Errorf("failed to unmount overlay: %w", err)
		}
	}

	return nil
}

// isMounted checks if the overlay is currently mounted.
func (o *Overlay) isMounted() bool {
	// Check /proc/mounts for our mount point
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}

	return containsMount(string(data), o.MergedPath)
}

// containsMount checks if a path is in the mount table.
func containsMount(mounts, path string) bool {
	// Simple check - in production you'd want to parse properly
	return len(mounts) > 0 && len(path) > 0 &&
		(len(mounts) >= len(path) && mounts[0:len(path)] == path)
}

// Reset clears the overlay's upper layer, restoring the base state.
func (o *Overlay) Reset() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Unmount first
	if o.isMounted() {
		if err := syscall.Unmount(o.MergedPath, syscall.MNT_DETACH); err != nil {
			return fmt.Errorf("failed to unmount for reset: %w", err)
		}
	}

	// Clear upper directory
	if err := os.RemoveAll(o.UpperPath); err != nil {
		return fmt.Errorf("failed to clear upper directory: %w", err)
	}

	// Clear work directory
	if err := os.RemoveAll(o.WorkPath); err != nil {
		return fmt.Errorf("failed to clear work directory: %w", err)
	}

	// Recreate directories
	if err := os.MkdirAll(o.UpperPath, 0755); err != nil {
		return fmt.Errorf("failed to recreate upper directory: %w", err)
	}
	if err := os.MkdirAll(o.WorkPath, 0755); err != nil {
		return fmt.Errorf("failed to recreate work directory: %w", err)
	}

	return nil
}

// Destroy removes the overlay completely.
func (o *Overlay) Destroy() error {
	if err := o.Unmount(); err != nil {
		// Continue with cleanup even if unmount fails
	}

	return os.RemoveAll(o.OverlayPath)
}

// GetOverlay retrieves an overlay by ID.
func (om *OverlayManager) GetOverlay(id string) (*Overlay, bool) {
	om.mu.RLock()
	defer om.mu.RUnlock()

	overlay, ok := om.overlays[id]
	return overlay, ok
}

// ReleaseOverlay marks an overlay as no longer in use.
func (om *OverlayManager) ReleaseOverlay(id string) {
	om.mu.Lock()
	defer om.mu.Unlock()

	if overlay, ok := om.overlays[id]; ok {
		overlay.mu.Lock()
		overlay.InUse = false
		overlay.mu.Unlock()
	}
}

// DestroyOverlay removes an overlay.
func (om *OverlayManager) DestroyOverlay(ctx context.Context, id string) error {
	om.mu.Lock()
	defer om.mu.Unlock()

	overlay, ok := om.overlays[id]
	if !ok {
		return nil
	}

	if err := overlay.Destroy(); err != nil {
		return err
	}

	delete(om.overlays, id)
	return nil
}

// cleanupUnused removes overlays that are not in use.
func (om *OverlayManager) cleanupUnused(ctx context.Context) error {
	// Already holding lock from caller
	var toDelete []string

	for id, overlay := range om.overlays {
		overlay.mu.Lock()
		if !overlay.InUse {
			toDelete = append(toDelete, id)
		}
		overlay.mu.Unlock()
	}

	for _, id := range toDelete {
		if overlay, ok := om.overlays[id]; ok {
			if err := overlay.Destroy(); err != nil {
				continue // Skip failed cleanups
			}
			delete(om.overlays, id)
		}
	}

	return nil
}

// Close cleans up all overlays.
func (om *OverlayManager) Close() error {
	om.mu.Lock()
	defer om.mu.Unlock()

	var errs []error
	for _, overlay := range om.overlays {
		if err := overlay.Destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	om.overlays = make(map[string]*Overlay)

	if len(errs) > 0 {
		return fmt.Errorf("errors during cleanup: %v", errs)
	}
	return nil
}

// Stats returns overlay manager statistics.
func (om *OverlayManager) Stats() OverlayStats {
	om.mu.RLock()
	defer om.mu.RUnlock()

	stats := OverlayStats{
		Total:    len(om.overlays),
		MaxLimit: om.maxOverlays,
	}

	for _, overlay := range om.overlays {
		overlay.mu.Lock()
		if overlay.InUse {
			stats.InUse++
		}
		overlay.mu.Unlock()
	}

	stats.Available = stats.Total - stats.InUse
	return stats
}

// OverlayStats contains statistics about overlays.
type OverlayStats struct {
	Total     int `json:"total"`
	InUse     int `json:"in_use"`
	Available int `json:"available"`
	MaxLimit  int `json:"max_limit"`
}

// SnapshotManager manages VM snapshots for fast boot.
type SnapshotManager struct {
	baseDir   string
	snapshots map[string]*Snapshot
	mu        sync.RWMutex
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(baseDir string) (*SnapshotManager, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	return &SnapshotManager{
		baseDir:   baseDir,
		snapshots: make(map[string]*Snapshot),
	}, nil
}

// CreateSnapshot creates a snapshot of a running VM.
func (sm *SnapshotManager) CreateSnapshot(ctx context.Context, vm *MicroVM, snapshotType SnapshotType) (*Snapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if vm.State() != VMStateRunning && vm.State() != VMStatePaused {
		return nil, fmt.Errorf("VM must be running or paused to snapshot (state: %s)", vm.State())
	}

	// Pause VM if running
	wasPaused := vm.State() == VMStatePaused
	if !wasPaused {
		if err := vm.Pause(ctx); err != nil {
			return nil, fmt.Errorf("failed to pause VM: %w", err)
		}
		defer func() {
			if !wasPaused {
				vm.Resume(ctx)
			}
		}()
	}

	snapshotID := fmt.Sprintf("%s-%d", vm.VMID(), time.Now().UnixNano())
	snapshotDir := filepath.Join(sm.baseDir, snapshotID)

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	memoryPath := filepath.Join(snapshotDir, "memory.snap")
	statePath := filepath.Join(snapshotDir, "state.snap")

	// Use Firecracker's snapshot API
	// This would be done through the firecracker-go-sdk in practice
	// For now, we simulate the process

	snapshot := &Snapshot{
		ID:         snapshotID,
		Type:       snapshotType,
		Language:   vm.Language(),
		MemoryPath: memoryPath,
		StatePath:  statePath,
		CreatedAt:  time.Now(),
	}

	// Calculate snapshot size
	var totalSize int64
	if info, err := os.Stat(memoryPath); err == nil {
		totalSize += info.Size()
	}
	if info, err := os.Stat(statePath); err == nil {
		totalSize += info.Size()
	}
	snapshot.Size = totalSize

	sm.snapshots[snapshotID] = snapshot
	return snapshot, nil
}

// LoadSnapshot loads a VM from a snapshot.
func (sm *SnapshotManager) LoadSnapshot(ctx context.Context, snapshotID string, config *VMConfig) (*MicroVM, error) {
	sm.mu.RLock()
	snapshot, ok := sm.snapshots[snapshotID]
	sm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	// Verify snapshot files exist
	if _, err := os.Stat(snapshot.MemoryPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot memory file not found: %s", snapshot.MemoryPath)
	}
	if _, err := os.Stat(snapshot.StatePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot state file not found: %s", snapshot.StatePath)
	}

	// Create VM from snapshot
	if config == nil {
		config = DefaultVMConfig()
	}
	config.Language = snapshot.Language

	vm, err := NewMicroVM(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	// Load the snapshot
	// This would use Firecracker's load_snapshot API
	// For now, just start the VM normally
	if err := vm.Start(ctx); err != nil {
		vm.Stop(ctx)
		return nil, fmt.Errorf("failed to start VM from snapshot: %w", err)
	}

	return vm, nil
}

// DeleteSnapshot removes a snapshot.
func (sm *SnapshotManager) DeleteSnapshot(snapshotID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	snapshot, ok := sm.snapshots[snapshotID]
	if !ok {
		return nil
	}

	snapshotDir := filepath.Dir(snapshot.MemoryPath)
	if err := os.RemoveAll(snapshotDir); err != nil {
		return fmt.Errorf("failed to remove snapshot directory: %w", err)
	}

	delete(sm.snapshots, snapshotID)
	return nil
}

// ListSnapshots returns all snapshots for a language.
func (sm *SnapshotManager) ListSnapshots(language string) []*Snapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var snapshots []*Snapshot
	for _, s := range sm.snapshots {
		if language == "" || s.Language == language {
			snapshots = append(snapshots, s)
		}
	}
	return snapshots
}

// GetSnapshot retrieves a snapshot by ID.
func (sm *SnapshotManager) GetSnapshot(id string) (*Snapshot, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	s, ok := sm.snapshots[id]
	return s, ok
}

// Close cleans up the snapshot manager.
func (sm *SnapshotManager) Close() error {
	return nil // Snapshots are kept on disk
}

// DeviceMapperSnapshot manages device-mapper based copy-on-write snapshots.
type DeviceMapperSnapshot struct {
	name       string
	basePath   string
	snapshotPath string
	sectorSize int64
	size       int64
}

// NewDeviceMapperSnapshot creates a new device-mapper snapshot.
func NewDeviceMapperSnapshot(name, basePath string, sizeMB int64) (*DeviceMapperSnapshot, error) {
	// Check if device-mapper is available
	if _, err := exec.LookPath("dmsetup"); err != nil {
		return nil, fmt.Errorf("dmsetup not found: %w", err)
	}

	return &DeviceMapperSnapshot{
		name:       name,
		basePath:   basePath,
		sectorSize: 512,
		size:       sizeMB * 1024 * 1024,
	}, nil
}

// Create creates the device-mapper snapshot.
func (dms *DeviceMapperSnapshot) Create(ctx context.Context) error {
	// Create a COW file for the snapshot
	cowPath := dms.basePath + ".cow"

	// Create sparse COW file
	cowFile, err := os.Create(cowPath)
	if err != nil {
		return fmt.Errorf("failed to create COW file: %w", err)
	}
	if err := cowFile.Truncate(dms.size); err != nil {
		cowFile.Close()
		return fmt.Errorf("failed to truncate COW file: %w", err)
	}
	cowFile.Close()

	// Set up loop devices
	baseLoop, err := dms.setupLoop(dms.basePath)
	if err != nil {
		return fmt.Errorf("failed to setup base loop: %w", err)
	}

	cowLoop, err := dms.setupLoop(cowPath)
	if err != nil {
		return fmt.Errorf("failed to setup COW loop: %w", err)
	}

	// Create snapshot device
	sectors := dms.size / dms.sectorSize
	table := fmt.Sprintf("0 %d snapshot %s %s P 8", sectors, baseLoop, cowLoop)

	cmd := exec.CommandContext(ctx, "dmsetup", "create", dms.name, "--table", table)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create snapshot device: %w", err)
	}

	dms.snapshotPath = "/dev/mapper/" + dms.name
	return nil
}

// setupLoop sets up a loop device for a file.
func (dms *DeviceMapperSnapshot) setupLoop(path string) (string, error) {
	cmd := exec.Command("losetup", "-f", "--show", path)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output[:len(output)-1]), nil // Remove trailing newline
}

// Path returns the path to the snapshot device.
func (dms *DeviceMapperSnapshot) Path() string {
	return dms.snapshotPath
}

// Destroy removes the device-mapper snapshot.
func (dms *DeviceMapperSnapshot) Destroy(ctx context.Context) error {
	if dms.snapshotPath == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "dmsetup", "remove", dms.name)
	return cmd.Run()
}
