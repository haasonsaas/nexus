package sessions

import (
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestSessionExpiry_NeverMode(t *testing.T) {
	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode: ResetModeNever,
		},
	})

	session := &models.Session{
		UpdatedAt: time.Now().Add(-365 * 24 * time.Hour), // 1 year old
	}

	if expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM) {
		t.Error("CheckExpiry() with never mode should return false")
	}
}

func TestSessionExpiry_DailyMode(t *testing.T) {
	// Fix the current time to 2pm
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:   ResetModeDaily,
			AtHour: 9, // 9am reset
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })
	expiry.location = time.UTC

	tests := []struct {
		name      string
		updatedAt time.Time
		expected  bool
	}{
		{
			name:      "Updated before today's reset should expire",
			updatedAt: time.Date(2024, 1, 15, 8, 0, 0, 0, time.UTC),
			expected:  true,
		},
		{
			name:      "Updated after today's reset should not expire",
			updatedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			expected:  false,
		},
		{
			name:      "Updated yesterday should expire",
			updatedAt: time.Date(2024, 1, 14, 20, 0, 0, 0, time.UTC),
			expected:  true,
		},
		{
			name:      "Updated a week ago should expire",
			updatedAt: time.Date(2024, 1, 8, 10, 0, 0, 0, time.UTC),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &models.Session{
				UpdatedAt: tt.updatedAt,
			}
			got := expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM)
			if got != tt.expected {
				t.Errorf("CheckExpiry() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSessionExpiry_DailyMode_BeforeResetHour(t *testing.T) {
	// Fix the current time to 7am (before 9am reset)
	fixedNow := time.Date(2024, 1, 15, 7, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:   ResetModeDaily,
			AtHour: 9,
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })
	expiry.location = time.UTC

	tests := []struct {
		name      string
		updatedAt time.Time
		expected  bool
	}{
		{
			name:      "Updated yesterday before reset should expire",
			updatedAt: time.Date(2024, 1, 14, 8, 0, 0, 0, time.UTC),
			expected:  true,
		},
		{
			name:      "Updated yesterday after reset should not expire",
			updatedAt: time.Date(2024, 1, 14, 10, 0, 0, 0, time.UTC),
			expected:  false,
		},
		{
			name:      "Updated earlier today should not expire",
			updatedAt: time.Date(2024, 1, 15, 5, 0, 0, 0, time.UTC),
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &models.Session{
				UpdatedAt: tt.updatedAt,
			}
			got := expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM)
			if got != tt.expected {
				t.Errorf("CheckExpiry() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSessionExpiry_IdleMode(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	tests := []struct {
		name      string
		updatedAt time.Time
		expected  bool
	}{
		{
			name:      "Active 5 minutes ago should not expire",
			updatedAt: fixedNow.Add(-5 * time.Minute),
			expected:  false,
		},
		{
			name:      "Active 29 minutes ago should not expire",
			updatedAt: fixedNow.Add(-29 * time.Minute),
			expected:  false,
		},
		{
			name:      "Active exactly 30 minutes ago should expire",
			updatedAt: fixedNow.Add(-30 * time.Minute),
			expected:  true,
		},
		{
			name:      "Active 1 hour ago should expire",
			updatedAt: fixedNow.Add(-1 * time.Hour),
			expected:  true,
		},
		{
			name:      "Active yesterday should expire",
			updatedAt: fixedNow.Add(-24 * time.Hour),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &models.Session{
				UpdatedAt: tt.updatedAt,
			}
			got := expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM)
			if got != tt.expected {
				t.Errorf("CheckExpiry() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSessionExpiry_DailyIdleMode(t *testing.T) {
	// Fix the current time to 2pm
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeDailyIdle,
			AtHour:      9,
			IdleMinutes: 60,
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })
	expiry.location = time.UTC

	tests := []struct {
		name      string
		updatedAt time.Time
		expected  bool
	}{
		{
			name:      "Active after reset, not idle - should not expire",
			updatedAt: fixedNow.Add(-30 * time.Minute),
			expected:  false,
		},
		{
			name:      "Active after reset, but idle - should expire",
			updatedAt: fixedNow.Add(-90 * time.Minute),
			expected:  true,
		},
		{
			name:      "Active before reset - should expire (daily triggers)",
			updatedAt: time.Date(2024, 1, 15, 8, 0, 0, 0, time.UTC),
			expected:  true,
		},
		{
			name:      "Yesterday but not idle - should expire (daily triggers)",
			updatedAt: time.Date(2024, 1, 14, 20, 0, 0, 0, time.UTC),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &models.Session{
				UpdatedAt: tt.updatedAt,
			}
			got := expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM)
			if got != tt.expected {
				t.Errorf("CheckExpiry() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSessionExpiry_ResetByType(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
		ResetByType: map[string]ResetConfig{
			ConvTypeDM: {
				Mode:        ResetModeIdle,
				IdleMinutes: 60, // DMs get longer idle
			},
			ConvTypeGroup: {
				Mode: ResetModeNever, // Groups never reset
			},
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	// Session 45 minutes idle
	session := &models.Session{
		UpdatedAt: fixedNow.Add(-45 * time.Minute),
	}

	// DM with 60 minute idle - should NOT expire
	if expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM) {
		t.Error("DM should not expire with 45 min idle (60 min threshold)")
	}

	// Group with never mode - should NOT expire
	if expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeGroup) {
		t.Error("Group should never expire")
	}

	// Thread uses default (30 min) - SHOULD expire
	if !expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeThread) {
		t.Error("Thread should expire with 45 min idle (30 min threshold)")
	}
}

func TestSessionExpiry_ResetByChannel(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
		ResetByChannel: map[string]ResetConfig{
			"slack": {
				Mode:        ResetModeIdle,
				IdleMinutes: 120, // Slack gets longer idle
			},
			"telegram": {
				Mode: ResetModeNever,
			},
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	// Session 60 minutes idle
	session := &models.Session{
		UpdatedAt: fixedNow.Add(-60 * time.Minute),
	}

	// Slack with 120 minute idle - should NOT expire
	if expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM) {
		t.Error("Slack should not expire with 60 min idle (120 min threshold)")
	}

	// Telegram with never mode - should NOT expire
	if expiry.CheckExpiry(session, models.ChannelTelegram, ConvTypeDM) {
		t.Error("Telegram should never expire")
	}

	// Discord uses default (30 min) - SHOULD expire
	if !expiry.CheckExpiry(session, models.ChannelDiscord, ConvTypeDM) {
		t.Error("Discord should expire with 60 min idle (30 min threshold)")
	}
}

func TestSessionExpiry_ChannelTakesPrecedenceOverType(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
		ResetByType: map[string]ResetConfig{
			ConvTypeDM: {
				Mode:        ResetModeIdle,
				IdleMinutes: 60,
			},
		},
		ResetByChannel: map[string]ResetConfig{
			"slack": {
				Mode:        ResetModeIdle,
				IdleMinutes: 120,
			},
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	// Session 90 minutes idle
	session := &models.Session{
		UpdatedAt: fixedNow.Add(-90 * time.Minute),
	}

	// Slack DM should use channel config (120 min), not type config (60 min)
	if expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM) {
		t.Error("Slack DM should use channel config (120 min), not expire with 90 min idle")
	}
}

func TestSessionExpiry_NilSession(t *testing.T) {
	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	})

	if expiry.CheckExpiry(nil, models.ChannelSlack, ConvTypeDM) {
		t.Error("CheckExpiry() with nil session should return false")
	}
}

func TestSessionExpiry_ZeroTimestamp(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	session := &models.Session{
		// Both UpdatedAt and CreatedAt are zero
	}

	if expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM) {
		t.Error("CheckExpiry() with zero timestamps should return false")
	}
}

func TestSessionExpiry_UsesCreatedAtIfNoUpdatedAt(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	session := &models.Session{
		CreatedAt: fixedNow.Add(-60 * time.Minute), // 60 minutes ago
		// UpdatedAt is zero
	}

	if !expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM) {
		t.Error("CheckExpiry() should use CreatedAt when UpdatedAt is zero")
	}
}

func TestSessionExpiry_GetNextResetTime(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:   ResetModeDaily,
			AtHour: 9,
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })
	expiry.location = time.UTC

	nextReset := expiry.GetNextResetTime(models.ChannelSlack, ConvTypeDM)
	expected := time.Date(2024, 1, 16, 9, 0, 0, 0, time.UTC)

	if !nextReset.Equal(expected) {
		t.Errorf("GetNextResetTime() = %v, want %v", nextReset, expected)
	}
}

func TestSessionExpiry_GetNextResetTime_BeforeResetHour(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 7, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:   ResetModeDaily,
			AtHour: 9,
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })
	expiry.location = time.UTC

	nextReset := expiry.GetNextResetTime(models.ChannelSlack, ConvTypeDM)
	expected := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC) // Same day

	if !nextReset.Equal(expected) {
		t.Errorf("GetNextResetTime() = %v, want %v", nextReset, expected)
	}
}

func TestSessionExpiry_GetNextResetTime_NeverMode(t *testing.T) {
	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode: ResetModeNever,
		},
	})

	nextReset := expiry.GetNextResetTime(models.ChannelSlack, ConvTypeDM)
	if !nextReset.IsZero() {
		t.Errorf("GetNextResetTime() with never mode should return zero time, got %v", nextReset)
	}
}

func TestSessionExpiry_GetNextResetTime_IdleMode(t *testing.T) {
	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	})

	nextReset := expiry.GetNextResetTime(models.ChannelSlack, ConvTypeDM)
	if !nextReset.IsZero() {
		t.Errorf("GetNextResetTime() with idle-only mode should return zero time, got %v", nextReset)
	}
}

func TestShouldResetSession(t *testing.T) {
	cfg := ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
	}

	session := &models.Session{
		Channel:   models.ChannelSlack,
		UpdatedAt: time.Now().Add(-60 * time.Minute),
	}

	if !ShouldResetSession(session, cfg) {
		t.Error("ShouldResetSession() should return true for 60 min idle session with 30 min threshold")
	}
}

func TestShouldResetSessionWithType(t *testing.T) {
	cfg := ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 30,
		},
		ResetByType: map[string]ResetConfig{
			ConvTypeGroup: {
				Mode: ResetModeNever,
			},
		},
	}

	session := &models.Session{
		Channel:   models.ChannelSlack,
		UpdatedAt: time.Now().Add(-60 * time.Minute),
	}

	// Group should never reset
	if ShouldResetSessionWithType(session, cfg, ConvTypeGroup) {
		t.Error("ShouldResetSessionWithType() for group should return false")
	}

	// DM should reset (uses default)
	if !ShouldResetSessionWithType(session, cfg, ConvTypeDM) {
		t.Error("ShouldResetSessionWithType() for DM should return true")
	}
}

func TestSessionExpiry_IdleMinutesZero(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{
		Reset: ResetConfig{
			Mode:        ResetModeIdle,
			IdleMinutes: 0, // Zero should mean no idle reset
		},
	})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	session := &models.Session{
		UpdatedAt: fixedNow.Add(-1000 * time.Hour), // Very old
	}

	if expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM) {
		t.Error("CheckExpiry() with zero IdleMinutes should return false")
	}
}

func TestSessionExpiry_InvalidAtHour(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		atHour int
	}{
		{"Negative hour", -1},
		{"Hour 24", 24},
		{"Hour 100", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expiry := NewSessionExpiry(ScopeConfig{
				Reset: ResetConfig{
					Mode:   ResetModeDaily,
					AtHour: tt.atHour,
				},
			})
			expiry.SetNowFunc(func() time.Time { return fixedNow })
			expiry.location = time.UTC

			// Should default to 0 and not panic
			session := &models.Session{
				UpdatedAt: time.Date(2024, 1, 14, 23, 0, 0, 0, time.UTC),
			}
			// Just verify it doesn't panic
			_ = expiry.CheckExpiry(session, models.ChannelSlack, ConvTypeDM)
		})
	}
}

func TestNewSessionExpiryWithLocation(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")

	expiry := NewSessionExpiryWithLocation(ScopeConfig{
		Reset: ResetConfig{
			Mode:   ResetModeDaily,
			AtHour: 9,
		},
	}, loc)

	if expiry.location != loc {
		t.Error("NewSessionExpiryWithLocation() should set the location")
	}
}

func TestNewSessionExpiryWithLocation_NilLocation(t *testing.T) {
	expiry := NewSessionExpiryWithLocation(ScopeConfig{}, nil)

	if expiry.location != time.Local {
		t.Error("NewSessionExpiryWithLocation() with nil should default to Local")
	}
}

func TestSessionExpiry_CheckExpiryWithConfig(t *testing.T) {
	fixedNow := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)

	expiry := NewSessionExpiry(ScopeConfig{})
	expiry.SetNowFunc(func() time.Time { return fixedNow })

	session := &models.Session{
		UpdatedAt: fixedNow.Add(-60 * time.Minute),
	}

	// Custom config with 30 minute idle
	customCfg := ResetConfig{
		Mode:        ResetModeIdle,
		IdleMinutes: 30,
	}

	if !expiry.CheckExpiryWithConfig(session, customCfg) {
		t.Error("CheckExpiryWithConfig() should use provided config")
	}

	// Custom config with 120 minute idle
	customCfg2 := ResetConfig{
		Mode:        ResetModeIdle,
		IdleMinutes: 120,
	}

	if expiry.CheckExpiryWithConfig(session, customCfg2) {
		t.Error("CheckExpiryWithConfig() with 120 min idle should not expire 60 min session")
	}
}
