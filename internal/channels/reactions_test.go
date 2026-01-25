package channels

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestShouldAckReaction(t *testing.T) {
	tests := []struct {
		name   string
		params AckReactionGateParams
		want   bool
	}{
		// Scope: off/none - never send
		{
			name: "scope off always returns false",
			params: AckReactionGateParams{
				Scope:    ScopeOff,
				IsDirect: true,
			},
			want: false,
		},
		{
			name: "scope none always returns false",
			params: AckReactionGateParams{
				Scope:   ScopeNone,
				IsGroup: true,
			},
			want: false,
		},

		// Scope: all - always send
		{
			name: "scope all returns true for direct",
			params: AckReactionGateParams{
				Scope:    ScopeAll,
				IsDirect: true,
			},
			want: true,
		},
		{
			name: "scope all returns true for group",
			params: AckReactionGateParams{
				Scope:   ScopeAll,
				IsGroup: true,
			},
			want: true,
		},

		// Scope: direct - only DMs
		{
			name: "scope direct returns true for DM",
			params: AckReactionGateParams{
				Scope:    ScopeDirect,
				IsDirect: true,
			},
			want: true,
		},
		{
			name: "scope direct returns false for group",
			params: AckReactionGateParams{
				Scope:   ScopeDirect,
				IsGroup: true,
			},
			want: false,
		},

		// Scope: group-all - all group messages
		{
			name: "scope group-all returns true for group",
			params: AckReactionGateParams{
				Scope:   ScopeGroupAll,
				IsGroup: true,
			},
			want: true,
		},
		{
			name: "scope group-all returns false for direct",
			params: AckReactionGateParams{
				Scope:    ScopeGroupAll,
				IsDirect: true,
			},
			want: false,
		},

		// Scope: group-mentions - only when mentioned
		{
			name: "scope group-mentions returns true when mentioned",
			params: AckReactionGateParams{
				Scope:                 ScopeGroupMentions,
				IsGroup:               true,
				IsMentionableGroup:    true,
				RequireMention:        true,
				CanDetectMention:      true,
				EffectiveWasMentioned: true,
			},
			want: true,
		},
		{
			name: "scope group-mentions returns true with bypass",
			params: AckReactionGateParams{
				Scope:               ScopeGroupMentions,
				IsGroup:             true,
				IsMentionableGroup:  true,
				RequireMention:      true,
				CanDetectMention:    true,
				ShouldBypassMention: true,
			},
			want: true,
		},
		{
			name: "scope group-mentions returns false when not mentioned",
			params: AckReactionGateParams{
				Scope:                 ScopeGroupMentions,
				IsGroup:               true,
				IsMentionableGroup:    true,
				RequireMention:        true,
				CanDetectMention:      true,
				EffectiveWasMentioned: false,
			},
			want: false,
		},
		{
			name: "scope group-mentions returns false when not mentionable",
			params: AckReactionGateParams{
				Scope:                 ScopeGroupMentions,
				IsGroup:               true,
				IsMentionableGroup:    false,
				RequireMention:        true,
				CanDetectMention:      true,
				EffectiveWasMentioned: true,
			},
			want: false,
		},
		{
			name: "scope group-mentions returns false when mention not required",
			params: AckReactionGateParams{
				Scope:                 ScopeGroupMentions,
				IsGroup:               true,
				IsMentionableGroup:    true,
				RequireMention:        false,
				CanDetectMention:      true,
				EffectiveWasMentioned: true,
			},
			want: false,
		},
		{
			name: "scope group-mentions returns false when cannot detect mention",
			params: AckReactionGateParams{
				Scope:                 ScopeGroupMentions,
				IsGroup:               true,
				IsMentionableGroup:    true,
				RequireMention:        true,
				CanDetectMention:      false,
				EffectiveWasMentioned: true,
			},
			want: false,
		},

		// Default scope (empty) should default to group-mentions
		{
			name: "empty scope defaults to group-mentions with mention",
			params: AckReactionGateParams{
				Scope:                 "",
				IsGroup:               true,
				IsMentionableGroup:    true,
				RequireMention:        true,
				CanDetectMention:      true,
				EffectiveWasMentioned: true,
			},
			want: true,
		},
		{
			name: "empty scope defaults to group-mentions without mention",
			params: AckReactionGateParams{
				Scope:                 "",
				IsGroup:               true,
				IsMentionableGroup:    true,
				RequireMention:        true,
				CanDetectMention:      true,
				EffectiveWasMentioned: false,
			},
			want: false,
		},

		// Unknown scope
		{
			name: "unknown scope returns false",
			params: AckReactionGateParams{
				Scope:    AckReactionScope("unknown"),
				IsDirect: true,
				IsGroup:  true,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldAckReaction(tt.params)
			if got != tt.want {
				t.Errorf("ShouldAckReaction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAckReactionForWhatsApp(t *testing.T) {
	tests := []struct {
		name   string
		params WhatsAppAckParams
		want   bool
	}{
		// Empty emoji
		{
			name: "empty emoji returns false",
			params: WhatsAppAckParams{
				Emoji:         "",
				IsDirect:      true,
				DirectEnabled: true,
			},
			want: false,
		},

		// Direct messages
		{
			name: "direct message with direct enabled",
			params: WhatsAppAckParams{
				Emoji:         "eyes",
				IsDirect:      true,
				DirectEnabled: true,
			},
			want: true,
		},
		{
			name: "direct message with direct disabled",
			params: WhatsAppAckParams{
				Emoji:         "eyes",
				IsDirect:      true,
				DirectEnabled: false,
			},
			want: false,
		},

		// Not direct and not group
		{
			name: "neither direct nor group returns false",
			params: WhatsAppAckParams{
				Emoji:    "eyes",
				IsDirect: false,
				IsGroup:  false,
			},
			want: false,
		},

		// Group with never mode
		{
			name: "group with never mode returns false",
			params: WhatsAppAckParams{
				Emoji:     "eyes",
				IsGroup:   true,
				GroupMode: WhatsAppAckNever,
			},
			want: false,
		},

		// Group with always mode
		{
			name: "group with always mode returns true",
			params: WhatsAppAckParams{
				Emoji:     "eyes",
				IsGroup:   true,
				GroupMode: WhatsAppAckAlways,
			},
			want: true,
		},

		// Group with mentions mode
		{
			name: "group with mentions mode when mentioned",
			params: WhatsAppAckParams{
				Emoji:        "eyes",
				IsGroup:      true,
				GroupMode:    WhatsAppAckMentions,
				WasMentioned: true,
			},
			want: true,
		},
		{
			name: "group with mentions mode when not mentioned",
			params: WhatsAppAckParams{
				Emoji:        "eyes",
				IsGroup:      true,
				GroupMode:    WhatsAppAckMentions,
				WasMentioned: false,
			},
			want: false,
		},
		{
			name: "group with mentions mode with group activated bypass",
			params: WhatsAppAckParams{
				Emoji:          "eyes",
				IsGroup:        true,
				GroupMode:      WhatsAppAckMentions,
				WasMentioned:   false,
				GroupActivated: true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldAckReactionForWhatsApp(tt.params)
			if got != tt.want {
				t.Errorf("ShouldAckReactionForWhatsApp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAckReactionTracker(t *testing.T) {
	t.Run("new tracker is empty", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		if tracker.Count() != 0 {
			t.Errorf("Count() = %d, want 0", tracker.Count())
		}
	})

	t.Run("track adds reaction", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		tracker.Track("msg-1", "eyes")

		emoji, exists := tracker.Get("msg-1")
		if !exists {
			t.Error("Get() returned exists=false, want true")
		}
		if emoji != "eyes" {
			t.Errorf("Get() emoji = %q, want %q", emoji, "eyes")
		}
		if tracker.Count() != 1 {
			t.Errorf("Count() = %d, want 1", tracker.Count())
		}
	})

	t.Run("get non-existent returns false", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		_, exists := tracker.Get("non-existent")
		if exists {
			t.Error("Get() returned exists=true for non-existent message")
		}
	})

	t.Run("mark acked", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		tracker.Track("msg-1", "eyes")

		if tracker.IsAcked("msg-1") {
			t.Error("IsAcked() = true before MarkAcked")
		}

		tracker.MarkAcked("msg-1")

		if !tracker.IsAcked("msg-1") {
			t.Error("IsAcked() = false after MarkAcked")
		}
	})

	t.Run("mark acked non-existent is no-op", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		// Should not panic
		tracker.MarkAcked("non-existent")
	})

	t.Run("clear removes reaction", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		tracker.Track("msg-1", "eyes")
		tracker.Track("msg-2", "hourglass")

		tracker.Clear("msg-1")

		_, exists := tracker.Get("msg-1")
		if exists {
			t.Error("Get() returned exists=true after Clear")
		}
		if tracker.Count() != 1 {
			t.Errorf("Count() = %d, want 1", tracker.Count())
		}
	})

	t.Run("clear all removes all reactions", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		tracker.Track("msg-1", "eyes")
		tracker.Track("msg-2", "hourglass")
		tracker.Track("msg-3", "thinking")

		tracker.ClearAll()

		if tracker.Count() != 0 {
			t.Errorf("Count() = %d after ClearAll, want 0", tracker.Count())
		}
	})

	t.Run("remove after reply", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		tracker.Track("msg-1", "eyes")

		called := false
		removeFn := func(ctx context.Context) error {
			called = true
			return nil
		}

		tracker.RemoveAfterReply("msg-1", true, removeFn)

		if !called {
			t.Error("removeFn was not called")
		}
		if !tracker.IsRemoved("msg-1") {
			t.Error("IsRemoved() = false after RemoveAfterReply")
		}
	})

	t.Run("remove after reply with removeAfter false", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		tracker.Track("msg-1", "eyes")

		called := false
		removeFn := func(ctx context.Context) error {
			called = true
			return nil
		}

		tracker.RemoveAfterReply("msg-1", false, removeFn)

		if called {
			t.Error("removeFn was called when removeAfter=false")
		}
		if tracker.IsRemoved("msg-1") {
			t.Error("IsRemoved() = true when removeAfter=false")
		}
	})

	t.Run("remove after reply non-existent is no-op", func(t *testing.T) {
		tracker := NewAckReactionTracker()

		called := false
		removeFn := func(ctx context.Context) error {
			called = true
			return nil
		}

		// Should not panic
		tracker.RemoveAfterReply("non-existent", true, removeFn)

		if called {
			t.Error("removeFn was called for non-existent message")
		}
	})

	t.Run("remove after reply captures error", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		tracker.Track("msg-1", "eyes")

		expectedErr := errors.New("remove failed")
		removeFn := func(ctx context.Context) error {
			return expectedErr
		}

		tracker.RemoveAfterReply("msg-1", true, removeFn)

		if tracker.RemoveError("msg-1") != expectedErr {
			t.Errorf("RemoveError() = %v, want %v", tracker.RemoveError("msg-1"), expectedErr)
		}
	})

	t.Run("remove error for non-existent returns nil", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		if tracker.RemoveError("non-existent") != nil {
			t.Error("RemoveError() for non-existent should return nil")
		}
	})

	t.Run("is acked for non-existent returns false", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		if tracker.IsAcked("non-existent") {
			t.Error("IsAcked() for non-existent should return false")
		}
	})

	t.Run("is removed for non-existent returns false", func(t *testing.T) {
		tracker := NewAckReactionTracker()
		if tracker.IsRemoved("non-existent") {
			t.Error("IsRemoved() for non-existent should return false")
		}
	})
}

func TestAckReactionTracker_Concurrent(t *testing.T) {
	tracker := NewAckReactionTracker()
	var wg sync.WaitGroup

	// Concurrent tracks
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			msgID := string(rune('a' + id%26))
			tracker.Track(msgID, "eyes")
			tracker.MarkAcked(msgID)
			tracker.Get(msgID)
			tracker.IsAcked(msgID)
		}(i)
	}

	wg.Wait()

	// Should not panic and should have some entries
	if tracker.Count() == 0 {
		t.Error("Expected some tracked reactions after concurrent access")
	}
}

func TestReactionConfig(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := DefaultReactionConfig()

		if !cfg.Enabled {
			t.Error("Enabled should be true by default")
		}
		if cfg.Emoji != "eyes" {
			t.Errorf("Emoji = %q, want %q", cfg.Emoji, "eyes")
		}
		if !cfg.RemoveAfterReply {
			t.Error("RemoveAfterReply should be true by default")
		}
		if cfg.Scope != ScopeGroupMentions {
			t.Errorf("Scope = %q, want %q", cfg.Scope, ScopeGroupMentions)
		}
		if !cfg.DirectEnabled {
			t.Error("DirectEnabled should be true by default")
		}
		if cfg.GroupMode != WhatsAppAckMentions {
			t.Errorf("GroupMode = %q, want %q", cfg.GroupMode, WhatsAppAckMentions)
		}
	})

	t.Run("validate with emoji", func(t *testing.T) {
		cfg := &ReactionConfig{
			Enabled: true,
			Emoji:   "eyes",
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() returned error for valid config: %v", err)
		}
	})

	t.Run("validate without emoji when enabled", func(t *testing.T) {
		cfg := &ReactionConfig{
			Enabled: true,
			Emoji:   "",
		}
		if err := cfg.Validate(); err != ErrInvalidReactionEmoji {
			t.Errorf("Validate() = %v, want %v", err, ErrInvalidReactionEmoji)
		}
	})

	t.Run("validate without emoji when disabled", func(t *testing.T) {
		cfg := &ReactionConfig{
			Enabled: false,
			Emoji:   "",
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() returned error when disabled: %v", err)
		}
	})
}

func TestReactionConfig_ShouldSendAck(t *testing.T) {
	tests := []struct {
		name               string
		config             *ReactionConfig
		isDirect           bool
		isGroup            bool
		isMentionableGroup bool
		wasMentioned       bool
		want               bool
	}{
		{
			name: "disabled config returns false",
			config: &ReactionConfig{
				Enabled: false,
				Scope:   ScopeAll,
			},
			isDirect: true,
			want:     false,
		},
		{
			name: "scope all returns true for direct",
			config: &ReactionConfig{
				Enabled: true,
				Emoji:   "eyes",
				Scope:   ScopeAll,
			},
			isDirect: true,
			want:     true,
		},
		{
			name: "scope direct returns true for DM",
			config: &ReactionConfig{
				Enabled: true,
				Emoji:   "eyes",
				Scope:   ScopeDirect,
			},
			isDirect: true,
			want:     true,
		},
		{
			name: "scope direct returns false for group",
			config: &ReactionConfig{
				Enabled: true,
				Emoji:   "eyes",
				Scope:   ScopeDirect,
			},
			isGroup: true,
			want:    false,
		},
		{
			name: "scope group-mentions returns true when mentioned",
			config: &ReactionConfig{
				Enabled: true,
				Emoji:   "eyes",
				Scope:   ScopeGroupMentions,
			},
			isGroup:            true,
			isMentionableGroup: true,
			wasMentioned:       true,
			want:               true,
		},
		{
			name: "scope group-mentions returns false when not mentioned",
			config: &ReactionConfig{
				Enabled: true,
				Emoji:   "eyes",
				Scope:   ScopeGroupMentions,
			},
			isGroup:            true,
			isMentionableGroup: true,
			wasMentioned:       false,
			want:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ShouldSendAck(tt.isDirect, tt.isGroup, tt.isMentionableGroup, tt.wasMentioned)
			if got != tt.want {
				t.Errorf("ShouldSendAck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAckReactionScopeConstants(t *testing.T) {
	// Verify constants have expected values
	if ScopeAll != "all" {
		t.Errorf("ScopeAll = %q, want %q", ScopeAll, "all")
	}
	if ScopeDirect != "direct" {
		t.Errorf("ScopeDirect = %q, want %q", ScopeDirect, "direct")
	}
	if ScopeGroupAll != "group-all" {
		t.Errorf("ScopeGroupAll = %q, want %q", ScopeGroupAll, "group-all")
	}
	if ScopeGroupMentions != "group-mentions" {
		t.Errorf("ScopeGroupMentions = %q, want %q", ScopeGroupMentions, "group-mentions")
	}
	if ScopeOff != "off" {
		t.Errorf("ScopeOff = %q, want %q", ScopeOff, "off")
	}
	if ScopeNone != "none" {
		t.Errorf("ScopeNone = %q, want %q", ScopeNone, "none")
	}
}

func TestWhatsAppAckReactionModeConstants(t *testing.T) {
	if WhatsAppAckAlways != "always" {
		t.Errorf("WhatsAppAckAlways = %q, want %q", WhatsAppAckAlways, "always")
	}
	if WhatsAppAckMentions != "mentions" {
		t.Errorf("WhatsAppAckMentions = %q, want %q", WhatsAppAckMentions, "mentions")
	}
	if WhatsAppAckNever != "never" {
		t.Errorf("WhatsAppAckNever = %q, want %q", WhatsAppAckNever, "never")
	}
}
