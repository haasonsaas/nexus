package cron

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/haasonsaas/nexus/internal/config"
)

var cronParser = cron.NewParser(
	cron.SecondOptional |
		cron.Minute |
		cron.Hour |
		cron.Dom |
		cron.Month |
		cron.Dow |
		cron.Descriptor,
)

// NewSchedule parses a schedule config into a Schedule.
func NewSchedule(cfg config.CronScheduleConfig) (Schedule, error) {
	if strings.TrimSpace(cfg.Cron) == "" && cfg.Every == 0 && strings.TrimSpace(cfg.At) == "" {
		return Schedule{}, fmt.Errorf("schedule is required")
	}
	sched := Schedule{
		CronExpr: strings.TrimSpace(cfg.Cron),
		Every:    cfg.Every,
		Timezone: strings.TrimSpace(cfg.Timezone),
	}
	if strings.TrimSpace(cfg.At) != "" {
		at, err := parseAt(cfg.At, sched.Timezone)
		if err != nil {
			return Schedule{}, err
		}
		sched.At = at
		sched.Kind = "at"
		return sched, nil
	}
	if sched.Every > 0 {
		sched.Kind = "every"
		return sched, nil
	}
	if sched.CronExpr != "" {
		if _, err := cronParser.Parse(sched.CronExpr); err != nil {
			return Schedule{}, fmt.Errorf("invalid cron expression: %w", err)
		}
		sched.Kind = "cron"
		return sched, nil
	}
	return Schedule{}, fmt.Errorf("invalid schedule")
}

// Next returns the next run time for the schedule after the given time.
func (s Schedule) Next(now time.Time) (time.Time, bool, error) {
	switch s.Kind {
	case "at":
		if s.At.IsZero() {
			return time.Time{}, false, fmt.Errorf("at schedule missing timestamp")
		}
		if now.After(s.At) {
			return time.Time{}, false, nil
		}
		return s.At, true, nil
	case "every":
		if s.Every <= 0 {
			return time.Time{}, false, fmt.Errorf("every schedule missing duration")
		}
		return now.Add(s.Every), true, nil
	case "cron":
		if s.CronExpr == "" {
			return time.Time{}, false, fmt.Errorf("cron schedule missing expression")
		}
		loc := now.Location()
		if s.Timezone != "" {
			if tz, err := time.LoadLocation(s.Timezone); err == nil {
				loc = tz
			}
		}
		schedule, err := cronParser.Parse(s.CronExpr)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("parse cron expression: %w", err)
		}
		next := schedule.Next(now.In(loc))
		return next, !next.IsZero(), nil
	default:
		return time.Time{}, false, fmt.Errorf("unknown schedule kind")
	}
}

func parseAt(value, tz string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("at schedule value required")
	}
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			if parsed, err := time.ParseInLocation(time.RFC3339, value, loc); err == nil {
				return parsed, nil
			}
			if parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc); err == nil {
				return parsed, nil
			}
		}
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse("2006-01-02 15:04", value); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid at schedule: %s", value)
}
