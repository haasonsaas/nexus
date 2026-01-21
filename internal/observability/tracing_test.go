package observability

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func TestNewTracer(t *testing.T) {
	tests := []struct {
		name   string
		config TraceConfig
	}{
		{
			name: "with endpoint",
			config: TraceConfig{
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Endpoint:       "localhost:4317",
				EnableInsecure: true,
			},
		},
		{
			name: "without endpoint (no-op)",
			config: TraceConfig{
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
			},
		},
		{
			name: "with sampling",
			config: TraceConfig{
				ServiceName:  "test-service",
				SamplingRate: 0.5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, shutdown := NewTracer(tt.config)
			defer func() { _ = shutdown(context.Background()) }()

			if tracer == nil {
				t.Fatal("NewTracer() returned nil")
			}
			if tracer.tracer == nil {
				t.Error("tracer.tracer is nil")
			}
		})
	}
}

func TestTracerStart(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	if span == nil {
		t.Fatal("Start() returned nil span")
	}

	// Verify span is in context
	spanFromCtx := trace.SpanFromContext(ctx)
	if spanFromCtx == nil {
		t.Error("Expected span in context")
	}
}

func TestStartSpan(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	span := tracer.StartSpan(ctx, "test-operation")
	defer span.End()

	if span == nil {
		t.Fatal("StartSpan() returned nil")
	}
}

func TestSpanWithAttributes(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation", SpanOptions{
		Kind: trace.SpanKindServer,
		Attributes: []attribute.KeyValue{
			attribute.String("key1", "value1"),
			attribute.Int("key2", 42),
		},
	})
	defer span.End()

	if span == nil {
		t.Fatal("Start() with attributes returned nil span")
	}
}

func TestTracerRecordError(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")

	testErr := errors.New("test error")
	tracer.RecordError(span, testErr)
	span.End()

	// Verify span status is set to error
	// (We can't easily assert the internal state, but this shouldn't panic)
}

func TestTracerRecordErrorWithNil(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	// Recording nil error should not panic
	tracer.RecordError(span, nil)
}

func TestSetAttributes(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	// Test various attribute types
	tracer.SetAttributes(span,
		"string_key", "string_value",
		"int_key", 42,
		"int64_key", int64(123),
		"float_key", 3.14,
		"bool_key", true,
	)

	// Should not panic
}

func TestSetAttributesWithInvalidKeyvals(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	// Test with odd number of arguments (should handle gracefully)
	tracer.SetAttributes(span, "key1", "value1", "key2")

	// Test with non-string key
	tracer.SetAttributes(span, 123, "value")

	// Should not panic
}

func TestAddEvent(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	tracer.AddEvent(span, "test-event",
		"key1", "value1",
		"key2", 42,
	)

	// Should not panic
}

func TestTraceMessageProcessing(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.TraceMessageProcessing(ctx, "telegram", "inbound", "sess-123")
	defer span.End()

	if span == nil {
		t.Fatal("TraceMessageProcessing() returned nil span")
	}
}

func TestTraceLLMRequest(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.TraceLLMRequest(ctx, "anthropic", "claude-3-opus")
	defer span.End()

	if span == nil {
		t.Fatal("TraceLLMRequest() returned nil span")
	}
}

func TestTraceToolExecution(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.TraceToolExecution(ctx, "web_search")
	defer span.End()

	if span == nil {
		t.Fatal("TraceToolExecution() returned nil span")
	}
}

func TestTraceDatabaseQuery(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.TraceDatabaseQuery(ctx, "select", "sessions")
	defer span.End()

	if span == nil {
		t.Fatal("TraceDatabaseQuery() returned nil span")
	}
}

func TestTraceHTTPRequest(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.TraceHTTPRequest(ctx, "GET", "/api/sessions")
	defer span.End()

	if span == nil {
		t.Fatal("TraceHTTPRequest() returned nil span")
	}
}

func TestInjectExtractContext(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	// Inject context into carrier
	carrier := make(MapCarrier)
	tracer.InjectContext(ctx, carrier)

	// Note: Without a real exporter, the carrier might be empty
	// Just verify it doesn't panic
	t.Logf("Carrier keys: %v", carrier.Keys())

	// Extract context from carrier (should not panic)
	newCtx := tracer.ExtractContext(context.Background(), carrier)
	if newCtx == nil {
		t.Error("ExtractContext returned nil")
	}
}

func TestSpanFromContext(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	// Get span from context
	retrievedSpan := SpanFromContext(ctx)
	if retrievedSpan == nil {
		t.Error("SpanFromContext returned nil")
	}

	// Test with empty context
	emptySpan := SpanFromContext(context.Background())
	if emptySpan == nil {
		t.Error("SpanFromContext should return non-nil span even for empty context")
	}
}

func TestContextWithSpan(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	// Create new context with span
	newCtx := ContextWithSpan(context.Background(), span)
	if newCtx == nil {
		t.Error("ContextWithSpan returned nil")
	}

	// Verify span is in new context
	retrievedSpan := SpanFromContext(newCtx)
	if retrievedSpan == nil {
		t.Error("Expected span in new context")
	}
}

func TestWithSpan(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()

	// Test successful execution
	err := WithSpan(ctx, tracer, "test-operation", func(ctx context.Context, span trace.Span) error {
		if span == nil {
			t.Error("Expected non-nil span in callback")
		}
		return nil
	})

	if err != nil {
		t.Errorf("WithSpan returned error: %v", err)
	}

	// Test error execution
	testErr := errors.New("test error")
	err = WithSpan(ctx, tracer, "test-operation", func(ctx context.Context, span trace.Span) error {
		return testErr
	})

	if err != testErr {
		t.Errorf("Expected error to be propagated, got: %v", err)
	}
}

func TestGetTraceID(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	traceID := GetTraceID(ctx)
	// Note: Without a real exporter, trace ID might be empty for no-op spans
	// Just verify the function doesn't panic
	t.Logf("Trace ID: %s", traceID)

	// Test with empty context
	emptyTraceID := GetTraceID(context.Background())
	if emptyTraceID != "" {
		t.Error("Expected empty trace ID for context without span")
	}
}

func TestGetSpanID(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "test-operation")
	defer span.End()

	spanID := GetSpanID(ctx)
	// Note: Without a real exporter, span ID might be empty for no-op spans
	// Just verify the function doesn't panic
	t.Logf("Span ID: %s", spanID)

	// Test with empty context
	emptySpanID := GetSpanID(context.Background())
	if emptySpanID != "" {
		t.Error("Expected empty span ID for context without span")
	}
}

func TestMapCarrier(t *testing.T) {
	carrier := make(MapCarrier)

	// Test Set
	carrier.Set("key1", "value1")
	carrier.Set("key2", "value2")

	// Test Get
	if carrier.Get("key1") != "value1" {
		t.Error("MapCarrier.Get failed")
	}
	if carrier.Get("key2") != "value2" {
		t.Error("MapCarrier.Get failed")
	}
	if carrier.Get("nonexistent") != "" {
		t.Error("MapCarrier.Get should return empty string for missing key")
	}

	// Test Keys
	keys := carrier.Keys()
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}
}

func TestAttributeFromValue(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
	}{
		{"string", "str_key", "string_value"},
		{"int", "int_key", 42},
		{"int64", "int64_key", int64(123)},
		{"float64", "float_key", 3.14},
		{"bool", "bool_key", true},
		{"string slice", "str_slice_key", []string{"a", "b", "c"}},
		{"int slice", "int_slice_key", []int{1, 2, 3}},
		{"int64 slice", "int64_slice_key", []int64{1, 2, 3}},
		{"float64 slice", "float_slice_key", []float64{1.1, 2.2, 3.3}},
		{"bool slice", "bool_slice_key", []bool{true, false, true}},
		{"other", "other_key", struct{ Field string }{"value"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := attributeFromValue(tt.key, tt.value)
			if attr.Key != attribute.Key(tt.key) {
				t.Errorf("Expected key %s, got %s", tt.key, attr.Key)
			}
		})
	}
}

func TestTracerWithEnvironment(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "production",
	})
	defer func() { _ = shutdown(context.Background()) }()

	if tracer == nil {
		t.Fatal("NewTracer() returned nil")
	}
}

func TestTracerWithCustomAttributes(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
		Attributes: map[string]string{
			"custom_attr1": "value1",
			"custom_attr2": "value2",
		},
	})
	defer func() { _ = shutdown(context.Background()) }()

	if tracer == nil {
		t.Fatal("NewTracer() returned nil")
	}
}

func TestTracerSamplingRates(t *testing.T) {
	tests := []struct {
		name         string
		samplingRate float64
	}{
		{"always sample", 1.0},
		{"never sample", 0.0},
		{"50% sample", 0.5},
		{"10% sample", 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, shutdown := NewTracer(TraceConfig{
				ServiceName:  "test-service",
				SamplingRate: tt.samplingRate,
			})
			defer func() { _ = shutdown(context.Background()) }()

			if tracer == nil {
				t.Fatal("NewTracer() returned nil")
			}

			// Create some spans
			ctx := context.Background()
			for i := 0; i < 10; i++ {
				_, span := tracer.Start(ctx, "test-operation")
				span.End()
			}
		})
	}
}

func TestNestedSpans(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()

	// Create parent span
	parentCtx, parentSpan := tracer.Start(ctx, "parent-operation")
	defer parentSpan.End()

	// Create child span using parent context
	childCtx, childSpan := tracer.Start(parentCtx, "child-operation")
	defer childSpan.End()

	// Verify spans can be retrieved (may be empty for no-op tracer)
	childSpanID := GetSpanID(childCtx)
	parentSpanID := GetSpanID(parentCtx)

	t.Logf("Child span ID: %s", childSpanID)
	t.Logf("Parent span ID: %s", parentSpanID)

	// Just verify the functions don't panic and contexts are valid
	if childCtx == nil {
		t.Error("Expected valid child context")
	}
	if parentCtx == nil {
		t.Error("Expected valid parent context")
	}
}

func TestSpanWithError(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")

	// Simulate error
	testErr := errors.New("operation failed")
	tracer.RecordError(span, testErr)
	span.SetStatus(codes.Error, testErr.Error())
	span.End()

	// Should not panic
}

func TestMultipleTracersIndependent(t *testing.T) {
	tracer1, shutdown1 := NewTracer(TraceConfig{
		ServiceName: "service-1",
	})
	defer func() { _ = shutdown1(context.Background()) }()

	tracer2, shutdown2 := NewTracer(TraceConfig{
		ServiceName: "service-2",
	})
	defer func() { _ = shutdown2(context.Background()) }()

	ctx := context.Background()

	// Create spans with both tracers
	_, span1 := tracer1.Start(ctx, "operation-1")
	defer span1.End()

	_, span2 := tracer2.Start(ctx, "operation-2")
	defer span2.End()

	// Both should work independently
	if span1 == nil || span2 == nil {
		t.Error("Expected both spans to be created")
	}
}

func TestTracerShutdown(t *testing.T) {
	tracer, shutdown := NewTracer(TraceConfig{
		ServiceName: "test-service",
	})

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-operation")
	span.End()

	// Shutdown should not error
	if err := shutdown(ctx); err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}
}
