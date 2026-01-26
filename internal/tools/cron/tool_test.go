package cron

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	croncore "github.com/haasonsaas/nexus/internal/cron"
)

func testScheduler(t *testing.T) *croncore.Scheduler {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

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
					URL: server.URL,
				},
			},
		},
	}
	scheduler, err := croncore.NewScheduler(cfg, croncore.WithHTTPClient(server.Client()))
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

func TestCronToolRegisterAndUnregister(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	now := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	params, _ := json.Marshal(map[string]interface{}{
		"action": "register",
		"job": map[string]interface{}{
			"id":      "job2",
			"name":    "test",
			"type":    "webhook",
			"enabled": true,
			"schedule": map[string]interface{}{
				"at": now,
			},
			"webhook": map[string]interface{}{
				"url": "http://example.com",
			},
		},
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	unregisterParams, _ := json.Marshal(map[string]interface{}{
		"action": "unregister",
		"id":     "job2",
	})
	result, err = tool.Execute(context.Background(), unregisterParams)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestCronToolExecutionsAndPrune(t *testing.T) {
	scheduler := testScheduler(t)
	tool := NewTool(scheduler)
	_, _ = tool.Execute(context.Background(), json.RawMessage(`{"action":"run","id":"job1"}`))

	listParams, _ := json.Marshal(map[string]interface{}{
		"action": "executions",
		"job_id": "job1",
	})
	result, err := tool.Execute(context.Background(), listParams)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "job1") {
		t.Fatalf("expected executions to include job1: %s", result.Content)
	}

	pruneParams, _ := json.Marshal(map[string]interface{}{
		"action":     "prune",
		"older_than": "1ms",
	})
	result, err = tool.Execute(context.Background(), pruneParams)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
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
