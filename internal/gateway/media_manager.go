// Package gateway provides the main Nexus gateway server.
//
// media_manager.go provides centralized media processing management.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/infra"
	"github.com/haasonsaas/nexus/internal/media"
	"github.com/haasonsaas/nexus/internal/media/transcribe"
)

// MediaManager manages media processing and aggregation for the gateway.
type MediaManager struct {
	*infra.BaseComponent

	mu sync.RWMutex

	config     *config.Config
	processor  media.Processor
	aggregator *media.Aggregator
}

// MediaManagerConfig configures the MediaManager.
type MediaManagerConfig struct {
	Config *config.Config
	Logger *slog.Logger
}

// NewMediaManager creates a new media manager.
func NewMediaManager(cfg MediaManagerConfig) *MediaManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &MediaManager{
		BaseComponent: infra.NewBaseComponent("media-manager", logger),
		config:        cfg.Config,
	}
}

// Start initializes media processing components.
func (m *MediaManager) Start(ctx context.Context) error {
	if !m.TransitionTo(infra.ComponentStateNew, infra.ComponentStateStarting) {
		if m.IsRunning() {
			return nil
		}
		return fmt.Errorf("media manager cannot start from state %s", m.State())
	}

	if m.config.Transcription.Enabled {
		if err := m.initTranscription(); err != nil {
			m.Logger().Warn("transcription not initialized", "error", err)
			// Not a fatal error - continue without transcription
		}
	}

	m.MarkStarted()
	m.Logger().Info("media manager started",
		"transcription_enabled", m.config.Transcription.Enabled,
		"processor_active", m.processor != nil,
	)
	return nil
}

// Stop shuts down media processing components.
func (m *MediaManager) Stop(ctx context.Context) error {
	_ = ctx // for future use
	if !m.TransitionTo(infra.ComponentStateRunning, infra.ComponentStateStopping) {
		if m.State() == infra.ComponentStateStopped {
			return nil
		}
		if m.State() != infra.ComponentStateFailed {
			return nil
		}
	}

	m.mu.Lock()
	m.processor = nil
	m.aggregator = nil
	m.mu.Unlock()

	m.MarkStopped()
	m.Logger().Info("media manager stopped")
	return nil
}

// Health returns the health status of the media manager.
func (m *MediaManager) Health(_ context.Context) infra.ComponentHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	details := make(map[string]string)
	if m.processor != nil {
		details["processor"] = "active"
	}
	if m.aggregator != nil {
		details["aggregator"] = "active"
	}

	switch m.State() {
	case infra.ComponentStateRunning:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthHealthy,
			Message: "running",
			Details: details,
		}
	case infra.ComponentStateStopped:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthUnhealthy,
			Message: "stopped",
		}
	case infra.ComponentStateFailed:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthUnhealthy,
			Message: "failed",
		}
	default:
		return infra.ComponentHealth{
			State:   infra.ServiceHealthUnknown,
			Message: m.State().String(),
		}
	}
}

// initTranscription initializes the transcription service.
func (m *MediaManager) initTranscription() error {
	cfg := m.config.Transcription

	transcriber, err := transcribe.New(transcribe.Config{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
		Language: cfg.Language,
		Logger:   m.Logger(),
	})
	if err != nil {
		return err
	}

	m.mu.Lock()
	processor := media.NewDefaultProcessor(m.Logger())
	processor.SetTranscriber(transcriber)
	m.processor = processor
	m.aggregator = media.NewAggregator(processor, m.Logger())
	m.mu.Unlock()

	return nil
}

// GetProcessor returns the media processor if available.
func (m *MediaManager) GetProcessor() media.Processor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.processor
}

// GetAggregator returns the media aggregator if available.
func (m *MediaManager) GetAggregator() *media.Aggregator {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.aggregator
}

// HasProcessor returns true if a media processor is available.
func (m *MediaManager) HasProcessor() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.processor != nil
}

// Ensure MediaManager implements FullLifecycleComponent.
var _ infra.FullLifecycleComponent = (*MediaManager)(nil)
