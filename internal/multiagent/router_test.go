package multiagent

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// mockIntentClassifier implements IntentClassifier for testing
type mockIntentClassifier struct {
	intent     string
	confidence float64
	err        error
}

func (m *mockIntentClassifier) Classify(ctx context.Context, message string, candidates []string) (string, float64, error) {
	return m.intent, m.confidence, m.err
}

func createTestOrchestrator() *Orchestrator {
	config := &MultiAgentConfig{
		DefaultAgentID:     "default-agent",
		EnablePeerHandoffs: true,
		MaxHandoffDepth:    10,
		DefaultContextMode: ContextFull,
		GlobalHandoffRules: []HandoffRule{
			{
				TargetAgentID: "global-target",
				Triggers: []RoutingTrigger{
					{Type: TriggerKeyword, Value: "global"},
				},
				Priority: 100,
			},
		},
	}

	orch := &Orchestrator{
		config:   config,
		agents:   make(map[string]*AgentDefinition),
		runtimes: make(map[string]*agent.Runtime),
	}

	// Register test agents
	agents := []*AgentDefinition{
		{
			ID:                 "default-agent",
			Name:               "Default Agent",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "code-agent",
			Name:               "Code Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "review-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerKeyword, Value: "review"},
						{Type: TriggerKeyword, Values: []string{"check", "verify"}},
					},
					Priority: 10,
				},
				{
					TargetAgentID: "test-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerPattern, Value: "test.*code"},
					},
					Priority: 20,
				},
			},
		},
		{
			ID:                 "review-agent",
			Name:               "Review Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "code-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerTaskComplete},
						{Type: TriggerError},
					},
				},
			},
		},
		{
			ID:                 "test-agent",
			Name:               "Test Agent",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "no-handoff-agent",
			Name:               "No Handoff Agent",
			CanReceiveHandoffs: false,
		},
		{
			ID:                 "intent-agent",
			Name:               "Intent Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "research-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerIntent, Value: "research", Threshold: 0.8},
					},
					Priority: 50,
				},
			},
		},
		{
			ID:                 "research-agent",
			Name:               "Research Agent",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "tool-agent",
			Name:               "Tool Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "code-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerToolUse, Value: "exec"},
					},
				},
			},
		},
		{
			ID:                 "explicit-agent",
			Name:               "Explicit Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "code-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerExplicit, Value: "code"},
					},
				},
			},
		},
		{
			ID:                 "fallback-agent",
			Name:               "Fallback Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "default-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerFallback},
					},
				},
			},
		},
		{
			ID:                 "always-agent",
			Name:               "Always Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "default-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerAlways},
					},
					Priority: 1,
				},
			},
		},
		{
			ID:                 "global-target",
			Name:               "Global Target",
			CanReceiveHandoffs: true,
		},
	}

	for _, a := range agents {
		orch.agents[a.ID] = a
	}

	return orch
}

// createCleanTestOrchestrator creates a test orchestrator without always/fallback agents
// This is used for tests that need to verify "no match" scenarios
func createCleanTestOrchestrator() *Orchestrator {
	config := &MultiAgentConfig{
		DefaultAgentID:     "default-agent",
		EnablePeerHandoffs: true,
		MaxHandoffDepth:    10,
		DefaultContextMode: ContextFull,
	}

	orch := &Orchestrator{
		config:   config,
		agents:   make(map[string]*AgentDefinition),
		runtimes: make(map[string]*agent.Runtime),
	}

	// Register test agents (without always-agent and fallback-agent)
	agents := []*AgentDefinition{
		{
			ID:                 "default-agent",
			Name:               "Default Agent",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "code-agent",
			Name:               "Code Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "review-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerKeyword, Value: "review"},
						{Type: TriggerKeyword, Values: []string{"check", "verify"}},
					},
					Priority: 10,
				},
				{
					TargetAgentID: "test-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerPattern, Value: "test.*code"},
					},
					Priority: 20,
				},
			},
		},
		{
			ID:                 "review-agent",
			Name:               "Review Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "code-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerTaskComplete},
						{Type: TriggerError},
					},
				},
			},
		},
		{
			ID:                 "test-agent",
			Name:               "Test Agent",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "intent-agent",
			Name:               "Intent Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "research-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerIntent, Value: "research", Threshold: 0.8},
					},
					Priority: 50,
				},
			},
		},
		{
			ID:                 "research-agent",
			Name:               "Research Agent",
			CanReceiveHandoffs: true,
		},
		{
			ID:                 "tool-agent",
			Name:               "Tool Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "code-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerToolUse, Value: "exec"},
					},
				},
			},
		},
		{
			ID:                 "explicit-agent",
			Name:               "Explicit Agent",
			CanReceiveHandoffs: true,
			HandoffRules: []HandoffRule{
				{
					TargetAgentID: "code-agent",
					Triggers: []RoutingTrigger{
						{Type: TriggerExplicit, Value: "code"},
					},
				},
			},
		},
	}

	for _, a := range agents {
		orch.agents[a.ID] = a
	}

	return orch
}

func TestNewRouter(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	if router == nil {
		t.Fatal("expected router to be created")
	}

	if router.orchestrator != orch {
		t.Error("expected orchestrator to be set")
	}

	if router.compiledPatterns == nil {
		t.Error("expected compiled patterns map to be initialized")
	}
}

func TestRouter_SetIntentClassifier(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	classifier := &mockIntentClassifier{
		intent:     "research",
		confidence: 0.9,
	}

	router.SetIntentClassifier(classifier)

	if router.intentClassifier == nil {
		t.Error("expected intent classifier to be set")
	}
}

func TestRouter_Route_KeywordTrigger(t *testing.T) {
	orch := createCleanTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	tests := []struct {
		name            string
		message         string
		currentAgent    string
		wantAgentID     string
		wantShouldRoute bool
	}{
		{
			name:            "keyword match - single",
			message:         "please review this code",
			currentAgent:    "code-agent",
			wantAgentID:     "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "keyword match - from values list",
			message:         "can you check this?",
			currentAgent:    "code-agent",
			wantAgentID:     "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "no keyword match",
			message:         "hello world",
			currentAgent:    "code-agent",
			wantAgentID:     "",
			wantShouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &models.Message{Content: tt.message}
			agentID, shouldRoute := router.Route(ctx, session, msg, tt.currentAgent)

			if shouldRoute != tt.wantShouldRoute {
				t.Errorf("shouldRoute = %v, want %v", shouldRoute, tt.wantShouldRoute)
			}

			if agentID != tt.wantAgentID {
				t.Errorf("agentID = %q, want %q", agentID, tt.wantAgentID)
			}
		})
	}
}

func TestRouter_Route_PatternTrigger(t *testing.T) {
	orch := createCleanTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	tests := []struct {
		name            string
		message         string
		currentAgent    string
		wantAgentID     string
		wantShouldRoute bool
	}{
		{
			name:            "pattern match",
			message:         "please test my code",
			currentAgent:    "code-agent",
			wantAgentID:     "test-agent",
			wantShouldRoute: true,
		},
		{
			name:            "pattern match - complex",
			message:         "test all the code",
			currentAgent:    "code-agent",
			wantAgentID:     "test-agent",
			wantShouldRoute: true,
		},
		{
			name:            "pattern no match",
			message:         "test something else",
			currentAgent:    "code-agent",
			wantAgentID:     "",
			wantShouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &models.Message{Content: tt.message}
			agentID, shouldRoute := router.Route(ctx, session, msg, tt.currentAgent)

			if shouldRoute != tt.wantShouldRoute {
				t.Errorf("shouldRoute = %v, want %v", shouldRoute, tt.wantShouldRoute)
			}

			if tt.wantShouldRoute && agentID != tt.wantAgentID {
				t.Errorf("agentID = %q, want %q", agentID, tt.wantAgentID)
			}
		})
	}
}

func TestRouter_Route_IntentTrigger(t *testing.T) {
	orch := createCleanTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	t.Run("intent trigger with classifier", func(t *testing.T) {
		router.SetIntentClassifier(&mockIntentClassifier{
			intent:     "research",
			confidence: 0.9,
		})

		msg := &models.Message{Content: "I need to research this topic"}
		agentID, shouldRoute := router.Route(ctx, session, msg, "intent-agent")

		if !shouldRoute {
			t.Error("expected shouldRoute to be true")
		}
		if agentID != "research-agent" {
			t.Errorf("expected research-agent, got %s", agentID)
		}
	})

	t.Run("intent below threshold", func(t *testing.T) {
		router.SetIntentClassifier(&mockIntentClassifier{
			intent:     "research",
			confidence: 0.5, // Below 0.8 threshold
		})

		msg := &models.Message{Content: "maybe research"}
		_, shouldRoute := router.Route(ctx, session, msg, "intent-agent")

		if shouldRoute {
			t.Error("expected shouldRoute to be false when below threshold")
		}
	})

	t.Run("intent without classifier", func(t *testing.T) {
		router.intentClassifier = nil

		msg := &models.Message{Content: "I need to research this"}
		_, shouldRoute := router.Route(ctx, session, msg, "intent-agent")

		if shouldRoute {
			t.Error("expected shouldRoute to be false without classifier")
		}
	})
}

func TestRouter_Route_ToolUseTrigger(t *testing.T) {
	orch := createCleanTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	tests := []struct {
		name            string
		toolCalls       []models.ToolCall
		currentAgent    string
		wantAgentID     string
		wantShouldRoute bool
	}{
		{
			name: "tool use match",
			toolCalls: []models.ToolCall{
				{Name: "exec", Input: []byte(`{}`)},
			},
			currentAgent:    "tool-agent",
			wantAgentID:     "code-agent",
			wantShouldRoute: true,
		},
		{
			name: "tool use no match",
			toolCalls: []models.ToolCall{
				{Name: "other-tool", Input: []byte(`{}`)},
			},
			currentAgent:    "tool-agent",
			wantAgentID:     "",
			wantShouldRoute: false,
		},
		{
			name:            "no tool calls",
			toolCalls:       nil,
			currentAgent:    "tool-agent",
			wantAgentID:     "",
			wantShouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &models.Message{
				Content:   "test",
				ToolCalls: tt.toolCalls,
			}
			agentID, shouldRoute := router.Route(ctx, session, msg, tt.currentAgent)

			if shouldRoute != tt.wantShouldRoute {
				t.Errorf("shouldRoute = %v, want %v", shouldRoute, tt.wantShouldRoute)
			}

			if agentID != tt.wantAgentID {
				t.Errorf("agentID = %q, want %q", agentID, tt.wantAgentID)
			}
		})
	}
}

func TestRouter_Route_ExplicitTrigger(t *testing.T) {
	orch := createCleanTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	tests := []struct {
		name            string
		message         string
		currentAgent    string
		wantAgentID     string
		wantShouldRoute bool
	}{
		{
			name:            "explicit handoff request",
			message:         "please hand off to code",
			currentAgent:    "explicit-agent",
			wantAgentID:     "code-agent",
			wantShouldRoute: true,
		},
		{
			name:            "transfer request",
			message:         "transfer to code agent",
			currentAgent:    "explicit-agent",
			wantAgentID:     "code-agent",
			wantShouldRoute: true,
		},
		{
			name:            "switch to request",
			message:         "switch to code please",
			currentAgent:    "explicit-agent",
			wantAgentID:     "code-agent",
			wantShouldRoute: true,
		},
		{
			name:            "at mention",
			message:         "@code help me",
			currentAgent:    "explicit-agent",
			wantAgentID:     "code-agent",
			wantShouldRoute: true,
		},
		{
			name:            "no explicit request",
			message:         "just a normal message",
			currentAgent:    "explicit-agent",
			wantAgentID:     "",
			wantShouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &models.Message{Content: tt.message}
			agentID, shouldRoute := router.Route(ctx, session, msg, tt.currentAgent)

			if shouldRoute != tt.wantShouldRoute {
				t.Errorf("shouldRoute = %v, want %v", shouldRoute, tt.wantShouldRoute)
			}

			if tt.wantShouldRoute && agentID != tt.wantAgentID {
				t.Errorf("agentID = %q, want %q", agentID, tt.wantAgentID)
			}
		})
	}
}

func TestRouter_Route_TaskCompleteTrigger(t *testing.T) {
	orch := createCleanTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	tests := []struct {
		name            string
		message         string
		metadata        map[string]any
		currentAgent    string
		wantShouldRoute bool
	}{
		{
			name:            "task complete phrase",
			message:         "task complete",
			currentAgent:    "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "finished phrase",
			message:         "I'm finished with the review",
			currentAgent:    "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "task complete metadata",
			message:         "done",
			metadata:        map[string]any{"task_complete": true},
			currentAgent:    "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "no completion signal",
			message:         "still working on it",
			currentAgent:    "review-agent",
			wantShouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &models.Message{
				Content:  tt.message,
				Metadata: tt.metadata,
			}
			_, shouldRoute := router.Route(ctx, session, msg, tt.currentAgent)

			if shouldRoute != tt.wantShouldRoute {
				t.Errorf("shouldRoute = %v, want %v", shouldRoute, tt.wantShouldRoute)
			}
		})
	}
}

func TestRouter_Route_ErrorTrigger(t *testing.T) {
	orch := createCleanTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	tests := []struct {
		name            string
		message         string
		toolResults     []models.ToolResult
		metadata        map[string]any
		currentAgent    string
		wantShouldRoute bool
	}{
		{
			name: "tool result error",
			toolResults: []models.ToolResult{
				{IsError: true, Content: "command failed"},
			},
			currentAgent:    "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "error in metadata",
			message:         "something happened",
			metadata:        map[string]any{"error": "timeout"},
			currentAgent:    "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "error phrase in content",
			message:         "I encountered an error trying to do this",
			currentAgent:    "review-agent",
			wantShouldRoute: true,
		},
		{
			name:            "no error",
			message:         "everything is fine",
			currentAgent:    "review-agent",
			wantShouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &models.Message{
				Content:     tt.message,
				ToolResults: tt.toolResults,
				Metadata:    tt.metadata,
			}
			_, shouldRoute := router.Route(ctx, session, msg, tt.currentAgent)

			if shouldRoute != tt.wantShouldRoute {
				t.Errorf("shouldRoute = %v, want %v", shouldRoute, tt.wantShouldRoute)
			}
		})
	}
}

func TestRouter_Route_AlwaysTrigger(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	msg := &models.Message{Content: "anything"}
	agentID, shouldRoute := router.Route(ctx, session, msg, "always-agent")

	if !shouldRoute {
		t.Error("expected always trigger to route")
	}

	if agentID != "default-agent" {
		t.Errorf("expected default-agent, got %s", agentID)
	}
}

func TestRouter_Route_GlobalRules(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	msg := &models.Message{Content: "this is a global message"}
	agentID, shouldRoute := router.Route(ctx, session, msg, "code-agent")

	if !shouldRoute {
		t.Error("expected global rule to match")
	}

	if agentID != "global-target" {
		t.Errorf("expected global-target, got %s", agentID)
	}
}

func TestRouter_Route_Priority(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	// Message that matches both review (priority 10) and test (priority 20)
	msg := &models.Message{Content: "review test my code"}
	agentID, shouldRoute := router.Route(ctx, session, msg, "code-agent")

	if !shouldRoute {
		t.Error("expected to route")
	}

	// Higher priority (20) should win
	if agentID != "test-agent" {
		t.Errorf("expected test-agent (higher priority), got %s", agentID)
	}
}

func TestRouter_Route_NoCurrentAgent(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()
	session := &models.Session{ID: "test-session"}

	msg := &models.Message{Content: "global keyword"}
	agentID, shouldRoute := router.Route(ctx, session, msg, "")

	if !shouldRoute {
		t.Error("expected global rule to match even without current agent")
	}

	if agentID != "global-target" {
		t.Errorf("expected global-target, got %s", agentID)
	}
}

func TestRouter_FindAgentByName(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	tests := []struct {
		name      string
		search    string
		wantID    string
		wantFound bool
	}{
		{
			name:      "find by exact ID",
			search:    "code-agent",
			wantID:    "code-agent",
			wantFound: true,
		},
		{
			name:      "find by name",
			search:    "Code Agent",
			wantID:    "code-agent",
			wantFound: true,
		},
		{
			name:      "find case insensitive",
			search:    "CODE AGENT",
			wantID:    "code-agent",
			wantFound: true,
		},
		{
			name:      "not found",
			search:    "non-existent",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, found := router.FindAgentByName(tt.search)

			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}

			if tt.wantFound && agent.ID != tt.wantID {
				t.Errorf("agent.ID = %s, want %s", agent.ID, tt.wantID)
			}
		})
	}
}

func TestRouter_GetCandidateAgents(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()

	t.Run("returns agents matching triggers", func(t *testing.T) {
		msg := &models.Message{Content: "review this"}
		candidates := router.GetCandidateAgents(ctx, msg)

		if len(candidates) == 0 {
			t.Error("expected at least one candidate")
		}
	})

	t.Run("returns all handoff-capable agents when no match", func(t *testing.T) {
		msg := &models.Message{Content: "xyz123 no match"}
		candidates := router.GetCandidateAgents(ctx, msg)

		// Should return all agents that can receive handoffs
		if len(candidates) == 0 {
			t.Error("expected fallback to all handoff-capable agents")
		}
	})
}

func TestRouter_BuildAgentDescriptions(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	desc := router.BuildAgentDescriptions()

	if desc == "" {
		t.Error("expected non-empty description")
	}

	// Check for expected content
	if !containsSubstring(desc, "Available agents") {
		t.Error("expected 'Available agents' header")
	}
}

func TestRouter_EvaluateKeywordTrigger(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	tests := []struct {
		name      string
		content   string
		trigger   *RoutingTrigger
		wantScore float64
	}{
		{
			name:    "single keyword match",
			content: "please review this",
			trigger: &RoutingTrigger{
				Type:  TriggerKeyword,
				Value: "review",
			},
			wantScore: 1.0,
		},
		{
			name:    "all keywords match",
			content: "review and check this",
			trigger: &RoutingTrigger{
				Type:   TriggerKeyword,
				Values: []string{"review", "check"},
			},
			wantScore: 1.0,
		},
		{
			name:    "partial keywords match",
			content: "review this",
			trigger: &RoutingTrigger{
				Type:   TriggerKeyword,
				Values: []string{"review", "check"},
			},
			wantScore: 0.5,
		},
		{
			name:    "no keyword match",
			content: "hello world",
			trigger: &RoutingTrigger{
				Type:  TriggerKeyword,
				Value: "review",
			},
			wantScore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := router.evaluateKeywordTrigger(tt.content, tt.trigger)

			if score != tt.wantScore {
				t.Errorf("score = %v, want %v", score, tt.wantScore)
			}
		})
	}
}

func TestRouter_EvaluatePatternTrigger(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	tests := []struct {
		name      string
		content   string
		trigger   *RoutingTrigger
		wantScore float64
	}{
		{
			name:    "pattern match",
			content: "test my code",
			trigger: &RoutingTrigger{
				Type:  TriggerPattern,
				Value: "test.*code",
			},
			wantScore: 1.0,
		},
		{
			name:    "pattern no match",
			content: "test something",
			trigger: &RoutingTrigger{
				Type:  TriggerPattern,
				Value: "test.*code",
			},
			wantScore: 0,
		},
		{
			name:    "case insensitive",
			content: "TEST MY CODE",
			trigger: &RoutingTrigger{
				Type:  TriggerPattern,
				Value: "test.*code",
			},
			wantScore: 1.0,
		},
		{
			name:    "empty pattern",
			content: "anything",
			trigger: &RoutingTrigger{
				Type:  TriggerPattern,
				Value: "",
			},
			wantScore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := router.evaluatePatternTrigger(tt.content, tt.trigger)

			if score != tt.wantScore {
				t.Errorf("score = %v, want %v", score, tt.wantScore)
			}
		})
	}
}

func TestRouter_PatternCaching(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	trigger := &RoutingTrigger{
		Type:  TriggerPattern,
		Value: "test.*pattern",
	}

	// First call compiles the pattern
	router.evaluatePatternTrigger("test this pattern", trigger)

	if _, ok := router.compiledPatterns["test.*pattern"]; !ok {
		t.Error("expected pattern to be cached")
	}

	// Second call should use cached pattern
	router.evaluatePatternTrigger("test another pattern", trigger)

	// Should still only have one cached pattern
	if len(router.compiledPatterns) != 1 {
		t.Errorf("expected 1 cached pattern, got %d", len(router.compiledPatterns))
	}
}

func TestRouter_InvalidPattern(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	trigger := &RoutingTrigger{
		Type:  TriggerPattern,
		Value: "[invalid(regex",
	}

	score := router.evaluatePatternTrigger("test", trigger)

	if score != 0 {
		t.Errorf("expected 0 score for invalid regex, got %v", score)
	}
}

func TestTriggerType_Values(t *testing.T) {
	// Verify trigger type constants
	types := []struct {
		triggerType TriggerType
		expected    string
	}{
		{TriggerKeyword, "keyword"},
		{TriggerPattern, "pattern"},
		{TriggerIntent, "intent"},
		{TriggerToolUse, "tool_use"},
		{TriggerExplicit, "explicit"},
		{TriggerFallback, "fallback"},
		{TriggerAlways, "always"},
		{TriggerTaskComplete, "task_complete"},
		{TriggerError, "error"},
	}

	for _, tt := range types {
		if string(tt.triggerType) != tt.expected {
			t.Errorf("trigger type %s != expected %s", tt.triggerType, tt.expected)
		}
	}
}

func TestRouteMatch_Fields(t *testing.T) {
	match := RouteMatch{
		TargetAgentID: "test-agent",
		Priority:      10,
		TriggerType:   TriggerKeyword,
		Confidence:    0.9,
		Rule: &HandoffRule{
			TargetAgentID: "test-agent",
			Priority:      10,
		},
	}

	if match.TargetAgentID != "test-agent" {
		t.Error("expected TargetAgentID to be set")
	}

	if match.Priority != 10 {
		t.Error("expected Priority to be set")
	}

	if match.TriggerType != TriggerKeyword {
		t.Error("expected TriggerType to be set")
	}

	if match.Confidence != 0.9 {
		t.Error("expected Confidence to be set")
	}

	if match.Rule == nil {
		t.Error("expected Rule to be set")
	}
}

func TestRouter_FallbackTrigger(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()

	// Fallback trigger should not match directly
	msg := &models.Message{Content: "fallback test"}
	trigger := &RoutingTrigger{Type: TriggerFallback}

	score := router.evaluateTrigger(ctx, msg, trigger)

	// Fallback is handled specially and should return 0 from evaluateTrigger
	if score != 0 {
		t.Errorf("expected fallback trigger to return 0, got %v", score)
	}
}

func TestRouter_UnknownTriggerType(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)
	ctx := context.Background()

	msg := &models.Message{Content: "test"}
	trigger := &RoutingTrigger{Type: "unknown_type"}

	score := router.evaluateTrigger(ctx, msg, trigger)

	if score != 0 {
		t.Errorf("expected unknown trigger type to return 0, got %v", score)
	}
}

func TestRouter_ExplicitTriggerPatterns(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	patterns := []struct {
		content string
		want    float64
	}{
		{"hand off to agent", 1.0},
		{"handoff to agent", 1.0},
		{"transfer to agent", 1.0},
		{"switch to agent", 1.0},
		{"let agent handle this", 1.0},
		{"ask agent to help", 1.0},
		{"@agent", 1.0},
		{"normal message", 0},
	}

	for _, p := range patterns {
		t.Run(p.content, func(t *testing.T) {
			trigger := &RoutingTrigger{Type: TriggerExplicit}
			score := router.evaluateExplicitTrigger(p.content, trigger)

			if score != p.want {
				t.Errorf("evaluateExplicitTrigger(%q) = %v, want %v", p.content, score, p.want)
			}
		})
	}
}

func TestRouter_TaskCompletePhrases(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	phrases := []string{
		"task complete",
		"task completed",
		"task done",
		"i'm done",
		"i am done",
		"finished",
		"completed successfully",
		"task is complete",
	}

	for _, phrase := range phrases {
		t.Run(phrase, func(t *testing.T) {
			msg := &models.Message{Content: phrase}
			trigger := &RoutingTrigger{Type: TriggerTaskComplete}

			score := router.evaluateTaskCompleteTrigger(msg, trigger)

			if score != 1.0 {
				t.Errorf("expected %q to match task complete, got score %v", phrase, score)
			}
		})
	}
}

func TestRouter_ErrorIndicators(t *testing.T) {
	orch := createTestOrchestrator()
	router := NewRouter(orch)

	indicators := []struct {
		content   string
		wantMatch bool
	}{
		{"there was an error", true},
		{"the operation failed", true},
		{"i cannot do that", true},
		{"i am unable to complete this", true},
		{"i don't know how to do that", true},
		{"this is out of my expertise", true},
		{"i need help with this", true},
		{"everything worked fine", false},
	}

	for _, ind := range indicators {
		t.Run(ind.content, func(t *testing.T) {
			msg := &models.Message{Content: ind.content}
			trigger := &RoutingTrigger{Type: TriggerError}

			score := router.evaluateErrorTrigger(msg, trigger)

			if ind.wantMatch && score == 0 {
				t.Errorf("expected %q to match error indicator", ind.content)
			}
			if !ind.wantMatch && score > 0 {
				t.Errorf("expected %q to NOT match error indicator", ind.content)
			}
		})
	}
}
