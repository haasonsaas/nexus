package channels

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Metrics provides observability into channel adapter operations.
// It tracks message counts, error rates, and latency distributions for monitoring
// and alerting in production environments.
type Metrics struct {
	// Message counters
	messagesSent     atomic.Uint64
	messagesReceived atomic.Uint64
	messagesFailed   atomic.Uint64

	// Error counters by code
	errorsByCode map[ErrorCode]*atomic.Uint64
	errorsMu     sync.RWMutex

	// Latency tracking
	sendLatency    *LatencyHistogram
	receiveLatency *LatencyHistogram

	// Connection events
	connectionsOpened atomic.Uint64
	connectionsClosed atomic.Uint64
	reconnectAttempts atomic.Uint64

	// Channel metadata
	channelType models.ChannelType
	startTime   time.Time
}

// NewMetrics creates a new Metrics instance for a channel adapter.
func NewMetrics(channelType models.ChannelType) *Metrics {
	return &Metrics{
		errorsByCode:   make(map[ErrorCode]*atomic.Uint64),
		sendLatency:    NewLatencyHistogram(),
		receiveLatency: NewLatencyHistogram(),
		channelType:    channelType,
		startTime:      time.Now(),
	}
}

// RecordMessageSent increments the sent message counter.
func (m *Metrics) RecordMessageSent() {
	m.messagesSent.Add(1)
}

// RecordMessageReceived increments the received message counter.
func (m *Metrics) RecordMessageReceived() {
	m.messagesReceived.Add(1)
}

// RecordMessageFailed increments the failed message counter.
func (m *Metrics) RecordMessageFailed() {
	m.messagesFailed.Add(1)
}

// RecordError increments the error counter for a specific error code.
func (m *Metrics) RecordError(code ErrorCode) {
	m.errorsMu.Lock()
	counter, exists := m.errorsByCode[code]
	if !exists {
		counter = &atomic.Uint64{}
		m.errorsByCode[code] = counter
	}
	m.errorsMu.Unlock()

	counter.Add(1)
}

// RecordSendLatency records the latency of a send operation.
func (m *Metrics) RecordSendLatency(duration time.Duration) {
	m.sendLatency.Record(duration)
}

// RecordReceiveLatency records the latency of a receive operation.
func (m *Metrics) RecordReceiveLatency(duration time.Duration) {
	m.receiveLatency.Record(duration)
}

// RecordConnectionOpened increments the connections opened counter.
func (m *Metrics) RecordConnectionOpened() {
	m.connectionsOpened.Add(1)
}

// RecordConnectionClosed increments the connections closed counter.
func (m *Metrics) RecordConnectionClosed() {
	m.connectionsClosed.Add(1)
}

// RecordReconnectAttempt increments the reconnect attempts counter.
func (m *Metrics) RecordReconnectAttempt() {
	m.reconnectAttempts.Add(1)
}

// Snapshot returns a point-in-time view of all metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.errorsMu.RLock()
	errors := make(map[ErrorCode]uint64, len(m.errorsByCode))
	for code, counter := range m.errorsByCode {
		errors[code] = counter.Load()
	}
	m.errorsMu.RUnlock()

	return MetricsSnapshot{
		ChannelType:       m.channelType,
		MessagesSent:      m.messagesSent.Load(),
		MessagesReceived:  m.messagesReceived.Load(),
		MessagesFailed:    m.messagesFailed.Load(),
		ErrorsByCode:      errors,
		SendLatency:       m.sendLatency.Snapshot(),
		ReceiveLatency:    m.receiveLatency.Snapshot(),
		ConnectionsOpened: m.connectionsOpened.Load(),
		ConnectionsClosed: m.connectionsClosed.Load(),
		ReconnectAttempts: m.reconnectAttempts.Load(),
		Uptime:            time.Since(m.startTime),
	}
}

// MetricsSnapshot represents a point-in-time snapshot of metrics.
type MetricsSnapshot struct {
	ChannelType       models.ChannelType
	MessagesSent      uint64
	MessagesReceived  uint64
	MessagesFailed    uint64
	ErrorsByCode      map[ErrorCode]uint64
	SendLatency       LatencySnapshot
	ReceiveLatency    LatencySnapshot
	ConnectionsOpened uint64
	ConnectionsClosed uint64
	ReconnectAttempts uint64
	Uptime            time.Duration
}

// LatencyHistogram tracks latency measurements using a simple bucketing approach.
type LatencyHistogram struct {
	mu      sync.RWMutex
	samples []time.Duration
	head    int
	count   int
	max     int
}

// NewLatencyHistogram creates a new latency histogram.
// It keeps the last 1000 samples for percentile calculation.
func NewLatencyHistogram() *LatencyHistogram {
	const defaultMaxSamples = 1000
	return &LatencyHistogram{
		samples: make([]time.Duration, defaultMaxSamples),
		max:     defaultMaxSamples,
	}
}

// Record adds a latency sample to the histogram.
func (h *LatencyHistogram) Record(duration time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.max == 0 {
		return
	}

	h.samples[h.head] = duration
	h.head = (h.head + 1) % h.max
	if h.count < h.max {
		h.count++
	}
}

// Snapshot returns a snapshot of latency statistics.
func (h *LatencyHistogram) Snapshot() LatencySnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.count == 0 {
		return LatencySnapshot{}
	}

	// Create a sorted copy for percentile calculation
	sorted := make([]time.Duration, h.count)
	if h.count < h.max {
		copy(sorted, h.samples[:h.count])
	} else {
		for i := 0; i < h.count; i++ {
			idx := (h.head + i) % h.max
			sorted[i] = h.samples[idx]
		}
	}

	// Simple insertion sort (good enough for 1000 samples)
	for i := 1; i < len(sorted); i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > key {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}

	// Calculate statistics
	var sum time.Duration
	min := sorted[0]
	max := sorted[len(sorted)-1]

	for _, d := range sorted {
		sum += d
	}

	return LatencySnapshot{
		Count: len(sorted),
		Min:   min,
		Max:   max,
		Mean:  sum / time.Duration(len(sorted)),
		P50:   sorted[len(sorted)*50/100],
		P95:   sorted[len(sorted)*95/100],
		P99:   sorted[len(sorted)*99/100],
	}
}

// LatencySnapshot represents latency statistics.
type LatencySnapshot struct {
	Count int
	Min   time.Duration
	Max   time.Duration
	Mean  time.Duration
	P50   time.Duration // Median
	P95   time.Duration
	P99   time.Duration
}
