// Package heartbeat provides heartbeat functionality with active hours support.
package heartbeat

import (
	"fmt"
	"regexp"
	"time"
)

// ActiveHoursConfig configures when heartbeats should run.
type ActiveHoursConfig struct {
	// Enabled turns on active hours restriction.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Start time in HH:MM format (e.g., "09:00").
	Start string `json:"start" yaml:"start"`

	// End time in HH:MM format (e.g., "17:00"). Use "24:00" for midnight.
	End string `json:"end" yaml:"end"`

	// Timezone for time calculations (e.g., "America/New_York", "local", "user").
	Timezone string `json:"timezone" yaml:"timezone"`

	// Days of week when active (0=Sunday, 1=Monday, ..., 6=Saturday).
	// Empty means all days.
	Days []int `json:"days" yaml:"days"`
}

// DefaultActiveHoursConfig returns a config for business hours.
func DefaultActiveHoursConfig() *ActiveHoursConfig {
	return &ActiveHoursConfig{
		Enabled:  false,
		Start:    "09:00",
		End:      "17:00",
		Timezone: "local",
		Days:     []int{1, 2, 3, 4, 5}, // Monday-Friday
	}
}

var timePattern = regexp.MustCompile(`^([01]\d|2[0-3]|24):([0-5]\d)$`)

// parseTime parses HH:MM format and returns minutes since midnight.
func parseTime(s string, allow24 bool) (int, error) {
	if !timePattern.MatchString(s) {
		return 0, fmt.Errorf("invalid time format: %s (expected HH:MM)", s)
	}

	var hour, minute int
	_, err := fmt.Sscanf(s, "%d:%d", &hour, &minute)
	if err != nil {
		return 0, err
	}

	if hour == 24 {
		if !allow24 || minute != 0 {
			return 0, fmt.Errorf("24:00 is only valid for end time")
		}
		return 24 * 60, nil
	}

	return hour*60 + minute, nil
}

// resolveTimezone resolves the timezone string to a *time.Location.
func resolveTimezone(tz string, userTz string) (*time.Location, error) {
	switch tz {
	case "", "local":
		return time.Local, nil
	case "user":
		if userTz != "" {
			return time.LoadLocation(userTz)
		}
		return time.Local, nil
	case "utc", "UTC":
		return time.UTC, nil
	default:
		return time.LoadLocation(tz)
	}
}

// IsActiveNow checks if the current time falls within active hours.
func (c *ActiveHoursConfig) IsActiveNow(userTimezone string) (bool, error) {
	return c.IsActiveAt(time.Now(), userTimezone)
}

// IsActiveAt checks if the given time falls within active hours.
func (c *ActiveHoursConfig) IsActiveAt(t time.Time, userTimezone string) (bool, error) {
	if !c.Enabled {
		return true, nil
	}

	// Resolve timezone
	loc, err := resolveTimezone(c.Timezone, userTimezone)
	if err != nil {
		return false, fmt.Errorf("invalid timezone %q: %w", c.Timezone, err)
	}

	// Convert to target timezone
	localTime := t.In(loc)

	// Check day of week
	if len(c.Days) > 0 {
		dayOK := false
		weekday := int(localTime.Weekday())
		for _, d := range c.Days {
			if d == weekday {
				dayOK = true
				break
			}
		}
		if !dayOK {
			return false, nil
		}
	}

	// Parse start and end times
	startMinutes, err := parseTime(c.Start, false)
	if err != nil {
		return false, fmt.Errorf("invalid start time: %w", err)
	}

	endMinutes, err := parseTime(c.End, true)
	if err != nil {
		return false, fmt.Errorf("invalid end time: %w", err)
	}

	// Calculate current minutes since midnight
	currentMinutes := localTime.Hour()*60 + localTime.Minute()

	// Check if within range
	if startMinutes <= endMinutes {
		// Normal range (e.g., 09:00-17:00)
		return currentMinutes >= startMinutes && currentMinutes < endMinutes, nil
	}

	// Overnight range (e.g., 22:00-06:00)
	return currentMinutes >= startMinutes || currentMinutes < endMinutes, nil
}

// NextActiveTime returns the next time that will be within active hours.
func (c *ActiveHoursConfig) NextActiveTime(t time.Time, userTimezone string) (time.Time, error) {
	if !c.Enabled {
		return t, nil
	}

	// Resolve timezone
	loc, err := resolveTimezone(c.Timezone, userTimezone)
	if err != nil {
		return t, fmt.Errorf("invalid timezone %q: %w", c.Timezone, err)
	}

	localTime := t.In(loc)

	// Parse start time
	startMinutes, err := parseTime(c.Start, false)
	if err != nil {
		return t, err
	}

	// Check up to 7 days ahead
	for dayOffset := 0; dayOffset < 8; dayOffset++ {
		checkTime := localTime.AddDate(0, 0, dayOffset)

		// If same day, start from current time; otherwise start of day
		if dayOffset > 0 {
			checkTime = time.Date(
				checkTime.Year(), checkTime.Month(), checkTime.Day(),
				0, 0, 0, 0, loc,
			)
		}

		// Check if this day is active
		if len(c.Days) > 0 {
			dayOK := false
			weekday := int(checkTime.Weekday())
			for _, d := range c.Days {
				if d == weekday {
					dayOK = true
					break
				}
			}
			if !dayOK {
				continue
			}
		}

		// Calculate target time on this day
		startTime := time.Date(
			checkTime.Year(), checkTime.Month(), checkTime.Day(),
			startMinutes/60, startMinutes%60, 0, 0, loc,
		)

		// If we're on the same day and past start time, check if we're in active window
		if dayOffset == 0 {
			active, _ := c.IsActiveAt(checkTime, userTimezone)
			if active {
				return t, nil // Already active
			}
			// If not active but start is later today
			if startTime.After(checkTime) {
				return startTime, nil
			}
			// Otherwise, continue to next day
			continue
		}

		return startTime, nil
	}

	// Fallback: return original time (shouldn't happen with valid config)
	return t, nil
}

// UntilActive returns the duration until the next active period.
func (c *ActiveHoursConfig) UntilActive(t time.Time, userTimezone string) (time.Duration, error) {
	nextActive, err := c.NextActiveTime(t, userTimezone)
	if err != nil {
		return 0, err
	}
	if nextActive.Before(t) || nextActive.Equal(t) {
		return 0, nil
	}
	return nextActive.Sub(t), nil
}

// HeartbeatSchedule determines when heartbeats should run.
type HeartbeatSchedule struct {
	// Interval is the time between heartbeats.
	Interval time.Duration

	// ActiveHours restricts when heartbeats can run.
	ActiveHours *ActiveHoursConfig

	// UserTimezone is the user's timezone for active hours calculation.
	UserTimezone string

	// Jitter adds randomness to prevent thundering herd.
	Jitter time.Duration
}

// NextHeartbeat returns the next heartbeat time from the given time.
func (s *HeartbeatSchedule) NextHeartbeat(from time.Time) (time.Time, error) {
	next := from.Add(s.Interval)

	// Add jitter if configured
	if s.Jitter > 0 {
		// Simple deterministic jitter based on timestamp
		jitterNanos := next.UnixNano() % int64(s.Jitter)
		next = next.Add(time.Duration(jitterNanos))
	}

	// Apply active hours restriction
	if s.ActiveHours != nil && s.ActiveHours.Enabled {
		activeNext, err := s.ActiveHours.NextActiveTime(next, s.UserTimezone)
		if err != nil {
			return next, err
		}
		next = activeNext
	}

	return next, nil
}

// ShouldRunNow returns true if a heartbeat should run now.
func (s *HeartbeatSchedule) ShouldRunNow(lastRun time.Time) (bool, error) {
	if time.Since(lastRun) < s.Interval {
		return false, nil
	}

	if s.ActiveHours != nil && s.ActiveHours.Enabled {
		return s.ActiveHours.IsActiveNow(s.UserTimezone)
	}

	return true, nil
}
