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
