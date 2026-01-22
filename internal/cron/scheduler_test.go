package cron

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
)

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
