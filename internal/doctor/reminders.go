package doctor

import (
	"context"
	"fmt"
	"time"

	"github.com/haasonsaas/nexus/internal/tasks"
)

// ReminderStatus provides summary of active reminders.
type ReminderStatus struct {
	Active       int       `json:"active"`
	Pending      int       `json:"pending"`      // Scheduled but not yet fired
	Overdue      int       `json:"overdue"`      // Past due but not fired
	NextReminder time.Time `json:"next_reminder,omitempty"`
	Errors       []string  `json:"errors,omitempty"`
}

// ProbeReminderStatus checks the status of scheduled reminders.
func ProbeReminderStatus(ctx context.Context, store tasks.Store) *ReminderStatus {
	status := &ReminderStatus{}

	if store == nil {
		return status
	}

	// List active tasks that are reminders
	taskList, err := store.ListTasks(ctx, tasks.ListTasksOptions{
		Status: func() *tasks.TaskStatus { s := tasks.TaskStatusActive; return &s }(),
		Limit:  1000,
	})
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
		return status
	}

	now := time.Now()
	var nextRun time.Time

	for _, task := range taskList {
		// Check if this is a reminder
		if task.Metadata == nil {
			continue
		}
		taskType, ok := task.Metadata["type"].(string)
		if !ok || taskType != "reminder" {
			continue
		}

		status.Active++

		if !task.NextRunAt.IsZero() {
			if task.NextRunAt.After(now) {
				status.Pending++
				if nextRun.IsZero() || task.NextRunAt.Before(nextRun) {
					nextRun = task.NextRunAt
				}
			} else {
				// Past due but still active
				status.Overdue++
			}
		}
	}

	status.NextReminder = nextRun
	return status
}

// FormatReminderStatus returns a human-readable status summary.
func FormatReminderStatus(status *ReminderStatus) string {
	if status.Active == 0 {
		return "No active reminders"
	}

	result := ""
	result += formatCount(status.Active, "reminder") + " active"

	if status.Pending > 0 {
		result += ", " + formatCount(status.Pending, "pending")
	}
	if status.Overdue > 0 {
		result += ", " + formatCount(status.Overdue, "overdue")
	}

	if !status.NextReminder.IsZero() {
		dur := time.Until(status.NextReminder)
		if dur > 0 {
			result += " (next in " + formatDurationShort(dur) + ")"
		}
	}

	return result
}

func formatCount(n int, singular string) string {
	if n == 1 {
		return "1 " + singular
	}
	return formatNumber(n) + " " + singular + "s"
}

func formatNumber(n int) string {
	return fmt.Sprintf("%d", n)
}

func formatDurationShort(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return formatNumber(int(d.Minutes())) + "m"
	}
	if d < 24*time.Hour {
		hrs := int(d.Hours())
		if hrs == 1 {
			return "1h"
		}
		return formatNumber(hrs) + "h"
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1d"
	}
	return formatNumber(days) + "d"
}
