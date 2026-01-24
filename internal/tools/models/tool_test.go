package models

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/models"
)

func TestNewTool(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)
	if tool == nil {
		t.Error("expected non-nil tool")
	}
	if tool.catalog != catalog {
		t.Error("catalog not set")
	}
}

func TestTool_Name(t *testing.T) {
	tool := NewTool(nil, nil)
	if tool.Name() != "models" {
		t.Errorf("expected 'models', got %q", tool.Name())
	}
}

func TestTool_Description(t *testing.T) {
	tool := NewTool(nil, nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestTool_Schema(t *testing.T) {
	tool := NewTool(nil, nil)
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
}

func TestTool_Execute_NilCatalog(t *testing.T) {
	tool := NewTool(nil, nil)
	params, _ := json.Marshal(map[string]interface{}{"action": "list"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nil catalog")
	}
	if !strings.Contains(result.Content, "unavailable") {
		t.Errorf("expected 'unavailable' in error: %s", result.Content)
	}
}

func TestTool_Execute_InvalidParams(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestTool_Execute_EmptyAction(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)
	params, _ := json.Marshal(map[string]interface{}{"action": ""})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty action")
	}
	if !strings.Contains(result.Content, "required") {
		t.Errorf("expected 'required' in error: %s", result.Content)
	}
}

func TestModelsToolList(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)

	params, _ := json.Marshal(map[string]interface{}{"action": "list"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "\"models\"") {
		t.Fatalf("expected models field, got %s", result.Content)
	}
}

func TestModelsToolList_WithFilters(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)

	params, _ := json.Marshal(map[string]interface{}{
		"action":             "list",
		"provider":           "anthropic",
		"capability":         "vision",
		"tier":               "flagship",
		"include_deprecated": true,
	})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "models") {
		t.Errorf("expected 'models' in result: %s", result.Content)
	}
}

func TestModelsToolProviders(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)

	params, _ := json.Marshal(map[string]interface{}{"action": "providers"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "providers") {
		t.Errorf("expected 'providers' in result: %s", result.Content)
	}
}

func TestModelsToolRefresh_NoBedrock(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)

	params, _ := json.Marshal(map[string]interface{}{"action": "refresh"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when bedrock not configured")
	}
	if !strings.Contains(result.Content, "not configured") {
		t.Errorf("expected 'not configured' in error: %s", result.Content)
	}
}

func TestModelsToolUnsupportedAction(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)

	params, _ := json.Marshal(map[string]interface{}{"action": "invalid"})
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

func TestModelsToolActionCaseInsensitive(t *testing.T) {
	catalog := models.NewCatalog()
	tool := NewTool(catalog, nil)

	for _, action := range []string{"LIST", "List", "PROVIDERS", "Providers"} {
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
