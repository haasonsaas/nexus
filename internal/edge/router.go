package edge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// SelectionStrategy controls how a candidate edge is selected.
type SelectionStrategy string

const (
	StrategyLeastBusy  SelectionStrategy = "least_busy"
	StrategyRoundRobin SelectionStrategy = "round_robin"
	StrategyRandom     SelectionStrategy = "random"
)

// SelectionCriteria describes how to choose an edge.
type SelectionCriteria struct {
	ToolName     string
	ChannelType  string
	Capabilities *pb.EdgeCapabilities
	Metadata     map[string]string
	Strategy     SelectionStrategy
}

// ExecuteToolAny selects an edge by criteria and executes the tool.
func (m *Manager) ExecuteToolAny(ctx context.Context, criteria SelectionCriteria, input string, opts ExecuteOptions) (*ToolExecutionResult, error) {
	toolName := strings.TrimSpace(criteria.ToolName)
	if toolName == "" {
		return nil, errors.New("tool_name is required for invoke_any")
	}
	edge, err := m.SelectEdge(criteria)
	if err != nil {
		return nil, err
	}
	return m.ExecuteTool(ctx, edge.ID, toolName, input, opts)
}

// SelectEdge selects a single edge matching the criteria.
func (m *Manager) SelectEdge(criteria SelectionCriteria) (*EdgeConnection, error) {
	candidates := m.listCandidates(criteria)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no edges match criteria")
	}

	strategy := normalizeStrategy(criteria.Strategy)
	switch strategy {
	case StrategyRoundRobin:
		idx := int(atomic.AddUint64(&m.rrCounter, 1)-1) % len(candidates)
		return candidates[idx].conn, nil
	case StrategyRandom:
		m.randMu.Lock()
		idx := m.rand.Intn(len(candidates))
		m.randMu.Unlock()
		return candidates[idx].conn, nil
	default:
		// least_busy
		best := candidates[0]
		for i := 1; i < len(candidates); i++ {
			if candidates[i].load < best.load {
				best = candidates[i]
				continue
			}
			if candidates[i].load == best.load && candidates[i].conn.ID < best.conn.ID {
				best = candidates[i]
			}
		}
		return best.conn, nil
	}
}

type edgeCandidate struct {
	conn *EdgeConnection
	load float64
}

func (m *Manager) listCandidates(criteria SelectionCriteria) []edgeCandidate {
	m.mu.RLock()
	edges := make([]*EdgeConnection, 0, len(m.edges))
	for _, conn := range m.edges {
		edges = append(edges, conn)
	}
	m.mu.RUnlock()

	sort.Slice(edges, func(i, j int) bool {
		return edges[i].ID < edges[j].ID
	})

	toolName := strings.TrimSpace(criteria.ToolName)
	channelType := strings.TrimSpace(criteria.ChannelType)
	meta := criteria.Metadata

	out := make([]edgeCandidate, 0, len(edges))
	for _, conn := range edges {
		if !edgeMatches(conn, toolName, channelType, criteria.Capabilities, meta) {
			continue
		}
		out = append(out, edgeCandidate{
			conn: conn,
			load: m.edgeLoad(conn),
		})
	}
	return out
}

func edgeMatches(conn *EdgeConnection, toolName, channelType string, caps *pb.EdgeCapabilities, metadata map[string]string) bool {
	conn.mu.RLock()
	defer conn.mu.RUnlock()

	if toolName != "" {
		if _, ok := conn.Tools[toolName]; !ok {
			return false
		}
	}

	if channelType != "" {
		match := false
		for _, ct := range conn.ChannelTypes {
			if ct == channelType {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	if caps != nil && !capabilitiesMatch(caps, conn.Capabilities) {
		return false
	}

	if len(metadata) > 0 {
		for key, val := range metadata {
			if strings.TrimSpace(key) == "" {
				continue
			}
			if strings.TrimSpace(val) == "" {
				continue
			}
			if conn.Metadata == nil || conn.Metadata[key] != val {
				return false
			}
		}
	}

	return true
}

func capabilitiesMatch(required, actual *pb.EdgeCapabilities) bool {
	if required == nil {
		return true
	}
	if actual == nil {
		return false
	}
	if required.Tools && !actual.Tools {
		return false
	}
	if required.Channels && !actual.Channels {
		return false
	}
	if required.Streaming && !actual.Streaming {
		return false
	}
	if required.Artifacts && !actual.Artifacts {
		return false
	}
	return true
}

func (m *Manager) edgeLoad(conn *EdgeConnection) float64 {
	conn.mu.RLock()
	defer conn.mu.RUnlock()

	active := float64(0)
	if conn.Metrics != nil {
		active = float64(conn.Metrics.ActiveToolCount)
	}
	max := float64(m.config.MaxConcurrentTools)
	if max <= 0 {
		max = 1
	}
	return active / max
}

func normalizeStrategy(strategy SelectionStrategy) SelectionStrategy {
	if strategy == "" {
		return StrategyLeastBusy
	}
	switch SelectionStrategy(strings.ToLower(string(strategy))) {
	case StrategyRoundRobin:
		return StrategyRoundRobin
	case StrategyRandom:
		return StrategyRandom
	default:
		return StrategyLeastBusy
	}
}
