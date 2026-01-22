package web

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/auth"
)

// LoggingMiddleware logs HTTP requests.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			// Log request
			if logger != nil {
				logger.Debug("http request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", wrapped.status,
					"duration", time.Since(start),
					"remote_addr", r.RemoteAddr,
				)
			}
		})
	}
}

// AuthMiddleware enforces authentication for HTTP requests.
func AuthMiddleware(service *auth.Service, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for static files
			if strings.HasPrefix(r.URL.Path, "/static/") {
				next.ServeHTTP(w, r)
				return
			}

			// Skip auth if service is nil or disabled
			if service == nil || !service.Enabled() {
				next.ServeHTTP(w, r)
				return
			}

			// Try Bearer token first
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				token := strings.TrimSpace(authHeader[7:])
				user, err := service.ValidateJWT(token)
				if err == nil {
					ctx := auth.WithUser(r.Context(), user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if logger != nil {
					logger.Warn("jwt validation failed", "error", err)
				}
			}

			// Try API key
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				apiKey = r.Header.Get("Api-Key")
			}
			if apiKey != "" {
				user, err := service.ValidateAPIKey(apiKey)
				if err == nil {
					ctx := auth.WithUser(r.Context(), user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if logger != nil {
					logger.Warn("api key validation failed", "error", err)
				}
			}

			// Try cookie-based session
			cookie, err := r.Cookie("nexus_session")
			if err == nil && cookie.Value != "" {
				user, err := service.ValidateJWT(cookie.Value)
				if err == nil {
					ctx := auth.WithUser(r.Context(), user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Check for query parameter token (for htmx requests)
			tokenParam := r.URL.Query().Get("token")
			if tokenParam != "" {
				user, err := service.ValidateJWT(tokenParam)
				if err == nil {
					ctx := auth.WithUser(r.Context(), user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Authentication failed
			if isHTMXRequest(r) || isAPIRequest(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if _, err := w.Write([]byte(`{"error":"unauthorized"}`)); err != nil {
					return
				}
				return
			}

			// For browser requests, show 401 page
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Unauthorized</title></head>
<body>
<h1>401 Unauthorized</h1>
<p>Authentication required to access this page.</p>
</body>
</html>`)); err != nil {
				return
			}
		})
	}
}

// CORSMiddleware adds CORS headers for API requests.
func CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowed := false
			for _, o := range allowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}

			if allowed && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// isHTMXRequest checks if the request is from htmx.
func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// isAPIRequest checks if the request is for the API.
func isAPIRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") ||
		r.Header.Get("Accept") == "application/json"
}
