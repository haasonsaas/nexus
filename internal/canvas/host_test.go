package canvas

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewHost(t *testing.T) {
	t.Run("creates host with valid root", func(t *testing.T) {
		tmpDir := t.TempDir()
		host, err := NewHost(tmpDir, nil)
		if err != nil {
			t.Errorf("NewHost error: %v", err)
		}
		if host == nil {
			t.Error("expected non-nil host")
		}
	})

	t.Run("returns error for empty root", func(t *testing.T) {
		_, err := NewHost("", nil)
		if err == nil {
			t.Error("expected error for empty root")
		}
	})

	t.Run("returns error for whitespace root", func(t *testing.T) {
		_, err := NewHost("   ", nil)
		if err == nil {
			t.Error("expected error for whitespace root")
		}
	})

	t.Run("uses custom logger", func(t *testing.T) {
		tmpDir := t.TempDir()
		logger := slog.Default()
		host, err := NewHost(tmpDir, logger)
		if err != nil {
			t.Errorf("NewHost error: %v", err)
		}
		if host.logger == nil {
			t.Error("expected logger to be set")
		}
	})
}

func TestHost_Handler(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	// Create test HTML file
	htmlContent := `<!DOCTYPE html><html><body><h1>Test</h1></body></html>`
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(htmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a CSS file
	cssContent := `body { color: red; }`
	if err := os.WriteFile(filepath.Join(tmpDir, "style.css"), []byte(cssContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create subdirectory with index
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	subContent := `<!DOCTYPE html><html><body><h1>Sub</h1></body></html>`
	if err := os.WriteFile(filepath.Join(subDir, "index.html"), []byte(subContent), 0644); err != nil {
		t.Fatal(err)
	}

	host, err := NewHost(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := host.Handler()

	tests := []struct {
		name           string
		path           string
		wantStatus     int
		wantContains   string
		wantNotContain string
		wantType       string
	}{
		{
			name:         "serves root index.html",
			path:         "/",
			wantStatus:   http.StatusOK,
			wantContains: "<h1>Test</h1>",
			wantType:     "text/html",
		},
		{
			name:         "injects live reload in HTML",
			path:         "/index.html",
			wantStatus:   http.StatusOK,
			wantContains: `<script src="/canvas/live.js"></script>`,
			wantType:     "text/html",
		},
		{
			name:         "serves CSS without modification",
			path:         "/style.css",
			wantStatus:   http.StatusOK,
			wantContains: "body { color: red; }",
		},
		{
			name:         "serves subdirectory index",
			path:         "/sub/",
			wantStatus:   http.StatusOK,
			wantContains: "<h1>Sub</h1>",
			wantType:     "text/html",
		},
		{
			name:       "returns 404 for missing file",
			path:       "/nonexistent.html",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "blocks path traversal",
			path:       "/../../../etc/passwd",
			wantStatus: http.StatusBadRequest, // http.ServeFile returns 400 for invalid paths
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			body := rec.Body.String()
			if tt.wantContains != "" && !strings.Contains(body, tt.wantContains) {
				t.Errorf("body should contain %q, got: %s", tt.wantContains, body)
			}

			if tt.wantNotContain != "" && strings.Contains(body, tt.wantNotContain) {
				t.Errorf("body should not contain %q", tt.wantNotContain)
			}

			if tt.wantType != "" {
				contentType := rec.Header().Get("Content-Type")
				if !strings.Contains(contentType, tt.wantType) {
					t.Errorf("Content-Type = %q, want to contain %q", contentType, tt.wantType)
				}
			}
		})
	}
}

func TestHost_LiveReloadScriptHandler(t *testing.T) {
	tmpDir := t.TempDir()
	host, err := NewHost(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := host.LiveReloadScriptHandler()
	req := httptest.NewRequest(http.MethodGet, "/canvas/live.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/javascript" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/javascript")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "EventSource") {
		t.Error("script should contain EventSource")
	}
	if !strings.Contains(body, "/canvas/live") {
		t.Error("script should reference /canvas/live endpoint")
	}
}

func TestHost_LiveReloadHandler(t *testing.T) {
	tmpDir := t.TempDir()
	host, err := NewHost(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := host.LiveReloadHandler()

	// Use a pipe to simulate SSE streaming
	req := httptest.NewRequest(http.MethodGet, "/canvas/live", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	// Run handler in goroutine
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rec, req)
		close(done)
	}()

	// Give handler time to start and send hello event
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop handler
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish")
	}

	// Check headers
	contentType := rec.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/event-stream")
	}

	cacheControl := rec.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cacheControl, "no-cache")
	}

	// Check body contains hello event
	body := rec.Body.String()
	if !strings.Contains(body, "event: hello") {
		t.Errorf("body should contain hello event, got: %s", body)
	}
}

func TestHost_StartAndClose(t *testing.T) {
	tmpDir := t.TempDir()
	host, err := NewHost(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := host.Start(ctx); err != nil {
		t.Errorf("Start error: %v", err)
	}

	host.mu.RLock()
	watching := host.watching
	host.mu.RUnlock()

	if !watching {
		t.Error("expected watching to be true after Start")
	}

	if err := host.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}

	host.mu.RLock()
	watching = host.watching
	host.mu.RUnlock()

	if watching {
		t.Error("expected watching to be false after Close")
	}
}

func TestHost_NilSafety(t *testing.T) {
	var host *Host

	// These should not panic
	if err := host.Start(context.Background()); err != nil {
		t.Errorf("Start on nil should return nil, got: %v", err)
	}
	if err := host.Close(); err != nil {
		t.Errorf("Close on nil should return nil, got: %v", err)
	}
}

func TestInjectLiveReload(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "injects before closing body",
			html:     `<html><body><h1>Test</h1></body></html>`,
			expected: `<html><body><h1>Test</h1><script src="/canvas/live.js"></script></body></html>`,
		},
		{
			name:     "appends if no body tag",
			html:     `<html><h1>Test</h1></html>`,
			expected: `<html><h1>Test</h1></html><script src="/canvas/live.js"></script>`,
		},
		{
			name:     "handles empty html",
			html:     ``,
			expected: `<script src="/canvas/live.js"></script>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := injectLiveReload(tt.html)
			if result != tt.expected {
				t.Errorf("injectLiveReload(%q) = %q, want %q", tt.html, result, tt.expected)
			}
		})
	}
}

func TestHost_BroadcastReload(t *testing.T) {
	tmpDir := t.TempDir()
	host, err := NewHost(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Add test client
	ch := make(chan struct{}, 1)
	host.addClient(ch)
	defer host.removeClient(ch)

	// Broadcast reload
	host.broadcastReload()

	// Check if client received notification
	select {
	case <-ch:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("client did not receive reload notification")
	}
}

func TestHost_ClientManagement(t *testing.T) {
	tmpDir := t.TempDir()
	host, err := NewHost(tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	ch1 := make(chan struct{}, 1)
	ch2 := make(chan struct{}, 1)

	host.addClient(ch1)
	host.addClient(ch2)

	host.mu.RLock()
	count := len(host.clients)
	host.mu.RUnlock()

	if count != 2 {
		t.Errorf("client count = %d, want 2", count)
	}

	host.removeClient(ch1)

	host.mu.RLock()
	count = len(host.clients)
	host.mu.RUnlock()

	if count != 1 {
		t.Errorf("client count after remove = %d, want 1", count)
	}
}

// Benchmark the injectLiveReload function
func BenchmarkInjectLiveReload(b *testing.B) {
	html := `<!DOCTYPE html>
<html>
<head><title>Test</title></head>
<body>
<h1>Hello World</h1>
<p>This is a test page with some content.</p>
</body>
</html>`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = injectLiveReload(html)
	}
}

// mockFlusher implements http.Flusher for testing
type mockResponseWriter struct {
	http.ResponseWriter
}

func (m *mockResponseWriter) Flush() {}

// Read all from response without blocking
func readNonBlocking(r io.Reader) string {
	data, _ := io.ReadAll(r)
	return string(data)
}
