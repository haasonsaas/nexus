package infra

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// ServiceHealth represents the health state of a component.
type ServiceHealth string

const (
	ServiceHealthHealthy   ServiceHealth = "healthy"
	ServiceHealthUnhealthy ServiceHealth = "unhealthy"
	ServiceHealthDegraded  ServiceHealth = "degraded"
	ServiceHealthUnknown   ServiceHealth = "unknown"
)

// HealthCheckResult represents the result of a health check.
type HealthCheckResult struct {
	Name      string            `json:"name"`
	Status    ServiceHealth     `json:"status"`
	Message   string            `json:"message,omitempty"`
	Latency   time.Duration     `json:"latency_ms"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MarshalJSON customizes JSON marshaling for HealthCheckResult.
func (r HealthCheckResult) MarshalJSON() ([]byte, error) {
	type Alias HealthCheckResult
	return json.Marshal(&struct {
		Alias
		LatencyMS int64 `json:"latency_ms"`
	}{
		Alias:     Alias(r),
		LatencyMS: r.Latency.Milliseconds(),
	})
}

// HealthChecker is a function that performs a health check.
type HealthChecker func(ctx context.Context) HealthCheckResult

// HealthCheckConfig configures a health check.
type HealthCheckConfig struct {
	// Name identifies this health check.
	Name string

	// Timeout is the maximum time for the check.
	Timeout time.Duration

	// Interval is how often to run the check (for background checks).
	Interval time.Duration

	// Critical indicates if this check failing should mark the service unhealthy.
	Critical bool

	// Checker is the function that performs the check.
	Checker HealthChecker
}

// HealthCheckRegistry manages health checks for a service.
type HealthCheckRegistry struct {
	mu sync.RWMutex

	checks  map[string]HealthCheckConfig
	results map[string]HealthCheckResult
	cancel  context.CancelFunc
	stopped bool
}

// NewHealthCheckRegistry creates a new health check registry.
func NewHealthCheckRegistry() *HealthCheckRegistry {
	return &HealthCheckRegistry{
		checks:  make(map[string]HealthCheckConfig),
		results: make(map[string]HealthCheckResult),
	}
}

// Register registers a health check.
func (r *HealthCheckRegistry) Register(config HealthCheckConfig) {
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.checks[config.Name] = config
	r.results[config.Name] = HealthCheckResult{
		Name:      config.Name,
		Status:    ServiceHealthUnknown,
		Timestamp: time.Now(),
	}
}

// RegisterSimple registers a simple health check function.
func (r *HealthCheckRegistry) RegisterSimple(name string, checker func(ctx context.Context) error) {
	r.Register(HealthCheckConfig{
		Name:     name,
		Critical: true,
		Checker: func(ctx context.Context) HealthCheckResult {
			result := HealthCheckResult{
				Name:      name,
				Timestamp: time.Now(),
			}
			if err := checker(ctx); err != nil {
				result.Status = ServiceHealthUnhealthy
				result.Message = err.Error()
			} else {
				result.Status = ServiceHealthHealthy
			}
			return result
		},
	})
}

// Unregister removes a health check.
func (r *HealthCheckRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.checks, name)
	delete(r.results, name)
}

// Check runs a specific health check.
func (r *HealthCheckRegistry) Check(ctx context.Context, name string) (HealthCheckResult, bool) {
	r.mu.RLock()
	config, ok := r.checks[name]
	r.mu.RUnlock()

	if !ok {
		return HealthCheckResult{}, false
	}

	return r.runCheck(ctx, config), true
}

// CheckAll runs all health checks.
func (r *HealthCheckRegistry) CheckAll(ctx context.Context) HealthReport {
	r.mu.RLock()
	checks := make([]HealthCheckConfig, 0, len(r.checks))
	for _, config := range r.checks {
		checks = append(checks, config)
	}
	r.mu.RUnlock()

	results := make([]HealthCheckResult, len(checks))
	var wg sync.WaitGroup

	for i, config := range checks {
		wg.Add(1)
		go func(idx int, cfg HealthCheckConfig) {
			defer wg.Done()
			results[idx] = r.runCheck(ctx, cfg)
		}(i, config)
	}

	wg.Wait()

	// Update cached results
	r.mu.Lock()
	for _, result := range results {
		r.results[result.Name] = result
	}
	r.mu.Unlock()

	return r.buildReport(results)
}

// runCheck runs a single health check with timeout.
func (r *HealthCheckRegistry) runCheck(ctx context.Context, config HealthCheckConfig) HealthCheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	start := time.Now()
	resultCh := make(chan HealthCheckResult, 1)

	go func() {
		result := config.Checker(checkCtx)
		result.Name = config.Name // Ensure correct name
		result.Latency = time.Since(start)
		resultCh <- result
	}()

	select {
	case result := <-resultCh:
		return result
	case <-checkCtx.Done():
		return HealthCheckResult{
			Name:      config.Name,
			Status:    ServiceHealthUnhealthy,
			Message:   "health check timed out",
			Latency:   time.Since(start),
			Timestamp: time.Now(),
		}
	}
}

// GetCached returns the cached result for a check.
func (r *HealthCheckRegistry) GetCached(name string) (HealthCheckResult, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result, ok := r.results[name]
	return result, ok
}

// GetAllCached returns all cached results.
func (r *HealthCheckRegistry) GetAllCached() HealthReport {
	r.mu.RLock()
	results := make([]HealthCheckResult, 0, len(r.results))
	for _, result := range r.results {
		results = append(results, result)
	}
	r.mu.RUnlock()

	return r.buildReport(results)
}

// buildReport creates a health report from results.
func (r *HealthCheckRegistry) buildReport(results []HealthCheckResult) HealthReport {
	r.mu.RLock()
	defer r.mu.RUnlock()

	report := HealthReport{
		Timestamp: time.Now(),
		Checks:    results,
	}

	// Determine overall status
	report.Status = ServiceHealthHealthy
	for _, result := range results {
		config, ok := r.checks[result.Name]
		if !ok {
			continue
		}

		switch result.Status {
		case ServiceHealthUnhealthy:
			if config.Critical {
				report.Status = ServiceHealthUnhealthy
			} else if report.Status == ServiceHealthHealthy {
				report.Status = ServiceHealthDegraded
			}
		case ServiceHealthDegraded:
			if report.Status == ServiceHealthHealthy {
				report.Status = ServiceHealthDegraded
			}
		case ServiceHealthUnknown:
			if config.Critical && report.Status == ServiceHealthHealthy {
				report.Status = ServiceHealthUnknown
			}
		}
	}

	return report
}

// StartBackgroundChecks starts periodic health checks.
func (r *HealthCheckRegistry) StartBackgroundChecks(ctx context.Context) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}

	ctx, r.cancel = context.WithCancel(ctx)
	r.mu.Unlock()

	// Get all checks
	r.mu.RLock()
	checks := make([]HealthCheckConfig, 0, len(r.checks))
	for _, config := range r.checks {
		checks = append(checks, config)
	}
	r.mu.RUnlock()

	// Start a goroutine for each check
	for _, config := range checks {
		go r.runBackgroundCheck(ctx, config)
	}
}

// runBackgroundCheck runs periodic health checks.
func (r *HealthCheckRegistry) runBackgroundCheck(ctx context.Context, config HealthCheckConfig) {
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	// Run immediately
	result := r.runCheck(ctx, config)
	r.mu.Lock()
	r.results[config.Name] = result
	r.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result := r.runCheck(ctx, config)
			r.mu.Lock()
			r.results[config.Name] = result
			r.mu.Unlock()
		}
	}
}

// Stop stops background health checks.
func (r *HealthCheckRegistry) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopped = true
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// Names returns the names of all registered checks.
func (r *HealthCheckRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.checks))
	for name := range r.checks {
		names = append(names, name)
	}
	return names
}

// HealthReport is a complete health report.
type HealthReport struct {
	Status    ServiceHealth        `json:"status"`
	Timestamp time.Time           `json:"timestamp"`
	Checks    []HealthCheckResult `json:"checks"`
}

// IsHealthy returns true if the overall status is healthy.
func (r HealthReport) IsHealthy() bool {
	return r.Status == ServiceHealthHealthy
}

// FailedChecks returns checks that are not healthy.
func (r HealthReport) FailedChecks() []HealthCheckResult {
	var failed []HealthCheckResult
	for _, check := range r.Checks {
		if check.Status != ServiceHealthHealthy {
			failed = append(failed, check)
		}
	}
	return failed
}

// LivenessChecker creates a simple liveness check.
func LivenessChecker() HealthChecker {
	return func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{
			Name:      "liveness",
			Status:    ServiceHealthHealthy,
			Timestamp: time.Now(),
		}
	}
}

// ReadinessChecker creates a readiness check that depends on other checks.
func ReadinessChecker(registry *HealthCheckRegistry, dependencies []string) HealthChecker {
	return func(ctx context.Context) HealthCheckResult {
		result := HealthCheckResult{
			Name:      "readiness",
			Timestamp: time.Now(),
		}

		for _, dep := range dependencies {
			depResult, ok := registry.GetCached(dep)
			if !ok {
				result.Status = ServiceHealthUnknown
				result.Message = "dependency " + dep + " not found"
				return result
			}
			if depResult.Status == ServiceHealthUnhealthy {
				result.Status = ServiceHealthUnhealthy
				result.Message = "dependency " + dep + " is unhealthy"
				return result
			}
		}

		result.Status = ServiceHealthHealthy
		return result
	}
}

// DefaultHealthRegistry is the global health check registry.
var DefaultHealthRegistry = NewHealthCheckRegistry()

// RegisterHealthCheck registers a check with the default registry.
func RegisterHealthCheck(config HealthCheckConfig) {
	DefaultHealthRegistry.Register(config)
}

// CheckHealth runs all checks with the default registry.
func CheckHealth(ctx context.Context) HealthReport {
	return DefaultHealthRegistry.CheckAll(ctx)
}
