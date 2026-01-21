// Package observability provides comprehensive monitoring and debugging capabilities
// for the Nexus application through metrics, structured logging, and distributed tracing.
//
// # Overview
//
// The observability package implements the three pillars of observability:
//
//  1. Metrics - Quantitative measurements using Prometheus
//  2. Logging - Structured logs with sensitive data redaction
//  3. Tracing - Distributed request tracing with OpenTelemetry
//
// # Architecture
//
// The package is designed to be:
//   - Low-overhead: Minimal performance impact on production systems
//   - Type-safe: Strongly-typed APIs reduce configuration errors
//   - Production-ready: Built-in security (redaction) and reliability features
//   - Standards-based: Uses Prometheus, OpenTelemetry, and slog
//
// # Metrics
//
// Metrics are implemented using Prometheus client libraries and track:
//   - Message flow through channels (Telegram, Discord, Slack)
//   - LLM API request latency and token usage
//   - Tool execution performance
//   - Error rates by component and type
//   - Active session counts
//   - HTTP request/response metrics
//   - Database query performance
//
// Example usage:
//
//	metrics := observability.NewMetrics()
//	defer prometheus.Handler() // Expose metrics endpoint
//
//	// Track message processing
//	metrics.MessageReceived("telegram", "inbound")
//
//	// Track LLM requests
//	start := time.Now()
//	// ... make LLM request ...
//	metrics.RecordLLMRequest("anthropic", "claude-3-opus", "success",
//	    time.Since(start).Seconds(), promptTokens, completionTokens)
//
//	// Track tool execution
//	start = time.Now()
//	// ... execute tool ...
//	metrics.RecordToolExecution("web_search", "success", time.Since(start).Seconds())
//
// # Logging
//
// Logging is built on Go's slog package with enhancements for:
//   - Automatic request ID correlation from context
//   - Sensitive data redaction (API keys, passwords, tokens)
//   - JSON output for production, text for development
//   - Configurable log levels
//
// Example usage:
//
//	logger := observability.NewLogger(observability.LogConfig{
//	    Level:     "info",
//	    Format:    "json",
//	    AddSource: true,
//	})
//
//	// Add context IDs for correlation
//	ctx := observability.AddRequestID(ctx, requestID)
//	ctx = observability.AddSessionID(ctx, sessionID)
//
//	// Structured logging with automatic context correlation
//	logger.Info(ctx, "Processing message",
//	    "channel", "telegram",
//	    "user_id", userID,
//	    "message_length", len(content),
//	)
//
//	// Error logging with automatic redaction
//	logger.Error(ctx, "LLM request failed",
//	    "error", err,
//	    "provider", "anthropic",
//	    "api_key", apiKey, // Automatically redacted
//	)
//
// # Tracing
//
// Distributed tracing uses OpenTelemetry to track requests across components:
//   - End-to-end request visualization
//   - Performance bottleneck identification
//   - Service dependency mapping
//   - Error correlation across services
//
// Example usage:
//
//	tracer, shutdown := observability.NewTracer(observability.TraceConfig{
//	    ServiceName:    "nexus",
//	    ServiceVersion: "1.0.0",
//	    Environment:    "production",
//	    Endpoint:       "localhost:4317", // OTLP collector
//	    SamplingRate:   0.1,              // Sample 10% of traces
//	})
//	defer shutdown(context.Background())
//
//	// Trace message processing
//	ctx, span := tracer.TraceMessageProcessing(ctx, "telegram", "inbound", sessionID)
//	defer span.End()
//
//	// Trace LLM requests
//	ctx, llmSpan := tracer.TraceLLMRequest(ctx, "anthropic", "claude-3-opus")
//	defer llmSpan.End()
//	tracer.SetAttributes(llmSpan, "prompt_tokens", 100, "completion_tokens", 500)
//
//	// Trace tool execution
//	ctx, toolSpan := tracer.TraceToolExecution(ctx, "web_search")
//	defer toolSpan.End()
//	if err != nil {
//	    tracer.RecordError(toolSpan, err)
//	}
//
// # Context Propagation
//
// All three components integrate with Go's context for automatic correlation:
//
//	// Add IDs to context
//	ctx = observability.AddRequestID(ctx, "req-123")
//	ctx = observability.AddSessionID(ctx, "sess-456")
//	ctx = observability.AddUserID(ctx, "user-789")
//	ctx = observability.AddChannel(ctx, "telegram")
//
//	// IDs automatically appear in logs
//	logger.Info(ctx, "Processing") // Includes request_id, session_id, etc.
//
//	// Spans inherit context
//	ctx, span := tracer.Start(ctx, "operation")
//	// Trace context propagates to child spans
//
// # Integration Example
//
// Complete example integrating all three components:
//
//	func ProcessMessage(ctx context.Context, msg *Message) error {
//	    // Add correlation IDs
//	    ctx = observability.AddRequestID(ctx, generateID())
//	    ctx = observability.AddSessionID(ctx, msg.SessionID)
//	    ctx = observability.AddChannel(ctx, msg.Channel)
//
//	    // Start tracing
//	    ctx, span := tracer.TraceMessageProcessing(ctx, msg.Channel, "inbound", msg.SessionID)
//	    defer span.End()
//
//	    // Track metrics
//	    metrics.MessageReceived(msg.Channel, "inbound")
//	    metrics.SessionStarted(msg.Channel)
//	    defer metrics.SessionEnded(msg.Channel, time.Since(start).Seconds())
//
//	    // Structured logging
//	    logger.Info(ctx, "Processing message", "content_length", len(msg.Content))
//
//	    // Process LLM request with full observability
//	    llmStart := time.Now()
//	    ctx, llmSpan := tracer.TraceLLMRequest(ctx, "anthropic", "claude-3-opus")
//	    defer llmSpan.End()
//
//	    response, err := llm.Complete(ctx, msg.Content)
//	    llmDuration := time.Since(llmStart).Seconds()
//
//	    if err != nil {
//	        metrics.RecordError("agent", "llm_request_failed")
//	        tracer.RecordError(llmSpan, err)
//	        logger.Error(ctx, "LLM request failed", "error", err)
//	        metrics.RecordLLMRequest("anthropic", "claude-3-opus", "error", llmDuration, 0, 0)
//	        return err
//	    }
//
//	    metrics.RecordLLMRequest("anthropic", "claude-3-opus", "success",
//	        llmDuration, response.PromptTokens, response.CompletionTokens)
//	    logger.Info(ctx, "LLM request completed",
//	        "duration_ms", llmDuration*1000,
//	        "tokens", response.CompletionTokens)
//
//	    return nil
//	}
//
// # Security Considerations
//
// The logging component automatically redacts:
//   - API keys (Anthropic, OpenAI, generic)
//   - Passwords and secrets
//   - JWT tokens
//   - Bearer tokens
//   - Custom patterns via configuration
//
// Sensitive fields in maps are also redacted:
//   - password, passwd, pwd
//   - secret, api_key, apikey
//   - token, auth, authorization
//   - private_key, privatekey
//
// # Performance
//
// The observability system is designed for minimal overhead:
//   - Metrics use lock-free counters where possible
//   - Logging with slog is highly efficient
//   - Tracing supports sampling to reduce overhead
//   - Context propagation is zero-allocation in most cases
//
// Typical overhead:
//   - Metrics: <1% CPU, ~10KB memory per metric
//   - Logging: ~1-5μs per log call
//   - Tracing: ~2-10μs per span (when sampled)
//
// # Configuration
//
// All components support configuration via structs:
//
//	// Metrics - no configuration needed, auto-registered
//	metrics := observability.NewMetrics()
//
//	// Logging - configurable output, level, format
//	logger := observability.NewLogger(observability.LogConfig{
//	    Level:          os.Getenv("LOG_LEVEL"),
//	    Format:         "json",
//	    AddSource:      true,
//	    RedactPatterns: []string{`custom-secret-\d+`},
//	})
//
//	// Tracing - configurable sampling, endpoint, attributes
//	tracer, shutdown := observability.NewTracer(observability.TraceConfig{
//	    ServiceName:    "nexus",
//	    ServiceVersion: version,
//	    Environment:    env,
//	    Endpoint:       os.Getenv("OTEL_ENDPOINT"),
//	    SamplingRate:   0.1,
//	    Attributes: map[string]string{
//	        "deployment.region": region,
//	        "deployment.cluster": cluster,
//	    },
//	})
//	defer shutdown(context.Background())
//
// # Testing
//
// All components provide testable interfaces:
//   - Metrics can be verified using prometheus/testutil
//   - Logging can write to bytes.Buffer for assertions
//   - Tracing works with no-op exporters in tests
//
// # Best Practices
//
//  1. Always propagate context to enable correlation
//  2. Use defer for span.End() to ensure spans are closed
//  3. Record errors on both metrics and traces
//  4. Use structured logging with key-value pairs
//  5. Set appropriate sampling rates for high-traffic systems
//  6. Add relevant attributes to spans for debugging
//  7. Use typed metric labels (avoid high-cardinality values)
//  8. Call shutdown() on tracer during graceful shutdown
//
// # Monitoring Dashboard
//
// The metrics exposed can be used to build dashboards:
//
//	# Message throughput
//	rate(nexus_messages_total[5m])
//
//	# LLM request latency (95th percentile)
//	histogram_quantile(0.95, rate(nexus_llm_request_duration_seconds_bucket[5m]))
//
//	# Error rate
//	rate(nexus_errors_total[5m])
//
//	# Active sessions
//	nexus_active_sessions
//
//	# Tool execution time
//	rate(nexus_tool_execution_duration_seconds_sum[5m]) /
//	rate(nexus_tool_execution_duration_seconds_count[5m])
//
// # Alerting
//
// Recommended alerts based on metrics:
//   - High error rate: nexus_errors_total > threshold
//   - High LLM latency: p95 latency > 10s
//   - Low message throughput: rate(nexus_messages_total) < threshold
//   - Session accumulation: nexus_active_sessions growing unbounded
//
// # Further Reading
//
//   - Prometheus best practices: https://prometheus.io/docs/practices/naming/
//   - OpenTelemetry specification: https://opentelemetry.io/docs/specs/otel/
//   - slog documentation: https://pkg.go.dev/log/slog
package observability
