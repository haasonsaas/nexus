package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/controlplane"
	"github.com/haasonsaas/nexus/internal/plugins"
	"gopkg.in/yaml.v3"
)

// ConfigSnapshot returns the raw config and hash for the current config file.
func (s *Server) ConfigSnapshot(ctx context.Context) (controlplane.ConfigSnapshot, error) {
	_ = ctx
	if s == nil {
		return controlplane.ConfigSnapshot{}, fmt.Errorf("server unavailable")
	}

	path := strings.TrimSpace(s.configPath)
	var raw []byte
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return controlplane.ConfigSnapshot{}, err
		}
		raw = data
	}
	if len(raw) == 0 && s.config != nil {
		payload, err := marshalConfig(s.config)
		if err != nil {
			return controlplane.ConfigSnapshot{}, err
		}
		raw = payload
	}
	if len(raw) == 0 {
		return controlplane.ConfigSnapshot{Path: path}, nil
	}

	hash := sha256.Sum256(raw)
	return controlplane.ConfigSnapshot{
		Path: path,
		Raw:  string(raw),
		Hash: hex.EncodeToString(hash[:]),
	}, nil
}

// ConfigSchema returns the JSON schema for the config.
func (s *Server) ConfigSchema(ctx context.Context) ([]byte, error) {
	_ = ctx
	return config.JSONSchema()
}

// ApplyConfig applies a new raw config, validating and updating runtime options.
func (s *Server) ApplyConfig(ctx context.Context, raw string, baseHash string) (*controlplane.ConfigApplyResult, error) {
	if s == nil {
		return nil, fmt.Errorf("server unavailable")
	}
	path := strings.TrimSpace(s.configPath)
	if path == "" {
		return nil, fmt.Errorf("config path not configured (start with --config)")
	}

	s.configApplyMu.Lock()
	defer s.configApplyMu.Unlock()

	snapshot, err := s.ConfigSnapshot(ctx)
	if err == nil && baseHash != "" && snapshot.Hash != baseHash {
		return nil, fmt.Errorf("config hash mismatch")
	}

	if strings.TrimSpace(raw) != "" {
		if err := writeRawConfig(path, raw); err != nil {
			return nil, err
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := plugins.ValidateConfig(cfg); err != nil {
		return nil, err
	}

	oldCfg := s.config
	s.config = cfg

	restartRequired, warnings := applyRuntimeConfigUpdates(ctx, s, cfg, oldCfg)
	return &controlplane.ConfigApplyResult{
		Applied:         true,
		RestartRequired: restartRequired,
		Warnings:        warnings,
	}, nil
}

// GatewayStatus returns a summary of runtime status.
func (s *Server) GatewayStatus(ctx context.Context) (controlplane.GatewayStatus, error) {
	_ = ctx
	status := controlplane.GatewayStatus{}
	if s == nil || s.config == nil {
		return status, nil
	}
	uptime := time.Since(s.startTime)
	status.UptimeSeconds = int64(uptime.Seconds())
	status.Uptime = uptime.String()
	status.StartTime = s.startTime.Format(time.RFC3339)
	status.ConfigPath = s.configPath
	status.GRPCAddress = fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.GRPCPort)
	status.HTTPAddress = fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.HTTPPort)
	return status, nil
}

func writeRawConfig(path string, raw string) error {
	data := []byte(raw)
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func marshalConfig(cfg *config.Config) ([]byte, error) {
	if cfg == nil {
		return nil, nil
	}
	payload, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func applyRuntimeConfigUpdates(ctx context.Context, s *Server, cfg *config.Config, oldCfg *config.Config) (bool, []string) {
	warnings := configRestartWarnings(oldCfg, cfg)

	if s == nil {
		return len(warnings) > 0, warnings
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if cfg != nil && s.mcpManager != nil && (oldCfg == nil || !reflect.DeepEqual(oldCfg.MCP, cfg.MCP)) {
		if err := s.mcpManager.Reload(ctx, &cfg.MCP); err != nil {
			warnings = append(warnings, fmt.Sprintf("mcp reload failed; restart required (%v)", err))
		} else if s.toolManager != nil {
			if err := s.toolManager.ReloadMCPTools(s.runtime, cfg); err != nil {
				warnings = append(warnings, fmt.Sprintf("mcp tools reload failed; restart required (%v)", err))
			}
		}
	}

	restartRequired := len(warnings) > 0

	// Update runtime options when possible.
	if s.runtime != nil && cfg != nil {
		elevatedTools := effectiveElevatedTools(cfg.Tools.Elevated, nil)
		basePolicy := buildApprovalPolicy(cfg.Tools.Execution, s.toolPolicyResolver)
		checker := agent.NewApprovalChecker(basePolicy)
		checker.SetStore(agent.NewMemoryApprovalStore())
		s.approvalChecker = checker

		s.runtime.SetOptions(agent.RuntimeOptions{
			MaxIterations:     cfg.Tools.Execution.MaxIterations,
			ToolParallelism:   cfg.Tools.Execution.Parallelism,
			ToolTimeout:       cfg.Tools.Execution.Timeout,
			ToolMaxAttempts:   cfg.Tools.Execution.MaxAttempts,
			ToolRetryBackoff:  cfg.Tools.Execution.RetryBackoff,
			DisableToolEvents: cfg.Tools.Execution.DisableEvents,
			MaxToolCalls:      cfg.Tools.Execution.MaxToolCalls,
			RequireApproval:   cfg.Tools.Execution.RequireApproval,
			ApprovalChecker:   checker,
			ElevatedTools:     elevatedTools,
			AsyncTools:        cfg.Tools.Execution.Async,
			ToolResultGuard: agent.ToolResultGuard{
				Enabled:         cfg.Tools.Execution.ResultGuard.Enabled,
				MaxChars:        cfg.Tools.Execution.ResultGuard.MaxChars,
				Denylist:        cfg.Tools.Execution.ResultGuard.Denylist,
				RedactPatterns:  cfg.Tools.Execution.ResultGuard.RedactPatterns,
				RedactionText:   cfg.Tools.Execution.ResultGuard.RedactionText,
				TruncateSuffix:  cfg.Tools.Execution.ResultGuard.TruncateSuffix,
				SanitizeSecrets: cfg.Tools.Execution.ResultGuard.SanitizeSecrets,
			},
			JobStore: s.jobStore,
			Logger:   s.logger,
		})

		if pruning := config.EffectiveContextPruningSettings(cfg.Session.ContextPruning); pruning != nil {
			s.runtime.SetContextPruning(pruning)
		}

		if system := buildSystemPrompt(cfg, SystemPromptOptions{}); system != "" {
			s.runtime.SetSystemPrompt(system)
		}
	}

	s.postureMu.Lock()
	lockdownRequested := s.postureLockdownRequested && !s.postureLockdownApplied
	s.postureMu.Unlock()
	if lockdownRequested {
		s.applyPostureLockdown(context.Background())
	}

	return restartRequired, warnings
}

func configRestartWarnings(oldCfg *config.Config, newCfg *config.Config) []string {
	if oldCfg == nil || newCfg == nil {
		return []string{"config reload requires restart"}
	}

	var warnings []string
	addWarning := func(name string, oldVal, newVal any) {
		if !reflect.DeepEqual(oldVal, newVal) {
			warnings = append(warnings, fmt.Sprintf("%s changed; restart required", name))
		}
	}

	addWarning("server", oldCfg.Server, newCfg.Server)
	addWarning("database", oldCfg.Database, newCfg.Database)
	addWarning("auth", oldCfg.Auth, newCfg.Auth)
	addWarning("channels", oldCfg.Channels, newCfg.Channels)
	addWarning("gateway", oldCfg.Gateway, newCfg.Gateway)
	addWarning("commands", oldCfg.Commands, newCfg.Commands)
	addWarning("llm", oldCfg.LLM, newCfg.LLM)
	addWarning("workspace", oldCfg.Workspace, newCfg.Workspace)
	addWarning("plugins", oldCfg.Plugins, newCfg.Plugins)
	addWarning("marketplace", oldCfg.Marketplace, newCfg.Marketplace)
	addWarning("skills", oldCfg.Skills, newCfg.Skills)
	addWarning("templates", oldCfg.Templates, newCfg.Templates)
	addWarning("vector_memory", oldCfg.VectorMemory, newCfg.VectorMemory)
	addWarning("rag", oldCfg.RAG, newCfg.RAG)
	addWarning("transcription", oldCfg.Transcription, newCfg.Transcription)
	addWarning("tools", struct {
		Sandbox      config.SandboxConfig
		Browser      config.BrowserConfig
		WebSearch    config.WebSearchConfig
		WebFetch     config.WebFetchConfig
		MemorySearch config.MemorySearchConfig
		Jobs         config.ToolJobsConfig
		ServiceNow   config.ServiceNowConfig
	}{
		Sandbox:      oldCfg.Tools.Sandbox,
		Browser:      oldCfg.Tools.Browser,
		WebSearch:    oldCfg.Tools.WebSearch,
		WebFetch:     oldCfg.Tools.WebFetch,
		MemorySearch: oldCfg.Tools.MemorySearch,
		Jobs:         oldCfg.Tools.Jobs,
		ServiceNow:   oldCfg.Tools.ServiceNow,
	}, struct {
		Sandbox      config.SandboxConfig
		Browser      config.BrowserConfig
		WebSearch    config.WebSearchConfig
		WebFetch     config.WebFetchConfig
		MemorySearch config.MemorySearchConfig
		Jobs         config.ToolJobsConfig
		ServiceNow   config.ServiceNowConfig
	}{
		Sandbox:      newCfg.Tools.Sandbox,
		Browser:      newCfg.Tools.Browser,
		WebSearch:    newCfg.Tools.WebSearch,
		WebFetch:     newCfg.Tools.WebFetch,
		MemorySearch: newCfg.Tools.MemorySearch,
		Jobs:         newCfg.Tools.Jobs,
		ServiceNow:   newCfg.Tools.ServiceNow,
	})
	addWarning("cron", oldCfg.Cron, newCfg.Cron)
	addWarning("tasks", oldCfg.Tasks, newCfg.Tasks)
	addWarning("edge", oldCfg.Edge, newCfg.Edge)
	addWarning("artifacts", oldCfg.Artifacts, newCfg.Artifacts)

	return warnings
}
