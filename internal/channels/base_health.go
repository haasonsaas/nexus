package channels

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// BaseHealthAdapter provides shared status, metrics, and degraded-state tracking.
type BaseHealthAdapter struct {
	channelType models.ChannelType
	logger      *slog.Logger

	status   Status
	statusMu sync.RWMutex

	degraded atomic.Bool

	metrics *Metrics
}

// NewBaseHealthAdapter creates a base health adapter with initialized metrics.
func NewBaseHealthAdapter(channelType models.ChannelType, logger *slog.Logger) *BaseHealthAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &BaseHealthAdapter{
		channelType: channelType,
		logger:      logger,
		status:      Status{Connected: false},
		metrics:     NewMetrics(channelType),
	}
}

// Status returns the current connection status.
func (b *BaseHealthAdapter) Status() Status {
	b.statusMu.RLock()
	defer b.statusMu.RUnlock()
	return b.status
}

// SetStatus updates the connection status and last ping time.
func (b *BaseHealthAdapter) SetStatus(connected bool, errMsg string) {
	b.statusMu.Lock()
	defer b.statusMu.Unlock()
	b.status = Status{
		Connected: connected,
		Error:     errMsg,
		LastPing:  time.Now().Unix(),
	}
}

// UpdateLastPing refreshes the last ping timestamp without changing state.
func (b *BaseHealthAdapter) UpdateLastPing() {
	b.statusMu.Lock()
	defer b.statusMu.Unlock()
	b.status.LastPing = time.Now().Unix()
}

// SetDegraded marks the adapter as degraded.
func (b *BaseHealthAdapter) SetDegraded(value bool) {
	b.degraded.Store(value)
}

// IsDegraded reports whether the adapter is in degraded mode.
func (b *BaseHealthAdapter) IsDegraded() bool {
	return b.degraded.Load()
}

// Metrics returns a snapshot of adapter metrics.
func (b *BaseHealthAdapter) Metrics() MetricsSnapshot {
	if b.metrics == nil {
		return MetricsSnapshot{ChannelType: b.channelType}
	}
	return b.metrics.Snapshot()
}

// RecordMessageSent increments the sent message counter.
func (b *BaseHealthAdapter) RecordMessageSent() {
	if b.metrics != nil {
		b.metrics.RecordMessageSent()
	}
}

// RecordMessageReceived increments the received message counter.
func (b *BaseHealthAdapter) RecordMessageReceived() {
	if b.metrics != nil {
		b.metrics.RecordMessageReceived()
	}
}

// RecordMessageFailed increments the failed message counter.
func (b *BaseHealthAdapter) RecordMessageFailed() {
	if b.metrics != nil {
		b.metrics.RecordMessageFailed()
	}
}

// RecordError increments the error counter for a specific code.
func (b *BaseHealthAdapter) RecordError(code ErrorCode) {
	if b.metrics != nil {
		b.metrics.RecordError(code)
	}
}

// RecordSendLatency records the latency of a send operation.
func (b *BaseHealthAdapter) RecordSendLatency(duration time.Duration) {
	if b.metrics != nil {
		b.metrics.RecordSendLatency(duration)
	}
}

// RecordReceiveLatency records the latency of a receive operation.
func (b *BaseHealthAdapter) RecordReceiveLatency(duration time.Duration) {
	if b.metrics != nil {
		b.metrics.RecordReceiveLatency(duration)
	}
}

// RecordConnectionOpened increments the connections opened counter.
func (b *BaseHealthAdapter) RecordConnectionOpened() {
	if b.metrics != nil {
		b.metrics.RecordConnectionOpened()
	}
}

// RecordConnectionClosed increments the connections closed counter.
func (b *BaseHealthAdapter) RecordConnectionClosed() {
	if b.metrics != nil {
		b.metrics.RecordConnectionClosed()
	}
}

// RecordReconnectAttempt increments the reconnect attempts counter.
func (b *BaseHealthAdapter) RecordReconnectAttempt() {
	if b.metrics != nil {
		b.metrics.RecordReconnectAttempt()
	}
}

// RecordActionExecuted records a successful action execution.
func (b *BaseHealthAdapter) RecordActionExecuted(action MessageAction, duration time.Duration) {
	if b.metrics != nil {
		b.metrics.RecordActionExecuted(action, duration)
	}
}

// RecordActionFailed records a failed action execution.
func (b *BaseHealthAdapter) RecordActionFailed(action MessageAction) {
	if b.metrics != nil {
		b.metrics.RecordActionFailed(action)
	}
}

// HealthCheck provides a default health check based on status/degraded state.
func (b *BaseHealthAdapter) HealthCheck(ctx context.Context) HealthStatus {
	start := time.Now()
	status := b.Status()
	healthy := status.Connected && status.Error == ""
	message := "ok"
	if !healthy {
		if status.Error != "" {
			message = status.Error
		} else {
			message = "not connected"
		}
	}
	_ = ctx
	return HealthStatus{
		Healthy:   healthy,
		Latency:   time.Since(start),
		Message:   message,
		LastCheck: time.Now(),
		Degraded:  b.IsDegraded(),
	}
}

// Logger returns the adapter logger.
func (b *BaseHealthAdapter) Logger() *slog.Logger {
	return b.logger
}
