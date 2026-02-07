package gateway

import (
	"net/http"
	"sync"
	"testing"
)

func TestIsIdempotencyDuplicate(t *testing.T) {
	s := &wsSession{
		idempotency: make(map[string]struct{}),
	}

	// First call with a key should not be a duplicate.
	if s.isIdempotencyDuplicate("key-1") {
		t.Fatal("first call should not be duplicate")
	}

	// Second call with same key should be a duplicate.
	if !s.isIdempotencyDuplicate("key-1") {
		t.Fatal("second call should be duplicate")
	}

	// Different key should not be a duplicate.
	if s.isIdempotencyDuplicate("key-2") {
		t.Fatal("different key should not be duplicate")
	}
}

func TestIsIdempotencyDuplicate_EmptyKey(t *testing.T) {
	s := &wsSession{
		idempotency: make(map[string]struct{}),
	}
	// Empty keys should never be considered duplicates.
	if s.isIdempotencyDuplicate("") {
		t.Fatal("empty key should not be duplicate")
	}
	if s.isIdempotencyDuplicate("   ") {
		t.Fatal("whitespace-only key should not be duplicate")
	}
}

func TestIsIdempotencyDuplicate_Concurrent(t *testing.T) {
	s := &wsSession{
		idempotency: make(map[string]struct{}),
	}

	const goroutines = 50
	var wg sync.WaitGroup
	duplicates := make([]bool, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			duplicates[idx] = s.isIdempotencyDuplicate("shared-key")
		}(i)
	}
	wg.Wait()

	// Exactly one goroutine should see it as non-duplicate (first insertion).
	nonDups := 0
	for _, dup := range duplicates {
		if !dup {
			nonDups++
		}
	}
	if nonDups != 1 {
		t.Fatalf("expected exactly 1 non-duplicate, got %d", nonDups)
	}
}

func TestSupportedWSMethods(t *testing.T) {
	methods := supportedWSMethods()
	if len(methods) == 0 {
		t.Fatal("expected non-empty methods list")
	}
	expected := map[string]bool{
		"connect":        false,
		"health":         false,
		"ping":           false,
		"chat.send":      false,
		"chat.history":   false,
		"chat.abort":     false,
		"sessions.list":  false,
		"sessions.patch": false,
	}
	for _, m := range methods {
		if _, ok := expected[m]; !ok {
			t.Errorf("unexpected method: %s", m)
		}
		expected[m] = true
	}
	for m, found := range expected {
		if !found {
			t.Errorf("missing expected method: %s", m)
		}
	}
}

func TestSupportedWSEvents(t *testing.T) {
	events := supportedWSEvents()
	if len(events) == 0 {
		t.Fatal("expected non-empty events list")
	}
	expected := map[string]bool{
		"tick":          false,
		"chat.chunk":    false,
		"chat.complete": false,
		"error":         false,
		"tool.call":     false,
		"session.event": false,
		"pong":          false,
	}
	for _, e := range events {
		if _, ok := expected[e]; !ok {
			t.Errorf("unexpected event: %s", e)
		}
		expected[e] = true
	}
	for e, found := range expected {
		if !found {
			t.Errorf("missing expected event: %s", e)
		}
	}
}

func TestRequestHostFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "from Host header",
			host:     "example.com",
			expected: "example.com",
		},
		{
			name:     "with port",
			host:     "example.com:8080",
			expected: "example.com:8080",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "http://"+tt.host+"/ws", nil)
			got := requestHostFromRequest(r)
			if got != tt.expected {
				t.Errorf("requestHostFromRequest() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRequestHostFromRequest_Nil(t *testing.T) {
	got := requestHostFromRequest(nil)
	if got != "" {
		t.Errorf("expected empty string for nil request, got %q", got)
	}
}

func TestForwardedProtoFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected string
	}{
		{
			name:     "with X-Forwarded-Proto",
			headers:  map[string]string{"X-Forwarded-Proto": "https"},
			expected: "https",
		},
		{
			name:     "without header",
			headers:  map[string]string{},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "http://example.com/ws", nil)
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			got := forwardedProtoFromRequest(r)
			if got != tt.expected {
				t.Errorf("forwardedProtoFromRequest() = %q, want %q", got, tt.expected)
			}
		})
	}
}
