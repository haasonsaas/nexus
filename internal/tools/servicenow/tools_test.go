package servicenow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func testClient() *Client {
	return NewClient(Config{
		InstanceURL: "https://test.service-now.com",
		Username:    "testuser",
		Password:    "testpass",
	})
}

func TestListTicketsTool(t *testing.T) {
	client := testClient()
	tool := NewListTicketsTool(client)

	t.Run("Name", func(t *testing.T) {
		if name := tool.Name(); name != "servicenow_list_tickets" {
			t.Errorf("Name() = %q, want %q", name, "servicenow_list_tickets")
		}
	})

	t.Run("Description", func(t *testing.T) {
		desc := tool.Description()
		if !strings.Contains(desc, "ServiceNow") {
			t.Errorf("Description() should mention ServiceNow, got %q", desc)
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("Schema() is not valid JSON: %v", err)
		}
		if parsed["type"] != "object" {
			t.Errorf("schema type = %v, want object", parsed["type"])
		}
		props, ok := parsed["properties"].(map[string]any)
		if !ok {
			t.Fatal("schema properties not found")
		}
		// Check expected properties exist
		expectedProps := []string{"state", "priority", "assigned_to_me", "limit"}
		for _, prop := range expectedProps {
			if _, exists := props[prop]; !exists {
				t.Errorf("schema missing property %q", prop)
			}
		}
	})
}

func TestGetTicketTool(t *testing.T) {
	client := testClient()
	tool := NewGetTicketTool(client)

	t.Run("Name", func(t *testing.T) {
		if name := tool.Name(); name != "servicenow_get_ticket" {
			t.Errorf("Name() = %q, want %q", name, "servicenow_get_ticket")
		}
	})

	t.Run("Description", func(t *testing.T) {
		desc := tool.Description()
		if !strings.Contains(desc, "INC") {
			t.Errorf("Description() should mention INC ticket format, got %q", desc)
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("Schema() is not valid JSON: %v", err)
		}
		// Check required field
		required, ok := parsed["required"].([]any)
		if !ok {
			t.Fatal("schema required field not found")
		}
		foundTicketNumber := false
		for _, r := range required {
			if r == "ticket_number" {
				foundTicketNumber = true
				break
			}
		}
		if !foundTicketNumber {
			t.Error("ticket_number should be required")
		}
	})

	t.Run("Execute missing ticket_number", func(t *testing.T) {
		params := json.RawMessage(`{}`)
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result == nil {
			t.Fatal("result is nil")
		}
		if !result.IsError {
			t.Error("expected IsError for missing ticket_number")
		}
		if !strings.Contains(result.Content, "required") {
			t.Errorf("Content should mention required, got %q", result.Content)
		}
	})
}

func TestAddCommentTool(t *testing.T) {
	client := testClient()
	tool := NewAddCommentTool(client)

	t.Run("Name", func(t *testing.T) {
		if name := tool.Name(); name != "servicenow_add_comment" {
			t.Errorf("Name() = %q, want %q", name, "servicenow_add_comment")
		}
	})

	t.Run("Description", func(t *testing.T) {
		desc := tool.Description()
		if !strings.Contains(desc, "comment") {
			t.Errorf("Description() should mention comment, got %q", desc)
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("Schema() is not valid JSON: %v", err)
		}
		required, ok := parsed["required"].([]any)
		if !ok {
			t.Fatal("schema required field not found")
		}
		if len(required) != 2 {
			t.Errorf("expected 2 required fields, got %d", len(required))
		}
	})

	t.Run("Execute missing ticket_number", func(t *testing.T) {
		params := json.RawMessage(`{"comment": "test comment"}`)
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError for missing ticket_number")
		}
	})

	t.Run("Execute missing comment", func(t *testing.T) {
		params := json.RawMessage(`{"ticket_number": "INC0012345"}`)
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError for missing comment")
		}
	})
}

func TestResolveTicketTool(t *testing.T) {
	client := testClient()
	tool := NewResolveTicketTool(client)

	t.Run("Name", func(t *testing.T) {
		if name := tool.Name(); name != "servicenow_resolve_ticket" {
			t.Errorf("Name() = %q, want %q", name, "servicenow_resolve_ticket")
		}
	})

	t.Run("Description", func(t *testing.T) {
		desc := tool.Description()
		if !strings.Contains(desc, "Resolve") {
			t.Errorf("Description() should mention Resolve, got %q", desc)
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("Schema() is not valid JSON: %v", err)
		}
		props, ok := parsed["properties"].(map[string]any)
		if !ok {
			t.Fatal("schema properties not found")
		}
		if _, exists := props["close_code"]; !exists {
			t.Error("schema missing close_code property")
		}
	})

	t.Run("Execute missing ticket_number", func(t *testing.T) {
		params := json.RawMessage(`{"resolution": "Fixed the issue"}`)
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError for missing ticket_number")
		}
	})

	t.Run("Execute missing resolution", func(t *testing.T) {
		params := json.RawMessage(`{"ticket_number": "INC0012345"}`)
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError for missing resolution")
		}
	})
}

func TestUpdateTicketTool(t *testing.T) {
	client := testClient()
	tool := NewUpdateTicketTool(client)

	t.Run("Name", func(t *testing.T) {
		if name := tool.Name(); name != "servicenow_update_ticket" {
			t.Errorf("Name() = %q, want %q", name, "servicenow_update_ticket")
		}
	})

	t.Run("Description", func(t *testing.T) {
		desc := tool.Description()
		if !strings.Contains(desc, "Update") {
			t.Errorf("Description() should mention Update, got %q", desc)
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		var parsed map[string]any
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatalf("Schema() is not valid JSON: %v", err)
		}
		props, ok := parsed["properties"].(map[string]any)
		if !ok {
			t.Fatal("schema properties not found")
		}
		expectedProps := []string{"ticket_number", "state", "priority", "assigned_to", "assignment_group"}
		for _, prop := range expectedProps {
			if _, exists := props[prop]; !exists {
				t.Errorf("schema missing property %q", prop)
			}
		}
	})

	t.Run("Execute missing ticket_number", func(t *testing.T) {
		params := json.RawMessage(`{"state": "in_progress"}`)
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError for missing ticket_number")
		}
	})
}

func TestNewToolConstructors(t *testing.T) {
	client := testClient()

	t.Run("NewListTicketsTool", func(t *testing.T) {
		tool := NewListTicketsTool(client)
		if tool == nil {
			t.Error("NewListTicketsTool returned nil")
		}
		if tool.client != client {
			t.Error("client not set correctly")
		}
	})

	t.Run("NewGetTicketTool", func(t *testing.T) {
		tool := NewGetTicketTool(client)
		if tool == nil {
			t.Error("NewGetTicketTool returned nil")
		}
		if tool.client != client {
			t.Error("client not set correctly")
		}
	})

	t.Run("NewAddCommentTool", func(t *testing.T) {
		tool := NewAddCommentTool(client)
		if tool == nil {
			t.Error("NewAddCommentTool returned nil")
		}
		if tool.client != client {
			t.Error("client not set correctly")
		}
	})

	t.Run("NewResolveTicketTool", func(t *testing.T) {
		tool := NewResolveTicketTool(client)
		if tool == nil {
			t.Error("NewResolveTicketTool returned nil")
		}
		if tool.client != client {
			t.Error("client not set correctly")
		}
	})

	t.Run("NewUpdateTicketTool", func(t *testing.T) {
		tool := NewUpdateTicketTool(client)
		if tool == nil {
			t.Error("NewUpdateTicketTool returned nil")
		}
		if tool.client != client {
			t.Error("client not set correctly")
		}
	})
}

func TestExecute_InvalidJSON(t *testing.T) {
	client := testClient()

	tools := []struct {
		name string
		exec func(context.Context, json.RawMessage) (interface{}, error)
	}{
		{"ListTicketsTool", func(ctx context.Context, p json.RawMessage) (interface{}, error) {
			return NewListTicketsTool(client).Execute(ctx, p)
		}},
		{"GetTicketTool", func(ctx context.Context, p json.RawMessage) (interface{}, error) {
			return NewGetTicketTool(client).Execute(ctx, p)
		}},
		{"AddCommentTool", func(ctx context.Context, p json.RawMessage) (interface{}, error) {
			return NewAddCommentTool(client).Execute(ctx, p)
		}},
		{"ResolveTicketTool", func(ctx context.Context, p json.RawMessage) (interface{}, error) {
			return NewResolveTicketTool(client).Execute(ctx, p)
		}},
		{"UpdateTicketTool", func(ctx context.Context, p json.RawMessage) (interface{}, error) {
			return NewUpdateTicketTool(client).Execute(ctx, p)
		}},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			params := json.RawMessage(`{invalid json}`)
			_, err := tt.exec(context.Background(), params)
			if err == nil {
				t.Error("expected error for invalid JSON")
			}
			if !strings.Contains(err.Error(), "parse params") {
				t.Errorf("error should mention parse params, got %v", err)
			}
		})
	}
}
