package sessions

import (
	"context"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ScopedStore wraps a Store and provides advanced session scoping and expiry.
type ScopedStore struct {
	store      Store
	keyBuilder *SessionKeyBuilder
	expiry     *SessionExpiry
	cfg        ScopeConfig
	mu         sync.Mutex // Protects atomic GetOrCreateScoped operations
}

// NewScopedStore creates a new ScopedStore wrapping the given store.
func NewScopedStore(store Store, cfg ScopeConfig) *ScopedStore {
	return &ScopedStore{
		store:      store,
		keyBuilder: NewSessionKeyBuilder(cfg),
		expiry:     NewSessionExpiry(cfg),
		cfg:        cfg,
	}
}

// NewScopedStoreWithLocation creates a ScopedStore with a specific timezone for expiry.
func NewScopedStoreWithLocation(store Store, cfg ScopeConfig, loc *time.Location) *ScopedStore {
	return &ScopedStore{
		store:      store,
		keyBuilder: NewSessionKeyBuilder(cfg),
		expiry:     NewSessionExpiryWithLocation(cfg, loc),
		cfg:        cfg,
	}
}

// GetOrCreateScoped gets or creates a session using advanced scoping rules.
// Parameters:
//   - agentID: the agent identifier
//   - channel: the channel type
//   - peerID: the peer identifier (user ID, chat ID, etc.)
//   - isGroup: whether this is a group conversation
//   - threadID: optional thread identifier
//   - convType: conversation type for expiry rules (dm, group, thread)
//
// This operation is atomic to prevent race conditions between check and create.
func (s *ScopedStore) GetOrCreateScoped(
	ctx context.Context,
	agentID string,
	channel models.ChannelType,
	peerID string,
	isGroup bool,
	threadID string,
	convType string,
) (*models.Session, error) {
	// Lock to ensure atomic check-delete-create sequence
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build the session key using scoping rules
	key := s.keyBuilder.BuildKey(agentID, channel, peerID, isGroup, threadID)

	// Try to get existing session
	session, err := s.store.GetByKey(ctx, key)
	if err == nil && session != nil {
		// Check if session should be reset due to expiry
		if s.expiry.CheckExpiry(session, channel, convType) {
			// Delete the expired session and create a new one
			if delErr := s.store.Delete(ctx, session.ID); delErr != nil {
				// Log deletion failure but continue - GetOrCreate will handle duplicates
				// This is safe because the underlying store's GetOrCreate is atomic
				return nil, delErr
			}
			return s.createNewSession(ctx, key, agentID, channel, peerID)
		}
		return session, nil
	}

	// Create new session
	return s.createNewSession(ctx, key, agentID, channel, peerID)
}

// createNewSession creates a new session with the given parameters.
func (s *ScopedStore) createNewSession(
	ctx context.Context,
	key string,
	agentID string,
	channel models.ChannelType,
	channelID string,
) (*models.Session, error) {
	return s.store.GetOrCreate(ctx, key, agentID, channel, channelID)
}

// GetSessionWithExpiryCheck retrieves a session and checks if it should be expired.
// Returns (session, shouldReset, error).
func (s *ScopedStore) GetSessionWithExpiryCheck(
	ctx context.Context,
	id string,
	convType string,
) (*models.Session, bool, error) {
	session, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}

	shouldReset := s.expiry.CheckExpiry(session, session.Channel, convType)
	return session, shouldReset, nil
}

// ResolveIdentity maps a platform-specific peer ID to a canonical identity.
func (s *ScopedStore) ResolveIdentity(channel string, peerID string) string {
	return s.keyBuilder.ResolveIdentity(channel, peerID)
}

// GetCanonicalID returns the canonical ID for a platform-specific peer.
func (s *ScopedStore) GetCanonicalID(channel string, peerID string) string {
	return s.keyBuilder.GetCanonicalID(channel, peerID)
}

// GetLinkedPeers returns all platform-specific peer IDs linked to a canonical identity.
func (s *ScopedStore) GetLinkedPeers(canonicalID string) []string {
	return s.keyBuilder.GetLinkedPeers(canonicalID)
}

// BuildKey generates a session key using the configured scoping rules.
func (s *ScopedStore) BuildKey(agentID string, channel models.ChannelType, peerID string, isGroup bool, threadID string) string {
	return s.keyBuilder.BuildKey(agentID, channel, peerID, isGroup, threadID)
}

// CheckExpiry checks if a session should be reset based on expiry configuration.
func (s *ScopedStore) CheckExpiry(session *models.Session, convType string) bool {
	if session == nil {
		return false
	}
	return s.expiry.CheckExpiry(session, session.Channel, convType)
}

// GetNextResetTime returns the next scheduled reset time for the given channel/type.
func (s *ScopedStore) GetNextResetTime(channel models.ChannelType, convType string) time.Time {
	return s.expiry.GetNextResetTime(channel, convType)
}

// Store returns the underlying store for direct access when needed.
func (s *ScopedStore) Store() Store {
	return s.store
}

// Delegate methods to underlying store

func (s *ScopedStore) Create(ctx context.Context, session *models.Session) error {
	return s.store.Create(ctx, session)
}

func (s *ScopedStore) Get(ctx context.Context, id string) (*models.Session, error) {
	return s.store.Get(ctx, id)
}

func (s *ScopedStore) Update(ctx context.Context, session *models.Session) error {
	return s.store.Update(ctx, session)
}

func (s *ScopedStore) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

func (s *ScopedStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	return s.store.GetByKey(ctx, key)
}

func (s *ScopedStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	return s.store.GetOrCreate(ctx, key, agentID, channel, channelID)
}

func (s *ScopedStore) List(ctx context.Context, agentID string, opts ListOptions) ([]*models.Session, error) {
	return s.store.List(ctx, agentID, opts)
}

func (s *ScopedStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	return s.store.AppendMessage(ctx, sessionID, msg)
}

func (s *ScopedStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	return s.store.GetHistory(ctx, sessionID, limit)
}

// SessionKeyWithScoping builds a session key using scoping configuration.
// This is an extension of the original SessionKey function with scoping support.
func SessionKeyWithScoping(
	agentID string,
	channel models.ChannelType,
	peerID string,
	isGroup bool,
	threadID string,
	cfg ScopeConfig,
) string {
	builder := NewSessionKeyBuilder(cfg)
	return builder.BuildKey(agentID, channel, peerID, isGroup, threadID)
}
