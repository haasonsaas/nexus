//go:build linux

package firecracker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PoolConfig contains configuration for the VM pool.
type PoolConfig struct {
	// InitialSize is the number of VMs to pre-warm at startup.
	InitialSize int

	// MaxSize is the maximum number of VMs in the pool.
	MaxSize int

	// MinIdle is the minimum number of idle VMs to maintain.
	MinIdle int

	// MaxIdleTime is how long a VM can be idle before being recycled.
	MaxIdleTime time.Duration

	// MaxExecCount is the maximum executions per VM before recycling.
	MaxExecCount int

	// MaxUptime is the maximum VM lifetime before recycling.
	MaxUptime time.Duration

	// WarmupInterval is how often to check and replenish the pool.
	WarmupInterval time.Duration

	// KernelPath is the path to the kernel image.
	KernelPath string

	// RootFSImages maps languages to their rootfs images.
	RootFSImages map[string]string

	// DefaultVCPUs is the default number of vCPUs per VM.
	DefaultVCPUs int64

	// DefaultMemMB is the default memory in MB per VM.
	DefaultMemMB int64

	// NetworkEnabled determines if network access is allowed.
	NetworkEnabled bool

	// OverlayEnabled enables copy-on-write overlays.
	OverlayEnabled bool

	// OverlayDir is the directory for overlay files.
	OverlayDir string
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
		NetworkEnabled: false,
		OverlayEnabled: true,
		OverlayDir:     "/tmp/firecracker-overlays",
	}
}

// VMPool manages a pool of pre-warmed Firecracker microVMs.
type VMPool struct {
	config *PoolConfig

	// pools holds per-language pools of VMs.
	pools   map[string]*languageVMPool
	poolsMu sync.RWMutex

	// cidCounter generates unique CIDs for vsock.
	cidCounter uint32

	// stats tracks pool statistics.
	stats PoolStats

	// closed indicates if the pool is shut down.
	closed   bool
	closedMu sync.RWMutex

	// stopCh signals background goroutines to stop.
	stopCh chan struct{}

	// wg tracks background goroutines.
	wg sync.WaitGroup
}

// languageVMPool manages VMs for a specific language.
type languageVMPool struct {
	language  string
	available chan *MicroVM
	creating  int32 // atomic
	total     int32 // atomic
	config    *PoolConfig
}

// PoolStats contains statistics about the VM pool.
type PoolStats struct {
	TotalVMs        int64 `json:"total_vms"`
	IdleVMs         int64 `json:"idle_vms"`
	ActiveVMs       int64 `json:"active_vms"`
	TotalCreated    int64 `json:"total_created"`
	TotalDestroyed  int64 `json:"total_destroyed"`
	TotalExecutions int64 `json:"total_executions"`
	CreationErrors  int64 `json:"creation_errors"`
}

// NewVMPool creates a new VM pool.
func NewVMPool(config *PoolConfig) (*VMPool, error) {
	if config == nil {
		config = DefaultPoolConfig()
	}

	if config.MaxSize < 1 {
		return nil, errors.New("max pool size must be at least 1")
	}

	pool := &VMPool{
		config:     config,
		pools:      make(map[string]*languageVMPool),
		cidCounter: 3, // CIDs 0, 1, 2 are reserved
		stopCh:     make(chan struct{}),
	}

	// Initialize per-language pools
	languages := []string{"python", "nodejs", "go", "bash"}
	for _, lang := range languages {
		if _, ok := config.RootFSImages[lang]; !ok {
			continue // Skip if no rootfs configured for this language
		}

		pool.pools[lang] = &languageVMPool{
			language:  lang,
			available: make(chan *MicroVM, config.MaxSize),
			config:    config,
		}
	}

	return pool, nil
}

func (p *VMPool) stopVM(ctx context.Context, vm *MicroVM) {
	if vm == nil {
		return
	}
	if err := vm.Stop(ctx); err != nil {
		// Best-effort cleanup; nothing to do here without a logger.
		_ = err
	}
}

// Start initializes the pool and begins background maintenance.
func (p *VMPool) Start(ctx context.Context) error {
	// Pre-warm the pool
	if err := p.warmup(ctx); err != nil {
		return fmt.Errorf("failed to warmup pool: %w", err)
	}

	// Start background maintenance
	p.wg.Add(1)
	go p.maintenanceLoop()

	return nil
}

// warmup pre-creates VMs for each language.
func (p *VMPool) warmup(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(p.pools)*p.config.InitialSize)

	for lang, langPool := range p.pools {
		for i := 0; i < p.config.InitialSize; i++ {
			wg.Add(1)
			go func(language string, lp *languageVMPool) {
				defer wg.Done()

				vm, err := p.createVM(ctx, language)
				if err != nil {
					errCh <- fmt.Errorf("failed to create %s VM: %w", language, err)
					return
				}

				select {
				case lp.available <- vm:
					atomic.AddInt64(&p.stats.IdleVMs, 1)
				default:
					// Pool full, stop the VM
					p.stopVM(ctx, vm)
				}
			}(lang, langPool)
		}
	}

	wg.Wait()
	close(errCh)

	// Collect errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
		atomic.AddInt64(&p.stats.CreationErrors, 1)
	}

	if len(errs) > 0 && len(errs) >= len(p.pools)*p.config.InitialSize {
		return fmt.Errorf("all warmup VMs failed to create: %v", errs[0])
	}

	return nil
}

// maintenanceLoop runs periodic maintenance tasks.
func (p *VMPool) maintenanceLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.WarmupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.performMaintenance()
		}
	}
}

// performMaintenance checks pool health and replenishes VMs.
func (p *VMPool) performMaintenance() {
	// Use timeout context to prevent maintenance from hanging indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	p.poolsMu.RLock()
	defer p.poolsMu.RUnlock()

	for lang, langPool := range p.pools {
		// Check idle VM count
		idleCount := len(langPool.available)
		totalCount := int(atomic.LoadInt32(&langPool.total))

		// Recycle old VMs
		p.recycleOldVMs(ctx, langPool)

		// Replenish if below minimum
		if idleCount < p.config.MinIdle && totalCount < p.config.MaxSize {
			toCreate := p.config.MinIdle - idleCount
			if toCreate+totalCount > p.config.MaxSize {
				toCreate = p.config.MaxSize - totalCount
			}

			for i := 0; i < toCreate; i++ {
				go func(language string, lp *languageVMPool) {
					vm, err := p.createVM(ctx, language)
					if err != nil {
						atomic.AddInt64(&p.stats.CreationErrors, 1)
						return
					}

					select {
					case lp.available <- vm:
						atomic.AddInt64(&p.stats.IdleVMs, 1)
					default:
						p.stopVM(ctx, vm)
					}
				}(lang, langPool)
			}
		}
	}
}

// recycleOldVMs removes VMs that exceed max uptime or exec count.
func (p *VMPool) recycleOldVMs(ctx context.Context, langPool *languageVMPool) {
	// Check each available VM
	toCheck := len(langPool.available)
	for i := 0; i < toCheck; i++ {
		select {
		case vm := <-langPool.available:
			shouldRecycle := false

			// Check uptime
			if p.config.MaxUptime > 0 && vm.Uptime() > p.config.MaxUptime {
				shouldRecycle = true
			}

			// Check exec count
			if p.config.MaxExecCount > 0 && vm.ExecCount() >= p.config.MaxExecCount {
				shouldRecycle = true
			}

			if shouldRecycle {
				go func(v *MicroVM) {
					p.stopVM(ctx, v)
					atomic.AddInt32(&langPool.total, -1)
					atomic.AddInt64(&p.stats.TotalDestroyed, 1)
					atomic.AddInt64(&p.stats.IdleVMs, -1)
				}(vm)
			} else {
				// Put it back
				langPool.available <- vm
			}
		default:
			return
		}
	}
}

// Get retrieves a VM from the pool for the specified language.
func (p *VMPool) Get(ctx context.Context, language string) (*MicroVM, error) {
	p.closedMu.RLock()
	if p.closed {
		p.closedMu.RUnlock()
		return nil, errors.New("pool is closed")
	}
	p.closedMu.RUnlock()

	p.poolsMu.RLock()
	langPool, ok := p.pools[language]
	p.poolsMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	// Try to get an available VM
	select {
	case vm := <-langPool.available:
		atomic.AddInt64(&p.stats.IdleVMs, -1)
		atomic.AddInt64(&p.stats.ActiveVMs, 1)
		return vm, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// No VM available, try to create one
	}

	// Check if we can create a new VM
	totalCount := int(atomic.LoadInt32(&langPool.total))
	creatingCount := int(atomic.LoadInt32(&langPool.creating))

	if totalCount+creatingCount >= p.config.MaxSize {
		// Wait for an available VM
		select {
		case vm := <-langPool.available:
			atomic.AddInt64(&p.stats.IdleVMs, -1)
			atomic.AddInt64(&p.stats.ActiveVMs, 1)
			return vm, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
			return nil, errors.New("timeout waiting for available VM")
		}
	}

	// Create a new VM
	atomic.AddInt32(&langPool.creating, 1)
	defer atomic.AddInt32(&langPool.creating, -1)

	vm, err := p.createVM(ctx, language)
	if err != nil {
		atomic.AddInt64(&p.stats.CreationErrors, 1)
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}

	atomic.AddInt64(&p.stats.ActiveVMs, 1)
	return vm, nil
}

// Put returns a VM to the pool.
func (p *VMPool) Put(vm *MicroVM) {
	if vm == nil {
		return
	}

	p.closedMu.RLock()
	if p.closed {
		p.closedMu.RUnlock()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		p.stopVM(stopCtx, vm)
		stopCancel()
		return
	}
	p.closedMu.RUnlock()

	atomic.AddInt64(&p.stats.ActiveVMs, -1)

	// Check if VM should be recycled
	shouldRecycle := false
	if p.config.MaxExecCount > 0 && vm.ExecCount() >= p.config.MaxExecCount {
		shouldRecycle = true
	}
	if p.config.MaxUptime > 0 && vm.Uptime() > p.config.MaxUptime {
		shouldRecycle = true
	}
	if vm.State() != VMStateRunning {
		shouldRecycle = true
	}

	if shouldRecycle {
		go func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			p.stopVM(stopCtx, vm)
			stopCancel()
			atomic.AddInt64(&p.stats.TotalDestroyed, 1)

			p.poolsMu.RLock()
			langPool, ok := p.pools[vm.Language()]
			p.poolsMu.RUnlock()
			if ok {
				atomic.AddInt32(&langPool.total, -1)
			}
		}()
		return
	}

	// Reset VM state before returning to pool
	if vm.Vsock() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := vm.Vsock().Reset(ctx); err != nil {
			_ = err
		}
		cancel()
	}

	p.poolsMu.RLock()
	langPool, ok := p.pools[vm.Language()]
	p.poolsMu.RUnlock()

	if !ok {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		p.stopVM(stopCtx, vm)
		stopCancel()
		return
	}

	// Try to return to pool
	select {
	case langPool.available <- vm:
		atomic.AddInt64(&p.stats.IdleVMs, 1)
	default:
		// Pool is full, destroy the VM
		go func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			p.stopVM(stopCtx, vm)
			stopCancel()
			atomic.AddInt32(&langPool.total, -1)
			atomic.AddInt64(&p.stats.TotalDestroyed, 1)
		}()
	}
}

// createVM creates a new microVM for the specified language.
func (p *VMPool) createVM(ctx context.Context, language string) (*MicroVM, error) {
	rootfsPath, ok := p.config.RootFSImages[language]
	if !ok {
		return nil, fmt.Errorf("no rootfs image for language: %s", language)
	}

	// Generate unique CID
	cid := atomic.AddUint32(&p.cidCounter, 1)

	vmConfig := &VMConfig{
		KernelPath:     p.config.KernelPath,
		RootFSPath:     rootfsPath,
		VCPUs:          p.config.DefaultVCPUs,
		MemSizeMB:      p.config.DefaultMemMB,
		NetworkEnabled: p.config.NetworkEnabled,
		VsockCID:       cid,
		Language:       language,
	}

	// Set up overlay if enabled
	if p.config.OverlayEnabled {
		overlayPath := fmt.Sprintf("%s/%s-%d.overlay", p.config.OverlayDir, language, cid)
		vmConfig.OverlayPath = overlayPath
	}

	// Create the VM
	vm, err := NewMicroVM(ctx, vmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create microVM: %w", err)
	}

	// Start the VM
	if err := vm.Start(ctx); err != nil {
		p.stopVM(ctx, vm)
		return nil, fmt.Errorf("failed to start microVM: %w", err)
	}

	// Wait for guest agent to be ready
	if vm.Vsock() != nil {
		healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		for i := 0; i < 30; i++ {
			if err := vm.Vsock().Health(healthCtx); err == nil {
				break
			}
			select {
			case <-healthCtx.Done():
				p.stopVM(ctx, vm)
				return nil, fmt.Errorf("guest agent health check timeout")
			case <-time.After(time.Second):
				continue
			}
		}
	}

	// Update stats
	p.poolsMu.RLock()
	langPool, ok := p.pools[language]
	p.poolsMu.RUnlock()
	if ok {
		atomic.AddInt32(&langPool.total, 1)
	}
	atomic.AddInt64(&p.stats.TotalCreated, 1)
	atomic.AddInt64(&p.stats.TotalVMs, 1)

	return vm, nil
}

// Stats returns current pool statistics.
func (p *VMPool) Stats() PoolStats {
	return PoolStats{
		TotalVMs:        atomic.LoadInt64(&p.stats.TotalVMs),
		IdleVMs:         atomic.LoadInt64(&p.stats.IdleVMs),
		ActiveVMs:       atomic.LoadInt64(&p.stats.ActiveVMs),
		TotalCreated:    atomic.LoadInt64(&p.stats.TotalCreated),
		TotalDestroyed:  atomic.LoadInt64(&p.stats.TotalDestroyed),
		TotalExecutions: atomic.LoadInt64(&p.stats.TotalExecutions),
		CreationErrors:  atomic.LoadInt64(&p.stats.CreationErrors),
	}
}

// LanguageStats returns statistics for a specific language pool.
func (p *VMPool) LanguageStats(language string) (available int, total int, err error) {
	p.poolsMu.RLock()
	langPool, ok := p.pools[language]
	p.poolsMu.RUnlock()

	if !ok {
		return 0, 0, fmt.Errorf("unsupported language: %s", language)
	}

	return len(langPool.available), int(atomic.LoadInt32(&langPool.total)), nil
}

// Warmup pre-creates additional VMs for a language.
func (p *VMPool) Warmup(ctx context.Context, language string, count int) error {
	p.closedMu.RLock()
	if p.closed {
		p.closedMu.RUnlock()
		return errors.New("pool is closed")
	}
	p.closedMu.RUnlock()

	p.poolsMu.RLock()
	langPool, ok := p.pools[language]
	p.poolsMu.RUnlock()

	if !ok {
		return fmt.Errorf("unsupported language: %s", language)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, count)

	for i := 0; i < count; i++ {
		totalCount := int(atomic.LoadInt32(&langPool.total))
		if totalCount >= p.config.MaxSize {
			break
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			vm, err := p.createVM(ctx, language)
			if err != nil {
				errCh <- err
				return
			}

			select {
			case langPool.available <- vm:
				atomic.AddInt64(&p.stats.IdleVMs, 1)
			default:
				p.stopVM(ctx, vm)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// Return first error if any
	for err := range errCh {
		return err
	}

	return nil
}

// Shrink reduces the pool size by stopping idle VMs.
func (p *VMPool) Shrink(language string, count int) error {
	p.poolsMu.RLock()
	langPool, ok := p.pools[language]
	p.poolsMu.RUnlock()

	if !ok {
		return fmt.Errorf("unsupported language: %s", language)
	}

	ctx := context.Background()
	for i := 0; i < count; i++ {
		select {
		case vm := <-langPool.available:
			go func(v *MicroVM) {
				p.stopVM(ctx, v)
				atomic.AddInt32(&langPool.total, -1)
				atomic.AddInt64(&p.stats.TotalDestroyed, 1)
				atomic.AddInt64(&p.stats.IdleVMs, -1)
			}(vm)
		default:
			return nil
		}
	}

	return nil
}

// Close shuts down the pool and all VMs.
func (p *VMPool) Close() error {
	p.closedMu.Lock()
	if p.closed {
		p.closedMu.Unlock()
		return nil
	}
	p.closed = true
	p.closedMu.Unlock()

	// Signal background goroutines to stop
	close(p.stopCh)

	// Wait for background goroutines
	p.wg.Wait()

	// Stop all VMs
	ctx := context.Background()
	p.poolsMu.Lock()
	defer p.poolsMu.Unlock()

	for _, langPool := range p.pools {
		close(langPool.available)
		for vm := range langPool.available {
			p.stopVM(ctx, vm)
		}
	}

	return nil
}

// Health checks the health of the pool.
func (p *VMPool) Health() error {
	p.closedMu.RLock()
	if p.closed {
		p.closedMu.RUnlock()
		return errors.New("pool is closed")
	}
	p.closedMu.RUnlock()

	p.poolsMu.RLock()
	defer p.poolsMu.RUnlock()

	for lang, langPool := range p.pools {
		available := len(langPool.available)
		total := int(atomic.LoadInt32(&langPool.total))

		if available == 0 && total == 0 {
			return fmt.Errorf("no VMs available for %s", lang)
		}
	}

	return nil
}

// IncrementExecCount increments the total execution counter.
func (p *VMPool) IncrementExecCount() {
	atomic.AddInt64(&p.stats.TotalExecutions, 1)
}
