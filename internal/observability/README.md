# Observability Package

Comprehensive monitoring and debugging for Nexus through metrics, structured logging, and distributed tracing.

## Quick Start

```go
package main

import (
    "context"
    "time"

    "github.com/haasonsaas/nexus/internal/observability"
)

func main() {
    // Initialize all components
    metrics := observability.NewMetrics()
    logger := observability.NewLogger(observability.LogConfig{
        Level:  "info",
        Format: "json",
    })
    tracer, shutdown := observability.NewTracer(observability.TraceConfig{
        ServiceName: "nexus",
        Endpoint:    "localhost:4317", // OTLP collector
    })
    defer shutdown(context.Background())

    // Use them together
    processMessage(metrics, logger, tracer)
}
```

## Metrics

Track application performance with Prometheus metrics:

```go
metrics := observability.NewMetrics()

// Track messages
metrics.MessageReceived("telegram", "inbound")
metrics.MessageSent("telegram")

// Track LLM requests
start := time.Now()
// ... make LLM API call ...
metrics.RecordLLMRequest(
    "anthropic",
    "claude-3-opus",
    "success",
    time.Since(start).Seconds(),
    100,  // prompt tokens
    500,  // completion tokens
)

// Track tool executions
start = time.Now()
// ... execute tool ...
metrics.RecordToolExecution("web_search", "success", time.Since(start).Seconds())

// Track errors
metrics.RecordError("agent", "timeout")

// Track sessions
metrics.SessionStarted("telegram")
sessionStart := time.Now()
// ... session lifecycle ...
metrics.SessionEnded("telegram", time.Since(sessionStart).Seconds())
```

### Available Metrics

- `nexus_messages_total` - Message counter by channel and direction
- `nexus_llm_request_duration_seconds` - LLM API request latency
- `nexus_llm_requests_total` - LLM request counter with status
- `nexus_llm_tokens_total` - Token usage by type (prompt/completion)
- `nexus_tool_executions_total` - Tool execution counter
- `nexus_tool_execution_duration_seconds` - Tool execution latency
- `nexus_errors_total` - Error counter by component and type
- `nexus_active_sessions` - Current active session gauge
- `nexus_session_duration_seconds` - Session duration histogram
- `nexus_http_requests_total` - HTTP request counter
- `nexus_http_request_duration_seconds` - HTTP request latency
- `nexus_database_queries_total` - Database query counter
- `nexus_database_query_duration_seconds` - Database query latency

## Logging

Structured logging with automatic sensitive data redaction:

```go
logger := observability.NewLogger(observability.LogConfig{
    Level:     "info",          // debug, info, warn, error
    Format:    "json",          // json or text
    AddSource: true,            // include file:line
    RedactPatterns: []string{   // custom redaction patterns
        `custom-secret-\d+`,
    },
})

// Add correlation IDs to context
ctx := observability.AddRequestID(ctx, "req-123")
ctx = observability.AddSessionID(ctx, "sess-456")
ctx = observability.AddUserID(ctx, "user-789")
ctx = observability.AddChannel(ctx, "telegram")

// Structured logging with automatic context
logger.Info(ctx, "Processing message",
    "channel", "telegram",
    "content_length", 1024,
)

// Error logging
logger.Error(ctx, "LLM request failed",
    "error", err,
    "provider", "anthropic",
    "api_key", apiKey,  // Automatically redacted!
)

// Component-scoped logger
componentLogger := logger.WithFields("component", "agent", "version", "1.0")
componentLogger.Info(ctx, "Starting up")
```

### Automatic Redaction

The logger automatically redacts:
- API keys (Anthropic, OpenAI, generic)
- Passwords and secrets
- JWT tokens
- Bearer tokens
- Custom patterns via configuration

Sensitive map keys are also redacted:
- `password`, `passwd`, `pwd`
- `secret`, `api_key`, `apikey`
- `token`, `auth`, `authorization`
- `private_key`, `privatekey`

## Tracing

Distributed request tracing with OpenTelemetry:

```go
tracer, shutdown := observability.NewTracer(observability.TraceConfig{
    ServiceName:    "nexus",
    ServiceVersion: "1.0.0",
    Environment:    "production",
    Endpoint:       "localhost:4317",  // OTLP collector
    SamplingRate:   0.1,               // Sample 10% of traces
    Attributes: map[string]string{
        "deployment.region": "us-west-2",
    },
})
defer shutdown(context.Background())

// Trace message processing
ctx, span := tracer.TraceMessageProcessing(ctx, "telegram", "inbound", sessionID)
defer span.End()

// Add attributes
tracer.SetAttributes(span,
    "user_id", userID,
    "message_length", len(content),
)

// Trace LLM requests
ctx, llmSpan := tracer.TraceLLMRequest(ctx, "anthropic", "claude-3-opus")
defer llmSpan.End()

if err != nil {
    tracer.RecordError(llmSpan, err)
}

// Trace tool execution
ctx, toolSpan := tracer.TraceToolExecution(ctx, "web_search")
defer toolSpan.End()

// Helper for automatic span management
err := observability.WithSpan(ctx, tracer, "operation", func(ctx context.Context, span trace.Span) error {
    // Do work
    return nil
})
```

### Context Propagation

Propagate trace context across services:

```go
// Service A: Inject context
carrier := make(observability.MapCarrier)
tracer.InjectContext(ctx, carrier)
// Send carrier in HTTP headers or message metadata

// Service B: Extract context
ctx := tracer.ExtractContext(context.Background(), carrier)
// Continue tracing with extracted context
```

## Complete Integration Example

```go
func ProcessMessage(ctx context.Context, msg *Message) error {
    // Add correlation IDs
    ctx = observability.AddRequestID(ctx, generateID())
    ctx = observability.AddSessionID(ctx, msg.SessionID)
    ctx = observability.AddChannel(ctx, msg.Channel)

    // Track metrics
    metrics.MessageReceived(msg.Channel, "inbound")
    metrics.SessionStarted(msg.Channel)
    defer func(start time.Time) {
        metrics.SessionEnded(msg.Channel, time.Since(start).Seconds())
    }(time.Now())

    // Start tracing
    ctx, span := tracer.TraceMessageProcessing(ctx, msg.Channel, "inbound", msg.SessionID)
    defer span.End()

    // Log processing start
    logger.Info(ctx, "Processing message", "content_length", len(msg.Content))

    // Process LLM request with full observability
    llmStart := time.Now()
    ctx, llmSpan := tracer.TraceLLMRequest(ctx, "anthropic", "claude-3-opus")
    defer llmSpan.End()

    response, err := llm.Complete(ctx, msg.Content)
    llmDuration := time.Since(llmStart).Seconds()

    if err != nil {
        metrics.RecordError("agent", "llm_request_failed")
        tracer.RecordError(llmSpan, err)
        logger.Error(ctx, "LLM request failed", "error", err)
        metrics.RecordLLMRequest("anthropic", "claude-3-opus", "error", llmDuration, 0, 0)
        return err
    }

    metrics.RecordLLMRequest("anthropic", "claude-3-opus", "success",
        llmDuration, response.PromptTokens, response.CompletionTokens)
    logger.Info(ctx, "LLM request completed",
        "duration_ms", llmDuration*1000,
        "tokens", response.CompletionTokens)

    return nil
}
```

## Monitoring Dashboards

### Prometheus Queries

```promql
# Message throughput
rate(nexus_messages_total[5m])

# LLM request latency (95th percentile)
histogram_quantile(0.95, rate(nexus_llm_request_duration_seconds_bucket[5m]))

# Error rate
rate(nexus_errors_total[5m])

# Active sessions
nexus_active_sessions

# Average tool execution time
rate(nexus_tool_execution_duration_seconds_sum[5m]) /
rate(nexus_tool_execution_duration_seconds_count[5m])
```

### Recommended Alerts

```yaml
- alert: HighErrorRate
  expr: rate(nexus_errors_total[5m]) > 0.1
  annotations:
    summary: High error rate detected

- alert: HighLLMLatency
  expr: histogram_quantile(0.95, rate(nexus_llm_request_duration_seconds_bucket[5m])) > 10
  annotations:
    summary: LLM requests are slow

- alert: SessionAccumulation
  expr: rate(nexus_active_sessions[5m]) > 0
  for: 30m
  annotations:
    summary: Sessions not being cleaned up
```

## Configuration

### Environment Variables

```bash
# Logging
export LOG_LEVEL=info
export LOG_FORMAT=json

# Tracing
export OTEL_ENDPOINT=localhost:4317
export OTEL_SERVICE_NAME=nexus
export OTEL_SERVICE_VERSION=1.0.0
export OTEL_SAMPLING_RATE=0.1
```

### Application Code

```go
logger := observability.NewLogger(observability.LogConfig{
    Level:  os.Getenv("LOG_LEVEL"),
    Format: os.Getenv("LOG_FORMAT"),
})

tracer, shutdown := observability.NewTracer(observability.TraceConfig{
    ServiceName:    os.Getenv("OTEL_SERVICE_NAME"),
    ServiceVersion: os.Getenv("OTEL_SERVICE_VERSION"),
    Endpoint:       os.Getenv("OTEL_ENDPOINT"),
    SamplingRate:   parseFloat(os.Getenv("OTEL_SAMPLING_RATE")),
})
defer shutdown(context.Background())
```

## Performance

Typical overhead per operation:
- **Metrics**: <1% CPU, ~10KB memory per metric
- **Logging**: ~1-5μs per log call
- **Tracing**: ~2-10μs per span (when sampled)

Use sampling in high-throughput scenarios:

```go
tracer, _ := observability.NewTracer(observability.TraceConfig{
    SamplingRate: 0.01,  // Sample 1% of traces
})
```

## Testing

All components support testable interfaces:

```go
// Test metrics
import "github.com/prometheus/client_golang/prometheus/testutil"

counter.WithLabelValues("test").Inc()
if testutil.CollectAndCount(counter) < 1 {
    t.Error("Expected counter to increment")
}

// Test logging
var buf bytes.Buffer
logger := observability.NewLogger(observability.LogConfig{
    Output: &buf,
})
logger.Info(ctx, "test message")
if !strings.Contains(buf.String(), "test message") {
    t.Error("Expected log output")
}

// Test tracing (no-op without endpoint)
tracer, shutdown := observability.NewTracer(observability.TraceConfig{
    ServiceName: "test",
})
defer shutdown(context.Background())
```

## Best Practices

1. **Always propagate context** - Use `context.Context` for correlation
2. **Use defer for spans** - Ensure spans are always closed
3. **Record errors consistently** - Update both metrics and traces
4. **Use structured logging** - Add key-value pairs instead of formatting
5. **Set appropriate sampling** - Don't trace 100% in production
6. **Add relevant attributes** - Include data useful for debugging
7. **Avoid high-cardinality labels** - Don't use user IDs as metric labels
8. **Call shutdown gracefully** - Allow traces to flush on exit

## See Also

- [Prometheus Best Practices](https://prometheus.io/docs/practices/naming/)
- [OpenTelemetry Specification](https://opentelemetry.io/docs/specs/otel/)
- [slog Documentation](https://pkg.go.dev/log/slog)
