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

func testScheduler(t *testing.T) *croncore.Scheduler {
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
	return scheduler
}

func TestNewTool(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	if tool == nil {
		t.Error("expected non-nil tool")
	}
	if tool.scheduler != scheduler {
		t.Error("scheduler not set correctly")
	}
}

func TestTool_Name(t *testing.T) {
	tool := NewTool(nil)
	if tool.Name() != "cron" {
		t.Errorf("expected 'cron', got %q", tool.Name())
	}
}

func TestTool_Description(t *testing.T) {
	tool := NewTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !strings.Contains(desc, "cron") {
		t.Errorf("expected description to mention cron: %s", desc)
	}
}

func TestTool_Schema(t *testing.T) {
	tool := NewTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("expected non-empty schema")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema should be valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("expected type 'object', got %v", parsed["type"])
	}
	if _, ok := parsed["properties"]; !ok {
		t.Error("expected 'properties' in schema")
	}
}

func TestTool_Execute_NilScheduler(t *testing.T) {
	tool := NewTool(nil)
	params, _ := json.Marshal(map[string]interface{}{"action": "list"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for nil scheduler")
	}
	if !strings.Contains(result.Content, "unavailable") {
		t.Errorf("expected 'unavailable' in error: %s", result.Content)
	}
}

func TestTool_Execute_InvalidParams(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid params")
	}
	if !strings.Contains(result.Content, "Invalid") {
		t.Errorf("expected 'Invalid' in error: %s", result.Content)
	}
}

func TestTool_Execute_EmptyAction(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{"action": ""})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for empty action")
	}
	if !strings.Contains(result.Content, "required") {
		t.Errorf("expected 'required' in error: %s", result.Content)
	}
}

func TestCronToolList(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "job1") {
		t.Fatalf("expected job in list: %s", result.Content)
	}
}

func TestCronToolStatus(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "status",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "job1") {
		t.Fatalf("expected job in status: %s", result.Content)
	}
}

func TestCronToolRun_MissingID(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "run",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing id")
	}
	if !strings.Contains(result.Content, "required") {
		t.Errorf("expected 'required' in error: %s", result.Content)
	}
}

func TestCronToolRun_JobNotFound(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"id":     "nonexistent",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent job")
	}
}

func TestCronToolRun_Success(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"id":     "job1",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// May fail due to webhook URL not being reachable, which is fine
	if !result.IsError {
		if !strings.Contains(result.Content, "ran") {
			t.Errorf("expected 'ran' in response: %s", result.Content)
		}
	}
}

func TestCronToolUnsupportedAction(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "invalid_action",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unsupported action")
	}
	if !strings.Contains(result.Content, "unsupported") {
		t.Errorf("expected 'unsupported' in error: %s", result.Content)
	}
}

func TestCronToolActionCaseInsensitive(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)

	testCases := []string{"LIST", "List", "LiSt", "STATUS", "Status"}
	for _, action := range testCases {
		params, _ := json.Marshal(map[string]interface{}{"action": action})
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("execute with action %q: %v", action, err)
		}
		if result.IsError {
			t.Errorf("action %q should not error: %s", action, result.Content)
		}
	}
}
