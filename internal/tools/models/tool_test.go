package models

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/models"
)

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
