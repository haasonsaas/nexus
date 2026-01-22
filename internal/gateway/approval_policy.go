package gateway

import (
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/tools/policy"
)

func buildApprovalPolicy(execCfg config.ToolExecutionConfig, resolver *policy.Resolver) *agent.ApprovalPolicy {
	base := agent.DefaultApprovalPolicy()
	applyApprovalConfig(base, execCfg.Approval, resolver)
	if len(execCfg.RequireApproval) > 0 {
		base.RequireApproval = append(base.RequireApproval, expandApprovalPatterns(execCfg.RequireApproval, resolver)...)
	}
	return base
}

func applyApprovalConfig(target *agent.ApprovalPolicy, cfg config.ApprovalConfig, resolver *policy.Resolver) {
	if target == nil {
		return
	}
	profile := strings.ToLower(strings.TrimSpace(cfg.Profile))
	if profile != "" {
		if profilePolicy, ok := policy.ToolProfiles[profile]; ok && profilePolicy != nil {
			target.Allowlist = append(target.Allowlist, expandApprovalPatterns(profilePolicy.Allow, resolver)...)
			if profile == string(policy.ProfileFull) && strings.TrimSpace(cfg.DefaultDecision) == "" {
				target.DefaultDecision = agent.ApprovalAllowed
			}
		}
	}
	if len(cfg.Allowlist) > 0 {
		target.Allowlist = append(target.Allowlist, expandApprovalPatterns(cfg.Allowlist, resolver)...)
	}
	if len(cfg.Denylist) > 0 {
		target.Denylist = append(target.Denylist, expandApprovalPatterns(cfg.Denylist, resolver)...)
	}
	if len(cfg.SafeBins) > 0 {
		target.SafeBins = expandApprovalPatterns(cfg.SafeBins, resolver)
	}
	if cfg.SkillAllowlist != nil {
		target.SkillAllowlist = *cfg.SkillAllowlist
	}
	if cfg.AskFallback != nil {
		target.AskFallback = *cfg.AskFallback
	}
	if decision, ok := parseApprovalDecision(cfg.DefaultDecision); ok {
		target.DefaultDecision = decision
	}
	if cfg.RequestTTL > 0 {
		target.RequestTTL = cfg.RequestTTL
	}
}

func expandApprovalPatterns(items []string, resolver *policy.Resolver) []string {
	if len(items) == 0 {
		return nil
	}
	if resolver != nil {
		return resolver.ExpandGroups(items)
	}
	return policy.ExpandGroups(items)
}

func cloneApprovalPolicy(policy *agent.ApprovalPolicy) *agent.ApprovalPolicy {
	if policy == nil {
		return nil
	}
	clone := *policy
	clone.Allowlist = append([]string(nil), policy.Allowlist...)
	clone.Denylist = append([]string(nil), policy.Denylist...)
	clone.RequireApproval = append([]string(nil), policy.RequireApproval...)
	clone.SafeBins = append([]string(nil), policy.SafeBins...)
	return &clone
}

func approvalPolicyForAgent(base *agent.ApprovalPolicy, overrides agentToolOverrides, resolver *policy.Resolver) *agent.ApprovalPolicy {
	if base == nil {
		return nil
	}
	if !overrides.HasExecution && !overrides.ApprovalProvided && len(overrides.Execution.RequireApproval) == 0 {
		return base
	}
	merged := cloneApprovalPolicy(base)
	if overrides.ApprovalProvided {
		applyApprovalConfig(merged, overrides.Execution.Approval, resolver)
	}
	if len(overrides.Execution.RequireApproval) > 0 {
		merged.RequireApproval = append(merged.RequireApproval, expandApprovalPatterns(overrides.Execution.RequireApproval, resolver)...)
	}
	return merged
}

func parseApprovalDecision(value string) (agent.ApprovalDecision, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", false
	case "allow", "allowed":
		return agent.ApprovalAllowed, true
	case "deny", "denied":
		return agent.ApprovalDenied, true
	case "pending", "ask":
		return agent.ApprovalPending, true
	default:
		return "", false
	}
}
