package facts

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExtractToolExecute(t *testing.T) {
	tool := NewExtractTool(10)
	params := map[string]any{
		"text": "Email me at alex@example.com or visit https://example.com. Call +1 (555) 123-4567.",
	}
	raw, _ := json.Marshal(params)

	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	var payload struct {
		Facts []Fact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if len(payload.Facts) != 3 {
		t.Fatalf("expected 3 facts, got %d", len(payload.Facts))
	}
}

func TestExtractToolMaxFacts(t *testing.T) {
	tool := NewExtractTool(1)
	params := map[string]any{
		"text":      "a@example.com b@example.com",
		"max_facts": 1,
	}
	raw, _ := json.Marshal(params)

	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	var payload struct {
		Facts []Fact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if len(payload.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(payload.Facts))
	}
}
