package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracer provides distributed tracing capabilities using OpenTelemetry.
//
// The tracing system enables:
//   - Distributed request tracing across services
//   - Performance profiling and bottleneck identification
//   - Service dependency mapping
//   - Error tracking and debugging
//   - Integration with observability platforms (Jaeger, Tempo, etc.)
//
// Architecture:
//   - Spans represent individual operations (LLM calls, tool executions, etc.)
//   - Context propagation ensures traces flow through async operations
//   - Attributes provide rich metadata for analysis
//   - Sampling controls overhead in high-throughput scenarios
//
// Usage:
//
//	tracer, shutdown := observability.NewTracer(observability.TraceConfig{
//	    ServiceName:    "nexus",
//	    ServiceVersion: "1.0.0",
//	    Environment:    "production",
//	    Endpoint:       "localhost:4317",
//	})
//	defer shutdown()
//
//	ctx, span := tracer.Start(ctx, "process_message")
//	defer span.End()
type Tracer struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	config   TraceConfig
}

// TraceConfig configures the distributed tracing behavior.
type TraceConfig struct {
	// ServiceName identifies this service in traces
	ServiceName string

	// ServiceVersion identifies the service version
	ServiceVersion string

	// Environment specifies the deployment environment (production, staging, dev)
	Environment string

	// Endpoint is the OTLP collector endpoint (e.g., "localhost:4317")
	// If empty, tracing is disabled
	Endpoint string

	// SamplingRate controls what fraction of traces are recorded (0.0 to 1.0)
	// 1.0 = all traces, 0.1 = 10% of traces
	// Defaults to 1.0 if not specified
	SamplingRate float64

	// Attributes are additional resource attributes to include in all spans
	Attributes map[string]string

	// EnableInsecure disables TLS for the OTLP connection (dev/testing only)
	EnableInsecure bool
}

// SpanOptions configures span creation behavior.
type SpanOptions struct {
	// Kind specifies the span kind (client, server, internal, producer, consumer)
	Kind trace.SpanKind

	// Attributes are key-value pairs attached to the span
	Attributes []attribute.KeyValue
}

// NewTracer creates a new tracer with the given configuration.
// Returns the tracer and a shutdown function that must be called on exit.
//
// If config.Endpoint is empty, a no-op tracer is returned that doesn't export traces.
//
// Example:
//
//	tracer, shutdown := observability.NewTracer(observability.TraceConfig{
//	    ServiceName:    "nexus",
//	    ServiceVersion: "1.0.0",
//	    Endpoint:       os.Getenv("OTEL_ENDPOINT"),
//	    SamplingRate:   0.1,
//	})
//	defer shutdown(context.Background())
func NewTracer(config TraceConfig) (*Tracer, func(context.Context) error) {
	// If no endpoint, return no-op tracer
	if config.Endpoint == "" {
		return &Tracer{
			tracer: otel.Tracer(config.ServiceName),
			config: config,
		}, func(context.Context) error { return nil }
	}

	// Set defaults
	if config.SamplingRate == 0 {
		config.SamplingRate = 1.0
	}
	if config.ServiceName == "" {
		config.ServiceName = "nexus"
	}

	// Create OTLP exporter
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.Endpoint),
	}
	if config.EnableInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(opts...),
	)
	if err != nil {
		// Fallback to no-op tracer if exporter creation fails
		return &Tracer{
			tracer: otel.Tracer(config.ServiceName),
			config: config,
		}, func(context.Context) error { return nil }
	}

	// Build resource attributes
	attrs := []attribute.KeyValue{
		semconv.ServiceName(config.ServiceName),
		semconv.ServiceVersion(config.ServiceVersion),
	}
	if config.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironment(config.Environment))
	}
	for k, v := range config.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		res = resource.Default()
	}

	// Create trace provider with sampling
	var sampler sdktrace.Sampler
	if config.SamplingRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if config.SamplingRate <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(config.SamplingRate)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global provider and propagator
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer := &Tracer{
		provider: provider,
		tracer:   provider.Tracer(config.ServiceName),
		config:   config,
	}

	shutdown := func(ctx context.Context) error {
		return provider.Shutdown(ctx)
	}

	return tracer, shutdown
}

// Start creates a new span and returns a context containing it.
// The span should be ended by calling span.End() when the operation completes.
//
// Example:
//
//	ctx, span := tracer.Start(ctx, "llm_request")
//	defer span.End()
func (t *Tracer) Start(ctx context.Context, name string, opts ...SpanOptions) (context.Context, trace.Span) {
	var options []trace.SpanStartOption

	if len(opts) > 0 {
		opt := opts[0]
		if opt.Kind != 0 {
			options = append(options, trace.WithSpanKind(opt.Kind))
		}
		if len(opt.Attributes) > 0 {
			options = append(options, trace.WithAttributes(opt.Attributes...))
		}
	}

	return t.tracer.Start(ctx, name, options...)
}

// StartSpan is a convenience wrapper around Start that returns just the span.
// The caller must still call span.End().
//
// Example:
//
//	span := tracer.StartSpan(ctx, "operation")
//	defer span.End()
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...SpanOptions) trace.Span {
	_, span := t.Start(ctx, name, opts...)
	return span
}

// RecordError records an error on the span and sets the span status to error.
//
// Example:
//
//	ctx, span := tracer.Start(ctx, "operation")
//	defer span.End()
//	if err != nil {
//	    tracer.RecordError(span, err)
//	}
func (t *Tracer) RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetAttributes sets multiple attributes on a span.
//
// Example:
//
//	tracer.SetAttributes(span,
//	    "channel", "telegram",
//	    "user_id", "12345",
//	    "message_length", 1024,
//	)
func (t *Tracer) SetAttributes(span trace.Span, keyvals ...any) {
	attrs := make([]attribute.KeyValue, 0, len(keyvals)/2)
	for i := 0; i < len(keyvals)-1; i += 2 {
		key, ok := keyvals[i].(string)
		if !ok {
			continue
		}
		val := keyvals[i+1]
		attrs = append(attrs, attributeFromValue(key, val))
	}
	span.SetAttributes(attrs...)
}

// AddEvent adds an event to the span with optional attributes.
//
// Example:
//
//	tracer.AddEvent(span, "tool_executed",
//	    "tool_name", "web_search",
//	    "duration_ms", 250,
//	)
func (t *Tracer) AddEvent(span trace.Span, name string, keyvals ...any) {
	attrs := make([]attribute.KeyValue, 0, len(keyvals)/2)
	for i := 0; i < len(keyvals)-1; i += 2 {
		key, ok := keyvals[i].(string)
		if !ok {
			continue
		}
		val := keyvals[i+1]
		attrs = append(attrs, attributeFromValue(key, val))
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// TraceMessageProcessing creates a span for message processing operations.
//
// Example:
//
//	ctx, span := tracer.TraceMessageProcessing(ctx, "telegram", "inbound", "sess-123")
//	defer span.End()
func (t *Tracer) TraceMessageProcessing(ctx context.Context, channel, direction, sessionID string) (context.Context, trace.Span) {
	return t.Start(ctx, "process_message", SpanOptions{
		Kind: trace.SpanKindServer,
		Attributes: []attribute.KeyValue{
			attribute.String("channel", channel),
			attribute.String("direction", direction),
			attribute.String("session_id", sessionID),
		},
	})
}

// TraceLLMRequest creates a span for LLM API requests.
//
// Example:
//
//	ctx, span := tracer.TraceLLMRequest(ctx, "anthropic", "claude-3-opus")
//	defer span.End()
func (t *Tracer) TraceLLMRequest(ctx context.Context, provider, model string) (context.Context, trace.Span) {
	return t.Start(ctx, fmt.Sprintf("llm.%s", provider), SpanOptions{
		Kind: trace.SpanKindClient,
		Attributes: []attribute.KeyValue{
			attribute.String("llm.provider", provider),
			attribute.String("llm.model", model),
		},
	})
}

// TraceToolExecution creates a span for tool executions.
//
// Example:
//
//	ctx, span := tracer.TraceToolExecution(ctx, "web_search")
//	defer span.End()
func (t *Tracer) TraceToolExecution(ctx context.Context, toolName string) (context.Context, trace.Span) {
	return t.Start(ctx, fmt.Sprintf("tool.%s", toolName), SpanOptions{
		Kind: trace.SpanKindInternal,
		Attributes: []attribute.KeyValue{
			attribute.String("tool.name", toolName),
		},
	})
}

// TraceDatabaseQuery creates a span for database queries.
//
// Example:
//
//	ctx, span := tracer.TraceDatabaseQuery(ctx, "select", "sessions")
//	defer span.End()
func (t *Tracer) TraceDatabaseQuery(ctx context.Context, operation, table string) (context.Context, trace.Span) {
	return t.Start(ctx, fmt.Sprintf("db.%s", operation), SpanOptions{
		Kind: trace.SpanKindClient,
		Attributes: []attribute.KeyValue{
			attribute.String("db.operation", operation),
			attribute.String("db.table", table),
		},
	})
}

// TraceHTTPRequest creates a span for HTTP requests.
//
// Example:
//
//	ctx, span := tracer.TraceHTTPRequest(ctx, "GET", "/api/sessions")
//	defer span.End()
func (t *Tracer) TraceHTTPRequest(ctx context.Context, method, path string) (context.Context, trace.Span) {
	return t.Start(ctx, fmt.Sprintf("http.%s %s", method, path), SpanOptions{
		Kind: trace.SpanKindServer,
		Attributes: []attribute.KeyValue{
			attribute.String("http.method", method),
			attribute.String("http.path", path),
		},
	})
}

// InjectContext injects trace context into a carrier (e.g., HTTP headers).
//
// Example:
//
//	carrier := make(map[string]string)
//	tracer.InjectContext(ctx, carrier)
//	// Use carrier in HTTP headers or message metadata
func (t *Tracer) InjectContext(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractContext extracts trace context from a carrier.
//
// Example:
//
//	carrier := propagation.MapCarrier(headers)
//	ctx := tracer.ExtractContext(ctx, carrier)
func (t *Tracer) ExtractContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// SpanFromContext returns the current span from the context.
// Returns a non-recording span if no span is present.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// ContextWithSpan returns a new context with the given span.
func ContextWithSpan(ctx context.Context, span trace.Span) context.Context {
	return trace.ContextWithSpan(ctx, span)
}

// attributeFromValue creates an attribute.KeyValue from a Go value.
func attributeFromValue(key string, val any) attribute.KeyValue {
	switch v := val.(type) {
	case string:
		return attribute.String(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case float64:
		return attribute.Float64(key, v)
	case bool:
		return attribute.Bool(key, v)
	case []string:
		return attribute.StringSlice(key, v)
	case []int:
		return attribute.IntSlice(key, v)
	case []int64:
		return attribute.Int64Slice(key, v)
	case []float64:
		return attribute.Float64Slice(key, v)
	case []bool:
		return attribute.BoolSlice(key, v)
	default:
		return attribute.String(key, fmt.Sprintf("%v", v))
	}
}

// WithSpan is a helper that creates a span, executes a function, and ends the span.
// If the function returns an error, it's recorded on the span.
//
// Example:
//
//	err := observability.WithSpan(ctx, tracer, "operation", func(ctx context.Context, span trace.Span) error {
//	    // Do work
//	    return nil
//	})
func WithSpan(ctx context.Context, tracer *Tracer, name string, fn func(context.Context, trace.Span) error) error {
	ctx, span := tracer.Start(ctx, name)
	defer span.End()

	err := fn(ctx, span)
	if err != nil {
		tracer.RecordError(span, err)
	}
	return err
}

// GetTraceID returns the trace ID from the context as a string.
// Returns empty string if no trace is active.
func GetTraceID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

// GetSpanID returns the span ID from the context as a string.
// Returns empty string if no span is active.
func GetSpanID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().SpanID().String()
}

// MapCarrier is a simple map-based carrier for context propagation.
type MapCarrier map[string]string

// Get returns the value for the given key.
func (m MapCarrier) Get(key string) string {
	return m[key]
}

// Set stores the key-value pair.
func (m MapCarrier) Set(key, value string) {
	m[key] = value
}

// Keys returns all keys in the carrier.
func (m MapCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
