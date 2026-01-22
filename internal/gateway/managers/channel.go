package managers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/media"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ChannelManager manages communication channels and their adapters.
// It handles registration, lifecycle, and message routing for all channel types.
type ChannelManager struct {
	mu     sync.RWMutex
	config *config.Config
	logger *slog.Logger

	// Channel registry
	registry *channels.Registry

	// Media processing
	mediaProcessor  media.Processor
	mediaAggregator *media.Aggregator

	// Lifecycle
	started bool
}

// ChannelManagerConfig holds configuration for ChannelManager.
type ChannelManagerConfig struct {
	Config          *config.Config
	Logger          *slog.Logger
	MediaProcessor  media.Processor
	MediaAggregator *media.Aggregator
}

// NewChannelManager creates a new ChannelManager.
func NewChannelManager(cfg ChannelManagerConfig) *ChannelManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ChannelManager{
		config:          cfg.Config,
		logger:          logger.With("component", "channel-manager"),
		registry:        channels.NewRegistry(),
		mediaProcessor:  cfg.MediaProcessor,
		mediaAggregator: cfg.MediaAggregator,
	}
}

// Start initializes and starts all registered channel adapters.
func (m *ChannelManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	// Start all channel adapters
	if err := m.registry.StartAll(ctx); err != nil {
		return fmt.Errorf("start channels: %w", err)
	}

	m.started = true
	m.logger.Info("channel manager started")
	return nil
}

// Stop gracefully shuts down all channel adapters.
func (m *ChannelManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	if err := m.registry.StopAll(ctx); err != nil {
		m.logger.Error("error stopping channels", "error", err)
		// Continue with shutdown even on errors
	}

	m.started = false
	m.logger.Info("channel manager stopped")
	return nil
}

// Registry returns the channel registry.
func (m *ChannelManager) Registry() *channels.Registry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.registry
}

// MediaProcessor returns the media processor.
func (m *ChannelManager) MediaProcessor() media.Processor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mediaProcessor
}

// MediaAggregator returns the media aggregator.
func (m *ChannelManager) MediaAggregator() *media.Aggregator {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mediaAggregator
}

// RegisterAdapter registers a channel adapter with the registry.
func (m *ChannelManager) RegisterAdapter(adapter channels.Adapter) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.registry.Register(adapter)
	m.logger.Info("registered channel adapter", "type", adapter.Type())
	return nil
}

// GetAdapter returns a channel adapter by type.
func (m *ChannelManager) GetAdapter(channelType models.ChannelType) (channels.Adapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.registry.Get(channelType)
}
