package multiagent

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// CapabilityRouter extends the basic Router with capability-based agent selection,
// fallback chains, and load balancing. Follows Clawdbot patterns for intelligent routing.
//
// Key features:
//   - Capability matching: Route to agents based on required capabilities
//   - Fallback chains: Define ordered fallback sequences for resilience
//   - Health-aware routing: Skip unhealthy agents
//   - Load balancing: Distribute load across capable agents
type CapabilityRouter struct {
	*Router

	logger *slog.Logger

	// Capability index for efficient lookup
	capabilityIndex map[string][]*AgentDefinition // capability -> agents

	// Fallback chains define ordered sequences of agents to try
	fallbackChains map[string][]string // chainName -> []agentIDs

	// Agent health tracking
	healthMu    sync.RWMutex
	agentHealth map[string]*AgentHealth

	// Load balancing
	loadMu    sync.RWMutex
	agentLoad map[string]int // agentID -> active request count

	// Configuration
	config CapabilityRouterConfig
}

// CapabilityRouterConfig configures the capability router.
type CapabilityRouterConfig struct {
	// EnableHealthChecks activates agent health monitoring.
	EnableHealthChecks bool `json:"enable_health_checks" yaml:"enable_health_checks"`

	// HealthCheckInterval is how often to check agent health.
	HealthCheckInterval time.Duration `json:"health_check_interval" yaml:"health_check_interval"`

	// UnhealthyThreshold is consecutive failures before marking unhealthy.
	UnhealthyThreshold int `json:"unhealthy_threshold" yaml:"unhealthy_threshold"`

	// EnableLoadBalancing activates load-based routing.
	EnableLoadBalancing bool `json:"enable_load_balancing" yaml:"enable_load_balancing"`

	// MaxConcurrentPerAgent limits concurrent requests per agent.
	MaxConcurrentPerAgent int `json:"max_concurrent_per_agent" yaml:"max_concurrent_per_agent"`

	// LoadBalanceStrategy is the strategy for distributing load.
	LoadBalanceStrategy LoadBalanceStrategy `json:"load_balance_strategy" yaml:"load_balance_strategy"`

	// DefaultFallbackChain is used when no specific chain matches.
	DefaultFallbackChain string `json:"default_fallback_chain" yaml:"default_fallback_chain"`

	// EnableCapabilityMatching activates capability-based routing.
	EnableCapabilityMatching bool `json:"enable_capability_matching" yaml:"enable_capability_matching"`
}

// LoadBalanceStrategy defines how load is distributed.
type LoadBalanceStrategy string

const (
	// StrategyRoundRobin cycles through agents.
	StrategyRoundRobin LoadBalanceStrategy = "round_robin"

	// StrategyLeastLoaded picks the agent with least active requests.
	StrategyLeastLoaded LoadBalanceStrategy = "least_loaded"

	// StrategyRandom picks a random capable agent.
	StrategyRandom LoadBalanceStrategy = "random"

	// StrategyPriority uses agent priority ordering.
	StrategyPriority LoadBalanceStrategy = "priority"
)

// AgentHealth tracks an agent's health status.
type AgentHealth struct {
	AgentID             string        `json:"agent_id"`
	Healthy             bool          `json:"healthy"`
	LastCheck           time.Time     `json:"last_check"`
	LastSuccess         time.Time     `json:"last_success"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	FailureReason       string        `json:"failure_reason,omitempty"`
	ResponseTimeAvg     time.Duration `json:"response_time_avg"`
}

// NewCapabilityRouter creates a new capability-aware router.
func NewCapabilityRouter(orchestrator *Orchestrator, config CapabilityRouterConfig, logger *slog.Logger) *CapabilityRouter {
	if logger == nil {
		logger = slog.Default()
	}

	r := &CapabilityRouter{
		Router:          NewRouter(orchestrator),
		logger:          logger.With("component", "capability-router"),
		capabilityIndex: make(map[string][]*AgentDefinition),
		fallbackChains:  make(map[string][]string),
		agentHealth:     make(map[string]*AgentHealth),
		agentLoad:       make(map[string]int),
		config:          config,
	}

	// Build capability index
	r.rebuildCapabilityIndex()

	return r
}

// rebuildCapabilityIndex builds the capability lookup index.
func (r *CapabilityRouter) rebuildCapabilityIndex() {
	r.capabilityIndex = make(map[string][]*AgentDefinition)

	for _, agent := range r.orchestrator.ListAgents() {
		// Index by tools (capabilities)
		for _, tool := range agent.Tools {
			r.capabilityIndex[tool] = append(r.capabilityIndex[tool], agent)
		}

		// Index by capabilities from metadata
		if caps, ok := agent.Metadata["capabilities"].([]string); ok {
			for _, cap := range caps {
				r.capabilityIndex[cap] = append(r.capabilityIndex[cap], agent)
			}
		}

		// Initialize health status
		r.agentHealth[agent.ID] = &AgentHealth{
			AgentID:     agent.ID,
			Healthy:     true,
			LastCheck:   time.Now(),
			LastSuccess: time.Now(),
		}
	}
}

// RegisterFallbackChain registers a named fallback chain.
func (r *CapabilityRouter) RegisterFallbackChain(name string, agentIDs []string) {
	r.fallbackChains[name] = agentIDs
	r.logger.Info("registered fallback chain", "name", name, "agents", agentIDs)
}

// RouteByCapability finds agents that have a specific capability.
func (r *CapabilityRouter) RouteByCapability(ctx context.Context, capability string) ([]*AgentDefinition, error) {
	agents := r.capabilityIndex[capability]
	if len(agents) == 0 {
		return nil, nil
	}

	// Filter by health
	if r.config.EnableHealthChecks {
		agents = r.filterHealthy(agents)
	}

	// Apply load balancing
	if r.config.EnableLoadBalancing {
		agents = r.sortByLoad(agents)
	}

	return agents, nil
}

// RouteByCapabilities finds agents that have ALL specified capabilities.
func (r *CapabilityRouter) RouteByCapabilities(ctx context.Context, capabilities []string) ([]*AgentDefinition, error) {
	if len(capabilities) == 0 {
		return r.orchestrator.ListAgents(), nil
	}

	// Start with agents having the first capability
	candidates := r.capabilityIndex[capabilities[0]]
	if len(candidates) == 0 {
		return nil, nil
	}

	// Filter to agents having all capabilities
	for _, cap := range capabilities[1:] {
		capAgents := make(map[string]bool)
		for _, agent := range r.capabilityIndex[cap] {
			capAgents[agent.ID] = true
		}

		var filtered []*AgentDefinition
		for _, agent := range candidates {
			if capAgents[agent.ID] {
				filtered = append(filtered, agent)
			}
		}
		candidates = filtered
	}

	// Filter by health
	if r.config.EnableHealthChecks {
		candidates = r.filterHealthy(candidates)
	}

	// Apply load balancing
	if r.config.EnableLoadBalancing {
		candidates = r.sortByLoad(candidates)
	}

	return candidates, nil
}

// RouteWithFallback attempts routing with a fallback chain.
func (r *CapabilityRouter) RouteWithFallback(ctx context.Context, chainName string, capabilities []string) (*AgentDefinition, error) {
	// Try capability-based routing first
	if len(capabilities) > 0 {
		agents, err := r.RouteByCapabilities(ctx, capabilities)
		if err == nil && len(agents) > 0 {
			return r.selectAgent(agents), nil
		}
	}

	// Use fallback chain
	chain, ok := r.fallbackChains[chainName]
	if !ok {
		// Try default chain
		chain = r.fallbackChains[r.config.DefaultFallbackChain]
	}

	for _, agentID := range chain {
		agent, ok := r.orchestrator.GetAgent(agentID)
		if !ok {
			continue
		}

		// Check health
		if r.config.EnableHealthChecks && !r.IsHealthy(agentID) {
			r.logger.Debug("skipping unhealthy agent in fallback", "agent_id", agentID)
			continue
		}

		// Check load
		if r.config.EnableLoadBalancing && !r.HasCapacity(agentID) {
			r.logger.Debug("skipping overloaded agent in fallback", "agent_id", agentID)
			continue
		}

		return agent, nil
	}

	return nil, nil
}

// SelectBestAgent selects the best agent for a request based on all factors.
func (r *CapabilityRouter) SelectBestAgent(ctx context.Context, requirements AgentRequirements) (*AgentDefinition, error) {
	var candidates []*AgentDefinition

	// Filter by required capabilities
	if len(requirements.RequiredCapabilities) > 0 {
		var err error
		candidates, err = r.RouteByCapabilities(ctx, requirements.RequiredCapabilities)
		if err != nil {
			return nil, err
		}
	} else {
		candidates = r.orchestrator.ListAgents()
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Filter by preferred capabilities (boost score)
	scored := r.scoreAgents(candidates, requirements)

	// Sort by score
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if len(scored) > 0 {
		return scored[0].agent, nil
	}

	return nil, nil
}

// AgentRequirements specifies what an agent needs to have.
type AgentRequirements struct {
	// RequiredCapabilities must all be present.
	RequiredCapabilities []string `json:"required_capabilities"`

	// PreferredCapabilities boost the agent's score if present.
	PreferredCapabilities []string `json:"preferred_capabilities"`

	// ExcludeAgents lists agents to skip.
	ExcludeAgents []string `json:"exclude_agents"`

	// PreferAgents lists agents to prefer (higher score).
	PreferAgents []string `json:"prefer_agents"`

	// MinHealthScore is the minimum health score (0.0 to 1.0).
	MinHealthScore float64 `json:"min_health_score"`

	// MaxLoad is the maximum acceptable load percentage (0.0 to 1.0).
	MaxLoad float64 `json:"max_load"`
}

type scoredAgent struct {
	agent *AgentDefinition
	score float64
}

func (r *CapabilityRouter) scoreAgents(agents []*AgentDefinition, req AgentRequirements) []scoredAgent {
	var scored []scoredAgent

	excludeSet := make(map[string]bool)
	for _, id := range req.ExcludeAgents {
		excludeSet[id] = true
	}

	preferSet := make(map[string]bool)
	for _, id := range req.PreferAgents {
		preferSet[id] = true
	}

	for _, agent := range agents {
		// Skip excluded agents
		if excludeSet[agent.ID] {
			continue
		}

		score := 1.0

		// Boost for preferred agents
		if preferSet[agent.ID] {
			score += 2.0
		}

		// Boost for preferred capabilities
		for _, cap := range req.PreferredCapabilities {
			if agent.HasTool(cap) || hasCapabilityInMetadata(agent, cap) {
				score += 0.5
			}
		}

		// Factor in health
		if r.config.EnableHealthChecks {
			health := r.GetHealth(agent.ID)
			if health != nil {
				if !health.Healthy {
					continue // Skip unhealthy
				}
				healthScore := 1.0 - float64(health.ConsecutiveFailures)*0.1
				if healthScore < req.MinHealthScore {
					continue
				}
				score *= healthScore
			}
		}

		// Factor in load
		if r.config.EnableLoadBalancing {
			load := r.GetLoad(agent.ID)
			maxLoad := r.config.MaxConcurrentPerAgent
			if maxLoad > 0 {
				loadPct := float64(load) / float64(maxLoad)
				if loadPct > req.MaxLoad && req.MaxLoad > 0 {
					continue
				}
				score *= (1.0 - loadPct*0.5) // Reduce score for loaded agents
			}
		}

		scored = append(scored, scoredAgent{agent: agent, score: score})
	}

	return scored
}

// Health management methods

// UpdateHealth updates an agent's health status.
func (r *CapabilityRouter) UpdateHealth(agentID string, success bool, responseTime time.Duration, failureReason string) {
	r.healthMu.Lock()
	defer r.healthMu.Unlock()

	health, ok := r.agentHealth[agentID]
	if !ok {
		health = &AgentHealth{AgentID: agentID}
		r.agentHealth[agentID] = health
	}

	health.LastCheck = time.Now()

	if success {
		health.ConsecutiveFailures = 0
		health.LastSuccess = time.Now()
		health.Healthy = true
		health.FailureReason = ""
		// Update average response time (exponential moving average)
		if health.ResponseTimeAvg == 0 {
			health.ResponseTimeAvg = responseTime
		} else {
			health.ResponseTimeAvg = time.Duration(float64(health.ResponseTimeAvg)*0.9 + float64(responseTime)*0.1)
		}
	} else {
		health.ConsecutiveFailures++
		health.FailureReason = failureReason
		if health.ConsecutiveFailures >= r.config.UnhealthyThreshold {
			health.Healthy = false
			r.logger.Warn("agent marked unhealthy",
				"agent_id", agentID,
				"consecutive_failures", health.ConsecutiveFailures,
				"reason", failureReason,
			)
		}
	}
}

// IsHealthy checks if an agent is healthy.
func (r *CapabilityRouter) IsHealthy(agentID string) bool {
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()

	health, ok := r.agentHealth[agentID]
	if !ok {
		return true // Unknown agents are assumed healthy
	}
	return health.Healthy
}

// GetHealth returns an agent's health status.
func (r *CapabilityRouter) GetHealth(agentID string) *AgentHealth {
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()

	health, ok := r.agentHealth[agentID]
	if !ok {
		return nil
	}
	// Return copy
	h := *health
	return &h
}

// ResetHealth resets an agent's health to healthy.
func (r *CapabilityRouter) ResetHealth(agentID string) {
	r.healthMu.Lock()
	defer r.healthMu.Unlock()

	if health, ok := r.agentHealth[agentID]; ok {
		health.Healthy = true
		health.ConsecutiveFailures = 0
		health.FailureReason = ""
		health.LastCheck = time.Now()
	}
}

// Load management methods

// IncrementLoad increments the load counter for an agent.
func (r *CapabilityRouter) IncrementLoad(agentID string) {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	r.agentLoad[agentID]++
}

// DecrementLoad decrements the load counter for an agent.
func (r *CapabilityRouter) DecrementLoad(agentID string) {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	if r.agentLoad[agentID] > 0 {
		r.agentLoad[agentID]--
	}
}

// GetLoad returns the current load for an agent.
func (r *CapabilityRouter) GetLoad(agentID string) int {
	r.loadMu.RLock()
	defer r.loadMu.RUnlock()
	return r.agentLoad[agentID]
}

// HasCapacity checks if an agent has capacity for more requests.
func (r *CapabilityRouter) HasCapacity(agentID string) bool {
	if r.config.MaxConcurrentPerAgent <= 0 {
		return true
	}
	return r.GetLoad(agentID) < r.config.MaxConcurrentPerAgent
}

// Helper methods

func (r *CapabilityRouter) filterHealthy(agents []*AgentDefinition) []*AgentDefinition {
	var healthy []*AgentDefinition
	for _, agent := range agents {
		if r.IsHealthy(agent.ID) {
			healthy = append(healthy, agent)
		}
	}
	return healthy
}

func (r *CapabilityRouter) sortByLoad(agents []*AgentDefinition) []*AgentDefinition {
	sorted := make([]*AgentDefinition, len(agents))
	copy(sorted, agents)

	sort.Slice(sorted, func(i, j int) bool {
		return r.GetLoad(sorted[i].ID) < r.GetLoad(sorted[j].ID)
	})

	return sorted
}

func (r *CapabilityRouter) selectAgent(agents []*AgentDefinition) *AgentDefinition {
	if len(agents) == 0 {
		return nil
	}

	switch r.config.LoadBalanceStrategy {
	case StrategyLeastLoaded:
		return r.sortByLoad(agents)[0]
	case StrategyPriority:
		// Agents are already sorted by priority in the orchestrator
		return agents[0]
	case StrategyRandom:
		// Use a simple selection based on time
		idx := time.Now().UnixNano() % int64(len(agents))
		return agents[idx]
	default: // StrategyRoundRobin
		// Simple round-robin based on total request count
		r.loadMu.RLock()
		total := 0
		for _, agent := range agents {
			total += r.agentLoad[agent.ID]
		}
		r.loadMu.RUnlock()
		idx := total % len(agents)
		return agents[idx]
	}
}

func hasCapabilityInMetadata(agent *AgentDefinition, capability string) bool {
	if agent.Metadata == nil {
		return false
	}
	caps, ok := agent.Metadata["capabilities"].([]string)
	if !ok {
		return false
	}
	for _, cap := range caps {
		if strings.EqualFold(cap, capability) {
			return true
		}
	}
	return false
}

// GetAllAgentHealth returns health status for all agents.
func (r *CapabilityRouter) GetAllAgentHealth() map[string]*AgentHealth {
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()

	result := make(map[string]*AgentHealth, len(r.agentHealth))
	for id, health := range r.agentHealth {
		h := *health
		result[id] = &h
	}
	return result
}

// GetAllAgentLoad returns load for all agents.
func (r *CapabilityRouter) GetAllAgentLoad() map[string]int {
	r.loadMu.RLock()
	defer r.loadMu.RUnlock()

	result := make(map[string]int, len(r.agentLoad))
	for id, load := range r.agentLoad {
		result[id] = load
	}
	return result
}

// RefreshCapabilityIndex rebuilds the capability index.
// Call this after agent configuration changes.
func (r *CapabilityRouter) RefreshCapabilityIndex() {
	r.rebuildCapabilityIndex()
}
