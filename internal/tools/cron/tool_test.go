package cron

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	croncore "github.com/haasonsaas/nexus/internal/cron"
)

func TestCronToolList(t *testing.T) {
	cfg := config.CronConfig{
		Enabled: true,
		Jobs: []config.CronJobConfig{
			{
				ID:      "job1",
				Name:    "test",
				Type:    "webhook",
				Enabled: true,
				Schedule: config.CronScheduleConfig{
					Every:    time.Hour,
					Timezone: "UTC",
				},
				Webhook: &config.CronWebhookConfig{
					URL: "http://example.com",
				},
			},
		},
	}

	scheduler, err := croncore.NewScheduler(cfg)
	if err != nil {
		t.Fatalf("scheduler: %v", err)
	}
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result.Content, "job1") {
		t.Fatalf("expected job in list: %s", result.Content)
	}
}
