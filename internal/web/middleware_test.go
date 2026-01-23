package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestLoggingMiddleware(t *testing.T) {
	t.Run("logs request with nil logger", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := LoggingMiddleware(nil)
		wrapped := middleware(handler)

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("logs request with logger", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})

		middleware := LoggingMiddleware(testLogger())
		wrapped := middleware(handler)

		req := httptest.NewRequest("POST", "/api/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	t.Run("skips static files", func(t *testing.T) {
		called := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		middleware := AuthMiddleware(nil, testLogger())
		wrapped := middleware(handler)

		req := httptest.NewRequest("GET", "/static/css/style.css", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if !called {
			t.Error("handler should be called for static files")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("skips when service is nil", func(t *testing.T) {
		called := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		})

		middleware := AuthMiddleware(nil, testLogger())
		wrapped := middleware(handler)

		req := httptest.NewRequest("GET", "/protected", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if !called {
			t.Error("handler should be called when auth service is nil")
		}
	})

	t.Run("returns 401 for API request without auth", func(t *testing.T) {
		// This test requires a real auth service that's enabled
		// For now, test the helper functions
	})
}

func TestCORSMiddleware(t *testing.T) {
	t.Run("allows wildcard origin", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := CORSMiddleware([]string{"*"})
		wrapped := middleware(handler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
			t.Errorf("CORS header not set correctly")
		}
	})

	t.Run("allows specific origin", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := CORSMiddleware([]string{"http://allowed.com"})
		wrapped := middleware(handler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Origin", "http://allowed.com")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "http://allowed.com" {
			t.Errorf("CORS header = %q, want %q", rec.Header().Get("Access-Control-Allow-Origin"), "http://allowed.com")
		}
	})

	t.Run("rejects disallowed origin", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := CORSMiddleware([]string{"http://allowed.com"})
		wrapped := middleware(handler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Origin", "http://notallowed.com")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("CORS header should not be set for disallowed origin")
		}
	})

	t.Run("handles preflight request", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called for preflight")
		})

		middleware := CORSMiddleware([]string{"*"})
		wrapped := middleware(handler)

		req := httptest.NewRequest("OPTIONS", "/api/test", nil)
		req.Header.Set("Origin", "http://example.com")
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
	})

	t.Run("no CORS without origin header", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := CORSMiddleware([]string{"*"})
		wrapped := middleware(handler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		rec := httptest.NewRecorder()

		wrapped.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("CORS header should not be set without Origin header")
		}
	})
}

func TestResponseWriter(t *testing.T) {
	t.Run("captures status code on WriteHeader", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

		rw.WriteHeader(http.StatusNotFound)

		if rw.status != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rw.status, http.StatusNotFound)
		}
		if !rw.wroteHeader {
			t.Error("wroteHeader should be true")
		}
	})

	t.Run("prevents double WriteHeader", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

		rw.WriteHeader(http.StatusNotFound)
		rw.WriteHeader(http.StatusOK) // Should be ignored

		if rw.status != http.StatusNotFound {
			t.Errorf("status = %d, want %d (first call)", rw.status, http.StatusNotFound)
		}
	})

	t.Run("Write sets default status", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

		_, err := rw.Write([]byte("test"))
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}

		if !rw.wroteHeader {
			t.Error("wroteHeader should be true after Write")
		}
	})
}

func TestIsHTMXRequest(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected bool
	}{
		{"htmx request", "true", true},
		{"not htmx", "", false},
		{"wrong value", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("HX-Request", tt.header)
			}
			if got := isHTMXRequest(req); got != tt.expected {
				t.Errorf("isHTMXRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsAPIRequest(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		accept   string
		expected bool
	}{
		{"api path", "/api/test", "", true},
		{"api path nested", "/api/v1/users", "", true},
		{"json accept", "/other", "application/json", true},
		{"non-api path", "/web/page", "text/html", false},
		{"empty", "/", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			if got := isAPIRequest(req); got != tt.expected {
				t.Errorf("isAPIRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}
