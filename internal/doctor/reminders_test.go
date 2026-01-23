package doctor

import (
	"testing"
	"time"
)

func TestFormatReminderStatus(t *testing.T) {
	t.Run("no reminders", func(t *testing.T) {
		status := &ReminderStatus{}
		result := FormatReminderStatus(status)
		if result != "No active reminders" {
			t.Errorf("got %q, want 'No active reminders'", result)
		}
	})

	t.Run("active reminders without pending", func(t *testing.T) {
		status := &ReminderStatus{Active: 2}
		result := FormatReminderStatus(status)
		if result != "2 reminders active" {
			t.Errorf("got %q, want '2 reminders active'", result)
		}
	})

	t.Run("with pending", func(t *testing.T) {
		status := &ReminderStatus{Active: 3, Pending: 2}
		result := FormatReminderStatus(status)
		expected := "3 reminders active, 2 pendings"
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
	})

	t.Run("with overdue", func(t *testing.T) {
		status := &ReminderStatus{Active: 1, Overdue: 1}
		result := FormatReminderStatus(status)
		expected := "1 reminder active, 1 overdue"
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
	})
}

func TestFormatDurationShort(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{30 * time.Second, "<1m"},
		{5 * time.Minute, "5m"},
		{1 * time.Hour, "1h"},
		{3 * time.Hour, "3h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDurationShort(tt.d)
			if result != tt.expected {
				t.Errorf("formatDurationShort(%v) = %q, want %q", tt.d, result, tt.expected)
			}
		})
	}
}
