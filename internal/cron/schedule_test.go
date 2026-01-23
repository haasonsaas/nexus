package cron

import (
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestScheduleNextAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronScheduleConfig{At: "2026-01-01T10:00:00Z"}
	sched, err := NewSchedule(cfg)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	next, ok, err := sched.Next(now)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected schedule to be due")
	}
	if !next.Equal(now) {
		t.Fatalf("expected next run at %v, got %v", now, next)
	}
}

func TestScheduleNextEvery(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronScheduleConfig{Every: 5 * time.Minute}
	sched, err := NewSchedule(cfg)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	next, ok, err := sched.Next(now)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected schedule to be valid")
	}
	expected := now.Add(5 * time.Minute)
	if !next.Equal(expected) {
		t.Fatalf("expected next run at %v, got %v", expected, next)
	}
}

func TestScheduleNextCron(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronScheduleConfig{Cron: "0 */5 * * *"}
	sched, err := NewSchedule(cfg)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	next, ok, err := sched.Next(now)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected schedule to be valid")
	}
	if !next.After(now) {
		t.Fatalf("expected next run after now")
	}
}

func TestNewSchedule_EmptyRequired(t *testing.T) {
	cfg := config.CronScheduleConfig{}
	_, err := NewSchedule(cfg)
	if err == nil {
		t.Error("expected error for empty schedule")
	}
}

func TestNewSchedule_InvalidCron(t *testing.T) {
	cfg := config.CronScheduleConfig{Cron: "invalid cron expr"}
	_, err := NewSchedule(cfg)
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestNewSchedule_AtWithTimezone(t *testing.T) {
	cfg := config.CronScheduleConfig{
		At:       "2026-01-15 10:00",
		Timezone: "America/New_York",
	}
	sched, err := NewSchedule(cfg)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if sched.Kind != "at" {
		t.Errorf("Kind = %q, want %q", sched.Kind, "at")
	}
}

func TestNewSchedule_InvalidAt(t *testing.T) {
	cfg := config.CronScheduleConfig{
		At: "not-a-date",
	}
	_, err := NewSchedule(cfg)
	if err == nil {
		t.Error("expected error for invalid at value")
	}
}

func TestScheduleNext_AtPastDue(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	cfg := config.CronScheduleConfig{At: "2026-01-01T10:00:00Z"}
	sched, err := NewSchedule(cfg)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	_, ok, err := sched.Next(now)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if ok {
		t.Error("expected ok=false for past due schedule")
	}
}

func TestScheduleNext_UnknownKind(t *testing.T) {
	sched := Schedule{Kind: "unknown"}
	_, _, err := sched.Next(time.Now())
	if err == nil {
		t.Error("expected error for unknown schedule kind")
	}
}

func TestScheduleNext_AtMissingTimestamp(t *testing.T) {
	sched := Schedule{Kind: "at"}
	_, _, err := sched.Next(time.Now())
	if err == nil {
		t.Error("expected error for at schedule missing timestamp")
	}
}

func TestScheduleNext_EveryMissingDuration(t *testing.T) {
	sched := Schedule{Kind: "every", Every: 0}
	_, _, err := sched.Next(time.Now())
	if err == nil {
		t.Error("expected error for every schedule missing duration")
	}
}

func TestScheduleNext_CronMissingExpression(t *testing.T) {
	sched := Schedule{Kind: "cron", CronExpr: ""}
	_, _, err := sched.Next(time.Now())
	if err == nil {
		t.Error("expected error for cron schedule missing expression")
	}
}

func TestScheduleNext_CronWithTimezone(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	cfg := config.CronScheduleConfig{
		Cron:     "0 9 * * *", // 9 AM daily
		Timezone: "America/New_York",
	}
	sched, err := NewSchedule(cfg)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	next, ok, err := sched.Next(now)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
	if next.IsZero() {
		t.Error("expected non-zero next time")
	}
}

func TestJobType_Constants(t *testing.T) {
	if JobTypeMessage != "message" {
		t.Errorf("JobTypeMessage = %q, want %q", JobTypeMessage, "message")
	}
	if JobTypeAgent != "agent" {
		t.Errorf("JobTypeAgent = %q, want %q", JobTypeAgent, "agent")
	}
	if JobTypeWebhook != "webhook" {
		t.Errorf("JobTypeWebhook = %q, want %q", JobTypeWebhook, "webhook")
	}
}

func TestJob_Struct(t *testing.T) {
	job := Job{
		ID:      "job-1",
		Name:    "Test Job",
		Type:    JobTypeWebhook,
		Enabled: true,
		Schedule: Schedule{
			Kind:  "every",
			Every: 5 * time.Minute,
		},
	}

	if job.ID != "job-1" {
		t.Errorf("ID = %q", job.ID)
	}
	if job.Type != JobTypeWebhook {
		t.Errorf("Type = %v", job.Type)
	}
	if !job.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestSchedule_Struct(t *testing.T) {
	sched := Schedule{
		Kind:     "cron",
		CronExpr: "0 */5 * * *",
		Timezone: "UTC",
	}

	if sched.Kind != "cron" {
		t.Errorf("Kind = %q", sched.Kind)
	}
	if sched.CronExpr != "0 */5 * * *" {
		t.Errorf("CronExpr = %q", sched.CronExpr)
	}
}
