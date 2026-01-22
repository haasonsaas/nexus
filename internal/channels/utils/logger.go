package utils

import "log/slog"

// EnsureLogger returns the provided logger or slog.Default() if nil.
// This reduces boilerplate nil checks across the codebase.
func EnsureLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

// EnsureLoggerWithComponent returns a logger with the component attribute set.
func EnsureLoggerWithComponent(logger *slog.Logger, component string) *slog.Logger {
	return EnsureLogger(logger).With("component", component)
}
