package multiagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// mustNewOrchestrator is a test helper that creates an orchestrator and fails the test on error.
func mustNewOrchestrator(t *testing.T, config *MultiAgentConfig, provider agent.LLMProvider, store sessions.Store) *Orchestrator {
	t.Helper()
	orch, err := NewOrchestrator(config, provider, store)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	return orch
}

func TestNewOrchestrator(t *testing.T) {
	tests := []struct {
		name         string
		config       *MultiAgentConfig
		wantDefaults bool
	}{
		{
			name:         "nil config uses defaults",
			config:       nil,
			wantDefaults: true,
		},
		{
			name: "custom config is preserved",
			config: &MultiAgentConfig{
				DefaultAgentID:     "custom-agent",
				MaxHandoffDepth:    5,
				HandoffTimeout:     2 * time.Minute,
				EnablePeerHandoffs: false,
			},
			wantDefaults: false,
		},
		{
			name: "config with supervisor",
			config: &MultiAgentConfig{
				DefaultAgentID:    "default",
				SupervisorAgentID: "supervisor",
				Agents: []AgentDefinition{
					{ID: "default", Name: "Default"},
					{ID: "supervisor", Name: "Supervisor"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch, err := NewOrchestrator(tt.config, nil, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if orch == nil {
				t.Fatal("expected orchestrator to be created")
			}

			if orch.agents == nil {
				t.Error("expected agents map to be initialized")
			}

			if orch.runtimes == nil {
				t.Error("expected runtimes map to be initialized")
			}

			if orch.contextManager == nil {
				t.Error("expected context manager to be initialized")
			}

			if orch.router == nil {
				t.Error("expected router to be initialized")
			}

			if orch.handoffTool == nil {
				t.Error("expected handoff tool to be initialized")
			}

			if tt.wantDefaults {
				if orch.config.MaxHandoffDepth != 10 {
					t.Errorf("expected default MaxHandoffDepth=10, got %d", orch.config.MaxHandoffDepth)
				}
				if orch.config.HandoffTimeout != 5*time.Minute {
					t.Errorf("expected default HandoffTimeout=5m, got %v", orch.config.HandoffTimeout)
				}
				if !orch.config.EnablePeerHandoffs {
					t.Error("expected default EnablePeerHandoffs=true")
				}
				if orch.config.DefaultContextMode != ContextFull {
					t.Errorf("expected default ContextMode=full, got %s", orch.config.DefaultContextMode)
				}
			}

			if tt.config != nil && tt.config.SupervisorAgentID != "" {
				if orch.supervisor == nil {
					t.Error("expected supervisor to be initialized when SupervisorAgentID is set")
				}
			}
		})
	}
}

func TestOrchestrator_RegisterAgent(t *testing.T) {
	tests := []struct {
		name    string
		agent   *AgentDefinition
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid agent",
			agent: &AgentDefinition{
				ID:          "test-agent",
				Name:        "Test Agent",
				Description: "A test agent",
			},
			wantErr: false,
		},
		{
			name:    "nil agent returns error",
			agent:   nil,
			wantErr: true,
			errMsg:  "agent definition cannot be nil",
		},
		{
			name: "empty ID returns error",
			agent: &AgentDefinition{
				ID:   "",
				Name: "No ID Agent",
			},
			wantErr: true,
			errMsg:  "agent ID cannot be empty",
		},
		{
			name: "agent with system prompt",
			agent: &AgentDefinition{
				ID:           "prompt-agent",
				Name:         "Prompt Agent",
				SystemPrompt: "You are a helpful assistant",
			},
			wantErr: false,
		},
		{
			name: "agent with model specified",
			agent: &AgentDefinition{
				ID:    "model-agent",
				Name:  "Model Agent",
				Model: "claude-3-opus",
			},
			wantErr: false,
		},
		{
			name: "agent with max iterations",
			agent: &AgentDefinition{
				ID:            "iter-agent",
				Name:          "Iter Agent",
				MaxIterations: 5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := mustNewOrchestrator(t, nil, nil, nil)

			err := orch.RegisterAgent(tt.agent)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("expected error %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify agent is registered
			agent, ok := orch.GetAgent(tt.agent.ID)
			if !ok {
				t.Fatal("expected agent to be retrievable")
			}

			if agent.ID != tt.agent.ID {
				t.Errorf("expected ID %s, got %s", tt.agent.ID, agent.ID)
			}

			// Verify runtime is created
			runtime, ok := orch.GetRuntime(tt.agent.ID)
			if !ok {
				t.Fatal("expected runtime to be created")
			}
			if runtime == nil {
				t.Error("expected runtime to not be nil")
			}
		})
	}
}

func TestOrchestrator_GetAgent(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	agent := &AgentDefinition{
		ID:          "get-test",
		Name:        "Get Test Agent",
		Description: "Test agent for GetAgent",
	}
	_ = orch.RegisterAgent(agent)

	t.Run("get existing agent", func(t *testing.T) {
		got, ok := orch.GetAgent("get-test")
		if !ok {
			t.Error("expected agent to be found")
		}
		if got.Name != "Get Test Agent" {
			t.Errorf("expected name %q, got %q", "Get Test Agent", got.Name)
		}
	})

	t.Run("get non-existent agent", func(t *testing.T) {
		_, ok := orch.GetAgent("non-existent")
		if ok {
			t.Error("expected agent to not be found")
		}
	})
}

func TestOrchestrator_GetRuntime(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	agent := &AgentDefinition{
		ID:   "runtime-test",
		Name: "Runtime Test Agent",
	}
	_ = orch.RegisterAgent(agent)

	t.Run("get existing runtime", func(t *testing.T) {
		runtime, ok := orch.GetRuntime("runtime-test")
		if !ok {
			t.Error("expected runtime to be found")
		}
		if runtime == nil {
			t.Error("expected runtime to not be nil")
		}
	})

	t.Run("get non-existent runtime", func(t *testing.T) {
		_, ok := orch.GetRuntime("non-existent")
		if ok {
			t.Error("expected runtime to not be found")
		}
	})
}

func TestOrchestrator_ListAgents(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	agents := []*AgentDefinition{
		{ID: "agent-1", Name: "Agent 1"},
		{ID: "agent-2", Name: "Agent 2"},
		{ID: "agent-3", Name: "Agent 3"},
	}

	for _, a := range agents {
		_ = orch.RegisterAgent(a)
	}

	listed := orch.ListAgents()

	if len(listed) != 3 {
		t.Errorf("expected 3 agents, got %d", len(listed))
	}

	// Verify all agents are present
	foundIDs := make(map[string]bool)
	for _, a := range listed {
		foundIDs[a.ID] = true
	}

	for _, a := range agents {
		if !foundIDs[a.ID] {
			t.Errorf("agent %s not found in listed agents", a.ID)
		}
	}
}

func TestOrchestrator_SetEventCallback(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	var receivedEvent *OrchestratorEvent
	callback := func(event *OrchestratorEvent) {
		receivedEvent = event
	}

	orch.SetEventCallback(callback)

	// Emit a test event
	testEvent := &OrchestratorEvent{
		Type:      EventAgentSelected,
		AgentID:   "test-agent",
		Timestamp: time.Now(),
	}
	orch.emitEvent(testEvent)

	if receivedEvent == nil {
		t.Error("expected callback to receive event")
	}

	if receivedEvent.Type != EventAgentSelected {
		t.Errorf("expected event type %s, got %s", EventAgentSelected, receivedEvent.Type)
	}

	if receivedEvent.AgentID != "test-agent" {
		t.Errorf("expected agent ID %q, got %q", "test-agent", receivedEvent.AgentID)
	}
}

func TestOrchestrator_EmitEventWithNilCallback(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	// Should not panic
	orch.emitEvent(&OrchestratorEvent{
		Type: EventAgentError,
	})
}

func TestOrchestrator_Config(t *testing.T) {
	config := &MultiAgentConfig{
		DefaultAgentID:     "test-default",
		EnablePeerHandoffs: true,
	}

	orch := mustNewOrchestrator(t, config, nil, nil)

	got := orch.Config()
	if got == nil {
		t.Fatal("expected config to be returned")
	}

	if got.DefaultAgentID != "test-default" {
		t.Errorf("expected DefaultAgentID %q, got %q", "test-default", got.DefaultAgentID)
	}
}

func TestOrchestrator_Provider(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	// Provider is nil since we passed nil
	if orch.Provider() != nil {
		t.Error("expected nil provider")
	}
}

func TestOrchestrator_Sessions(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	// Sessions is nil since we passed nil
	if orch.Sessions() != nil {
		t.Error("expected nil sessions store")
	}
}

func TestOrchestrator_BuildHandoffMessage(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	tests := []struct {
		name     string
		request  *HandoffRequest
		contains []string
	}{
		{
			name: "basic handoff",
			request: &HandoffRequest{
				FromAgentID: "agent-1",
				Reason:      "Need code review",
			},
			contains: []string{
				"agent-1",
				"Need code review",
			},
		},
		{
			name: "handoff with context",
			request: &HandoffRequest{
				FromAgentID: "agent-1",
				Reason:      "Needs research",
				Context: &SharedContext{
					Task:    "Find information about Go",
					Summary: "User asked about Go programming",
				},
			},
			contains: []string{
				"agent-1",
				"Needs research",
				"Find information about Go",
				"Go programming",
			},
		},
		{
			name: "handoff with return expected",
			request: &HandoffRequest{
				FromAgentID:    "agent-1",
				Reason:         "Quick task",
				ReturnExpected: true,
			},
			contains: []string{
				"agent-1",
				"Quick task",
				"return",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := orch.buildHandoffMessage(tt.request)

			for _, s := range tt.contains {
				if !containsString(msg, s) {
					t.Errorf("expected message to contain %q, got: %s", s, msg)
				}
			}
		})
	}
}

func TestOrchestrator_GetSessionMetadata(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	t.Run("nil metadata returns empty", func(t *testing.T) {
		session := &models.Session{ID: "test-1"}
		meta := orch.getSessionMetadata(session)

		if meta == nil {
			t.Fatal("expected metadata to be created")
		}
		if meta.CurrentAgentID != "" {
			t.Error("expected empty current agent ID")
		}
	})

	t.Run("extracts current agent ID", func(t *testing.T) {
		session := &models.Session{
			ID: "test-2",
			Metadata: map[string]any{
				"current_agent_id": "agent-1",
			},
		}
		meta := orch.getSessionMetadata(session)

		if meta.CurrentAgentID != "agent-1" {
			t.Errorf("expected current agent ID %q, got %q", "agent-1", meta.CurrentAgentID)
		}
	})

	t.Run("extracts handoff count", func(t *testing.T) {
		session := &models.Session{
			ID: "test-3",
			Metadata: map[string]any{
				"handoff_count": 5,
			},
		}
		meta := orch.getSessionMetadata(session)

		if meta.HandoffCount != 5 {
			t.Errorf("expected handoff count 5, got %d", meta.HandoffCount)
		}
	})

	t.Run("extracts handoff stack", func(t *testing.T) {
		session := &models.Session{
			ID: "test-4",
			Metadata: map[string]any{
				"active_handoff_stack": []string{"agent-1", "agent-2"},
			},
		}
		meta := orch.getSessionMetadata(session)

		if len(meta.ActiveHandoffStack) != 2 {
			t.Errorf("expected 2 items in stack, got %d", len(meta.ActiveHandoffStack))
		}
	})
}

func TestOrchestrator_UpdateSessionMetadata(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	t.Run("updates session metadata", func(t *testing.T) {
		session := &models.Session{ID: "test-1"}
		now := time.Now()
		meta := &SessionMetadata{
			CurrentAgentID:     "agent-1",
			HandoffCount:       3,
			ActiveHandoffStack: []string{"agent-2"},
			LastHandoffAt:      &now,
		}

		orch.updateSessionMetadata(session, meta)

		if session.Metadata == nil {
			t.Fatal("expected metadata to be created")
		}

		if session.Metadata["current_agent_id"] != "agent-1" {
			t.Error("expected current_agent_id to be set")
		}
		if session.Metadata["handoff_count"] != 3 {
			t.Error("expected handoff_count to be set")
		}
	})

	t.Run("initializes nil metadata", func(t *testing.T) {
		session := &models.Session{ID: "test-2", Metadata: nil}
		meta := &SessionMetadata{CurrentAgentID: "test"}

		orch.updateSessionMetadata(session, meta)

		if session.Metadata == nil {
			t.Error("expected metadata map to be initialized")
		}
	})
}

func TestOrchestrator_BuildAgentContext(t *testing.T) {
	config := &MultiAgentConfig{
		EnablePeerHandoffs: true,
	}
	orch := mustNewOrchestrator(t, config, nil, nil)

	agent := &AgentDefinition{
		ID:   "ctx-agent",
		Name: "Context Agent",
	}
	_ = orch.RegisterAgent(agent)

	t.Run("adds agent ID to context", func(t *testing.T) {
		ctx := context.Background()
		meta := &SessionMetadata{}

		agentCtx := orch.buildAgentContext(ctx, "ctx-agent", meta)

		agentID, ok := CurrentAgentFromContext(agentCtx)
		if !ok {
			t.Error("expected agent ID in context")
		}
		if agentID != "ctx-agent" {
			t.Errorf("expected agent ID %q, got %q", "ctx-agent", agentID)
		}
	})

	t.Run("adds handoff stack to context", func(t *testing.T) {
		ctx := context.Background()
		meta := &SessionMetadata{
			ActiveHandoffStack: []string{"agent-1", "agent-2"},
		}

		agentCtx := orch.buildAgentContext(ctx, "ctx-agent", meta)

		stack := HandoffStackFromContext(agentCtx)
		if len(stack) != 2 {
			t.Errorf("expected 2 items in stack, got %d", len(stack))
		}
	})
}

func TestOrchestrator_IsHandoffResult(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	tests := []struct {
		name   string
		result *models.ToolResult
		want   bool
	}{
		{
			name:   "nil result",
			result: nil,
			want:   false,
		},
		{
			name: "empty content",
			result: &models.ToolResult{
				Content: "",
			},
			want: false,
		},
		{
			name: "JSON content is handoff",
			result: &models.ToolResult{
				Content: `{"handoff_request": {}}`,
			},
			want: true,
		},
		{
			name: "non-JSON content is not handoff",
			result: &models.ToolResult{
				Content: "plain text result",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orch.isHandoffResult(tt.result)
			if got != tt.want {
				t.Errorf("isHandoffResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrchestratorEventTypes(t *testing.T) {
	// Verify event type constants
	types := []struct {
		eventType OrchestratorEventType
		expected  string
	}{
		{EventAgentSelected, "agent_selected"},
		{EventHandoffInitiated, "handoff_initiated"},
		{EventHandoffCompleted, "handoff_completed"},
		{EventHandoffFailed, "handoff_failed"},
		{EventContextShared, "context_shared"},
		{EventAgentError, "agent_error"},
	}

	for _, tt := range types {
		if string(tt.eventType) != tt.expected {
			t.Errorf("event type %s != expected %s", tt.eventType, tt.expected)
		}
	}
}

func TestWithCurrentAgent(t *testing.T) {
	ctx := context.Background()

	agentCtx := WithCurrentAgent(ctx, "test-agent")

	agentID, ok := CurrentAgentFromContext(agentCtx)
	if !ok {
		t.Error("expected agent ID to be in context")
	}
	if agentID != "test-agent" {
		t.Errorf("expected agent ID %q, got %q", "test-agent", agentID)
	}
}

func TestCurrentAgentFromContext_NotSet(t *testing.T) {
	ctx := context.Background()

	_, ok := CurrentAgentFromContext(ctx)
	if ok {
		t.Error("expected no agent ID in empty context")
	}
}

func TestWithHandoffStack(t *testing.T) {
	ctx := context.Background()
	stack := []string{"agent-1", "agent-2", "agent-3"}

	stackCtx := WithHandoffStack(ctx, stack)

	got := HandoffStackFromContext(stackCtx)
	if len(got) != 3 {
		t.Errorf("expected 3 items in stack, got %d", len(got))
	}
}

func TestHandoffStackFromContext_NotSet(t *testing.T) {
	ctx := context.Background()

	stack := HandoffStackFromContext(ctx)
	if stack != nil {
		t.Errorf("expected nil stack, got %v", stack)
	}
}

func TestOrchestrator_RegisterToolForAgent(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	agent := &AgentDefinition{
		ID:   "tool-agent",
		Name: "Tool Agent",
	}
	_ = orch.RegisterAgent(agent)

	t.Run("register tool for existing agent", func(t *testing.T) {
		tool := &mockTool{name: "test-tool"}
		err := orch.RegisterToolForAgent("tool-agent", tool)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("register tool for non-existent agent", func(t *testing.T) {
		tool := &mockTool{name: "test-tool"}
		err := orch.RegisterToolForAgent("non-existent", tool)
		if err == nil {
			t.Error("expected error for non-existent agent")
		}
	})
}

func TestOrchestrator_RegisterToolForAll(t *testing.T) {
	orch := mustNewOrchestrator(t, nil, nil, nil)

	agents := []*AgentDefinition{
		{ID: "agent-1", Name: "Agent 1"},
		{ID: "agent-2", Name: "Agent 2"},
	}

	for _, a := range agents {
		_ = orch.RegisterAgent(a)
	}

	tool := &mockTool{name: "shared-tool"}
	orch.RegisterToolForAll(tool)

	// Just verify no panic occurs - actual tool registration
	// would need to check the runtime internals
}

// mockTool implements the agent.Tool interface for testing
type mockTool struct {
	name string
}

func (t *mockTool) Name() string            { return t.name }
func (t *mockTool) Description() string     { return "mock tool" }
func (t *mockTool) Schema() json.RawMessage { return []byte("{}") }
func (t *mockTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	return &agent.ToolResult{Content: "mock result"}, nil
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
