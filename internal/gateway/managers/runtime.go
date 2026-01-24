package managers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/skills"
)

// RuntimeManager manages the agent runtime, LLM providers, sessions, and memory.
// It is the core execution manager that provides the AI capabilities.
type RuntimeManager struct {
	mu     sync.RWMutex
	config *config.Config
	logger *slog.Logger

	// Core components
	runtime      *agent.Runtime
	llmProvider  agent.LLMProvider
	defaultModel string

	// Session management
	sessions    sessions.Store
	branchStore sessions.BranchStore

	// Memory systems
	vectorMemory *memory.Manager
	memoryLogger *sessions.MemoryLogger

	// Skills
	skillsManager *skills.Manager

	// Approval
	approvalChecker *agent.ApprovalChecker

	// Lifecycle
	started bool
}

// RuntimeManagerConfig holds configuration for RuntimeManager.
type RuntimeManagerConfig struct {
	Config        *config.Config
	Logger        *slog.Logger
	SkillsManager *skills.Manager
	VectorMemory  *memory.Manager
}

// NewRuntimeManager creates a new RuntimeManager.
func NewRuntimeManager(cfg RuntimeManagerConfig) *RuntimeManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &RuntimeManager{
		config:        cfg.Config,
		logger:        logger.With("component", "runtime-manager"),
		skillsManager: cfg.SkillsManager,
		vectorMemory:  cfg.VectorMemory,
	}
}

// Start initializes the runtime manager and its subsystems.
func (m *RuntimeManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	// Initialize session store
	if err := m.initSessionStore(); err != nil {
		return fmt.Errorf("init session store: %w", err)
	}

	// Initialize branch store
	m.initBranchStore()

	// Initialize memory logger
	m.initMemoryLogger()

	// Initialize LLM provider
	if err := m.initProvider(); err != nil {
		return fmt.Errorf("init provider: %w", err)
	}

	// Initialize approval checker
	m.initApprovalChecker()

	m.started = true
	m.logger.Info("runtime manager started")
	return nil
}

// Stop gracefully shuts down the runtime manager.
func (m *RuntimeManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	var errs []error

	// Close vector memory
	if m.vectorMemory != nil {
		if err := m.vectorMemory.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close vector memory: %w", err))
		}
	}

	// Close skills manager
	if m.skillsManager != nil {
		if err := m.skillsManager.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close skills manager: %w", err))
		}
	}

	// Close session store
	if closer, ok := m.sessions.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close session store: %w", err))
		}
	}

	m.started = false
	m.logger.Info("runtime manager stopped")

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Runtime returns the agent runtime, initializing it if necessary.
// This method is safe to call concurrently.
func (m *RuntimeManager) Runtime(ctx context.Context) (*agent.Runtime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.runtime != nil {
		return m.runtime, nil
	}

	runtime := agent.NewRuntime(m.llmProvider, m.sessions)
	if m.branchStore != nil {
		runtime.SetBranchStore(m.branchStore)
	}
	if m.defaultModel != "" {
		runtime.SetDefaultModel(m.defaultModel)
	}
	if m.config != nil {
		if pruning := config.EffectiveContextPruningSettings(m.config.Session.ContextPruning); pruning != nil {
			runtime.SetContextPruning(pruning)
		}
	}

	m.runtime = runtime
	return runtime, nil
}

// Sessions returns the session store.
func (m *RuntimeManager) Sessions() sessions.Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions
}

// BranchStore returns the branch store.
func (m *RuntimeManager) BranchStore() sessions.BranchStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.branchStore
}

// VectorMemory returns the vector memory manager.
func (m *RuntimeManager) VectorMemory() *memory.Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.vectorMemory
}

// SkillsManager returns the skills manager.
func (m *RuntimeManager) SkillsManager() *skills.Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.skillsManager
}

// ApprovalChecker returns the approval checker.
func (m *RuntimeManager) ApprovalChecker() *agent.ApprovalChecker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approvalChecker
}

// LLMProvider returns the LLM provider.
func (m *RuntimeManager) LLMProvider() agent.LLMProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.llmProvider
}

// DefaultModel returns the default model name.
func (m *RuntimeManager) DefaultModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultModel
}

// initSessionStore creates the session store based on configuration.
func (m *RuntimeManager) initSessionStore() error {
	if m.config.Database.URL == "" {
		return errors.New("database url is required for session persistence")
	}

	poolCfg := sessions.DefaultCockroachConfig()
	if m.config.Database.MaxConnections > 0 {
		poolCfg.MaxOpenConns = m.config.Database.MaxConnections
	}
	if m.config.Database.ConnMaxLifetime > 0 {
		poolCfg.ConnMaxLifetime = m.config.Database.ConnMaxLifetime
	}

	store, err := sessions.NewCockroachStoreFromDSN(m.config.Database.URL, poolCfg)
	if err != nil {
		return err
	}
	m.sessions = store
	return nil
}

// initBranchStore creates the branch store.
func (m *RuntimeManager) initBranchStore() {
	if cr, ok := m.sessions.(*sessions.CockroachStore); ok {
		m.branchStore = sessions.NewCockroachBranchStore(cr.DB())
	} else {
		m.branchStore = sessions.NewMemoryBranchStore()
	}
}

// initMemoryLogger creates the memory logger if enabled.
func (m *RuntimeManager) initMemoryLogger() {
	if m.config.Session.Memory.Enabled {
		m.memoryLogger = sessions.NewMemoryLogger(m.config.Session.Memory.Directory)
	}
}

// initProvider creates the LLM provider with failover support.
func (m *RuntimeManager) initProvider() error {
	providerID := strings.TrimSpace(m.config.LLM.DefaultProvider)
	if providerID == "" {
		providerID = "anthropic"
	}
	providerID = strings.ToLower(providerID)

	primary, model, err := m.buildProvider(providerID)
	if err != nil {
		return err
	}

	// Wrap with failover orchestrator if fallback chain is configured
	if len(m.config.LLM.FallbackChain) > 0 {
		orchestrator := agent.NewFailoverOrchestrator(primary, agent.DefaultFailoverConfig())

		for _, fallbackID := range m.config.LLM.FallbackChain {
			fallbackID = strings.ToLower(strings.TrimSpace(fallbackID))
			if fallbackID == "" || fallbackID == providerID {
				continue
			}

			fallback, _, err := m.buildProvider(fallbackID)
			if err != nil {
				m.logger.Warn("failed to create fallback provider", "provider", fallbackID, "error", err)
				continue
			}
			orchestrator.AddProvider(fallback)
		}

		m.llmProvider = orchestrator
	} else {
		m.llmProvider = primary
	}

	m.defaultModel = model
	return nil
}

// buildProvider creates a single LLM provider by ID.
func (m *RuntimeManager) buildProvider(providerID string) (agent.LLMProvider, string, error) {
	providerCfg, ok := m.config.LLM.Providers[providerID]
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

// initApprovalChecker creates the approval checker.
func (m *RuntimeManager) initApprovalChecker() {
	// Use default policy as a base
	basePolicy := agent.DefaultApprovalPolicy()

	// If no tools require approval, allow all by default
	if len(m.config.Tools.Execution.RequireApproval) == 0 {
		basePolicy.Allowlist = []string{"*"}
		basePolicy.DefaultDecision = agent.ApprovalAllowed
	} else {
		// Use the configured require_approval list
		basePolicy.RequireApproval = m.config.Tools.Execution.RequireApproval
	}

	checker := agent.NewApprovalChecker(basePolicy)
	checker.SetStore(agent.NewMemoryApprovalStore())
	m.approvalChecker = checker
}
