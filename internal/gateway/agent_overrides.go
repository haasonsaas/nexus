package gateway

import (
	"encoding/json"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

type agentToolOverrides struct {
	Execution        config.ToolExecutionConfig
	HasExecution     bool
	ApprovalProvided bool
	Elevated         config.ElevatedConfig
	HasElevated      bool
}

func parseAgentToolOverrides(agentModel *models.Agent) agentToolOverrides {
	var overrides agentToolOverrides
	if agentModel == nil || len(agentModel.Config) == 0 {
		return overrides
	}
	rawTools, ok := agentModel.Config["tools"]
	if !ok || rawTools == nil {
		return overrides
	}
	toolsMap, ok := rawTools.(map[string]any)
	if !ok {
		return overrides
	}

	if rawExec, ok := toolsMap["execution"]; ok && rawExec != nil {
		if payload, err := json.Marshal(rawExec); err == nil {
			if err := json.Unmarshal(payload, &overrides.Execution); err == nil {
				overrides.HasExecution = true
			}
		}
		if execMap, ok := rawExec.(map[string]any); ok {
			if _, ok := execMap["approval"]; ok {
				overrides.ApprovalProvided = true
			}
		}
	}

	if rawElevated, ok := toolsMap["elevated"]; ok && rawElevated != nil {
		if payload, err := json.Marshal(rawElevated); err == nil {
			if err := json.Unmarshal(payload, &overrides.Elevated); err == nil {
				overrides.HasElevated = true
			}
		}
	}

	return overrides
}

func runtimeOptionsOverrideFromExecution(execCfg config.ToolExecutionConfig) agent.RuntimeOptions {
	return agent.RuntimeOptions{
		MaxIterations:     execCfg.MaxIterations,
		ToolParallelism:   execCfg.Parallelism,
		ToolTimeout:       execCfg.Timeout,
		ToolMaxAttempts:   execCfg.MaxAttempts,
		ToolRetryBackoff:  execCfg.RetryBackoff,
		DisableToolEvents: execCfg.DisableEvents,
		MaxToolCalls:      execCfg.MaxToolCalls,
		RequireApproval:   execCfg.RequireApproval,
		AsyncTools:        execCfg.Async,
		ToolResultGuard: agent.ToolResultGuard{
			Enabled:        execCfg.ResultGuard.Enabled,
			MaxChars:       execCfg.ResultGuard.MaxChars,
			Denylist:       execCfg.ResultGuard.Denylist,
			RedactPatterns: execCfg.ResultGuard.RedactPatterns,
			RedactionText:  execCfg.ResultGuard.RedactionText,
			TruncateSuffix: execCfg.ResultGuard.TruncateSuffix,
		},
	}
}
