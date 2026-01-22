package multiagent

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
)

// mockProvider implements agent.LLMProvider for testing
type mockLLMProvider struct{}

// mockSessionStore implements sessions.Store for testing
type mockSessionStore struct {
	history []*mockMessage
}

type mockMessage struct {
	ID      string
	Role    string
	Content string
}

func (m *mockSessionStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*mockMessage, error) {
	if limit > len(m.history) {
		limit = len(m.history)
	}
	return m.history[:limit], nil
}

// testSetup creates a basic orchestrator with agents for testing
func testSetup(t *testing.T) (*Orchestrator, *CapabilityRouter) {
	t.Helper()

	config := &MultiAgentConfig{
		DefaultAgentID:     "agent-1",
		EnablePeerHandoffs: true,
		MaxHandoffDepth:    10,
		HandoffTimeout:     5 * time.Minute,
		DefaultContextMode: ContextFull,
	}

	// Create orchestrator without real provider/sessions (they're not used in unit tests)
	orch := &Orchestrator{
		config:   config,
		agents:   make(map[string]*AgentDefinition),
		runtimes: make(map[string]*agent.Runtime),
	}

	// Register test agents
	agents := []*AgentDefinition{
		{
			ID:                 "agent-1",
			Name:               "General Agent",
			Description:        "General purpose agent",
			Tools:              []string{"search", "read"},
			CanReceiveHandoffs: true,
			Metadata: map[string]any{
				"capabilities": []string{"general", "research"},
			},
		},
		{
			ID:                 "agent-2",
			Name:               "Code Agent",
			Description:        "Code expert agent",
			Tools:              []string{"exec", "write", "read"},
			CanReceiveHandoffs: true,
			Metadata: map[string]any{
				"capabilities": []string{"coding", "debugging"},
			},
		},
		{
			ID:                 "agent-3",
			Name:               "Data Agent",
			Description:        "Data analysis agent",
			Tools:              []string{"analyze", "visualize"},
			CanReceiveHandoffs: true,
			Metadata: map[string]any{
				"capabilities": []string{"data", "analytics"},
			},
		},
		{
			ID:                 "agent-4",
			Name:               "No Handoff Agent",
			Description:        "Cannot receive handoffs",
			Tools:              []string{"special"},
			CanReceiveHandoffs: false,
		},
	}

	for _, agent := range agents {
		orch.agents[agent.ID] = agent
	}

	// Create capability router
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routerConfig := CapabilityRouterConfig{
		EnableHealthChecks:       true,
		EnableLoadBalancing:      true,
		EnableCapabilityMatching: true,
		UnhealthyThreshold:       3,
		MaxConcurrentPerAgent:    10,
		LoadBalanceStrategy:      StrategyLeastLoaded,
	}

	capRouter := NewCapabilityRouter(orch, routerConfig, logger)

	return orch, capRouter
}

// Helper to make orchestrator.ListAgents work in tests
func (o *Orchestrator) listAgentsForTest() []*AgentDefinition {
	agents := make([]*AgentDefinition, 0, len(o.agents))
	for _, a := range o.agents {
		agents = append(agents, a)
	}
	return agents
}

func TestNewCapabilityRouter(t *testing.T) {
	tests := []struct {
		name   string
		config CapabilityRouterConfig
	}{
		{
			name: "with defaults",
			config: CapabilityRouterConfig{
				EnableHealthChecks:  true,
				EnableLoadBalancing: true,
			},
		},
		{
			name: "with round robin strategy",
			config: CapabilityRouterConfig{
				LoadBalanceStrategy: StrategyRoundRobin,
			},
		},
		{
			name: "with priority strategy",
			config: CapabilityRouterConfig{
				LoadBalanceStrategy: StrategyPriority,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := testSetup(t)
			if router == nil {
				t.Fatal("expected router to be created")
			}

			if router.capabilityIndex == nil {
				t.Error("expected capability index to be initialized")
			}

			if router.agentHealth == nil {
				t.Error("expected agent health map to be initialized")
			}

			if router.agentLoad == nil {
				t.Error("expected agent load map to be initialized")
			}
		})
	}
}

func TestCapabilityRouter_RouteByCapability(t *testing.T) {
	_, router := testSetup(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		capability     string
		wantAgentCount int
		wantNil        bool
	}{
		{
			name:           "find agents with search tool",
			capability:     "search",
			wantAgentCount: 1,
		},
		{
			name:           "find agents with read tool",
			capability:     "read",
			wantAgentCount: 2, // agent-1 and agent-2 both have read
		},
		{
			name:           "find agents with exec tool",
			capability:     "exec",
			wantAgentCount: 1,
		},
		{
			name:           "find agents with coding capability",
			capability:     "coding",
			wantAgentCount: 1,
		},
		{
			name:           "no agents with unknown capability",
			capability:     "unknown-capability",
			wantAgentCount: 0,
			wantNil:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents, err := router.RouteByCapability(ctx, tt.capability)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil && agents != nil && len(agents) != 0 {
				t.Errorf("expected nil or empty, got %d agents", len(agents))
			}

			if !tt.wantNil && len(agents) != tt.wantAgentCount {
				t.Errorf("expected %d agents, got %d", tt.wantAgentCount, len(agents))
			}
		})
	}
}

func TestCapabilityRouter_RouteByCapabilities(t *testing.T) {
	_, router := testSetup(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		capabilities   []string
		wantAgentCount int
		wantNil        bool
	}{
		{
			name:           "empty capabilities returns all agents",
			capabilities:   []string{},
			wantAgentCount: 4,
		},
		{
			name:           "single capability",
			capabilities:   []string{"read"},
			wantAgentCount: 2,
		},
		{
			name:           "multiple capabilities - intersection",
			capabilities:   []string{"exec", "write"},
			wantAgentCount: 1, // Only agent-2 has both
		},
		{
			name:           "no matching agents",
			capabilities:   []string{"search", "exec"},
			wantAgentCount: 0,
			wantNil:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents, err := router.RouteByCapabilities(ctx, tt.capabilities)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil && agents != nil && len(agents) > 0 {
				t.Errorf("expected nil or empty, got %d agents", len(agents))
			}

			if !tt.wantNil && len(agents) != tt.wantAgentCount {
				t.Errorf("expected %d agents, got %d", tt.wantAgentCount, len(agents))
			}
		})
	}
}

func TestCapabilityRouter_RegisterFallbackChain(t *testing.T) {
	_, router := testSetup(t)

	tests := []struct {
		name     string
		chain    string
		agentIDs []string
	}{
		{
			name:     "simple chain",
			chain:    "default",
			agentIDs: []string{"agent-1", "agent-2", "agent-3"},
		},
		{
			name:     "coding chain",
			chain:    "coding",
			agentIDs: []string{"agent-2", "agent-1"},
		},
		{
			name:     "empty chain",
			chain:    "empty",
			agentIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router.RegisterFallbackChain(tt.chain, tt.agentIDs)

			if got := router.fallbackChains[tt.chain]; len(got) != len(tt.agentIDs) {
				t.Errorf("expected chain length %d, got %d", len(tt.agentIDs), len(got))
			}
		})
	}
}

func TestCapabilityRouter_RouteWithFallback(t *testing.T) {
	orch, router := testSetup(t)
	ctx := context.Background()

	// Register fallback chains
	router.RegisterFallbackChain("default", []string{"agent-1", "agent-2", "agent-3"})
	router.RegisterFallbackChain("coding", []string{"agent-2", "agent-1"})
	router.config.DefaultFallbackChain = "default"

	tests := []struct {
		name         string
		chainName    string
		capabilities []string
		wantAgentID  string
		wantNil      bool
	}{
		{
			name:         "match by capabilities",
			chainName:    "default",
			capabilities: []string{"exec"},
			wantAgentID:  "agent-2",
		},
		{
			name:         "use fallback chain when no capability match",
			chainName:    "coding",
			capabilities: []string{"unknown"},
			wantAgentID:  "agent-2", // First in coding chain
		},
		{
			name:         "use default chain for unknown chain name",
			chainName:    "unknown-chain",
			capabilities: []string{"unknown"},
			wantAgentID:  "agent-1", // First in default chain
		},
		{
			name:         "empty capabilities uses chain",
			chainName:    "coding",
			capabilities: []string{},
			wantAgentID:  "", // Returns first from ListAgents
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := router.RouteWithFallback(ctx, tt.chainName, tt.capabilities)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil {
				if agent != nil {
					t.Errorf("expected nil agent, got %s", agent.ID)
				}
				return
			}

			if tt.wantAgentID != "" && agent != nil && agent.ID != tt.wantAgentID {
				t.Errorf("expected agent %s, got %s", tt.wantAgentID, agent.ID)
			}
			_ = orch // silence unused warning
		})
	}
}

func TestCapabilityRouter_HealthTracking(t *testing.T) {
	_, router := testSetup(t)

	t.Run("initial health status", func(t *testing.T) {
		// All agents should be healthy initially
		for _, agentID := range []string{"agent-1", "agent-2", "agent-3"} {
			if !router.IsHealthy(agentID) {
				t.Errorf("expected agent %s to be healthy", agentID)
			}
		}
	})

	t.Run("unknown agent is assumed healthy", func(t *testing.T) {
		if !router.IsHealthy("non-existent") {
			t.Error("expected unknown agent to be assumed healthy")
		}
	})

	t.Run("mark agent unhealthy after threshold", func(t *testing.T) {
		agentID := "agent-1"

		// Simulate failures up to threshold
		for i := 0; i < router.config.UnhealthyThreshold; i++ {
			router.UpdateHealth(agentID, false, 0, "connection failed")
		}

		if router.IsHealthy(agentID) {
			t.Error("expected agent to be marked unhealthy after threshold")
		}

		health := router.GetHealth(agentID)
		if health == nil {
			t.Fatal("expected health status to exist")
		}
		if health.Healthy {
			t.Error("expected health.Healthy to be false")
		}
		if health.ConsecutiveFailures != router.config.UnhealthyThreshold {
			t.Errorf("expected %d consecutive failures, got %d",
				router.config.UnhealthyThreshold, health.ConsecutiveFailures)
		}
	})

	t.Run("successful request resets failure count", func(t *testing.T) {
		agentID := "agent-1"

		// Mark as unhealthy
		for i := 0; i < router.config.UnhealthyThreshold; i++ {
			router.UpdateHealth(agentID, false, 0, "failed")
		}

		// Successful request should reset
		router.UpdateHealth(agentID, true, 100*time.Millisecond, "")

		if !router.IsHealthy(agentID) {
			t.Error("expected agent to be healthy after successful request")
		}

		health := router.GetHealth(agentID)
		if health.ConsecutiveFailures != 0 {
			t.Errorf("expected 0 consecutive failures, got %d", health.ConsecutiveFailures)
		}
	})

	t.Run("reset health manually", func(t *testing.T) {
		agentID := "agent-2"

		// Mark as unhealthy
		for i := 0; i < router.config.UnhealthyThreshold; i++ {
			router.UpdateHealth(agentID, false, 0, "failed")
		}

		router.ResetHealth(agentID)

		if !router.IsHealthy(agentID) {
			t.Error("expected agent to be healthy after reset")
		}
	})

	t.Run("response time averaging", func(t *testing.T) {
		agentID := "agent-3"

		// First successful request
		router.UpdateHealth(agentID, true, 100*time.Millisecond, "")
		health := router.GetHealth(agentID)
		if health.ResponseTimeAvg != 100*time.Millisecond {
			t.Errorf("expected 100ms avg, got %v", health.ResponseTimeAvg)
		}

		// Second request (exponential moving average)
		router.UpdateHealth(agentID, true, 200*time.Millisecond, "")
		health = router.GetHealth(agentID)
		// EMA: 0.9 * 100 + 0.1 * 200 = 110ms
		expected := time.Duration(float64(100*time.Millisecond)*0.9 + float64(200*time.Millisecond)*0.1)
		if health.ResponseTimeAvg != expected {
			t.Errorf("expected ~%v avg, got %v", expected, health.ResponseTimeAvg)
		}
	})
}

func TestCapabilityRouter_LoadBalancing(t *testing.T) {
	_, router := testSetup(t)

	t.Run("increment and decrement load", func(t *testing.T) {
		agentID := "agent-1"

		if router.GetLoad(agentID) != 0 {
			t.Error("expected initial load to be 0")
		}

		router.IncrementLoad(agentID)
		if router.GetLoad(agentID) != 1 {
			t.Errorf("expected load to be 1, got %d", router.GetLoad(agentID))
		}

		router.IncrementLoad(agentID)
		if router.GetLoad(agentID) != 2 {
			t.Errorf("expected load to be 2, got %d", router.GetLoad(agentID))
		}

		router.DecrementLoad(agentID)
		if router.GetLoad(agentID) != 1 {
			t.Errorf("expected load to be 1, got %d", router.GetLoad(agentID))
		}
	})

	t.Run("decrement does not go below zero", func(t *testing.T) {
		agentID := "agent-2"

		router.DecrementLoad(agentID)
		if router.GetLoad(agentID) != 0 {
			t.Errorf("expected load to remain 0, got %d", router.GetLoad(agentID))
		}
	})

	t.Run("has capacity check", func(t *testing.T) {
		agentID := "agent-1"

		// Reset load
		router.agentLoad[agentID] = 0

		if !router.HasCapacity(agentID) {
			t.Error("expected agent to have capacity when load is 0")
		}

		// Max out the load
		router.agentLoad[agentID] = router.config.MaxConcurrentPerAgent

		if router.HasCapacity(agentID) {
			t.Error("expected agent to NOT have capacity when at max")
		}
	})
}

func TestCapabilityRouter_SelectBestAgent(t *testing.T) {
	_, router := testSetup(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		requirements AgentRequirements
		setup        func()
		wantAgentID  string
		wantNil      bool
	}{
		{
			name: "select by required capability",
			requirements: AgentRequirements{
				RequiredCapabilities: []string{"exec"},
			},
			wantAgentID: "agent-2",
		},
		{
			name: "select with preference",
			requirements: AgentRequirements{
				PreferAgents: []string{"agent-3"},
			},
			wantAgentID: "agent-3", // Preferred agent gets score boost
		},
		{
			name: "exclude agent",
			requirements: AgentRequirements{
				RequiredCapabilities: []string{"read"},
				ExcludeAgents:        []string{"agent-2"},
			},
			wantAgentID: "agent-1", // agent-2 is excluded
		},
		{
			name: "no matching agents returns nil",
			requirements: AgentRequirements{
				RequiredCapabilities: []string{"nonexistent"},
			},
			wantNil: true,
		},
		{
			name: "preferred capability boosts score",
			requirements: AgentRequirements{
				PreferredCapabilities: []string{"coding"},
			},
			wantAgentID: "agent-2", // Has coding capability
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			agent, err := router.SelectBestAgent(ctx, tt.requirements)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil {
				if agent != nil {
					t.Errorf("expected nil, got agent %s", agent.ID)
				}
				return
			}

			if agent == nil {
				t.Fatal("expected agent, got nil")
			}

			if tt.wantAgentID != "" && agent.ID != tt.wantAgentID {
				t.Errorf("expected agent %s, got %s", tt.wantAgentID, agent.ID)
			}
		})
	}
}

func TestCapabilityRouter_SelectBestAgent_HealthFiltering(t *testing.T) {
	_, router := testSetup(t)
	ctx := context.Background()

	// Mark agent-2 as unhealthy
	for i := 0; i < router.config.UnhealthyThreshold; i++ {
		router.UpdateHealth("agent-2", false, 0, "down")
	}

	// Request agent with exec capability (only agent-2 has it)
	agent, err := router.SelectBestAgent(ctx, AgentRequirements{
		RequiredCapabilities: []string{"exec"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since agent-2 is unhealthy, we should get nil
	if agent != nil {
		t.Errorf("expected nil (unhealthy agent filtered), got %s", agent.ID)
	}
}

func TestCapabilityRouter_SelectBestAgent_LoadFiltering(t *testing.T) {
	_, router := testSetup(t)
	ctx := context.Background()

	// Max out agent-1's load
	router.agentLoad["agent-1"] = router.config.MaxConcurrentPerAgent

	agent, err := router.SelectBestAgent(ctx, AgentRequirements{
		RequiredCapabilities: []string{"read"},
		MaxLoad:              0.5, // 50% max load
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// agent-1 is overloaded, so should get agent-2
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.ID == "agent-1" {
		t.Error("expected overloaded agent-1 to be filtered out")
	}
}

func TestCapabilityRouter_LoadBalanceStrategies(t *testing.T) {
	tests := []struct {
		name     string
		strategy LoadBalanceStrategy
	}{
		{name: "round_robin", strategy: StrategyRoundRobin},
		{name: "least_loaded", strategy: StrategyLeastLoaded},
		{name: "random", strategy: StrategyRandom},
		{name: "priority", strategy: StrategyPriority},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := testSetup(t)
			router.config.LoadBalanceStrategy = tt.strategy
			ctx := context.Background()

			// Make multiple selections
			selected := make(map[string]int)
			for i := 0; i < 10; i++ {
				agents, _ := router.RouteByCapability(ctx, "read")
				if len(agents) > 0 {
					// Simulate selection
					agent := router.selectAgent(agents)
					if agent != nil {
						selected[agent.ID]++
					}
				}
			}

			// Just verify we got selections without panicking
			if len(selected) == 0 {
				t.Error("expected some agents to be selected")
			}
		})
	}
}

func TestCapabilityRouter_GetAllAgentHealth(t *testing.T) {
	_, router := testSetup(t)

	// Update some health statuses
	router.UpdateHealth("agent-1", true, 100*time.Millisecond, "")
	router.UpdateHealth("agent-2", false, 0, "error")

	allHealth := router.GetAllAgentHealth()

	if len(allHealth) == 0 {
		t.Error("expected health data for agents")
	}

	// Verify we get copies, not references
	if h, ok := allHealth["agent-1"]; ok {
		h.Healthy = false
		if !router.IsHealthy("agent-1") {
			t.Error("modifying returned health should not affect router state")
		}
	}
}

func TestCapabilityRouter_GetAllAgentLoad(t *testing.T) {
	_, router := testSetup(t)

	router.IncrementLoad("agent-1")
	router.IncrementLoad("agent-1")
	router.IncrementLoad("agent-2")

	allLoad := router.GetAllAgentLoad()

	if allLoad["agent-1"] != 2 {
		t.Errorf("expected agent-1 load to be 2, got %d", allLoad["agent-1"])
	}
	if allLoad["agent-2"] != 1 {
		t.Errorf("expected agent-2 load to be 1, got %d", allLoad["agent-2"])
	}
}

func TestCapabilityRouter_RefreshCapabilityIndex(t *testing.T) {
	orch, router := testSetup(t)

	// Add a new agent to the orchestrator
	newAgent := &AgentDefinition{
		ID:                 "agent-new",
		Name:               "New Agent",
		Tools:              []string{"new-tool"},
		CanReceiveHandoffs: true,
	}
	orch.agents[newAgent.ID] = newAgent

	// Refresh the index
	router.RefreshCapabilityIndex()

	// Verify new agent is in the index
	ctx := context.Background()
	agents, _ := router.RouteByCapability(ctx, "new-tool")
	if len(agents) != 1 || agents[0].ID != "agent-new" {
		t.Error("expected refreshed index to include new agent")
	}
}

func TestCapabilityRouter_FilterHealthy(t *testing.T) {
	_, router := testSetup(t)

	agents := []*AgentDefinition{
		{ID: "agent-1"},
		{ID: "agent-2"},
		{ID: "agent-3"},
	}

	// Mark agent-2 as unhealthy
	for i := 0; i < router.config.UnhealthyThreshold; i++ {
		router.UpdateHealth("agent-2", false, 0, "down")
	}

	healthy := router.filterHealthy(agents)

	if len(healthy) != 2 {
		t.Errorf("expected 2 healthy agents, got %d", len(healthy))
	}

	for _, a := range healthy {
		if a.ID == "agent-2" {
			t.Error("unhealthy agent-2 should be filtered out")
		}
	}
}

func TestCapabilityRouter_SortByLoad(t *testing.T) {
	_, router := testSetup(t)

	agents := []*AgentDefinition{
		{ID: "agent-1"},
		{ID: "agent-2"},
		{ID: "agent-3"},
	}

	// Set different loads
	router.agentLoad["agent-1"] = 5
	router.agentLoad["agent-2"] = 2
	router.agentLoad["agent-3"] = 8

	sorted := router.sortByLoad(agents)

	if sorted[0].ID != "agent-2" {
		t.Errorf("expected agent-2 (load 2) first, got %s", sorted[0].ID)
	}
	if sorted[1].ID != "agent-1" {
		t.Errorf("expected agent-1 (load 5) second, got %s", sorted[1].ID)
	}
	if sorted[2].ID != "agent-3" {
		t.Errorf("expected agent-3 (load 8) third, got %s", sorted[2].ID)
	}
}

func TestCapabilityRouter_EdgeCases(t *testing.T) {
	t.Run("route with nil logger uses default", func(t *testing.T) {
		orch := &Orchestrator{
			config: &MultiAgentConfig{},
			agents: make(map[string]*AgentDefinition),
		}
		router := NewCapabilityRouter(orch, CapabilityRouterConfig{}, nil)
		if router.logger == nil {
			t.Error("expected default logger to be set")
		}
	})

	t.Run("select agent from empty list", func(t *testing.T) {
		_, router := testSetup(t)
		agent := router.selectAgent([]*AgentDefinition{})
		if agent != nil {
			t.Error("expected nil from empty agent list")
		}
	})

	t.Run("route with fallback - all agents unhealthy", func(t *testing.T) {
		_, router := testSetup(t)
		ctx := context.Background()

		router.RegisterFallbackChain("test", []string{"agent-1", "agent-2", "agent-3"})

		// Mark all agents unhealthy
		for _, id := range []string{"agent-1", "agent-2", "agent-3"} {
			for i := 0; i < router.config.UnhealthyThreshold; i++ {
				router.UpdateHealth(id, false, 0, "down")
			}
		}

		agent, _ := router.RouteWithFallback(ctx, "test", []string{"nonexistent"})
		if agent != nil {
			t.Errorf("expected nil when all fallback agents are unhealthy, got %s", agent.ID)
		}
	})

	t.Run("route with fallback - all agents overloaded", func(t *testing.T) {
		_, router := testSetup(t)
		ctx := context.Background()

		router.RegisterFallbackChain("test", []string{"agent-1", "agent-2"})

		// Max out all agents
		router.agentLoad["agent-1"] = router.config.MaxConcurrentPerAgent
		router.agentLoad["agent-2"] = router.config.MaxConcurrentPerAgent

		agent, _ := router.RouteWithFallback(ctx, "test", []string{"nonexistent"})
		if agent != nil {
			t.Errorf("expected nil when all fallback agents are overloaded, got %s", agent.ID)
		}
	})
}

func TestHasCapabilityInMetadata(t *testing.T) {
	tests := []struct {
		name       string
		agent      *AgentDefinition
		capability string
		want       bool
	}{
		{
			name: "capability exists",
			agent: &AgentDefinition{
				ID: "test",
				Metadata: map[string]any{
					"capabilities": []string{"coding", "debugging"},
				},
			},
			capability: "coding",
			want:       true,
		},
		{
			name: "capability case insensitive",
			agent: &AgentDefinition{
				ID: "test",
				Metadata: map[string]any{
					"capabilities": []string{"Coding"},
				},
			},
			capability: "coding",
			want:       true,
		},
		{
			name: "capability not found",
			agent: &AgentDefinition{
				ID: "test",
				Metadata: map[string]any{
					"capabilities": []string{"coding"},
				},
			},
			capability: "testing",
			want:       false,
		},
		{
			name: "nil metadata",
			agent: &AgentDefinition{
				ID: "test",
			},
			capability: "coding",
			want:       false,
		},
		{
			name: "wrong metadata type",
			agent: &AgentDefinition{
				ID: "test",
				Metadata: map[string]any{
					"capabilities": "not-a-slice",
				},
			},
			capability: "coding",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasCapabilityInMetadata(tt.agent, tt.capability)
			if got != tt.want {
				t.Errorf("hasCapabilityInMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadBalanceStrategy_Values(t *testing.T) {
	// Verify strategy constants have expected values
	tests := []struct {
		strategy LoadBalanceStrategy
		expected string
	}{
		{StrategyRoundRobin, "round_robin"},
		{StrategyLeastLoaded, "least_loaded"},
		{StrategyRandom, "random"},
		{StrategyPriority, "priority"},
	}

	for _, tt := range tests {
		if string(tt.strategy) != tt.expected {
			t.Errorf("strategy %s != expected %s", tt.strategy, tt.expected)
		}
	}
}
