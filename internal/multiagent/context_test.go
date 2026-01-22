package multiagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// mockContextSummarizer implements ContextSummarizer for testing
type mockContextSummarizer struct {
	summary string
	err     error
}

func (m *mockContextSummarizer) Summarize(ctx context.Context, messages []*models.Message, maxLength int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.summary, nil
}

// mockSessionStoreForContext implements a partial sessions.Store for testing
type mockSessionStoreForContext struct {
	history []*models.Message
	err     error
}

func (m *mockSessionStoreForContext) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	if limit > len(m.history) {
		limit = len(m.history)
	}
	return m.history[:limit], nil
}

func createContextTestOrchestrator() *Orchestrator {
	config := &MultiAgentConfig{
		DefaultAgentID:     "default-agent",
		EnablePeerHandoffs: true,
		DefaultContextMode: ContextFull,
	}

	orch := &Orchestrator{
		config: config,
		agents: make(map[string]*AgentDefinition),
	}

	orch.agents["agent-1"] = &AgentDefinition{
		ID:                 "agent-1",
		Name:               "Agent 1",
		CanReceiveHandoffs: true,
	}

	orch.agents["agent-2"] = &AgentDefinition{
		ID:                 "agent-2",
		Name:               "Agent 2",
		CanReceiveHandoffs: true,
		HandoffRules: []HandoffRule{
			{
				TargetAgentID: "agent-1",
				ContextMode:   ContextSummary,
				Triggers: []RoutingTrigger{
					{Type: TriggerExplicit, Value: "agent-1"},
				},
			},
		},
	}

	return orch
}

func TestNewContextManager(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	if cm == nil {
		t.Fatal("expected context manager to be created")
	}

	if cm.orchestrator != orch {
		t.Error("expected orchestrator to be set")
	}

	if cm.defaultMode != ContextFull {
		t.Errorf("expected default mode %s, got %s", ContextFull, cm.defaultMode)
	}

	if cm.maxMessages != 50 {
		t.Errorf("expected maxMessages=50, got %d", cm.maxMessages)
	}

	if cm.maxSummaryLength != 1000 {
		t.Errorf("expected maxSummaryLength=1000, got %d", cm.maxSummaryLength)
	}
}

func TestContextManager_SetSummarizer(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	summarizer := &mockContextSummarizer{summary: "test summary"}
	cm.SetSummarizer(summarizer)

	if cm.summarizer == nil {
		t.Error("expected summarizer to be set")
	}
}

func TestContextManager_SetMaxMessages(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	cm.SetMaxMessages(100)

	if cm.maxMessages != 100 {
		t.Errorf("expected maxMessages=100, got %d", cm.maxMessages)
	}
}

func TestContextManager_SetMaxSummaryLength(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	cm.SetMaxSummaryLength(2000)

	if cm.maxSummaryLength != 2000 {
		t.Errorf("expected maxSummaryLength=2000, got %d", cm.maxSummaryLength)
	}
}

func TestContextManager_ConvertToSharedMessages(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	messages := []*models.Message{
		{
			Role:      models.RoleUser,
			Content:   "Hello",
			CreatedAt: time.Now(),
		},
		{
			Role:      models.RoleAssistant,
			Content:   "Hi there",
			CreatedAt: time.Now(),
			Metadata: map[string]any{
				"agent_id": "agent-1",
			},
		},
		nil, // Should be skipped
	}

	shared := cm.convertToSharedMessages(messages)

	if len(shared) != 2 {
		t.Errorf("expected 2 shared messages, got %d", len(shared))
	}

	if shared[0].Role != string(models.RoleUser) {
		t.Errorf("expected role 'user', got %s", shared[0].Role)
	}

	if shared[1].AgentID != "agent-1" {
		t.Errorf("expected agent_id 'agent-1', got %s", shared[1].AgentID)
	}
}

func TestContextManager_BuildBasicSummary(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	t.Run("empty messages", func(t *testing.T) {
		summary := cm.buildBasicSummary([]*models.Message{})
		if !containsSubstring(summary, "No conversation history") {
			t.Error("expected 'No conversation history' message")
		}
	})

	t.Run("with messages", func(t *testing.T) {
		messages := []*models.Message{
			{Role: models.RoleUser, Content: "What is Go programming?"},
			{Role: models.RoleAssistant, Content: "Go is a programming language..."},
			{Role: models.RoleUser, Content: "Show me an example"},
			{Role: models.RoleTool, Content: "code output"},
			{
				Role:      models.RoleAssistant,
				Content:   "Here's an example",
				ToolCalls: []models.ToolCall{{Name: "exec"}},
			},
		}

		summary := cm.buildBasicSummary(messages)

		if !containsSubstring(summary, "Conversation summary") {
			t.Error("expected 'Conversation summary' header")
		}

		if !containsSubstring(summary, "2 user messages") {
			t.Error("expected user message count")
		}

		if !containsSubstring(summary, "Original request") {
			t.Error("expected 'Original request'")
		}

		if !containsSubstring(summary, "Tools used") {
			t.Error("expected 'Tools used'")
		}
	})
}

func TestContextManager_GenerateSummary(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)
	ctx := context.Background()

	messages := []*models.Message{
		{Role: models.RoleUser, Content: "Test message"},
	}

	t.Run("without summarizer uses basic", func(t *testing.T) {
		summary, err := cm.generateSummary(ctx, messages)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !containsSubstring(summary, "Conversation summary") {
			t.Error("expected basic summary")
		}
	})

	t.Run("with summarizer", func(t *testing.T) {
		cm.SetSummarizer(&mockContextSummarizer{summary: "AI-generated summary"})

		summary, err := cm.generateSummary(ctx, messages)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if summary != "AI-generated summary" {
			t.Errorf("expected 'AI-generated summary', got %s", summary)
		}
	})
}

func TestContextManager_FilterMessages(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	messages := []*models.Message{
		{Role: models.RoleUser, Content: "User message"},
		{Role: models.RoleAssistant, Content: "Assistant message"},
		{Role: models.RoleSystem, Content: "System message"},
		{Role: models.RoleTool, Content: "Tool result"},
		nil,                                  // Should be skipped
		{Role: models.RoleUser, Content: ""}, // Empty, should be skipped
	}

	request := &HandoffRequest{
		FromAgentID: "agent-1",
		ToAgentID:   "agent-2",
	}

	filtered := cm.filterMessages(messages, request)

	// Default filter includes user and assistant
	expectedCount := 2
	if len(filtered) != expectedCount {
		t.Errorf("expected %d filtered messages, got %d", expectedCount, len(filtered))
	}
}

func TestContextManager_FilterMessages_CustomRoles(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	messages := []*models.Message{
		{Role: models.RoleUser, Content: "User message"},
		{Role: models.RoleAssistant, Content: "Assistant message"},
		{Role: models.RoleSystem, Content: "System message"},
	}

	request := &HandoffRequest{
		FromAgentID: "agent-1",
		ToAgentID:   "agent-2",
		Context: &SharedContext{
			Metadata: map[string]any{
				"include_roles": []string{string(models.RoleUser)},
			},
		},
	}

	filtered := cm.filterMessages(messages, request)

	if len(filtered) != 1 {
		t.Errorf("expected 1 filtered message, got %d", len(filtered))
	}

	if filtered[0].Role != string(models.RoleUser) {
		t.Error("expected only user messages")
	}
}

func TestContextManager_GetLastNCount(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	t.Run("default count", func(t *testing.T) {
		request := &HandoffRequest{}
		n := cm.getLastNCount(request)
		if n != 10 {
			t.Errorf("expected default 10, got %d", n)
		}
	})

	t.Run("custom count from metadata", func(t *testing.T) {
		request := &HandoffRequest{
			Context: &SharedContext{
				Metadata: map[string]any{
					"last_n": 5,
				},
			},
		}
		n := cm.getLastNCount(request)
		if n != 5 {
			t.Errorf("expected 5, got %d", n)
		}
	})
}

func TestContextManager_ExtractVariables(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)

	messages := []*models.Message{
		{
			Role:    models.RoleUser,
			Content: "Test",
			Metadata: map[string]any{
				"variables": map[string]any{
					"name": "John",
				},
				"entities": map[string]any{
					"location": "New York",
				},
			},
			CreatedAt: time.Now(),
		},
		nil, // Should be skipped
	}

	shared := &SharedContext{
		Variables: make(map[string]any),
	}

	cm.extractVariables(messages, shared)

	if shared.Variables["name"] != "John" {
		t.Error("expected variable 'name' to be extracted")
	}

	if shared.Variables["entity_location"] != "New York" {
		t.Error("expected entity 'location' to be extracted")
	}

	if _, ok := shared.Variables["conversation_start"]; !ok {
		t.Error("expected conversation_start to be set")
	}

	// message_count is the slice length (including nil entries), not non-nil count
	if shared.Variables["message_count"] != 2 {
		t.Errorf("expected message_count=2, got %v", shared.Variables["message_count"])
	}
}

func TestMergeContexts(t *testing.T) {
	now := time.Now()

	ctx1 := &SharedContext{
		Summary:        "First summary",
		Task:           "First task",
		PreviousAgents: []string{"agent-1"},
		Variables: map[string]any{
			"var1": "value1",
		},
		Metadata: map[string]any{
			"meta1": "value1",
		},
		Messages: []SharedMessage{
			{Role: "user", Content: "Hello", Timestamp: now},
		},
	}

	ctx2 := &SharedContext{
		Summary:        "Second summary",
		Task:           "Second task",
		PreviousAgents: []string{"agent-2", "agent-1"}, // agent-1 is duplicate
		Variables: map[string]any{
			"var2": "value2",
		},
		Metadata: map[string]any{
			"meta2": "value2",
		},
		Messages: []SharedMessage{
			{Role: "user", Content: "Hello", Timestamp: now},          // Duplicate
			{Role: "assistant", Content: "Hi", Timestamp: now.Add(1)}, // New
		},
	}

	merged := MergeContexts(ctx1, ctx2, nil) // nil should be handled

	if !containsSubstring(merged.Summary, "First summary") {
		t.Error("expected first summary in merged")
	}

	if !containsSubstring(merged.Summary, "Second summary") {
		t.Error("expected second summary in merged")
	}

	if merged.Task != "Second task" {
		t.Errorf("expected latest task, got %s", merged.Task)
	}

	// Check unique previous agents
	if len(merged.PreviousAgents) != 2 {
		t.Errorf("expected 2 unique previous agents, got %d", len(merged.PreviousAgents))
	}

	// Check variables merged
	if merged.Variables["var1"] != "value1" || merged.Variables["var2"] != "value2" {
		t.Error("expected variables to be merged")
	}

	// Check messages deduplicated by timestamp
	if len(merged.Messages) != 2 {
		t.Errorf("expected 2 unique messages, got %d", len(merged.Messages))
	}
}

func TestMergeContexts_Empty(t *testing.T) {
	merged := MergeContexts()

	if merged == nil {
		t.Fatal("expected non-nil merged context")
	}

	if merged.Summary != "" {
		t.Error("expected empty summary")
	}

	if len(merged.Variables) != 0 {
		t.Error("expected empty variables")
	}
}

func TestFormatContextForPrompt(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		result := FormatContextForPrompt(nil)
		if result != "" {
			t.Error("expected empty string for nil context")
		}
	})

	t.Run("full context", func(t *testing.T) {
		ctx := &SharedContext{
			Task:           "Review code",
			Summary:        "User submitted Python code for review",
			PreviousAgents: []string{"coordinator", "code-analyzer"},
			Variables: map[string]any{
				"language": "Python",
			},
			Messages: []SharedMessage{
				{Role: "user", Content: "Please review my code", AgentID: ""},
				{Role: "assistant", Content: "I'll review it", AgentID: "code-agent"},
			},
		}

		formatted := FormatContextForPrompt(ctx)

		expectedSections := []string{
			"Current Task",
			"Review code",
			"Previous Agents",
			"coordinator",
			"code-analyzer",
			"Conversation Summary",
			"Python code",
			"Context Variables",
			"language",
			"Conversation History",
			"code-agent",
		}

		for _, section := range expectedSections {
			if !containsSubstring(formatted, section) {
				t.Errorf("expected formatted context to contain %q", section)
			}
		}
	})
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestContextFilter_Fields(t *testing.T) {
	now := time.Now()
	filter := &ContextFilter{
		IncludeRoles:     []string{"user", "assistant"},
		ExcludeRoles:     []string{"system"},
		IncludeAgents:    []string{"agent-1"},
		ExcludeAgents:    []string{"agent-2"},
		MinTimestamp:     &now,
		MaxTimestamp:     &now,
		ContainsKeywords: []string{"important"},
		MaxMessages:      10,
	}

	if len(filter.IncludeRoles) != 2 {
		t.Error("expected IncludeRoles to be set")
	}

	if len(filter.ExcludeRoles) != 1 {
		t.Error("expected ExcludeRoles to be set")
	}

	if filter.MaxMessages != 10 {
		t.Error("expected MaxMessages to be set")
	}
}

func TestApplyFilter(t *testing.T) {
	now := time.Now()
	messages := []SharedMessage{
		{Role: "user", Content: "Important message", Timestamp: now},
		{Role: "assistant", Content: "Response", AgentID: "agent-1", Timestamp: now.Add(1 * time.Second)},
		{Role: "system", Content: "System message", Timestamp: now.Add(2 * time.Second)},
		{Role: "assistant", Content: "From agent-2", AgentID: "agent-2", Timestamp: now.Add(3 * time.Second)},
	}

	t.Run("nil filter returns all", func(t *testing.T) {
		result := ApplyFilter(messages, nil)
		if len(result) != len(messages) {
			t.Errorf("expected %d messages, got %d", len(messages), len(result))
		}
	})

	t.Run("filter by include roles", func(t *testing.T) {
		filter := &ContextFilter{
			IncludeRoles: []string{"user"},
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 1 {
			t.Errorf("expected 1 message, got %d", len(result))
		}
	})

	t.Run("filter by exclude roles", func(t *testing.T) {
		filter := &ContextFilter{
			ExcludeRoles: []string{"system"},
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 3 {
			t.Errorf("expected 3 messages, got %d", len(result))
		}
	})

	t.Run("filter by include agents", func(t *testing.T) {
		filter := &ContextFilter{
			IncludeAgents: []string{"agent-1"},
		}
		result := ApplyFilter(messages, filter)
		// Agent filter only applies to messages WITH an AgentID
		// Messages without AgentID pass through, plus agent-1 message
		// So: user (no AgentID) + assistant (agent-1) + system (no AgentID) = 3
		if len(result) != 3 {
			t.Errorf("expected 3 messages, got %d", len(result))
		}
	})

	t.Run("filter by exclude agents", func(t *testing.T) {
		filter := &ContextFilter{
			ExcludeAgents: []string{"agent-2"},
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 3 {
			t.Errorf("expected 3 messages, got %d", len(result))
		}
	})

	t.Run("filter by min timestamp", func(t *testing.T) {
		minTime := now.Add(1 * time.Second)
		filter := &ContextFilter{
			MinTimestamp: &minTime,
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 3 {
			t.Errorf("expected 3 messages, got %d", len(result))
		}
	})

	t.Run("filter by max timestamp", func(t *testing.T) {
		maxTime := now.Add(1 * time.Second)
		filter := &ContextFilter{
			MaxTimestamp: &maxTime,
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 2 {
			t.Errorf("expected 2 messages, got %d", len(result))
		}
	})

	t.Run("filter by keywords", func(t *testing.T) {
		filter := &ContextFilter{
			ContainsKeywords: []string{"important"},
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 1 {
			t.Errorf("expected 1 message, got %d", len(result))
		}
	})

	t.Run("filter by keywords case insensitive", func(t *testing.T) {
		filter := &ContextFilter{
			ContainsKeywords: []string{"IMPORTANT"},
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 1 {
			t.Errorf("expected 1 message, got %d", len(result))
		}
	})

	t.Run("limit max messages", func(t *testing.T) {
		filter := &ContextFilter{
			MaxMessages: 2,
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 2 {
			t.Errorf("expected 2 messages, got %d", len(result))
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		filter := &ContextFilter{
			IncludeRoles:  []string{"user", "assistant"},
			ExcludeAgents: []string{"agent-2"},
			MaxMessages:   2,
		}
		result := ApplyFilter(messages, filter)
		if len(result) != 2 {
			t.Errorf("expected 2 messages, got %d", len(result))
		}
	})
}

func TestContextSharingMode_Values(t *testing.T) {
	modes := []struct {
		mode     ContextSharingMode
		expected string
	}{
		{ContextFull, "full"},
		{ContextSummary, "summary"},
		{ContextFiltered, "filtered"},
		{ContextNone, "none"},
		{ContextLastN, "last_n"},
	}

	for _, m := range modes {
		if string(m.mode) != m.expected {
			t.Errorf("mode %s != expected %s", m.mode, m.expected)
		}
	}
}

func TestSharedContext_Fields(t *testing.T) {
	ctx := &SharedContext{
		Summary:        "Test summary",
		Task:           "Test task",
		PreviousAgents: []string{"agent-1"},
		Variables: map[string]any{
			"key": "value",
		},
		Metadata: map[string]any{
			"meta": "data",
		},
		Messages: []SharedMessage{
			{Role: "user", Content: "Test"},
		},
	}

	if ctx.Summary != "Test summary" {
		t.Error("expected Summary to be set")
	}

	if ctx.Task != "Test task" {
		t.Error("expected Task to be set")
	}

	if len(ctx.PreviousAgents) != 1 {
		t.Error("expected PreviousAgents to be set")
	}

	if ctx.Variables["key"] != "value" {
		t.Error("expected Variables to be set")
	}

	if ctx.Metadata["meta"] != "data" {
		t.Error("expected Metadata to be set")
	}

	if len(ctx.Messages) != 1 {
		t.Error("expected Messages to be set")
	}
}

func TestSharedMessage_Fields(t *testing.T) {
	now := time.Now()
	msg := SharedMessage{
		Role:      "assistant",
		Content:   "Test content",
		AgentID:   "test-agent",
		Timestamp: now,
	}

	if msg.Role != "assistant" {
		t.Error("expected Role to be set")
	}

	if msg.Content != "Test content" {
		t.Error("expected Content to be set")
	}

	if msg.AgentID != "test-agent" {
		t.Error("expected AgentID to be set")
	}

	if msg.Timestamp != now {
		t.Error("expected Timestamp to be set")
	}
}

func TestContextManager_SummarizerError(t *testing.T) {
	orch := createContextTestOrchestrator()
	cm := NewContextManager(orch)
	ctx := context.Background()

	// Set a summarizer that returns an error
	cm.SetSummarizer(&mockContextSummarizer{err: errors.New("summarizer error")})

	messages := []*models.Message{
		{Role: models.RoleUser, Content: "Test message"},
	}

	// Summarizer error is propagated (implementation doesn't fallback on error)
	_, err := cm.generateSummary(ctx, messages)
	if err == nil {
		t.Fatal("expected error from summarizer")
	}

	if err.Error() != "summarizer error" {
		t.Errorf("expected 'summarizer error', got %q", err.Error())
	}
}
