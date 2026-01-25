// Package gateway provides the main Nexus gateway server.
//
// integration.go wires up cross-cutting concerns and provides integration
// between the gateway and various observability, health, and usage systems.
package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/commands"
	"github.com/haasonsaas/nexus/internal/infra"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/internal/usage"
)

// IntegrationConfig configures integrated subsystems.
type IntegrationConfig struct {
	// Diagnostics controls diagnostic event emission.
	DiagnosticsEnabled bool

	// Health check configuration.
	HealthProbeTimeout time.Duration

	// Usage tracking configuration.
	UsageCacheTTL time.Duration

	// Migration configuration.
	AutoMigrate bool
	StateDir    string
}

// DefaultIntegrationConfig returns sensible defaults.
func DefaultIntegrationConfig() *IntegrationConfig {
	return &IntegrationConfig{
		DiagnosticsEnabled: true,
		HealthProbeTimeout: 10 * time.Second,
		UsageCacheTTL:      5 * time.Minute,
		AutoMigrate:        true,
	}
}

// Integration provides integrated subsystems for the gateway.
type Integration struct {
	config           *IntegrationConfig
	activityTracker  *channels.ActivityTracker
	healthChecker    *commands.HealthChecker
	migrationManager *infra.MigrationManager
	usageCache       *usage.UsageCache
	usageRegistry    *usage.UsageFetcherRegistry
}

// NewIntegration creates a new integration instance.
func NewIntegration(config *IntegrationConfig) *Integration {
	if config == nil {
		config = DefaultIntegrationConfig()
	}

	// Initialize activity tracker
	activityTracker := channels.NewActivityTracker()

	// Initialize health checker
	healthChecker := commands.NewHealthChecker(&commands.HealthCheckerConfig{
		TimeoutMs:       config.HealthProbeTimeout.Milliseconds(),
		ProbeChannels:   true,
		IncludeAgents:   true,
		IncludeSessions: true,
	})

	// Initialize migration manager
	migrationManager := infra.NewMigrationManager(&infra.MigrationManagerConfig{
		StateDir:    config.StateDir,
		AutoMigrate: config.AutoMigrate,
		Logger:      infra.NewStdLogger(),
	})

	// Register built-in migrations
	migrationManager.Register(infra.SessionKeyMigration())

	// Initialize usage tracking
	usageRegistry := usage.NewUsageFetcherRegistry()
	usageCache := usage.NewUsageCache(usageRegistry, config.UsageCacheTTL)

	return &Integration{
		config:           config,
		activityTracker:  activityTracker,
		healthChecker:    healthChecker,
		migrationManager: migrationManager,
		usageCache:       usageCache,
		usageRegistry:    usageRegistry,
	}
}

// Start initializes and starts integrated subsystems.
func (i *Integration) Start(ctx context.Context) error {
	// Enable diagnostic events if configured
	if i.config.DiagnosticsEnabled {
		observability.SetDiagnosticsEnabled(true)
	}

	// Run auto-migrations if enabled
	if err := i.migrationManager.AutoMigrateOnStartup(); err != nil {
		return err
	}

	return nil
}

// Stop shuts down integrated subsystems.
func (i *Integration) Stop(ctx context.Context) error {
	observability.SetDiagnosticsEnabled(false)
	return nil
}

// ActivityTracker returns the channel activity tracker.
func (i *Integration) ActivityTracker() *channels.ActivityTracker {
	return i.activityTracker
}

// HealthChecker returns the health checker.
func (i *Integration) HealthChecker() *commands.HealthChecker {
	return i.healthChecker
}

// MigrationManager returns the migration manager.
func (i *Integration) MigrationManager() *infra.MigrationManager {
	return i.migrationManager
}

// UsageCache returns the usage cache.
func (i *Integration) UsageCache() *usage.UsageCache {
	return i.usageCache
}

// UsageRegistry returns the usage fetcher registry.
func (i *Integration) UsageRegistry() *usage.UsageFetcherRegistry {
	return i.usageRegistry
}

// RecordInbound records inbound channel activity.
func (i *Integration) RecordInbound(channel, accountID string) {
	i.activityTracker.Record(channel, accountID, channels.DirectionInbound)
}

// RecordOutbound records outbound channel activity.
func (i *Integration) RecordOutbound(channel, accountID string) {
	i.activityTracker.Record(channel, accountID, channels.DirectionOutbound)
}

// ConfigureProviderUsage registers usage fetchers for configured providers.
func (i *Integration) ConfigureProviderUsage(anthropicKey, openaiKey, geminiKey string) {
	if anthropicKey != "" {
		i.usageRegistry.Register(&usage.AnthropicUsageFetcher{
			APIKey:     anthropicKey,
			HTTPClient: &http.Client{Timeout: 30 * time.Second},
		})
	}
	if openaiKey != "" {
		i.usageRegistry.Register(&usage.OpenAIUsageFetcher{
			APIKey:     openaiKey,
			HTTPClient: &http.Client{Timeout: 30 * time.Second},
		})
	}
	if geminiKey != "" {
		i.usageRegistry.Register(&usage.GeminiUsageFetcher{
			APIKey:     geminiKey,
			HTTPClient: &http.Client{Timeout: 30 * time.Second},
		})
	}
}

// EmitModelUsage emits a diagnostic event for model usage.
func EmitModelUsage(sessionKey, sessionID, channel, provider, model string, inputTokens, outputTokens int64, costUSD float64, durationMs int64) {
	observability.EmitModelUsage(&observability.ModelUsageEvent{
		SessionKey: sessionKey,
		SessionID:  sessionID,
		Channel:    channel,
		Provider:   provider,
		Model:      model,
		Usage: observability.UsageDetails{
			Input:  inputTokens,
			Output: outputTokens,
			Total:  inputTokens + outputTokens,
		},
		CostUSD:    costUSD,
		DurationMs: durationMs,
	})
}

// EmitWebhookReceived emits a diagnostic event for webhook receipt.
func EmitWebhookReceived(channel, updateType, chatID string) {
	observability.EmitWebhookReceived(&observability.WebhookReceivedEvent{
		Channel:    channel,
		UpdateType: updateType,
		ChatID:     chatID,
	})
}

// EmitWebhookProcessed emits a diagnostic event for webhook processing completion.
func EmitWebhookProcessed(channel, updateType, chatID string, durationMs int64) {
	observability.EmitWebhookProcessed(&observability.WebhookProcessedEvent{
		Channel:    channel,
		UpdateType: updateType,
		ChatID:     chatID,
		DurationMs: durationMs,
	})
}

// EmitWebhookError emits a diagnostic event for webhook errors.
func EmitWebhookError(channel, updateType, chatID, errMsg string) {
	observability.EmitWebhookError(&observability.WebhookErrorEvent{
		Channel:    channel,
		UpdateType: updateType,
		ChatID:     chatID,
		Error:      errMsg,
	})
}

// EmitMessageQueued emits a diagnostic event for message queuing.
func EmitMessageQueued(sessionKey, sessionID, channel, source string, queueDepth int) {
	observability.EmitMessageQueued(&observability.MessageQueuedEvent{
		SessionKey: sessionKey,
		SessionID:  sessionID,
		Channel:    channel,
		Source:     source,
		QueueDepth: queueDepth,
	})
}

// EmitMessageProcessed emits a diagnostic event for message processing completion.
func EmitMessageProcessed(channel, messageID, chatID, sessionKey, sessionID, outcome, reason, errMsg string, durationMs int64) {
	observability.EmitMessageProcessed(&observability.MessageProcessedEvent{
		Channel:    channel,
		MessageID:  messageID,
		ChatID:     chatID,
		SessionKey: sessionKey,
		SessionID:  sessionID,
		Outcome:    outcome,
		Reason:     reason,
		Error:      errMsg,
		DurationMs: durationMs,
	})
}

// EmitSessionState emits a diagnostic event for session state changes.
func EmitSessionState(sessionKey, sessionID string, prevState, state observability.DiagnosticSessionState, reason string, queueDepth int) {
	observability.EmitSessionState(&observability.SessionStateEvent{
		SessionKey: sessionKey,
		SessionID:  sessionID,
		PrevState:  prevState,
		State:      state,
		Reason:     reason,
		QueueDepth: queueDepth,
	})
}

// DiagnosticListener wraps a function to receive diagnostic events.
type DiagnosticListener = observability.DiagnosticListener

// OnDiagnosticEvent registers a listener for diagnostic events.
func OnDiagnosticEvent(listener DiagnosticListener) func() {
	return observability.OnDiagnosticEvent(listener)
}

// HealthCheckResult wraps the health check response.
type HealthCheckResult = commands.HealthSummary

// ChannelHealthProber wraps a channel-specific health prober.
type ChannelHealthProber = commands.ChannelProber

// RegisterHealthProber registers a channel health prober.
func (i *Integration) RegisterHealthProber(channel string, prober ChannelHealthProber) {
	i.healthChecker.RegisterProber(channel, prober)
}

// CheckHealth performs a health check with the given options.
func (i *Integration) CheckHealth(ctx context.Context, opts *commands.HealthCheckOptions) (*HealthCheckResult, error) {
	return i.healthChecker.Check(ctx, opts)
}

// FormatHealthSummary formats a health summary for display.
func FormatHealthSummary(summary *HealthCheckResult) string {
	return commands.FormatHealthSummary(summary)
}

// GetMigrationStatus returns the current migration status.
func (i *Integration) GetMigrationStatus() (current, latest infra.MigrationVersion, pending int, err error) {
	current, err = i.migrationManager.CurrentVersion()
	if err != nil {
		return 0, 0, 0, err
	}
	latest = i.migrationManager.LatestVersion()
	migrations, err := i.migrationManager.PendingMigrations()
	if err != nil {
		return current, latest, 0, err
	}
	return current, latest, len(migrations), nil
}

// RunMigrations runs pending migrations.
func (i *Integration) RunMigrations(ctx *infra.MigrationContext) (*infra.MigrationResult, error) {
	return i.migrationManager.MigrateUp(ctx)
}

// GetProviderUsage returns usage data for a provider.
func (i *Integration) GetProviderUsage(ctx context.Context, provider string) (*usage.ProviderUsage, error) {
	return i.usageCache.Get(ctx, provider)
}

// GetAllProviderUsage returns usage data for all configured providers.
func (i *Integration) GetAllProviderUsage(ctx context.Context) []*usage.ProviderUsage {
	return i.usageCache.GetAll(ctx)
}

// FormatProviderUsage formats provider usage for display.
func FormatProviderUsage(u *usage.ProviderUsage) string {
	return usage.FormatProviderUsage(u)
}

// GetActivityStats returns channel activity statistics.
func (i *Integration) GetActivityStats() channels.ActivityStats {
	return i.activityTracker.Stats()
}
