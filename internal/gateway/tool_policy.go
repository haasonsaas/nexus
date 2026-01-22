package gateway

import (
	"context"

	"github.com/haasonsaas/nexus/internal/tools/policy"
)

func (s *Server) toolPolicyForAgent(ctx context.Context, agentID string) *policy.Policy {
	if s == nil || s.stores.Agents == nil || agentID == "" {
		return nil
	}
	agent, err := s.stores.Agents.Get(ctx, agentID)
	if err != nil || agent == nil {
		return nil
	}
	if len(agent.Tools) == 0 {
		return nil
	}
	return &policy.Policy{Allow: agent.Tools}
}
