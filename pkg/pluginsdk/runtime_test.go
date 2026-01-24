package pluginsdk

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToolDefinitionStruct(t *testing.T) {
	schema := json.RawMessage(`{"type": "object", "properties": {"input": {"type": "string"}}}`)
	def := ToolDefinition{
		Name:        "my-tool",
		Description: "A tool that does something",
		Schema:      schema,
	}

	if def.Name != "my-tool" {
		t.Errorf("Name = %q", def.Name)
	}
	if def.Description != "A tool that does something" {
		t.Errorf("Description = %q", def.Description)
	}
	if def.Schema == nil {
		t.Error("Schema should not be nil")
	}
}

func TestToolResultStruct(t *testing.T) {
	t.Run("success result", func(t *testing.T) {
		result := ToolResult{
			Content: "Operation completed successfully",
			IsError: false,
		}

		if result.Content != "Operation completed successfully" {
			t.Errorf("Content = %q", result.Content)
		}
		if result.IsError {
			t.Error("IsError should be false")
		}
	})

	t.Run("error result", func(t *testing.T) {
		result := ToolResult{
			Content: "Something went wrong",
			IsError: true,
		}

		if !result.IsError {
			t.Error("IsError should be true")
		}
	})
}

func TestCLICommandStruct(t *testing.T) {
	cmd := CLICommand{
		Use:     "search [query]",
		Short:   "Search for plugins",
		Long:    "Search for plugins in the marketplace using a query string",
		Example: "nexus plugins search telegram",
		Subcommands: []*CLICommand{
			{Use: "advanced", Short: "Advanced search"},
		},
	}

	if cmd.Use != "search [query]" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Short != "Search for plugins" {
		t.Errorf("Short = %q", cmd.Short)
	}
	if len(cmd.Subcommands) != 1 {
		t.Errorf("len(Subcommands) = %d", len(cmd.Subcommands))
	}
}

func TestServiceStruct(t *testing.T) {
	svc := Service{
		ID:          "background-worker",
		Name:        "Background Worker",
		Description: "Processes background tasks",
	}

	if svc.ID != "background-worker" {
		t.Errorf("ID = %q", svc.ID)
	}
	if svc.Name != "Background Worker" {
		t.Errorf("Name = %q", svc.Name)
	}
	if svc.Description != "Processes background tasks" {
		t.Errorf("Description = %q", svc.Description)
	}
}

func TestHookEventStruct(t *testing.T) {
	event := HookEvent{
		Type:      "message.received",
		SessionID: "sess-123",
		ChannelID: "chan-456",
		Data: map[string]any{
			"content": "Hello",
			"user":    "test-user",
		},
	}

	if event.Type != "message.received" {
		t.Errorf("Type = %q", event.Type)
	}
	if event.SessionID != "sess-123" {
		t.Errorf("SessionID = %q", event.SessionID)
	}
	if event.ChannelID != "chan-456" {
		t.Errorf("ChannelID = %q", event.ChannelID)
	}
	if event.Data["content"] != "Hello" {
		t.Errorf("Data[content] = %v", event.Data["content"])
	}
}

func TestHookRegistrationStruct(t *testing.T) {
	reg := HookRegistration{
		EventType: "agent.started",
		Priority:  10,
		Name:      "my-hook",
	}

	if reg.EventType != "agent.started" {
		t.Errorf("EventType = %q", reg.EventType)
	}
	if reg.Priority != 10 {
		t.Errorf("Priority = %d", reg.Priority)
	}
	if reg.Name != "my-hook" {
		t.Errorf("Name = %q", reg.Name)
	}
}

func TestPluginAPIStruct(t *testing.T) {
	api := PluginAPI{
		Config: map[string]any{
			"token": "secret",
		},
		ResolvePath: func(path string) string {
			return "/workspace/" + path
		},
	}

	if api.Config["token"] != "secret" {
		t.Errorf("Config[token] = %v", api.Config["token"])
	}
	if api.ResolvePath("file.txt") != "/workspace/file.txt" {
		t.Errorf("ResolvePath = %q", api.ResolvePath("file.txt"))
	}
}

func TestStatusStruct(t *testing.T) {
	status := Status{
		Connected: true,
		Error:     "",
		LastPing:  time.Now().Unix(),
	}

	if !status.Connected {
		t.Error("Connected should be true")
	}
	if status.Error != "" {
		t.Errorf("Error = %q", status.Error)
	}
	if status.LastPing == 0 {
		t.Error("LastPing should be set")
	}
}

func TestHealthStatusStruct(t *testing.T) {
	status := HealthStatus{
		Healthy:   true,
		Latency:   50 * time.Millisecond,
		Message:   "All systems operational",
		LastCheck: time.Now(),
		Degraded:  false,
	}

	if !status.Healthy {
		t.Error("Healthy should be true")
	}
	if status.Latency != 50*time.Millisecond {
		t.Errorf("Latency = %v", status.Latency)
	}
	if status.Degraded {
		t.Error("Degraded should be false")
	}
}

func TestStatus_Error(t *testing.T) {
	status := Status{
		Connected: false,
		Error:     "Connection refused",
		LastPing:  0,
	}

	if status.Connected {
		t.Error("Connected should be false")
	}
	if status.Error != "Connection refused" {
		t.Errorf("Error = %q", status.Error)
	}
}

func TestHealthStatus_Degraded(t *testing.T) {
	status := HealthStatus{
		Healthy:   true,
		Latency:   500 * time.Millisecond,
		Message:   "High latency detected",
		LastCheck: time.Now(),
		Degraded:  true,
	}

	if !status.Healthy {
		t.Error("Healthy should be true")
	}
	if !status.Degraded {
		t.Error("Degraded should be true")
	}
}
