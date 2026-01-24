// Package pairing provides secure device pairing with one-time codes for
// authenticating new users on messaging channels.
package pairing

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// CodeLength is the length of pairing codes.
	CodeLength = 8
	// CodeAlphabet contains unambiguous characters (no 0O1I).
	CodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	// PendingTTL is how long a pending request stays valid.
	PendingTTL = time.Hour
	// MaxPending is the maximum number of pending requests per channel.
	MaxPending = 3
)

var (
	// ErrInvalidChannel indicates an invalid channel name.
	ErrInvalidChannel = errors.New("invalid pairing channel")
	// ErrMaxPending indicates too many pending requests.
	ErrMaxPending = errors.New("maximum pending requests reached")
	// ErrCodeNotFound indicates the pairing code wasn't found.
	ErrCodeNotFound = errors.New("pairing code not found")
)

// Request represents a pending pairing request.
type Request struct {
	ID         string            `json:"id"`
	Code       string            `json:"code"`
	CreatedAt  time.Time         `json:"created_at"`
	LastSeenAt time.Time         `json:"last_seen_at"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// IsExpired returns true if the request has expired.
func (r *Request) IsExpired() bool {
	return time.Since(r.CreatedAt) > PendingTTL
}

// storeData is the persisted format.
type storeData struct {
	Version  int        `json:"version"`
	Requests []*Request `json:"requests"`
}

// allowFromData is the persisted allowlist format.
type allowFromData struct {
	Version   int      `json:"version"`
	AllowFrom []string `json:"allow_from"`
}

// Store manages pairing requests and allowlists for a channel.
type Store struct {
	mu      sync.RWMutex
	dataDir string
}

// NewStore creates a new pairing store.
func NewStore(dataDir string) *Store {
	return &Store{dataDir: dataDir}
}

// safeChannelKey sanitizes a channel name for use in filenames.
func safeChannelKey(channel string) (string, error) {
	raw := strings.TrimSpace(strings.ToLower(channel))
	if raw == "" {
		return "", ErrInvalidChannel
	}
	// Remove path traversal and special characters
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, raw)
	if safe == "" || safe == "_" {
		return "", ErrInvalidChannel
	}
	return safe, nil
}

func (s *Store) pairingPath(channel string) (string, error) {
	key, err := safeChannelKey(channel)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dataDir, fmt.Sprintf("%s-pairing.json", key)), nil
}

func (s *Store) allowFromPath(channel string) (string, error) {
	key, err := safeChannelKey(channel)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dataDir, fmt.Sprintf("%s-allowfrom.json", key)), nil
}

// generateCode creates a random human-friendly code.
func generateCode() (string, error) {
	b := make([]byte, CodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, CodeLength)
	for i := 0; i < CodeLength; i++ {
		code[i] = CodeAlphabet[int(b[i])%len(CodeAlphabet)]
	}
	return string(code), nil
}

// generateUniqueCode creates a code not in the existing set.
func generateUniqueCode(existing map[string]bool) (string, error) {
	for i := 0; i < 500; i++ {
		code, err := generateCode()
		if err != nil {
			return "", err
		}
		if !existing[code] {
			return code, nil
		}
	}
	return "", errors.New("failed to generate unique pairing code")
}

// readStore reads the pairing store for a channel.
func (s *Store) readStore(channel string) (*storeData, error) {
	path, err := s.pairingPath(channel)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &storeData{Version: 1, Requests: nil}, nil
		}
		return nil, err
	}

	var store storeData
	if err := json.Unmarshal(data, &store); err != nil {
		return &storeData{Version: 1, Requests: nil}, nil
	}
	return &store, nil
}

// writeStore writes the pairing store for a channel.
func (s *Store) writeStore(channel string, store *storeData) error {
	path, err := s.pairingPath(channel)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	// Write atomically
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// pruneExpired removes expired requests from the list.
func pruneExpired(requests []*Request) []*Request {
	result := make([]*Request, 0, len(requests))
	for _, r := range requests {
		if !r.IsExpired() {
			result = append(result, r)
		}
	}
	return result
}

// pruneExcess removes oldest requests beyond the max limit.
func pruneExcess(requests []*Request, max int) []*Request {
	if max <= 0 || len(requests) <= max {
		return requests
	}
	// Sort by LastSeenAt and keep most recent
	// Simple approach: just keep the last N
	return requests[len(requests)-max:]
}

// ListRequests returns all pending pairing requests for a channel.
func (s *Store) ListRequests(channel string) ([]*Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.readStore(channel)
	if err != nil {
		return nil, err
	}

	pruned := pruneExpired(store.Requests)
	pruned = pruneExcess(pruned, MaxPending)

	// Write back if we pruned anything
	if len(pruned) != len(store.Requests) {
		store.Requests = pruned
		if err := s.writeStore(channel, store); err != nil {
			return nil, err
		}
	}

	return pruned, nil
}

// UpsertRequest creates or updates a pairing request.
// Returns the pairing code and whether it was newly created.
func (s *Store) UpsertRequest(channel, id string, meta map[string]string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.readStore(channel)
	if err != nil {
		return "", false, err
	}

	now := time.Now()
	pruned := pruneExpired(store.Requests)

	// Check if request already exists
	var existing *Request
	existingIdx := -1
	existingCodes := make(map[string]bool)
	for i, r := range pruned {
		existingCodes[r.Code] = true
		if r.ID == id {
			existing = r
			existingIdx = i
		}
	}

	if existing != nil {
		// Update existing request
		existing.LastSeenAt = now
		if meta != nil {
			existing.Meta = meta
		}
		store.Requests = pruned
		if err := s.writeStore(channel, store); err != nil {
			return "", false, err
		}
		return existing.Code, false, nil
	}

	// Check pending limit
	pruned = pruneExcess(pruned, MaxPending)
	if MaxPending > 0 && len(pruned) >= MaxPending {
		store.Requests = pruned
		_ = s.writeStore(channel, store)
		return "", false, ErrMaxPending
	}

	// Create new request
	code, err := generateUniqueCode(existingCodes)
	if err != nil {
		return "", false, err
	}

	request := &Request{
		ID:         id,
		Code:       code,
		CreatedAt:  now,
		LastSeenAt: now,
		Meta:       meta,
	}

	store.Requests = append(pruned, request)
	if existingIdx >= 0 {
		store.Requests[existingIdx] = request
	}

	if err := s.writeStore(channel, store); err != nil {
		return "", false, err
	}

	return code, true, nil
}

// ApproveCode approves a pairing code and adds the ID to the allowlist.
// Returns the request ID if found, or ErrCodeNotFound.
func (s *Store) ApproveCode(channel, code string) (string, *Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "", nil, ErrCodeNotFound
	}

	store, err := s.readStore(channel)
	if err != nil {
		return "", nil, err
	}

	pruned := pruneExpired(store.Requests)

	// Find the request with this code
	var found *Request
	foundIdx := -1
	for i, r := range pruned {
		if strings.ToUpper(r.Code) == code {
			found = r
			foundIdx = i
			break
		}
	}

	if found == nil {
		// Save pruned state
		if len(pruned) != len(store.Requests) {
			store.Requests = pruned
			_ = s.writeStore(channel, store)
		}
		return "", nil, ErrCodeNotFound
	}

	// Remove from pending requests
	store.Requests = append(pruned[:foundIdx], pruned[foundIdx+1:]...)
	if err := s.writeStore(channel, store); err != nil {
		return "", nil, err
	}

	// Add to allowlist
	if err := s.addToAllowlist(channel, found.ID); err != nil {
		return "", nil, err
	}

	return found.ID, found, nil
}

// readAllowlist reads the allowlist for a channel.
func (s *Store) readAllowlist(channel string) ([]string, error) {
	path, err := s.allowFromPath(channel)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var store allowFromData
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, nil
	}
	return store.AllowFrom, nil
}

// writeAllowlist writes the allowlist for a channel.
func (s *Store) writeAllowlist(channel string, allowFrom []string) error {
	path, err := s.allowFromPath(channel)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	store := allowFromData{
		Version:   1,
		AllowFrom: allowFrom,
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// addToAllowlist adds an entry to the channel's allowlist.
func (s *Store) addToAllowlist(channel, entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil
	}

	allowFrom, err := s.readAllowlist(channel)
	if err != nil {
		return err
	}

	// Check if already exists
	for _, e := range allowFrom {
		if e == entry {
			return nil
		}
	}

	allowFrom = append(allowFrom, entry)
	return s.writeAllowlist(channel, allowFrom)
}

// GetAllowlist returns the allowlist for a channel.
func (s *Store) GetAllowlist(channel string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readAllowlist(channel)
}

// AddToAllowlist adds an entry to the channel's allowlist.
func (s *Store) AddToAllowlist(channel, entry string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addToAllowlist(channel, entry)
}

// RemoveFromAllowlist removes an entry from the channel's allowlist.
func (s *Store) RemoveFromAllowlist(channel, entry string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil
	}

	allowFrom, err := s.readAllowlist(channel)
	if err != nil {
		return err
	}

	// Find and remove
	found := false
	result := make([]string, 0, len(allowFrom))
	for _, e := range allowFrom {
		if e == entry {
			found = true
			continue
		}
		result = append(result, e)
	}

	if !found {
		return nil
	}

	return s.writeAllowlist(channel, result)
}

// IsAllowed checks if an ID is in the channel's allowlist.
func (s *Store) IsAllowed(channel, id string) (bool, error) {
	allowFrom, err := s.GetAllowlist(channel)
	if err != nil {
		return false, err
	}

	id = strings.TrimSpace(id)
	for _, e := range allowFrom {
		if e == id {
			return true, nil
		}
	}
	return false, nil
}
