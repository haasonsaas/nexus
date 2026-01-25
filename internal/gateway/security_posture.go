package gateway

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/internal/security"
)

// startSecurityPosture launches the background security posture worker.
func (s *Server) startSecurityPosture(ctx context.Context) {
	if s == nil || s.config == nil {
		return
	}
	cfg := s.config.Security.Posture
	if !cfg.Enabled {
		return
	}

	interval := cfg.Interval
	if interval <= 0 {
		interval = 10 * time.Minute
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.runSecurityPosture(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runSecurityPosture(ctx)
			}
		}
	}()
}

func (s *Server) runSecurityPosture(ctx context.Context) {
	if s == nil || s.config == nil {
		return
	}
	cfg := s.config.Security.Posture
	if !cfg.Enabled {
		return
	}

	if !s.acquirePostureRun() {
		return
	}
	defer s.releasePostureRun()

	includeFilesystem := boolValue(cfg.IncludeFilesystem, true)
	includeGateway := boolValue(cfg.IncludeGateway, true)
	includeConfig := boolValue(cfg.IncludeConfig, true)
	if !includeFilesystem && !includeGateway && !includeConfig {
		includeFilesystem = true
		includeGateway = true
		includeConfig = true
	}

	stateDir := s.config.Workspace.Path
	if strings.TrimSpace(stateDir) == "" {
		stateDir = ".nexus"
	}

	auditOpts := security.AuditOptions{
		StateDir:           stateDir,
		ConfigPath:         s.configPath,
		Config:             s.config,
		IncludeFilesystem:  includeFilesystem,
		IncludeGateway:     includeGateway,
		IncludeConfig:      includeConfig,
		CheckSymlinks:      boolValue(cfg.CheckSymlinks, true),
		AllowGroupReadable: cfg.AllowGroupReadable,
	}

	report, err := security.RunAudit(auditOpts)
	if err != nil {
		s.logger.Warn("security posture audit failed", "error", err)
		return
	}

	s.logger.Info("security posture audit complete",
		"critical", report.Summary.Critical,
		"warn", report.Summary.Warn,
		"info", report.Summary.Info,
	)

	remediationApplied := false
	if cfg.AutoRemediation.Enabled && report.HasHighOrAbove() {
		mode := strings.ToLower(strings.TrimSpace(cfg.AutoRemediation.Mode))
		switch mode {
		case "lockdown":
			remediationApplied = s.applyPostureLockdown(ctx)
		case "warn_only", "":
			// no-op
		default:
			s.logger.Warn("unknown security remediation mode", "mode", cfg.AutoRemediation.Mode)
		}
	}

	if boolValue(cfg.EmitEvents, true) && s.eventRecorder != nil {
		data := map[string]interface{}{
			"summary": map[string]int{
				"critical": report.Summary.Critical,
				"warn":     report.Summary.Warn,
				"info":     report.Summary.Info,
			},
			"total":               len(report.Findings),
			"remediation_mode":    cfg.AutoRemediation.Mode,
			"remediation_applied": remediationApplied,
			"top_findings":        summarizeFindings(report.Findings, 5),
		}
		if err := s.eventRecorder.Record(ctx, observability.EventTypeCustom, "security.posture", data); err != nil {
			s.logger.Debug("failed to record security posture event", "error", err)
		}
	}
}

func (s *Server) acquirePostureRun() bool {
	s.postureMu.Lock()
	defer s.postureMu.Unlock()
	if s.postureRunning {
		return false
	}
	s.postureRunning = true
	return true
}

func (s *Server) releasePostureRun() {
	s.postureMu.Lock()
	s.postureRunning = false
	s.postureMu.Unlock()
}

func (s *Server) applyPostureLockdown(ctx context.Context) bool {
	s.postureMu.Lock()
	if s.postureLockdownApplied {
		s.postureMu.Unlock()
		return true
	}
	s.postureLockdownRequested = true
	runtime := s.runtime
	s.postureMu.Unlock()

	if runtime == nil {
		return false
	}

	policy := agent.DefaultApprovalPolicy()
	policy.Allowlist = nil
	policy.Denylist = nil
	policy.SafeBins = nil
	policy.SkillAllowlist = false
	policy.RequireApproval = []string{"*"}
	policy.DefaultDecision = agent.ApprovalPending

	checker := agent.NewApprovalChecker(policy)
	checker.SetStore(agent.NewMemoryApprovalStore())
	s.approvalChecker = checker

	elevatedTools := []string{"__disabled__"}
	runtime.SetOptions(agent.RuntimeOptions{
		MaxIterations:     s.config.Tools.Execution.MaxIterations,
		ToolParallelism:   s.config.Tools.Execution.Parallelism,
		ToolTimeout:       s.config.Tools.Execution.Timeout,
		ToolMaxAttempts:   s.config.Tools.Execution.MaxAttempts,
		ToolRetryBackoff:  s.config.Tools.Execution.RetryBackoff,
		DisableToolEvents: s.config.Tools.Execution.DisableEvents,
		MaxToolCalls:      s.config.Tools.Execution.MaxToolCalls,
		RequireApproval:   []string{"*"},
		ApprovalChecker:   checker,
		ElevatedTools:     elevatedTools,
		AsyncTools:        s.config.Tools.Execution.Async,
		ToolResultGuard: agent.ToolResultGuard{
			Enabled:         s.config.Tools.Execution.ResultGuard.Enabled,
			MaxChars:        s.config.Tools.Execution.ResultGuard.MaxChars,
			Denylist:        s.config.Tools.Execution.ResultGuard.Denylist,
			RedactPatterns:  s.config.Tools.Execution.ResultGuard.RedactPatterns,
			RedactionText:   s.config.Tools.Execution.ResultGuard.RedactionText,
			TruncateSuffix:  s.config.Tools.Execution.ResultGuard.TruncateSuffix,
			SanitizeSecrets: s.config.Tools.Execution.ResultGuard.SanitizeSecrets,
		},
		JobStore: s.jobStore,
		Logger:   s.logger,
	})

	s.postureMu.Lock()
	s.postureLockdownApplied = true
	s.postureMu.Unlock()

	s.logger.Warn("security posture lockdown applied")
	_ = ctx // ctx currently unused but kept for future context-based remediation
	return true
}

func summarizeFindings(findings []security.AuditFinding, limit int) []map[string]interface{} {
	if len(findings) == 0 || limit <= 0 {
		return nil
	}
	sorted := make([]security.AuditFinding, len(findings))
	copy(sorted, findings)
	sort.Slice(sorted, func(i, j int) bool {
		left := severityRank(sorted[i].Severity)
		right := severityRank(sorted[j].Severity)
		if left != right {
			return left > right
		}
		return sorted[i].CheckID < sorted[j].CheckID
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	out := make([]map[string]interface{}, 0, len(sorted))
	for _, f := range sorted {
		out = append(out, map[string]interface{}{
			"check_id":    f.CheckID,
			"severity":    f.Severity,
			"title":       f.Title,
			"remediation": f.Remediation,
		})
	}
	return out
}

func severityRank(sev security.AuditSeverity) int {
	switch sev {
	case security.SeverityCritical, security.SeverityHigh:
		return 3
	case security.SeverityWarn, security.SeverityMedium:
		return 2
	case security.SeverityInfo, security.SeverityLow:
		return 1
	default:
		return 0
	}
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
