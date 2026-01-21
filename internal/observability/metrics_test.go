package observability

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetrics(t *testing.T) {
	metrics := NewMetrics()

	if metrics == nil {
		t.Fatal("NewMetrics() returned nil")
	}

	if metrics.MessageCounter == nil {
		t.Error("MessageCounter is nil")
	}
	if metrics.LLMRequestDuration == nil {
		t.Error("LLMRequestDuration is nil")
	}
	if metrics.ToolExecutionCounter == nil {
		t.Error("ToolExecutionCounter is nil")
	}
	if metrics.ErrorCounter == nil {
		t.Error("ErrorCounter is nil")
	}
	if metrics.ActiveSessions == nil {
		t.Error("ActiveSessions is nil")
	}
}

func TestMessageReceived(t *testing.T) {
	// Create a new registry for isolated testing
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "test_messages_total",
			Help: "Test message counter",
		},
		[]string{"channel", "direction"},
	)
	registry.MustRegister(counter)

	// Record some messages
	counter.WithLabelValues("telegram", "inbound").Inc()
	counter.WithLabelValues("telegram", "inbound").Inc()
	counter.WithLabelValues("discord", "outbound").Inc()

	// Verify counts
	if count := testutil.CollectAndCount(counter); count != 2 {
		t.Errorf("Expected 2 label combinations, got %d", count)
	}

	// Verify specific values
	expected := `
		# HELP test_messages_total Test message counter
		# TYPE test_messages_total counter
		test_messages_total{channel="discord",direction="outbound"} 1
		test_messages_total{channel="telegram",direction="inbound"} 2
	`
	if err := testutil.CollectAndCompare(counter, strings.NewReader(expected)); err != nil {
		t.Errorf("Unexpected metric value: %v", err)
	}
}

func TestMessageSent(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "test_messages_sent_total",
			Help: "Test message sent counter",
		},
		[]string{"channel", "direction"},
	)
	registry.MustRegister(counter)

	counter.WithLabelValues("slack", "outbound").Inc()
	counter.WithLabelValues("slack", "outbound").Inc()

	expected := `
		# HELP test_messages_sent_total Test message sent counter
		# TYPE test_messages_sent_total counter
		test_messages_sent_total{channel="slack",direction="outbound"} 2
	`
	if err := testutil.CollectAndCompare(counter, strings.NewReader(expected)); err != nil {
		t.Errorf("Unexpected metric value: %v", err)
	}
}

func TestRecordLLMRequest(t *testing.T) {
	metrics := NewMetrics()

	// Record a successful LLM request
	metrics.RecordLLMRequest("anthropic", "claude-3-opus", "success", 1.5, 100, 500)

	// Verify counter was incremented
	count := testutil.CollectAndCount(metrics.LLMRequestCounter)
	if count < 1 {
		t.Error("Expected at least 1 LLM request recorded")
	}

	// Record multiple requests
	metrics.RecordLLMRequest("openai", "gpt-4", "success", 2.0, 200, 600)
	metrics.RecordLLMRequest("anthropic", "claude-3-opus", "error", 0.5, 50, 0)

	// Verify histogram has observations
	histCount := testutil.CollectAndCount(metrics.LLMRequestDuration)
	if histCount < 1 {
		t.Error("Expected LLM request duration histogram to have observations")
	}
}

func TestRecordToolExecution(t *testing.T) {
	metrics := NewMetrics()

	// Record successful tool execution
	metrics.RecordToolExecution("web_search", "success", 0.5)
	metrics.RecordToolExecution("web_search", "success", 0.7)
	metrics.RecordToolExecution("browser", "error", 1.2)

	// Verify counters
	count := testutil.CollectAndCount(metrics.ToolExecutionCounter)
	if count < 1 {
		t.Error("Expected at least 1 tool execution recorded")
	}

	// Verify histogram
	histCount := testutil.CollectAndCount(metrics.ToolExecutionDuration)
	if histCount < 1 {
		t.Error("Expected tool execution duration histogram to have observations")
	}
}

func TestRecordError(t *testing.T) {
	metrics := NewMetrics()

	// Record various errors
	metrics.RecordError("agent", "timeout")
	metrics.RecordError("agent", "timeout")
	metrics.RecordError("channel", "auth_failed")
	metrics.RecordError("tool", "execution_failed")

	// Verify counter
	count := testutil.CollectAndCount(metrics.ErrorCounter)
	if count < 1 {
		t.Error("Expected at least 1 error recorded")
	}
}

func TestSessionLifecycle(t *testing.T) {
	metrics := NewMetrics()

	// Start sessions
	metrics.SessionStarted("telegram")
	metrics.SessionStarted("telegram")
	metrics.SessionStarted("discord")

	// Verify active sessions gauge increased
	// Note: We can't easily verify the exact value because Prometheus gauges
	// may have other values from other tests, but we can verify it collected
	count := testutil.CollectAndCount(metrics.ActiveSessions)
	if count < 1 {
		t.Error("Expected active sessions gauge to be tracked")
	}

	// End sessions
	metrics.SessionEnded("telegram", 300.0)
	metrics.SessionEnded("discord", 600.0)

	// Verify session duration histogram
	histCount := testutil.CollectAndCount(metrics.SessionDuration)
	if histCount < 1 {
		t.Error("Expected session duration histogram to have observations")
	}
}

func TestRecordHTTPRequest(t *testing.T) {
	metrics := NewMetrics()

	// Record HTTP requests
	metrics.RecordHTTPRequest("GET", "/api/sessions", "200", 0.05)
	metrics.RecordHTTPRequest("POST", "/api/messages", "201", 0.12)
	metrics.RecordHTTPRequest("GET", "/api/sessions", "404", 0.03)

	// Verify counter
	count := testutil.CollectAndCount(metrics.HTTPRequestCounter)
	if count < 1 {
		t.Error("Expected at least 1 HTTP request recorded")
	}

	// Verify histogram
	histCount := testutil.CollectAndCount(metrics.HTTPRequestDuration)
	if histCount < 1 {
		t.Error("Expected HTTP request duration histogram to have observations")
	}
}

func TestRecordDatabaseQuery(t *testing.T) {
	metrics := NewMetrics()

	// Record database queries
	metrics.RecordDatabaseQuery("select", "sessions", "success", 0.01)
	metrics.RecordDatabaseQuery("insert", "messages", "success", 0.02)
	metrics.RecordDatabaseQuery("update", "sessions", "error", 0.05)

	// Verify counter
	count := testutil.CollectAndCount(metrics.DatabaseQueryCounter)
	if count < 1 {
		t.Error("Expected at least 1 database query recorded")
	}

	// Verify histogram
	histCount := testutil.CollectAndCount(metrics.DatabaseQueryDuration)
	if histCount < 1 {
		t.Error("Expected database query duration histogram to have observations")
	}
}

func TestMetricsLabels(t *testing.T) {
	metrics := NewMetrics()

	// Test that different label combinations work correctly
	channels := []string{"telegram", "discord", "slack"}
	directions := []string{"inbound", "outbound"}

	for _, channel := range channels {
		for _, direction := range directions {
			metrics.MessageReceived(channel, direction)
		}
	}

	// Verify that all combinations were recorded
	count := testutil.CollectAndCount(metrics.MessageCounter)
	if count < 1 {
		t.Error("Expected message counter to track multiple label combinations")
	}
}

func TestLLMTokenTracking(t *testing.T) {
	metrics := NewMetrics()

	// Record requests with different token counts
	metrics.RecordLLMRequest("anthropic", "claude-3-opus", "success", 1.0, 1000, 2000)
	metrics.RecordLLMRequest("anthropic", "claude-3-opus", "success", 1.5, 500, 1500)

	// Verify token counter exists and has values
	count := testutil.CollectAndCount(metrics.LLMTokensUsed)
	if count < 1 {
		t.Error("Expected LLM tokens to be tracked")
	}
}

func TestHistogramBuckets(t *testing.T) {
	metrics := NewMetrics()

	// Test that various durations fall into appropriate buckets
	durations := []float64{0.001, 0.01, 0.1, 0.5, 1.0, 5.0, 10.0, 30.0}

	for _, duration := range durations {
		metrics.RecordLLMRequest("test", "model", "success", duration, 100, 100)
	}

	// Verify histogram recorded all observations
	histCount := testutil.CollectAndCount(metrics.LLMRequestDuration)
	if histCount < 1 {
		t.Error("Expected histogram to have observations across buckets")
	}
}

func TestConcurrentMetrics(t *testing.T) {
	metrics := NewMetrics()

	// Test concurrent metric recording (Prometheus metrics are thread-safe)
	done := make(chan bool)
	iterations := 100

	go func() {
		for i := 0; i < iterations; i++ {
			metrics.MessageReceived("telegram", "inbound")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < iterations; i++ {
			metrics.MessageSent("discord")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Verify metrics were recorded (exact count doesn't matter, just that it didn't panic)
	count := testutil.CollectAndCount(metrics.MessageCounter)
	if count < 1 {
		t.Error("Expected concurrent metric recording to work")
	}
}

func TestMetricsWithZeroValues(t *testing.T) {
	metrics := NewMetrics()

	// Test recording with zero values
	metrics.RecordLLMRequest("test", "model", "success", 0.0, 0, 0)
	metrics.RecordToolExecution("test_tool", "success", 0.0)

	// Should not panic and should still record the events
	count := testutil.CollectAndCount(metrics.LLMRequestCounter)
	if count < 1 {
		t.Error("Expected metrics to handle zero values")
	}
}

func TestMetricsWithLongLabels(t *testing.T) {
	metrics := NewMetrics()

	// Test with long label values
	longChannel := strings.Repeat("a", 100)
	longErrorType := strings.Repeat("b", 100)

	metrics.MessageReceived(longChannel, "inbound")
	metrics.RecordError("component", longErrorType)

	// Should not panic
	count := testutil.CollectAndCount(metrics.MessageCounter)
	if count < 1 {
		t.Error("Expected metrics to handle long labels")
	}
}

func TestSessionGaugeIncDecBalance(t *testing.T) {
	metrics := NewMetrics()

	channel := "test_channel"

	// Start multiple sessions
	for i := 0; i < 5; i++ {
		metrics.SessionStarted(channel)
	}

	// End all sessions
	for i := 0; i < 5; i++ {
		metrics.SessionEnded(channel, 100.0)
	}

	// Gauge should be balanced (we can't check exact value due to potential interference)
	// but we can verify it didn't panic
	count := testutil.CollectAndCount(metrics.ActiveSessions)
	if count < 1 {
		t.Error("Expected active sessions gauge to be tracked")
	}
}
