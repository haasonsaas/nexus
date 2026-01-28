// Package gateway provides the main Nexus gateway server.
//
// runtime.go contains runtime initialization, provider setup, and tool registration.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/agent/routing"
	"github.com/haasonsaas/nexus/internal/attention"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	canvastools "github.com/haasonsaas/nexus/internal/tools/canvas"
	"github.com/haasonsaas/nexus/internal/tools/computeruse"
	crontools "github.com/haasonsaas/nexus/internal/tools/cron"
	exectools "github.com/haasonsaas/nexus/internal/tools/exec"
	"github.com/haasonsaas/nexus/internal/tools/facts"
	"github.com/haasonsaas/nexus/internal/tools/files"
	gatewaytools "github.com/haasonsaas/nexus/internal/tools/gateway"
	"github.com/haasonsaas/nexus/internal/tools/homeassistant"
	jobtools "github.com/haasonsaas/nexus/internal/tools/jobs"
	"github.com/haasonsaas/nexus/internal/tools/memorysearch"
	"github.com/haasonsaas/nexus/internal/tools/message"
	modelstools "github.com/haasonsaas/nexus/internal/tools/models"
	nodestools "github.com/haasonsaas/nexus/internal/tools/nodes"
	ragtools "github.com/haasonsaas/nexus/internal/tools/rag"
	"github.com/haasonsaas/nexus/internal/tools/reminders"
	"github.com/haasonsaas/nexus/internal/tools/sandbox"
	"github.com/haasonsaas/nexus/internal/tools/sandbox/firecracker"
	"github.com/haasonsaas/nexus/internal/tools/servicenow"
	sessiontools "github.com/haasonsaas/nexus/internal/tools/sessions"
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
			return nil, fmt.Errorf("create session store: %w", err)
		}
		s.sessions = store
	}
	s.ensureSessionLocker()
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
		return nil, fmt.Errorf("create LLM provider: %w", err)
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
	if s.toolManager != nil {
		s.toolManager.SetSessionStore(s.sessions)
		if err := s.toolManager.RegisterTools(ctx, runtime); err != nil {
			return nil, fmt.Errorf("register tools: %w", err)
		}
	} else if err := s.registerTools(ctx, runtime); err != nil {
		return nil, fmt.Errorf("register tools: %w", err)
	}
	if s.runtimePlugins != nil {
		if err := s.runtimePlugins.LoadTools(s.config, runtime); err != nil {
			return nil, fmt.Errorf("load runtime plugins: %w", err)
		}
	}

	if traceDir := strings.TrimSpace(os.Getenv("NEXUS_TRACE_DIR")); traceDir != "" {
		tracePlugin, err := agent.NewTraceDirectoryPlugin(traceDir)
		if err != nil {
			s.logger.Warn("failed to initialize trace directory", "error", err, "trace_dir", traceDir)
		} else {
			runtime.Use(tracePlugin)
			s.tracePlugin = tracePlugin
			s.logger.Info("trace capture enabled", "trace_dir", traceDir)
		}
	}
	s.registerMCPSamplingHandler()

	// Register event timeline plugin for observability
	if plugin := s.GetEventTimelinePlugin(); plugin != nil {
		runtime.Use(plugin)
	}
	// Register tracing plugin for OpenTelemetry spans
	if plugin := s.GetTracingPlugin(); plugin != nil {
		runtime.Use(plugin)
	}

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
	if pruning := config.EffectiveContextPruningSettings(s.config.Session.ContextPruning); pruning != nil {
		runtime.SetContextPruning(pruning)
	}

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
	s.postureMu.Lock()
	lockdownRequested := s.postureLockdownRequested && !s.postureLockdownApplied
	s.postureMu.Unlock()
	if lockdownRequested {
		s.applyPostureLockdown(ctx)
	}
	return runtime, nil
}

func (s *Server) ensureSessionLocker() {
	if s == nil || s.config == nil {
		return
	}
	if !s.config.Cluster.Enabled || !s.config.Cluster.SessionLocks.Enabled {
		return
	}
	if _, ok := s.sessionLocker.(*sessions.DBLocker); ok {
		return
	}
	cr, ok := s.sessions.(*sessions.CockroachStore)
	if !ok {
		s.logger.Warn("cluster session locks require cockroach session store")
		return
	}
	locker, err := sessions.NewDBLocker(cr.DB(), sessions.DBLockerConfig{
		OwnerID:         s.nodeID,
		TTL:             s.config.Cluster.SessionLocks.TTL,
		RefreshInterval: s.config.Cluster.SessionLocks.RefreshInterval,
		AcquireTimeout:  s.config.Cluster.SessionLocks.AcquireTimeout,
		PollInterval:    s.config.Cluster.SessionLocks.PollInterval,
	})
	if err != nil {
		s.logger.Warn("failed to enable db session locks", "error", err)
		return
	}
	s.sessionLocker = locker
	s.logger.Info("db session locks enabled", "node_id", s.nodeID)
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
// If a fallback chain is configured, it wraps the primary provider with a failover orchestrator.
func (s *Server) newProvider() (agent.LLMProvider, string, error) {
	providerID := strings.TrimSpace(s.config.LLM.DefaultProvider)
	if providerID == "" {
		providerID = "anthropic"
	}
	providerID = normalizeProviderID(providerID)

	primary, model, err := s.buildProvider(providerID)
	if err != nil {
		return nil, "", err
	}

	providerMap := map[string]agent.LLMProvider{
		providerID: primary,
	}
	selected := primary

	// Wrap with failover orchestrator if fallback chain is configured
	if len(s.config.LLM.FallbackChain) > 0 {
		orchestrator := agent.NewFailoverOrchestrator(primary, agent.DefaultFailoverConfig())

		for _, fallbackID := range s.config.LLM.FallbackChain {
			fallbackID = normalizeProviderID(fallbackID)
			if fallbackID == "" || fallbackID == providerID {
				continue // Skip empty or duplicate of primary
			}

			fallback, _, err := s.buildProvider(fallbackID)
			if err != nil {
				// Log warning but don't fail - just skip this fallback
				if s.logger != nil {
					s.logger.Warn("failed to create fallback provider", "provider", fallbackID, "error", err)
				}
				continue
			}
			orchestrator.AddProvider(fallback)
			if _, ok := providerMap[fallbackID]; !ok {
				providerMap[fallbackID] = fallback
			}
		}

		providerMap[providerID] = orchestrator
		selected = orchestrator
	}

	// Add routing target providers.
	if s.config.LLM.Routing.Enabled {
		for _, rule := range s.config.LLM.Routing.Rules {
			targetID := normalizeProviderID(rule.Target.Provider)
			if targetID == "" {
				continue
			}
			if _, ok := providerMap[targetID]; ok {
				continue
			}
			target, _, err := s.buildProvider(targetID)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("failed to create routed provider", "provider", targetID, "error", err)
				}
				continue
			}
			providerMap[targetID] = target
		}
		fallbackID := normalizeProviderID(s.config.LLM.Routing.Fallback.Provider)
		if fallbackID != "" {
			if _, ok := providerMap[fallbackID]; !ok {
				target, _, err := s.buildProvider(fallbackID)
				if err != nil {
					if s.logger != nil {
						s.logger.Warn("failed to create routing fallback provider", "provider", fallbackID, "error", err)
					}
				} else {
					providerMap[fallbackID] = target
				}
			}
		}
	}

	localProviders := []string{}
	if s.config.LLM.AutoDiscover.Ollama.Enabled {
		discovered, err := discoverOllama(s.config.LLM.AutoDiscover.Ollama.ProbeLocations, s.logger)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("ollama discovery failed", "error", err)
			}
		} else if discovered != nil {
			provider := providers.NewOllamaProvider(providers.OllamaConfig{
				BaseURL:      discovered.BaseURL,
				DefaultModel: discovered.DefaultModel,
			})
			providerMap["ollama"] = provider
			localProviders = append(localProviders, "ollama")
		}
	}

	if s.config.LLM.Routing.Enabled {
		rules := make([]routing.Rule, 0, len(s.config.LLM.Routing.Rules))
		for _, rule := range s.config.LLM.Routing.Rules {
			rules = append(rules, routing.Rule{
				Name: rule.Name,
				Match: routing.Match{
					Patterns: rule.Match.Patterns,
					Tags:     rule.Match.Tags,
				},
				Target: routing.Target{
					Provider: rule.Target.Provider,
					Model:    rule.Target.Model,
				},
			})
		}

		preferLocal := s.config.LLM.Routing.PreferLocal || s.config.LLM.AutoDiscover.Ollama.PreferLocal
		router := routing.NewRouter(routing.Config{
			DefaultProvider: providerID,
			PreferLocal:     preferLocal,
			LocalProviders:  localProviders,
			Rules:           rules,
			Fallback: routing.Target{
				Provider: s.config.LLM.Routing.Fallback.Provider,
				Model:    s.config.LLM.Routing.Fallback.Model,
			},
			FailureCooldown: s.config.LLM.Routing.UnhealthyCooldown,
		}, providerMap)
		selected = router
	}

	return selected, model, nil
}

// buildProvider creates a single LLM provider by ID.
func (s *Server) buildProvider(providerID string) (agent.LLMProvider, string, error) {
	baseID, profileID := splitProviderProfileID(providerID)
	providerKey := strings.ToLower(strings.TrimSpace(baseID))
	providerCfg, ok := s.config.LLM.Providers[providerKey]
	if !ok {
		providerCfg, ok = s.config.LLM.Providers[baseID]
	}
	if !ok {
		return nil, "", fmt.Errorf("provider config missing for %q", providerID)
	}
	effectiveCfg, err := resolveProviderProfile(providerCfg, profileID)
	if err != nil {
		return nil, "", err
	}

	switch providerKey {
	case "anthropic":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("anthropic api key is required")
		}
		provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
			APIKey:       effectiveCfg.APIKey,
			DefaultModel: effectiveCfg.DefaultModel,
			BaseURL:      effectiveCfg.BaseURL,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, effectiveCfg.DefaultModel, nil
	case "openai":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("openai api key is required")
		}
		provider := providers.NewOpenAIProviderWithConfig(providers.OpenAIConfig{
			APIKey:  effectiveCfg.APIKey,
			BaseURL: effectiveCfg.BaseURL,
		})
		return provider, effectiveCfg.DefaultModel, nil
	case "google", "gemini":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("google api key is required")
		}
		provider, err := providers.NewGoogleProvider(providers.GoogleConfig{
			APIKey:       effectiveCfg.APIKey,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, effectiveCfg.DefaultModel, nil
	case "openrouter":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("openrouter api key is required")
		}
		provider, err := providers.NewOpenRouterProvider(providers.OpenRouterConfig{
			APIKey:       effectiveCfg.APIKey,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, effectiveCfg.DefaultModel, nil
	case "azure":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("azure api key is required")
		}
		endpoint := strings.TrimSpace(effectiveCfg.BaseURL)
		if endpoint == "" {
			return nil, "", errors.New("azure endpoint (base_url) is required")
		}
		apiVersion := strings.TrimSpace(effectiveCfg.APIVersion)
		if apiVersion == "" {
			apiVersion = strings.TrimSpace(os.Getenv("AZURE_OPENAI_API_VERSION"))
		}
		provider, err := providers.NewAzureOpenAIProvider(providers.AzureOpenAIConfig{
			Endpoint:     endpoint,
			APIKey:       effectiveCfg.APIKey,
			APIVersion:   apiVersion,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, effectiveCfg.DefaultModel, nil
	case "bedrock":
		region := strings.TrimSpace(s.config.LLM.Bedrock.Region)
		provider, err := providers.NewBedrockProvider(providers.BedrockConfig{
			Region:       region,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, effectiveCfg.DefaultModel, nil
	case "ollama":
		defaultModel := strings.TrimSpace(effectiveCfg.DefaultModel)
		if defaultModel == "" {
			defaultModel = "llama3"
		}
		provider := providers.NewOllamaProvider(providers.OllamaConfig{
			BaseURL:      effectiveCfg.BaseURL,
			DefaultModel: defaultModel,
		})
		return provider, defaultModel, nil
	case "copilot-proxy":
		models := []string{}
		if strings.TrimSpace(effectiveCfg.DefaultModel) != "" {
			models = []string{strings.TrimSpace(effectiveCfg.DefaultModel)}
		}
		provider, err := providers.NewCopilotProxyProvider(providers.CopilotProxyConfig{
			BaseURL: effectiveCfg.BaseURL,
			Models:  models,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, effectiveCfg.DefaultModel, nil
	default:
		return nil, "", fmt.Errorf("unsupported provider %q", providerKey)
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
				if s.config.Tools.Sandbox.MinIdle == 0 {
					fcConfig.PoolConfig.MinIdle = s.config.Tools.Sandbox.PoolSize
				}
			}
			if s.config.Tools.Sandbox.MaxPoolSize > 0 {
				fcConfig.PoolConfig.MaxSize = s.config.Tools.Sandbox.MaxPoolSize
			}
			if s.config.Tools.Sandbox.MinIdle > 0 {
				fcConfig.PoolConfig.MinIdle = s.config.Tools.Sandbox.MinIdle
			}
			if s.config.Tools.Sandbox.MaxIdleTime > 0 {
				fcConfig.PoolConfig.MaxIdleTime = s.config.Tools.Sandbox.MaxIdleTime
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
			if s.config.Tools.Sandbox.Snapshots.Enabled {
				fcConfig.EnableSnapshots = true
				if s.config.Tools.Sandbox.Snapshots.RefreshInterval > 0 {
					fcConfig.SnapshotRefreshInterval = s.config.Tools.Sandbox.Snapshots.RefreshInterval
				}
				if s.config.Tools.Sandbox.Snapshots.MaxAge > 0 {
					fcConfig.SnapshotMaxAge = s.config.Tools.Sandbox.Snapshots.MaxAge
				}
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

	fileCfg := files.Config{Workspace: s.config.Workspace.Path}
	runtime.RegisterTool(files.NewReadTool(fileCfg))
	runtime.RegisterTool(files.NewWriteTool(fileCfg))
	runtime.RegisterTool(files.NewEditTool(fileCfg))
	runtime.RegisterTool(files.NewApplyPatchTool(fileCfg))

	execManager := exectools.NewManager(s.config.Workspace.Path)
	runtime.RegisterTool(exectools.NewExecTool("exec", execManager))
	runtime.RegisterTool(exectools.NewExecTool("bash", execManager))
	runtime.RegisterTool(exectools.NewProcessTool(execManager))

	if s.sessions != nil {
		runtime.RegisterTool(sessiontools.NewListTool(s.sessions, s.config.Session.DefaultAgentID))
		runtime.RegisterTool(sessiontools.NewHistoryTool(s.sessions))
		runtime.RegisterTool(sessiontools.NewStatusTool(s.sessions))
		runtime.RegisterTool(sessiontools.NewSendTool(s.sessions, runtime))
	}
	if s.channels != nil {
		runtime.RegisterTool(message.NewTool("message", s.channels, s.sessions, s.config.Session.DefaultAgentID))
		runtime.RegisterTool(message.NewTool("send_message", s.channels, s.sessions, s.config.Session.DefaultAgentID))
	}
	if s.cronScheduler != nil {
		runtime.RegisterTool(crontools.NewTool(s.cronScheduler))
	}
	if s.canvasHost != nil || s.canvasManager != nil {
		runtime.RegisterTool(canvastools.NewTool(s.canvasHost, s.canvasManager))
	}

	runtime.RegisterTool(gatewaytools.NewTool(s))

	if s.modelCatalog != nil {
		runtime.RegisterTool(modelstools.NewTool(s.modelCatalog, s.bedrockDiscovery))
	}

	if s.config.Edge.Enabled && s.edgeManager != nil {
		runtime.RegisterTool(nodestools.NewTool(s.edgeManager, s.edgeTOFU))
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

	if s.config.Tools.WebFetch.Enabled {
		fetchConfig := &websearch.FetchConfig{
			MaxChars: s.config.Tools.WebFetch.MaxChars,
		}
		runtime.RegisterTool(websearch.NewWebFetchTool(fetchConfig))
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
		runtime.RegisterTool(memorysearch.NewMemoryGetTool(searchConfig))
	}

	if s.config.RAG.Enabled && s.ragIndex != nil {
		searchCfg := ragtools.DefaultSearchToolConfig()
		if s.config.RAG.Search.DefaultLimit > 0 {
			searchCfg.DefaultLimit = s.config.RAG.Search.DefaultLimit
		}
		if s.config.RAG.Search.MaxResults > 0 {
			searchCfg.MaxLimit = s.config.RAG.Search.MaxResults
		}
		if s.config.RAG.Search.DefaultThreshold > 0 {
			searchCfg.DefaultThreshold = s.config.RAG.Search.DefaultThreshold
		}
		runtime.RegisterTool(ragtools.NewSearchTool(s.ragIndex, &searchCfg))
		runtime.RegisterTool(ragtools.NewUploadTool(s.ragIndex, nil))
	}

	if s.config.Tools.FactExtract.Enabled {
		runtime.RegisterTool(facts.NewExtractTool(s.config.Tools.FactExtract.MaxFacts))
	}

	if s.skillsManager != nil {
		for _, skill := range s.skillsManager.ListEligible() {
			for _, tool := range skills.BuildSkillTools(skill, execManager) {
				runtime.RegisterTool(tool)
			}
		}
	}

	if s.jobStore != nil {
		runtime.RegisterTool(jobtools.NewStatusTool(s.jobStore))
	}

	if s.attentionFeed != nil {
		runtime.RegisterTool(attention.NewListAttentionTool(s.attentionFeed))
		runtime.RegisterTool(attention.NewGetAttentionTool(s.attentionFeed))
		runtime.RegisterTool(attention.NewHandleAttentionTool(s.attentionFeed))
		runtime.RegisterTool(attention.NewSnoozeAttentionTool(s.attentionFeed))
		runtime.RegisterTool(attention.NewStatsAttentionTool(s.attentionFeed))
	}

	// Register reminder tools if task store is available
	if s.taskStore != nil && s.config.Tasks.Enabled {
		runtime.RegisterTool(reminders.NewSetTool(s.taskStore))
		runtime.RegisterTool(reminders.NewCancelTool(s.taskStore))
		runtime.RegisterTool(reminders.NewListTool(s.taskStore))
		s.logger.Info("registered reminder tools")
	}

	// Register ServiceNow tools if enabled
	if s.config.Tools.ServiceNow.Enabled {
		snowClient := servicenow.NewClient(servicenow.Config{
			InstanceURL: s.config.Tools.ServiceNow.InstanceURL,
			Username:    s.config.Tools.ServiceNow.Username,
			Password:    s.config.Tools.ServiceNow.Password,
		})
		runtime.RegisterTool(servicenow.NewListTicketsTool(snowClient))
		runtime.RegisterTool(servicenow.NewGetTicketTool(snowClient))
		runtime.RegisterTool(servicenow.NewAddCommentTool(snowClient))
		runtime.RegisterTool(servicenow.NewResolveTicketTool(snowClient))
		runtime.RegisterTool(servicenow.NewUpdateTicketTool(snowClient))
		s.logger.Info("registered ServiceNow tools")
	}

	// Register Home Assistant tools if enabled
	if s.config.Channels.HomeAssistant.Enabled {
		haClient, err := homeassistant.NewClient(homeassistant.Config{
			BaseURL: s.config.Channels.HomeAssistant.BaseURL,
			Token:   s.config.Channels.HomeAssistant.Token,
			Timeout: s.config.Channels.HomeAssistant.Timeout,
		})
		if err != nil {
			return fmt.Errorf("home assistant client: %w", err)
		}
		runtime.RegisterTool(homeassistant.NewCallServiceTool(haClient))
		runtime.RegisterTool(homeassistant.NewGetStateTool(haClient))
		runtime.RegisterTool(homeassistant.NewListEntitiesTool(haClient))
		s.logger.Info("registered Home Assistant tools")
	}

	if s.config.MCP.Enabled && s.mcpManager != nil {
		mcp.RegisterToolsWithRegistrar(runtime, s.mcpManager, s.toolPolicyResolver)
	}

	// Register edge tools if enabled
	if s.config.Edge.Enabled && s.edgeManager != nil {
		s.registerEdgeTools(runtime)
	}

	return nil
}

// registerEdgeTools registers tools from connected edges with the runtime.
func (s *Server) registerEdgeTools(runtime *agent.Runtime) {
	provider := edge.NewToolProvider(s.edgeManager)
	for _, tool := range provider.GetTools() {
		runtime.RegisterTool(tool)
	}
	s.logger.Info("registered edge tools", "count", len(provider.GetTools()))

	if s.config != nil && s.config.Tools.ComputerUse.Enabled {
		runtime.RegisterTool(computeruse.NewTool(s.edgeManager, computeruse.Config{
			EdgeID:          s.config.Tools.ComputerUse.EdgeID,
			DisplayWidthPx:  s.config.Tools.ComputerUse.DisplayWidthPx,
			DisplayHeightPx: s.config.Tools.ComputerUse.DisplayHeightPx,
			DisplayNumber:   s.config.Tools.ComputerUse.DisplayNumber,
		}))
		s.logger.Info("registered computer use tool", "edge_id", s.config.Tools.ComputerUse.EdgeID)
	}
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
