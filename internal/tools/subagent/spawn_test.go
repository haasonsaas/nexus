package subagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewManager(t *testing.T) {
	t.Run("with positive maxActive", func(t *testing.T) {
		m := NewManager(nil, 10)
		if m == nil {
			t.Fatal("expected non-nil manager")
		}
		if m.maxActive != 10 {
			t.Errorf("maxActive = %d, want %d", m.maxActive, 10)
		}
	})

	t.Run("with zero maxActive defaults to 5", func(t *testing.T) {
		m := NewManager(nil, 0)
		if m.maxActive != 5 {
			t.Errorf("maxActive = %d, want %d", m.maxActive, 5)
		}
	})

	t.Run("with negative maxActive defaults to 5", func(t *testing.T) {
		m := NewManager(nil, -1)
		if m.maxActive != 5 {
			t.Errorf("maxActive = %d, want %d", m.maxActive, 5)
		}
	})
}

func TestManager_SetAnnouncer(t *testing.T) {
	m := NewManager(nil, 5)
	m.SetAnnouncer(func(ctx context.Context, parentSession, msg string) error {
		return nil
	})

	if m.announcer == nil {
		t.Error("announcer should be set")
	}
}

func TestManager_Get(t *testing.T) {
	m := NewManager(nil, 5)

	t.Run("returns false for nonexistent agent", func(t *testing.T) {
		_, ok := m.Get("nonexistent")
		if ok {
			t.Error("expected false for nonexistent agent")
		}
	})

	t.Run("returns agent when exists", func(t *testing.T) {
		m.agents["test-id"] = &SubAgent{ID: "test-id", Name: "Test"}
		sa, ok := m.Get("test-id")
		if !ok {
			t.Error("expected true for existing agent")
		}
		if sa.Name != "Test" {
			t.Errorf("Name = %q, want %q", sa.Name, "Test")
		}
	})
}

func TestManager_List(t *testing.T) {
	m := NewManager(nil, 5)
	m.agents["a1"] = &SubAgent{ID: "a1", ParentID: "parent-1"}
	m.agents["a2"] = &SubAgent{ID: "a2", ParentID: "parent-1"}
	m.agents["a3"] = &SubAgent{ID: "a3", ParentID: "parent-2"}

	t.Run("filters by parent ID", func(t *testing.T) {
		list := m.List("parent-1")
		if len(list) != 2 {
			t.Errorf("expected 2 agents for parent-1, got %d", len(list))
		}
	})

	t.Run("returns empty for unknown parent", func(t *testing.T) {
		list := m.List("unknown")
		if len(list) != 0 {
			t.Errorf("expected 0 agents for unknown parent, got %d", len(list))
		}
	})
}

func TestManager_Cancel(t *testing.T) {
	m := NewManager(nil, 5)

	t.Run("returns error for nonexistent agent", func(t *testing.T) {
		err := m.Cancel("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent agent")
		}
	})

	t.Run("returns error for non-running agent", func(t *testing.T) {
		m.agents["completed"] = &SubAgent{ID: "completed", Status: "completed"}
		err := m.Cancel("completed")
		if err == nil {
			t.Error("expected error for completed agent")
		}
	})

	t.Run("cancels running agent", func(t *testing.T) {
		m.agents["running"] = &SubAgent{ID: "running", Status: "running"}
		err := m.Cancel("running")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		sa := m.agents["running"]
		if sa.Status != "cancelled" {
			t.Errorf("Status = %q, want %q", sa.Status, "cancelled")
		}
		if sa.CompletedAt.IsZero() {
			t.Error("CompletedAt should be set")
		}
		if sa.Error != "cancelled by user" {
			t.Errorf("Error = %q, want %q", sa.Error, "cancelled by user")
		}
	})
}

func TestManager_ActiveCount(t *testing.T) {
	m := NewManager(nil, 5)
	if m.ActiveCount() != 0 {
		t.Errorf("ActiveCount() = %d, want 0", m.ActiveCount())
	}

	m.activeCount = 3
	if m.ActiveCount() != 3 {
		t.Errorf("ActiveCount() = %d, want 3", m.ActiveCount())
	}
}

func TestManager_completeSubAgent(t *testing.T) {
	m := NewManager(nil, 5)

	t.Run("ignores nonexistent agent", func(t *testing.T) {
		// Should not panic
		m.completeSubAgent("nonexistent", "result", "")
	})

	t.Run("marks agent as completed", func(t *testing.T) {
		m.agents["test"] = &SubAgent{ID: "test", Status: "running"}
		m.completeSubAgent("test", "success result", "")

		sa := m.agents["test"]
		if sa.Status != "completed" {
			t.Errorf("Status = %q, want %q", sa.Status, "completed")
		}
		if sa.Result != "success result" {
			t.Errorf("Result = %q, want %q", sa.Result, "success result")
		}
		if !sa.CompletedAt.After(sa.CreatedAt) || sa.CompletedAt.IsZero() {
			t.Error("CompletedAt should be set")
		}
	})

	t.Run("marks agent as failed", func(t *testing.T) {
		m.agents["test2"] = &SubAgent{ID: "test2", Status: "running"}
		m.completeSubAgent("test2", "", "error message")

		sa := m.agents["test2"]
		if sa.Status != "failed" {
			t.Errorf("Status = %q, want %q", sa.Status, "failed")
		}
		if sa.Error != "error message" {
			t.Errorf("Error = %q, want %q", sa.Error, "error message")
		}
	})
}

func TestSpawnTool(t *testing.T) {
	m := NewManager(nil, 5)
	tool := NewSpawnTool(m)

	t.Run("Name", func(t *testing.T) {
		if tool.Name() != "spawn_subagent" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "spawn_subagent")
		}
	})

	t.Run("Description", func(t *testing.T) {
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		if schema == nil {
			t.Error("Schema() should not be nil")
		}
		if schema["type"] != "object" {
			t.Errorf("Schema type = %v, want object", schema["type"])
		}
	})

	t.Run("Execute returns error for empty name", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`{"name":"","task":"test"}`))
		if err == nil {
			t.Error("expected error for empty name")
		}
	})

	t.Run("Execute returns error for empty task", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`{"name":"test","task":""}`))
		if err == nil {
			t.Error("expected error for empty task")
		}
	})

	t.Run("Execute returns error for invalid JSON", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`invalid json`))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestStatusTool(t *testing.T) {
	m := NewManager(nil, 5)
	tool := NewStatusTool(m)

	t.Run("Name", func(t *testing.T) {
		if tool.Name() != "subagent_status" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "subagent_status")
		}
	})

	t.Run("Description", func(t *testing.T) {
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		if schema == nil {
			t.Error("Schema() should not be nil")
		}
	})

	t.Run("Execute returns error for nonexistent agent", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`{"id":"nonexistent"}`))
		if err == nil {
			t.Error("expected error for nonexistent agent")
		}
	})

	t.Run("Execute returns agent status", func(t *testing.T) {
		m.agents["test-agent"] = &SubAgent{
			ID:     "test-agent",
			Name:   "Test",
			Status: "running",
			Task:   "Test task",
		}

		result, err := tool.Execute(context.Background(), []byte(`{"id":"test-agent"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == "" {
			t.Error("expected non-empty result")
		}
	})

	t.Run("Execute returns completed agent with result", func(t *testing.T) {
		m.agents["completed-agent"] = &SubAgent{
			ID:     "completed-agent",
			Name:   "Completed",
			Status: "completed",
			Task:   "Test task",
			Result: "Done successfully",
		}

		result, err := tool.Execute(context.Background(), []byte(`{"id":"completed-agent"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !containsStr(result, "Done successfully") {
			t.Errorf("result should contain Result, got: %s", result)
		}
	})

	t.Run("Execute returns failed agent with error", func(t *testing.T) {
		m.agents["failed-agent"] = &SubAgent{
			ID:     "failed-agent",
			Name:   "Failed",
			Status: "failed",
			Task:   "Test task",
			Error:  "Something went wrong",
		}

		result, err := tool.Execute(context.Background(), []byte(`{"id":"failed-agent"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !containsStr(result, "Something went wrong") {
			t.Errorf("result should contain Error, got: %s", result)
		}
	})

	t.Run("Execute lists agents when no ID", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == "" {
			t.Error("expected non-empty result")
		}
	})

	t.Run("Execute returns error for invalid JSON", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`invalid`))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestCancelTool(t *testing.T) {
	m := NewManager(nil, 5)
	tool := NewCancelTool(m)

	t.Run("Name", func(t *testing.T) {
		if tool.Name() != "subagent_cancel" {
			t.Errorf("Name() = %q, want %q", tool.Name(), "subagent_cancel")
		}
	})

	t.Run("Description", func(t *testing.T) {
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("Schema", func(t *testing.T) {
		schema := tool.Schema()
		if schema == nil {
			t.Error("Schema() should not be nil")
		}
	})

	t.Run("Execute returns error for empty id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`{"id":""}`))
		if err == nil {
			t.Error("expected error for empty id")
		}
	})

	t.Run("Execute returns error for nonexistent agent", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`{"id":"nonexistent"}`))
		if err == nil {
			t.Error("expected error for nonexistent agent")
		}
	})

	t.Run("Execute cancels running agent", func(t *testing.T) {
		m.agents["to-cancel"] = &SubAgent{ID: "to-cancel", Status: "running"}

		result, err := tool.Execute(context.Background(), []byte(`{"id":"to-cancel"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !containsStr(result, "cancelled") {
			t.Errorf("result should mention cancelled, got: %s", result)
		}
	})

	t.Run("Execute returns error for invalid JSON", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), []byte(`invalid`))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"long string truncated", "hello world", 8, "hello..."},
		{"empty string", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestSubAgentJSON(t *testing.T) {
	sa := &SubAgent{
		ID:       "test-id",
		ParentID: "parent-id",
		Name:     "Test Agent",
		Task:     "Do something",
		Status:   "running",
	}

	data, err := json.Marshal(sa)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded SubAgent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != sa.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, sa.ID)
	}
	if decoded.Name != sa.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, sa.Name)
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
