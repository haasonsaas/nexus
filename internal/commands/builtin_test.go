package commands

import (
	"context"
	"strings"
	"testing"
)

func requireBuiltins(t *testing.T, r *Registry) {
	t.Helper()
	if err := RegisterBuiltins(r); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"HELLO", "HELLO"},
		{"h", "H"},
		{"system", "System"},
		{"config", "Config"},
	}

	for _, tt := range tests {
		result := titleCase(tt.input)
		if result != tt.expected {
			t.Errorf("titleCase(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestRegisterBuiltins(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	// Verify expected commands are registered
	expectedCommands := []string{
		"help", "status", "new", "model", "stop", "whoami",
		"undo", "memory", "compact", "context", "send", "think",
	}

	for _, name := range expectedCommands {
		if _, found := r.Get(name); !found {
			t.Errorf("builtin command %q not registered", name)
		}
	}

	// Verify aliases work
	aliases := map[string]string{
		"h":                 "help",
		"?":                 "help",
		"commands":          "help",
		"reset":             "new",
		"clear":             "new",
		"abort":             "stop",
		"cancel":            "stop",
		"id":                "whoami",
		"mem":               "memory",
		"summarize":         "compact",
		"ctx":               "context",
		"prompt":            "context",
		"thinking":          "think",
		"extended-thinking": "think",
	}

	for alias, expectedName := range aliases {
		cmd, found := r.Get(alias)
		if !found {
			t.Errorf("alias %q not registered", alias)
			continue
		}
		if cmd.Name != expectedName {
			t.Errorf("alias %q maps to %q, want %q", alias, cmd.Name, expectedName)
		}
	}
}

func TestBuiltinHandlers_Status(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	result, err := r.Execute(context.Background(), &Invocation{Name: "status"})
	if err != nil {
		t.Fatalf("status command failed: %v", err)
	}
	if result.Text == "" {
		t.Error("status returned empty text")
	}
}

func TestBuiltinHandlers_New(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("without model", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "new"})
		if err != nil {
			t.Fatalf("new command failed: %v", err)
		}
		if result.Data["action"] != "new_session" {
			t.Errorf("action = %v, want new_session", result.Data["action"])
		}
	})

	t.Run("with model", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "new", Args: "gpt-4"})
		if err != nil {
			t.Fatalf("new command failed: %v", err)
		}
		if result.Data["model"] != "gpt-4" {
			t.Errorf("model = %v, want gpt-4", result.Data["model"])
		}
	})
}

func TestBuiltinHandlers_Model(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("get model without context", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "model"})
		if err != nil {
			t.Fatalf("model command failed: %v", err)
		}
		if result.Data["action"] != "get_model" {
			t.Errorf("action = %v, want get_model", result.Data["action"])
		}
	})

	t.Run("get model with context", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name:    "model",
			Context: map[string]any{"model": "claude-3"},
		})
		if err != nil {
			t.Fatalf("model command failed: %v", err)
		}
		if !strings.Contains(result.Text, "claude-3") {
			t.Errorf("result text doesn't contain model name: %s", result.Text)
		}
	})

	t.Run("set model", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "model", Args: "gpt-4o"})
		if err != nil {
			t.Fatalf("model command failed: %v", err)
		}
		if result.Data["action"] != "set_model" {
			t.Errorf("action = %v, want set_model", result.Data["action"])
		}
		if result.Data["model"] != "gpt-4o" {
			t.Errorf("model = %v, want gpt-4o", result.Data["model"])
		}
	})
}

func TestBuiltinHandlers_Stop(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("with active run", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name:    "stop",
			Context: map[string]any{"has_active_run": true},
		})
		if err != nil {
			t.Fatalf("stop command failed: %v", err)
		}
		if result.Data["action"] != "abort" {
			t.Errorf("action = %v, want abort", result.Data["action"])
		}
	})

	t.Run("without active run", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name:    "stop",
			Context: map[string]any{"has_active_run": false},
		})
		if err != nil {
			t.Fatalf("stop command failed: %v", err)
		}
		if strings.Contains(result.Text, "No active run") == false {
			t.Errorf("expected 'No active run' message, got: %s", result.Text)
		}
	})
}

func TestBuiltinHandlers_Whoami(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("with context", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name: "whoami",
			Context: map[string]any{
				"channel":    "telegram",
				"channel_id": "123456",
				"user_id":    "user789",
			},
		})
		if err != nil {
			t.Fatalf("whoami command failed: %v", err)
		}
		if !strings.Contains(result.Text, "telegram") {
			t.Error("result doesn't contain channel")
		}
		if !strings.Contains(result.Text, "123456") {
			t.Error("result doesn't contain channel_id")
		}
		if !strings.Contains(result.Text, "user789") {
			t.Error("result doesn't contain user_id")
		}
	})

	t.Run("without context", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "whoami"})
		if err != nil {
			t.Fatalf("whoami command failed: %v", err)
		}
		if !strings.Contains(result.Text, "unavailable") {
			t.Errorf("expected unavailable message, got: %s", result.Text)
		}
	})
}

func TestBuiltinHandlers_Memory(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("without query", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "memory"})
		if err != nil {
			t.Fatalf("memory command failed: %v", err)
		}
		if !strings.Contains(result.Text, "Usage") {
			t.Errorf("expected usage message, got: %s", result.Text)
		}
	})

	t.Run("with query", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "memory", Args: "test query"})
		if err != nil {
			t.Fatalf("memory command failed: %v", err)
		}
		if result.Data["action"] != "memory_search" {
			t.Errorf("action = %v, want memory_search", result.Data["action"])
		}
		if result.Data["query"] != "test query" {
			t.Errorf("query = %v, want 'test query'", result.Data["query"])
		}
	})
}

func TestBuiltinHandlers_Send(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	tests := []struct {
		name           string
		args           string
		expectedPolicy string
	}{
		{"enable on", "on", "on"},
		{"enable yes", "yes", "on"},
		{"enable true", "true", "on"},
		{"disable off", "off", "off"},
		{"disable no", "no", "off"},
		{"disable false", "false", "off"},
		{"inherit reset", "reset", "inherit"},
		{"inherit default", "default", "inherit"},
		{"inherit inherit", "inherit", "inherit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.Execute(context.Background(), &Invocation{Name: "send", Args: tt.args})
			if err != nil {
				t.Fatalf("send command failed: %v", err)
			}
			if result.Data["send_policy"] != tt.expectedPolicy {
				t.Errorf("send_policy = %v, want %v", result.Data["send_policy"], tt.expectedPolicy)
			}
		})
	}

	t.Run("status", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "send"})
		if err != nil {
			t.Fatalf("send command failed: %v", err)
		}
		if !strings.Contains(result.Text, "Usage") {
			t.Errorf("expected usage info, got: %s", result.Text)
		}
	})

	t.Run("invalid policy", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "send", Args: "invalid"})
		if err != nil {
			t.Fatalf("send command failed: %v", err)
		}
		if result.Error == "" {
			t.Error("expected error for invalid policy")
		}
	})
}

func TestBuiltinHandlers_Think(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("enable with default budget", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "think", Args: "on"})
		if err != nil {
			t.Fatalf("think command failed: %v", err)
		}
		// "on" is not valid, it parses budget, so it defaults to 10000
		if result.Data["enabled"] != true {
			t.Error("thinking should be enabled")
		}
	})

	t.Run("enable with custom budget", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "think", Args: "5000"})
		if err != nil {
			t.Fatalf("think command failed: %v", err)
		}
		if result.Data["enabled"] != true {
			t.Error("thinking should be enabled")
		}
		if result.Data["budget"] != 5000 {
			t.Errorf("budget = %v, want 5000", result.Data["budget"])
		}
	})

	t.Run("budget below minimum", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "think", Args: "500"})
		if err != nil {
			t.Fatalf("think command failed: %v", err)
		}
		// Should be clamped to minimum
		if result.Data["budget"] != 1024 {
			t.Errorf("budget = %v, want 1024 (minimum)", result.Data["budget"])
		}
	})

	t.Run("disable", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "think", Args: "off"})
		if err != nil {
			t.Fatalf("think command failed: %v", err)
		}
		if result.Data["enabled"] != false {
			t.Error("thinking should be disabled")
		}
	})

	t.Run("status without context", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "think"})
		if err != nil {
			t.Fatalf("think command failed: %v", err)
		}
		if !strings.Contains(result.Text, "disabled") {
			t.Errorf("expected disabled status, got: %s", result.Text)
		}
	})

	t.Run("status with enabled thinking", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name: "think",
			Context: map[string]any{
				"thinking_enabled": true,
				"thinking_budget":  8000,
			},
		})
		if err != nil {
			t.Fatalf("think command failed: %v", err)
		}
		if !strings.Contains(result.Text, "enabled") {
			t.Errorf("expected enabled status, got: %s", result.Text)
		}
		if !strings.Contains(result.Text, "8000") {
			t.Errorf("expected budget in output, got: %s", result.Text)
		}
	})
}

func TestBuiltinHandlers_Context(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("without context", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "context"})
		if err != nil {
			t.Fatalf("context command failed: %v", err)
		}
		if !strings.Contains(result.Text, "unavailable") {
			t.Errorf("expected unavailable message, got: %s", result.Text)
		}
	})

	t.Run("list mode", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name: "context",
			Args: "list",
			Context: map[string]any{
				"agent_id":       "agent-1",
				"model":          "claude-3",
				"system_prompt":  "You are a helpful assistant.\nBe concise.",
				"memory_enabled": true,
				"tool_count":     10,
			},
		})
		if err != nil {
			t.Fatalf("context command failed: %v", err)
		}
		if !strings.Contains(result.Text, "agent-1") {
			t.Error("missing agent_id")
		}
		if !strings.Contains(result.Text, "claude-3") {
			t.Error("missing model")
		}
		// In list mode, should show summary not full prompt
		if strings.Contains(result.Text, "You are a helpful assistant") {
			t.Error("list mode should not show full prompt")
		}
	})

	t.Run("detail mode", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{
			Name: "context",
			Args: "detail",
			Context: map[string]any{
				"system_prompt": "You are a helpful assistant.",
			},
		})
		if err != nil {
			t.Fatalf("context command failed: %v", err)
		}
		if !strings.Contains(result.Text, "You are a helpful assistant") {
			t.Error("detail mode should show full prompt")
		}
		if !result.Markdown {
			t.Error("detail mode should enable markdown")
		}
	})
}

func TestBuiltinHandlers_Help(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	t.Run("list all commands", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "help"})
		if err != nil {
			t.Fatalf("help command failed: %v", err)
		}
		if !strings.Contains(result.Text, "Available Commands") {
			t.Error("missing header")
		}
		if !result.Markdown {
			t.Error("help should use markdown")
		}
	})

	t.Run("specific command", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "help", Args: "model"})
		if err != nil {
			t.Fatalf("help command failed: %v", err)
		}
		if !strings.Contains(result.Text, "/model") {
			t.Error("missing command name")
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "help", Args: "nonexistent"})
		if err != nil {
			t.Fatalf("help command failed: %v", err)
		}
		if !strings.Contains(result.Text, "Unknown command") {
			t.Error("expected unknown command message")
		}
	})

	t.Run("with slash prefix", func(t *testing.T) {
		result, err := r.Execute(context.Background(), &Invocation{Name: "help", Args: "/model"})
		if err != nil {
			t.Fatalf("help command failed: %v", err)
		}
		if !strings.Contains(result.Text, "/model") {
			t.Error("should strip slash and find command")
		}
	})
}

func TestBuiltinHandlers_Undo(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	result, err := r.Execute(context.Background(), &Invocation{Name: "undo"})
	if err != nil {
		t.Fatalf("undo command failed: %v", err)
	}
	if result.Data["action"] != "undo" {
		t.Errorf("action = %v, want undo", result.Data["action"])
	}
}

func TestBuiltinHandlers_Compact(t *testing.T) {
	r := NewRegistry(nil)
	requireBuiltins(t, r)

	result, err := r.Execute(context.Background(), &Invocation{Name: "compact"})
	if err != nil {
		t.Fatalf("compact command failed: %v", err)
	}
	if result.Data["action"] != "compact" {
		t.Errorf("action = %v, want compact", result.Data["action"])
	}
}
