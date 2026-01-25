package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	profilesFilename    = "auth-profiles.json"
	defaultCooldownSecs = 300 // 5 minutes cooldown after failure
	profilesVersion     = 1
)

// CredentialType identifies the type of credential.
type CredentialType string

const (
	CredentialAPIKey CredentialType = "api_key"
	CredentialOAuth  CredentialType = "oauth"
	CredentialToken  CredentialType = "token"
)

var (
	ErrNoProfiles      = errors.New("no profiles configured for provider")
	ErrAllInCooldown   = errors.New("all profiles in cooldown")
	ErrProfileNotFound = errors.New("profile not found")
)

// ProfileCredential holds authentication credentials for a provider profile.
type ProfileCredential struct {
	Type     CredentialType `json:"type"`
	Provider string         `json:"provider"`
	// For api_key
	Key string `json:"key,omitempty"`
	// For oauth
	Access  string `json:"access,omitempty"`
	Refresh string `json:"refresh,omitempty"`
	Expires int64  `json:"expires,omitempty"`
	// For token
	Token string `json:"token,omitempty"`
	// Optional metadata
	Email    string `json:"email,omitempty"`
	LastUsed int64  `json:"last_used,omitempty"`
}

// ProfileUsageStats tracks usage and failure statistics for a profile.
type ProfileUsageStats struct {
	LastUsed    int64 `json:"last_used,omitempty"`
	LastSuccess int64 `json:"last_success,omitempty"`
	LastFailure int64 `json:"last_failure,omitempty"`
	FailCount   int   `json:"fail_count,omitempty"`
}

// ProfileStore manages authentication profiles with rotation support.
type ProfileStore struct {
	mu         sync.RWMutex
	Version    int                          `json:"version"`
	Profiles   map[string]ProfileCredential `json:"profiles"`            // profileID -> credential
	Order      map[string][]string          `json:"order,omitempty"`     // provider -> ordered profile IDs
	LastGood   map[string]string            `json:"last_good,omitempty"` // provider -> last successful profileID
	UsageStats map[string]ProfileUsageStats `json:"usage_stats,omitempty"`

	// CooldownSecs configures how long to skip failed profiles (default 300s)
	CooldownSecs int64 `json:"cooldown_secs,omitempty"`
}

// LoadProfileStore loads auth profiles from disk.
func LoadProfileStore(stateDir string) (*ProfileStore, error) {
	path := filepath.Join(stateDir, profilesFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newProfileStore(), nil
		}
		return nil, err
	}

	store := &ProfileStore{}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}

	// Initialize maps if nil
	store.initMaps()
	return store, nil
}

// SaveProfileStore persists auth profiles to disk.
func SaveProfileStore(store *ProfileStore, stateDir string) error {
	if store == nil {
		return nil
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(stateDir, profilesFilename)
	return os.WriteFile(path, data, 0o600)
}

// GetCredential returns the best credential for a provider, applying rotation.
// Returns the credential, profile ID, and any error.
func (s *ProfileStore) GetCredential(provider string) (*ProfileCredential, string, error) {
	if s == nil {
		return nil, "", ErrNoProfiles
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	profiles := s.resolveProfileOrderLocked(provider)
	if len(profiles) == 0 {
		return nil, "", ErrNoProfiles
	}

	now := time.Now().Unix()
	cooldown := s.getCooldownSecs()

	// Try lastGood first if not in cooldown
	if lastGood, ok := s.LastGood[provider]; ok {
		if cred, ok := s.Profiles[lastGood]; ok && cred.Provider == provider {
			if !s.isInCooldownLocked(lastGood, now, cooldown) {
				credCopy := cred
				return &credCopy, lastGood, nil
			}
		}
	}

	// Try profiles in order, skipping those in cooldown
	for _, profileID := range profiles {
		if s.isInCooldownLocked(profileID, now, cooldown) {
			continue
		}
		cred := s.Profiles[profileID]
		credCopy := cred
		return &credCopy, profileID, nil
	}

	return nil, "", ErrAllInCooldown
}

// MarkSuccess records a successful auth attempt.
func (s *ProfileStore) MarkSuccess(profileID string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()

	// Update usage stats
	stats := s.UsageStats[profileID]
	stats.LastUsed = now
	stats.LastSuccess = now
	stats.FailCount = 0 // Reset fail count on success
	s.UsageStats[profileID] = stats

	// Update credential LastUsed
	if cred, ok := s.Profiles[profileID]; ok {
		cred.LastUsed = now
		s.Profiles[profileID] = cred

		// Set as lastGood for this provider
		s.LastGood[cred.Provider] = profileID
	}
}

// MarkFailure records a failed auth attempt and rotates to next profile.
func (s *ProfileStore) MarkFailure(profileID string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()

	// Update usage stats
	stats := s.UsageStats[profileID]
	stats.LastUsed = now
	stats.LastFailure = now
	stats.FailCount++
	s.UsageStats[profileID] = stats

	// If this was the lastGood, clear it to force rotation
	if cred, ok := s.Profiles[profileID]; ok {
		if s.LastGood[cred.Provider] == profileID {
			delete(s.LastGood, cred.Provider)
		}
	}
}

// AddProfile adds or updates a profile credential.
func (s *ProfileStore) AddProfile(profileID string, cred ProfileCredential) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.initMaps()
	s.Profiles[profileID] = cred

	// Add to order list if not present
	order := s.Order[cred.Provider]
	found := false
	for _, id := range order {
		if id == profileID {
			found = true
			break
		}
	}
	if !found {
		s.Order[cred.Provider] = append(order, profileID)
	}
}

// RemoveProfile removes a profile by ID.
func (s *ProfileStore) RemoveProfile(profileID string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cred, ok := s.Profiles[profileID]
	if !ok {
		return
	}

	delete(s.Profiles, profileID)
	delete(s.UsageStats, profileID)

	// Remove from order
	if order, ok := s.Order[cred.Provider]; ok {
		newOrder := make([]string, 0, len(order))
		for _, id := range order {
			if id != profileID {
				newOrder = append(newOrder, id)
			}
		}
		if len(newOrder) > 0 {
			s.Order[cred.Provider] = newOrder
		} else {
			delete(s.Order, cred.Provider)
		}
	}

	// Clear lastGood if it was this profile
	if s.LastGood[cred.Provider] == profileID {
		delete(s.LastGood, cred.Provider)
	}
}

// ResolveProfileOrder returns profiles for a provider in priority order.
func (s *ProfileStore) ResolveProfileOrder(provider string) []string {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.resolveProfileOrderLocked(provider)
}

// GetProfile returns a profile by ID.
func (s *ProfileStore) GetProfile(profileID string) (*ProfileCredential, error) {
	if s == nil {
		return nil, ErrProfileNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	cred, ok := s.Profiles[profileID]
	if !ok {
		return nil, ErrProfileNotFound
	}

	credCopy := cred
	return &credCopy, nil
}

// GetStats returns usage stats for a profile.
func (s *ProfileStore) GetStats(profileID string) ProfileUsageStats {
	if s == nil {
		return ProfileUsageStats{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.UsageStats[profileID]
}

// ListProviders returns all providers that have profiles.
func (s *ProfileStore) ListProviders() []string {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	providers := make(map[string]struct{})
	for _, cred := range s.Profiles {
		providers[cred.Provider] = struct{}{}
	}

	result := make([]string, 0, len(providers))
	for p := range providers {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// ListProfiles returns all profile IDs for a provider.
func (s *ProfileStore) ListProfiles(provider string) []string {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var profiles []string
	for id, cred := range s.Profiles {
		if cred.Provider == provider {
			profiles = append(profiles, id)
		}
	}
	sort.Strings(profiles)
	return profiles
}

// SetOrder sets the profile order for a provider.
func (s *ProfileStore) SetOrder(provider string, order []string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.initMaps()
	if len(order) == 0 {
		delete(s.Order, provider)
	} else {
		s.Order[provider] = order
	}
}

// resolveProfileOrderLocked returns profiles in priority order (must hold lock).
func (s *ProfileStore) resolveProfileOrderLocked(provider string) []string {
	// Use configured order if available
	if order, ok := s.Order[provider]; ok && len(order) > 0 {
		// Filter to only include existing profiles
		result := make([]string, 0, len(order))
		for _, id := range order {
			if cred, ok := s.Profiles[id]; ok && cred.Provider == provider {
				result = append(result, id)
			}
		}
		return result
	}

	// Fall back to alphabetical order
	var profiles []string
	for id, cred := range s.Profiles {
		if cred.Provider == provider {
			profiles = append(profiles, id)
		}
	}
	sort.Strings(profiles)
	return profiles
}

// isInCooldownLocked checks if a profile is in cooldown (must hold lock).
func (s *ProfileStore) isInCooldownLocked(profileID string, now, cooldownSecs int64) bool {
	stats, ok := s.UsageStats[profileID]
	if !ok {
		return false
	}
	if stats.LastFailure == 0 {
		return false
	}
	// Not in cooldown if we've had a success since the failure
	if stats.LastSuccess >= stats.LastFailure {
		return false
	}
	return now-stats.LastFailure < cooldownSecs
}

// getCooldownSecs returns the cooldown period.
func (s *ProfileStore) getCooldownSecs() int64 {
	if s.CooldownSecs > 0 {
		return s.CooldownSecs
	}
	return defaultCooldownSecs
}

// initMaps ensures all maps are initialized.
func (s *ProfileStore) initMaps() {
	if s.Profiles == nil {
		s.Profiles = make(map[string]ProfileCredential)
	}
	if s.Order == nil {
		s.Order = make(map[string][]string)
	}
	if s.LastGood == nil {
		s.LastGood = make(map[string]string)
	}
	if s.UsageStats == nil {
		s.UsageStats = make(map[string]ProfileUsageStats)
	}
}

// newProfileStore creates a new empty profile store.
func newProfileStore() *ProfileStore {
	return &ProfileStore{
		Version:    profilesVersion,
		Profiles:   make(map[string]ProfileCredential),
		Order:      make(map[string][]string),
		LastGood:   make(map[string]string),
		UsageStats: make(map[string]ProfileUsageStats),
	}
}
