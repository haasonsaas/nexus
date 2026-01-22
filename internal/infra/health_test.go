package infra

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthCheckRegistry_Register(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:     "test",
		Critical: true,
		Checker: func(ctx context.Context) HealthCheckResult {
			return HealthCheckResult{
				Name:   "test",
				Status: ServiceHealthHealthy,
			}
		},
	})

	names := registry.Names()
	if len(names) != 1 || names[0] != "test" {
		t.Errorf("expected 1 check named 'test', got %v", names)
	}
}

func TestHealthCheckRegistry_RegisterSimple(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.RegisterSimple("db", func(ctx context.Context) error {
		return nil
	})

	result, ok := registry.Check(context.Background(), "db")
	if !ok {
		t.Fatal("expected check to be found")
	}
	if result.Status != ServiceHealthHealthy {
		t.Errorf("expected healthy status, got %s", result.Status)
	}
}

func TestHealthCheckRegistry_RegisterSimpleError(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.RegisterSimple("db", func(ctx context.Context) error {
		return errors.New("connection failed")
	})

	result, ok := registry.Check(context.Background(), "db")
	if !ok {
		t.Fatal("expected check to be found")
	}
	if result.Status != ServiceHealthUnhealthy {
		t.Errorf("expected unhealthy status, got %s", result.Status)
	}
	if result.Message != "connection failed" {
		t.Errorf("expected error message, got %s", result.Message)
	}
}

func TestHealthCheckRegistry_Unregister(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:    "test",
		Checker: LivenessChecker(),
	})

	registry.Unregister("test")

	names := registry.Names()
	if len(names) != 0 {
		t.Errorf("expected 0 checks after unregister, got %d", len(names))
	}
}

func TestHealthCheckRegistry_Check(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name: "test",
		Checker: func(ctx context.Context) HealthCheckResult {
			return HealthCheckResult{
				Name:   "test",
				Status: ServiceHealthHealthy,
				Metadata: map[string]string{
					"version": "1.0",
				},
			}
		},
	})

	result, ok := registry.Check(context.Background(), "test")
	if !ok {
		t.Fatal("expected check to be found")
	}
	if result.Status != ServiceHealthHealthy {
		t.Errorf("expected healthy, got %s", result.Status)
	}
	if result.Metadata["version"] != "1.0" {
		t.Errorf("expected version metadata, got %v", result.Metadata)
	}
}

func TestHealthCheckRegistry_CheckNotFound(t *testing.T) {
	registry := NewHealthCheckRegistry()

	_, ok := registry.Check(context.Background(), "nonexistent")
	if ok {
		t.Error("expected check not found")
	}
}

func TestHealthCheckRegistry_CheckTimeout(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:    "slow",
		Timeout: 50 * time.Millisecond,
		Checker: func(ctx context.Context) HealthCheckResult {
			time.Sleep(200 * time.Millisecond)
			return HealthCheckResult{
				Name:   "slow",
				Status: ServiceHealthHealthy,
			}
		},
	})

	result, ok := registry.Check(context.Background(), "slow")
	if !ok {
		t.Fatal("expected check to be found")
	}
	if result.Status != ServiceHealthUnhealthy {
		t.Errorf("expected unhealthy due to timeout, got %s", result.Status)
	}
	if result.Message != "health check timed out" {
		t.Errorf("expected timeout message, got %s", result.Message)
	}
}

func TestHealthCheckRegistry_CheckAll(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:    "check1",
		Checker: LivenessChecker(),
	})
	registry.Register(HealthCheckConfig{
		Name: "check2",
		Checker: func(ctx context.Context) HealthCheckResult {
			return HealthCheckResult{
				Name:   "check2",
				Status: ServiceHealthHealthy,
			}
		},
	})

	report := registry.CheckAll(context.Background())

	if report.Status != ServiceHealthHealthy {
		t.Errorf("expected overall healthy, got %s", report.Status)
	}
	if len(report.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(report.Checks))
	}
}

func TestHealthCheckRegistry_CheckAllUnhealthy(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:     "healthy",
		Critical: false,
		Checker:  LivenessChecker(),
	})
	registry.Register(HealthCheckConfig{
		Name:     "unhealthy",
		Critical: true,
		Checker: func(ctx context.Context) HealthCheckResult {
			return HealthCheckResult{
				Name:   "unhealthy",
				Status: ServiceHealthUnhealthy,
			}
		},
	})

	report := registry.CheckAll(context.Background())

	if report.Status != ServiceHealthUnhealthy {
		t.Errorf("expected overall unhealthy due to critical check, got %s", report.Status)
	}
}

func TestHealthCheckRegistry_CheckAllDegraded(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:     "healthy",
		Critical: true,
		Checker:  LivenessChecker(),
	})
	registry.Register(HealthCheckConfig{
		Name:     "unhealthy",
		Critical: false, // Not critical
		Checker: func(ctx context.Context) HealthCheckResult {
			return HealthCheckResult{
				Name:   "unhealthy",
				Status: ServiceHealthUnhealthy,
			}
		},
	})

	report := registry.CheckAll(context.Background())

	if report.Status != ServiceHealthDegraded {
		t.Errorf("expected degraded (non-critical failure), got %s", report.Status)
	}
}

func TestHealthCheckRegistry_GetCached(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:    "test",
		Checker: LivenessChecker(),
	})

	// Run check to populate cache
	registry.CheckAll(context.Background())

	result, ok := registry.GetCached("test")
	if !ok {
		t.Fatal("expected cached result")
	}
	if result.Status != ServiceHealthHealthy {
		t.Errorf("expected healthy, got %s", result.Status)
	}
}

func TestHealthCheckRegistry_GetAllCached(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:    "test1",
		Checker: LivenessChecker(),
	})
	registry.Register(HealthCheckConfig{
		Name:    "test2",
		Checker: LivenessChecker(),
	})

	// Run checks to populate cache
	registry.CheckAll(context.Background())

	report := registry.GetAllCached()
	if len(report.Checks) != 2 {
		t.Errorf("expected 2 cached checks, got %d", len(report.Checks))
	}
}

func TestHealthCheckRegistry_BackgroundChecks(t *testing.T) {
	registry := NewHealthCheckRegistry()

	var count int32

	registry.Register(HealthCheckConfig{
		Name:     "counter",
		Interval: 20 * time.Millisecond,
		Checker: func(ctx context.Context) HealthCheckResult {
			atomic.AddInt32(&count, 1)
			return HealthCheckResult{
				Name:   "counter",
				Status: ServiceHealthHealthy,
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry.StartBackgroundChecks(ctx)

	// Wait for a few intervals
	time.Sleep(100 * time.Millisecond)

	registry.Stop()

	finalCount := atomic.LoadInt32(&count)
	if finalCount < 3 {
		t.Errorf("expected at least 3 checks, got %d", finalCount)
	}
}

func TestHealthCheckRegistry_Stop(t *testing.T) {
	registry := NewHealthCheckRegistry()

	var count int32

	registry.Register(HealthCheckConfig{
		Name:     "counter",
		Interval: 10 * time.Millisecond,
		Checker: func(ctx context.Context) HealthCheckResult {
			atomic.AddInt32(&count, 1)
			return HealthCheckResult{
				Name:   "counter",
				Status: ServiceHealthHealthy,
			}
		},
	})

	ctx := context.Background()
	registry.StartBackgroundChecks(ctx)

	time.Sleep(50 * time.Millisecond)
	registry.Stop()

	countAtStop := atomic.LoadInt32(&count)
	time.Sleep(50 * time.Millisecond)
	countAfterStop := atomic.LoadInt32(&count)

	if countAfterStop > countAtStop+1 {
		t.Errorf("expected checks to stop, count went from %d to %d", countAtStop, countAfterStop)
	}
}

func TestHealthReport_IsHealthy(t *testing.T) {
	report := HealthReport{Status: ServiceHealthHealthy}
	if !report.IsHealthy() {
		t.Error("expected IsHealthy() to return true")
	}

	report = HealthReport{Status: ServiceHealthUnhealthy}
	if report.IsHealthy() {
		t.Error("expected IsHealthy() to return false")
	}
}

func TestHealthReport_FailedChecks(t *testing.T) {
	report := HealthReport{
		Checks: []HealthCheckResult{
			{Name: "healthy", Status: ServiceHealthHealthy},
			{Name: "unhealthy", Status: ServiceHealthUnhealthy},
			{Name: "degraded", Status: ServiceHealthDegraded},
		},
	}

	failed := report.FailedChecks()
	if len(failed) != 2 {
		t.Errorf("expected 2 failed checks, got %d", len(failed))
	}
}

func TestLivenessChecker(t *testing.T) {
	checker := LivenessChecker()
	result := checker(context.Background())

	if result.Name != "liveness" {
		t.Errorf("expected name 'liveness', got %s", result.Name)
	}
	if result.Status != ServiceHealthHealthy {
		t.Errorf("expected healthy, got %s", result.Status)
	}
}

func TestReadinessChecker(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name:    "db",
		Checker: LivenessChecker(),
	})

	// Run to populate cache
	registry.CheckAll(context.Background())

	checker := ReadinessChecker(registry, []string{"db"})
	result := checker(context.Background())

	if result.Status != ServiceHealthHealthy {
		t.Errorf("expected healthy readiness, got %s", result.Status)
	}
}

func TestReadinessChecker_UnhealthyDependency(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name: "db",
		Checker: func(ctx context.Context) HealthCheckResult {
			return HealthCheckResult{
				Name:   "db",
				Status: ServiceHealthUnhealthy,
			}
		},
	})

	// Run to populate cache
	registry.CheckAll(context.Background())

	checker := ReadinessChecker(registry, []string{"db"})
	result := checker(context.Background())

	if result.Status != ServiceHealthUnhealthy {
		t.Errorf("expected unhealthy readiness, got %s", result.Status)
	}
}

func TestReadinessChecker_MissingDependency(t *testing.T) {
	registry := NewHealthCheckRegistry()

	checker := ReadinessChecker(registry, []string{"nonexistent"})
	result := checker(context.Background())

	if result.Status != ServiceHealthUnknown {
		t.Errorf("expected unknown status for missing dependency, got %s", result.Status)
	}
}

func TestDefaultHealthRegistry(t *testing.T) {
	// Reset for test
	DefaultHealthRegistry = NewHealthCheckRegistry()

	RegisterHealthCheck(HealthCheckConfig{
		Name:    "test",
		Checker: LivenessChecker(),
	})

	report := CheckHealth(context.Background())

	if len(report.Checks) != 1 {
		t.Errorf("expected 1 check, got %d", len(report.Checks))
	}
}

func TestHealthCheckResult_MarshalJSON(t *testing.T) {
	result := HealthCheckResult{
		Name:      "test",
		Status:    ServiceHealthHealthy,
		Latency:   150 * time.Millisecond,
		Timestamp: time.Now(),
	}

	data, err := result.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Should contain latency_ms as number
	if string(data) == "" {
		t.Error("expected non-empty JSON")
	}
}

func TestHealthCheckRegistry_CheckRecordsLatency(t *testing.T) {
	registry := NewHealthCheckRegistry()

	registry.Register(HealthCheckConfig{
		Name: "slow",
		Checker: func(ctx context.Context) HealthCheckResult {
			time.Sleep(50 * time.Millisecond)
			return HealthCheckResult{
				Name:   "slow",
				Status: ServiceHealthHealthy,
			}
		},
	})

	result, ok := registry.Check(context.Background(), "slow")
	if !ok {
		t.Fatal("expected check to be found")
	}

	if result.Latency < 40*time.Millisecond {
		t.Errorf("expected latency >= 40ms, got %v", result.Latency)
	}
}
