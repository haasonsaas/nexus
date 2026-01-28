package gateway

import (
	"encoding/json"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
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

func toolPolicyFromMessage(msg *models.Message) *policy.Policy {
	if msg == nil || len(msg.Metadata) == 0 {
		return nil
	}
	raw, ok := msg.Metadata["tool_policy"]
	if !ok || raw == nil {
		return nil
	}
	return parseToolPolicy(raw)
}

func toolPolicyFromConfig(cfg config.ToolPoliciesConfig, msg *models.Message, peerID string) *policy.Policy {
	if strings.TrimSpace(cfg.Default) == "" && len(cfg.Rules) == 0 {
		return nil
	}

	defaultMode := strings.ToLower(strings.TrimSpace(cfg.Default))
	if defaultMode == "" {
		defaultMode = "allow"
	}

	var allow []string
	var deny []string
	for _, rule := range cfg.Rules {
		if !toolPolicyRuleMatches(msg, peerID, rule) {
			continue
		}
		tool := strings.TrimSpace(rule.Tool)
		if tool == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rule.Action)) {
		case "deny":
			deny = append(deny, tool)
		case "allow":
			allow = append(allow, tool)
		default:
			continue
		}
	}

	switch defaultMode {
	case "deny":
		return &policy.Policy{
			Allow: allow,
			Deny:  deny,
		}
	default:
		if len(deny) == 0 {
			return nil
		}
		return &policy.Policy{
			Profile: policy.ProfileFull,
			Deny:    deny,
		}
	}
}

func toolPolicyRuleMatches(msg *models.Message, peerID string, rule config.ToolPolicyRule) bool {
	if msg == nil {
		return false
	}
	if len(rule.Channels) == 0 {
		return true
	}

	channel := strings.ToLower(strings.TrimSpace(string(msg.Channel)))
	channelID := strings.TrimSpace(msg.ChannelID)

	for _, entry := range rule.Channels {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if entry == "*" {
			return true
		}

		if strings.Contains(entry, ":") {
			parts := strings.SplitN(entry, ":", 2)
			entryChannel := strings.ToLower(strings.TrimSpace(parts[0]))
			entryTarget := strings.TrimSpace(parts[1])
			if entryChannel != channel {
				continue
			}
			if entryTarget == "*" {
				return true
			}
			if peerID != "" && entryTarget == peerID {
				return true
			}
			if channelID != "" && entryTarget == channelID {
				return true
			}
			continue
		}

		if strings.ToLower(entry) == channel {
			return true
		}
	}

	return false
}

func (s *Server) resolveToolPolicy(agentModel *models.Agent, msg *models.Message) *policy.Policy {
	var global *policy.Policy
	if s != nil && s.config != nil {
		global = toolPolicyFromConfig(s.config.Tools.Policies, msg, s.extractPeerID(msg))
	}

	policies := make([]*policy.Policy, 0, 3)
	if global != nil {
		policies = append(policies, global)
	}
	if agentPolicy := toolPolicyFromAgent(agentModel); agentPolicy != nil {
		policies = append(policies, agentPolicy)
	}
	if msgPolicy := toolPolicyFromMessage(msg); msgPolicy != nil {
		policies = append(policies, msgPolicy)
	}
	if len(policies) == 0 {
		return nil
	}
	return policy.Merge(policies...)
}

func parseAgentToolPolicy(cfg map[string]any) *policy.Policy {
	if len(cfg) == 0 {
		return nil
	}
	raw, ok := cfg["tool_policy"]
	if !ok || raw == nil {
		return nil
	}
	return parseToolPolicy(raw)
}

func parseToolPolicy(raw any) *policy.Policy {
	if raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case policy.Policy:
		return &value
	case *policy.Policy:
		return value
	case map[string]any:
		if len(value) == 0 {
			return nil
		}
	case map[string]string:
		if len(value) == 0 {
			return nil
		}
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
