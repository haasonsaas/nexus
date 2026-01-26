package multiagent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// SwarmRole describes an agent's role in swarm execution.
type SwarmRole string

const (
	RoleGatherer    SwarmRole = "gatherer"
	RoleProcessor   SwarmRole = "processor"
	RoleSynthesizer SwarmRole = "synthesizer"
	RoleValidator   SwarmRole = "validator"
)

// SwarmConfig configures swarm execution behavior.
type SwarmConfig struct {
	Enabled           bool               `json:"enabled" yaml:"enabled"`
	MaxParallelAgents int                `json:"max_parallel_agents,omitempty" yaml:"max_parallel_agents"`
	SharedContext     SwarmSharedContextConfig `json:"shared_context,omitempty" yaml:"shared_context"`
}

type SwarmSharedContextConfig struct {
	Backend string        `json:"backend,omitempty" yaml:"backend"` // memory, redis (future)
	TTL     time.Duration `json:"ttl,omitempty" yaml:"ttl"`
}

// SwarmContextUpdate represents a published swarm context update.
type SwarmContextUpdate struct {
	AgentID    string    `json:"agent_id"`
	Data       any       `json:"data"`
	OccurredAt time.Time `json:"occurred_at"`
}

// SwarmSharedContext is the coordination channel for swarm agent outputs.
type SwarmSharedContext interface {
	Publish(agentID string, data any)
	GetFromAgent(agentID string) (any, bool)
	Subscribe() <-chan SwarmContextUpdate
	Close()
}

// InMemorySwarmContext is a best-effort in-memory implementation of SwarmSharedContext.
// It stores the latest published value per agent and exposes a single updates stream.
type InMemorySwarmContext struct {
	mu      sync.RWMutex
	latest  map[string]any
	updates chan SwarmContextUpdate
	closed  bool
}

func NewInMemorySwarmContext(buffer int) *InMemorySwarmContext {
	if buffer <= 0 {
		buffer = 32
	}
	return &InMemorySwarmContext{
		latest:  make(map[string]any),
		updates: make(chan SwarmContextUpdate, buffer),
	}
}

func (c *InMemorySwarmContext) Publish(agentID string, data any) {
	if c == nil {
		return
	}
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.latest[id] = data
	update := SwarmContextUpdate{AgentID: id, Data: data, OccurredAt: time.Now()}
	updates := c.updates
	c.mu.Unlock()

	select {
	case updates <- update:
	default:
		// Best-effort: avoid blocking the swarm executor.
	}
}

func (c *InMemorySwarmContext) GetFromAgent(agentID string) (any, bool) {
	if c == nil {
		return nil, false
	}
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.latest[id]
	return v, ok
}

func (c *InMemorySwarmContext) Subscribe() <-chan SwarmContextUpdate {
	if c == nil {
		ch := make(chan SwarmContextUpdate)
		close(ch)
		return ch
	}
	return c.updates
}

func (c *InMemorySwarmContext) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.updates)
	c.mu.Unlock()
}

// DependencyGraph is a stage-oriented representation of agent dependencies.
// Each stage can be executed in parallel, and later stages depend on prior stages.
type DependencyGraph struct {
	stages [][]string
}

func (g *DependencyGraph) Stages() [][]string {
	if g == nil {
		return nil
	}
	out := make([][]string, len(g.stages))
	for i := range g.stages {
		out[i] = append([]string(nil), g.stages[i]...)
	}
	return out
}

// BuildDependencyGraph computes a stage-ordered execution plan based on AgentDefinition.DependsOn.
func BuildDependencyGraph(agents []AgentDefinition) (*DependencyGraph, error) {
	if len(agents) == 0 {
		return &DependencyGraph{}, nil
	}

	byID := make(map[string]AgentDefinition, len(agents))
	indegree := make(map[string]int, len(agents))
	dependents := make(map[string][]string, len(agents))

	for _, a := range agents {
		id := strings.TrimSpace(a.ID)
		if id == "" {
			return nil, fmt.Errorf("agent id cannot be empty")
		}
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("duplicate agent id %q", id)
		}
		byID[id] = a
		indegree[id] = 0
	}

	for _, a := range agents {
		id := strings.TrimSpace(a.ID)
		for _, depRaw := range a.DependsOn {
			dep := strings.TrimSpace(depRaw)
			if dep == "" {
				continue
			}
			if _, ok := byID[dep]; !ok {
				return nil, fmt.Errorf("agent %q depends on unknown agent %q", id, dep)
			}
			indegree[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}

	ready := make([]string, 0)
	for id, deg := range indegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	processed := 0
	var stages [][]string

	for len(ready) > 0 {
		stage := append([]string(nil), ready...)
		sort.Strings(stage)
		stages = append(stages, stage)

		next := make([]string, 0)
		for _, id := range stage {
			processed++
			for _, dep := range dependents[id] {
				indegree[dep]--
				if indegree[dep] == 0 {
					next = append(next, dep)
				}
			}
		}
		sort.Strings(next)
		ready = next
	}

	if processed != len(byID) {
		return nil, fmt.Errorf("dependency cycle detected")
	}

	return &DependencyGraph{stages: stages}, nil
}

// SwarmExecutor runs an agent in swarm mode.
type SwarmExecutor func(ctx context.Context, agentID string, shared SwarmSharedContext) (any, error)

type SwarmAgentResult struct {
	AgentID string
	Output  any
	Err     error
}

type SwarmResult struct {
	Results []SwarmAgentResult
}

// Swarm executes a dependency graph with bounded parallelism and a shared context.
type Swarm struct {
	cfg   SwarmConfig
	graph *DependencyGraph
}

func NewSwarm(cfg SwarmConfig, agents []AgentDefinition) (*Swarm, error) {
	if cfg.MaxParallelAgents <= 0 {
		cfg.MaxParallelAgents = 5
	}
	graph, err := BuildDependencyGraph(agents)
	if err != nil {
		return nil, err
	}
	return &Swarm{cfg: cfg, graph: graph}, nil
}

func (s *Swarm) Execute(ctx context.Context, exec SwarmExecutor) (*SwarmResult, error) {
	if s == nil {
		return nil, fmt.Errorf("swarm is nil")
	}
	if exec == nil {
		return nil, fmt.Errorf("executor is nil")
	}

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	shared := NewInMemorySwarmContext(128)
	defer shared.Close()

	sem := make(chan struct{}, s.cfg.MaxParallelAgents)
	var (
		mu      sync.Mutex
		results []SwarmAgentResult
		firstErr error
	)

	for _, stage := range s.graph.Stages() {
		var wg sync.WaitGroup
		for _, agentID := range stage {
			id := agentID
			wg.Add(1)
			go func() {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
				case <-execCtx.Done():
					mu.Lock()
					results = append(results, SwarmAgentResult{AgentID: id, Err: execCtx.Err()})
					mu.Unlock()
					return
				}
				defer func() { <-sem }()

				out, err := exec(execCtx, id, shared)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
				}

				shared.Publish(id, out)
				mu.Lock()
				results = append(results, SwarmAgentResult{AgentID: id, Output: out, Err: err})
				mu.Unlock()
			}()
		}
		wg.Wait()

		if execCtx.Err() != nil {
			break
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].AgentID < results[j].AgentID })
	return &SwarmResult{Results: results}, firstErr
}
