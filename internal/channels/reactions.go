package channels

import (
	"context"
	"sync"
)

// AckReactionScope defines when to send ack reactions
type AckReactionScope string

const (
	ScopeAll           AckReactionScope = "all"            // Always send
	ScopeDirect        AckReactionScope = "direct"         // Only in DMs
	ScopeGroupAll      AckReactionScope = "group-all"      // All group messages
	ScopeGroupMentions AckReactionScope = "group-mentions" // Only when mentioned in groups
	ScopeOff           AckReactionScope = "off"            // Never send
	ScopeNone          AckReactionScope = "none"           // Never send (alias)
)

// WhatsAppAckReactionMode for WhatsApp-specific handling
type WhatsAppAckReactionMode string

const (
	WhatsAppAckAlways   WhatsAppAckReactionMode = "always"
	WhatsAppAckMentions WhatsAppAckReactionMode = "mentions"
	WhatsAppAckNever    WhatsAppAckReactionMode = "never"
)

// AckReactionGateParams for determining if reaction should be sent
type AckReactionGateParams struct {
	Scope                 AckReactionScope
	IsDirect              bool
	IsGroup               bool
	IsMentionableGroup    bool
	RequireMention        bool
	CanDetectMention      bool
	EffectiveWasMentioned bool
	ShouldBypassMention   bool
}

// ShouldAckReaction determines if an ack reaction should be sent
func ShouldAckReaction(params AckReactionGateParams) bool {
	scope := params.Scope
	if scope == "" {
		scope = ScopeGroupMentions
	}

	if scope == ScopeOff || scope == ScopeNone {
		return false
	}
	if scope == ScopeAll {
		return true
	}
	if scope == ScopeDirect {
		return params.IsDirect
	}
	if scope == ScopeGroupAll {
		return params.IsGroup
	}
	if scope == ScopeGroupMentions {
		if !params.IsMentionableGroup {
			return false
		}
		if !params.RequireMention {
			return false
		}
		if !params.CanDetectMention {
			return false
		}
		return params.EffectiveWasMentioned || params.ShouldBypassMention
	}
	return false
}

// WhatsAppAckParams for WhatsApp-specific ack decisions
type WhatsAppAckParams struct {
	Emoji          string
	IsDirect       bool
	IsGroup        bool
	DirectEnabled  bool
	GroupMode      WhatsAppAckReactionMode
	WasMentioned   bool
	GroupActivated bool
}

// ShouldAckReactionForWhatsApp determines WhatsApp-specific ack behavior
func ShouldAckReactionForWhatsApp(params WhatsAppAckParams) bool {
	if params.Emoji == "" {
		return false
	}
	if params.IsDirect {
		return params.DirectEnabled
	}
	if !params.IsGroup {
		return false
	}
	if params.GroupMode == WhatsAppAckNever {
		return false
	}
	if params.GroupMode == WhatsAppAckAlways {
		return true
	}
	return ShouldAckReaction(AckReactionGateParams{
		Scope:                 ScopeGroupMentions,
		IsDirect:              false,
		IsGroup:               true,
		IsMentionableGroup:    true,
		RequireMention:        true,
		CanDetectMention:      true,
		EffectiveWasMentioned: params.WasMentioned,
		ShouldBypassMention:   params.GroupActivated,
	})
}

// AckReactionTracker tracks pending ack reactions
type AckReactionTracker struct {
	mu      sync.Mutex
	pending map[string]*pendingReaction
}

type pendingReaction struct {
	emoji     string
	acked     bool
	removed   bool
	removeErr error
}

// NewAckReactionTracker creates a new tracker
func NewAckReactionTracker() *AckReactionTracker {
	return &AckReactionTracker{
		pending: make(map[string]*pendingReaction),
	}
}

// Track starts tracking a reaction
func (t *AckReactionTracker) Track(messageID, emoji string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[messageID] = &pendingReaction{
		emoji:   emoji,
		acked:   false,
		removed: false,
	}
}

// MarkAcked marks a reaction as acknowledged
func (t *AckReactionTracker) MarkAcked(messageID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if pr, ok := t.pending[messageID]; ok {
		pr.acked = true
	}
}

// RemoveAfterReply schedules removal after reply is sent
func (t *AckReactionTracker) RemoveAfterReply(messageID string, removeAfter bool, removeFn func(ctx context.Context) error) {
	t.mu.Lock()
	pr, ok := t.pending[messageID]
	if !ok {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	if !removeAfter {
		return
	}

	// Execute removal function
	if removeFn != nil {
		err := removeFn(context.Background())
		t.mu.Lock()
		pr.removed = true
		pr.removeErr = err
		t.mu.Unlock()
	}
}

// Get returns the pending reaction for a message ID if it exists
func (t *AckReactionTracker) Get(messageID string) (emoji string, exists bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if pr, ok := t.pending[messageID]; ok {
		return pr.emoji, true
	}
	return "", false
}

// IsAcked returns whether a reaction has been acknowledged
func (t *AckReactionTracker) IsAcked(messageID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if pr, ok := t.pending[messageID]; ok {
		return pr.acked
	}
	return false
}

// IsRemoved returns whether a reaction has been removed
func (t *AckReactionTracker) IsRemoved(messageID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if pr, ok := t.pending[messageID]; ok {
		return pr.removed
	}
	return false
}

// RemoveError returns any error from removing a reaction
func (t *AckReactionTracker) RemoveError(messageID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if pr, ok := t.pending[messageID]; ok {
		return pr.removeErr
	}
	return nil
}

// Clear removes a message from tracking
func (t *AckReactionTracker) Clear(messageID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, messageID)
}

// ClearAll removes all tracked messages
func (t *AckReactionTracker) ClearAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = make(map[string]*pendingReaction)
}

// Count returns the number of tracked reactions
func (t *AckReactionTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// ReactionConfig for configuring ack reactions
type ReactionConfig struct {
	Enabled          bool
	Emoji            string // Default emoji to use (e.g., "eyes", "hourglass")
	RemoveAfterReply bool
	Scope            AckReactionScope
	DirectEnabled    bool
	GroupMode        WhatsAppAckReactionMode
}

// DefaultReactionConfig returns sensible defaults
func DefaultReactionConfig() *ReactionConfig {
	return &ReactionConfig{
		Enabled:          true,
		Emoji:            "eyes",
		RemoveAfterReply: true,
		Scope:            ScopeGroupMentions,
		DirectEnabled:    true,
		GroupMode:        WhatsAppAckMentions,
	}
}

// Validate checks if the config is valid
func (c *ReactionConfig) Validate() error {
	if c.Emoji == "" && c.Enabled {
		return ErrInvalidReactionEmoji
	}
	return nil
}

// ShouldSendAck determines if an ack should be sent based on this config
func (c *ReactionConfig) ShouldSendAck(isDirect, isGroup, isMentionableGroup, wasMentioned bool) bool {
	if !c.Enabled {
		return false
	}
	return ShouldAckReaction(AckReactionGateParams{
		Scope:                 c.Scope,
		IsDirect:              isDirect,
		IsGroup:               isGroup,
		IsMentionableGroup:    isMentionableGroup,
		RequireMention:        true,
		CanDetectMention:      true,
		EffectiveWasMentioned: wasMentioned,
		ShouldBypassMention:   false,
	})
}
