package utils

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestEnsureLogger(t *testing.T) {
	t.Run("returns provided logger when not nil", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))

		result := EnsureLogger(logger)
		if result != logger {
			t.Error("expected same logger instance")
		}
	})

	t.Run("returns default logger when nil", func(t *testing.T) {
		result := EnsureLogger(nil)
		if result == nil {
			t.Error("expected non-nil logger")
		}
		// Should return slog.Default()
		if result != slog.Default() {
			t.Error("expected slog.Default()")
		}
	})
}

func TestEnsureLoggerWithComponent(t *testing.T) {
	t.Run("adds component attribute", func(t *testing.T) {
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, nil)
		logger := slog.New(handler)

		result := EnsureLoggerWithComponent(logger, "test-component")
		result.Info("test message")

		output := buf.String()
		if !strings.Contains(output, "component=test-component") {
			t.Errorf("expected component attribute in output, got: %s", output)
		}
	})

	t.Run("handles nil logger", func(t *testing.T) {
		// Should not panic
		result := EnsureLoggerWithComponent(nil, "test-component")
		if result == nil {
			t.Error("expected non-nil logger")
		}
	})
}
