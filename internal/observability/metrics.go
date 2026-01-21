package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics provides a centralized interface for collecting application metrics.
//
// The metrics system is built on Prometheus and tracks:
//   - Message flow through different channels (Telegram, Discord, Slack)
//   - LLM request performance and response times
//   - Tool execution patterns and latencies
//   - Error rates categorized by type and component
//   - Active session counts for capacity planning
//
// Usage:
//
//	metrics := observability.NewMetrics()
//	metrics.MessageReceived("telegram", "inbound")
//	defer metrics.LLMRequestDuration("anthropic", "claude-3-opus").Observe(time.Since(start).Seconds())
type Metrics struct {
	// MessageCounter tracks messages by channel and direction.
	// Labels: channel (telegram|discord|slack), direction (inbound|outbound)
	MessageCounter *prometheus.CounterVec

	// LLMRequestDuration measures LLM API call latency in seconds.
	// Labels: provider (anthropic|openai), model
	// Buckets: 0.1s, 0.5s, 1s, 2s, 5s, 10s, 30s, 60s
	LLMRequestDuration *prometheus.HistogramVec

	// LLMRequestCounter counts LLM requests by provider and model.
	// Labels: provider (anthropic|openai), model, status (success|error)
	LLMRequestCounter *prometheus.CounterVec

	// LLMTokensUsed tracks token consumption.
	// Labels: provider, model, type (prompt|completion)
	LLMTokensUsed *prometheus.CounterVec

	// ToolExecutionCounter counts tool invocations.
	// Labels: tool_name, status (success|error)
	ToolExecutionCounter *prometheus.CounterVec

	// ToolExecutionDuration measures tool execution time in seconds.
	// Labels: tool_name
	// Buckets: 0.01s, 0.05s, 0.1s, 0.5s, 1s, 5s, 10s, 30s, 60s
	ToolExecutionDuration *prometheus.HistogramVec

	// ErrorCounter tracks errors by type and component.
	// Labels: component (agent|channel|tool|session), error_type
	ErrorCounter *prometheus.CounterVec

	// ActiveSessions is a gauge tracking current active sessions.
	// Labels: channel
	ActiveSessions *prometheus.GaugeVec

	// SessionDuration measures session lifetime in seconds.
	// Labels: channel
	// Buckets: 60s, 300s, 600s, 1800s, 3600s, 7200s, 14400s, 28800s
	SessionDuration *prometheus.HistogramVec

	// HTTPRequestDuration measures HTTP API request latency.
	// Labels: method, path, status_code
	// Buckets: 0.001s, 0.005s, 0.01s, 0.05s, 0.1s, 0.5s, 1s, 5s
	HTTPRequestDuration *prometheus.HistogramVec

	// HTTPRequestCounter counts HTTP requests.
	// Labels: method, path, status_code
	HTTPRequestCounter *prometheus.CounterVec

	// DatabaseQueryDuration measures database query latency.
	// Labels: operation (select|insert|update|delete), table
	// Buckets: 0.001s, 0.005s, 0.01s, 0.05s, 0.1s, 0.5s, 1s, 5s
	DatabaseQueryDuration *prometheus.HistogramVec

	// DatabaseQueryCounter counts database queries.
	// Labels: operation, table, status (success|error)
	DatabaseQueryCounter *prometheus.CounterVec
}

// NewMetrics creates and registers all Prometheus metrics.
// This should be called once at application startup.
//
// All metrics are automatically registered with Prometheus's default registry
// and will be available at the /metrics endpoint when using prometheus HTTP handler.
func NewMetrics() *Metrics {
	return &Metrics{
		MessageCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_messages_total",
				Help: "Total number of messages processed by channel and direction",
			},
			[]string{"channel", "direction"},
		),

		LLMRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_llm_request_duration_seconds",
				Help:    "Duration of LLM API requests in seconds",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"provider", "model"},
		),

		LLMRequestCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_llm_requests_total",
				Help: "Total number of LLM requests by provider, model, and status",
			},
			[]string{"provider", "model", "status"},
		),

		LLMTokensUsed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_llm_tokens_total",
				Help: "Total number of tokens used by provider, model, and type",
			},
			[]string{"provider", "model", "type"},
		),

		ToolExecutionCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_tool_executions_total",
				Help: "Total number of tool executions by tool name and status",
			},
			[]string{"tool_name", "status"},
		),

		ToolExecutionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_tool_execution_duration_seconds",
				Help:    "Duration of tool executions in seconds",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60},
			},
			[]string{"tool_name"},
		),

		ErrorCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_errors_total",
				Help: "Total number of errors by component and error type",
			},
			[]string{"component", "error_type"},
		),

		ActiveSessions: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "nexus_active_sessions",
				Help: "Current number of active sessions by channel",
			},
			[]string{"channel"},
		),

		SessionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_session_duration_seconds",
				Help:    "Duration of sessions in seconds",
				Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 28800},
			},
			[]string{"channel"},
		),

		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_http_request_duration_seconds",
				Help:    "Duration of HTTP requests in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
			},
			[]string{"method", "path", "status_code"},
		),

		HTTPRequestCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status_code"},
		),

		DatabaseQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_database_query_duration_seconds",
				Help:    "Duration of database queries in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
			},
			[]string{"operation", "table"},
		),

		DatabaseQueryCounter: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_database_queries_total",
				Help: "Total number of database queries",
			},
			[]string{"operation", "table", "status"},
		),
	}
}

// MessageReceived increments the message counter for a given channel and direction.
//
// Example:
//
//	metrics.MessageReceived("telegram", "inbound")
func (m *Metrics) MessageReceived(channel, direction string) {
	m.MessageCounter.WithLabelValues(channel, direction).Inc()
}

// MessageSent increments the message counter for outbound messages.
//
// Example:
//
//	metrics.MessageSent("discord")
func (m *Metrics) MessageSent(channel string) {
	m.MessageCounter.WithLabelValues(channel, "outbound").Inc()
}

// RecordLLMRequest records metrics for an LLM API request.
//
// Example:
//
//	start := time.Now()
//	// ... make LLM request ...
//	metrics.RecordLLMRequest("anthropic", "claude-3-opus", "success", time.Since(start).Seconds(), 100, 500)
func (m *Metrics) RecordLLMRequest(provider, model, status string, durationSeconds float64, promptTokens, completionTokens int) {
	m.LLMRequestCounter.WithLabelValues(provider, model, status).Inc()
	m.LLMRequestDuration.WithLabelValues(provider, model).Observe(durationSeconds)
	if promptTokens > 0 {
		m.LLMTokensUsed.WithLabelValues(provider, model, "prompt").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		m.LLMTokensUsed.WithLabelValues(provider, model, "completion").Add(float64(completionTokens))
	}
}

// RecordToolExecution records metrics for a tool execution.
//
// Example:
//
//	start := time.Now()
//	// ... execute tool ...
//	metrics.RecordToolExecution("web_search", "success", time.Since(start).Seconds())
func (m *Metrics) RecordToolExecution(toolName, status string, durationSeconds float64) {
	m.ToolExecutionCounter.WithLabelValues(toolName, status).Inc()
	m.ToolExecutionDuration.WithLabelValues(toolName).Observe(durationSeconds)
}

// RecordError increments the error counter for a given component and error type.
//
// Example:
//
//	metrics.RecordError("agent", "api_timeout")
//	metrics.RecordError("channel", "auth_failed")
func (m *Metrics) RecordError(component, errorType string) {
	m.ErrorCounter.WithLabelValues(component, errorType).Inc()
}

// SessionStarted increments the active sessions gauge.
//
// Example:
//
//	metrics.SessionStarted("telegram")
func (m *Metrics) SessionStarted(channel string) {
	m.ActiveSessions.WithLabelValues(channel).Inc()
}

// SessionEnded decrements the active sessions gauge and records session duration.
//
// Example:
//
//	start := time.Now()
//	// ... session lifecycle ...
//	metrics.SessionEnded("slack", time.Since(start).Seconds())
func (m *Metrics) SessionEnded(channel string, durationSeconds float64) {
	m.ActiveSessions.WithLabelValues(channel).Dec()
	m.SessionDuration.WithLabelValues(channel).Observe(durationSeconds)
}

// RecordHTTPRequest records metrics for an HTTP request.
//
// Example:
//
//	start := time.Now()
//	// ... handle HTTP request ...
//	metrics.RecordHTTPRequest("GET", "/api/sessions", "200", time.Since(start).Seconds())
func (m *Metrics) RecordHTTPRequest(method, path, statusCode string, durationSeconds float64) {
	m.HTTPRequestCounter.WithLabelValues(method, path, statusCode).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path, statusCode).Observe(durationSeconds)
}

// RecordDatabaseQuery records metrics for a database query.
//
// Example:
//
//	start := time.Now()
//	// ... execute database query ...
//	metrics.RecordDatabaseQuery("select", "sessions", "success", time.Since(start).Seconds())
func (m *Metrics) RecordDatabaseQuery(operation, table, status string, durationSeconds float64) {
	m.DatabaseQueryCounter.WithLabelValues(operation, table, status).Inc()
	m.DatabaseQueryDuration.WithLabelValues(operation, table).Observe(durationSeconds)
}
