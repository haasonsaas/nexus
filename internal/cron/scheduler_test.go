package cron

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestNewScheduler_EmptyConfig(t *testing.T) {
	cfg := config.CronConfig{}
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if scheduler == nil {
		t.Fatal("expected non-nil scheduler")
	}
	if len(scheduler.jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(scheduler.jobs))
	}
}

func TestNewScheduler_WithOptions(t *testing.T) {
	cfg := config.CronConfig{}
	customNow := func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }
	customClient := &http.Client{}

	scheduler, err := NewScheduler(cfg,
		WithNow(customNow),
		WithHTTPClient(customClient),
		WithTickInterval(time.Minute),
	)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if scheduler.httpClient != customClient {
		t.Error("custom HTTP client not set")
	}
	if scheduler.tickInterval != time.Minute {
		t.Errorf("expected tick interval minute, got %v", scheduler.tickInterval)
	}
}

func TestNewScheduler_DisabledJob(t *testing.T) {
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "disabled-job",
				Name:    "test",
				Type:    "webhook",
				Enabled: false, // disabled
				Schedule: config.CronScheduleConfig{
					Every: time.Hour,
				},
			},
		},
	}
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	// Disabled jobs are skipped
	if len(scheduler.jobs) != 0 {
		t.Errorf("expected 0 jobs (disabled skipped), got %d", len(scheduler.jobs))
	}
}

func TestScheduler_Jobs(t *testing.T) {
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "job-1",
				Name:    "test",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					Every: time.Hour,
				},
				Webhook: &config.CronWebhookConfig{
					URL: "http://example.com",
				},
			},
		},
	}
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	jobs := scheduler.Jobs()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestScheduler_RunJob_NotFound(t *testing.T) {
	cfg := config.CronConfig{}
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	err = scheduler.RunJob(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestScheduler_Start_AlreadyStarted(t *testing.T) {
	cfg := config.CronConfig{}
	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start in goroutine
	go scheduler.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	// Try to start again - should return nil (idempotent)
	err = scheduler.Start(ctx)
	if err != nil {
		t.Errorf("expected nil error for idempotent start, got %v", err)
	}

	cancel()
	time.Sleep(10 * time.Millisecond)
}

func TestScheduler_Start_NilScheduler(t *testing.T) {
	var scheduler *Scheduler
	err := scheduler.Start(context.Background())
	if err != nil {
		t.Error("expected nil error for nil scheduler")
	}
}

func TestSchedulerRunsWebhookJob(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "job-1",
				Name:    "webhook",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					At: now.Format(time.RFC3339),
				},
				Webhook: &config.CronWebhookConfig{
					URL: server.URL,
				},
			},
		},
	}

	scheduler, err := NewScheduler(cfg, WithNow(func() time.Time { return now }), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	count := scheduler.RunOnce(context.Background())
	if count != 1 {
		t.Fatalf("expected 1 job run, got %d", count)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected webhook to be called")
	}
}

func TestSchedulerRunsJobWithHeaders(t *testing.T) {
	var receivedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "job-with-headers",
				Name:    "webhook",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					At: now.Format(time.RFC3339),
				},
				Webhook: &config.CronWebhookConfig{
					URL:     server.URL,
					Headers: map[string]string{"X-Custom": "test-value"},
				},
			},
		},
	}

	scheduler, err := NewScheduler(cfg, WithNow(func() time.Time { return now }), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	scheduler.RunOnce(context.Background())

	if receivedHeader != "test-value" {
		t.Errorf("expected X-Custom header 'test-value', got %q", receivedHeader)
	}
}

func TestSchedulerRunsWebhookWithAuth(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "job-auth",
				Name:    "webhook",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					At: now.Format(time.RFC3339),
				},
				Webhook: &config.CronWebhookConfig{
					URL: server.URL,
					Auth: &config.CronWebhookAuth{
						Type:  "bearer",
						Token: "secret-token",
					},
				},
			},
		},
	}

	scheduler, err := NewScheduler(cfg, WithNow(func() time.Time { return now }), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	count := scheduler.RunOnce(context.Background())
	if count != 1 {
		t.Fatalf("expected 1 job run, got %d", count)
	}
	if authHeader != "Bearer secret-token" {
		t.Fatalf("expected bearer auth header, got %q", authHeader)
	}
}

func TestSchedulerRegisterUnregisterJob(t *testing.T) {
	scheduler, err := NewScheduler(config.CronConfig{})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	jobCfg := config.CronJobConfig{
		ID:      "job-1",
		Name:    "dynamic",
		Type:    "webhook",
		Enabled: true,
		Schedule: config.CronScheduleConfig{
			Every: time.Hour,
		},
		Webhook: &config.CronWebhookConfig{URL: "http://example.com"},
	}
	job, err := scheduler.RegisterJob(jobCfg)
	if err != nil {
		t.Fatalf("RegisterJob() error = %v", err)
	}
	if job == nil || job.ID != "job-1" {
		t.Fatalf("expected job to be registered")
	}
	if len(scheduler.Jobs()) != 1 {
		t.Fatalf("expected 1 job, got %d", len(scheduler.Jobs()))
	}
	if !scheduler.UnregisterJob("job-1") {
		t.Fatal("expected job to be removed")
	}
	if len(scheduler.Jobs()) != 0 {
		t.Fatalf("expected 0 jobs after removal")
	}
}

func TestSchedulerRetrySchedulesNextRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "job-retry",
				Name:    "retry",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					At: now.Format(time.RFC3339),
				},
				Webhook: &config.CronWebhookConfig{URL: server.URL},
				Retry: config.CronRetryConfig{
					MaxRetries: 2,
					Backoff:    time.Minute,
				},
			},
		},
	}

	scheduler, err := NewScheduler(cfg, WithNow(func() time.Time { return now }), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	count := scheduler.RunOnce(context.Background())
	if count != 1 {
		t.Fatalf("expected 1 job run, got %d", count)
	}
	jobs := scheduler.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].RetryCount != 1 {
		t.Fatalf("expected retry count 1, got %d", jobs[0].RetryCount)
	}
	expectedNext := now.Add(time.Minute)
	if !jobs[0].NextRun.Equal(expectedNext) {
		t.Fatalf("expected next run %v, got %v", expectedNext, jobs[0].NextRun)
	}
}

func TestSchedulerRunOnce_NoReadyJobs(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "future-job",
				Name:    "webhook",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					// Job scheduled for future
					At: now.Add(time.Hour).Format(time.RFC3339),
				},
				Webhook: &config.CronWebhookConfig{
					URL: "http://example.com",
				},
			},
		},
	}

	scheduler, err := NewScheduler(cfg, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	count := scheduler.RunOnce(context.Background())
	if count != 0 {
		t.Errorf("expected 0 jobs run (not yet ready), got %d", count)
	}
}

func TestScheduler_RunJob_DefaultWebhookTimeout(t *testing.T) {
	originalTimeout := defaultWebhookTimeout
	defaultWebhookTimeout = 50 * time.Millisecond
	defer func() { defaultWebhookTimeout = originalTimeout }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "job-1",
				Name:    "webhook",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					At: now.Format(time.RFC3339),
				},
				Webhook: &config.CronWebhookConfig{
					URL: server.URL,
				},
			},
		},
	}

	scheduler, err := NewScheduler(cfg, WithNow(func() time.Time { return now }), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = scheduler.RunJob(ctx, "job-1")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}
