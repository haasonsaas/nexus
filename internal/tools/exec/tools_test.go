package exec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExecToolRunsCommand(t *testing.T) {
	mgr := NewManager(t.TempDir())
	tool := NewExecTool("exec", mgr)
	params, _ := json.Marshal(map[string]interface{}{
		"command": "echo hello",
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("expected stdout in result: %s", result.Content)
	}
}

func TestProcessToolLifecycle(t *testing.T) {
	mgr := NewManager(t.TempDir())
	execTool := NewExecTool("exec", mgr)
	procTool := NewProcessTool(mgr)

	params, _ := json.Marshal(map[string]interface{}{
		"command":    "echo background",
		"background": true,
	})
	result, err := execTool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}

	var payload struct {
		ProcessID string `json:"process_id"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if payload.ProcessID == "" {
		t.Fatalf("expected process_id")
	}

	time.Sleep(50 * time.Millisecond)
	statusParams, _ := json.Marshal(map[string]interface{}{
		"action":     "status",
		"process_id": payload.ProcessID,
	})
	statusResult, err := procTool.Execute(context.Background(), statusParams)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if statusResult.IsError {
		t.Fatalf("expected status success: %s", statusResult.Content)
	}

	removeParams, _ := json.Marshal(map[string]interface{}{
		"action":     "remove",
		"process_id": payload.ProcessID,
	})
	removeResult, err := procTool.Execute(context.Background(), removeParams)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removeResult.IsError {
		t.Fatalf("expected remove success: %s", removeResult.Content)
	}
}
