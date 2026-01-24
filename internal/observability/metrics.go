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

	// WebhookReceived counts webhook requests received.
	// Labels: channel, update_type
	WebhookReceived *prometheus.CounterVec

	// WebhookDuration measures webhook processing latency.
	// Labels: channel, update_type
	// Buckets: 0.01s, 0.05s, 0.1s, 0.5s, 1s, 2s, 5s, 10s
	WebhookDuration *prometheus.HistogramVec

	// WebhookErrors counts webhook processing errors.
	// Labels: channel, update_type
	WebhookErrors *prometheus.CounterVec

	// MessageQueueDepth tracks current queue depth.
	// Labels: channel
	MessageQueueDepth *prometheus.GaugeVec

	// MessageQueueWait measures time spent waiting in queue.
	// Labels: channel
	// Buckets: 0.1s, 0.5s, 1s, 2s, 5s, 10s, 30s, 60s
	MessageQueueWait *prometheus.HistogramVec

	// MessageProcessed counts messages by outcome.
	// Labels: channel, outcome (success|error|dropped)
	MessageProcessed *prometheus.CounterVec

	// LLMCostUSD tracks estimated cost in USD.
	// Labels: provider, model
	LLMCostUSD *prometheus.CounterVec

	// ContextWindowUsed tracks context window utilization.
	// Labels: provider, model
	// Buckets: 1000, 4000, 8000, 16000, 32000, 64000, 128000
	ContextWindowUsed *prometheus.HistogramVec

	// SessionStuck counts sessions stuck in processing.
	// Labels: channel
	SessionStuck *prometheus.CounterVec

	// RunAttempts counts run attempts (for retry tracking).
	// Labels: status (success|retry|failed)
	RunAttempts *prometheus.CounterVec
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

		WebhookReceived: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_webhook_received_total",
				Help: "Total number of webhook requests received",
			},
			[]string{"channel", "update_type"},
		),

		WebhookDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_webhook_duration_seconds",
				Help:    "Duration of webhook processing in seconds",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
			},
			[]string{"channel", "update_type"},
		),

		WebhookErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_webhook_errors_total",
				Help: "Total number of webhook processing errors",
			},
			[]string{"channel", "update_type"},
		),

		MessageQueueDepth: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "nexus_message_queue_depth",
				Help: "Current message queue depth by channel",
			},
			[]string{"channel"},
		),

		MessageQueueWait: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_message_queue_wait_seconds",
				Help:    "Time spent waiting in message queue",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"channel"},
		),

		MessageProcessed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_messages_processed_total",
				Help: "Total number of messages processed by outcome",
			},
			[]string{"channel", "outcome"},
		),

		LLMCostUSD: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_llm_cost_usd_total",
				Help: "Estimated LLM API cost in USD",
			},
			[]string{"provider", "model"},
		),

		ContextWindowUsed: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nexus_context_window_tokens",
				Help:    "Context window tokens used",
				Buckets: []float64{1000, 4000, 8000, 16000, 32000, 64000, 128000},
			},
			[]string{"provider", "model"},
		),

		SessionStuck: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_session_stuck_total",
				Help: "Number of sessions stuck in processing",
			},
			[]string{"channel"},
		),

		RunAttempts: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nexus_run_attempts_total",
				Help: "Total number of run attempts by status",
			},
			[]string{"status"},
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

// RecordWebhookReceived records a webhook receipt.
//
// Example:
//
//	metrics.RecordWebhookReceived("telegram", "message")
func (m *Metrics) RecordWebhookReceived(channel, updateType string) {
	m.WebhookReceived.WithLabelValues(channel, updateType).Inc()
}

// RecordWebhookProcessed records webhook processing completion.
//
// Example:
//
//	start := time.Now()
//	// ... process webhook ...
//	metrics.RecordWebhookProcessed("discord", "message", time.Since(start).Seconds(), nil)
func (m *Metrics) RecordWebhookProcessed(channel, updateType string, durationSeconds float64, err error) {
	m.WebhookDuration.WithLabelValues(channel, updateType).Observe(durationSeconds)
	if err != nil {
		m.WebhookErrors.WithLabelValues(channel, updateType).Inc()
	}
}

// SetMessageQueueDepth sets the current queue depth.
//
// Example:
//
//	metrics.SetMessageQueueDepth("telegram", 5)
func (m *Metrics) SetMessageQueueDepth(channel string, depth int) {
	m.MessageQueueDepth.WithLabelValues(channel).Set(float64(depth))
}

// RecordMessageQueued records a message being queued.
//
// Example:
//
//	metrics.RecordMessageQueued("slack")
func (m *Metrics) RecordMessageQueued(channel string) {
	m.MessageQueueDepth.WithLabelValues(channel).Inc()
}

// RecordMessageDequeued records a message being processed from queue.
//
// Example:
//
//	metrics.RecordMessageDequeued("slack", 2.5)
func (m *Metrics) RecordMessageDequeued(channel string, waitSeconds float64) {
	m.MessageQueueDepth.WithLabelValues(channel).Dec()
	m.MessageQueueWait.WithLabelValues(channel).Observe(waitSeconds)
}

// RecordMessageProcessed records message processing completion.
//
// Example:
//
//	metrics.RecordMessageProcessed("telegram", "success")
//	metrics.RecordMessageProcessed("telegram", "error")
//	metrics.RecordMessageProcessed("telegram", "dropped")
func (m *Metrics) RecordMessageProcessed(channel, outcome string) {
	m.MessageProcessed.WithLabelValues(channel, outcome).Inc()
}

// RecordLLMCost records estimated API cost.
//
// Example:
//
//	metrics.RecordLLMCost("anthropic", "claude-3-opus", 0.015)
func (m *Metrics) RecordLLMCost(provider, model string, costUSD float64) {
	m.LLMCostUSD.WithLabelValues(provider, model).Add(costUSD)
}

// RecordContextWindow records context window utilization.
//
// Example:
//
//	metrics.RecordContextWindow("anthropic", "claude-3-opus", 45000)
func (m *Metrics) RecordContextWindow(provider, model string, tokensUsed int) {
	m.ContextWindowUsed.WithLabelValues(provider, model).Observe(float64(tokensUsed))
}

// RecordSessionStuck records a session detected as stuck.
//
// Example:
//
//	metrics.RecordSessionStuck("telegram")
func (m *Metrics) RecordSessionStuck(channel string) {
	m.SessionStuck.WithLabelValues(channel).Inc()
}

// RecordRunAttempt records a run attempt.
//
// Example:
//
//	metrics.RecordRunAttempt("success")
//	metrics.RecordRunAttempt("retry")
//	metrics.RecordRunAttempt("failed")
func (m *Metrics) RecordRunAttempt(status string) {
	m.RunAttempts.WithLabelValues(status).Inc()
}
