package observability

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
)

// Logger provides structured logging with built-in request correlation and
// sensitive data redaction.
//
// The logging system is built on Go's slog package and provides:
//   - Configurable log levels (DEBUG, INFO, WARN, ERROR)
//   - JSON output format for production environments
//   - Human-readable text format for development
//   - Automatic request ID correlation from context
//   - Redaction of sensitive data (API keys, tokens, passwords)
//   - Structured fields for rich log analysis
//
// Usage:
//
//	logger := observability.NewLogger(observability.LogConfig{
//	    Level:  "info",
//	    Format: "json",
//	})
//	logger.Info(ctx, "Processing message", "channel", "telegram", "user_id", "12345")
type Logger struct {
	logger  *slog.Logger
	config  LogConfig
	redacts []*regexp.Regexp
}

// LogConfig configures the logging behavior.
type LogConfig struct {
	// Level sets the minimum log level: "debug", "info", "warn", "error"
	Level string

	// Format specifies output format: "json" or "text"
	// JSON format is recommended for production; text for development
	Format string

	// Output is the writer for log output (defaults to os.Stdout)
	Output io.Writer

	// AddSource includes file and line number in log records
	AddSource bool

	// RedactPatterns are additional regex patterns for sensitive data redaction
	// Default patterns already cover common secrets (API keys, tokens, passwords)
	RedactPatterns []string
}

// ContextKey is the type for context keys used in logging.
type ContextKey string

const (
	// RequestIDKey is the context key for request IDs.
	RequestIDKey ContextKey = "request_id"

	// SessionIDKey is the context key for session IDs.
	SessionIDKey ContextKey = "session_id"

	// UserIDKey is the context key for user IDs.
	UserIDKey ContextKey = "user_id"

	// ChannelKey is the context key for channel type.
	ChannelKey ContextKey = "channel"
)

// DefaultRedactPatterns contains regex patterns for common sensitive data.
var DefaultRedactPatterns = []string{
	// API keys and tokens
	`(?i)(api[_-]?key|apikey)[\s:=]+["\']?([a-zA-Z0-9_\-]{16,})["\']?`,
	`(?i)(bearer|token)[\s:]+([a-zA-Z0-9_\-\.]{16,})`,
	`(?i)(secret|password|passwd|pwd)[\s:=]+["\']?([^\s"']{8,})["\']?`,

	// Anthropic API keys
	`sk-ant-[a-zA-Z0-9_-]{95,}`,

	// OpenAI API keys (48 chars after sk-)
	`sk-[a-zA-Z0-9]{48,}`,

	// JWT tokens
	`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`,

	// Generic hex secrets (32+ chars)
	`(?i)(secret|key|token)[\s:=]+["\']?([a-fA-F0-9]{32,})["\']?`,
}

// NewLogger creates a new structured logger with the given configuration.
//
// If config.Output is nil, logs are written to os.Stdout.
// If config.Level is empty or invalid, defaults to "info".
// If config.Format is empty, defaults to "json".
//
// Example:
//
//	logger := observability.NewLogger(observability.LogConfig{
//	    Level:     "debug",
//	    Format:    "text",
//	    AddSource: true,
//	})
func NewLogger(config LogConfig) *Logger {
	// Set defaults
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.Level == "" {
		config.Level = "info"
	}
	if config.Format == "" {
		config.Format = "json"
	}

	// Parse log level
	var level slog.Level
	switch strings.ToLower(config.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Create handler based on format
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: config.AddSource,
	}

	if config.Format == "json" {
		handler = slog.NewJSONHandler(config.Output, opts)
	} else {
		handler = slog.NewTextHandler(config.Output, opts)
	}

	// Compile redaction patterns
	redacts := make([]*regexp.Regexp, 0)
	allPatterns := append(DefaultRedactPatterns, config.RedactPatterns...)
	for _, pattern := range allPatterns {
		if re, err := regexp.Compile(pattern); err == nil {
			redacts = append(redacts, re)
		}
	}

	return &Logger{
		logger:  slog.New(handler),
		config:  config,
		redacts: redacts,
	}
}

// WithContext returns a new logger that includes context fields in all log records.
//
// It extracts well-known fields from the context:
//   - request_id
//   - session_id
//   - user_id
//   - channel
//
// Example:
//
//	ctx := context.WithValue(ctx, observability.RequestIDKey, "req-123")
//	logger := logger.WithContext(ctx)
//	logger.Info(ctx, "Processing request") // Automatically includes request_id=req-123
func (l *Logger) WithContext(ctx context.Context) *Logger {
	attrs := make([]slog.Attr, 0, 4)

	if requestID, ok := ctx.Value(RequestIDKey).(string); ok && requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	if sessionID, ok := ctx.Value(SessionIDKey).(string); ok && sessionID != "" {
		attrs = append(attrs, slog.String("session_id", sessionID))
	}
	if userID, ok := ctx.Value(UserIDKey).(string); ok && userID != "" {
		attrs = append(attrs, slog.String("user_id", userID))
	}
	if channel, ok := ctx.Value(ChannelKey).(string); ok && channel != "" {
		attrs = append(attrs, slog.String("channel", channel))
	}

	if len(attrs) == 0 {
		return l
	}

	// Convert []slog.Attr to []any for slog.Group
	anyAttrs := make([]any, len(attrs))
	for i, attr := range attrs {
		anyAttrs[i] = attr
	}

	return &Logger{
		logger:  l.logger.With(slog.Group("context", anyAttrs...)),
		config:  l.config,
		redacts: l.redacts,
	}
}

// Debug logs a debug-level message with optional key-value pairs.
//
// Example:
//
//	logger.Debug(ctx, "Cache hit", "key", "user:123", "ttl", 300)
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelDebug, msg, args...)
}

// Info logs an info-level message with optional key-value pairs.
//
// Example:
//
//	logger.Info(ctx, "Message received", "channel", "telegram", "bytes", 1024)
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelInfo, msg, args...)
}

// Warn logs a warning-level message with optional key-value pairs.
//
// Example:
//
//	logger.Warn(ctx, "Rate limit approaching", "current", 95, "max", 100)
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelWarn, msg, args...)
}

// Error logs an error-level message with optional key-value pairs.
// If an error is passed as one of the args, it's automatically extracted and redacted.
//
// Example:
//
//	logger.Error(ctx, "Failed to process message", "error", err, "retry_count", 3)
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelError, msg, args...)
}

// log is the internal logging implementation that handles redaction and context extraction.
func (l *Logger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	// Redact sensitive data from message
	msg = l.redactString(msg)

	// Redact sensitive data from args
	redactedArgs := make([]any, len(args))
	for i, arg := range args {
		redactedArgs[i] = l.redactValue(arg)
	}

	// Extract context fields
	attrs := make([]any, 0, len(redactedArgs)+8)

	if requestID, ok := ctx.Value(RequestIDKey).(string); ok && requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	if sessionID, ok := ctx.Value(SessionIDKey).(string); ok && sessionID != "" {
		attrs = append(attrs, "session_id", sessionID)
	}
	if userID, ok := ctx.Value(UserIDKey).(string); ok && userID != "" {
		attrs = append(attrs, "user_id", userID)
	}
	if channel, ok := ctx.Value(ChannelKey).(string); ok && channel != "" {
		attrs = append(attrs, "channel", channel)
	}

	attrs = append(attrs, redactedArgs...)

	l.logger.Log(ctx, level, msg, attrs...)
}

// redactValue redacts sensitive data from a value.
func (l *Logger) redactValue(v any) any {
	switch val := v.(type) {
	case string:
		return l.redactString(val)
	case error:
		return l.redactString(val.Error())
	case []byte:
		return l.redactString(string(val))
	case map[string]any:
		return l.redactMap(val)
	case map[string]string:
		m := make(map[string]any, len(val))
		for k, v := range val {
			m[k] = v
		}
		return l.redactMap(m)
	default:
		// For other types, try to convert to JSON and redact
		if b, err := json.Marshal(v); err == nil {
			return l.redactString(string(b))
		}
		return v
	}
}

// redactString applies all redaction patterns to a string.
func (l *Logger) redactString(s string) string {
	for _, re := range l.redacts {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// redactMap redacts sensitive data from a map.
func (l *Logger) redactMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	sensitiveKeys := map[string]bool{
		"password":      true,
		"passwd":        true,
		"secret":        true,
		"token":         true,
		"api_key":       true,
		"apikey":        true,
		"private_key":   true,
		"privatekey":    true,
		"auth":          true,
		"authorization": true,
	}

	for k, v := range m {
		lowerKey := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
		if sensitiveKeys[lowerKey] {
			result[k] = "[REDACTED]"
		} else {
			result[k] = l.redactValue(v)
		}
	}
	return result
}

// WithFields returns a new logger with the given fields added to all log records.
//
// Example:
//
//	componentLogger := logger.WithFields("component", "agent", "version", "1.0")
//	componentLogger.Info(ctx, "Starting up") // Includes component and version
func (l *Logger) WithFields(args ...any) *Logger {
	return &Logger{
		logger:  l.logger.With(args...),
		config:  l.config,
		redacts: l.redacts,
	}
}

// LogMiddleware returns an HTTP middleware that logs requests and responses.
//
// Example:
//
//	mux := http.NewServeMux()
//	mux.Handle("/api/", logger.LogMiddleware(apiHandler))
func (l *Logger) LogMiddleware(next func(w io.Writer, r io.Reader) error) func(w io.Writer, r io.Reader) error {
	return func(w io.Writer, r io.Reader) error {
		start := time.Now()
		err := next(w, r)
		duration := time.Since(start)

		ctx := context.Background()
		if err != nil {
			l.Error(ctx, "Request failed",
				"duration_ms", duration.Milliseconds(),
				"error", err,
			)
		} else {
			l.Info(ctx, "Request completed",
				"duration_ms", duration.Milliseconds(),
			)
		}
		return err
	}
}

// MustNewLogger is like NewLogger but panics if the logger cannot be created.
// Useful for initialization in main functions.
//
// Example:
//
//	logger := observability.MustNewLogger(observability.LogConfig{
//	    Level: os.Getenv("LOG_LEVEL"),
//	})
func MustNewLogger(config LogConfig) *Logger {
	logger := NewLogger(config)
	if logger == nil {
		panic("failed to create logger")
	}
	return logger
}

// AddRequestID adds a request ID to the context.
//
// Example:
//
//	ctx := observability.AddRequestID(ctx, "req-123")
//	logger.Info(ctx, "Processing") // Will include request_id=req-123
func AddRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// AddSessionID adds a session ID to the context.
//
// Example:
//
//	ctx := observability.AddSessionID(ctx, "sess-456")
func AddSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, SessionIDKey, sessionID)
}

// AddUserID adds a user ID to the context.
//
// Example:
//
//	ctx := observability.AddUserID(ctx, "user-789")
func AddUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// AddChannel adds a channel type to the context.
//
// Example:
//
//	ctx := observability.AddChannel(ctx, "telegram")
func AddChannel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ChannelKey, channel)
}

// GetRequestID retrieves the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetSessionID retrieves the session ID from the context.
func GetSessionID(ctx context.Context) string {
	if id, ok := ctx.Value(SessionIDKey).(string); ok {
		return id
	}
	return ""
}

// LogLevelFromString converts a string to a slog.Level.
// Returns LevelInfo if the string is not recognized.
func LogLevelFromString(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Sync flushes any buffered log entries (no-op for stdlib logger but included for interface compatibility).
func (l *Logger) Sync() error {
	// slog doesn't require explicit syncing, but this is here for interface compatibility
	// with other logging libraries that might need it
	return nil
}
