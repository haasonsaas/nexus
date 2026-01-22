package gateway

import (
	"encoding/json"

	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

func toolPolicyFromAgent(agentModel *models.Agent) *policy.Policy {
	if agentModel == nil {
		return nil
	}
	toolPolicy := parseAgentToolPolicy(agentModel.Config)
	if toolPolicy == nil && len(agentModel.Tools) == 0 {
		return nil
	}
	if len(agentModel.Tools) > 0 {
		toolPolicy = policy.Merge(toolPolicy, &policy.Policy{Allow: agentModel.Tools})
	}
	return toolPolicy
}

func parseAgentToolPolicy(cfg map[string]any) *policy.Policy {
	if len(cfg) == 0 {
		return nil
	}
	raw, ok := cfg["tool_policy"]
	if !ok || raw == nil {
		return nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var pol policy.Policy
	if err := json.Unmarshal(payload, &pol); err != nil {
		return nil
	}
	if pol.Profile == "" && len(pol.Allow) == 0 && len(pol.Deny) == 0 && len(pol.ByProvider) == 0 {
		return nil
	}
	return &pol
}
