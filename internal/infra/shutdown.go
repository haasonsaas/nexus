package infra

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ShutdownPhase represents different phases during shutdown.
// Components registered with earlier phases are shut down first.
type ShutdownPhase int

const (
	// PhasePreShutdown runs first - stop accepting new work.
	PhasePreShutdown ShutdownPhase = iota
	// PhaseServices shuts down background services.
	PhaseServices
	// PhaseConnections closes external connections (databases, APIs).
	PhaseConnections
	// PhaseCleanup performs final cleanup (scratch files, logs).
	PhaseCleanup
	phaseCount // sentinel for iteration
)

func (p ShutdownPhase) String() string {
	switch p {
	case PhasePreShutdown:
		return "pre-shutdown"
	case PhaseServices:
		return "services"
	case PhaseConnections:
		return "connections"
	case PhaseCleanup:
		return "cleanup"
	default:
		return fmt.Sprintf("phase-%d", p)
	}
}

// ShutdownFunc is a function that performs cleanup during shutdown.
// It receives a context that may be cancelled if the shutdown times out.
type ShutdownFunc func(ctx context.Context) error

// ShutdownHandler represents a registered shutdown handler.
type ShutdownHandler struct {
	Name     string
	Phase    ShutdownPhase
	Func     ShutdownFunc
	Timeout  time.Duration // Optional per-handler timeout (0 = use default)
	Critical bool          // If true, errors are logged but don't stop shutdown
}

// ShutdownCoordinator manages graceful shutdown of multiple components.
type ShutdownCoordinator struct {
	mu             sync.Mutex
	handlers       [phaseCount][]ShutdownHandler
	defaultTimeout time.Duration
	logger         *slog.Logger
	shutdownOnce   sync.Once
	shutdownCh     chan struct{}
	shuttingDown   atomic.Bool
	results        []ShutdownResult
}

// ShutdownResult contains the result of a handler's shutdown.
type ShutdownResult struct {
	Name     string
	Phase    ShutdownPhase
	Duration time.Duration
	Error    error
}

// NewShutdownCoordinator creates a new shutdown coordinator.
func NewShutdownCoordinator(defaultTimeout time.Duration, logger *slog.Logger) *ShutdownCoordinator {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &ShutdownCoordinator{
		defaultTimeout: defaultTimeout,
		logger:         logger,
		shutdownCh:     make(chan struct{}),
	}
}

// Register adds a shutdown handler.
func (c *ShutdownCoordinator) Register(handler ShutdownHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if handler.Phase < 0 || handler.Phase >= phaseCount {
		handler.Phase = PhaseCleanup
	}

	c.handlers[handler.Phase] = append(c.handlers[handler.Phase], handler)
}

// RegisterFunc is a convenience method to register a simple shutdown function.
func (c *ShutdownCoordinator) RegisterFunc(name string, phase ShutdownPhase, fn ShutdownFunc) {
	c.Register(ShutdownHandler{
		Name:  name,
		Phase: phase,
		Func:  fn,
	})
}

// RegisterService is a convenience method for registering service shutdowns.
func (c *ShutdownCoordinator) RegisterService(name string, fn ShutdownFunc) {
	c.Register(ShutdownHandler{
		Name:  name,
		Phase: PhaseServices,
		Func:  fn,
	})
}

// RegisterConnection is a convenience method for registering connection closures.
func (c *ShutdownCoordinator) RegisterConnection(name string, fn ShutdownFunc) {
	c.Register(ShutdownHandler{
		Name:  name,
		Phase: PhaseConnections,
		Func:  fn,
	})
}

// OnSignal registers signal handlers for graceful shutdown.
// Returns a channel that receives when shutdown is complete.
func (c *ShutdownCoordinator) OnSignal(signals ...os.Signal) <-chan struct{} {
	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)

	done := make(chan struct{})

	go func() {
		sig := <-sigCh
		c.logger.Info("received shutdown signal", "signal", sig)

		ctx, cancel := context.WithTimeout(context.Background(), c.defaultTimeout)
		defer cancel()

		c.Shutdown(ctx)
		close(done)
	}()

	return done
}

// Shutdown performs graceful shutdown of all registered handlers.
// Handlers are called in phase order. Within a phase, handlers run concurrently.
func (c *ShutdownCoordinator) Shutdown(ctx context.Context) []ShutdownResult {
	var results []ShutdownResult

	c.shutdownOnce.Do(func() {
		c.shuttingDown.Store(true)
		close(c.shutdownCh)

		c.logger.Info("starting graceful shutdown")
		start := time.Now()

		for phase := ShutdownPhase(0); phase < phaseCount; phase++ {
			c.mu.Lock()
			handlers := c.handlers[phase]
			c.mu.Unlock()

			if len(handlers) == 0 {
				continue
			}

			c.logger.Info("executing shutdown phase", "phase", phase.String(), "handlers", len(handlers))
			phaseResults := c.runPhase(ctx, phase, handlers)
			results = append(results, phaseResults...)

			// Check if context is cancelled
			if ctx.Err() != nil {
				c.logger.Warn("shutdown context cancelled", "phase", phase.String())
				break
			}
		}

		c.logger.Info("graceful shutdown complete", "duration", time.Since(start))
		c.results = results
	})

	return results
}

// runPhase executes all handlers in a phase concurrently.
func (c *ShutdownCoordinator) runPhase(ctx context.Context, _ ShutdownPhase, handlers []ShutdownHandler) []ShutdownResult {
	results := make([]ShutdownResult, len(handlers))
	var wg sync.WaitGroup

	for i, handler := range handlers {
		wg.Add(1)
		go func(idx int, h ShutdownHandler) {
			defer wg.Done()
			results[idx] = c.runHandler(ctx, h)
		}(i, handler)
	}

	wg.Wait()
	return results
}

// runHandler executes a single handler with its timeout.
func (c *ShutdownCoordinator) runHandler(ctx context.Context, handler ShutdownHandler) ShutdownResult {
	result := ShutdownResult{
		Name:  handler.Name,
		Phase: handler.Phase,
	}

	start := time.Now()

	// Determine timeout
	timeout := handler.Timeout
	if timeout <= 0 {
		timeout = c.defaultTimeout
	}

	// Create handler context with timeout
	handlerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Run the handler
	done := make(chan error, 1)
	go func() {
		done <- handler.Func(handlerCtx)
	}()

	select {
	case err := <-done:
		result.Duration = time.Since(start)
		result.Error = err
		if err != nil {
			c.logger.Warn("shutdown handler error",
				"handler", handler.Name,
				"phase", handler.Phase.String(),
				"error", err,
				"critical", handler.Critical,
			)
		} else {
			c.logger.Debug("shutdown handler complete",
				"handler", handler.Name,
				"duration", result.Duration,
			)
		}
	case <-handlerCtx.Done():
		result.Duration = time.Since(start)
		result.Error = handlerCtx.Err()
		c.logger.Warn("shutdown handler timed out",
			"handler", handler.Name,
			"phase", handler.Phase.String(),
			"timeout", timeout,
		)
	}

	return result
}

// IsShuttingDown returns true if shutdown has been initiated.
func (c *ShutdownCoordinator) IsShuttingDown() bool {
	return c.shuttingDown.Load()
}

// Done returns a channel that is closed when shutdown begins.
func (c *ShutdownCoordinator) Done() <-chan struct{} {
	return c.shutdownCh
}

// Results returns the results from the last shutdown.
func (c *ShutdownCoordinator) Results() []ShutdownResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.results
}

// DefaultShutdownCoordinator is the global shutdown coordinator.
var DefaultShutdownCoordinator = NewShutdownCoordinator(30*time.Second, nil)

// RegisterShutdown registers a handler with the default coordinator.
func RegisterShutdown(handler ShutdownHandler) {
	DefaultShutdownCoordinator.Register(handler)
}

// RegisterShutdownFunc registers a function with the default coordinator.
func RegisterShutdownFunc(name string, phase ShutdownPhase, fn ShutdownFunc) {
	DefaultShutdownCoordinator.RegisterFunc(name, phase, fn)
}

// OnShutdownSignal sets up signal handling with the default coordinator.
func OnShutdownSignal(signals ...os.Signal) <-chan struct{} {
	return DefaultShutdownCoordinator.OnSignal(signals...)
}

// TriggerShutdown initiates shutdown on the default coordinator.
func TriggerShutdown(ctx context.Context) []ShutdownResult {
	return DefaultShutdownCoordinator.Shutdown(ctx)
}
