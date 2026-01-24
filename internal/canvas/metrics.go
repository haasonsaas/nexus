package canvas

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	ActiveViewers prometheus.Gauge
	UpdatesTotal  prometheus.Counter
	ActionsTotal  prometheus.Counter
}

var (
	metricsOnce     sync.Once
	metricsInstance *Metrics
)

func NewMetrics() *Metrics {
	metricsOnce.Do(func() {
		metricsInstance = &Metrics{
			ActiveViewers: promauto.NewGauge(prometheus.GaugeOpts{
				Name: "nexus_canvas_active_viewers",
				Help: "Current number of active canvas viewers",
			}),
			UpdatesTotal: promauto.NewCounter(prometheus.CounterOpts{
				Name: "nexus_canvas_updates_total",
				Help: "Total number of canvas updates emitted",
			}),
			ActionsTotal: promauto.NewCounter(prometheus.CounterOpts{
				Name: "nexus_canvas_actions_total",
				Help: "Total number of canvas actions received",
			}),
		}
	})
	return metricsInstance
}

func (m *Metrics) ViewerConnected() {
	if m == nil || m.ActiveViewers == nil {
		return
	}
	m.ActiveViewers.Inc()
}

func (m *Metrics) ViewerDisconnected() {
	if m == nil || m.ActiveViewers == nil {
		return
	}
	m.ActiveViewers.Dec()
}

func (m *Metrics) RecordUpdate() {
	if m == nil || m.UpdatesTotal == nil {
		return
	}
	m.UpdatesTotal.Inc()
}

func (m *Metrics) RecordAction() {
	if m == nil || m.ActionsTotal == nil {
		return
	}
	m.ActionsTotal.Inc()
}
