// Package gateway provides the main Nexus gateway server.
//
// runtime.go contains runtime initialization, provider setup, and tool registration.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	jobtools "github.com/haasonsaas/nexus/internal/tools/jobs"
	"github.com/haasonsaas/nexus/internal/tools/memorysearch"
	"github.com/haasonsaas/nexus/internal/tools/sandbox"
	"github.com/haasonsaas/nexus/internal/tools/sandbox/firecracker"
	"github.com/haasonsaas/nexus/internal/tools/websearch"
)

// ensureRuntime initializes the agent runtime if not already created.
func (s *Server) ensureRuntime(ctx context.Context) (*agent.Runtime, error) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()

	if s.runtime != nil {
		return s.runtime, nil
	}

	if s.sessions == nil {
		store, err := s.newSessionStore()
		if err != nil {
			return nil, err
		}
		s.sessions = store
	}
	if s.branchStore == nil {
		if cr, ok := s.sessions.(*sessions.CockroachStore); ok {
			s.branchStore = sessions.NewCockroachBranchStore(cr.DB())
		} else {
			s.branchStore = sessions.NewMemoryBranchStore()
		}
	}
	if s.memoryLogger == nil && s.config.Session.Memory.Enabled {
		s.memoryLogger = sessions.NewMemoryLogger(s.config.Session.Memory.Directory)
	}

	provider, defaultModel, err := s.newProvider()
	if err != nil {
		return nil, err
	}
	if s.llmProvider == nil {
		s.llmProvider = provider
		s.defaultModel = defaultModel
	}
	if s.mcpManager != nil {
		if err := s.mcpManager.Start(ctx); err != nil {
			return nil, fmt.Errorf("mcp manager: %w", err)
		}
	}

	runtime := agent.NewRuntime(provider, s.sessions)
	if s.branchStore != nil {
		runtime.SetBranchStore(s.branchStore)
	}
	if defaultModel != "" {
		runtime.SetDefaultModel(defaultModel)
	}
	if system := buildSystemPrompt(s.config, SystemPromptOptions{}); system != "" {
		runtime.SetSystemPrompt(system)
	}
	if err := s.registerTools(ctx, runtime); err != nil {
		return nil, err
	}
	if s.runtimePlugins != nil {
		if err := s.runtimePlugins.LoadTools(s.config, runtime); err != nil {
			return nil, err
		}
	}
	s.registerMCPSamplingHandler()

	if s.approvalChecker == nil {
		basePolicy := buildApprovalPolicy(s.config.Tools.Execution, s.toolPolicyResolver)
		checker := agent.NewApprovalChecker(basePolicy)
		checker.SetStore(agent.NewMemoryApprovalStore())
		s.approvalChecker = checker
	}
	elevatedTools := effectiveElevatedTools(s.config.Tools.Elevated, nil)
	runtime.SetOptions(agent.RuntimeOptions{
		MaxIterations:     s.config.Tools.Execution.MaxIterations,
		ToolParallelism:   s.config.Tools.Execution.Parallelism,
		ToolTimeout:       s.config.Tools.Execution.Timeout,
		ToolMaxAttempts:   s.config.Tools.Execution.MaxAttempts,
		ToolRetryBackoff:  s.config.Tools.Execution.RetryBackoff,
		DisableToolEvents: s.config.Tools.Execution.DisableEvents,
		MaxToolCalls:      s.config.Tools.Execution.MaxToolCalls,
		RequireApproval:   s.config.Tools.Execution.RequireApproval,
		ApprovalChecker:   s.approvalChecker,
		ElevatedTools:     elevatedTools,
		AsyncTools:        s.config.Tools.Execution.Async,
		JobStore:          s.jobStore,
		Logger:            s.logger,
	})

	// Initialize broadcast manager if configured
	if s.broadcastManager == nil && s.config.Gateway.Broadcast.Groups != nil && len(s.config.Gateway.Broadcast.Groups) > 0 {
		s.broadcastManager = NewBroadcastManager(
			BroadcastConfig{
				Strategy: BroadcastStrategy(s.config.Gateway.Broadcast.Strategy),
				Groups:   s.config.Gateway.Broadcast.Groups,
			},
			s.sessions,
			runtime,
			s.logger,
		)
	}

	s.runtime = runtime
	return runtime, nil
}

// newSessionStore creates a new session store based on configuration.
func (s *Server) newSessionStore() (sessions.Store, error) {
	if s.config.Database.URL == "" {
		return nil, errors.New("database url is required for session persistence")
	}

	poolCfg := sessions.DefaultCockroachConfig()
	if s.config.Database.MaxConnections > 0 {
		poolCfg.MaxOpenConns = s.config.Database.MaxConnections
	}
	if s.config.Database.ConnMaxLifetime > 0 {
		poolCfg.ConnMaxLifetime = s.config.Database.ConnMaxLifetime
	}

	return sessions.NewCockroachStoreFromDSN(s.config.Database.URL, poolCfg)
}

// newProvider creates a new LLM provider based on configuration.
func (s *Server) newProvider() (agent.LLMProvider, string, error) {
	providerID := strings.TrimSpace(s.config.LLM.DefaultProvider)
	if providerID == "" {
		providerID = "anthropic"
	}
	providerID = strings.ToLower(providerID)

	providerCfg, ok := s.config.LLM.Providers[providerID]
	if !ok {
		return nil, "", fmt.Errorf("provider config missing for %q", providerID)
	}

	switch providerID {
	case "anthropic":
		if providerCfg.APIKey == "" {
			return nil, "", errors.New("anthropic api key is required")
		}
		provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
			APIKey:       providerCfg.APIKey,
			DefaultModel: providerCfg.DefaultModel,
			BaseURL:      providerCfg.BaseURL,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, providerCfg.DefaultModel, nil
	case "openai":
		if providerCfg.APIKey == "" {
			return nil, "", errors.New("openai api key is required")
		}
		provider := providers.NewOpenAIProviderWithConfig(providers.OpenAIConfig{
			APIKey:  providerCfg.APIKey,
			BaseURL: providerCfg.BaseURL,
		})
		return provider, providerCfg.DefaultModel, nil
	case "google", "gemini":
		if providerCfg.APIKey == "" {
			return nil, "", errors.New("google api key is required")
		}
		provider, err := providers.NewGoogleProvider(providers.GoogleConfig{
			APIKey:       providerCfg.APIKey,
			DefaultModel: providerCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, providerCfg.DefaultModel, nil
	default:
		return nil, "", fmt.Errorf("unsupported provider %q", providerID)
	}
}

// registerTools registers all enabled tools with the runtime.
func (s *Server) registerTools(ctx context.Context, runtime *agent.Runtime) error {
	if s.config.Tools.Sandbox.Enabled {
		opts := []sandbox.Option{}
		backend := strings.ToLower(strings.TrimSpace(s.config.Tools.Sandbox.Backend))
		switch backend {
		case "", "docker":
			// default
		case "firecracker":
			fcConfig := firecracker.DefaultBackendConfig()
			fcConfig.NetworkEnabled = s.config.Tools.Sandbox.NetworkEnabled
			if s.config.Tools.Sandbox.PoolSize > 0 {
				fcConfig.PoolConfig.InitialSize = s.config.Tools.Sandbox.PoolSize
			}
			if s.config.Tools.Sandbox.MaxPoolSize > 0 {
				fcConfig.PoolConfig.MaxSize = s.config.Tools.Sandbox.MaxPoolSize
			}
			if s.config.Tools.Sandbox.Limits.MaxCPU > 0 {
				vcpus := int64((s.config.Tools.Sandbox.Limits.MaxCPU + 999) / 1000)
				if vcpus < 1 {
					vcpus = 1
				}
				fcConfig.DefaultVCPUs = vcpus
				fcConfig.PoolConfig.DefaultVCPUs = vcpus
			}
			if memMB, err := parseMemoryMB(s.config.Tools.Sandbox.Limits.MaxMemory); err == nil && memMB > 0 {
				fcConfig.DefaultMemMB = int64(memMB)
				fcConfig.PoolConfig.DefaultMemMB = int64(memMB)
			}
			fcBackend, err := firecracker.NewBackend(fcConfig)
			if err != nil {
				s.logger.Warn("firecracker backend unavailable, falling back to docker", "error", err)
			} else if err := fcBackend.Start(ctx); err != nil {
				s.logger.Warn("firecracker backend start failed, falling back to docker", "error", err)
				_ = fcBackend.Close()
			} else {
				sandbox.InitFirecrackerBackend(fcBackend)
				s.firecrackerBackend = fcBackend
				opts = append(opts, sandbox.WithBackend(sandbox.BackendFirecracker))
			}
		default:
			return fmt.Errorf("unsupported sandbox backend %q", backend)
		}

		if s.config.Tools.Sandbox.PoolSize > 0 {
			opts = append(opts, sandbox.WithPoolSize(s.config.Tools.Sandbox.PoolSize))
		}
		if s.config.Tools.Sandbox.MaxPoolSize > 0 {
			opts = append(opts, sandbox.WithMaxPoolSize(s.config.Tools.Sandbox.MaxPoolSize))
		}
		if s.config.Tools.Sandbox.Timeout > 0 {
			opts = append(opts, sandbox.WithDefaultTimeout(s.config.Tools.Sandbox.Timeout))
		}
		if s.config.Tools.Sandbox.Limits.MaxCPU > 0 {
			opts = append(opts, sandbox.WithDefaultCPU(s.config.Tools.Sandbox.Limits.MaxCPU))
		}
		if memMB, err := parseMemoryMB(s.config.Tools.Sandbox.Limits.MaxMemory); err == nil && memMB > 0 {
			opts = append(opts, sandbox.WithDefaultMemory(memMB))
		}
		if s.config.Tools.Sandbox.NetworkEnabled {
			opts = append(opts, sandbox.WithNetworkEnabled(true))
		}
		if err := sandbox.Register(runtime, opts...); err != nil {
			return fmt.Errorf("sandbox tool: %w", err)
		}
	}

	if s.config.Tools.Browser.Enabled {
		pool, err := browser.NewPool(browser.PoolConfig{
			Headless: s.config.Tools.Browser.Headless,
		})
		if err != nil {
			return fmt.Errorf("browser pool: %w", err)
		}
		s.browserPool = pool
		runtime.RegisterTool(browser.NewBrowserTool(pool))
	}

	if s.config.Tools.WebSearch.Enabled {
		searchConfig := &websearch.Config{
			SearXNGURL: s.config.Tools.WebSearch.URL,
		}
		switch strings.ToLower(strings.TrimSpace(s.config.Tools.WebSearch.Provider)) {
		case string(websearch.BackendSearXNG):
			searchConfig.DefaultBackend = websearch.BackendSearXNG
		case string(websearch.BackendBraveSearch):
			searchConfig.DefaultBackend = websearch.BackendBraveSearch
		case string(websearch.BackendDuckDuckGo):
			searchConfig.DefaultBackend = websearch.BackendDuckDuckGo
		default:
			if searchConfig.SearXNGURL != "" {
				searchConfig.DefaultBackend = websearch.BackendSearXNG
			} else {
				searchConfig.DefaultBackend = websearch.BackendDuckDuckGo
			}
		}
		runtime.RegisterTool(websearch.NewWebSearchTool(searchConfig))
	}

	if s.config.Tools.MemorySearch.Enabled {
		searchConfig := &memorysearch.Config{
			Directory:     s.config.Tools.MemorySearch.Directory,
			MemoryFile:    s.config.Tools.MemorySearch.MemoryFile,
			WorkspacePath: s.config.Workspace.Path,
			MaxResults:    s.config.Tools.MemorySearch.MaxResults,
			MaxSnippetLen: s.config.Tools.MemorySearch.MaxSnippetLen,
			Mode:          s.config.Tools.MemorySearch.Mode,
			Embeddings: memorysearch.EmbeddingsConfig{
				Provider: s.config.Tools.MemorySearch.Embeddings.Provider,
				APIKey:   s.config.Tools.MemorySearch.Embeddings.APIKey,
				BaseURL:  s.config.Tools.MemorySearch.Embeddings.BaseURL,
				Model:    s.config.Tools.MemorySearch.Embeddings.Model,
				CacheDir: s.config.Tools.MemorySearch.Embeddings.CacheDir,
				CacheTTL: s.config.Tools.MemorySearch.Embeddings.CacheTTL,
				Timeout:  s.config.Tools.MemorySearch.Embeddings.Timeout,
			},
		}
		runtime.RegisterTool(memorysearch.NewMemorySearchTool(searchConfig))
	}

	if s.jobStore != nil {
		runtime.RegisterTool(jobtools.NewStatusTool(s.jobStore))
	}

	if s.config.MCP.Enabled && s.mcpManager != nil {
		mcp.RegisterToolsWithRegistrar(runtime, s.mcpManager, s.toolPolicyResolver)
	}

	return nil
}

// parseMemoryMB parses a memory string (e.g., "512MB", "1GB") to megabytes.
func parseMemoryMB(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	upper := strings.ToUpper(trimmed)
	multiplier := 1.0
	switch {
	case strings.HasSuffix(upper, "GB"):
		multiplier = 1024.0
		upper = strings.TrimSuffix(upper, "GB")
	case strings.HasSuffix(upper, "G"):
		multiplier = 1024.0
		upper = strings.TrimSuffix(upper, "G")
	case strings.HasSuffix(upper, "MB"):
		upper = strings.TrimSuffix(upper, "MB")
	case strings.HasSuffix(upper, "M"):
		upper = strings.TrimSuffix(upper, "M")
	case strings.HasSuffix(upper, "KB"):
		multiplier = 1.0 / 1024.0
		upper = strings.TrimSuffix(upper, "KB")
	case strings.HasSuffix(upper, "K"):
		multiplier = 1.0 / 1024.0
		upper = strings.TrimSuffix(upper, "K")
	}
	valueFloat, err := strconv.ParseFloat(strings.TrimSpace(upper), 64)
	if err != nil {
		return 0, err
	}
	return int(valueFloat * multiplier), nil
}
